package runtimehost

import (
	"context"
	"reflect"
	"testing"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockerevents "github.com/docker/docker/api/types/events"
	dockerfilters "github.com/docker/docker/api/types/filters"
)

func TestAppleContainerHelperHealth(t *testing.T) {
	var calls [][]string
	helper := &appleContainerHelperClient{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if !reflect.DeepEqual(args, []string{"health"}) {
			t.Fatalf("args = %#v", args)
		}
		return []byte(`{"ok":true,"backend":"apple-container","event_support":"none"}`), nil, nil
	}}

	health, err := helper.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if health.Backend != BackendAppleContainer || health.EventSupport != "none" {
		t.Fatalf("health = %#v", health)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestAppleContainerHelperListOwned(t *testing.T) {
	helper := &appleContainerHelperClient{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		if !reflect.DeepEqual(args, []string{"list-owned", "--home-hash", "abc123"}) {
			t.Fatalf("args = %#v", args)
		}
		return []byte(`{
			"ok": true,
			"backend": "apple-container",
			"containers": [{
				"status": "running",
				"configuration": {
					"id": "agency-henry-workspace",
					"labels": {
						"agency.managed": "true",
						"agency.backend": "apple-container",
						"agency.home": "abc123"
					}
				}
			}]
		}`), nil, nil
	}}

	containers, err := helper.ListOwned(context.Background(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(containers) != 1 || containers[0].Configuration.ID != "agency-henry-workspace" {
		t.Fatalf("containers = %#v", containers)
	}
}

func TestAppleContainerHelperLifecycleOperations(t *testing.T) {
	tests := []struct {
		name string
		run  func(context.Context, *appleContainerHelperClient) error
		want []string
	}{
		{name: "start", run: func(ctx context.Context, helper *appleContainerHelperClient) error {
			_, err := helper.Start(ctx, "owned")
			return err
		}, want: []string{"start", "owned"}},
		{name: "stop", run: func(ctx context.Context, helper *appleContainerHelperClient) error {
			_, err := helper.Stop(ctx, "owned", 7)
			return err
		}, want: []string{"stop", "--time", "7", "owned"}},
		{name: "kill", run: func(ctx context.Context, helper *appleContainerHelperClient) error {
			_, err := helper.Kill(ctx, "owned", "KILL")
			return err
		}, want: []string{"kill", "--signal", "KILL", "owned"}},
		{name: "delete", run: func(ctx context.Context, helper *appleContainerHelperClient) error {
			_, err := helper.Delete(ctx, "owned", true)
			return err
		}, want: []string{"delete", "--force", "owned"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helper := &appleContainerHelperClient{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
				if !reflect.DeepEqual(args, tt.want) {
					t.Fatalf("args = %#v, want %#v", args, tt.want)
				}
				return []byte(`{"ok":true,"backend":"apple-container","container_id":"owned","event":{"id":"evt-runtime-1","source_type":"platform","source_name":"host-adapter/apple-container","event_type":"runtime.container.started","timestamp":"2026-04-26T00:00:00Z","data":{"container_id":"owned"}}}`), nil, nil
			}}
			if err := tt.run(context.Background(), helper); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAppleContainerHelperExec(t *testing.T) {
	helper := &appleContainerHelperClient{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		want := []string{"exec", "--user", "root", "--workdir", "/workspace", "owned", "sh", "-c", "id"}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("args = %#v, want %#v", args, want)
		}
		return []byte(`{"ok":true,"backend":"apple-container","container_id":"owned","output":"uid=0(root)\n"}`), nil, nil
	}}
	out, err := helper.Exec(context.Background(), "owned", "root", "/workspace", []string{"sh", "-c", "id"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "uid=0(root)\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestAppleContainerHelperEventsOnce(t *testing.T) {
	helper := &appleContainerHelperClient{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		want := []string{"events", "--once", "--home-hash", "home-a"}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("args = %#v, want %#v", args, want)
		}
		return []byte(`{"id":"evt-runtime-1","source_type":"platform","source_name":"host-adapter/apple-container","event_type":"runtime.container.started","timestamp":"2026-04-26T00:00:00Z","data":{"container_id":"owned"},"metadata":{"owned":true}}
{"id":"evt-runtime-2","source_type":"platform","source_name":"host-adapter/apple-container","event_type":"runtime.container.stopped","timestamp":"2026-04-26T00:00:01Z","data":{"container_id":"old"},"metadata":{"owned":true}}
`), nil, nil
	}}
	events, err := helper.EventsOnce(context.Background(), "home-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].EventType != "runtime.container.started" || events[1].Data["container_id"] != "old" {
		t.Fatalf("events = %#v", events)
	}
}

func TestAppleContainerHelperFromConfig(t *testing.T) {
	t.Setenv("AGENCY_APPLE_CONTAINER_HELPER_BIN", "")
	if _, ok := appleContainerHelperFromConfig(nil); ok {
		t.Fatal("expected helper to be absent without env or config")
	}
	helper, ok := appleContainerHelperFromConfig(map[string]string{"helper_binary": "/tmp/helper"})
	if !ok {
		t.Fatal("expected helper from config")
	}
	if helper.binary != "/tmp/helper" {
		t.Fatalf("helper binary = %q", helper.binary)
	}
}

func TestAppleContainerWaitHelperFromConfig(t *testing.T) {
	t.Setenv("AGENCY_APPLE_CONTAINER_WAIT_HELPER_BIN", "")
	if _, ok := appleContainerWaitHelperFromConfig(nil); ok {
		t.Fatal("expected wait helper to be absent without env or config")
	}
	helper, ok := appleContainerWaitHelperFromConfig(map[string]string{"wait_helper_binary": "/tmp/wait-helper"})
	if !ok {
		t.Fatal("expected wait helper from config")
	}
	if helper.binary != "/tmp/wait-helper" {
		t.Fatalf("wait helper binary = %q", helper.binary)
	}
}

func TestAppleContainerWaitHelperHealth(t *testing.T) {
	helper := &appleContainerWaitHelperClient{runHealth: func(ctx context.Context) ([]byte, []byte, error) {
		return []byte(`{"ok":true,"backend":"apple-container","event_support":"process_wait"}`), nil, nil
	}}
	health, err := helper.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if health.Backend != BackendAppleContainer || health.EventSupport != "process_wait" {
		t.Fatalf("health = %#v", health)
	}
}

func TestAppleContainerPingUsesConfiguredHelper(t *testing.T) {
	client := &RawClient{
		backend: BackendAppleContainer,
		appleContainer: &appleContainerConfig{helper: &appleContainerHelperClient{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			if !reflect.DeepEqual(args, []string{"health"}) {
				t.Fatalf("args = %#v", args)
			}
			return []byte(`{"ok":true,"backend":"apple-container","event_support":"none"}`), nil, nil
		}}},
	}
	ping, err := client.Ping(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ping.APIVersion != "apple-container/helper" {
		t.Fatalf("ping = %#v", ping)
	}
}

func TestAppleContainerRawClientStartUsesWaitHelperAndPublishesExit(t *testing.T) {
	waitEvents := make(chan AppleContainerHelperEvent, 2)
	waitEvents <- AppleContainerHelperEvent{
		EventType: "runtime.container.started",
		Timestamp: "2026-04-26T00:00:00Z",
		Data:      map[string]any{"container_id": "agency-henry-workspace"},
	}
	waitEvents <- AppleContainerHelperEvent{
		EventType: "runtime.container.exited",
		Timestamp: "2026-04-26T00:00:01Z",
		Data:      map[string]any{"container_id": "agency-henry-workspace", "exit_code": "7"},
	}
	close(waitEvents)
	waitErrs := make(chan error)
	close(waitErrs)
	client := &RawClient{
		backend: BackendAppleContainer,
		appleContainer: &appleContainerConfig{
			waitHelper: &appleContainerWaitHelperClient{run: func(ctx context.Context, containerID string) (<-chan AppleContainerHelperEvent, <-chan error, error) {
				if containerID != "agency-henry-workspace" {
					t.Fatalf("containerID = %q", containerID)
				}
				return waitEvents, waitErrs, nil
			}},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, errs := client.Events(ctx, dockerevents.ListOptions{Filters: dockerfilters.NewArgs(dockerfilters.Arg("event", "die"))})

	if err := client.ContainerStart(context.Background(), "agency-henry-workspace", dockercontainer.StartOptions{}); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errs:
		t.Fatalf("unexpected stream error: %v", err)
	case ev := <-events:
		if ev.Action != "die" || ev.Actor.Attributes["name"] != "agency-henry-workspace" || ev.Actor.Attributes["exitCode"] != "7" {
			t.Fatalf("event = %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for exit event")
	}
}

func TestAppleContainerRawClientUsesHelperForSupportedLifecycle(t *testing.T) {
	var calls [][]string
	client := &RawClient{
		backend: BackendAppleContainer,
		appleContainer: &appleContainerConfig{helper: &appleContainerHelperClient{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			calls = append(calls, append([]string(nil), args...))
			return []byte(`{"ok":true,"backend":"apple-container","container_id":"owned","output":"ok\n","event":{"id":"evt-runtime-1","source_type":"platform","source_name":"host-adapter/apple-container","event_type":"runtime.container.started","timestamp":"2026-04-26T00:00:00Z","data":{"container_id":"owned"}}}`), nil, nil
		}}},
	}
	timeout := 3
	if err := client.ContainerStart(context.Background(), "owned", dockercontainer.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := client.ContainerStop(context.Background(), "owned", dockercontainer.StopOptions{Timeout: &timeout}); err != nil {
		t.Fatal(err)
	}
	if err := client.ContainerKill(context.Background(), "owned", "KILL"); err != nil {
		t.Fatal(err)
	}
	if err := client.ContainerRemove(context.Background(), "owned", dockercontainer.RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	out, err := client.Exec(context.Background(), "owned", "root", []string{"sh", "-c", "id"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok\n" {
		t.Fatalf("out = %q", out)
	}

	want := [][]string{
		{"start", "owned"},
		{"stop", "--time", "3", "owned"},
		{"kill", "--signal", "KILL", "owned"},
		{"delete", "--force", "owned"},
		{"exec", "--user", "root", "owned", "sh", "-c", "id"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestAppleContainerRawClientStreamsHelperLifecycleEvents(t *testing.T) {
	client := &RawClient{
		backend: BackendAppleContainer,
		appleContainer: &appleContainerConfig{helper: &appleContainerHelperClient{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			return []byte(`{"ok":true,"backend":"apple-container","container_id":"agency-henry-workspace","event":{"id":"evt-runtime-1","source_type":"platform","source_name":"host-adapter/apple-container","event_type":"runtime.container.exited","timestamp":"2026-04-26T00:00:00Z","data":{"container_id":"agency-henry-workspace","exit_code":"7"}}}`), nil, nil
		}}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, errs := client.Events(ctx, dockerevents.ListOptions{Filters: dockerfilters.NewArgs(dockerfilters.Arg("event", "die"))})

	if err := client.ContainerStop(context.Background(), "agency-henry-workspace", dockercontainer.StopOptions{}); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errs:
		t.Fatalf("unexpected event stream error: %v", err)
	case ev := <-events:
		if ev.Action != "die" || ev.Actor.Attributes["name"] != "agency-henry-workspace" || ev.Actor.Attributes["exitCode"] != "7" {
			t.Fatalf("event = %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for helper lifecycle event")
	}
}

func TestAppleContainerRawClientEventsSeedFromHelperReconcile(t *testing.T) {
	client := &RawClient{
		backend: BackendAppleContainer,
		appleContainer: &appleContainerConfig{helper: &appleContainerHelperClient{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			want := []string{"events", "--once"}
			if !reflect.DeepEqual(args, want) {
				t.Fatalf("args = %#v, want %#v", args, want)
			}
			return []byte(`{"id":"evt-runtime-1","source_type":"platform","source_name":"host-adapter/apple-container","event_type":"runtime.container.stopped","timestamp":"2026-04-26T00:00:00Z","data":{"container_id":"agency-henry-workspace","exit_code":"137"},"metadata":{"owned":true}}`), nil, nil
		}}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, errs := client.Events(ctx, dockerevents.ListOptions{Filters: dockerfilters.NewArgs(dockerfilters.Arg("event", "die"))})

	select {
	case err := <-errs:
		t.Fatalf("unexpected event stream error: %v", err)
	case ev := <-events:
		if ev.Action != "die" || ev.Actor.Attributes["name"] != "agency-henry-workspace" || ev.Actor.Attributes["exitCode"] != "137" {
			t.Fatalf("event = %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reconcile event")
	}
}
