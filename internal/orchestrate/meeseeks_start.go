package orchestrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"log/slog"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"

	agencyDocker "github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/images"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/orchestrate/containers"
)

// MeeseeksStartSequence orchestrates the abbreviated 5-phase Meeseeks startup.
// Skips identity (phase 5) and session (phase 7) from the regular 7-phase start.
type MeeseeksStartSequence struct {
	Meeseeks  *models.Meeseeks
	Home      string
	Version   string
	SourceDir string // agency_core/ path for dev-mode image builds
	BuildID   string
	Docker    *agencyDocker.Client
	Log       *slog.Logger

	// Parent's constraints file path (mounted read-only into Meeseeks)
	ParentConstraintsPath string

	// Internal state
	cli           *dockerclient.Client
	networkCreated bool
	enforcerID    string
	workspaceID   string
}

// Run executes the 5-phase abbreviated startup:
//  1. Validate — check task + tools
//  2. Enforcement — start enforcer sidecar
//  3. Constraints — mount parent's constraints read-only
//  4. Workspace — create workspace container with body image
//  5. Body — start body runtime with Meeseeks system prompt
func (ms *MeeseeksStartSequence) Run(ctx context.Context) error {
	if ms.Docker != nil {
		ms.cli = ms.Docker.RawClient()
	} else {
		var err error
		ms.cli, err = dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
		if err != nil {
			return fmt.Errorf("docker client: %w", err)
		}
	}

	// Phase 1: Validate
	ms.Log.Info("meeseeks phase 1/5: validate", "id", ms.Meeseeks.ID)
	if err := ms.phase1Validate(); err != nil {
		return fmt.Errorf("phase 1 (validate): %w", err)
	}

	// Phase 2: Network + Enforcement
	ms.Log.Info("meeseeks phase 2/5: enforcement", "id", ms.Meeseeks.ID)
	if err := ms.phase2Enforcement(ctx); err != nil {
		ms.failClosed(ctx)
		return fmt.Errorf("phase 2 (enforcement): %w", err)
	}

	// Phase 3: Constraints
	ms.Log.Info("meeseeks phase 3/5: constraints", "id", ms.Meeseeks.ID)
	if err := ms.phase3Constraints(); err != nil {
		ms.failClosed(ctx)
		return fmt.Errorf("phase 3 (constraints): %w", err)
	}

	// Phase 4: Workspace
	ms.Log.Info("meeseeks phase 4/5: workspace", "id", ms.Meeseeks.ID)
	if err := ms.phase4Workspace(ctx); err != nil {
		ms.failClosed(ctx)
		return fmt.Errorf("phase 4 (workspace): %w", err)
	}

	// Phase 5: Body runtime started (workspace container runs the body)
	ms.Log.Info("meeseeks phase 5/5: body started", "id", ms.Meeseeks.ID)

	return nil
}

func (ms *MeeseeksStartSequence) phase1Validate() error {
	if ms.Meeseeks.Task == "" {
		return fmt.Errorf("task is required")
	}

	// Create audit directory
	auditDir := filepath.Join(ms.Home, "audit", ms.Meeseeks.ID)
	if err := os.MkdirAll(auditDir, 0700); err != nil {
		return fmt.Errorf("create audit dir: %w", err)
	}
	enforcerAuditDir := filepath.Join(auditDir, "enforcer")
	if err := os.MkdirAll(enforcerAuditDir, 0777); err != nil {
		return fmt.Errorf("create enforcer audit dir: %w", err)
	}

	return nil
}

func (ms *MeeseeksStartSequence) phase2Enforcement(ctx context.Context) error {
	netName := ms.Meeseeks.NetworkName

	// Create dedicated internal network for this Meeseeks
	if err := containers.CreateInternalNetwork(ctx, ms.cli, netName, map[string]string{
		"agency.type":     "meeseeks-internal",
		"agency.meeseeks": ms.Meeseeks.ID,
		"agency.parent":   ms.Meeseeks.ParentAgent,
	}); err != nil {
		return fmt.Errorf("create network %s: %w", netName, err)
	}
	ms.networkCreated = true

	// Resolve enforcer image
	if err := images.Resolve(ctx, ms.cli, "enforcer", ms.Version, ms.SourceDir, ms.BuildID, ms.Log); err != nil {
		return fmt.Errorf("resolve enforcer image: %w", err)
	}

	// Enforcer audit dir
	auditDir := filepath.Join(ms.Home, "audit", ms.Meeseeks.ID, "enforcer")

	// Enforcer data dir
	dataDir := filepath.Join(ms.Home, "infrastructure", "enforcer", "data", ms.Meeseeks.ID)
	os.MkdirAll(dataDir, 0777)

	// Policy dir
	policyDir := filepath.Join(ms.Home, "infrastructure", "enforcer", "policy")
	os.MkdirAll(policyDir, 0755)

	// Config dir
	configDir := filepath.Join(ms.Home, "infrastructure", "enforcer", "config")
	os.MkdirAll(configDir, 0755)

	// Services dir
	servicesDir := filepath.Join(ms.Home, "services")
	os.MkdirAll(servicesDir, 0755)

	env := []string{
		"HOME=/agency/enforcer/data",
		"AGENT_NAME=" + ms.Meeseeks.ID,
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

	// Routing config
	routingYAML := filepath.Join(ms.Home, "infrastructure", "routing.yaml")
	if fileExists(routingYAML) {
		binds = append(binds, routingYAML+":/agency/enforcer/routing.yaml:ro")
	}

	// Server config
	serverConfig := filepath.Join(configDir, "server-config.yaml")
	if fileExists(serverConfig) {
		binds = append(binds, serverConfig+":/agency/enforcer/server-config.yaml:ro")
	}

	// Egress CA cert
	egressCA := filepath.Join(ms.Home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	if fileExists(egressCA) {
		binds = append(binds, egressCA+":/etc/ssl/certs/agency-egress-ca.pem:ro")
		env = append(env, "SSL_CERT_FILE=/etc/ssl/certs/agency-egress-ca.pem")
	}

	meEnforcerHC := containers.HostConfigDefaults(containers.RoleEnforcer)
	meEnforcerHC.Binds = binds
	meEnforcerHC.NetworkMode = container.NetworkMode(netName)
	meEnforcerHC.Tmpfs = map[string]string{"/tmp": "size=64M", "/run": "size=32M"}

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
		meEnforcerHC,
		nil,
	)
	if err != nil {
		return fmt.Errorf("create/start enforcer container: %w", err)
	}
	ms.enforcerID = enforcerContainerID

	// Wait for running
	if err := waitContainerRunning(ctx, ms.cli, ms.Meeseeks.EnforcerName, 10*time.Second); err != nil {
		return err
	}

	// Connect to gateway network (hub — service access, signals, budget)
	_ = ms.cli.NetworkConnect(ctx, gatewayNet, enforcerContainerID, &network.EndpointSettings{
		Aliases: []string{"enforcer"},
	})

	// Connect to egress network (LLM proxy)
	_ = ms.cli.NetworkConnect(ctx, egressIntNet, enforcerContainerID, &network.EndpointSettings{
		Aliases: []string{"enforcer"},
	})

	return nil
}

func (ms *MeeseeksStartSequence) phase3Constraints() error {
	// Verify parent constraints exist
	if ms.ParentConstraintsPath == "" {
		// Default: use parent agent's constraints
		ms.ParentConstraintsPath = filepath.Join(ms.Home, "agents", ms.Meeseeks.ParentAgent, "constraints.yaml")
	}
	if !fileExists(ms.ParentConstraintsPath) {
		return fmt.Errorf("parent constraints not found: %s", ms.ParentConstraintsPath)
	}
	return nil
}

func (ms *MeeseeksStartSequence) phase4Workspace(ctx context.Context) error {
	// Resolve body image
	if err := images.Resolve(ctx, ms.cli, "body", ms.Version, ms.SourceDir, ms.BuildID, ms.Log); err != nil {
		return fmt.Errorf("resolve body image: %w", err)
	}

	netName := ms.Meeseeks.NetworkName
	auditDir := filepath.Join(ms.Home, "audit", ms.Meeseeks.ID)

	env := []string{
		"AGENCY_ENFORCER_URL=http://enforcer:3128/v1",
		"OPENAI_API_BASE=http://enforcer:3128/v1",
		"HTTP_PROXY=http://enforcer:3128",
		"HTTPS_PROXY=http://enforcer:3128",
		"NO_PROXY=enforcer,comms,knowledge,localhost,127.0.0.1",
		"AGENCY_AGENT_NAME=" + ms.Meeseeks.ID,
		"AGENCY_MODEL=claude-" + ms.Meeseeks.Model,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		// Meeseeks-specific environment variables
		"AGENCY_MEESEEKS=true",
		"AGENCY_MEESEEKS_ID=" + ms.Meeseeks.ID,
		"AGENCY_MEESEEKS_TASK=" + ms.Meeseeks.Task,
		"AGENCY_MEESEEKS_PARENT=" + ms.Meeseeks.ParentAgent,
		fmt.Sprintf("AGENCY_MEESEEKS_BUDGET=%.4f", ms.Meeseeks.Budget),
		"AGENCY_MEESEEKS_CHANNEL=" + ms.Meeseeks.Channel,
	}

	// CA certs
	egressCA := filepath.Join(ms.Home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	caBundle := filepath.Join(ms.Home, "infrastructure", "egress", "certs", "ca-bundle.pem")
	if fileExists(caBundle) {
		env = append(env,
			"SSL_CERT_FILE=/etc/ssl/certs/agency-ca-bundle.pem",
			"REQUESTS_CA_BUNDLE=/etc/ssl/certs/agency-ca-bundle.pem",
		)
	}
	if fileExists(egressCA) {
		env = append(env, "NODE_EXTRA_CA_CERTS=/etc/ssl/certs/agency-egress-ca.pem")
	}

	binds := []string{
		ms.ParentConstraintsPath + ":/agency/constraints.yaml:ro", // Tenet 1 + 11: parent's constraints, read-only
		auditDir + ":/var/lib/agency/audit:ro",                     // Tenet 2: read-only audit
	}

	// CA certs
	if fileExists(egressCA) {
		binds = append(binds, egressCA+":/etc/ssl/certs/agency-egress-ca.pem:ro")
	}
	if fileExists(caBundle) {
		binds = append(binds, caBundle+":/etc/ssl/certs/agency-ca-bundle.pem:ro")
	}

	meWorkspaceHC := containers.HostConfigDefaults(containers.RoleMeeseeks)
	meWorkspaceHC.Binds = binds
	meWorkspaceHC.NetworkMode = container.NetworkMode(netName)
	meWorkspaceHC.Tmpfs = map[string]string{"/tmp": "size=256M", "/run": "size=64M"}

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
		meWorkspaceHC,
		nil,
	)
	if err != nil {
		return fmt.Errorf("create/start workspace container: %w", err)
	}
	ms.workspaceID = workspaceContainerID

	ms.Log.Info("meeseeks workspace started",
		"id", ms.Meeseeks.ID,
		"container", ms.Meeseeks.ContainerName,
		"task", ms.Meeseeks.Task,
	)

	return nil
}

// failClosed tears down everything created so far on phase failure.
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
