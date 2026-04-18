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
	"gopkg.in/yaml.v3"
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

	policyDir := e.ensurePolicy()
	configDir := e.ensureConfig(rotateKey)
	dataDir := filepath.Join(e.Home, "infrastructure", "enforcer", "data", e.AgentName)
	auditDir := filepath.Join(e.Home, "audit", e.AgentName, "enforcer")
	agentDir := filepath.Join(e.Home, "agents", e.AgentName)
	deploymentsDir := filepath.Join(e.Home, "deployments")
	servicesDir := filepath.Join(e.Home, "services")
	for _, dir := range []string{dataDir, auditDir, agentDir, deploymentsDir, servicesDir} {
		_ = os.MkdirAll(dir, 0o777)
	}

	internalNet := fmt.Sprintf("%s-%s-internal", prefix, e.AgentName)
	commsHost := scopedInfraName(fmt.Sprintf("%s-infra-comms", prefix))
	knowledgeHost := scopedInfraName(fmt.Sprintf("%s-infra-knowledge", prefix))
	webFetchHost := scopedInfraName(fmt.Sprintf("%s-infra-web-fetch", prefix))
	gatewayHostName := gatewayHost(e.cli.Backend())

	env := map[string]string{
		"HOME":               "/agency/enforcer/data",
		"AGENT_NAME":         e.AgentName,
		"CONSTRAINT_WS_PORT": "8081",
		"GATEWAY_URL":        "http://" + gatewayHostName + ":8200",
		"COMMS_URL":          "http://" + commsHost + ":8080",
		"KNOWLEDGE_URL":      "http://" + knowledgeHost + ":8080",
		"WEB_FETCH_URL":      "http://" + webFetchHost + ":8080",
		"AGENCY_CALLER":      "enforcer",
	}
	if e.LifecycleID != "" {
		env["AGENCY_LIFECYCLE_ID"] = e.LifecycleID
	}
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
				env["GATEWAY_URL"] = "http://" + gatewayHostName + ":8200"
			}
		}
	}

	egressCA := filepath.Join(e.Home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	if fileExists(egressCA) {
		env["SSL_CERT_FILE"] = "/etc/ssl/certs/agency-egress-ca.pem"
	}

	perAgentAuthDir := filepath.Join(e.Home, "agents", e.AgentName, "state", "enforcer-auth")
	binds := []string{
		policyDir + ":/agency/enforcer/policy:ro",
		filepath.Join(configDir, "server-config.yaml") + ":/agency/enforcer/server-config.yaml:ro",
		perAgentAuthDir + ":/agency/enforcer/auth:ro",
		dataDir + ":/agency/enforcer/data:rw",
		auditDir + ":/agency/enforcer/audit:rw",
		agentDir + ":/agency/agent:ro",
		deploymentsDir + ":/agency/deployments:ro",
		servicesDir + ":/agency/enforcer/services:ro",
	}

	routingYAML := filepath.Join(e.Home, "infrastructure", "routing.yaml")
	if fileExists(routingYAML) {
		binds = append(binds, routingYAML+":/agency/enforcer/routing.yaml:ro")
	}
	if fileExists(egressCA) {
		binds = append(binds, egressCA+":/etc/ssl/certs/agency-egress-ca.pem:ro")
	}
	memoryDir := filepath.Join(agentDir, "memory")
	if _, err := os.Stat(memoryDir); err == nil {
		binds = append(binds, memoryDir+":/agency/memory:ro")
	}

	hostConfig := containers.HostConfigDefaults(containers.RoleEnforcer)
	hostConfig.Binds = binds
	hostConfig.NetworkMode = container.NetworkMode(internalNet)
	hostConfig.Tmpfs = map[string]string{"/tmp": "size=64M", "/run": "size=32M"}
	hostPort := e.ConstraintHostPort
	if hostPort == "" {
		var err error
		hostPort, err = pickLoopbackPort()
		if err != nil {
			return "", fmt.Errorf("allocate enforcer constraint port: %w", err)
		}
	}
	hostConfig.PortBindings = nat.PortMap{
		"8081/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: hostPort}},
	}

	logFormat := os.Getenv("AGENCY_LOG_FORMAT")
	if logFormat == "" {
		logFormat = "json"
	}
	env["AGENCY_LOG_FORMAT"] = logFormat
	env["AGENCY_COMPONENT"] = "enforcer"
	if env["BUILD_ID"] == "" {
		env["BUILD_ID"] = e.BuildID
	}

	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			internalNet: {
				Aliases: []string{"enforcer"},
			},
		},
	}
	if e.cli.Backend() == runtimehost.BackendContainerd {
		netCfg.EndpointsConfig[gatewayNetName()] = &network.EndpointSettings{}
		netCfg.EndpointsConfig[egressIntNetName()] = &network.EndpointSettings{}
	}

	enforcerContainerID, err := containers.CreateAndStart(ctx, e.cli,
		e.ContainerName,
		&container.Config{
			Image:    enforcerImage,
			Hostname: "enforcer",
			Env:      mapToEnv(env),
			ExposedPorts: nat.PortSet{
				"8081/tcp": struct{}{},
			},
			Labels: map[string]string{
				services.LabelServiceEnabled:         "true",
				services.LabelServiceName:            e.AgentName + "/enforcer",
				services.LabelServicePort:            "3128",
				services.LabelServiceHealth:          "/health",
				services.LabelServiceNetwork:         internalNet,
				services.LabelServiceHMAC:            services.GenerateHMAC(e.ContainerName, e.hmacKey),
				"agency.managed":                     "true",
				"agency.agent":                       e.AgentName,
				"agency.type":                        "enforcer",
				"agency.constraint.port":             "8081",
				"agency.constraint.ws.path":          "/ws",
				"agency.constraint.rest.constraints": "/constraints",
				"agency.constraint.rest.ack":         "/constraints/ack",
				"agency.principal.uuid":              e.PrincipalUUID,
				"agency.build.id":                    imageops.ImageBuildLabel(ctx, e.cli, enforcerImage),
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
		hostConfig,
		netCfg,
	)
	if err != nil {
		return "", fmt.Errorf("create/start enforcer: %w", err)
	}

	if err := waitContainerRunning(ctx, e.cli, e.ContainerName, 10*time.Second); err != nil {
		return "", err
	}
	if e.cli.Backend() != runtimehost.BackendContainerd {
		_ = e.cli.NetworkConnect(ctx, gatewayNetName(), enforcerContainerID, &network.EndpointSettings{
			Aliases: []string{"enforcer"},
		})
		_ = e.cli.NetworkConnect(ctx, egressIntNetName(), enforcerContainerID, &network.EndpointSettings{
			Aliases: []string{"enforcer"},
		})
		if err := waitContainerNetworks(ctx, e.cli, e.ContainerName, []string{
			internalNet,
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
		e.log.Info("scoped API key generated", "agent", e.AgentName, "rotated", rotateKey)
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
