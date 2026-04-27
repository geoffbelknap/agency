package main

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRunHealth(t *testing.T) {
	var calls [][]string
	var out bytes.Buffer
	err := run([]string{"health"}, &out, func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		calls = append(calls, append([]string(nil), args...))
		switch {
		case reflect.DeepEqual(args, []string{"system", "status", "--format", "json"}):
			return []byte(`{"status":"running"}`), nil, nil
		case reflect.DeepEqual(args, []string{"system", "version", "--format", "json"}):
			return []byte(`{"version":"0.11.0"}`), nil, nil
		default:
			t.Fatalf("unexpected args: %#v", args)
		}
		return nil, nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var resp healthResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Backend != backendName || resp.EventSupport != "none" || resp.Version == "" {
		t.Fatalf("response = %#v", resp)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestRunListOwnedFiltersByLabels(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"list-owned", "--home-hash", "home-a"}, &out, func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		if !reflect.DeepEqual(args, []string{"list", "--format", "json", "--all"}) {
			t.Fatalf("args = %#v", args)
		}
		return []byte(`[
			{"status":"running","configuration":{"id":"owned","labels":{"agency.managed":"true","agency.backend":"apple-container","agency.home":"home-a"}}},
			{"status":"running","configuration":{"id":"other-home","labels":{"agency.managed":"true","agency.backend":"apple-container","agency.home":"home-b"}}},
			{"status":"running","configuration":{"id":"docker","labels":{"agency.managed":"true","agency.backend":"docker","agency.home":"home-a"}}},
			{"status":"running","configuration":{"id":"unmanaged","labels":{"agency.backend":"apple-container","agency.home":"home-a"}}}
		]`), nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var resp listOwnedResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || len(resp.Containers) != 1 || resp.Containers[0].Configuration.ID != "owned" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRunInspectWrapsContainerOutput(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"inspect", "owned"}, &out, func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		if !reflect.DeepEqual(args, []string{"inspect", "owned"}) {
			t.Fatalf("args = %#v", args)
		}
		return []byte(`{"status":"running","configuration":{"id":"owned","labels":{"agency.managed":"true"}}}`), nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var resp inspectResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || len(resp.Containers) != 1 || resp.Containers[0].Configuration.ID != "owned" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRunLifecycleCommands(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantCalls [][]string
		status    string
		eventType string
	}{
		{name: "start", args: []string{"start", "owned"}, wantCalls: [][]string{{"start", "owned"}, {"inspect", "owned"}}, status: "running", eventType: "runtime.container.started"},
		{name: "stop", args: []string{"stop", "--time", "7", "owned"}, wantCalls: [][]string{{"stop", "--time", "7", "owned"}, {"inspect", "owned"}}, status: "stopped", eventType: "runtime.container.stopped"},
		{name: "kill", args: []string{"kill", "--signal", "KILL", "owned"}, wantCalls: [][]string{{"kill", "--signal", "KILL", "owned"}, {"inspect", "owned"}}, status: "stopped", eventType: "runtime.container.killed"},
		{name: "delete", args: []string{"delete", "--force", "owned"}, wantCalls: [][]string{{"inspect", "owned"}, {"delete", "--force", "owned"}}, status: "stopped", eventType: "runtime.container.deleted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls [][]string
			var out bytes.Buffer
			err := run(tt.args, &out, func(ctx context.Context, args ...string) ([]byte, []byte, error) {
				calls = append(calls, append([]string(nil), args...))
				if len(calls) > len(tt.wantCalls) || !reflect.DeepEqual(args, tt.wantCalls[len(calls)-1]) {
					t.Fatalf("call %d args = %#v, want sequence %#v", len(calls), args, tt.wantCalls)
				}
				if args[0] == "inspect" {
					return []byte(`{"status":"` + tt.status + `","configuration":{"id":"owned","labels":{"agency.managed":"true","agency.backend":"apple-container","agency.home":"home-a","agency.agent":"henry","agency.role":"workspace"}}}`), nil, nil
				}
				return []byte("ok\n"), nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, tt.wantCalls) {
				t.Fatalf("calls = %#v, want %#v", calls, tt.wantCalls)
			}
			var resp operationResponse
			if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if !resp.OK || resp.Backend != backendName || resp.ContainerID != "owned" || resp.Output != "ok\n" || resp.Event == nil {
				t.Fatalf("response = %#v", resp)
			}
			if resp.Event.EventType != tt.eventType || resp.Event.Data["agent"] != "henry" {
				t.Fatalf("event = %#v", resp.Event)
			}
		})
	}
}

func TestRunExecUsesStructuredArgs(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"exec", "--user", "root", "--workdir", "/workspace", "owned", "sh", "-c", "id"}, &out, func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		want := []string{"exec", "--user", "root", "--workdir", "/workspace", "owned", "sh", "-c", "id"}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("args = %#v, want %#v", args, want)
		}
		return []byte("uid=0(root)\n"), nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var resp operationResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.ContainerID != "owned" || resp.Output != "uid=0(root)\n" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRunEventsOnceEmitsOwnedJSONL(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"events", "--once", "--home-hash", "home-a"}, &out, func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		if !reflect.DeepEqual(args, []string{"list", "--format", "json", "--all"}) {
			t.Fatalf("args = %#v", args)
		}
		return []byte(`[
			{"status":"running","configuration":{"id":"agency-henry-workspace","labels":{"agency.managed":"true","agency.backend":"apple-container","agency.home":"home-a","agency.agent":"henry","agency.role":"workspace","agency.instance":"inst-1"}}},
			{"status":"stopped","configuration":{"id":"other-home","labels":{"agency.managed":"true","agency.backend":"apple-container","agency.home":"home-b","agency.agent":"old"}}},
			{"status":"running","configuration":{"id":"unmanaged","labels":{"agency.backend":"apple-container","agency.home":"home-a"}}}
		]`), nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("lines = %d: %s", len(lines), out.String())
	}
	var event helperEvent
	if err := json.Unmarshal(lines[0], &event); err != nil {
		t.Fatal(err)
	}
	if event.SourceType != "platform" || event.SourceName != "host-adapter/apple-container" || event.EventType != "runtime.container.started" {
		t.Fatalf("event = %#v", event)
	}
	if event.Data["container_id"] != "agency-henry-workspace" || event.Data["agent"] != "henry" || event.Data["reason"] != "reconcile_once" {
		t.Fatalf("data = %#v", event.Data)
	}
	if event.Metadata["agency_home_hash"] != "home-a" || event.Metadata["owned"] != true {
		t.Fatalf("metadata = %#v", event.Metadata)
	}
}

func TestRunEventsRequiresOnce(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"events"}, &out, func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		t.Fatalf("container command should not run: %#v", args)
		return nil, nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "wait-backed helper") {
		t.Fatalf("err = %v", err)
	}
}

func TestAppleContainerCommandEnvDropsAgencyHome(t *testing.T) {
	got := appleContainerCommandEnv([]string{"PATH=/bin", "AGENCY_HOME=/tmp/agency", "HOME=/Users/test"})
	want := []string{"PATH=/bin", "HOME=/Users/test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("env = %#v, want %#v", got, want)
	}
}
