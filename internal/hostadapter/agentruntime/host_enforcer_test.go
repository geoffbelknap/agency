package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
		StateDir:    filepath.Join(dir, "state"),
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
		"CONSTRAINT_WS_BIND_ADDR=127.0.0.1",
		"CONSTRAINT_WS_PORT=19081",
		"ENFORCER_BIND_ADDR=127.0.0.1",
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
	if _, err := os.Stat(filepath.Join(dir, "state", "alice.json")); err != nil {
		t.Fatalf("host enforcer state was not persisted: %v", err)
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
	if _, err := os.Stat(filepath.Join(dir, "state", "alice.json")); !os.IsNotExist(err) {
		t.Fatalf("host enforcer state still exists after stop: %v", err)
	}
}

func TestHostEnforcerSupervisorSignal(t *testing.T) {
	dir := t.TempDir()
	readyFile := filepath.Join(dir, "ready.txt")
	reloadFile := filepath.Join(dir, "reload.txt")
	script := filepath.Join(dir, "enforcer-test")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'echo reload > \"$AGENCY_TEST_RELOAD_FILE\"' HUP\ntrap 'exit 0' TERM\necho ready > \"$AGENCY_TEST_READY_FILE\"\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
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
			"AGENCY_TEST_READY_FILE":  readyFile,
			"AGENCY_TEST_RELOAD_FILE": reloadFile,
		},
	}
	if err := supervisor.Start(context.Background(), spec, nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = supervisor.Stop(context.Background(), "alice")
	})
	waitForFile(t, readyFile)
	if err := supervisor.Signal("alice", syscall.SIGHUP); err != nil {
		t.Fatalf("Signal returned error: %v", err)
	}
	waitForFile(t, reloadFile)
}

func TestHostEnforcerSupervisorMarksUnexpectedExitCrashed(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "enforcer-test")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 42\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	supervisor := &HostEnforcerSupervisor{BinaryPath: script}
	spec := EnforcerLaunchSpec{
		AgentName:          "alice",
		ProxyHostPort:      "19128",
		ConstraintHostPort: "19081",
	}
	if err := supervisor.Start(context.Background(), spec, nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	status := waitForEnforcerStatus(t, supervisor, "alice", HostEnforcerStateCrashed)
	if status.ExitCode != 42 {
		t.Fatalf("exit code = %d, want 42", status.ExitCode)
	}
	if status.LastError == "" {
		t.Fatal("expected last error for crashed enforcer")
	}
}

func TestHostEnforcerSupervisorRestoresPersistedProcess(t *testing.T) {
	dir := t.TempDir()
	readyFile := filepath.Join(dir, "ready.txt")
	script := filepath.Join(dir, "enforcer-test")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'exit 0' TERM\necho ready > \"$AGENCY_TEST_READY_FILE\"\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(dir, "state")
	first := &HostEnforcerSupervisor{
		BinaryPath:  script,
		StateDir:    stateDir,
		StopTimeout: time.Second,
	}
	spec := EnforcerLaunchSpec{
		AgentName:          "alice",
		ProxyHostPort:      "19128",
		ConstraintHostPort: "19081",
		Env: map[string]string{
			"AGENCY_TEST_READY_FILE": readyFile,
		},
	}
	if err := first.Start(context.Background(), spec, nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	waitForFile(t, readyFile)

	restored := &HostEnforcerSupervisor{
		StateDir:    stateDir,
		StopTimeout: time.Second,
	}
	status, err := restored.Inspect("alice")
	if err != nil {
		t.Fatalf("Inspect restored process returned error: %v", err)
	}
	if status.State != HostEnforcerStateRunning || status.PID <= 0 {
		t.Fatalf("unexpected restored status: %#v", status)
	}
	if err := restored.Start(context.Background(), spec, nil); err != nil {
		t.Fatalf("Start should be idempotent for restored process: %v", err)
	}
	if err := restored.Stop(context.Background(), "alice"); err != nil {
		t.Fatalf("Stop restored process returned error: %v", err)
	}
	if processAlive(status.PID) {
		t.Fatalf("restored process pid %d still alive after stop", status.PID)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "alice.json")); !os.IsNotExist(err) {
		t.Fatalf("host enforcer state still exists after restored stop: %v", err)
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

func waitForEnforcerStatus(t *testing.T, supervisor *HostEnforcerSupervisor, agentName, state string) HostEnforcerStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last HostEnforcerStatus
	for time.Now().Before(deadline) {
		status, err := supervisor.Inspect(agentName)
		if err == nil {
			last = status
			if status.State == state {
				return status
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for enforcer %s state %s; last=%#v", agentName, state, last)
	return HostEnforcerStatus{}
}
