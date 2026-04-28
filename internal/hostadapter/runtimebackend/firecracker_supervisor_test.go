package runtimebackend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func TestFirecrackerVMSupervisorStopEscalatesToKillProcessGroup(t *testing.T) {
	s := &FirecrackerVMSupervisor{
		BinaryPath:  "/bin/sh",
		StopTimeout: 50 * time.Millisecond,
	}
	spec := runtimecontract.RuntimeSpec{RuntimeID: "alice"}
	if err := s.Start(context.Background(), spec, []string{"-c", "trap '' TERM; sleep 30"}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := waitForFirecrackerVM(t, s, "alice", func(status FirecrackerVMStatus) bool {
		return status.State == FirecrackerVMRunning && status.PID > 0
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(context.Background(), "alice"); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if err := waitForFirecrackerVM(t, s, "alice", func(status FirecrackerVMStatus) bool {
		return status.State == FirecrackerVMStopped
	}); err != nil {
		t.Fatal(err)
	}
	status, err := s.Inspect("alice")
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	if status.Duration <= 0 {
		t.Fatalf("duration = %s, want > 0", status.Duration)
	}
}

func TestFirecrackerVMSupervisorRestartsOnFailure(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "attempts")
	s := &FirecrackerVMSupervisor{
		BinaryPath:     "/bin/sh",
		RestartBackoff: 10 * time.Millisecond,
	}
	spec := runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		Lifecycle: runtimecontract.RuntimeLifecycleSpec{
			RestartPolicy: "on-failure",
		},
	}
	if err := s.Start(context.Background(), spec, []string{"-c", "echo x >> " + marker + "; exit 1"}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := waitForFirecrackerVM(t, s, "alice", func(status FirecrackerVMStatus) bool {
		data, _ := os.ReadFile(marker)
		return status.Restarts >= 1 && status.Crashes >= 1 && len(data) >= 4
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(context.Background(), "alice"); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if len(data) < 4 {
		t.Fatalf("expected more than one attempt, marker=%q", string(data))
	}
}

func TestFirecrackerVMSupervisorNeverRestartPolicyLeavesCrashState(t *testing.T) {
	s := &FirecrackerVMSupervisor{BinaryPath: "/bin/sh"}
	spec := runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		Lifecycle: runtimecontract.RuntimeLifecycleSpec{
			RestartPolicy: "never",
		},
	}
	if err := s.Start(context.Background(), spec, []string{"-c", "exit 1"}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := waitForFirecrackerVM(t, s, "alice", func(status FirecrackerVMStatus) bool {
		return status.State == FirecrackerVMCrashed
	}); err != nil {
		t.Fatal(err)
	}
	status, err := s.Inspect("alice")
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	if status.Restarts != 0 || status.Crashes != 1 {
		t.Fatalf("status = %#v", status)
	}
	if status.ExitCode != 1 {
		t.Fatalf("exit code = %d, want 1", status.ExitCode)
	}
}

func TestFirecrackerVMSupervisorCapturesLogs(t *testing.T) {
	dir := t.TempDir()
	s := &FirecrackerVMSupervisor{
		BinaryPath: "/bin/sh",
		LogDir:     dir,
	}
	spec := runtimecontract.RuntimeSpec{RuntimeID: "alice"}
	if err := s.Start(context.Background(), spec, []string{"-c", "echo stdout; echo stderr >&2"}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := waitForFirecrackerVM(t, s, "alice", func(status FirecrackerVMStatus) bool {
		return status.State == FirecrackerVMCrashed
	}); err != nil {
		t.Fatal(err)
	}
	status, err := s.Inspect("alice")
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	if status.LogPath != filepath.Join(dir, "alice.log") {
		t.Fatalf("log path = %q", status.LogPath)
	}
	data, err := os.ReadFile(status.LogPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "stdout") || !strings.Contains(text, "stderr") {
		t.Fatalf("log missing output: %q", text)
	}
}

func waitForFirecrackerVM(t *testing.T, s *FirecrackerVMSupervisor, runtimeID string, ok func(FirecrackerVMStatus) bool) error {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last FirecrackerVMStatus
	for time.Now().Before(deadline) {
		status, err := s.Inspect(runtimeID)
		if err == nil {
			last = status
			if ok(status) {
				return nil
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for VM status; last=%#v", last)
}
