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

func (inf *Infra) hostKnowledgeEnabled() bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv("AGENCY_HOST_INFRA_KNOWLEDGE")))
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

func (inf *Infra) ensureHostKnowledge(ctx context.Context) error {
	knowledgeDir := filepath.Join(inf.Home, "knowledge", "data")
	if err := prepareKnowledgeDataDir(knowledgeDir); err != nil {
		return fmt.Errorf("prepare knowledge data: %w", err)
	}
	if err := prepareKnowledgeRegistrySnapshot(inf.Home, knowledgeDir); err != nil {
		return fmt.Errorf("prepare knowledge registry snapshot: %w", err)
	}
	if pid, ok := inf.hostInfraPID("knowledge"); ok {
		if processAlive(pid) && inf.hostKnowledgeHealthy(ctx) {
			return nil
		}
		if err := inf.stopHostKnowledge(ctx); err != nil {
			inf.log.Warn("stop stale host knowledge", "err", err)
		}
	}
	if inf.hostKnowledgeHealthy(ctx) {
		return fmt.Errorf("host knowledge port %s is already serving without a managed host knowledge process; refresh gateway-proxy or stop the legacy knowledge bridge", inf.gatewayProxyPort("8204"))
	}

	sourceDir, err := inf.hostInfraSourceDir(filepath.Join("services", "knowledge", "server.py"))
	if err != nil {
		return fmt.Errorf("host knowledge source unavailable: %w", err)
	}

	runDir := filepath.Join(inf.Home, "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("prepare run dir: %w", err)
	}
	logDir := filepath.Join(inf.Home, "logs", "infra")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("prepare host knowledge log dir: %w", err)
	}
	logPath := filepath.Join(logDir, "knowledge.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open host knowledge log: %w", err)
	}

	cmd := exec.Command(inf.hostKnowledgePython(sourceDir), "-u", "-m", "services.knowledge.server",
		"--port", inf.gatewayProxyPort("8204"),
		"--data-dir", knowledgeDir,
	)
	cmd.Dir = sourceDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), inf.hostKnowledgeEnv(sourceDir, knowledgeDir)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start host knowledge: %w", err)
	}
	pid := cmd.Process.Pid
	if err := logFile.Close(); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		return fmt.Errorf("close host knowledge log: %w", err)
	}
	if err := inf.writeHostInfraPID("knowledge", pid); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		return fmt.Errorf("write host knowledge pid: %w", err)
	}
	if err := inf.writeHostInfraMetadata("knowledge", pid, cmd.Args, logPath, "http://127.0.0.1:"+inf.gatewayProxyPort("8204")+"/health"); err != nil {
		inf.log.Warn("write host knowledge metadata", "err", err)
	}
	go func() {
		if err := cmd.Wait(); err != nil && inf.log != nil {
			inf.log.Warn("host knowledge exited", "pid", pid, "err", err)
		}
	}()
	if err := inf.waitHostKnowledgeHealthy(ctx, pid, 90*time.Second); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		return err
	}
	return nil
}

func (inf *Infra) hostKnowledgeEnv(sourceDir, knowledgeDir string) []string {
	env := map[string]string{
		"PYTHONPATH":                 sourceDir,
		"HTTPS_PROXY":                "http://127.0.0.1:" + inf.egressProxyPort(),
		"NO_PROXY":                   "localhost,127.0.0.1",
		"AGENCY_GATEWAY_TOKEN":       inf.GatewayToken,
		"AGENCY_GATEWAY_URL":         "http://" + inf.GatewayAddr,
		"AGENCY_CALLER":              "knowledge",
		"AGENCY_COMPONENT":           "knowledge",
		"BUILD_ID":                   inf.BuildID,
		"AGENCY_HOME":                knowledgeDir,
		"AGENCY_ONTOLOGY_PATH":       filepath.Join(inf.Home, "knowledge", "ontology.yaml"),
		"CLASSIFICATION_CONFIG_PATH": filepath.Join(inf.Home, "knowledge", "classification.yaml"),
	}
	return mapToEnv(env)
}

func (inf *Infra) stopHostKnowledge(ctx context.Context) error {
	pid, err := inf.readHostInfraPID("knowledge")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if pid <= 0 {
		inf.removeHostInfraPID("knowledge")
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
	}
	deadline := time.Now().Add(time.Duration(stopTimeoutFor("knowledge")) * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			inf.removeHostInfraPID("knowledge")
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
	inf.removeHostInfraPID("knowledge")
	return nil
}

func (inf *Infra) hostKnowledgeStatus(ctx context.Context) runtimehost.InfraComponent {
	status := runtimehost.InfraComponent{Name: "knowledge", State: "missing", Health: "none"}
	pid, ok := inf.hostInfraPID("knowledge")
	if ok && processAlive(pid) {
		status.State = "running"
		status.ComponentID = fmt.Sprintf("host:%d", pid)
		status.ContainerID = status.ComponentID
		if inf.hostKnowledgeHealthy(ctx) {
			status.Health = "healthy"
		}
	} else if ok {
		status.State = "stopped"
	}
	status.BuildID = inf.BuildID
	return status
}

func (inf *Infra) hostKnowledgePython(sourceDir string) string {
	if python := strings.TrimSpace(os.Getenv("AGENCY_HOST_KNOWLEDGE_PYTHON")); python != "" {
		return python
	}
	venvPython := filepath.Join(sourceDir, ".venv", "bin", "python")
	if info, err := os.Stat(venvPython); err == nil && !info.IsDir() {
		return venvPython
	}
	return "python3"
}

func (inf *Infra) waitHostKnowledgeHealthy(ctx context.Context, pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return fmt.Errorf("host knowledge exited before becoming healthy")
		}
		if inf.hostKnowledgeHealthy(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("host knowledge not healthy within %v", timeout)
}

func (inf *Infra) hostKnowledgeHealthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+inf.gatewayProxyPort("8204")+"/health", nil)
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
