package meeseeksruntime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"log/slog"

	agentruntime "github.com/geoffbelknap/agency/internal/hostadapter/agentruntime"
	"github.com/geoffbelknap/agency/internal/hostadapter/imageops"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/orchestrate/containers"
)

const (
	prefix        = "agency"
	enforcerImage = "agency-enforcer:latest"
	bodyImage     = "agency-body:latest"
	agencyUID     = "61000"
	agencyGID     = "61000"
)

type MeeseeksStartSequence struct {
	Meeseeks  *models.Meeseeks
	Home      string
	Version   string
	SourceDir string
	BuildID   string
	Container *runtimehost.Client
	Log       *slog.Logger

	ParentConstraintsPath string

	cli            *runtimehost.RawClient
	networkCreated bool
	enforcerID     string
	workspaceID    string
}

func (ms *MeeseeksStartSequence) Run(ctx context.Context) error {
	if ms.Container != nil {
		ms.cli = ms.Container.RawClient()
	} else {
		var err error
		ms.cli, err = runtimehost.NewRawClient()
		if err != nil {
			return fmt.Errorf("runtime backend client: %w", err)
		}
	}

	ms.Log.Info("meeseeks phase 1/5: validate", "id", ms.Meeseeks.ID)
	if err := ms.phase1Validate(); err != nil {
		return fmt.Errorf("phase 1 (validate): %w", err)
	}

	ms.Log.Info("meeseeks phase 2/5: enforcement", "id", ms.Meeseeks.ID)
	if err := ms.phase2Enforcement(ctx); err != nil {
		ms.failClosed(ctx)
		return fmt.Errorf("phase 2 (enforcement): %w", err)
	}

	ms.Log.Info("meeseeks phase 3/5: constraints", "id", ms.Meeseeks.ID)
	if err := ms.phase3Constraints(); err != nil {
		ms.failClosed(ctx)
		return fmt.Errorf("phase 3 (constraints): %w", err)
	}

	ms.Log.Info("meeseeks phase 4/5: workspace", "id", ms.Meeseeks.ID)
	if err := ms.phase4Workspace(ctx); err != nil {
		ms.failClosed(ctx)
		return fmt.Errorf("phase 4 (workspace): %w", err)
	}

	ms.Log.Info("meeseeks phase 5/5: body started", "id", ms.Meeseeks.ID)
	return nil
}

func (ms *MeeseeksStartSequence) phase1Validate() error {
	if ms.Meeseeks.Task == "" {
		return fmt.Errorf("task is required")
	}
	auditDir := filepath.Join(ms.Home, "audit", ms.Meeseeks.ID)
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		return fmt.Errorf("create audit dir: %w", err)
	}
	enforcerAuditDir := filepath.Join(auditDir, "enforcer")
	if err := os.MkdirAll(enforcerAuditDir, 0o777); err != nil {
		return fmt.Errorf("create enforcer audit dir: %w", err)
	}
	return nil
}

func (ms *MeeseeksStartSequence) phase2Enforcement(ctx context.Context) error {
	netName := ms.Meeseeks.NetworkName
	if err := containers.CreateInternalNetwork(ctx, ms.cli, netName, map[string]string{
		"agency.type":     "meeseeks-internal",
		"agency.meeseeks": ms.Meeseeks.ID,
		"agency.parent":   ms.Meeseeks.ParentAgent,
	}); err != nil {
		return fmt.Errorf("create network %s: %w", netName, err)
	}
	ms.networkCreated = true

	if err := imageops.Resolve(ctx, ms.cli, "enforcer", ms.Version, ms.SourceDir, ms.BuildID, ms.Log); err != nil {
		return fmt.Errorf("resolve enforcer image: %w", err)
	}

	auditDir := filepath.Join(ms.Home, "audit", ms.Meeseeks.ID, "enforcer")
	dataDir := filepath.Join(ms.Home, "infrastructure", "enforcer", "data", ms.Meeseeks.ID)
	_ = os.MkdirAll(dataDir, 0o777)
	policyDir := filepath.Join(ms.Home, "infrastructure", "enforcer", "policy")
	_ = os.MkdirAll(policyDir, 0o755)
	configDir := filepath.Join(ms.Home, "infrastructure", "enforcer", "config")
	_ = os.MkdirAll(configDir, 0o755)
	servicesDir := filepath.Join(ms.Home, "services")
	_ = os.MkdirAll(servicesDir, 0o755)

	env := []string{
		"HOME=/agency/enforcer/data",
		"AGENT_NAME=" + ms.Meeseeks.ID,
		"AGENCY_ENFORCER_PROXY_URL=http://" + meeseeksEnforcerHost(ms.Meeseeks.ID, ms.cli.Backend()) + ":3128",
		"AGENCY_ENFORCER_CONTROL_URL=http://" + meeseeksEnforcerHost(ms.Meeseeks.ID, ms.cli.Backend()) + ":8081",
		"AGENCY_ENFORCER_HEALTH_URL=http://" + meeseeksEnforcerHost(ms.Meeseeks.ID, ms.cli.Backend()) + ":3128/health",
		"CONSTRAINT_WS_PORT=8081",
		fmt.Sprintf("AGENCY_MEESEEKS_BUDGET=%.4f", ms.Meeseeks.Budget),
		"AGENCY_MEESEEKS=true",
	}
	binds := []string{
		policyDir + ":/agency/enforcer/policy:ro",
		dataDir + ":/agency/enforcer/data:rw",
		auditDir + ":/agency/enforcer/audit:rw",
		servicesDir + ":/agency/enforcer/services:ro",
	}

	routingYAML := filepath.Join(ms.Home, "infrastructure", "routing.yaml")
	if fileExists(routingYAML) {
		binds = append(binds, routingYAML+":/agency/enforcer/routing.yaml:ro")
	}
	serverConfig := filepath.Join(configDir, "server-config.yaml")
	if fileExists(serverConfig) {
		binds = append(binds, serverConfig+":/agency/enforcer/server-config.yaml:ro")
	}
	egressCA := filepath.Join(ms.Home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	if fileExists(egressCA) {
		binds = append(binds, egressCA+":/etc/ssl/certs/agency-egress-ca.pem:ro")
		env = append(env, "SSL_CERT_FILE=/etc/ssl/certs/agency-egress-ca.pem")
	}

	hc := containers.HostConfigDefaults(containers.RoleEnforcer)
	hc.Binds = binds
	hc.NetworkMode = container.NetworkMode(netName)
	hc.Tmpfs = map[string]string{"/tmp": "size=64M", "/run": "size=32M"}
	netCfg := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{netName: {}}}
	if usesCreateTimeMediationNetworks(ms.cli.Backend()) {
		netCfg.EndpointsConfig[gatewayNetName()] = &network.EndpointSettings{}
		netCfg.EndpointsConfig[egressIntNetName()] = &network.EndpointSettings{}
	}

	enforcerContainerID, err := containers.CreateAndStart(ctx, ms.cli,
		ms.Meeseeks.EnforcerName,
		&container.Config{
			Image:    enforcerImage,
			Hostname: "enforcer",
			Env:      env,
			Labels: map[string]string{
				"agency.type":     "meeseeks-enforcer",
				"agency.meeseeks": ms.Meeseeks.ID,
				"agency.parent":   ms.Meeseeks.ParentAgent,
			},
			Healthcheck: &container.HealthConfig{
				Test:        []string{"CMD-SHELL", "curl -sf http://127.0.0.1:3128/health || exit 1"},
				Interval:    2 * time.Second,
				Timeout:     3 * time.Second,
				Retries:     5,
				StartPeriod: 3 * time.Second,
			},
		},
		hc,
		netCfg,
	)
	if err != nil {
		return fmt.Errorf("create/start enforcer container: %w", err)
	}
	ms.enforcerID = enforcerContainerID

	if err := agentruntime.WaitContainerRunning(ctx, ms.cli, ms.Meeseeks.EnforcerName, 10*time.Second); err != nil {
		return err
	}

	if !usesCreateTimeMediationNetworks(ms.cli.Backend()) {
		_ = ms.cli.NetworkConnect(ctx, gatewayNetName(), enforcerContainerID, &network.EndpointSettings{
			Aliases: []string{"enforcer"},
		})
		_ = ms.cli.NetworkConnect(ctx, egressIntNetName(), enforcerContainerID, &network.EndpointSettings{
			Aliases: []string{"enforcer"},
		})
	}
	return nil
}

func (ms *MeeseeksStartSequence) phase3Constraints() error {
	if ms.ParentConstraintsPath == "" {
		ms.ParentConstraintsPath = filepath.Join(ms.Home, "agents", ms.Meeseeks.ParentAgent, "constraints.yaml")
	}
	if !fileExists(ms.ParentConstraintsPath) {
		return fmt.Errorf("parent constraints not found: %s", ms.ParentConstraintsPath)
	}
	return nil
}

func (ms *MeeseeksStartSequence) phase4Workspace(ctx context.Context) error {
	if err := imageops.Resolve(ctx, ms.cli, "body", ms.Version, ms.SourceDir, ms.BuildID, ms.Log); err != nil {
		return fmt.Errorf("resolve body image: %w", err)
	}

	netName := ms.Meeseeks.NetworkName
	enforcerHostName := meeseeksEnforcerHost(ms.Meeseeks.ID, ms.cli.Backend())
	auditDir := filepath.Join(ms.Home, "audit", ms.Meeseeks.ID)
	env := []string{
		"AGENCY_ENFORCER_PROXY_URL=http://" + enforcerHostName + ":3128",
		"AGENCY_ENFORCER_CONTROL_URL=http://" + enforcerHostName + ":8081",
		"AGENCY_ENFORCER_HEALTH_URL=http://" + enforcerHostName + ":3128/health",
		"AGENCY_ENFORCER_URL=http://" + enforcerHostName + ":3128/v1",
		"HTTP_PROXY=http://" + enforcerHostName + ":3128",
		"HTTPS_PROXY=http://" + enforcerHostName + ":3128",
		"AGENCY_COMMS_URL=http://" + enforcerHostName + ":8081/mediation/comms",
		"AGENCY_KNOWLEDGE_URL=http://" + enforcerHostName + ":8081/mediation/knowledge",
		"NO_PROXY=" + enforcerHostName + ",localhost,127.0.0.1",
		"AGENCY_AGENT_NAME=" + ms.Meeseeks.ID,
		"AGENCY_MODEL=" + ms.Meeseeks.Model,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"AGENCY_MEESEEKS=true",
		"AGENCY_MEESEEKS_ID=" + ms.Meeseeks.ID,
		"AGENCY_MEESEEKS_TASK=" + ms.Meeseeks.Task,
		"AGENCY_MEESEEKS_PARENT=" + ms.Meeseeks.ParentAgent,
		fmt.Sprintf("AGENCY_MEESEEKS_BUDGET=%.4f", ms.Meeseeks.Budget),
		"AGENCY_MEESEEKS_CHANNEL=" + ms.Meeseeks.Channel,
	}

	egressCA := filepath.Join(ms.Home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	caBundle := filepath.Join(ms.Home, "infrastructure", "egress", "certs", "ca-bundle.pem")
	if fileExists(caBundle) {
		env = append(env, "SSL_CERT_FILE=/etc/ssl/certs/agency-ca-bundle.pem", "REQUESTS_CA_BUNDLE=/etc/ssl/certs/agency-ca-bundle.pem")
	}
	if fileExists(egressCA) {
		env = append(env, "NODE_EXTRA_CA_CERTS=/etc/ssl/certs/agency-egress-ca.pem")
	}

	binds := []string{
		ms.ParentConstraintsPath + ":/agency/constraints.yaml:ro",
		auditDir + ":/var/lib/agency/audit:ro",
	}
	if fileExists(egressCA) {
		binds = append(binds, egressCA+":/etc/ssl/certs/agency-egress-ca.pem:ro")
	}
	if fileExists(caBundle) {
		binds = append(binds, caBundle+":/etc/ssl/certs/agency-ca-bundle.pem:ro")
	}

	hc := containers.HostConfigDefaults(containers.RoleMeeseeks)
	hc.Binds = binds
	hc.NetworkMode = container.NetworkMode(netName)
	hc.Tmpfs = map[string]string{"/tmp": "size=256M", "/run": "size=64M"}

	workspaceContainerID, err := containers.CreateAndStart(ctx, ms.cli,
		ms.Meeseeks.ContainerName,
		&container.Config{
			Image:    bodyImage,
			Hostname: "workspace",
			User:     agencyUID + ":" + agencyGID,
			Env:      env,
			Labels: map[string]string{
				"agency.type":     "meeseeks-workspace",
				"agency.meeseeks": ms.Meeseeks.ID,
				"agency.parent":   ms.Meeseeks.ParentAgent,
			},
		},
		hc,
		nil,
	)
	if err != nil {
		return fmt.Errorf("create/start workspace container: %w", err)
	}
	ms.workspaceID = workspaceContainerID

	ms.Log.Info("meeseeks workspace started", "id", ms.Meeseeks.ID, "container", ms.Meeseeks.ContainerName, "task", ms.Meeseeks.Task)
	return nil
}

func (ms *MeeseeksStartSequence) failClosed(ctx context.Context) {
	ms.Log.Warn("meeseeks fail-closed teardown", "id", ms.Meeseeks.ID)
	if ms.workspaceID != "" {
		_ = containers.StopAndRemove(ctx, ms.cli, ms.Meeseeks.ContainerName, 5)
	}
	if ms.enforcerID != "" {
		_ = containers.StopAndRemove(ctx, ms.cli, ms.Meeseeks.EnforcerName, 5)
	}
	if ms.networkCreated {
		_ = ms.cli.NetworkRemove(ctx, ms.Meeseeks.NetworkName)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func gatewayNetName() string {
	instance := strings.TrimSpace(strings.ToLower(os.Getenv("AGENCY_INFRA_INSTANCE")))
	if instance == "" {
		return "agency-gateway"
	}
	instance = strings.NewReplacer("_", "-", ".", "-", "/", "-").Replace(instance)
	instance = strings.Trim(instance, "-")
	return "agency-gateway-" + instance
}

func egressIntNetName() string {
	instance := strings.TrimSpace(strings.ToLower(os.Getenv("AGENCY_INFRA_INSTANCE")))
	if instance == "" {
		return "agency-egress-int"
	}
	instance = strings.NewReplacer("_", "-", ".", "-", "/", "-").Replace(instance)
	instance = strings.Trim(instance, "-")
	return "agency-egress-int-" + instance
}

func meeseeksEnforcerHost(id, backend string) string {
	if runtimehost.NormalizeContainerBackend(backend) == runtimehost.BackendContainerd {
		return fmt.Sprintf("%s-%s-enforcer", prefix, id)
	}
	return "enforcer"
}

func usesCreateTimeMediationNetworks(backend string) bool {
	return runtimehost.RequiresCreateTimeNetworkTopology(backend)
}
