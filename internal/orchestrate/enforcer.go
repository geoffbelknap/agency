package orchestrate

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"strings"

	"github.com/charmbracelet/log"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"

	"github.com/geoffbelknap/agency/internal/images"
	"github.com/geoffbelknap/agency/internal/orchestrate/containers"
	"github.com/geoffbelknap/agency/internal/services"
	"gopkg.in/yaml.v3"
)

const enforcerImage = "agency-enforcer:latest"

// Enforcer manages the per-agent enforcer sidecar container.
type Enforcer struct {
	AgentName     string
	ContainerName string
	Home          string
	Version       string
	SourceDir     string
	BuildID       string
	LifecycleID   string
	cli           *client.Client
	log           *log.Logger
	hmacKey       []byte
}

func NewEnforcer(agentName, home, version string, logger *log.Logger, hmacKey []byte) (*Enforcer, error) {
	return NewEnforcerWithClient(agentName, home, version, logger, hmacKey, nil)
}

// NewEnforcerWithClient creates an Enforcer using the provided Docker client (or a new
// one if cli is nil). Prefer passing a shared client to avoid redundant connections.
func NewEnforcerWithClient(agentName, home, version string, logger *log.Logger, hmacKey []byte, cli *client.Client) (*Enforcer, error) {
	if cli == nil {
		var err error
		cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return nil, err
		}
	}
	return &Enforcer{
		AgentName:     agentName,
		ContainerName: fmt.Sprintf("%s-%s-enforcer", prefix, agentName),
		Home:          home,
		Version:       version,
		cli:           cli,
		log:           logger,
		hmacKey:       hmacKey,
	}, nil
}

// Start creates and starts the enforcer sidecar. If already running, sends SIGHUP
// to reload config (avoids expensive container create cycle on restarts).
// Returns the scoped API key for the workspace to authenticate with.
func (e *Enforcer) Start(ctx context.Context) (scopedKey string, err error) {
	return e.start(ctx, false)
}

// StartWithKeyRotation creates and starts the enforcer sidecar, forcing
// generation of a new scoped key regardless of existing state.
// Used on agent restart to ensure fresh credentials (ASK tenet 4: least privilege).
func (e *Enforcer) StartWithKeyRotation(ctx context.Context) (scopedKey string, err error) {
	return e.start(ctx, true)
}

func (e *Enforcer) start(ctx context.Context, rotateKey bool) (scopedKey string, err error) {
	if err := images.Resolve(ctx, e.cli, "enforcer", e.Version, e.SourceDir, e.BuildID, e.log); err != nil {
		return "", fmt.Errorf("resolve enforcer image: %w", err)
	}

	// Always recreate the enforcer container to ensure env vars (GATEWAY_URL,
	// GATEWAY_TOKEN, AGENCY_LIFECYCLE_ID) are current. SIGHUP reloads config
	// files but can't update environment variables.
	// Remove stale container
	_ = e.cli.ContainerRemove(ctx, e.ContainerName, container.RemoveOptions{Force: true})

	// Ensure directories
	policyDir := e.ensurePolicy()
	configDir := e.ensureConfig(rotateKey)
	dataDir := filepath.Join(e.Home, "infrastructure", "enforcer", "data", e.AgentName)
	auditDir := filepath.Join(e.Home, "audit", e.AgentName, "enforcer")
	agentDir := filepath.Join(e.Home, "agents", e.AgentName)
	servicesDir := filepath.Join(e.Home, "services")
	for _, d := range []string{dataDir, auditDir, agentDir, servicesDir} {
		os.MkdirAll(d, 0777)
	}

	internalNet := fmt.Sprintf("%s-%s-internal", prefix, e.AgentName)

	env := map[string]string{
		"HOME":               "/agency/enforcer/data",
		"AGENT_NAME":         e.AgentName,
		"CONSTRAINT_WS_PORT": "8081",
		"GATEWAY_URL":        "http://gateway:8200",
	}
	if e.LifecycleID != "" {
		env["AGENCY_LIFECYCLE_ID"] = e.LifecycleID
	}
	// Gateway auth token + listen port from config
	if tokenData, err := os.ReadFile(filepath.Join(e.Home, "config.yaml")); err == nil {
		var cf struct {
			Token       string `yaml:"token"`
			GatewayAddr string `yaml:"gateway_addr"`
		}
		if yaml.Unmarshal(tokenData, &cf) == nil {
			if cf.Token != "" {
				env["GATEWAY_TOKEN"] = cf.Token
			}
			if cf.GatewayAddr != "" {
				env["GATEWAY_URL"] = "http://gateway:8200"
			}
		}
	}
	egressCA := filepath.Join(e.Home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	if fileExists(egressCA) {
		env["SSL_CERT_FILE"] = "/etc/ssl/certs/agency-egress-ca.pem"
	}

	// Per-agent auth directory for scoped API key isolation (ASK tenet 4)
	perAgentAuthDir := filepath.Join(e.Home, "agents", e.AgentName, "state", "enforcer-auth")

	binds := []string{
		policyDir + ":/agency/enforcer/policy:ro",
		filepath.Join(configDir, "server-config.yaml") + ":/agency/enforcer/server-config.yaml:ro",
		perAgentAuthDir + ":/agency/enforcer/auth:ro",
		dataDir + ":/agency/enforcer/data:rw",
		auditDir + ":/agency/enforcer/audit:rw",
		agentDir + ":/agency/agent:ro",
		servicesDir + ":/agency/enforcer/services:ro",
	}

	// Routing config
	routingYAML := filepath.Join(e.Home, "infrastructure", "routing.yaml")
	if fileExists(routingYAML) {
		binds = append(binds, routingYAML+":/agency/enforcer/routing.yaml:ro")
	}

	// Egress CA cert
	if fileExists(egressCA) {
		binds = append(binds, egressCA+":/etc/ssl/certs/agency-egress-ca.pem:ro")
	}

	// Memory directory — mounted read-only into the enforcer so the auditor can
	// observe mutations without being able to alter agent identity state
	// (ASK Tenet 25: identity writes are logged with provenance).
	memoryDir := filepath.Join(agentDir, "memory")
	if _, err := os.Stat(memoryDir); err == nil {
		binds = append(binds, memoryDir+":/agency/memory:ro")
	}

	enforcerHostConfig := containers.HostConfigDefaults(containers.RoleEnforcer)
	enforcerHostConfig.Binds = binds
	enforcerHostConfig.NetworkMode = container.NetworkMode(internalNet)
	enforcerHostConfig.Tmpfs = map[string]string{"/tmp": "size=64M", "/run": "size=32M"}

	containerID, err := containers.CreateAndStart(ctx, e.cli,
		e.ContainerName,
		&container.Config{
			Image:    enforcerImage,
			Hostname: "enforcer",
			Env:      mapToEnv(env),
			Labels: map[string]string{
				services.LabelServiceEnabled:         "true",
				services.LabelServiceName:            e.AgentName + "/enforcer",
				services.LabelServicePort:            "3128",
				services.LabelServiceHealth:          "/health",
				services.LabelServiceNetwork:         internalNet,
				services.LabelServiceHMAC:            services.GenerateHMAC(e.ContainerName, e.hmacKey),
				"agency.constraint.port":             "8081",
				"agency.constraint.ws.path":          "/ws",
				"agency.constraint.rest.constraints": "/constraints",
				"agency.constraint.rest.ack":         "/constraints/ack",
				"agency.build.id":                    images.ImageBuildLabel(ctx, e.cli, enforcerImage),
				"agency.build.gateway":               e.BuildID,
			},
			Healthcheck: &container.HealthConfig{
				Test:        []string{"CMD-SHELL", "curl -sf http://127.0.0.1:3128/health || exit 1"},
				Interval:    2 * time.Second,
				Timeout:     3 * time.Second,
				Retries:     5,
				StartPeriod: 3 * time.Second,
			},
		},
		enforcerHostConfig,
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("create/start enforcer: %w", err)
	}

	// Wait for running
	if err := waitContainerRunning(ctx, e.cli, e.ContainerName, 10*time.Second); err != nil {
		return "", err
	}

	// Connect to mediation network
	_ = e.cli.NetworkConnect(ctx, mediationNet, containerID, &network.EndpointSettings{
		Aliases: []string{"enforcer"},
	})

	return e.readAPIKey(), nil
}

// HealthCheck waits for the enforcer to become healthy.
func (e *Enforcer) HealthCheck(ctx context.Context, timeout time.Duration) error {
	return waitContainerHealthy(ctx, e.cli, e.ContainerName, timeout)
}

// Stop stops and removes the enforcer container.
func (e *Enforcer) Stop(ctx context.Context) {
	t := 10
	_ = e.cli.ContainerStop(ctx, e.ContainerName, container.StopOptions{Timeout: &t})
	_ = e.cli.ContainerRemove(ctx, e.ContainerName, container.RemoveOptions{Force: true})
}

// Pause pauses the enforcer container.
func (e *Enforcer) Pause(ctx context.Context) {
	_ = e.cli.ContainerPause(ctx, e.ContainerName)
}

// Unpause unpauses the enforcer container.
func (e *Enforcer) Unpause(ctx context.Context) {
	_ = e.cli.ContainerUnpause(ctx, e.ContainerName)
}

func (e *Enforcer) readAPIKey() string {
	keysFile := filepath.Join(e.Home, "agents", e.AgentName, "state", "enforcer-auth", "api_keys.yaml")
	data, err := os.ReadFile(keysFile)
	if err != nil {
		return ""
	}
	// Simple YAML parse: - key: "VALUE"
	for _, line := range splitLines(string(data)) {
		if len(line) > 7 && line[:7] == "- key: " {
			val := line[7:]
			val = trimQuotes(val)
			return val
		}
	}
	return ""
}

func (e *Enforcer) ensurePolicy() string {
	dir := filepath.Join(e.Home, "infrastructure", "enforcer", "policy")
	os.MkdirAll(dir, 0755)
	policyFile := filepath.Join(dir, "standard-agent.yaml")
	if !fileExists(policyFile) {
		os.WriteFile(policyFile, []byte(defaultEnforcerPolicy), 0644)
	}
	return dir
}

func (e *Enforcer) ensureConfig(rotateKey bool) string {
	dir := filepath.Join(e.Home, "infrastructure", "enforcer", "config")
	os.MkdirAll(dir, 0755)

	configFile := filepath.Join(dir, "server-config.yaml")
	if !fileExists(configFile) {
		os.WriteFile(configFile, []byte(defaultEnforcerConfig), 0644)
	}

	// Write API keys to per-agent state directory so each agent has its own
	// scoped key, and the enforcer container mounts from this path.
	authDir := filepath.Join(e.Home, "agents", e.AgentName, "state", "enforcer-auth")
	os.MkdirAll(authDir, 0755)
	keysFile := filepath.Join(authDir, "api_keys.yaml")

	needNewKey := rotateKey || !fileExists(keysFile)
	if !needNewKey {
		// Check that existing key has the scoped prefix; if not, regenerate
		existing := e.readAPIKey()
		scopedPrefix := "agency-scoped--"
		if len(existing) < len(scopedPrefix) || existing[:len(scopedPrefix)] != scopedPrefix {
			needNewKey = true
		}
	}
	if needNewKey {
		key := "agency-scoped--" + generateToken(32)
		os.WriteFile(keysFile, []byte(fmt.Sprintf("- key: \"%s\"\n  name: \"agency-workspace\"\n", key)), 0644)
		// Log that the scoped key was generated without exposing the value
		e.log.Info("scoped API key generated", "agent", e.AgentName, "rotated", rotateKey)
	}
	return dir
}

// -- Shared helpers --

func waitContainerRunning(ctx context.Context, cli *client.Client, name string, timeout time.Duration) error {
	// Quick check — already running?
	if info, err := cli.ContainerInspect(ctx, name); err == nil && info.State.Running {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	eventCh, errCh := cli.Events(ctx, events.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("container", name),
			filters.Arg("event", "start"),
		),
	})

	for {
		select {
		case <-eventCh:
			if info, err := cli.ContainerInspect(ctx, name); err == nil && info.State.Running {
				return nil
			}
		case err := <-errCh:
			if ctx.Err() != nil {
				return fmt.Errorf("container %s did not start within %v", name, timeout)
			}
			return fmt.Errorf("event stream error for %s: %w", name, err)
		case <-ctx.Done():
			return fmt.Errorf("container %s did not start within %v", name, timeout)
		}
	}
}

func waitContainerHealthy(ctx context.Context, cli *client.Client, name string, timeout time.Duration) error {
	// Quick check — already healthy?
	if info, err := cli.ContainerInspect(ctx, name); err == nil {
		if info.State.Health != nil && info.State.Health.Status == "healthy" {
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	eventCh, errCh := cli.Events(ctx, events.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("container", name),
			filters.Arg("event", "health_status"),
		),
	})

	for {
		select {
		case ev := <-eventCh:
			status := ev.Actor.Attributes["health_status"]
			if status == "" {
				status = ev.Status
			}
			if strings.Contains(status, "healthy") && !strings.Contains(status, "unhealthy") {
				return nil
			}
		case err := <-errCh:
			if ctx.Err() != nil {
				return fmt.Errorf("container %s did not become healthy within %v", name, timeout)
			}
			return fmt.Errorf("event stream error for %s: %w", name, err)
		case <-ctx.Done():
			return fmt.Errorf("container %s did not become healthy within %v", name, timeout)
		}
	}
}

func generateToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)[:n+10]
}

func splitLines(s string) []string {
	var lines []string
	for _, l := range append([]string{}, split(s)...) {
		l = trim(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func split(s string) []string {
	return stringsSplit(s, "\n")
}

func stringsSplit(s, sep string) []string {
	result := []string{}
	for {
		idx := indexOf(s, rune(sep[0]))
		if idx < 0 {
			result = append(result, s)
			break
		}
		result = append(result, s[:idx])
		s = s[idx+1:]
	}
	return result
}

func indexOf(s string, c rune) int {
	for i, r := range s {
		if r == c {
			return i
		}
	}
	return -1
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\r' || s[0] == '\n') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}

func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

const defaultEnforcerPolicy = `version: 1
name: agency-standard-agent
rules:
  files:
    - name: allow-workspace
      paths: ["/workspace/**"]
      operations: [read, open, stat, list, write, create, mkdir, chmod, rename]
      decision: allow
    - name: allow-tmp
      paths: ["/tmp/**"]
      operations: [read, open, stat, list, write, create, mkdir, delete]
      decision: allow
    - name: allow-system-read
      paths: ["/usr/**", "/lib/**", "/etc/**"]
      operations: [read, open, stat, list]
      decision: allow
    - name: deny-credentials
      paths: ["**/.env", "**/*.key", "**/*.pem", "**/secrets/**"]
      operations: [read, open, write, create, delete]
      decision: deny
    - name: default-deny-files
      paths: ["**"]
      operations: [read, open, stat, list, write, create, delete, mkdir, chmod, rename]
      decision: deny
  commands:
    - name: allow-standard-tools
      commands: [python, python3, node, npm, pip, git, ls, cat, head, tail, grep, find, jq, sh]
      decision: allow
    - name: block-dangerous
      commands: [rm, shutdown, reboot, kill, pkill, dd, mkfs]
      decision: deny
  network:
    - name: allow-proxy
      domains: [egress]
      ports: [3128]
      decision: allow
    - name: default-deny-network
      domains: ["*"]
      decision: deny
`

const defaultEnforcerConfig = `server:
  listen: 0.0.0.0:18080
auth:
  type: api_key
  api_key:
    keys_file: /agency/enforcer/auth/api_keys.yaml
policy:
  path: /agency/enforcer/policy
audit:
  path: /agency/enforcer/audit
  format: json
  flush_interval: 5s
  storage:
    sqlite_path: /agency/enforcer/data/events.db
session:
  workspace: /workspace
  base_dir: /agency/enforcer/data/sessions
`
