package runtimebackend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

func TestFirecrackerVMSupervisorStopCleansPersistedProcess(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("/bin/sh", "-c", "trap '' TERM; sleep 30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})

	s := &FirecrackerVMSupervisor{
		BinaryPath:  "/bin/sh",
		PIDDir:      dir,
		StopTimeout: 50 * time.Millisecond,
	}
	if err := s.writePID("alice", cmd.Process.Pid); err != nil {
		t.Fatalf("writePID returned error: %v", err)
	}
	if err := s.Stop(context.Background(), "alice"); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if _, err := os.Stat(s.pidPath("alice")); !os.IsNotExist(err) {
		t.Fatalf("expected persisted pid to be removed, err=%v", err)
	}
}

func TestFirecrackerVMSupervisorInspectsPersistedProcess(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("/bin/sh", "-c", "sleep 30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})

	s := &FirecrackerVMSupervisor{
		BinaryPath: "/bin/sh",
		LogDir:     filepath.Join(dir, "logs"),
		PIDDir:     filepath.Join(dir, "pids"),
	}
	if err := s.writePID("alice", cmd.Process.Pid); err != nil {
		t.Fatalf("writePID returned error: %v", err)
	}

	restarted := &FirecrackerVMSupervisor{
		BinaryPath: "/bin/sh",
		LogDir:     s.LogDir,
		PIDDir:     s.PIDDir,
	}
	status, err := restarted.Inspect("alice")
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	if status.State != FirecrackerVMRunning || status.PID != cmd.Process.Pid {
		t.Fatalf("status = %#v", status)
	}
	if status.LogPath != filepath.Join(s.LogDir, "alice.log") {
		t.Fatalf("log path = %q", status.LogPath)
	}
}

func TestFirecrackerVMSupervisorStartSkipsPersistedProcess(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	cmd := exec.Command("/bin/sh", "-c", "sleep 30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})

	s := &FirecrackerVMSupervisor{
		BinaryPath: "/bin/sh",
		PIDDir:     filepath.Join(dir, "pids"),
	}
	if err := s.writePID("alice", cmd.Process.Pid); err != nil {
		t.Fatalf("writePID returned error: %v", err)
	}
	if err := s.Start(context.Background(), runtimecontract.RuntimeSpec{RuntimeID: "alice"}, []string{"-c", "echo duplicate > " + marker}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("duplicate process marker exists, err=%v", err)
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

func TestRemoveStaleFirecrackerSocketsRemovesAPISocketArg(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "firecracker-api.sock")
	if err := os.WriteFile(socketPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale socket marker: %v", err)
	}
	if err := removeStaleFirecrackerSockets([]string{"--api-sock", socketPath}); err != nil {
		t.Fatalf("removeStaleFirecrackerSockets returned error: %v", err)
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("expected stale api socket to be removed, err=%v", err)
	}
}

func TestRemoveStaleFirecrackerSocketsRemovesAPISocketEqualsArg(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "firecracker-api.sock")
	if err := os.WriteFile(socketPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale socket marker: %v", err)
	}
	if err := removeStaleFirecrackerSockets([]string{"--api-sock=" + socketPath}); err != nil {
		t.Fatalf("removeStaleFirecrackerSockets returned error: %v", err)
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("expected stale api socket to be removed, err=%v", err)
	}
}

func TestRemoveStaleFirecrackerSocketsRemovesVsockUDSPath(t *testing.T) {
	dir := t.TempDir()
	apiSocketPath := filepath.Join(dir, "firecracker-api.sock")
	vsockPath := filepath.Join(dir, "vsock.sock")
	configPath := filepath.Join(dir, "vm.json")
	for _, path := range []string{apiSocketPath, vsockPath} {
		if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
			t.Fatalf("write stale socket marker %s: %v", path, err)
		}
	}
	config := fmt.Sprintf(`{"vsock":{"uds_path":%q}}`, vsockPath)
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	args := []string{"--api-sock", apiSocketPath, "--config-file", configPath}
	if err := removeStaleFirecrackerSockets(args); err != nil {
		t.Fatalf("removeStaleFirecrackerSockets returned error: %v", err)
	}
	for _, path := range []string{apiSocketPath, vsockPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected stale socket %s to be removed, err=%v", path, err)
		}
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
