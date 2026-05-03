package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/pkg/envfile"
)

func (inf *Infra) hostEgressEnabled() bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv("AGENCY_HOST_INFRA_EGRESS")))
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

func (inf *Infra) ensureHostEgress(ctx context.Context) error {
	paths, err := inf.prepareHostEgressPaths()
	if err != nil {
		return err
	}
	if pid, ok := inf.hostInfraPID("egress"); ok {
		if processAlive(pid) && inf.hostEgressHealthy(ctx) {
			return nil
		}
		if err := inf.stopHostEgress(ctx); err != nil {
			inf.log.Warn("stop stale host egress", "err", err)
		}
	}
	if inf.hostEgressHealthy(ctx) {
		return fmt.Errorf("host egress port %s is already serving without a managed host egress process; stop the legacy egress bridge", inf.egressProxyPort())
	}

	sourceDir, err := inf.hostInfraSourceDir(filepath.Join("services", "egress", "addon.py"))
	if err != nil {
		return fmt.Errorf("host egress source unavailable: %w", err)
	}
	egressDir := filepath.Join(sourceDir, "services", "egress")
	addonPath := filepath.Join(egressDir, "addon.py")
	if err := inf.fetchHostEgressBlocklists(ctx, sourceDir, paths); err != nil {
		inf.log.Warn("host egress blocklist fetch failed", "err", err)
	}

	logPath := filepath.Join(paths.logDir, "egress.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open host egress log: %w", err)
	}

	cmd := exec.Command(inf.hostEgressMitmdump(sourceDir),
		"--listen-port", inf.egressProxyPort(),
		"--set", "block_global=false",
		"--set", "confdir="+paths.certsDir,
		"--scripts", addonPath,
	)
	cmd.Dir = egressDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), inf.hostEgressEnv(sourceDir, paths)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start host egress: %w", err)
	}
	pid := cmd.Process.Pid
	if err := logFile.Close(); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		return fmt.Errorf("close host egress log: %w", err)
	}
	if err := inf.writeHostInfraPID("egress", pid); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		return fmt.Errorf("write host egress pid: %w", err)
	}
	if err := inf.writeHostInfraMetadata("egress", pid, cmd.Args, logPath, "http://127.0.0.1:"+inf.egressProxyPort()+"/health"); err != nil {
		inf.log.Warn("write host egress metadata", "err", err)
	}
	go func() {
		if err := cmd.Wait(); err != nil && inf.log != nil {
			inf.log.Warn("host egress exited", "pid", pid, "err", err)
		}
	}()
	if err := inf.waitHostEgressHealthy(ctx, pid, 30*time.Second); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		return err
	}
	return nil
}

type hostEgressPaths struct {
	configDir     string
	blocklistsDir string
	certsDir      string
	logDir        string
	runDir        string
	policyPath    string
	swapPath      string
	swapLocalPath string
}

func (inf *Infra) prepareHostEgressPaths() (hostEgressPaths, error) {
	infraDir := filepath.Join(inf.Home, "infrastructure")
	configDir := filepath.Join(infraDir, "egress")
	paths := hostEgressPaths{
		configDir:     configDir,
		blocklistsDir: filepath.Join(configDir, "blocklists"),
		certsDir:      filepath.Join(configDir, "certs"),
		logDir:        filepath.Join(inf.Home, "logs", "infra"),
		runDir:        filepath.Join(inf.Home, "run"),
		policyPath:    filepath.Join(configDir, "policy.yaml"),
		swapPath:      filepath.Join(infraDir, "credential-swaps.yaml"),
		swapLocalPath: filepath.Join(infraDir, "credential-swaps.local.yaml"),
	}
	for _, dir := range []string{paths.blocklistsDir, paths.certsDir, paths.logDir, paths.runDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return paths, err
		}
	}
	for _, dir := range []string{paths.blocklistsDir, paths.certsDir} {
		if err := os.Chmod(dir, 0o777); err != nil {
			return paths, err
		}
	}
	if err := repairMitmproxyCertStore(paths.certsDir); err != nil {
		return paths, err
	}
	if entries, err := os.ReadDir(paths.certsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				_ = os.Chmod(filepath.Join(paths.certsDir, e.Name()), 0o644)
			}
		}
	}
	return paths, nil
}

func repairMitmproxyCertStore(certsDir string) error {
	for _, name := range []string{
		"mitmproxy-ca.pem",
		"mitmproxy-ca-cert.pem",
		"mitmproxy-ca-cert.cer",
		"mitmproxy-ca-cert.p12",
		"mitmproxy-dhparam.pem",
	} {
		path := filepath.Join(certsDir, name)
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("remove invalid mitmproxy cert directory %s: %w", path, err)
			}
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat mitmproxy cert path %s: %w", path, err)
		}
	}
	return nil
}

func (inf *Infra) fetchHostEgressBlocklists(ctx context.Context, sourceDir string, paths hostEgressPaths) error {
	script := filepath.Join(sourceDir, "services", "egress", "fetch_blocklists.py")
	if _, err := os.Stat(script); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, inf.hostEgressPython(sourceDir), script, paths.policyPath, paths.blocklistsDir)
	cmd.Dir = filepath.Join(sourceDir, "services", "egress")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (inf *Infra) hostEgressEnv(sourceDir string, paths hostEgressPaths) []string {
	env := envfile.Load(filepath.Join(inf.Home, ".env"))
	cfg := config.Load()
	for k, v := range cfg.ConfigVars {
		env[k] = v
	}
	env["PYTHONPATH"] = filepath.Join(sourceDir, "services", "egress") + string(os.PathListSeparator) + sourceDir
	env["GATEWAY_URL"] = "http://" + inf.GatewayAddr
	env["GATEWAY_TOKEN"] = inf.EgressToken
	env["GATEWAY_SOCKET"] = filepath.Join(paths.runDir, "gateway-cred.sock")
	env["AGENCY_CALLER"] = "egress"
	env["AGENCY_COMPONENT"] = "egress"
	env["BUILD_ID"] = inf.BuildID
	env["AGENCY_HOST_EGRESS_PROXY_URL"] = "http://127.0.0.1:" + inf.egressProxyPort()
	env["AGENCY_EGRESS_POLICY_PATH"] = paths.policyPath
	env["AGENCY_EGRESS_BLOCKLIST_DIR"] = paths.blocklistsDir
	env["AGENCY_EGRESS_LOG_DIR"] = paths.logDir
	env["AGENCY_EGRESS_SWAP_CONFIG_PATH"] = paths.swapPath
	env["AGENCY_EGRESS_SWAP_LOCAL_PATH"] = paths.swapLocalPath
	return mapToEnv(env)
}

func (inf *Infra) stopHostEgress(ctx context.Context) error {
	pid, err := inf.readHostInfraPID("egress")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if pid <= 0 {
		inf.removeHostInfraPID("egress")
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
	}
	deadline := time.Now().Add(time.Duration(stopTimeoutFor("egress")) * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			inf.removeHostInfraPID("egress")
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
	inf.removeHostInfraPID("egress")
	return nil
}

func (inf *Infra) hostEgressStatus(ctx context.Context) runtimehost.InfraComponent {
	status := runtimehost.InfraComponent{Name: "egress", State: "missing", Health: "none"}
	pid, ok := inf.hostInfraPID("egress")
	if ok && processAlive(pid) {
		status.State = "running"
		status.ComponentID = fmt.Sprintf("host:%d", pid)
		status.ContainerID = status.ComponentID
		if inf.hostEgressHealthy(ctx) {
			status.Health = "healthy"
		}
	} else if ok {
		status.State = "stopped"
	}
	status.BuildID = inf.BuildID
	return status
}

func (inf *Infra) hostEgressPython(sourceDir string) string {
	if python := strings.TrimSpace(os.Getenv("AGENCY_HOST_EGRESS_PYTHON")); python != "" {
		return python
	}
	if venvPython := hostInfraVenvBin(sourceDir, "python"); venvPython != "" {
		return venvPython
	}
	return "python3"
}

func (inf *Infra) hostEgressMitmdump(sourceDir string) string {
	if mitmdump := strings.TrimSpace(os.Getenv("AGENCY_HOST_EGRESS_MITMDUMP")); mitmdump != "" {
		return mitmdump
	}
	if venvMitmdump := hostInfraVenvBin(sourceDir, "mitmdump"); venvMitmdump != "" {
		return venvMitmdump
	}
	return "mitmdump"
}

func (inf *Infra) waitHostEgressHealthy(ctx context.Context, pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return fmt.Errorf("host egress exited before becoming healthy")
		}
		if inf.hostEgressHealthy(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("host egress not healthy within %v", timeout)
}

func (inf *Infra) hostEgressHealthy(ctx context.Context) bool {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", "127.0.0.1:"+inf.egressProxyPort())
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
