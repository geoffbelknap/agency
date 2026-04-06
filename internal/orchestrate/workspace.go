package orchestrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"

	"github.com/geoffbelknap/agency/internal/images"
	"github.com/geoffbelknap/agency/internal/orchestrate/containers"
)

// forbiddenProviderKeys lists LLM provider API key env vars that must never
// appear in a workspace container. OPENAI_API_KEY is handled separately because
// it is allowed when it holds an agency-scoped token (prefix "agency-scoped--").
// Corresponds to Python verify_no_provider_keys (ASK tenet 1/5).
var forbiddenProviderKeys = []string{
	"ANTHROPIC_API_KEY",
	"GOOGLE_API_KEY",
	"GEMINI_API_KEY",
	"AWS_SECRET_ACCESS_KEY",
}

const (
	bodyImage  = "agency-body:latest"
	agencyUID  = "61000"
	agencyGID  = "61000"
)

// Workspace manages the per-agent workspace (body runtime) container.
type Workspace struct {
	AgentName     string
	ContainerName string
	Home          string
	Version       string
	SourceDir     string
	BuildID       string
	PrincipalUUID string // agent UUID from principal registry
	cli           *client.Client
	log           *log.Logger
}

func NewWorkspace(agentName, home, version string, logger *log.Logger) (*Workspace, error) {
	return NewWorkspaceWithClient(agentName, home, version, logger, nil)
}

// NewWorkspaceWithClient creates a Workspace using the provided Docker client (or a new
// one if cli is nil). Prefer passing a shared client to avoid redundant connections.
func NewWorkspaceWithClient(agentName, home, version string, logger *log.Logger, cli *client.Client) (*Workspace, error) {
	if cli == nil {
		var err error
		cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
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

// StartOptions configures the workspace container.
type StartOptions struct {
	ScopedKey  string // Enforcer API key for auth
	Model      string // Primary LLM model
	AdminModel string // Mini-tier model for cheap calls
	ExtraBinds []string
	Deps       WorkspaceDeps // Declared workspace dependencies
}


// Start creates and starts the workspace container inside the enforcement boundary.
func (w *Workspace) Start(ctx context.Context, opts StartOptions) error {
	// Resolve body image (local → GHCR pull → embedded build)
	if err := images.Resolve(ctx, w.cli, "body", w.Version, w.SourceDir, w.BuildID, w.log); err != nil {
		return fmt.Errorf("resolve body image: %w", err)
	}

	// Remove existing
	_ = w.cli.ContainerRemove(ctx, w.ContainerName, container.RemoveOptions{Force: true})

	agentDir := filepath.Join(w.Home, "agents", w.AgentName)
	internalNet := fmt.Sprintf("%s-%s-internal", prefix, w.AgentName)

	// Ensure workspace volume
	volName := fmt.Sprintf("%s-%s-workspace-data", prefix, w.AgentName)
	w.ensureVolume(ctx, volName)

	// Ensure directories
	memoryDir := filepath.Join(agentDir, "memory")
	os.MkdirAll(memoryDir, 0777)
	auditDir := filepath.Join(w.Home, "audit", w.AgentName)
	os.MkdirAll(auditDir, 0700)
	os.Chmod(auditDir, 0700)
	stateDir := filepath.Join(agentDir, "state")
	os.MkdirAll(stateDir, 0755)

	// Ensure state files
	contextFile := filepath.Join(stateDir, "session-context.json")
	if !fileExists(contextFile) {
		os.WriteFile(contextFile, []byte("{}\n"), 0666)
	}
	signalsFile := filepath.Join(stateDir, "agent-signals.jsonl")
	if !fileExists(signalsFile) {
		os.WriteFile(signalsFile, []byte(""), 0666)
	}

	// Constraints must exist (tenet 1)
	constraintsFile := filepath.Join(agentDir, "constraints.yaml")
	if !fileExists(constraintsFile) {
		return fmt.Errorf("constraints.yaml not found for %s", w.AgentName)
	}

	// Build proxy URL with scoped key for auth
	proxyURL := "http://enforcer:3128"
	if opts.ScopedKey != "" {
		proxyURL = fmt.Sprintf("http://%s:x@enforcer:3128", opts.ScopedKey)
	}

	env := map[string]string{
		"AGENCY_ENFORCER_URL":   "http://enforcer:3128/v1",
		"OPENAI_API_BASE":       "http://enforcer:3128/v1",
		"HTTP_PROXY":            proxyURL,
		"HTTPS_PROXY":           proxyURL,
		"NO_PROXY":              "enforcer,localhost,127.0.0.1",
		"AGENCY_COMMS_URL":      "http://enforcer:8081/mediation/comms",
		"AGENCY_KNOWLEDGE_URL":  "http://enforcer:8081/mediation/knowledge",
		"AGENCY_AGENT_NAME":     w.AgentName,
		"AGENCY_MODEL":          "claude-sonnet",
		"PATH":                  "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}

	if opts.Model != "" {
		env["AGENCY_MODEL"] = opts.Model
	}
	if opts.AdminModel != "" {
		env["AGENCY_ADMIN_MODEL"] = opts.AdminModel
	}
	if opts.ScopedKey != "" {
		env["OPENAI_API_KEY"] = opts.ScopedKey
	}

	// CA certs for egress proxy trust
	egressCA := filepath.Join(w.Home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	caBundle := filepath.Join(w.Home, "infrastructure", "egress", "certs", "ca-bundle.pem")
	if fileExists(caBundle) {
		env["SSL_CERT_FILE"] = "/etc/ssl/certs/agency-ca-bundle.pem"
		env["REQUESTS_CA_BUNDLE"] = "/etc/ssl/certs/agency-ca-bundle.pem"
	}
	if fileExists(egressCA) {
		env["NODE_EXTRA_CA_CERTS"] = "/etc/ssl/certs/agency-egress-ca.pem"
	}

	// Inject workspace dependency env vars (resolved from component manifests)
	if opts.Deps.Env != nil {
		resolved := resolveWorkspaceEnv(opts.Deps.Env, w.Home, opts.ScopedKey)
		for k, v := range resolved {
			env[k] = v
		}
	}

	// When pip packages are declared, set PYTHONUSERBASE and PATH so
	// pip --user installs to /tmp/pip-packages and CLI scripts are found.
	if len(opts.Deps.Pip) > 0 {
		env["PYTHONUSERBASE"] = "/tmp/pip-packages"
		env["PYTHONPATH"] = "/tmp/pip-packages/lib/python3/site-packages:/tmp/pip-packages/lib/python3.13/site-packages:/tmp/pip-packages/lib/python3.12/site-packages"
		env["PATH"] = "/tmp/pip-packages/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}

	// Core binds — enforce ASK tenets via mount modes
	binds := []string{
		constraintsFile + ":/agency/constraints.yaml:ro",          // Tenet 1: read-only
		volName + ":/workspace:rw",                                 // Named volume
		memoryDir + ":/agency/memory:rw",                           // Agent-writable
		filepath.Join(agentDir, "identity.md") + ":/agency/identity.md:ro",
		auditDir + ":/var/lib/agency/audit:ro",                     // Tenet 2: read-only
		contextFile + ":/agency/state/session-context.json:ro",     // Agent reads, never writes
		signalsFile + ":/agency/state/agent-signals.jsonl:rw",
	}

	// Optional file mounts — static files read once at startup.
	// Hot-reloadable files (PLATFORM.md, mission.yaml, services-manifest.json)
	// are fetched via API from the enforcer at http://enforcer:8081/config/.
	for _, pair := range []struct{ src, dst string }{
		{filepath.Join(agentDir, "AGENTS.md"), "/agency/AGENTS.md:ro"},
		{filepath.Join(agentDir, "FRAMEWORK.md"), "/agency/FRAMEWORK.md:ro"},
		{filepath.Join(agentDir, "skills-manifest.json"), "/agency/skills-manifest.json:ro"},
		{filepath.Join(agentDir, "mcp-servers.json"), "/agency/mcp-servers.json:ro"},
		{filepath.Join(w.Home, "knowledge", "ontology.yaml"), "/agency/knowledge/ontology.yaml:ro"},
	} {
		if fileExists(pair.src) {
			binds = append(binds, pair.src+":"+pair.dst)
		}
	}

	// CA certs
	if fileExists(egressCA) {
		binds = append(binds, egressCA+":/etc/ssl/certs/agency-egress-ca.pem:ro")
	}
	if fileExists(caBundle) {
		binds = append(binds, caBundle+":/etc/ssl/certs/agency-ca-bundle.pem:ro")
	}

	// Extra binds (cross-boundary visibility, etc.)
	binds = append(binds, opts.ExtraBinds...)

	hostConfig := containers.HostConfigDefaults(containers.RoleWorkspace)
	hostConfig.Binds = binds
	hostConfig.NetworkMode = container.NetworkMode(internalNet)
	hostConfig.Tmpfs = workspaceTmpfs(opts.Deps)
	hostConfig.SecurityOpt = []string{
		"no-new-privileges:true",
		"seccomp=" + containers.SeccompProfile(w.Home),
	}

	if _, err := containers.CreateAndStart(ctx, w.cli,
		w.ContainerName,
		&container.Config{
			Image:    bodyImage,
			Hostname: "workspace",
			User:     agencyUID + ":" + agencyGID,
			Env:      mapToEnv(env),
			Labels: map[string]string{
				"agency.agent":          w.AgentName,
				"agency.type":           "workspace",
				"agency.principal.uuid": w.PrincipalUUID,
				"agency.build.id":       images.ImageBuildLabel(ctx, w.cli, bodyImage),
				"agency.build.gateway":  w.BuildID,
			},
		},
		hostConfig,
		nil,
	); err != nil {
		return fmt.Errorf("create/start workspace: %w", err)
	}

	// ASK tenet 1/5: verify no real LLM provider keys leaked into the workspace.
	// This mirrors the Python verify_no_provider_keys check that ran during start.
	if err := w.verifyNoProviderKeys(ctx); err != nil {
		// Fail closed: tear down the workspace container on violation.
		_ = w.cli.ContainerRemove(ctx, w.ContainerName, container.RemoveOptions{Force: true})
		return fmt.Errorf("provider key verification failed (ASK tenet violation): %w", err)
	}

	// Provision declared packages (Tier 1 tool integration)
	if len(opts.Deps.Pip) > 0 || len(opts.Deps.Apt) > 0 {
		w.log.Info("provisioning workspace dependencies", "agent", w.AgentName)
		if err := provisionWorkspace(ctx, w.cli, w.ContainerName, opts.Deps, w.log); err != nil {
			w.log.Error("workspace provisioning failed — agent can still operate without vendor CLI",
				"agent", w.AgentName, "err", err)
		}
	}

	w.log.Info("workspace started", "agent", w.AgentName, "container", w.ContainerName)
	return nil
}

// Stop stops the workspace container.
func (w *Workspace) Stop(ctx context.Context) {
	t := 30
	_ = w.cli.ContainerStop(ctx, w.ContainerName, container.StopOptions{Timeout: &t})
}

// Remove removes the workspace container.
func (w *Workspace) Remove(ctx context.Context) {
	_ = w.cli.ContainerRemove(ctx, w.ContainerName, container.RemoveOptions{Force: true})
}

// Pause pauses the workspace container.
func (w *Workspace) Pause(ctx context.Context) {
	_ = w.cli.ContainerPause(ctx, w.ContainerName)
}

// Unpause unpauses the workspace container.
func (w *Workspace) Unpause(ctx context.Context) {
	_ = w.cli.ContainerUnpause(ctx, w.ContainerName)
}

// IsRunning returns true if the workspace is running.
func (w *Workspace) IsRunning(ctx context.Context) bool {
	info, err := w.cli.ContainerInspect(ctx, w.ContainerName)
	if err != nil {
		return false
	}
	return info.State.Running
}

// verifyNoProviderKeys inspects the running workspace container and verifies
// that no real LLM provider API keys are present in its environment. This is a
// critical ASK tenet enforcement: agents must never see real credentials.
// OPENAI_API_KEY is permitted only if it holds an agency-scoped token.
func (w *Workspace) verifyNoProviderKeys(ctx context.Context) error {
	info, err := w.cli.ContainerInspect(ctx, w.ContainerName)
	if err != nil {
		return fmt.Errorf("cannot inspect workspace container: %w", err)
	}

	var leaked []string
	for _, envVar := range info.Config.Env {
		for _, key := range forbiddenProviderKeys {
			if strings.HasPrefix(envVar, key+"=") {
				parts := strings.SplitN(envVar, "=", 2)
				if len(parts) == 2 && parts[1] != "" {
					leaked = append(leaked, key)
				}
			}
		}
		// OPENAI_API_KEY: only agency-scoped tokens are allowed
		if strings.HasPrefix(envVar, "OPENAI_API_KEY=") {
			parts := strings.SplitN(envVar, "=", 2)
			if len(parts) == 2 && parts[1] != "" && !strings.HasPrefix(parts[1], "agency-scoped--") {
				leaked = append(leaked, "OPENAI_API_KEY (not agency-scoped)")
			}
		}
	}

	if len(leaked) > 0 {
		w.log.Error("FRAMEWORK VIOLATION: provider keys in workspace",
			"agent", w.AgentName, "keys", strings.Join(leaked, ", "))
		return fmt.Errorf("LLM provider credentials visible in workspace env: %s", strings.Join(leaked, ", "))
	}

	w.log.Debug("provider key verification passed", "agent", w.AgentName)
	return nil
}

func (w *Workspace) ensureVolume(ctx context.Context, name string) {
	_, err := w.cli.VolumeInspect(ctx, name)
	if err != nil {
		w.cli.VolumeCreate(ctx, volume.CreateOptions{
			Name: name,
			Labels: map[string]string{
				"agency.agent": w.AgentName,
				"agency.type":  "workspace",
			},
		})
	}
}
