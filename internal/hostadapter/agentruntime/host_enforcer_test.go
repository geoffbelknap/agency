package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHostEnforcerSupervisorStartStop(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "env.txt")
	script := filepath.Join(dir, "enforcer-test")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nenv | sort > \"$AGENCY_TEST_ENV_FILE\"\ntrap 'exit 0' TERM\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	supervisor := &HostEnforcerSupervisor{
		BinaryPath:  script,
		StopTimeout: time.Second,
	}
	spec := EnforcerLaunchSpec{
		AgentName:          "alice",
		ProxyHostPort:      "19128",
		ConstraintHostPort: "19081",
		Env: map[string]string{
			"AGENCY_TEST_ENV_FILE": envFile,
		},
		Mounts: []EnforcerMount{
			{HostPath: filepath.Join(dir, "auth"), GuestPath: "/agency/enforcer/auth", Mode: "ro"},
			{HostPath: filepath.Join(dir, "data"), GuestPath: "/agency/enforcer/data", Mode: "rw"},
		},
	}
	if err := supervisor.Start(context.Background(), spec, map[string]string{"comms": "http://127.0.0.1:8202"}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	waitForFile(t, envFile)
	env, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	for _, want := range []string{
		"AGENCY_TEST_ENV_FILE=" + envFile,
		"API_KEYS_FILE=" + filepath.Join(dir, "auth", "api_keys.yaml"),
		"COMMS_URL=http://127.0.0.1:8202",
		"CONSTRAINT_WS_PORT=19081",
		"ENFORCER_PORT=19128",
		"HOME=" + filepath.Join(dir, "data"),
	} {
		if !strings.Contains(string(env), want+"\n") {
			t.Fatalf("env missing %q in:\n%s", want, string(env))
		}
	}
	status, err := supervisor.Inspect("alice")
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	if status.State != HostEnforcerStateRunning || status.PID <= 0 {
		t.Fatalf("unexpected running status: %#v", status)
	}
	if err := supervisor.Stop(context.Background(), "alice"); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	status, err = supervisor.Inspect("alice")
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	if status.State != HostEnforcerStateStopped {
		t.Fatalf("state = %q, want %q", status.State, HostEnforcerStateStopped)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}
