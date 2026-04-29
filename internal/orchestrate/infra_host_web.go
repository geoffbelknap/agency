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

	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
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
	switch strings.TrimSpace(inf.RuntimeBackendName) {
	case hostruntimebackend.BackendFirecracker, hostruntimebackend.BackendAppleVFMicroVM:
		return true
	default:
		return false
	}
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
	logDir := filepath.Join(inf.Home, "logs", "infra")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("prepare host web log dir: %w", err)
	}
	runDir := filepath.Join(inf.Home, "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("prepare run dir: %w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "web.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open host web log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(inf.hostWebVite(webDir), "preview", "--host", "127.0.0.1", "--port", inf.webPort())
	cmd.Dir = webDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(),
		"AGENCY_HOME="+inf.Home,
		"BUILD_ID="+inf.BuildID,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start host web: %w", err)
	}
	pid := cmd.Process.Pid
	if err := inf.writeHostInfraPID("web", pid); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		return fmt.Errorf("write host web pid: %w", err)
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
	sourceDir, err := inf.hostInfraSourceDir(filepath.Join("web", "package.json"))
	if err != nil {
		return "", fmt.Errorf("host web source unavailable: %w", err)
	}
	webDir := filepath.Join(sourceDir, "web")
	return webDir, nil
}

func (inf *Infra) ensureHostWebBuild(ctx context.Context, webDir string) error {
	if _, err := os.Stat(filepath.Join(webDir, "dist", "index.html")); err == nil {
		return nil
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

func (inf *Infra) hostWebVite(webDir string) string {
	if vite := strings.TrimSpace(os.Getenv("AGENCY_HOST_WEB_VITE")); vite != "" {
		return vite
	}
	local := filepath.Join(webDir, "node_modules", ".bin", "vite")
	if info, err := os.Stat(local); err == nil && !info.IsDir() {
		return local
	}
	return "vite"
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
		status.ContainerID = fmt.Sprintf("host:%d", pid)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+inf.webPort()+"/health", nil)
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
