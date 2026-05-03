package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

func (inf *Infra) hostWebEnabled() bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv("AGENCY_HOST_INFRA_WEB")))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	if hostServiceRuntimeBackend(inf.RuntimeBackendName) {
		return true
	}
	return false
}

func (inf *Infra) ensureHostWeb(ctx context.Context) error {
	if pid, ok := inf.hostInfraPID("web"); ok {
		if processAlive(pid) && inf.hostWebHealthy(ctx) {
			return nil
		}
		if err := inf.stopHostWeb(ctx); err != nil {
			inf.log.Warn("stop stale host web", "err", err)
		}
	}
	if inf.hostWebHealthy(ctx) {
		return fmt.Errorf("host web port %s is already serving without a managed host web process", inf.webPort())
	}

	webDir, err := inf.hostWebDir()
	if err != nil {
		return err
	}
	if err := inf.ensureHostWebBuild(ctx, webDir); err != nil {
		return err
	}
	distDir := filepath.Join(webDir, "dist")
	logDir := filepath.Join(inf.Home, "logs", "infra")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("prepare host web log dir: %w", err)
	}
	runDir := filepath.Join(inf.Home, "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("prepare run dir: %w", err)
	}
	logPath := filepath.Join(logDir, "web.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open host web log: %w", err)
	}

	cmd := exec.Command(inf.hostWebCommand(), "host-web-serve", "--dist-dir", distDir, "--host", "127.0.0.1", "--port", inf.webPort())
	cmd.Dir = webDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = inf.hostWebPreviewEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start host web: %w", err)
	}
	pid := cmd.Process.Pid
	if err := logFile.Close(); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		return fmt.Errorf("close host web log: %w", err)
	}
	if err := inf.writeHostInfraPID("web", pid); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		return fmt.Errorf("write host web pid: %w", err)
	}
	if err := inf.writeHostInfraMetadata("web", pid, cmd.Args, logPath, "http://127.0.0.1:"+inf.webPort()+"/health"); err != nil {
		inf.log.Warn("write host web metadata", "err", err)
	}
	go func() {
		if err := cmd.Wait(); err != nil && inf.log != nil {
			inf.log.Warn("host web exited", "pid", pid, "err", err)
		}
	}()
	if err := inf.waitHostWebHealthy(ctx, pid, 30*time.Second); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		return err
	}
	return nil
}

func (inf *Infra) hostWebDir() (string, error) {
	sourceDir, err := inf.hostInfraSourceDir(filepath.Join("web", "dist", "index.html"))
	if err == nil {
		return filepath.Join(sourceDir, "web"), nil
	}
	sourceDir, sourceErr := inf.hostInfraSourceDir(filepath.Join("web", "package.json"))
	if sourceErr == nil {
		return filepath.Join(sourceDir, "web"), nil
	}
	return "", fmt.Errorf("host web source unavailable: %w", err)
}

func (inf *Infra) ensureHostWebBuild(ctx context.Context, webDir string) error {
	if _, err := os.Stat(filepath.Join(webDir, "dist", "index.html")); err == nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(webDir, "package.json")); err != nil {
		return fmt.Errorf("host web dist missing at %s and source package unavailable", filepath.Join(webDir, "dist", "index.html"))
	}
	cmd := exec.CommandContext(ctx, inf.hostWebNPM(), "run", "build")
	cmd.Dir = webDir
	cmd.Env = append(os.Environ(),
		"AGENCY_HOME="+inf.Home,
		"BUILD_ID="+inf.BuildID,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build host web: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (inf *Infra) hostWebNPM() string {
	if npm := strings.TrimSpace(os.Getenv("AGENCY_HOST_WEB_NPM")); npm != "" {
		return npm
	}
	return "npm"
}

func (inf *Infra) hostWebCommand() string {
	if command := strings.TrimSpace(os.Getenv("AGENCY_HOST_WEB_COMMAND")); command != "" {
		return command
	}
	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		return exe
	}
	return "agency"
}

func (inf *Infra) hostWebPreviewEnv() []string {
	return append(os.Environ(),
		"AGENCY_HOME="+inf.Home,
		"BUILD_ID="+inf.BuildID,
		"VITE_DISABLE_HTTPS=1",
	)
}

func (inf *Infra) stopHostWeb(ctx context.Context) error {
	pid, err := inf.readHostInfraPID("web")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if pid <= 0 {
		inf.removeHostInfraPID("web")
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
	}
	deadline := time.Now().Add(time.Duration(stopTimeoutFor("web")) * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			inf.removeHostInfraPID("web")
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
	inf.removeHostInfraPID("web")
	return nil
}

func (inf *Infra) hostWebStatus(ctx context.Context) runtimehost.InfraComponent {
	status := runtimehost.InfraComponent{Name: "web", State: "missing", Health: "none"}
	pid, ok := inf.hostInfraPID("web")
	if ok && processAlive(pid) {
		status.State = "running"
		status.ComponentID = fmt.Sprintf("host:%d", pid)
		status.ContainerID = status.ComponentID
		if inf.hostWebHealthy(ctx) {
			status.Health = "healthy"
		}
	} else if ok {
		status.State = "stopped"
	}
	status.BuildID = inf.BuildID
	return status
}

func (inf *Infra) waitHostWebHealthy(ctx context.Context, pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return fmt.Errorf("host web exited before becoming healthy")
		}
		if inf.hostWebHealthy(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("host web not healthy within %v", timeout)
}

func (inf *Infra) hostWebHealthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+inf.webPort()+"/", nil)
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
