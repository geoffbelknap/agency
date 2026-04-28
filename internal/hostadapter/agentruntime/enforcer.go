package agentruntime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"log/slog"

	"github.com/geoffbelknap/agency/internal/hostadapter/imageops"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/orchestrate/containers"
	"github.com/geoffbelknap/agency/internal/services"
)

const enforcerImage = "agency-enforcer:latest"

type Enforcer struct {
	AgentName          string
	ContainerName      string
	Home               string
	Version            string
	SourceDir          string
	BuildID            string
	ProxyHostPort      string
	ConstraintHostPort string
	LifecycleID        string
	PrincipalUUID      string
	cli                *runtimehost.RawClient
	log                *slog.Logger
	hmacKey            []byte
}

func NewEnforcer(agentName, home, version string, logger *slog.Logger, hmacKey []byte) (*Enforcer, error) {
	return NewEnforcerWithClient(agentName, home, version, logger, hmacKey, nil)
}

func NewEnforcerWithClient(agentName, home, version string, logger *slog.Logger, hmacKey []byte, cli *runtimehost.RawClient) (*Enforcer, error) {
	if cli == nil {
		var err error
		cli, err = runtimehost.NewRawClient()
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

func (e *Enforcer) Start(ctx context.Context) (string, error) {
	return e.start(ctx, false)
}

func (e *Enforcer) StartWithKeyRotation(ctx context.Context) (string, error) {
	return e.start(ctx, true)
}

func (e *Enforcer) start(ctx context.Context, rotateKey bool) (string, error) {
	if err := imageops.Resolve(ctx, e.cli, "enforcer", e.Version, e.SourceDir, e.BuildID, e.log); err != nil {
		return "", fmt.Errorf("resolve enforcer image: %w", err)
	}

	if err := containers.StopAndRemove(ctx, e.cli, e.ContainerName, 5); err != nil {
		return "", fmt.Errorf("remove previous enforcer container: %w", err)
	}

	spec, err := e.BuildLaunchSpec(ctx, rotateKey)
	if err != nil {
		return "", err
	}

	hostConfig := containers.HostConfigDefaults(containers.RoleEnforcer)
	hostConfig.Binds = spec.ContainerBinds()
	hostConfig.NetworkMode = container.NetworkMode(spec.InternalNetwork)
	hostConfig.Tmpfs = map[string]string{"/tmp": "size=64M", "/run": "size=32M"}
	hostConfig.PortBindings = nat.PortMap{
		nat.Port(spec.ConstraintPort + "/tcp"): []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: spec.ConstraintHostPort}},
	}

	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			spec.InternalNetwork: {
				Aliases: []string{"enforcer"},
			},
		},
	}
	if usesCreateTimeMediationNetworks(e.cli.Backend()) {
		netCfg.EndpointsConfig[gatewayNetName()] = &network.EndpointSettings{}
		netCfg.EndpointsConfig[egressIntNetName()] = &network.EndpointSettings{}
	}

	enforcerContainerID, err := containers.CreateAndStart(ctx, e.cli,
		e.ContainerName,
		&container.Config{
			Image:    spec.Image,
			Hostname: spec.Hostname,
			Env:      mapToEnv(spec.Env),
			ExposedPorts: nat.PortSet{
				nat.Port(spec.ConstraintPort + "/tcp"): struct{}{},
			},
			Labels: map[string]string{
				services.LabelServiceEnabled:         "true",
				services.LabelServiceName:            e.AgentName + "/enforcer",
				services.LabelServicePort:            spec.ProxyPort,
				services.LabelServiceHealth:          "/health",
				services.LabelServiceNetwork:         spec.InternalNetwork,
				services.LabelServiceHMAC:            services.GenerateHMAC(e.ContainerName, e.hmacKey),
				"agency.managed":                     "true",
				"agency.agent":                       e.AgentName,
				"agency.type":                        "enforcer",
				"agency.constraint.port":             spec.ConstraintPort,
				"agency.constraint.ws.path":          "/ws",
				"agency.constraint.rest.constraints": "/constraints",
				"agency.constraint.rest.ack":         "/constraints/ack",
				"agency.principal.uuid":              spec.PrincipalUUID,
				"agency.build.id":                    imageops.ImageBuildLabel(ctx, e.cli, spec.Image),
				"agency.build.gateway":               spec.BuildID,
			},
			Healthcheck: &container.HealthConfig{
				Test:        []string{"CMD-SHELL", "curl -sf http://127.0.0.1:3128/health || exit 1"},
				Interval:    2 * time.Second,
				Timeout:     3 * time.Second,
				Retries:     5,
				StartPeriod: 3 * time.Second,
			},
		},
		hostConfig,
		netCfg,
	)
	if err != nil {
		return "", fmt.Errorf("create/start enforcer: %w", err)
	}

	if err := waitContainerRunning(ctx, e.cli, e.ContainerName, 10*time.Second); err != nil {
		return "", err
	}
	if !usesCreateTimeMediationNetworks(e.cli.Backend()) {
		_ = e.cli.NetworkConnect(ctx, gatewayNetName(), enforcerContainerID, &network.EndpointSettings{
			Aliases: []string{"enforcer"},
		})
		_ = e.cli.NetworkConnect(ctx, egressIntNetName(), enforcerContainerID, &network.EndpointSettings{
			Aliases: []string{"enforcer"},
		})
	}
	if e.cli.Backend() != runtimehost.BackendContainerd {
		if err := waitContainerNetworks(ctx, e.cli, e.ContainerName, []string{
			spec.InternalNetwork,
			gatewayNetName(),
			egressIntNetName(),
		}, 5*time.Second); err != nil {
			return "", err
		}
	}

	return e.readAPIKey(), nil
}

func (e *Enforcer) HealthCheck(ctx context.Context, timeout time.Duration) error {
	return waitContainerHealthy(ctx, e.cli, e.ContainerName, timeout)
}

func (e *Enforcer) Stop(ctx context.Context) {
	timeout := 10
	_ = e.cli.ContainerStop(ctx, e.ContainerName, container.StopOptions{Timeout: &timeout})
	_ = e.cli.ContainerRemove(ctx, e.ContainerName, container.RemoveOptions{Force: true})
}

func (e *Enforcer) Pause(ctx context.Context) {
	_ = e.cli.ContainerPause(ctx, e.ContainerName)
}

func (e *Enforcer) Unpause(ctx context.Context) {
	_ = e.cli.ContainerUnpause(ctx, e.ContainerName)
}

func (e *Enforcer) readAPIKey() string {
	keysFile := filepath.Join(e.Home, "agents", e.AgentName, "state", "enforcer-auth", "api_keys.yaml")
	data, err := os.ReadFile(keysFile)
	if err != nil {
		return ""
	}
	for _, line := range splitLines(string(data)) {
		if len(line) > 7 && line[:7] == "- key: " {
			return trimQuotes(line[7:])
		}
	}
	return ""
}

func (e *Enforcer) ensurePolicy() string {
	dir := filepath.Join(e.Home, "infrastructure", "enforcer", "policy")
	_ = os.MkdirAll(dir, 0o755)
	policyFile := filepath.Join(dir, "standard-agent.yaml")
	if !fileExists(policyFile) {
		_ = os.WriteFile(policyFile, []byte(defaultEnforcerPolicy), 0o644)
	}
	return dir
}

func (e *Enforcer) ensureConfig(rotateKey bool) string {
	dir := filepath.Join(e.Home, "infrastructure", "enforcer", "config")
	_ = os.MkdirAll(dir, 0o755)

	configFile := filepath.Join(dir, "server-config.yaml")
	if !fileExists(configFile) {
		_ = os.WriteFile(configFile, []byte(defaultEnforcerConfig), 0o644)
	}

	authDir := filepath.Join(e.Home, "agents", e.AgentName, "state", "enforcer-auth")
	_ = os.MkdirAll(authDir, 0o755)
	keysFile := filepath.Join(authDir, "api_keys.yaml")

	needNewKey := rotateKey || !fileExists(keysFile)
	if !needNewKey {
		existing := e.readAPIKey()
		if len(existing) < len("agency-scoped--") || existing[:len("agency-scoped--")] != "agency-scoped--" {
			needNewKey = true
		}
	}
	if needNewKey {
		key := "agency-scoped--" + generateToken(32)
		_ = os.WriteFile(keysFile, []byte(fmt.Sprintf("- key: \"%s\"\n  name: \"agency-workspace\"\n", key)), 0o644)
		if e.log != nil {
			e.log.Info("scoped API key generated", "agent", e.AgentName, "rotated", rotateKey)
		}
	}
	return dir
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
