package agentruntime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/pkg/pathsafety"
)

const (
	EnforcerProxyPort      = "3128"
	EnforcerConstraintPort = "8081"
)

type EnforcerLaunchSpec struct {
	AgentName          string
	ComponentName      string
	Image              string
	Hostname           string
	InternalNetwork    string
	ProxyHostPort      string
	ConstraintHostPort string
	ConstraintPort     string
	ProxyPort          string
	Env                map[string]string
	Mounts             []EnforcerMount
	PrincipalUUID      string
	BuildID            string
}

type EnforcerMount struct {
	HostPath  string
	GuestPath string
	Mode      string
}

func (e *Enforcer) BuildLaunchSpec(ctx context.Context, rotateKey bool) (EnforcerLaunchSpec, error) {
	_ = ctx
	agentName, err := pathsafety.Segment("agent name", e.AgentName)
	if err != nil {
		return EnforcerLaunchSpec{}, err
	}
	policyDir := e.ensurePolicy()
	configDir := e.ensureConfig(rotateKey)
	dataDir, err := pathsafety.Join(e.Home, "infrastructure", "enforcer", "data", agentName)
	if err != nil {
		return EnforcerLaunchSpec{}, err
	}
	auditDir, err := pathsafety.Join(e.Home, "audit", agentName, "enforcer")
	if err != nil {
		return EnforcerLaunchSpec{}, err
	}
	agentDir, err := pathsafety.Join(e.Home, "agents", agentName)
	if err != nil {
		return EnforcerLaunchSpec{}, err
	}
	deploymentsDir := filepath.Join(e.Home, "deployments")
	servicesDir := filepath.Join(e.Home, "services")
	for _, dir := range []string{dataDir, auditDir, agentDir, deploymentsDir, servicesDir} {
		_ = os.MkdirAll(dir, 0o777)
	}

	backend := e.backendName()
	internalNet := fmt.Sprintf("%s-%s-internal", prefix, agentName)
	env := map[string]string{
		"HOME":               "/agency/enforcer/data",
		"AGENT_NAME":         agentName,
		"CONSTRAINT_WS_PORT": EnforcerConstraintPort,
		"GATEWAY_URL":        "http://" + gatewayHost(backend) + ":8200",
		"COMMS_URL":          "http://" + scopedInfraName(fmt.Sprintf("%s-infra-comms", prefix)) + ":8080",
		"KNOWLEDGE_URL":      "http://" + scopedInfraName(fmt.Sprintf("%s-infra-knowledge", prefix)) + ":8080",
		"WEB_FETCH_URL":      "http://" + scopedInfraName(fmt.Sprintf("%s-infra-web-fetch", prefix)) + ":8080",
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
				env["GATEWAY_URL"] = "http://" + gatewayHost(backend) + ":8200"
			}
		}
	}

	egressCA := filepath.Join(e.Home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	if fileExists(egressCA) {
		env["SSL_CERT_FILE"] = "/etc/ssl/certs/agency-egress-ca.pem"
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

	proxyHostPort := e.ProxyHostPort
	if proxyHostPort == "" {
		var err error
		proxyHostPort, err = pickLoopbackPort()
		if err != nil {
			return EnforcerLaunchSpec{}, fmt.Errorf("allocate enforcer proxy port: %w", err)
		}
	}

	constraintHostPort := e.ConstraintHostPort
	if constraintHostPort == "" {
		var err error
		constraintHostPort, err = pickLoopbackPort()
		if err != nil {
			return EnforcerLaunchSpec{}, fmt.Errorf("allocate enforcer constraint port: %w", err)
		}
	}

	perAgentAuthDir, err := pathsafety.Join(e.Home, "agents", agentName, "state", "enforcer-auth")
	if err != nil {
		return EnforcerLaunchSpec{}, err
	}
	mounts := []EnforcerMount{
		{HostPath: policyDir, GuestPath: "/agency/enforcer/policy", Mode: "ro"},
		{HostPath: filepath.Join(configDir, "server-config.yaml"), GuestPath: "/agency/enforcer/server-config.yaml", Mode: "ro"},
		{HostPath: perAgentAuthDir, GuestPath: "/agency/enforcer/auth", Mode: "ro"},
		{HostPath: dataDir, GuestPath: "/agency/enforcer/data", Mode: "rw"},
		{HostPath: auditDir, GuestPath: "/agency/enforcer/audit", Mode: "rw"},
		{HostPath: agentDir, GuestPath: "/agency/agent", Mode: "ro"},
		{HostPath: deploymentsDir, GuestPath: "/agency/deployments", Mode: "ro"},
		{HostPath: servicesDir, GuestPath: "/agency/enforcer/services", Mode: "ro"},
	}
	routingYAML := filepath.Join(e.Home, "infrastructure", "routing.yaml")
	if fileExists(routingYAML) {
		mounts = append(mounts, EnforcerMount{HostPath: routingYAML, GuestPath: "/agency/enforcer/routing.yaml", Mode: "ro"})
	}
	if fileExists(egressCA) {
		mounts = append(mounts, EnforcerMount{HostPath: egressCA, GuestPath: "/etc/ssl/certs/agency-egress-ca.pem", Mode: "ro"})
	}
	memoryDir := filepath.Join(agentDir, "memory")
	if _, err := os.Stat(memoryDir); err == nil {
		mounts = append(mounts, EnforcerMount{HostPath: memoryDir, GuestPath: "/agency/memory", Mode: "ro"})
	}

	return EnforcerLaunchSpec{
		AgentName:          agentName,
		ComponentName:      e.ContainerName,
		Image:              enforcerImage,
		Hostname:           "enforcer",
		InternalNetwork:    internalNet,
		ProxyHostPort:      proxyHostPort,
		ConstraintHostPort: constraintHostPort,
		ConstraintPort:     EnforcerConstraintPort,
		ProxyPort:          EnforcerProxyPort,
		Env:                env,
		Mounts:             mounts,
		PrincipalUUID:      e.PrincipalUUID,
		BuildID:            e.BuildID,
	}, nil
}

func (s EnforcerLaunchSpec) ContainerBinds() []string {
	binds := make([]string, 0, len(s.Mounts))
	for _, mount := range s.Mounts {
		binds = append(binds, mount.HostPath+":"+mount.GuestPath+":"+mount.Mode)
	}
	return binds
}

func (s EnforcerLaunchSpec) HostProcessEnv(serviceURLs map[string]string) map[string]string {
	env := make(map[string]string, len(s.Env)+12)
	for key, value := range s.Env {
		env[key] = value
	}
	env["ENFORCER_PORT"] = s.ProxyHostPort
	env["CONSTRAINT_WS_PORT"] = s.ConstraintHostPort
	env["ENFORCER_BIND_ADDR"] = "127.0.0.1"
	env["CONSTRAINT_WS_BIND_ADDR"] = "127.0.0.1"
	for service, url := range serviceURLs {
		switch service {
		case "gateway":
			env["GATEWAY_URL"] = url
		case "comms":
			env["COMMS_URL"] = url
		case "knowledge":
			env["KNOWLEDGE_URL"] = url
		case "web-fetch":
			env["WEB_FETCH_URL"] = url
		case "egress":
			env["EGRESS_PROXY"] = url
		}
	}
	for _, mount := range s.Mounts {
		switch mount.GuestPath {
		case "/agency/enforcer/auth":
			env["API_KEYS_FILE"] = filepath.Join(mount.HostPath, "api_keys.yaml")
		case "/agency/enforcer/audit":
			env["ENFORCER_LOG_DIR"] = mount.HostPath
		case "/agency/enforcer/data":
			env["HOME"] = mount.HostPath
		case "/agency/enforcer/routing.yaml":
			env["ROUTING_CONFIG"] = mount.HostPath
		case "/agency/enforcer/services":
			env["SERVICES_DIR"] = mount.HostPath
		case "/agency/agent":
			env["AGENT_DIR"] = mount.HostPath
			domainsFile := filepath.Join(mount.HostPath, "egress-domains.yaml")
			if fileExists(domainsFile) {
				env["EGRESS_DOMAINS_FILE"] = domainsFile
			}
		case "/etc/ssl/certs/agency-egress-ca.pem":
			env["SSL_CERT_FILE"] = mount.HostPath
		}
	}
	return env
}

func (e *Enforcer) backendName() string {
	if e.cli == nil {
		return runtimehost.BackendDocker
	}
	return e.cli.Backend()
}
