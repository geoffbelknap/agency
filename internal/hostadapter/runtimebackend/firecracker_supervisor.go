package runtimebackend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/geoffbelknap/agency/internal/pkg/pathsafety"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

const (
	FirecrackerVMStarting = "starting"
	FirecrackerVMRunning  = "running"
	FirecrackerVMStopping = "stopping"
	FirecrackerVMStopped  = "stopped"
	FirecrackerVMCrashed  = "crashed"
)

type FirecrackerVMSupervisor struct {
	BinaryPath     string
	LogDir         string
	PIDDir         string
	StopTimeout    time.Duration
	RestartBackoff time.Duration

	mu    sync.Mutex
	tasks map[string]*firecrackerVMTask
}

type FirecrackerVMStatus struct {
	RuntimeID string
	State     string
	PID       int
	ExitCode  int
	Restarts  int
	Crashes   int
	StartedAt time.Time
	StoppedAt time.Time
	Duration  time.Duration
	LastError string
	LogPath   string
}

type firecrackerVMTask struct {
	runtimeID     string
	args          []string
	restartPolicy string
	supervisor    *FirecrackerVMSupervisor

	cmd           *exec.Cmd
	logFile       *os.File
	logPath       string
	done          chan struct{}
	state         string
	pid           int
	exitCode      int
	restarts      int
	crashes       int
	startedAt     time.Time
	stoppedAt     time.Time
	lastError     string
	stopRequested bool
}

func (s *FirecrackerVMSupervisor) Start(ctx context.Context, spec runtimecontract.RuntimeSpec, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	runtimeID, err := pathsafety.Segment("firecracker runtime id", spec.RuntimeID)
	if err != nil {
		return fmt.Errorf("firecracker supervisor: %w", err)
	}
	spec.RuntimeID = runtimeID
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tasks == nil {
		s.tasks = make(map[string]*firecrackerVMTask)
	}
	if task := s.tasks[spec.RuntimeID]; task != nil && (task.state == FirecrackerVMStarting || task.state == FirecrackerVMRunning) {
		return nil
	}
	if status, ok := s.persistedStatus(spec.RuntimeID); ok && status.State == FirecrackerVMRunning {
		return nil
	}
	task := &firecrackerVMTask{
		runtimeID:     spec.RuntimeID,
		args:          append([]string(nil), args...),
		restartPolicy: spec.Lifecycle.RestartPolicy,
		supervisor:    s,
		exitCode:      -1,
	}
	s.tasks[spec.RuntimeID] = task
	return task.startLocked()
}

func (s *FirecrackerVMSupervisor) Stop(ctx context.Context, runtimeID string) error {
	s.mu.Lock()
	task := s.taskLocked(runtimeID)
	if task == nil {
		s.mu.Unlock()
		return s.stopPersisted(ctx, runtimeID)
	}
	task.stopRequested = true
	task.state = FirecrackerVMStopping
	cmd := task.cmd
	done := task.done
	s.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		s.markStopped(runtimeID)
		return nil
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-done:
		return nil
	case <-time.After(s.stopTimeout()):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return ctx.Err()
	}
	select {
	case <-done:
	case <-time.After(s.stopTimeout()):
	}
	return nil
}

func (s *FirecrackerVMSupervisor) Inspect(runtimeID string) (FirecrackerVMStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.taskLocked(runtimeID)
	if task == nil {
		if status, ok := s.persistedStatus(runtimeID); ok {
			return status, nil
		}
		return FirecrackerVMStatus{}, fmt.Errorf("firecracker supervisor: runtime %q is not tracked", runtimeID)
	}
	return task.statusLocked(), nil
}

func (s *FirecrackerVMSupervisor) taskLocked(runtimeID string) *firecrackerVMTask {
	if s.tasks == nil {
		return nil
	}
	return s.tasks[runtimeID]
}

func (s *FirecrackerVMSupervisor) markStopped(runtimeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task := s.taskLocked(runtimeID); task != nil {
		task.state = FirecrackerVMStopped
		task.stoppedAt = time.Now().UTC()
	}
}

func (s *FirecrackerVMSupervisor) binaryPath() string {
	if s.BinaryPath != "" {
		return s.BinaryPath
	}
	return "firecracker"
}

func (s *FirecrackerVMSupervisor) stopTimeout() time.Duration {
	if s.StopTimeout > 0 {
		return s.StopTimeout
	}
	return 10 * time.Second
}

func (s *FirecrackerVMSupervisor) restartBackoff() time.Duration {
	if s.RestartBackoff > 0 {
		return s.RestartBackoff
	}
	return time.Second
}

func (t *firecrackerVMTask) startLocked() error {
	cmd := exec.Command(t.supervisor.binaryPath(), t.args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	logFile, logPath, err := t.supervisor.openLog(t.runtimeID)
	if err != nil {
		t.state = FirecrackerVMCrashed
		t.lastError = err.Error()
		return err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	t.cmd = cmd
	t.logFile = logFile
	t.logPath = logPath
	t.done = make(chan struct{})
	t.state = FirecrackerVMStarting
	t.stopRequested = false
	t.startedAt = time.Now().UTC()
	t.stoppedAt = time.Time{}
	t.lastError = ""
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		t.state = FirecrackerVMCrashed
		t.lastError = err.Error()
		close(t.done)
		return err
	}
	t.pid = cmd.Process.Pid
	if err := t.supervisor.writePID(t.runtimeID, t.pid); err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = logFile.Close()
		t.state = FirecrackerVMCrashed
		t.lastError = err.Error()
		close(t.done)
		return err
	}
	t.state = FirecrackerVMRunning
	go t.watch(cmd, t.done)
	return nil
}

func (t *firecrackerVMTask) watch(cmd *exec.Cmd, done chan struct{}) {
	err := cmd.Wait()
	if t.logFile != nil {
		_ = t.logFile.Close()
	}
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	t.supervisor.mu.Lock()
	defer t.supervisor.mu.Unlock()
	if t.cmd != cmd {
		close(done)
		return
	}
	now := time.Now().UTC()
	t.exitCode = exitCode
	t.stoppedAt = now
	_ = t.supervisor.removePID(t.runtimeID)
	if err != nil {
		t.lastError = err.Error()
	}
	cleanStop := t.stopRequested
	unexpected := !cleanStop || exitCode != 0
	if unexpected && !cleanStop {
		t.crashes++
	}
	if unexpected && !cleanStop && shouldRestartFirecrackerVM(t.restartPolicy) {
		t.state = FirecrackerVMCrashed
		close(done)
		t.scheduleRestartLocked()
		return
	}
	if unexpected && !cleanStop {
		t.state = FirecrackerVMCrashed
	} else {
		t.state = FirecrackerVMStopped
	}
	close(done)
}

func (t *firecrackerVMTask) scheduleRestartLocked() {
	go func() {
		time.Sleep(t.supervisor.restartBackoff())
		t.supervisor.mu.Lock()
		defer t.supervisor.mu.Unlock()
		if current := t.supervisor.taskLocked(t.runtimeID); current != t || t.stopRequested {
			return
		}
		t.restarts++
		if err := t.startLocked(); err != nil {
			t.lastError = err.Error()
		}
	}()
}

func (t *firecrackerVMTask) statusLocked() FirecrackerVMStatus {
	durationEnd := t.stoppedAt
	if durationEnd.IsZero() {
		durationEnd = time.Now().UTC()
	}
	return FirecrackerVMStatus{
		RuntimeID: t.runtimeID,
		State:     t.state,
		PID:       t.pid,
		ExitCode:  t.exitCode,
		Restarts:  t.restarts,
		Crashes:   t.crashes,
		StartedAt: t.startedAt,
		StoppedAt: t.stoppedAt,
		Duration:  durationEnd.Sub(t.startedAt),
		LastError: t.lastError,
		LogPath:   t.logPath,
	}
}

func (s *FirecrackerVMSupervisor) openLog(runtimeID string) (*os.File, string, error) {
	dir := strings.TrimSpace(s.LogDir)
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "agency-firecracker", "logs")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create firecracker log dir: %w", err)
	}
	path, err := pathsafety.Join(dir, runtimeID+".log")
	if err != nil {
		return nil, "", err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, "", fmt.Errorf("open firecracker log: %w", err)
	}
	return file, path, nil
}

func (s *FirecrackerVMSupervisor) pidDir() string {
	if strings.TrimSpace(s.PIDDir) != "" {
		return s.PIDDir
	}
	if strings.TrimSpace(s.LogDir) != "" {
		return filepath.Join(filepath.Dir(s.LogDir), "pids")
	}
	return filepath.Join(os.TempDir(), "agency-firecracker", "pids")
}

func (s *FirecrackerVMSupervisor) pidPath(runtimeID string) string {
	path, err := pathsafety.Join(s.pidDir(), runtimeID+".pid")
	if err != nil {
		return filepath.Join(s.pidDir(), "invalid.pid")
	}
	return path
}

func (s *FirecrackerVMSupervisor) writePID(runtimeID string, pid int) error {
	if err := os.MkdirAll(s.pidDir(), 0o755); err != nil {
		return fmt.Errorf("create firecracker pid dir: %w", err)
	}
	if err := os.WriteFile(s.pidPath(runtimeID), []byte(fmt.Sprintf("%d\n", pid)), 0o644); err != nil {
		return fmt.Errorf("write firecracker pid: %w", err)
	}
	return nil
}

func (s *FirecrackerVMSupervisor) removePID(runtimeID string) error {
	err := os.Remove(s.pidPath(runtimeID))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *FirecrackerVMSupervisor) persistedStatus(runtimeID string) (FirecrackerVMStatus, bool) {
	pid, startedAt, ok := s.readPersistedPID(runtimeID)
	if !ok {
		return FirecrackerVMStatus{}, false
	}
	if !s.pidLooksLikeFirecracker(pid) {
		_ = s.removePID(runtimeID)
		return FirecrackerVMStatus{}, false
	}
	now := time.Now().UTC()
	return FirecrackerVMStatus{
		RuntimeID: runtimeID,
		State:     FirecrackerVMRunning,
		PID:       pid,
		ExitCode:  -1,
		StartedAt: startedAt,
		Duration:  now.Sub(startedAt),
		LogPath:   s.logPath(runtimeID),
	}, true
}

func (s *FirecrackerVMSupervisor) readPersistedPID(runtimeID string) (int, time.Time, bool) {
	path := s.pidPath(runtimeID)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, time.Time{}, false
		}
		return 0, time.Time{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, time.Time{}, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		_ = s.removePID(runtimeID)
		return 0, time.Time{}, false
	}
	if !processExists(pid) {
		_ = s.removePID(runtimeID)
		return 0, time.Time{}, false
	}
	return pid, info.ModTime().UTC(), true
}

func (s *FirecrackerVMSupervisor) stopPersisted(ctx context.Context, runtimeID string) error {
	pid, _, ok := s.readPersistedPID(runtimeID)
	if !ok {
		return nil
	}
	if !s.pidLooksLikeFirecracker(pid) {
		_ = s.removePID(runtimeID)
		return nil
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.After(s.stopTimeout())
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			return ctx.Err()
		case <-deadline:
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			_ = s.removePID(runtimeID)
			return nil
		case <-tick.C:
			if !processExists(pid) {
				_ = s.removePID(runtimeID)
				return nil
			}
		}
	}
}

func (s *FirecrackerVMSupervisor) logPath(runtimeID string) string {
	dir := strings.TrimSpace(s.LogDir)
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "agency-firecracker", "logs")
	}
	path, err := pathsafety.Join(dir, runtimeID+".log")
	if err != nil {
		return filepath.Join(dir, "invalid.log")
	}
	return path
}

func (s *FirecrackerVMSupervisor) pidLooksLikeFirecracker(pid int) bool {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return processExists(pid)
	}
	cmdline := string(data)
	binary := filepath.Base(s.binaryPath())
	return strings.Contains(cmdline, binary) || strings.Contains(cmdline, "firecracker")
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func shouldRestartFirecrackerVM(policy string) bool {
	switch policy {
	case "always", "on-failure", "unless-stopped":
		return true
	default:
		return false
	}
}
