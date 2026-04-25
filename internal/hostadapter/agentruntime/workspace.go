package agentruntime

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/volume"
	"log/slog"

	"github.com/geoffbelknap/agency/internal/hostadapter/imageops"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/orchestrate/containers"
	"github.com/geoffbelknap/agency/internal/providerenv"
)

const (
	bodyImage = "agency-body:latest"
	agencyUID = "61000"
	agencyGID = "61000"
)

type Workspace struct {
	AgentName     string
	ContainerName string
	Home          string
	Backend       string
	Version       string
	SourceDir     string
	BuildID       string
	PrincipalUUID string
	cli           *runtimehost.RawClient
	log           *slog.Logger
}

type StartOptions struct {
	ScopedKey  string
	Model      string
	AdminModel string
	Env        map[string]string
	ExtraBinds []string
	Deps       WorkspaceDeps
}

func NewWorkspace(agentName, home, version string, logger *slog.Logger) (*Workspace, error) {
	return NewWorkspaceWithClient(agentName, home, version, logger, nil)
}

func NewWorkspaceWithClient(agentName, home, version string, logger *slog.Logger, cli *runtimehost.RawClient) (*Workspace, error) {
	if cli == nil {
		var err error
		cli, err = runtimehost.NewRawClient()
		if err != nil {
			return nil, err
		}
	}
	return &Workspace{
		AgentName:     agentName,
		ContainerName: fmt.Sprintf("%s-%s-workspace", prefix, agentName),
		Home:          home,
		Version:       version,
		cli:           cli,
		log:           logger,
	}, nil
}

func (w *Workspace) Start(ctx context.Context, opts StartOptions) error {
	if err := imageops.Resolve(ctx, w.cli, "body", w.Version, w.SourceDir, w.BuildID, w.log); err != nil {
		return fmt.Errorf("resolve body image: %w", err)
	}

	if err := containers.StopAndRemove(ctx, w.cli, w.ContainerName, 5); err != nil {
		return fmt.Errorf("remove previous workspace container: %w", err)
	}

	agentDir := filepath.Join(w.Home, "agents", w.AgentName)
	internalNet := fmt.Sprintf("%s-%s-internal", prefix, w.AgentName)

	volName := fmt.Sprintf("%s-%s-workspace-data", prefix, w.AgentName)
	w.ensureVolume(ctx, volName)

	memoryDir := filepath.Join(agentDir, "memory")
	_ = os.MkdirAll(memoryDir, 0o777)
	auditDir := filepath.Join(w.Home, "audit", w.AgentName)
	_ = os.MkdirAll(auditDir, 0o700)
	_ = os.Chmod(auditDir, 0o700)
	stateDir := filepath.Join(agentDir, "state")
	_ = os.MkdirAll(stateDir, 0o755)
	configDir := "/agency-config"

	contextFile := filepath.Join(stateDir, "session-context.json")
	if !fileExists(contextFile) {
		_ = os.WriteFile(contextFile, []byte("{}\n"), 0o666)
	}
	signalsFile := filepath.Join(stateDir, "agent-signals.jsonl")
	if !fileExists(signalsFile) {
		_ = os.WriteFile(signalsFile, []byte(""), 0o666)
	}

	constraintsFile := filepath.Join(agentDir, "constraints.yaml")
	if !fileExists(constraintsFile) {
		return fmt.Errorf("constraints.yaml not found for %s", w.AgentName)
	}

	enforcerHostName := enforcerHost(w.AgentName, w.Backend)
	defaultProxyURL := "http://" + enforcerHostName + ":3128"
	defaultControlURL := "http://" + enforcerHostName + ":8081"
	transportProxyURL := envValue(opts.Env, "AGENCY_ENFORCER_PROXY_URL", defaultProxyURL)
	controlURL := envValue(opts.Env, "AGENCY_ENFORCER_CONTROL_URL", defaultControlURL)
	if isContainerdBackend(w.Backend) {
		transportProxyURL = defaultProxyURL
		controlURL = defaultControlURL
	}
	enforcerURL := envValue(opts.Env, "AGENCY_ENFORCER_URL", strings.TrimRight(transportProxyURL, "/")+"/v1")
	if isContainerdBackend(w.Backend) {
		enforcerURL = strings.TrimRight(transportProxyURL, "/") + "/v1"
	}

	proxyURL := transportProxyURL
	if opts.ScopedKey != "" {
		proxyURL = strings.Replace(transportProxyURL, "http://", fmt.Sprintf("http://%s:x@", opts.ScopedKey), 1)
	}

	env := map[string]string{
		"AGENCY_CONFIG_DIR":           configDir,
		"AGENCY_ENFORCER_PROXY_URL":   transportProxyURL,
		"AGENCY_ENFORCER_URL":         enforcerURL,
		"AGENCY_ENFORCER_CONTROL_URL": controlURL,
		"AGENCY_ENFORCER_HEALTH_URL":  strings.TrimRight(transportProxyURL, "/") + "/health",
		"HTTP_PROXY":                  proxyURL,
		"HTTPS_PROXY":                 proxyURL,
		"NO_PROXY":                    enforcerHostName + ",localhost,127.0.0.1",
		"AGENCY_COMMS_URL":            strings.TrimRight(controlURL, "/") + "/mediation/comms",
		"AGENCY_KNOWLEDGE_URL":        strings.TrimRight(controlURL, "/") + "/mediation/knowledge",
		"AGENCY_AGENT_NAME":           w.AgentName,
		"AGENCY_MODEL":                "standard",
		"PATH":                        "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
	if opts.Model != "" {
		env["AGENCY_MODEL"] = opts.Model
	}
	if opts.AdminModel != "" {
		env["AGENCY_ADMIN_MODEL"] = opts.AdminModel
	}
	if opts.ScopedKey != "" {
		env["AGENCY_LLM_API_KEY"] = opts.ScopedKey
	}
	for k, v := range opts.Env {
		if strings.TrimSpace(k) == "" || v == "" {
			continue
		}
		env[k] = v
	}

	egressCA := filepath.Join(w.Home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	caBundle := filepath.Join(w.Home, "infrastructure", "egress", "certs", "ca-bundle.pem")
	if fileExists(caBundle) {
		env["SSL_CERT_FILE"] = "/etc/ssl/certs/agency-ca-bundle.pem"
		env["REQUESTS_CA_BUNDLE"] = "/etc/ssl/certs/agency-ca-bundle.pem"
	}
	if fileExists(egressCA) {
		env["NODE_EXTRA_CA_CERTS"] = "/etc/ssl/certs/agency-egress-ca.pem"
	}
	if opts.Deps.Env != nil {
		for k, v := range resolveWorkspaceEnv(opts.Deps.Env, w.Home, opts.ScopedKey) {
			env[k] = v
		}
	}
	if len(opts.Deps.Pip) > 0 {
		env["PYTHONUSERBASE"] = "/tmp/pip-packages"
		env["PYTHONPATH"] = "/tmp/pip-packages/lib/python3/site-packages:/tmp/pip-packages/lib/python3.13/site-packages:/tmp/pip-packages/lib/python3.12/site-packages"
		env["PATH"] = "/tmp/pip-packages/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}

	binds := []string{
		agentDir + ":" + configDir + ":ro",
		stateDir + ":" + configDir + "/state:rw",
		volName + ":/workspace:rw",
		memoryDir + ":/agency/memory:rw",
		auditDir + ":/var/lib/agency/audit:ro",
	}
	if knowledgeDir := filepath.Join(w.Home, "knowledge"); fileExists(filepath.Join(knowledgeDir, "ontology.yaml")) {
		if err := stageKnowledgeConfig(agentDir, knowledgeDir); err != nil {
			return fmt.Errorf("stage knowledge config: %w", err)
		}
	}
	if fileExists(egressCA) {
		binds = append(binds, egressCA+":/etc/ssl/certs/agency-egress-ca.pem:ro")
	}
	if fileExists(caBundle) {
		binds = append(binds, caBundle+":/etc/ssl/certs/agency-ca-bundle.pem:ro")
	}
	binds = append(binds, opts.ExtraBinds...)

	hostConfig := containers.HostConfigDefaults(containers.RoleWorkspace)
	hostConfig.Binds = binds
	hostConfig.NetworkMode = container.NetworkMode(internalNet)
	hostConfig.Tmpfs = workspaceTmpfs(opts.Deps)
	hostConfig.SecurityOpt = containers.WorkspaceSecurityOpts(w.Home, w.Backend)

	if _, err := w.createAndStartWorkspace(ctx, &container.Config{
		Image:    bodyImage,
		Hostname: "workspace",
		User:     agencyUID + ":" + agencyGID,
		Env:      mapToEnv(env),
		Labels: map[string]string{
			"agency.managed":        "true",
			"agency.agent":          w.AgentName,
			"agency.type":           "workspace",
			"agency.principal.uuid": w.PrincipalUUID,
			"agency.build.id":       imageops.ImageBuildLabel(ctx, w.cli, bodyImage),
			"agency.build.gateway":  w.BuildID,
		},
	}, hostConfig); err != nil {
		return fmt.Errorf("create/start workspace: %w", err)
	}

	if err := w.verifyNoProviderKeys(ctx); err != nil {
		_ = w.cli.ContainerRemove(ctx, w.ContainerName, container.RemoveOptions{Force: true})
		return fmt.Errorf("provider key verification failed (ASK tenet violation): %w", err)
	}

	if len(opts.Deps.Pip) > 0 || len(opts.Deps.Apt) > 0 {
		w.log.Info("provisioning workspace dependencies", "agent", w.AgentName)
		if err := provisionWorkspace(ctx, w.cli, w.ContainerName, opts.Deps, w.log); err != nil {
			w.log.Error("workspace provisioning failed — agent can still operate without vendor CLI", "agent", w.AgentName, "err", err)
		}
	}

	w.log.Info("workspace started", "agent", w.AgentName, "container", w.ContainerName)
	return nil
}

func envValue(values map[string]string, key, fallback string) string {
	if values == nil {
		return fallback
	}
	if v := strings.TrimSpace(values[key]); v != "" {
		return v
	}
	return fallback
}

func stageKnowledgeConfig(agentDir, knowledgeDir string) error {
	targetDir := filepath.Join(agentDir, "knowledge")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"ontology.yaml", "base-ontology.yaml"} {
		src := filepath.Join(knowledgeDir, name)
		if !fileExists(src) {
			continue
		}
		if err := copyFile(src, filepath.Join(targetDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func (w *Workspace) Stop(ctx context.Context) {
	timeout := 30
	_ = w.cli.ContainerStop(ctx, w.ContainerName, container.StopOptions{Timeout: &timeout})
}

func (w *Workspace) Remove(ctx context.Context) {
	_ = w.cli.ContainerRemove(ctx, w.ContainerName, container.RemoveOptions{Force: true})
}

func (w *Workspace) Pause(ctx context.Context) {
	_ = w.cli.ContainerPause(ctx, w.ContainerName)
}

func (w *Workspace) Unpause(ctx context.Context) {
	_ = w.cli.ContainerUnpause(ctx, w.ContainerName)
}

func (w *Workspace) IsRunning(ctx context.Context) bool {
	info, err := w.cli.ContainerInspect(ctx, w.ContainerName)
	if err != nil {
		return false
	}
	return info.State.Running
}

func (w *Workspace) verifyNoProviderKeys(ctx context.Context) error {
	info, err := w.cli.ContainerInspect(ctx, w.ContainerName)
	if err != nil {
		return fmt.Errorf("cannot inspect workspace container: %w", err)
	}

	leaked := providerenv.LeakedWorkspaceCredentialNames(info.Config.Env)

	if len(leaked) > 0 {
		w.log.Error("FRAMEWORK VIOLATION: provider keys in workspace", "agent", w.AgentName, "keys", strings.Join(leaked, ", "))
		return fmt.Errorf("LLM provider credentials visible in workspace env: %s", strings.Join(leaked, ", "))
	}
	w.log.Debug("provider key verification passed", "agent", w.AgentName)
	return nil
}

func (w *Workspace) ensureVolume(ctx context.Context, name string) {
	_, err := w.cli.VolumeInspect(ctx, name)
	if err == nil {
		return
	}
	_, _ = w.cli.VolumeCreate(ctx, volume.CreateOptions{
		Name: name,
		Labels: map[string]string{
			"agency.agent": w.AgentName,
			"agency.type":  "workspace",
		},
	})
}

func (w *Workspace) createAndStartWorkspace(ctx context.Context, config *container.Config, hostConfig *container.HostConfig) (string, error) {
	backoff := []time.Duration{0, 250 * time.Millisecond, 750 * time.Millisecond}
	var lastErr error

	for attempt, delay := range backoff {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}

		id, err := containers.CreateAndStart(ctx, w.cli, w.ContainerName, config, hostConfig, nil)
		if err == nil {
			return id, nil
		}
		lastErr = err
		if !isTransientNetnsStartupError(err) || attempt == len(backoff)-1 {
			break
		}
		_ = containers.StopAndRemove(ctx, w.cli, w.ContainerName, 5)
	}

	return "", lastErr
}

func isTransientNetnsStartupError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "bind-mount /proc/") &&
		strings.Contains(msg, "ns/net") &&
		strings.Contains(msg, "no such file or directory")
}
