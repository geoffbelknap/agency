package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

func (inf *Infra) hostCommsEnabled() bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv("AGENCY_HOST_INFRA_COMMS")))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	switch strings.TrimSpace(inf.RuntimeBackendName) {
	case hostruntimebackend.BackendFirecracker, hostruntimebackend.BackendAppleVFMicroVM:
		return true
	default:
		return false
	}
}

func (inf *Infra) ensureHostComms(ctx context.Context) error {
	commsData := filepath.Join(inf.Home, "infrastructure", "comms", "data")
	agentsDir := filepath.Join(inf.Home, "agents")
	if err := prepareCommsDataDir(commsData); err != nil {
		return fmt.Errorf("prepare comms data: %w", err)
	}
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return fmt.Errorf("prepare agents dir: %w", err)
	}
	if pid, ok := inf.hostCommsPID(); ok {
		if processAlive(pid) && inf.hostCommsHealthy(ctx) {
			return inf.ensureSystemChannels(ctx)
		}
		if err := inf.stopHostComms(ctx); err != nil {
			inf.log.Warn("stop stale host comms", "err", err)
		}
	}
	if inf.hostCommsHealthy(ctx) {
		return fmt.Errorf("host comms port %s is already serving without a managed host comms process; refresh gateway-proxy or stop the legacy comms bridge", inf.gatewayProxyPort("8202"))
	}

	sourceDir := strings.TrimSpace(inf.SourceDir)
	if sourceDir == "" {
		if wd, err := os.Getwd(); err == nil {
			sourceDir = wd
		}
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "images", "comms", "server.py")); err != nil {
		return fmt.Errorf("host comms source unavailable under %s: %w", sourceDir, err)
	}

	runDir := filepath.Join(inf.Home, "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("prepare run dir: %w", err)
	}
	logDir := filepath.Join(inf.Home, "logs", "infra")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("prepare host comms log dir: %w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "comms.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open host comms log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(inf.hostCommsPython(sourceDir), "-u", "-m", "images.comms.server",
		"--port", inf.gatewayProxyPort("8202"),
		"--data-dir", commsData,
		"--agents-dir", agentsDir,
	)
	cmd.Dir = sourceDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(),
		"PYTHONPATH="+sourceDir,
		"AGENCY_COMPONENT=comms",
		"AGENCY_CALLER=comms",
		"BUILD_ID="+inf.BuildID,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start host comms: %w", err)
	}
	pid := cmd.Process.Pid
	if err := os.WriteFile(inf.hostCommsPIDPath(), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		return fmt.Errorf("write host comms pid: %w", err)
	}
	go func() {
		if err := cmd.Wait(); err != nil && inf.log != nil {
			inf.log.Warn("host comms exited", "pid", pid, "err", err)
		}
	}()
	if err := inf.waitHostCommsHealthy(ctx, pid, 30*time.Second); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		return err
	}
	return inf.ensureSystemChannels(ctx)
}

func (inf *Infra) stopHostComms(ctx context.Context) error {
	pid, err := inf.readHostCommsPID()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if pid <= 0 {
		_ = os.Remove(inf.hostCommsPIDPath())
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
	}
	deadline := time.Now().Add(time.Duration(stopTimeoutFor("comms")) * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			_ = os.Remove(inf.hostCommsPIDPath())
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
	_ = os.Remove(inf.hostCommsPIDPath())
	return nil
}

func (inf *Infra) hostCommsStatus(ctx context.Context) runtimehost.InfraComponent {
	status := runtimehost.InfraComponent{Name: "comms", State: "missing", Health: "none"}
	pid, ok := inf.hostCommsPID()
	if ok && processAlive(pid) {
		status.State = "running"
		status.ContainerID = fmt.Sprintf("host:%d", pid)
		if inf.hostCommsHealthy(ctx) {
			status.Health = "healthy"
		}
	} else if ok {
		status.State = "stopped"
	}
	status.BuildID = inf.BuildID
	return status
}

func (inf *Infra) HostInfraStatuses(ctx context.Context) []runtimehost.InfraComponent {
	if !inf.hostCommsEnabled() {
		return nil
	}
	return []runtimehost.InfraComponent{inf.hostCommsStatus(ctx)}
}

func (inf *Infra) hostCommsPIDPath() string {
	return filepath.Join(inf.Home, "run", "comms.pid")
}

func (inf *Infra) hostCommsPython(sourceDir string) string {
	if python := strings.TrimSpace(os.Getenv("AGENCY_HOST_COMMS_PYTHON")); python != "" {
		return python
	}
	venvPython := filepath.Join(sourceDir, ".venv", "bin", "python")
	if info, err := os.Stat(venvPython); err == nil && !info.IsDir() {
		return venvPython
	}
	return "python3"
}

func (inf *Infra) readHostCommsPID() (int, error) {
	data, err := os.ReadFile(inf.hostCommsPIDPath())
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func (inf *Infra) hostCommsPID() (int, bool) {
	pid, err := inf.readHostCommsPID()
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func (inf *Infra) waitHostCommsHealthy(ctx context.Context, pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return fmt.Errorf("host comms exited before becoming healthy")
		}
		if inf.hostCommsHealthy(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("host comms not healthy within %v", timeout)
}

func (inf *Infra) hostCommsHealthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+inf.gatewayProxyPort("8202")+"/health", nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
