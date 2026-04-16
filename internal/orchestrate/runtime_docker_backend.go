package orchestrate

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"log/slog"

	dockerclient "github.com/docker/docker/client"

	agencyDocker "github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/orchestrate/containers"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

type dockerRuntimeBackend struct {
	home      string
	version   string
	sourceDir string
	buildID   string
	docker    *agencyDocker.Client
	log       *slog.Logger
}

func (b *dockerRuntimeBackend) Name() string {
	return defaultRuntimeBackend
}

func (b *dockerRuntimeBackend) Ensure(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	if err := b.EnsureEnforcer(ctx, spec, false); err != nil {
		return err
	}
	return b.EnsureWorkspace(ctx, spec)
}

func (b *dockerRuntimeBackend) EnsureEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec, rotateKey bool) error {
	sharedCli, err := b.rawClient()
	if err != nil {
		return err
	}
	if err := b.ensureAgentNetwork(ctx, spec.RuntimeID); err != nil {
		return err
	}
	enf, err := NewEnforcerWithClient(spec.RuntimeID, b.home, b.version, b.log, nil, sharedCli)
	if err != nil {
		return err
	}
	enf.SourceDir = b.sourceDir
	enf.BuildID = b.buildID
	enf.ConstraintHostPort = hostPortFromEndpoint(spec.Transport.Enforcer.Endpoint)
	if rotateKey {
		_, err = enf.StartWithKeyRotation(ctx)
	} else {
		_, err = enf.Start(ctx)
	}
	if err != nil {
		return err
	}
	return enf.HealthCheck(ctx, 30*time.Second)
}

func (b *dockerRuntimeBackend) EnsureWorkspace(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	sharedCli, err := b.rawClient()
	if err != nil {
		return err
	}
	ws, err := NewWorkspaceWithClient(spec.RuntimeID, b.home, b.version, b.log, sharedCli)
	if err != nil {
		return err
	}
	ws.SourceDir = b.sourceDir
	ws.BuildID = b.buildID
	return ws.Start(ctx, StartOptions{
		ScopedKey: readScopedAPIKey(spec.Transport.Enforcer.TokenRef),
		Model:     spec.Package.Env["AGENCY_MODEL"],
		AdminModel: spec.Package.Env["AGENCY_ADMIN_MODEL"],
		Env:       spec.Package.Env,
	})
}

func (b *dockerRuntimeBackend) Stop(ctx context.Context, runtimeID string) error {
	sharedCli, err := b.rawClient()
	if err != nil {
		return err
	}
	for _, name := range []string{
		fmt.Sprintf("%s-%s-workspace", prefix, runtimeID),
		fmt.Sprintf("%s-%s-enforcer", prefix, runtimeID),
	} {
		_ = containers.StopAndRemove(ctx, sharedCli, name, 10)
	}
	return nil
}

func (b *dockerRuntimeBackend) Inspect(ctx context.Context, runtimeID string) (runtimecontract.BackendStatus, error) {
	sharedCli, err := b.rawClient()
	if err != nil {
		return runtimecontract.BackendStatus{}, err
	}
	wsName := fmt.Sprintf("%s-%s-workspace", prefix, runtimeID)
	enfName := fmt.Sprintf("%s-%s-enforcer", prefix, runtimeID)
	wsState := inspectContainerState(ctx, sharedCli, wsName)
	enfState := inspectContainerState(ctx, sharedCli, enfName)
	status := runtimecontract.BackendStatus{
		RuntimeID: runtimeID,
		Details: map[string]string{
			"workspace_state": wsState,
			"enforcer_state":  enfState,
		},
	}
	switch {
	case wsState == "running" && enfState == "running":
		status.Phase = runtimecontract.RuntimePhaseRunning
		status.Healthy = true
	case wsState == "running" && enfState != "running":
		status.Phase = runtimecontract.RuntimePhaseDegraded
		status.Details["last_error"] = "workspace is running without an enforcer"
	case wsState != "running" && enfState == "running":
		status.Phase = runtimecontract.RuntimePhaseStarting
	case wsState == "exited" || enfState == "exited":
		status.Phase = runtimecontract.RuntimePhaseFailed
		status.Details["last_error"] = "runtime container exited unexpectedly"
	default:
		status.Phase = runtimecontract.RuntimePhaseStopped
	}
	return status, nil
}

func (b *dockerRuntimeBackend) Validate(ctx context.Context, runtimeID string) error {
	status, err := b.Inspect(ctx, runtimeID)
	if err != nil {
		return err
	}
	if status.Details["enforcer_state"] == "running" {
		return nil
	}
	return fmt.Errorf("runtime %q enforcer is not running", runtimeID)
}

func (b *dockerRuntimeBackend) Capabilities(ctx context.Context) (runtimecontract.BackendCapabilities, error) {
	_ = ctx
	if b.docker == nil {
		return runtimecontract.BackendCapabilities{}, fmt.Errorf("docker is not available")
	}
	return runtimecontract.BackendCapabilities{
		SupportedTransportTypes: []string{runtimecontract.TransportTypeLoopbackHTTP},
		SupportsComposeLike:     false,
		SupportsRootless:        false,
	}, nil
}

func (b *dockerRuntimeBackend) rawClient() (*dockerclient.Client, error) {
	if b.docker == nil {
		return nil, fmt.Errorf("docker is not available")
	}
	return b.docker.RawClient(), nil
}

func (b *dockerRuntimeBackend) ensureAgentNetwork(ctx context.Context, runtimeID string) error {
	infra, err := NewInfra(b.home, b.version, b.docker, b.log, nil)
	if err != nil {
		return err
	}
	infra.SourceDir = b.sourceDir
	infra.BuildID = b.buildID
	agentNet := fmt.Sprintf("%s-%s-internal", prefix, runtimeID)
	if err := infra.EnsureAgentNetwork(ctx, agentNet); err != nil {
		return fmt.Errorf("create agent network: %w", err)
	}
	return nil
}

func hostPortFromEndpoint(endpoint string) string {
	if strings.TrimSpace(endpoint) == "" {
		return ""
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return parsed.Port()
}

func inspectContainerState(ctx context.Context, cli *dockerclient.Client, name string) string {
	info, err := cli.ContainerInspect(ctx, name)
	if err != nil {
		return "stopped"
	}
	switch {
	case info.State == nil:
		return "stopped"
	case info.State.Running:
		return "running"
	case info.State.Paused:
		return "paused"
	case info.State.Status != "":
		return info.State.Status
	default:
		return "stopped"
	}
}

func readScopedAPIKey(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- key: ") {
			return strings.Trim(strings.TrimPrefix(line, "- key: "), "\"")
		}
	}
	return ""
}
