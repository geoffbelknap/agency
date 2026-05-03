package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/geoffbelknap/agency/internal/pkg/pathsafety"
)

const (
	HostEnforcerStateStarting = "starting"
	HostEnforcerStateRunning  = "running"
	HostEnforcerStateStopping = "stopping"
	HostEnforcerStateStopped  = "stopped"
	HostEnforcerStateCrashed  = "crashed"
)

type HostEnforcerSupervisor struct {
	BinaryPath  string
	StateDir    string
	StopTimeout time.Duration
	HTTPClient  *http.Client

	mu        sync.Mutex
	processes map[string]*hostEnforcerProcess
}

type HostEnforcerStatus struct {
	AgentName string
	State     string
	PID       int
	ExitCode  int
	StartedAt time.Time
	StoppedAt time.Time
	LastError string
	LogPath   string
}

type hostEnforcerProcess struct {
	spec      EnforcerLaunchSpec
	cmd       *exec.Cmd
	state     string
	pid       int
	exitCode  int
	startedAt time.Time
	stoppedAt time.Time
	lastError string
	logPath   string
	logFile   *os.File
	cleanStop bool
	done      chan struct{}
}

type persistedHostEnforcerProcess struct {
	Spec      EnforcerLaunchSpec `json:"spec"`
	PID       int                `json:"pid"`
	LogPath   string             `json:"log_path,omitempty"`
	StartedAt time.Time          `json:"started_at"`
}

func (s *HostEnforcerSupervisor) Start(ctx context.Context, spec EnforcerLaunchSpec, serviceURLs map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	agentName, err := pathsafety.Segment("agent name", spec.AgentName)
	if err != nil {
		return fmt.Errorf("host enforcer: %w", err)
	}
	spec.AgentName = agentName
	s.mu.Lock()
	if s.processes != nil {
		if existing := s.processes[spec.AgentName]; existing != nil && existing.state == HostEnforcerStateRunning {
			if existing.spec.ProxyHostPort == spec.ProxyHostPort && existing.spec.ConstraintHostPort == spec.ConstraintHostPort {
				s.mu.Unlock()
				return nil
			}
			s.mu.Unlock()
			if err := s.Stop(ctx, spec.AgentName); err != nil {
				return err
			}
			s.mu.Lock()
		}
	}
	s.mu.Unlock()

	binary := s.BinaryPath
	if binary == "" {
		binary = "enforcer"
	}
	logPath := s.logPath(spec.AgentName)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("create host enforcer log dir: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open host enforcer log %s: %w", logPath, err)
	}
	env := spec.HostProcessEnv(serviceURLs)
	cmd := exec.Command(binary)
	cmd.Env = processEnv(env)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start host enforcer %s: %w", spec.AgentName, err)
	}
	proc := &hostEnforcerProcess{
		spec:      spec,
		cmd:       cmd,
		state:     HostEnforcerStateRunning,
		pid:       cmd.Process.Pid,
		exitCode:  -1,
		startedAt: time.Now(),
		logPath:   logPath,
		logFile:   logFile,
		done:      make(chan struct{}),
	}

	s.mu.Lock()
	if s.processes == nil {
		s.processes = make(map[string]*hostEnforcerProcess)
	}
	if existing := s.processes[spec.AgentName]; existing != nil && existing.state == HostEnforcerStateRunning {
		s.mu.Unlock()
		_ = killProcessGroup(cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
		_ = logFile.Close()
		return fmt.Errorf("host enforcer %s already running", spec.AgentName)
	}
	s.processes[spec.AgentName] = proc
	s.mu.Unlock()

	if err := s.persist(proc); err != nil {
		_ = killProcessGroup(cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
		_ = logFile.Close()
		s.mu.Lock()
		if current := s.processes[spec.AgentName]; current == proc {
			delete(s.processes, spec.AgentName)
		}
		s.mu.Unlock()
		return err
	}
	go s.wait(spec.AgentName, proc)
	return nil
}

func (s *HostEnforcerSupervisor) Stop(ctx context.Context, agentName string) error {
	proc, ok := s.process(agentName)
	if !ok {
		return nil
	}

	s.mu.Lock()
	if proc.state == HostEnforcerStateStopped || proc.state == HostEnforcerStateCrashed {
		s.mu.Unlock()
		_ = s.removeState(agentName)
		return nil
	}
	proc.cleanStop = true
	proc.state = HostEnforcerStateStopping
	cmd := proc.cmd
	s.mu.Unlock()

	_ = killProcessGroup(proc.pid, syscall.SIGTERM)
	timeout := s.StopTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	if cmd == nil {
		return s.waitForRestoredStop(ctx, proc, timer.C)
	}
	select {
	case <-proc.done:
		_ = s.removeState(agentName)
		return nil
	case <-ctx.Done():
		_ = killProcessGroup(proc.pid, syscall.SIGKILL)
		<-proc.done
		_ = s.removeState(agentName)
		return ctx.Err()
	case <-timer.C:
		_ = killProcessGroup(proc.pid, syscall.SIGKILL)
		<-proc.done
		_ = s.removeState(agentName)
		return nil
	}
}

func (s *HostEnforcerSupervisor) Signal(agentName string, sig syscall.Signal) error {
	proc, ok := s.process(agentName)
	if !ok {
		return fmt.Errorf("host enforcer %s not found", agentName)
	}
	s.mu.Lock()
	running := proc.state == HostEnforcerStateRunning
	pid := proc.pid
	s.mu.Unlock()
	if !running {
		return fmt.Errorf("host enforcer %s is not running", agentName)
	}
	return killProcessGroup(pid, sig)
}

func (s *HostEnforcerSupervisor) Inspect(agentName string) (HostEnforcerStatus, error) {
	proc, ok := s.process(agentName)
	if !ok {
		return HostEnforcerStatus{}, fmt.Errorf("host enforcer %s not found", agentName)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return proc.status(), nil
}

func (s *HostEnforcerSupervisor) HealthCheck(ctx context.Context, agentName string, timeout time.Duration) error {
	proc, ok := s.process(agentName)
	if !ok {
		return fmt.Errorf("host enforcer %s not found", agentName)
	}
	client := s.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	url := "http://127.0.0.1:" + proc.spec.ProxyHostPort + "/health"
	var lastErr error
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("health returned %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	if lastErr != nil {
		return fmt.Errorf("host enforcer %s not healthy%s: %w", agentName, s.logFailureHint(proc), lastErr)
	}
	return fmt.Errorf("host enforcer %s not healthy%s", agentName, s.logFailureHint(proc))
}

func (s *HostEnforcerSupervisor) process(agentName string) (*hostEnforcerProcess, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.processes == nil {
		s.processes = make(map[string]*hostEnforcerProcess)
	}
	if proc, ok := s.processes[agentName]; ok {
		s.refreshRestoredProcessLocked(agentName, proc)
		return proc, true
	}
	proc, err := s.restoreLocked(agentName)
	if err == nil {
		s.processes[agentName] = proc
		return proc, true
	}
	proc, ok := s.processes[agentName]
	return proc, ok
}

func (s *HostEnforcerSupervisor) wait(agentName string, proc *hostEnforcerProcess) {
	err := proc.cmd.Wait()
	status := proc.cmd.ProcessState
	if proc.logFile != nil {
		_ = proc.logFile.Close()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	proc.stoppedAt = time.Now()
	if status != nil {
		proc.exitCode = status.ExitCode()
	}
	if err != nil {
		proc.lastError = err.Error()
	}
	if proc.cleanStop || proc.exitCode == 0 {
		proc.state = HostEnforcerStateStopped
	} else {
		proc.state = HostEnforcerStateCrashed
	}
	if current := s.processes[agentName]; current == proc {
		s.processes[agentName] = proc
	}
	_ = s.removeState(agentName)
	close(proc.done)
}

func (p *hostEnforcerProcess) status() HostEnforcerStatus {
	return HostEnforcerStatus{
		AgentName: p.spec.AgentName,
		State:     p.state,
		PID:       p.pid,
		ExitCode:  p.exitCode,
		StartedAt: p.startedAt,
		StoppedAt: p.stoppedAt,
		LastError: p.lastError,
		LogPath:   p.logPath,
	}
}

func processEnv(env map[string]string) []string {
	out := os.Environ()
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

func killProcessGroup(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func (s *HostEnforcerSupervisor) waitForRestoredStop(ctx context.Context, proc *hostEnforcerProcess, timeout <-chan time.Time) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !processAlive(proc.pid) {
			s.mu.Lock()
			proc.state = HostEnforcerStateStopped
			proc.stoppedAt = time.Now()
			s.mu.Unlock()
			_ = s.removeState(proc.spec.AgentName)
			return nil
		}
		select {
		case <-ctx.Done():
			_ = killProcessGroup(proc.pid, syscall.SIGKILL)
			_ = s.removeState(proc.spec.AgentName)
			return ctx.Err()
		case <-timeout:
			_ = killProcessGroup(proc.pid, syscall.SIGKILL)
		case <-ticker.C:
		}
	}
}

func (s *HostEnforcerSupervisor) persist(proc *hostEnforcerProcess) error {
	state := persistedHostEnforcerProcess{
		Spec:      proc.spec,
		PID:       proc.pid,
		LogPath:   proc.logPath,
		StartedAt: proc.startedAt,
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal host enforcer state: %w", err)
	}
	if err := os.MkdirAll(s.stateDir(), 0o755); err != nil {
		return fmt.Errorf("create host enforcer state dir: %w", err)
	}
	if err := os.WriteFile(s.statePath(proc.spec.AgentName), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write host enforcer state: %w", err)
	}
	return nil
}

func (s *HostEnforcerSupervisor) restoreLocked(agentName string) (*hostEnforcerProcess, error) {
	data, err := os.ReadFile(s.statePath(agentName))
	if err != nil {
		return nil, err
	}
	var state persistedHostEnforcerProcess
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.Spec.AgentName == "" {
		state.Spec.AgentName = agentName
	}
	proc := &hostEnforcerProcess{
		spec:      state.Spec,
		state:     HostEnforcerStateRunning,
		pid:       state.PID,
		exitCode:  -1,
		startedAt: state.StartedAt,
		logPath:   state.LogPath,
		done:      make(chan struct{}),
	}
	if proc.logPath == "" {
		proc.logPath = s.logPath(agentName)
	}
	if !processAlive(proc.pid) {
		proc.state = HostEnforcerStateCrashed
		proc.stoppedAt = time.Now()
		proc.lastError = "host enforcer process is not running"
		_ = s.removeState(agentName)
	}
	return proc, nil
}

func (s *HostEnforcerSupervisor) refreshRestoredProcessLocked(agentName string, proc *hostEnforcerProcess) {
	if proc.cmd != nil || proc.state != HostEnforcerStateRunning {
		return
	}
	if processAlive(proc.pid) {
		return
	}
	proc.state = HostEnforcerStateCrashed
	proc.stoppedAt = time.Now()
	proc.lastError = "host enforcer process is not running"
	_ = s.removeState(agentName)
}

func (s *HostEnforcerSupervisor) removeState(agentName string) error {
	if err := os.Remove(s.statePath(agentName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *HostEnforcerSupervisor) statePath(agentName string) string {
	path, err := pathsafety.Join(s.stateDir(), agentName+".json")
	if err != nil {
		return filepath.Join(s.stateDir(), "invalid.json")
	}
	return path
}

func (s *HostEnforcerSupervisor) logPath(agentName string) string {
	path, err := pathsafety.Join(filepath.Join(s.stateDir(), "logs"), agentName+".log")
	if err != nil {
		return filepath.Join(s.stateDir(), "logs", "invalid.log")
	}
	return path
}

func (s *HostEnforcerSupervisor) stateDir() string {
	if s.StateDir != "" {
		return s.StateDir
	}
	return filepath.Join(os.TempDir(), "agency-host-enforcers")
}

func (s *HostEnforcerSupervisor) logFailureHint(proc *hostEnforcerProcess) string {
	if proc == nil || proc.logPath == "" {
		return ""
	}
	tail := strings.TrimSpace(readLogTail(proc.logPath, 4096))
	if tail == "" {
		return " (log " + proc.logPath + " is empty)"
	}
	return " (log " + proc.logPath + ": " + tail + ")"
}

func readLogTail(path string, maxBytes int64) string {
	if maxBytes <= 0 {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	offset := info.Size() - maxBytes
	if offset < 0 {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return ""
	}
	return string(data)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
