package runtimehost

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	dockertypes "github.com/docker/docker/api/types"
)

func TestKnownBackendsForLinux(t *testing.T) {
	got := knownBackendsFor("linux")
	wantOrder := []string{BackendPodman, BackendDocker, BackendContainerd}
	if len(got) != len(wantOrder) {
		t.Fatalf("want %d backends on linux, got %d: %+v", len(wantOrder), len(got), got)
	}
	for i, name := range wantOrder {
		if got[i].Name != name {
			t.Errorf("position %d: want %q, got %q", i, name, got[i].Name)
		}
	}
}

func TestKnownBackendsForDarwin(t *testing.T) {
	got := knownBackendsFor("darwin")
	wantOrder := []string{BackendPodman, BackendDocker}
	if len(got) != len(wantOrder) {
		t.Fatalf("want %d backends on darwin, got %d", len(wantOrder), len(got))
	}
	for i, name := range wantOrder {
		if got[i].Name != name {
			t.Errorf("position %d: want %q, got %q", i, name, got[i].Name)
		}
	}
}

func TestKnownBackendsForWSL(t *testing.T) {
	got := knownBackendsFor("wsl")
	if len(got) != 3 {
		t.Fatalf("want 3 backends on wsl, got %d", len(got))
	}
}

func TestKnownBackendsForWindows(t *testing.T) {
	got := knownBackendsFor("windows")
	if len(got) != 1 || got[0].Name != BackendDocker {
		t.Fatalf("want docker-only on windows, got %+v", got)
	}
}

// fakePingable implements the pingable interface with a canned response.
type fakePingable struct{ err error }

func (f *fakePingable) Ping(ctx context.Context) (dockertypes.Ping, error) {
	return dockertypes.Ping{}, f.err
}

// withFakeClientFactory swaps clientFactory for the duration of the test.
func withFakeClientFactory(t *testing.T, fn func(backend string, cfg map[string]string) (pingable, error)) {
	t.Helper()
	orig := clientFactory
	clientFactory = fn
	t.Cleanup(func() { clientFactory = orig })
}

// withFakeLookPath swaps lookPath for the duration of the test.
func withFakeLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	orig := lookPath
	lookPath = fn
	t.Cleanup(func() { lookPath = orig })
}

func TestProbeBackendReachable(t *testing.T) {
	withFakeLookPath(t, func(cmd string) (string, error) {
		return "/usr/bin/" + cmd, nil
	})
	withFakeClientFactory(t, func(backend string, cfg map[string]string) (pingable, error) {
		return &fakePingable{}, nil
	})
	// Seed a fake XDG_RUNTIME_DIR-based podman socket path resolution by
	// passing a probe that will fall back to resolveBackendHost default.
	// This test does not require the socket to exist on disk because the
	// client factory is faked.
	t.Setenv("PODMAN_HOST", "unix:///tmp/fake-podman.sock")
	d := ProbeBackend(BackendProbe{Name: BackendPodman, CLICommand: "podman"})
	if !d.Reachable {
		t.Fatalf("want Reachable=true, got %+v", d)
	}
	if !d.CLIFound {
		t.Errorf("want CLIFound=true")
	}
	if d.Endpoint == "" {
		t.Errorf("want Endpoint populated")
	}
}

func TestProbeBackendPingFails(t *testing.T) {
	withFakeLookPath(t, func(cmd string) (string, error) { return "/usr/bin/" + cmd, nil })
	pingErr := errors.New("connection refused")
	withFakeClientFactory(t, func(backend string, cfg map[string]string) (pingable, error) {
		return &fakePingable{err: pingErr}, nil
	})
	t.Setenv("PODMAN_HOST", "unix:///tmp/fake-podman.sock")
	d := ProbeBackend(BackendProbe{Name: BackendPodman, CLICommand: "podman"})
	if d.Reachable {
		t.Fatal("want Reachable=false when Ping errors")
	}
	if d.Err == nil || !strings.Contains(d.Err.Error(), "ping") {
		t.Errorf("want ping error, got %v", d.Err)
	}
}

func TestProbeBackendCLINotFound(t *testing.T) {
	withFakeLookPath(t, func(cmd string) (string, error) {
		return "", exec.ErrNotFound
	})
	withFakeClientFactory(t, func(backend string, cfg map[string]string) (pingable, error) {
		return &fakePingable{}, nil
	})
	t.Setenv("PODMAN_HOST", "unix:///tmp/fake-podman.sock")
	d := ProbeBackend(BackendProbe{Name: BackendPodman, CLICommand: "podman"})
	if d.CLIFound {
		t.Error("want CLIFound=false when lookPath errors")
	}
	// CLI absence should not block reachability — the SDK talks to the socket directly.
	if !d.Reachable {
		t.Errorf("want Reachable=true even without CLI when socket responds")
	}
}

func TestProbeBackendNoEndpoint(t *testing.T) {
	// resolveBackendHost returns "" for an unknown backend name — the cleanest
	// way to exercise the no-endpoint path without depending on the state of
	// real docker/podman sockets on the host the tests run on.
	withFakeLookPath(t, func(cmd string) (string, error) { return "/usr/bin/" + cmd, nil })
	withFakeClientFactory(t, func(backend string, cfg map[string]string) (pingable, error) {
		t.Fatal("clientFactory should not be called when endpoint is empty")
		return nil, nil
	})
	d := ProbeBackend(BackendProbe{Name: "nonexistent-backend", CLICommand: "nothing"})
	if d.Reachable {
		t.Fatal("want Reachable=false when no endpoint resolves")
	}
	if d.Err == nil || !strings.Contains(d.Err.Error(), "endpoint") {
		t.Errorf("want endpoint error, got %v", d.Err)
	}
}

func TestPreferredReachableFirstWins(t *testing.T) {
	dets := []BackendDetection{
		{Probe: BackendProbe{Name: BackendPodman}, Reachable: false},
		{Probe: BackendProbe{Name: BackendDocker}, Reachable: true},
		{Probe: BackendProbe{Name: BackendContainerd}, Reachable: true},
	}
	got, ok := PreferredReachable(dets)
	if !ok {
		t.Fatal("want ok=true")
	}
	if got.Name() != BackendDocker {
		t.Errorf("want docker (first reachable), got %q", got.Name())
	}
}

func TestPreferredReachableNone(t *testing.T) {
	dets := []BackendDetection{
		{Probe: BackendProbe{Name: BackendPodman}, Reachable: false},
		{Probe: BackendProbe{Name: BackendDocker}, Reachable: false},
	}
	_, ok := PreferredReachable(dets)
	if ok {
		t.Error("want ok=false when none reachable")
	}
}

func TestSelectReachableFilters(t *testing.T) {
	dets := []BackendDetection{
		{Probe: BackendProbe{Name: BackendPodman}, Reachable: true},
		{Probe: BackendProbe{Name: BackendDocker}, Reachable: false},
		{Probe: BackendProbe{Name: BackendContainerd}, Reachable: true},
	}
	got := SelectReachable(dets)
	if len(got) != 2 {
		t.Fatalf("want 2 reachable, got %d", len(got))
	}
	if got[0].Name() != BackendPodman || got[1].Name() != BackendContainerd {
		t.Errorf("wrong order: %+v", got)
	}
}

func TestInstallHintNonEmptyPerPlatform(t *testing.T) {
	// InstallHint() branches on currentPlatform(), which reads runtime.GOOS.
	// We can't change GOOS at test time; verify the current host's hint is
	// non-empty and actionable.
	h := InstallHint()
	if strings.TrimSpace(h) == "" {
		t.Fatal("InstallHint returned empty")
	}
	if !strings.Contains(strings.ToLower(h), "podman") &&
		!strings.Contains(strings.ToLower(h), "docker") {
		t.Errorf("InstallHint should mention podman or docker; got: %s", h)
	}
}
