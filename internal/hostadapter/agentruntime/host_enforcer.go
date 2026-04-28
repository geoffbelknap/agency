package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
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
	cleanStop bool
	done      chan struct{}
}

func (s *HostEnforcerSupervisor) Start(ctx context.Context, spec EnforcerLaunchSpec, serviceURLs map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if spec.AgentName == "" {
		return errors.New("host enforcer: missing agent name")
	}
	binary := s.BinaryPath
	if binary == "" {
		binary = "enforcer"
	}
	env := spec.HostProcessEnv(serviceURLs)
	cmd := exec.Command(binary)
	cmd.Env = processEnv(env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start host enforcer %s: %w", spec.AgentName, err)
	}
	proc := &hostEnforcerProcess{
		spec:      spec,
		cmd:       cmd,
		state:     HostEnforcerStateRunning,
		pid:       cmd.Process.Pid,
		exitCode:  -1,
		startedAt: time.Now(),
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
		return fmt.Errorf("host enforcer %s already running", spec.AgentName)
	}
	s.processes[spec.AgentName] = proc
	s.mu.Unlock()

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
		return nil
	}
	proc.cleanStop = true
	proc.state = HostEnforcerStateStopping
	s.mu.Unlock()

	_ = killProcessGroup(proc.pid, syscall.SIGTERM)
	timeout := s.StopTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-proc.done:
		return nil
	case <-ctx.Done():
		_ = killProcessGroup(proc.pid, syscall.SIGKILL)
		<-proc.done
		return ctx.Err()
	case <-timer.C:
		_ = killProcessGroup(proc.pid, syscall.SIGKILL)
		<-proc.done
		return nil
	}
}

func (s *HostEnforcerSupervisor) Inspect(agentName string) (HostEnforcerStatus, error) {
	proc, ok := s.process(agentName)
	if !ok {
		return HostEnforcerStatus{}, fmt.Errorf("host enforcer %s not found", agentName)
	}
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
		return fmt.Errorf("host enforcer %s not healthy: %w", agentName, lastErr)
	}
	return fmt.Errorf("host enforcer %s not healthy", agentName)
}

func (s *HostEnforcerSupervisor) process(agentName string) (*hostEnforcerProcess, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	proc, ok := s.processes[agentName]
	return proc, ok
}

func (s *HostEnforcerSupervisor) wait(agentName string, proc *hostEnforcerProcess) {
	err := proc.cmd.Wait()
	status := proc.cmd.ProcessState

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
