package orchestrate

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"log/slog"

	"github.com/geoffbelknap/agency/internal/comms"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/hostadapter/containerops"
	"github.com/geoffbelknap/agency/internal/hostadapter/imageops"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/infratier"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/orchestrate/containers"
	"github.com/geoffbelknap/agency/internal/pkg/envfile"
	"github.com/geoffbelknap/agency/internal/registry"
	"github.com/geoffbelknap/agency/internal/routing"
	"github.com/geoffbelknap/agency/internal/services"
)

type dockerNetworkAPI interface {
	NetworkInspect(ctx context.Context, networkID string, options containerops.InspectOptions) (containerops.Inspect, error)
	NetworkCreate(ctx context.Context, name string, options containerops.CreateOptions) (containerops.CreateResponse, error)
	NetworkRemove(ctx context.Context, networkID string) error
}

const prefix = "agency"

const (
	baseGatewayNet   = runtimehost.BaseGatewayNet
	baseEgressIntNet = runtimehost.BaseEgressIntNet
	baseEgressExtNet = runtimehost.BaseEgressExtNet
	baseOperatorNet  = runtimehost.BaseOperatorNet
)

var defaultImages = runtimehost.DefaultImages
var defaultHealthChecks = runtimehost.DefaultHealthChecks

// loggingEnv returns standard logging environment variables for a container.
// Every agency container gets these so structured logging works by default.
func (inf *Infra) loggingEnv(component string) map[string]string {
	format := os.Getenv("AGENCY_LOG_FORMAT")
	if format == "" {
		format = "json"
	}
	return map[string]string{
		"AGENCY_LOG_FORMAT": format,
		"AGENCY_COMPONENT":  component,
		"BUILD_ID":          inf.BuildID,
	}
}

// mergeEnv copies all entries from src into dst (dst wins on conflict).
func mergeEnv(dst, src map[string]string) {
	for k, v := range src {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}

// Infra manages shared infrastructure containers.
type Infra struct {
	Home         string
	Instance     string
	Version      string
	SourceDir    string
	BuildID      string
	GatewayAddr  string // e.g. "127.0.0.1:8200"
	GatewayToken string // full auth token from config.yaml
	EgressToken  string // scoped token for egress credential resolution
	Registry     *registry.Registry
	Optimizer    *routing.RoutingOptimizer
	Docker       *runtimehost.DockerHandle
	Comms        comms.Client
	cli          *runtimehost.RawClient
	log          *slog.Logger
	hmacKey      []byte
}

// NewInfra creates a new infrastructure manager.
func NewInfra(home, version string, dc *runtimehost.DockerHandle, logger *slog.Logger, hmacKey []byte) (*Infra, error) {
	var cli *runtimehost.RawClient
	if dc != nil {
		cli = dc.RawClient()
	} else {
		var err error
		cli, err = runtimehost.NewRawClient()
		if err != nil {
			return nil, err
		}
	}

	reg, err := registry.Open(filepath.Join(home, "registry.db"))
	if err != nil {
		return nil, fmt.Errorf("open principal registry: %w", err)
	}

	return &Infra{
		Home:     home,
		Instance: infraInstanceName(),
		Version:  version,
		Docker:   dc,
		Registry: reg,
		Comms:    dc,
		cli:      cli,
		log:      logger,
		hmacKey:  hmacKey,
	}, nil
}

func infraInstanceName() string {
	return runtimehost.InfraInstanceName()
}

func scopedInfraName(base string) string {
	return runtimehost.ScopedInfraName(base)
}

func gatewayNetName() string {
	return runtimehost.GatewayNetName()
}

func egressIntNetName() string {
	return runtimehost.EgressIntNetName()
}

func egressExtNetName() string {
	return runtimehost.EgressExtNetName()
}

func operatorNetName() string {
	return runtimehost.OperatorNetName()
}

func (inf *Infra) scopedName(base string) string {
	if inf.Instance == "" {
		return base
	}
	return fmt.Sprintf("%s-%s", base, inf.Instance)
}

func (inf *Infra) containerName(role string) string {
	return inf.scopedName(fmt.Sprintf("%s-infra-%s", prefix, role))
}

func (inf *Infra) gatewayContainerHost() string {
	if inf.cli != nil && inf.cli.Backend() == runtimehost.BackendContainerd {
		return inf.containerName("gateway")
	}
	return "gateway"
}

func (inf *Infra) egressContainerHost() string {
	if inf.cli != nil && inf.cli.Backend() == runtimehost.BackendContainerd {
		return inf.containerName("egress")
	}
	return "egress"
}

func (inf *Infra) gatewayNetName() string {
	return inf.scopedName(baseGatewayNet)
}

func (inf *Infra) egressIntNetName() string {
	return inf.scopedName(baseEgressIntNet)
}

func (inf *Infra) egressExtNetName() string {
	return inf.scopedName(baseEgressExtNet)
}

func (inf *Infra) operatorNetName() string {
	return inf.scopedName(baseOperatorNet)
}

func (inf *Infra) instanceLabel() string {
	if inf.Instance == "" {
		return "default"
	}
	return inf.Instance
}

func (inf *Infra) infraLabels(ctx context.Context, imageRef, component string) map[string]string {
	buildID := ""
	if inf.cli != nil {
		buildID = imageops.ImageBuildLabel(ctx, inf.cli, imageRef)
	}
	return map[string]string{
		"agency.managed":       "true",
		"agency.role":          "infra",
		"agency.component":     component,
		"agency.instance":      inf.instanceLabel(),
		"agency.build.id":      buildID,
		"agency.build.gateway": inf.BuildID,
	}
}

func (inf *Infra) infraNetworkLabels(component string) map[string]string {
	return map[string]string{
		"agency.role":      "infra",
		"agency.component": component,
		"agency.instance":  inf.instanceLabel(),
	}
}

// serviceLabels returns Docker labels for service discovery.
func (inf *Infra) serviceLabels(ctx context.Context, imageRef, serviceName, port string) map[string]string {
	cname := inf.containerName(serviceName)
	labels := inf.infraLabels(ctx, imageRef, serviceName)
	labels[services.LabelServiceEnabled] = "true"
	labels[services.LabelServiceName] = serviceName
	labels[services.LabelServicePort] = port
	labels[services.LabelServiceHealth] = "/health"
	labels[services.LabelServiceNetwork] = inf.gatewayNetName()
	labels[services.LabelServiceHMAC] = services.GenerateHMAC(cname, inf.hmacKey)
	return labels
}

// ProgressFunc is called with component name and status during infrastructure startup.
type ProgressFunc func(component, status string)

// EnsureRunning starts all shared infrastructure if not already running.
func (inf *Infra) EnsureRunning(ctx context.Context) error {
	return inf.EnsureRunningWithProgress(ctx, nil)
}

// EnsureRunningWithProgress starts all shared infrastructure, calling onProgress
// for each component as it is started.
func (inf *Infra) EnsureRunningWithProgress(ctx context.Context, onProgress ProgressFunc) error {
	inf.log.Info("ensuring shared infrastructure")

	progress := func(component, status string) {
		inf.log.Info(status, "component", component)
		if onProgress != nil {
			onProgress(component, status)
		}
	}

	progress("configs", "Preparing configuration")

	// Retroactively enforce 0700 on audit directory and all subdirs (ASK tenet 2).
	// os.MkdirAll does not update permissions on existing directories, so we walk
	// the entire audit tree and chmod every entry to close the window where dirs
	// created before this fix had 0777 permissions.
	auditDir := filepath.Join(inf.Home, "audit")
	if err := enforceAuditDirPerms(auditDir); err != nil {
		inf.log.Warn("audit dir permission enforcement", "err", err)
	}

	if err := inf.ensureConfigs(); err != nil {
		return fmt.Errorf("ensure configs: %w", err)
	}

	// Seed built-in service definitions into the capability registry.
	// Copies from source tree if the registry entry doesn't exist yet.
	inf.seedBuiltinServices()

	// Merge ontology (base + extensions) on startup
	inf.mergeOntology()

	// Write default classification config if it doesn't exist
	inf.ensureDefaultClassification()

	progress("networks", "Creating managed networks")
	if err := inf.ensureNetworks(ctx); err != nil {
		return fmt.Errorf("ensure networks: %w", err)
	}

	// Gateway proxy must start first — it's the hub. Every other container
	// depends on it for Docker DNS resolution ("gateway" hostname), credential
	// resolution, and event publishing through the socket proxy.
	progress("gateway-proxy", "Starting gateway proxy")
	if err := inf.ensureGatewayProxy(ctx); err != nil {
		return fmt.Errorf("start gateway-proxy: %w", err)
	}
	progress("gateway-proxy", "Started gateway-proxy")

	componentSet := map[string]struct {
		name   string
		desc   string
		ensure func(ctx context.Context) error
	}{
		"egress":     {"egress", "Starting egress proxy (credential swap, network mediation)", inf.ensureEgress},
		"comms":      {"comms", "Starting comms server (channels, messaging)", inf.ensureComms},
		"knowledge":  {"knowledge", "Starting knowledge graph", inf.ensureKnowledge},
		"intake":     {"intake", "Starting intake service", inf.ensureIntake},
		"web-fetch":  {"web-fetch", "Starting web-fetch service (content extraction, security scanning)", inf.ensureWebFetch},
		"web":        {"web", "Starting web UI", inf.ensureWeb},
		"relay":      {"relay", "Starting relay tunnel", inf.ensureRelay},
		"embeddings": {"embeddings", "Starting embeddings service (local vector embeddings)", inf.ensureEmbeddings},
	}
	var components []struct {
		name   string
		desc   string
		ensure func(ctx context.Context) error
	}
	for _, name := range infratier.StartupComponents() {
		if comp, ok := componentSet[name]; ok {
			components = append(components, comp)
		}
	}

	// Start remaining components in parallel — they all depend on gateway-proxy
	// which is now ready, but are independent of each other.
	progress("infra", "Starting all services")
	type result struct {
		name string
		err  error
	}
	resultCh := make(chan result, len(components))
	for _, comp := range components {
		comp := comp
		go func() {
			err := comp.ensure(ctx)
			if err == nil {
				progress(comp.name, "Started "+comp.name)
			}
			resultCh <- result{comp.name, err}
		}()
	}

	var errs []string
	for range components {
		r := <-resultCh
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("start %s: %s", r.name, r.err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("infrastructure failures: %s", strings.Join(errs, "; "))
	}

	if err := inf.waitCommsReady(ctx, 30*time.Second); err != nil {
		return fmt.Errorf("comms bridge: %w", err)
	}

	if err := inf.ensureSystemChannels(ctx); err != nil {
		return fmt.Errorf("system channels: %w", err)
	}

	// Audit: verify no managed container has Docker socket access
	if violations := inf.AuditDockerSocket(ctx); len(violations) > 0 {
		inf.log.Error("Docker socket audit FAILED — containers with /var/run/docker.sock mounted",
			"containers", violations)
	}

	return nil
}

// Teardown stops and removes all shared infrastructure containers.
func (inf *Infra) Teardown(ctx context.Context) error {
	return inf.TeardownWithProgress(ctx, nil)
}

// TeardownWithProgress stops and removes all shared infrastructure containers,
// calling onProgress for each component.
func (inf *Infra) TeardownWithProgress(ctx context.Context, onProgress ProgressFunc) error {
	inf.log.Info("tearing down infrastructure")
	if onProgress != nil {
		onProgress("infra", "Stopping all services")
	}
	// Teardown runs in reverse dependency order. Services depend on the gateway
	// proxy, so remove them first and tear down the proxy last.
	roles := []string{"relay", "web", "web-fetch", "intake", "knowledge", "comms", "egress", "embeddings"}

	var wg sync.WaitGroup
	for _, role := range roles {
		role := role
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := inf.containerName(role)
			if err := inf.stopAndRemove(ctx, name, stopTimeoutFor(role)); err != nil {
				inf.log.Warn("teardown", "container", name, "err", err)
				inf.logTeardownContainerState(ctx, name)
			}
			if onProgress != nil {
				onProgress(role, "Stopped "+role)
			}
		}()
	}
	wg.Wait()

	if err := inf.stopAndRemove(ctx, inf.containerName("gateway-proxy"), stopTimeoutFor("gateway-proxy")); err != nil {
		inf.log.Warn("teardown", "container", inf.containerName("gateway-proxy"), "err", err)
		inf.logTeardownContainerState(ctx, inf.containerName("gateway-proxy"))
	}
	if onProgress != nil {
		onProgress("gateway-proxy", "Stopped gateway-proxy")
	}

	// Clean up agency-managed networks after all containers are stopped
	inf.cleanNetworks(ctx)

	return nil
}

// cleanNetworks removes agency-managed Docker networks that have no connected endpoints.
func (inf *Infra) cleanNetworks(ctx context.Context) {
	runtimehost.CleanManagedNetworks(ctx, inf.cli, inf.log)
}

// RestartComponent stops, removes, and recreates a single component.
func (inf *Infra) RestartComponent(ctx context.Context, component string) error {
	return inf.RestartComponentWithProgress(ctx, component, nil)
}

// RestartComponentWithProgress stops, removes, and recreates a single component,
// calling onProgress for each step.
func (inf *Infra) RestartComponentWithProgress(ctx context.Context, component string, onProgress ProgressFunc) error {
	valid := map[string]func(ctx context.Context) error{
		"gateway-proxy": inf.ensureGatewayProxy,
		"egress":        inf.ensureEgress,
		"comms":         inf.ensureComms,
		"knowledge":     inf.ensureKnowledge,
		"intake":        inf.ensureIntake,
		"web-fetch":     inf.ensureWebFetch,
		"web":           inf.ensureWeb,
		"embeddings":    inf.ensureEmbeddings,
	}

	ensure, ok := valid[component]
	if !ok {
		return fmt.Errorf("invalid component %q", component)
	}

	if onProgress != nil {
		onProgress(component, "Stopping "+component)
	}
	name := inf.containerName(component)
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor(component))

	if onProgress != nil {
		onProgress(component, "Starting "+component)
	}
	return ensure(ctx)
}

// EnsureAgentNetwork creates a per-agent internal network if it doesn't exist.
// The network is created with Internal: true so containers have no default
// route to the host — all external access must go through the enforcer/egress
// chain (ASK Tenet 3: mediation is complete).
func (inf *Infra) EnsureAgentNetwork(ctx context.Context, netName string) error {
	if err := runtimehost.EnsureInternalNetworkReady(ctx, inf.cli, netName); err != nil {
		inf.log.Error("failed to create agent network", "network", netName, "err", err)
		return err
	}
	inf.log.Debug("ensured agent network", "network", netName, "internal", true)
	return nil
}

func ensureInternalNetworkReady(ctx context.Context, cli dockerNetworkAPI, netName string) error {
	if _, err := cli.NetworkInspect(ctx, netName, containerops.InspectOptions{}); err == nil {
		return nil
	} else if !containers.IsNetworkNotFound(err) {
		return fmt.Errorf("inspect agent network %s: %w", netName, err)
	}

	createErr := containers.CreateInternalNetwork(ctx, cli, netName, nil)
	if createErr != nil && !containers.IsNetworkAlreadyExists(createErr) {
		return createErr
	}

	backoff := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 250 * time.Millisecond, 500 * time.Millisecond, time.Second}
	for _, delay := range backoff {
		if _, err := cli.NetworkInspect(ctx, netName, containerops.InspectOptions{}); err == nil {
			return nil
		} else if !containers.IsNetworkNotFound(err) {
			return fmt.Errorf("verify agent network %s: %w", netName, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	if _, err := cli.NetworkInspect(ctx, netName, containerops.InspectOptions{}); err == nil {
		return nil
	} else if containers.IsNetworkNotFound(err) {
		return fmt.Errorf("agent network %s not ready after create", netName)
	} else {
		return fmt.Errorf("verify agent network %s: %w", netName, err)
	}
}

// -- Config generation --

func (inf *Infra) ensureConfigs() error {
	infraDir := filepath.Join(inf.Home, "infrastructure")
	os.MkdirAll(infraDir, 0755)

	egressDir := filepath.Join(infraDir, "egress")
	os.MkdirAll(egressDir, 0755)

	policyFile := filepath.Join(egressDir, "policy.yaml")
	if _, err := os.Stat(policyFile); os.IsNotExist(err) {
		os.WriteFile(policyFile, []byte(defaultEgressPolicy), 0644)
	}

	routingFile := filepath.Join(infraDir, "routing.yaml")
	if _, err := os.Stat(routingFile); os.IsNotExist(err) {
		os.WriteFile(routingFile, []byte(defaultRoutingConfig), 0644)
	}
	return nil
}

// seedBuiltinServices copies built-in service definition YAMLs from the source
// tree into ~/.agency/registry/services/ if they don't already exist. This ensures
// built-in capabilities (web-fetch, brave-search, etc.) are available in the
// registry without manual `agency cap add` on fresh installs.
func (inf *Infra) seedBuiltinServices() {
	if inf.SourceDir == "" {
		return // release mode — services come from hub or manual registration
	}
	servicesSource := filepath.Join(inf.SourceDir, "services")
	registryDir := filepath.Join(inf.Home, "registry", "services")
	os.MkdirAll(registryDir, 0755)

	entries, err := os.ReadDir(servicesSource)
	if err != nil {
		inf.log.Debug("no built-in services directory", "path", servicesSource)
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		dest := filepath.Join(registryDir, e.Name())
		if _, err := os.Stat(dest); err == nil {
			continue // already registered — don't overwrite operator customizations
		}
		data, err := os.ReadFile(filepath.Join(servicesSource, e.Name()))
		if err != nil {
			continue
		}
		if err := os.WriteFile(dest, data, 0644); err == nil {
			inf.log.Info("seeded built-in service", "service", e.Name())
		}
	}
}

// -- Network management --

func (inf *Infra) ensureNetworks(ctx context.Context) error {
	// ASK Tenet 3: mediation is complete. Internal networks have no default
	// route to the host — containers can only reach peers on the same network.
	// Hub-and-spoke topology:
	// - inf.gatewayNetName(): Internal — hub for all services and enforcers
	// - inf.egressIntNetName(): Internal — services that need outbound proxy access
	// - inf.egressExtNetName(): NOT internal — egress proxy reaches internet
	// - inf.operatorNetName(): NOT internal — operator tools (web, relay)
	type netSpec struct {
		name     string
		internal bool
	}
	nets := []netSpec{
		{inf.gatewayNetName(), true},    // Hub — all services and enforcers
		{inf.egressIntNetName(), true},  // Services → egress proxy
		{inf.egressExtNetName(), false}, // Egress proxy → internet
		{inf.operatorNetName(), false},  // Operator tools (web, relay)
	}
	for _, n := range nets {
		_, inspectErr := inf.cli.NetworkInspect(ctx, n.name, containerops.InspectOptions{})
		if inspectErr != nil {
			var err error
			switch {
			case n.name == inf.gatewayNetName() || n.name == inf.egressIntNetName():
				err = containers.CreateMediationNetwork(ctx, inf.cli, n.name, inf.infraNetworkLabels(n.name))
			case n.name == inf.operatorNetName():
				err = containers.CreateOperatorNetwork(ctx, inf.cli, n.name, inf.infraNetworkLabels(n.name))
			default:
				err = containers.CreateEgressNetwork(ctx, inf.cli, n.name, inf.infraNetworkLabels(n.name))
			}
			if err != nil {
				return fmt.Errorf("create network %s: %w", n.name, err)
			}
			inf.log.Debug("created network", "name", n.name, "internal", n.internal)
		}
	}
	return nil
}

// -- Individual containers --

func (inf *Infra) ensureGatewayProxy(ctx context.Context) error {
	if err := imageops.Resolve(ctx, inf.cli, "gateway-proxy", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve gateway-proxy image: %w", err)
	}
	name := inf.containerName("gateway-proxy")
	hostGatewayHosts := runtimehost.HostGatewayAliasesEnv(inf.backendName())
	if inf.isRunning(ctx, name) &&
		inf.isCurrentBuild(ctx, name) &&
		inf.isHealthyOrNoCheck(ctx, name) &&
		inf.hasContainerEnv(ctx, name, "AGENCY_HOST_GATEWAY_PORT", inf.gatewayPort()) &&
		inf.hasContainerEnv(ctx, name, "AGENCY_HOST_GATEWAY_HOSTS", hostGatewayHosts) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("gateway-proxy"))

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.NetworkMode = containerops.NetworkMode(inf.gatewayNetName())
	hc.ReadonlyRootfs = true
	hc.Resources.Memory = 64 * 1024 * 1024
	hc.Resources.NanoCPUs = 500_000_000
	// No PID limit — socat forks per connection and the gateway-proxy handles
	// all inter-container traffic. A limit here causes fork() exhaustion under
	// normal load. The memory limit (64MB) is the effective constraint.
	hc.Resources.PidsLimit = nil
	// Reverse bridges: host gateway reaches services through published ports.
	// Ports must publish to the host for macOS Docker Desktop compatibility.
	hc.PortBindings = containerops.PortMap{
		"8202/tcp": []containerops.PortBinding{{HostIP: "127.0.0.1", HostPort: inf.gatewayProxyPort("8202")}},
		"8204/tcp": []containerops.PortBinding{{HostIP: "127.0.0.1", HostPort: inf.gatewayProxyPort("8204")}},
		"8205/tcp": []containerops.PortBinding{{HostIP: "127.0.0.1", HostPort: inf.gatewayProxyPort("8205")}},
	}
	// Mount the run directory so socat can reach the gateway Unix socket.
	// Mount the directory (not the socket file) so new sockets from daemon
	// restarts are picked up without recreating the container.
	runDir := filepath.Join(inf.Home, "run")
	hc.Binds = []string{runDir + ":/run:ro"}

	netCfg := &containerops.NetworkingConfig{
		EndpointsConfig: map[string]*containerops.EndpointSettings{
			inf.gatewayNetName(): {
				Aliases: []string{"gateway"},
			},
			inf.operatorNetName(): {},
		},
	}

	if _, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&containerops.Config{
			Image:    defaultImages["gateway-proxy"],
			Hostname: "gateway-proxy",
			Env: mapToEnv(map[string]string{
				"AGENCY_HOST_GATEWAY_PORT":  inf.gatewayPort(),
				"AGENCY_HOST_GATEWAY_HOSTS": hostGatewayHosts,
			}),
			ExposedPorts: containerops.PortSet{
				"8202/tcp": struct{}{},
				"8204/tcp": struct{}{},
				"8205/tcp": struct{}{},
			},
			Labels: inf.infraLabels(ctx, defaultImages["gateway-proxy"], "gateway-proxy"),
		},
		hc, netCfg,
	); err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}
	// No Docker health check — verify readiness by probing the gateway socket
	// directly from the Go process. This avoids socat fork pressure.
	return inf.waitSocketReady(ctx, 10*time.Second)
}

func (inf *Infra) backendName() string {
	if inf.Docker == nil {
		return runtimehost.BackendDocker
	}
	return inf.Docker.Backend()
}

func (inf *Infra) ensureEgress(ctx context.Context) error {
	if err := imageops.Resolve(ctx, inf.cli, "egress", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve egress image: %w", err)
	}
	name := inf.containerName("egress")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) && inf.isHealthyOrNoCheck(ctx, name) && inf.hasContainerEnv(ctx, name, "GATEWAY_TOKEN", inf.EgressToken) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("egress"))

	infraDir := filepath.Join(inf.Home, "infrastructure")
	configDir := filepath.Join(infraDir, "egress")
	blocklists := filepath.Join(configDir, "blocklists")
	certsDir := filepath.Join(configDir, "certs")
	os.MkdirAll(blocklists, 0777)
	os.Chmod(blocklists, 0777) // ensure umask doesn't restrict
	os.MkdirAll(certsDir, 0777)
	os.Chmod(certsDir, 0777) // ensure umask doesn't restrict

	// Cert files generated by mitmproxy on first run may be root-owned with 600
	// perms. CAP_DROP ALL removes DAC_OVERRIDE, so the container can't read
	// its own certs on restart. Fix permissions on any existing cert files.
	if entries, err := os.ReadDir(certsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				os.Chmod(filepath.Join(certsDir, e.Name()), 0644)
			}
		}
	}

	binds := []string{
		configDir + ":/app/config:ro",
		blocklists + ":/app/blocklists:rw",
		certsDir + ":/app/certs:rw",
	}

	// Load env from .env file (legacy) and config.yaml config: section.
	// Config vars take precedence — .env is for backward compat.
	env := envfile.Load(filepath.Join(inf.Home, ".env"))
	cfg := config.Load()
	for k, v := range cfg.ConfigVars {
		env[k] = v
	}

	// Routing config
	routingFile := filepath.Join(infraDir, "routing.yaml")
	if fileExists(routingFile) {
		binds = append(binds, routingFile+":/app/secrets/routing.yaml:ro")
	}

	// Credential swap config
	swapConfig := filepath.Join(infraDir, "credential-swaps.yaml")
	if fileExists(swapConfig) {
		binds = append(binds, swapConfig+":/app/secrets/credential-swaps.yaml:ro")
	}
	swapLocal := filepath.Join(infraDir, "credential-swaps.local.yaml")
	if fileExists(swapLocal) {
		binds = append(binds, swapLocal+":/app/secrets/credential-swaps.local.yaml:ro")
	}

	// Credential resolution: mount the credential-only socket (read-only)
	// so the egress resolver can authenticate with the gateway. The proxy
	// container exposes the gateway as gateway:8200 on the mediation network.
	runDir := filepath.Join(inf.Home, "run")
	credSockPath := filepath.Join(runDir, "gateway-cred.sock")
	if fileExists(credSockPath) {
		binds = append(binds, runDir+":/app/run:ro")
		env["GATEWAY_SOCKET"] = "/app/run/gateway-cred.sock"
	}
	env["GATEWAY_URL"] = "http://" + inf.gatewayContainerHost() + ":8200"
	env["GATEWAY_TOKEN"] = inf.EgressToken
	env["AGENCY_CALLER"] = "egress"

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.Binds = binds
	hc.NetworkMode = containerops.NetworkMode(inf.egressIntNetName())

	mergeEnv(env, inf.loggingEnv("egress"))
	netCfg := (*containerops.NetworkingConfig)(nil)
	if inf.cli.Backend() == runtimehost.BackendContainerd {
		netCfg = &containerops.NetworkingConfig{
			EndpointsConfig: map[string]*containerops.EndpointSettings{
				inf.egressIntNetName(): {},
				inf.egressExtNetName(): {},
				inf.gatewayNetName():   {},
			},
		}
	}

	id, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&containerops.Config{
			Image:       defaultImages["egress"],
			Hostname:    "egress",
			Env:         mapToEnv(env),
			Labels:      inf.serviceLabels(ctx, defaultImages["egress"], "egress", "3128"),
			Healthcheck: defaultHealthChecks["egress"],
		},
		hc, netCfg,
	)
	if err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}

	// Connect to egress-ext network (internet access)
	if inf.cli.Backend() != runtimehost.BackendContainerd {
		inf.connectIfNeeded(ctx, id, inf.egressExtNetName(), []string{"egress"})

		// Connect to gateway network (hub — service access)
		inf.connectIfNeeded(ctx, id, inf.gatewayNetName(), []string{"egress"})
	}

	return inf.waitHealthy(ctx, name, 30*time.Second)
}

func (inf *Infra) ensureComms(ctx context.Context) error {
	if err := imageops.Resolve(ctx, inf.cli, "comms", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve comms image: %w", err)
	}
	name := inf.containerName("comms")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) && inf.isHealthyOrNoCheck(ctx, name) {
		if err := inf.ensureSystemChannels(ctx); err != nil {
			return fmt.Errorf("system channels: %w", err)
		}
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("comms"))

	commsData := filepath.Join(inf.Home, "infrastructure", "comms", "data")
	if err := prepareCommsDataDir(commsData); err != nil {
		return fmt.Errorf("prepare comms data: %w", err)
	}
	agentsDir := filepath.Join(inf.Home, "agents")
	os.MkdirAll(agentsDir, 0755)

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.Binds = []string{
		commsData + ":/app/data:rw",
		agentsDir + ":/app/agents:rw",
	}
	hc.NetworkMode = containerops.NetworkMode(inf.gatewayNetName())
	// No host port binding — comms is reached via Docker container IP.
	// Host port publishing is unreliable on some hosts with user-defined networks.

	if _, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&containerops.Config{
			Image:       defaultImages["comms"],
			Hostname:    "comms",
			Env:         mapToEnv(func() map[string]string { e := inf.loggingEnv("comms"); e["AGENCY_CALLER"] = "comms"; return e }()),
			Labels:      inf.serviceLabels(ctx, defaultImages["comms"], "comms", "8080"),
			Healthcheck: defaultHealthChecks["comms"],
		},
		hc, nil,
	); err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}

	if err := inf.waitHealthy(ctx, name, 30*time.Second); err != nil {
		return err
	}

	return nil
}

func (inf *Infra) ensureKnowledge(ctx context.Context) error {
	if err := imageops.Resolve(ctx, inf.cli, "knowledge", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve knowledge image: %w", err)
	}
	name := inf.containerName("knowledge")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) && inf.isHealthyOrNoCheck(ctx, name) && inf.hasContainerEnv(ctx, name, "AGENCY_GATEWAY_TOKEN", inf.GatewayToken) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("knowledge"))

	knowledgeDir := filepath.Join(inf.Home, "knowledge", "data")
	os.MkdirAll(knowledgeDir, 0777)
	os.Chmod(knowledgeDir, 0777)
	fixDirPerms(knowledgeDir, 0777)

	env := map[string]string{
		"HTTPS_PROXY":          "http://" + inf.egressContainerHost() + ":3128",
		"NO_PROXY":             inf.containerName("embeddings") + ",localhost,127.0.0.1," + inf.gatewayContainerHost(),
		"AGENCY_GATEWAY_TOKEN": inf.GatewayToken,
		"AGENCY_GATEWAY_URL":   "http://" + inf.gatewayContainerHost() + ":8200",
		"AGENCY_CALLER":        "knowledge",
	}

	binds := []string{
		knowledgeDir + ":/data:rw",
	}

	// Mount merged ontology into knowledge container (read-only)
	ontologyFile := filepath.Join(inf.Home, "knowledge", "ontology.yaml")
	if fileExists(ontologyFile) {
		binds = append(binds, ontologyFile+":/app/ontology.yaml:ro")
	}

	egressCA := filepath.Join(inf.Home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	if fileExists(egressCA) {
		env["SSL_CERT_FILE"] = "/etc/ssl/certs/agency-egress-ca.pem"
		binds = append(binds, egressCA+":/etc/ssl/certs/agency-egress-ca.pem:ro")
	}

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.Binds = binds
	hc.NetworkMode = containerops.NetworkMode(inf.gatewayNetName())
	if !inf.suppressDirectServiceHostPorts() {
		hc.PortBindings = containerops.PortMap{
			"8080/tcp": []containerops.PortBinding{{HostIP: "127.0.0.1", HostPort: inf.knowledgePort()}},
		}
	}

	mergeEnv(env, inf.loggingEnv("knowledge"))
	netCfg := (*containerops.NetworkingConfig)(nil)
	if inf.cli.Backend() == runtimehost.BackendContainerd {
		netCfg = &containerops.NetworkingConfig{
			EndpointsConfig: map[string]*containerops.EndpointSettings{
				inf.gatewayNetName():   {},
				inf.egressIntNetName(): {},
			},
		}
	}
	id, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&containerops.Config{
			Image:        defaultImages["knowledge"],
			Hostname:     "knowledge",
			Env:          mapToEnv(env),
			Labels:       inf.serviceLabels(ctx, defaultImages["knowledge"], "knowledge", "8080"),
			Healthcheck:  defaultHealthChecks["knowledge"],
			ExposedPorts: containerops.PortSet{"8080/tcp": struct{}{}},
		},
		hc, netCfg,
	)
	if err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}
	if err := inf.waitHealthy(ctx, name, 90*time.Second); err != nil {
		return err
	}

	// Connect knowledge to egress-int network for outbound proxy access
	if inf.cli.Backend() != runtimehost.BackendContainerd {
		inf.connectIfNeeded(ctx, id, inf.egressIntNetName(), []string{"knowledge"})
	}
	return nil
}

func (inf *Infra) ensureIntake(ctx context.Context) error {
	if err := imageops.Resolve(ctx, inf.cli, "intake", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve intake image: %w", err)
	}
	name := inf.containerName("intake")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) && inf.isHealthyOrNoCheck(ctx, name) && inf.hasContainerEnv(ctx, name, "GATEWAY_TOKEN", inf.GatewayToken) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("intake"))

	intakeDir := filepath.Join(inf.Home, "infrastructure", "intake", "data")
	os.MkdirAll(intakeDir, 0777)
	os.Chmod(intakeDir, 0777)
	fixDirPerms(intakeDir, 0777)
	connectorsDir := filepath.Join(inf.Home, "connectors")
	os.MkdirAll(connectorsDir, 0755)

	binds := []string{
		connectorsDir + ":/app/connectors:ro",
		intakeDir + ":/app/data:rw",
	}

	agentsDir := filepath.Join(inf.Home, "agents")
	if fileExists(agentsDir) {
		binds = append(binds, agentsDir+":/app/agents:ro")
	}

	env := map[string]string{
		"HTTP_PROXY":    "http://" + inf.egressContainerHost() + ":3128",
		"HTTPS_PROXY":   "http://" + inf.egressContainerHost() + ":3128",
		"NO_PROXY":      inf.gatewayContainerHost() + ",localhost,127.0.0.1",
		"GATEWAY_URL":   "http://" + inf.gatewayContainerHost() + ":8200",
		"GATEWAY_TOKEN": inf.GatewayToken,
		"AGENCY_CALLER": "intake",
	}

	// Load operator config vars (LC_ORG_ID, etc.) from config.yaml and .env (legacy)
	for k, v := range envfile.Load(filepath.Join(inf.Home, ".env")) {
		env[k] = v
	}
	intakeCfg := config.Load()
	for k, v := range intakeCfg.ConfigVars {
		env[k] = v
	}

	// Egress CA cert
	egressCA := filepath.Join(inf.Home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	if fileExists(egressCA) {
		binds = append(binds, egressCA+":/app/egress-ca.pem:ro")
		env["EGRESS_CA_CERT"] = "/app/egress-ca.pem"
	}

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.Binds = binds
	hc.NetworkMode = containerops.NetworkMode(inf.gatewayNetName())
	hc.Resources.Memory = 128 * 1024 * 1024 // 128MB — intake is lightweight
	if !inf.suppressDirectServiceHostPorts() {
		hc.PortBindings = containerops.PortMap{
			"8080/tcp": []containerops.PortBinding{{HostIP: "127.0.0.1", HostPort: inf.intakePort()}},
		}
	}

	mergeEnv(env, inf.loggingEnv("intake"))
	netCfg := (*containerops.NetworkingConfig)(nil)
	if inf.cli.Backend() == runtimehost.BackendContainerd {
		netCfg = &containerops.NetworkingConfig{
			EndpointsConfig: map[string]*containerops.EndpointSettings{
				inf.gatewayNetName():   {},
				inf.egressIntNetName(): {},
			},
		}
	}
	id, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&containerops.Config{
			Image:        defaultImages["intake"],
			Hostname:     "intake",
			Env:          mapToEnv(env),
			Labels:       inf.serviceLabels(ctx, defaultImages["intake"], "intake", "8080"),
			Healthcheck:  defaultHealthChecks["intake"],
			ExposedPorts: containerops.PortSet{"8080/tcp": struct{}{}},
		},
		hc, netCfg,
	)
	if err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}

	// Connect intake to egress-int network for outbound proxy access
	if inf.cli.Backend() != runtimehost.BackendContainerd {
		inf.connectIfNeeded(ctx, id, inf.egressIntNetName(), []string{"intake"})
	}

	return inf.waitHealthy(ctx, name, 90*time.Second)
}

func (inf *Infra) ensureWebFetch(ctx context.Context) error {
	if err := imageops.Resolve(ctx, inf.cli, "web-fetch", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve web-fetch image: %w", err)
	}
	name := inf.containerName("web-fetch")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) && inf.isHealthyOrNoCheck(ctx, name) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("web-fetch"))

	configDir := filepath.Join(inf.Home, "web-fetch")
	os.MkdirAll(configDir, 0755)
	auditDir := filepath.Join(inf.Home, "audit", "web-fetch")
	os.MkdirAll(auditDir, 0777)
	os.Chmod(auditDir, 0777)

	binds := []string{
		configDir + ":/agency/web-fetch/config/:ro",
		auditDir + ":/agency/web-fetch/audit/:rw",
	}

	env := map[string]string{
		"HTTP_PROXY":    "http://" + inf.egressContainerHost() + ":3128",
		"HTTPS_PROXY":   "http://" + inf.egressContainerHost() + ":3128",
		"NO_PROXY":      inf.gatewayContainerHost() + ",localhost,127.0.0.1",
		"AGENCY_CALLER": "web-fetch",
	}

	if v := os.Getenv("WEB_FETCH_AUDIT_HMAC_KEY"); v != "" {
		env["WEB_FETCH_AUDIT_HMAC_KEY"] = v
	}

	// Egress CA cert
	egressCA := filepath.Join(inf.Home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	if fileExists(egressCA) {
		binds = append(binds, egressCA+":/app/egress-ca.pem:ro")
		env["EGRESS_CA_CERT"] = "/app/egress-ca.pem"
	}

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.Binds = binds
	hc.NetworkMode = containerops.NetworkMode(inf.gatewayNetName())
	if !inf.suppressDirectServiceHostPorts() {
		hc.PortBindings = containerops.PortMap{
			"8080/tcp": []containerops.PortBinding{{HostIP: "127.0.0.1", HostPort: inf.webFetchPort()}},
		}
	}

	mergeEnv(env, inf.loggingEnv("web-fetch"))
	netCfg := (*containerops.NetworkingConfig)(nil)
	if inf.cli.Backend() == runtimehost.BackendContainerd {
		netCfg = &containerops.NetworkingConfig{
			EndpointsConfig: map[string]*containerops.EndpointSettings{
				inf.gatewayNetName():   {},
				inf.egressIntNetName(): {},
			},
		}
	}
	id, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&containerops.Config{
			Image:        defaultImages["web-fetch"],
			Hostname:     "web-fetch",
			Env:          mapToEnv(env),
			Labels:       inf.serviceLabels(ctx, defaultImages["web-fetch"], "web-fetch", "8080"),
			Healthcheck:  defaultHealthChecks["web-fetch"],
			ExposedPorts: containerops.PortSet{"8080/tcp": struct{}{}},
		},
		hc, netCfg,
	)
	if err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}

	// Connect web-fetch to egress-int network for outbound proxy access
	if inf.cli.Backend() != runtimehost.BackendContainerd {
		inf.connectIfNeeded(ctx, id, inf.egressIntNetName(), []string{"web-fetch"})
	}

	return inf.waitHealthy(ctx, name, 90*time.Second)
}

func (inf *Infra) ensureWeb(ctx context.Context) error {
	// agency-web lives in the repo's top-level web/ directory, so it uses the
	// main source tree as the resolver entrypoint instead of images/web/.
	if err := imageops.Resolve(ctx, inf.cli, "web", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		inf.log.Warn("agency-web image not available, skipping", "err", err)
		return nil // non-fatal — web UI is optional
	}
	name := inf.containerName("web")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) && inf.isHealthyOrNoCheck(ctx, name) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("web"))

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	useHostNetwork := inf.webUsesHostNetwork()
	if useHostNetwork {
		hc.NetworkMode = containerops.NetworkMode("host")
	} else {
		hc.NetworkMode = containerops.NetworkMode(inf.operatorNetName())
	}
	hc.ReadonlyRootfs = true
	hc.Tmpfs = map[string]string{
		"/var/cache/nginx":    "rw,noexec,nosuid,size=16m",
		"/var/lib/nginx/logs": "rw,noexec,nosuid,size=1m",
		"/var/lib/nginx/tmp":  "rw,noexec,nosuid,size=1m",
		"/run/nginx":          "rw,noexec,nosuid,size=1m",
		"/var/run":            "rw,noexec,nosuid,size=1m",
		"/tmp":                "rw,noexec,nosuid,size=1m",
	}
	if !useHostNetwork {
		hc.PortBindings = containerops.PortMap{
			"8280/tcp": []containerops.PortBinding{{HostIP: "127.0.0.1", HostPort: inf.webPort()}},
		}
		// Web container needs the full gateway API (not the restricted socket proxy),
		// so route to the host's TCP listener instead of the mediation-net gateway-proxy.
		hc.ExtraHosts = []string{"gateway:host-gateway"}
	}
	hc.Resources.Memory = 64 * 1024 * 1024 // 64MB — nginx serving static files
	hc.Resources.NanoCPUs = 500_000_000    // 0.5 CPU
	pidsLimit := int64(64)
	hc.Resources.PidsLimit = &pidsLimit

	if _, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&containerops.Config{
			Image:    defaultImages["web"],
			Hostname: "web",
			Env: mapToEnv(map[string]string{
				"AGENCY_GATEWAY_HOST": inf.webGatewayHost(),
				"AGENCY_GATEWAY_PORT": inf.gatewayPort(),
				"AGENCY_WEB_LISTEN":   inf.webListenAddr(),
			}),
			Labels:       inf.infraLabels(ctx, defaultImages["web"], "web"),
			Healthcheck:  inf.webHealthCheck(),
			ExposedPorts: containerops.PortSet{"8280/tcp": struct{}{}},
		},
		hc, nil,
	); err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}
	return inf.waitHealthy(ctx, name, 30*time.Second)
}

func (inf *Infra) ensureRelay(ctx context.Context) error {
	// Relay is optional — only start if relay.yaml exists.
	relayConfigPath := filepath.Join(inf.Home, "relay.yaml")
	if !fileExists(relayConfigPath) {
		return nil
	}

	if err := imageops.Resolve(ctx, inf.cli, "relay", inf.Version, "", inf.BuildID, inf.log); err != nil {
		inf.log.Warn("agency-relay image not available, skipping", "err", err)
		return nil // non-fatal — relay is optional
	}
	name := inf.containerName("relay")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("relay"))

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.NetworkMode = containerops.NetworkMode(inf.operatorNetName())
	hc.Binds = []string{
		inf.Home + ":/home/relay/.agency:rw",
	}
	hc.ExtraHosts = []string{"gateway:host-gateway"}
	hc.Resources.Memory = 32 * 1024 * 1024 // 32MB — lightweight tunnel
	hc.Resources.NanoCPUs = 250_000_000    // 0.25 CPU
	pidsLimit := int64(32)
	hc.Resources.PidsLimit = &pidsLimit

	if _, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&containerops.Config{
			Image:    defaultImages["relay"],
			Hostname: "relay",
			Env: []string{
				"AGENCY_HOME=/home/relay/.agency",
			},
			Labels: inf.infraLabels(ctx, defaultImages["relay"], "relay"),
		},
		hc, nil,
	); err != nil {
		return err
	}

	return inf.waitRunning(ctx, name, 10*time.Second)
}

func (inf *Infra) ensureEmbeddings(ctx context.Context) error {
	// Conditional: only start if embedding provider is explicitly set to ollama.
	// Otherwise, keep embeddings out of the default core runtime path.
	provider := os.Getenv("KNOWLEDGE_EMBED_PROVIDER")
	if provider != "ollama" {
		name := inf.containerName("embeddings")
		if inf.isRunning(ctx, name) {
			inf.log.Info("embeddings provider changed, stopping container", "provider", provider)
			_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("embeddings"))
		}
		return nil
	}

	upstreamRef := fmt.Sprintf("%s:%s", imageops.OllamaUpstream, imageops.OllamaVersion)
	if err := imageops.ResolveUpstream(ctx, inf.cli, "embeddings", inf.Version, upstreamRef, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve embeddings image: %w", err)
	}
	name := inf.containerName("embeddings")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) && inf.isHealthyOrNoCheck(ctx, name) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("embeddings"))

	// Model weights persist across restarts
	dataDir := filepath.Join(inf.Home, "infrastructure", "embeddings")
	os.MkdirAll(dataDir, 0777)
	os.Chmod(dataDir, 0777)

	binds := []string{
		dataDir + ":/root/.ollama:rw",
	}

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.Binds = binds
	hc.NetworkMode = containerops.NetworkMode(inf.gatewayNetName())
	// Override memory: 3GB for model inference
	hc.Resources.Memory = 3 * 1024 * 1024 * 1024

	if _, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&containerops.Config{
			Image:       defaultImages["embeddings"],
			Hostname:    "embeddings",
			Labels:      inf.infraLabels(ctx, defaultImages["embeddings"], "embeddings"),
			Healthcheck: defaultHealthChecks["embeddings"],
		},
		hc, nil,
	); err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}
	// Ollama needs longer to initialize than our Python services
	return inf.waitHealthy(ctx, name, 60*time.Second)
}

func (inf *Infra) waitCommsReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, err := inf.Comms.CommsRequest(ctx, "GET", "/channels", nil); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if lastErr != nil {
		return fmt.Errorf("proxy path to comms not ready within %v: %w", timeout, lastErr)
	}
	return fmt.Errorf("proxy path to comms not ready within %v", timeout)
}

// -- System channels --

func (inf *Infra) ensureSystemChannels(ctx context.Context) error {
	channels := []struct {
		name    string
		topic   string
		members []string
	}{
		{"_knowledge-updates", "Knowledge graph updates", nil},
		{"operator", "Platform alerts and operator notifications", []string{"_operator"}},
		{"general", "Shared channel for operator and all agents", []string{"_operator"}},
	}

	for _, ch := range channels {
		body := map[string]interface{}{
			"name":       ch.name,
			"type":       "system",
			"created_by": "_platform",
			"topic":      ch.topic,
			"visibility": "open",
		}
		if ch.members != nil {
			body["members"] = ch.members
		}
		// Single path through the gateway-proxy. Retry with backoff for startup timing.
		var lastErr error
		for attempt := 0; attempt < 8; attempt++ {
			if attempt > 0 {
				time.Sleep(2 * time.Second)
			}
			_, lastErr = inf.Comms.CommsRequest(ctx, "POST", "/channels", body)
			if lastErr == nil || strings.Contains(lastErr.Error(), "409") {
				lastErr = nil
				break
			}
		}
		if lastErr != nil {
			return fmt.Errorf("create system channel %s: %w", ch.name, lastErr)
		}

		// Register channel in the principal registry. Ignore "already exists"
		// errors — system channels are idempotent across restarts.
		if inf.Registry != nil {
			if _, regErr := inf.Registry.Register("channel", ch.name); regErr != nil {
				if !strings.Contains(regErr.Error(), "UNIQUE constraint") {
					inf.log.Warn("registry: register channel", "channel", ch.name, "err", regErr)
				}
			}
		}
	}

	// Register default classification roles so they exist in the principal registry.
	if inf.Registry != nil {
		for _, roleName := range []string{"internal", "restricted", "confidential"} {
			if _, regErr := inf.Registry.Register("role", roleName); regErr != nil {
				if !strings.Contains(regErr.Error(), "UNIQUE constraint") {
					inf.log.Warn("registry: register classification role", "role", roleName, "err", regErr)
				}
			}
		}
	}

	if err := inf.WriteRegistrySnapshot(); err != nil {
		inf.log.Warn("write registry snapshot", "err", err)
	}

	return nil
}

// WriteRegistrySnapshot exports all principals to registry.json in the home directory.
func (inf *Infra) WriteRegistrySnapshot() error {
	if inf.Registry == nil {
		return nil
	}
	data, err := inf.Registry.Snapshot()
	if err != nil {
		return fmt.Errorf("generate registry snapshot: %w", err)
	}
	path := filepath.Join(inf.Home, "registry.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write registry snapshot: %w", err)
	}
	return nil
}

// -- Helpers --

func (inf *Infra) isRunning(ctx context.Context, name string) bool {
	info, err := inf.cli.ContainerInspect(ctx, name)
	if err != nil {
		return false
	}
	return info.State.Running
}

// isHealthyOrNoCheck returns true if the container has no healthcheck
// or if its healthcheck is passing. Returns false for unhealthy or
// restarting containers — those should be replaced, not skipped.
func (inf *Infra) isHealthyOrNoCheck(ctx context.Context, name string) bool {
	info, err := inf.cli.ContainerInspect(ctx, name)
	if err != nil {
		return false
	}
	if info.State.Health == nil {
		return true // no healthcheck configured
	}
	return info.State.Health.Status == "healthy"
}

func (inf *Infra) isCurrentBuild(ctx context.Context, containerName string) bool {
	if inf.BuildID == "" {
		return true
	}
	inspect, err := inf.cli.ContainerInspect(ctx, containerName)
	if err != nil {
		return false
	}
	return inspect.Config.Labels["agency.build.gateway"] == inf.BuildID
}

func (inf *Infra) hasContainerEnv(ctx context.Context, containerName, key, want string) bool {
	inspect, err := inf.cli.ContainerInspect(ctx, containerName)
	if err != nil {
		return false
	}
	prefix := key + "="
	for _, envVar := range inspect.Config.Env {
		if strings.HasPrefix(envVar, prefix) {
			return strings.TrimPrefix(envVar, prefix) == want
		}
	}
	return false
}

func (inf *Infra) stopAndRemove(ctx context.Context, name string, timeoutSecs int) error {
	return containers.StopAndRemove(ctx, inf.cli, name, timeoutSecs)
}

func (inf *Infra) logTeardownContainerState(ctx context.Context, name string) {
	inspect, err := inf.cli.ContainerInspect(ctx, name)
	if err != nil {
		inf.log.Warn("teardown inspect failed", "container", name, "err", err, "home", inf.Home, "instance", inf.instanceLabel())
		return
	}

	state := "unknown"
	running := false
	restarting := false
	dead := false
	exitCode := 0
	if inspect.State != nil {
		state = inspect.State.Status
		running = inspect.State.Running
		restarting = inspect.State.Restarting
		dead = inspect.State.Dead
		exitCode = inspect.State.ExitCode
	}

	inf.log.Warn(
		"teardown container still present",
		"container", name,
		"home", inf.Home,
		"instance", inf.instanceLabel(),
		"state", state,
		"running", running,
		"restarting", restarting,
		"dead", dead,
		"exit_code", exitCode,
	)
}

// stopTimeoutFor returns the appropriate stop timeout for a given component role.
// knowledge and comms get 10s: knowledge needs SQLite WAL checkpoint time,
// comms needs time to drain active connections.
// All others get 5s.
func stopTimeoutFor(role string) int {
	switch role {
	case "knowledge", "comms":
		return 10
	default:
		return 5
	}
}

func (inf *Infra) waitRunning(ctx context.Context, name string, timeout time.Duration) error {
	return runtimehost.WaitRunning(ctx, inf.cli, name, timeout)
}

func (inf *Infra) waitHealthy(ctx context.Context, name string, timeout time.Duration) error {
	return runtimehost.WaitHealthy(ctx, inf.cli, name, timeout)
}

// waitSocketReady polls the gateway Unix socket directly from the Go process.
// Used instead of Docker health checks for the gateway-proxy container to avoid
// socat fork pressure. Connects to the socket and sends a minimal HTTP request.
func (inf *Infra) waitSocketReady(ctx context.Context, timeout time.Duration) error {
	sockPath := filepath.Join(inf.Home, "run", "gateway.sock")
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", sockPath, 2*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("gateway socket not ready within %v", timeout)
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("gateway socket %s not ready within %v", sockPath, timeout)
}

func (inf *Infra) connectIfNeeded(ctx context.Context, containerID, netName string, aliases []string) {
	runtimehost.ConnectIfNeeded(ctx, inf.cli, containerID, netName, aliases, inf.log)
}

// fixDirPerms recursively sets permissions on all subdirectories and files
// under dir. Needed because containers with CAP_DROP ALL lack DAC_OVERRIDE
// and can't access files/dirs owned by other UIDs with restrictive perms.
func fixDirPerms(dir string, perm os.FileMode) {
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		os.Chmod(path, perm)
		return nil
	})
}

func prepareCommsDataDir(dataDir string) error {
	for _, dir := range []string{
		dataDir,
		filepath.Join(dataDir, "channels"),
		filepath.Join(dataDir, "cursors"),
	} {
		if err := os.MkdirAll(dir, 0o777); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o777); err != nil {
			return err
		}
	}

	// Fix subdirectory permissions — containers may have created these with
	// restrictive ownership/perms on an earlier backend. Best-effort chmod
	// handles normal same-user files; explicit writable probes below surface
	// root-owned files with a useful repair command.
	fixDirPerms(dataDir, 0o777)

	for _, seed := range []struct {
		path    string
		content []byte
	}{
		{filepath.Join(dataDir, "index.db"), nil},
		{filepath.Join(dataDir, "subscriptions.db"), nil},
		{filepath.Join(dataDir, "cursors", "_operator.json"), []byte("{}")},
	} {
		if err := ensureWritableSeedFile(seed.path, seed.content); err != nil {
			return err
		}
	}

	for _, dir := range []string{
		dataDir,
		filepath.Join(dataDir, "channels"),
		filepath.Join(dataDir, "cursors"),
	} {
		if err := probeWritableDir(dir); err != nil {
			return err
		}
	}
	return nil
}

func ensureWritableSeedFile(path string, content []byte) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, content, 0o666); err != nil {
			return commsDataRepairError(path, err)
		}
	} else if err != nil {
		return err
	}
	_ = os.Chmod(path, 0o666)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return commsDataRepairError(path, err)
	}
	return f.Close()
}

func probeWritableDir(dir string) error {
	probe, err := os.CreateTemp(dir, ".agency-write-test-*")
	if err != nil {
		return commsDataRepairError(dir, err)
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

func commsDataRepairError(path string, err error) error {
	root := commsDataRootForRepair(path)
	return fmt.Errorf("%s is not writable by the current user: %w; repair with: sudo chown -R %d:%d %s && chmod -R u+rwX,go+rwX %s",
		path, err, os.Getuid(), os.Getgid(), root, root)
}

func commsDataRootForRepair(path string) string {
	current := path
	for {
		if filepath.Base(current) == "data" && filepath.Base(filepath.Dir(current)) == "comms" {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
		current = parent
	}
}

func mapToEnv(m map[string]string) []string {
	var env []string
	for k, v := range m {
		env = append(env, k+"="+v)
	}
	return env
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// gatewayPort extracts the port from the GatewayAddr field (e.g. "127.0.0.1:8200" → "8200").
func (inf *Infra) gatewayPort() string {
	if inf.GatewayAddr == "" {
		return "8200"
	}
	if idx := strings.LastIndex(inf.GatewayAddr, ":"); idx >= 0 {
		return inf.GatewayAddr[idx+1:]
	}
	return "8200"
}

func (inf *Infra) gatewayHost() string {
	if inf.GatewayAddr == "" {
		return "127.0.0.1"
	}
	if host, _, err := net.SplitHostPort(inf.GatewayAddr); err == nil && host != "" {
		return host
	}
	return "127.0.0.1"
}

func (inf *Infra) gatewayHostIsLoopback() bool {
	host := inf.gatewayHost()
	return host == "localhost" || strings.HasPrefix(host, "127.") || host == "::1"
}

func (inf *Infra) webUsesHostNetwork() bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("AGENCY_WEB_NETWORK_MODE")))
	if mode == "bridge" {
		return false
	}
	if mode == "host" {
		return runtime.GOOS == "linux"
	}
	// Linux containers cannot reach a host process bound only to 127.0.0.1 via
	// host-gateway. Use host networking as a compatibility shim while preserving
	// the same external contract: both gateway and web remain loopback-only.
	return runtime.GOOS == "linux" && inf.gatewayHostIsLoopback()
}

func (inf *Infra) webGatewayHost() string {
	if inf.webUsesHostNetwork() {
		return "127.0.0.1"
	}
	return "gateway"
}

func (inf *Infra) webListenAddr() string {
	if inf.webUsesHostNetwork() {
		return "127.0.0.1:" + inf.webPort()
	}
	return "8280"
}

func (inf *Infra) webHealthCheck() *containerops.HealthConfig {
	url := "http://127.0.0.1:8280/health"
	if inf.webUsesHostNetwork() {
		url = "http://127.0.0.1:" + inf.webPort() + "/health"
	}
	return &containerops.HealthConfig{
		Test:        []string{"CMD", "wget", "-q", "-O-", url},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 5 * time.Second,
		Retries:     3,
	}
}

// webPort returns the host port for the web container. Defaults to 8280 but
// can be overridden for isolated local stacks.
func (inf *Infra) webPort() string {
	return envPort("AGENCY_WEB_PORT", "8280")
}

func (inf *Infra) gatewayProxyPort(port string) string {
	switch port {
	case "8202":
		return envPort("AGENCY_GATEWAY_PROXY_PORT", "8202")
	case "8204":
		return envPort("AGENCY_GATEWAY_PROXY_KNOWLEDGE_PORT", "8204")
	case "8205":
		return envPort("AGENCY_GATEWAY_PROXY_INTAKE_PORT", "8205")
	default:
		return port
	}
}

func (inf *Infra) knowledgePort() string {
	return envPort("AGENCY_KNOWLEDGE_PORT", "8204")
}

func (inf *Infra) intakePort() string {
	return envPort("AGENCY_INTAKE_PORT", "8205")
}

func (inf *Infra) webFetchPort() string {
	return envPort("AGENCY_WEB_FETCH_PORT", "8206")
}

var detectWSLHost = func() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	s := strings.ToLower(string(data))
	return strings.Contains(s, "microsoft") || strings.Contains(s, "wsl")
}

func (inf *Infra) suppressDirectServiceHostPorts() bool {
	if inf == nil || inf.cli == nil || inf.cli.Backend() != runtimehost.BackendPodman {
		return false
	}
	endpoint := strings.TrimPrefix(inf.cli.Endpoint(), "unix://")
	return strings.HasPrefix(endpoint, "/run/user/") && detectWSLHost()
}

func envPort(envKey, fallback string) string {
	if raw := os.Getenv(envKey); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 && p < 65536 {
			return raw
		}
	}
	return fallback
}

// enforceAuditDirPerms walks the audit directory tree and sets 0700 on every
// directory it finds. os.MkdirAll does not update permissions on pre-existing
// directories, so this corrects any dirs created before the 0700 fix was in place.
func enforceAuditDirPerms(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip entries we cannot access (e.g. root doesn't exist yet).
			return nil
		}
		if d.IsDir() {
			if chmodErr := os.Chmod(path, 0700); chmodErr != nil {
				return chmodErr
			}
		}
		return nil
	})
}

// ensureDefaultClassification writes the default classification.yaml if it doesn't exist.
func (inf *Infra) ensureDefaultClassification() {
	classificationPath := filepath.Join(inf.Home, "knowledge", "classification.yaml")
	if _, err := os.Stat(classificationPath); os.IsNotExist(err) {
		defaultConfig := `version: 1
tiers:
  public:
    description: "No access restrictions"
    scope: {}
  internal:
    description: "Any registered principal"
    scope:
      principals: ["role:internal"]
  restricted:
    description: "Limited access"
    scope:
      principals: ["role:restricted"]
  confidential:
    description: "Need-to-know only"
    scope:
      principals: ["role:confidential"]
`
		if err := os.MkdirAll(filepath.Dir(classificationPath), 0755); err != nil {
			inf.log.Warn("classification config: mkdir failed", "err", err)
			return
		}
		if err := os.WriteFile(classificationPath, []byte(defaultConfig), 0644); err != nil {
			inf.log.Warn("classification config: write failed", "err", err)
			return
		}
		inf.log.Info("wrote default classification config", "path", classificationPath)
	}
}

// mergeOntology loads the base ontology + extensions and writes the merged result.
func (inf *Infra) mergeOntology() {
	if err := knowledge.EnsureBaseOntology(inf.Home); err != nil {
		inf.log.Warn("base ontology seed failed", "err", err)
		return
	}
	cfg, err := knowledge.LoadOntology(inf.Home)
	if err != nil {
		inf.log.Warn("ontology merge failed", "err", err)
		return
	}
	if err := knowledge.WriteOntology(inf.Home, cfg); err != nil {
		inf.log.Warn("ontology write failed", "err", err)
		return
	}
	inf.log.Info("ontology merged", "version", cfg.Version, "entity_types", len(cfg.EntityTypes), "relationship_types", len(cfg.RelationshipTypes))
}

// -- Default configs --

const defaultEgressPolicy = `# Agency egress policy
version: "0.1"
mode: allowlist
default_action: block
rules:
  - domain: "api.anthropic.com"
    action: allow
  - domain: "api.openai.com"
    action: allow
  - domain: "generativelanguage.googleapis.com"
    action: allow
`

const defaultRoutingConfig = `# Agency LLM routing config
version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com/v1
    auth_env: ANTHROPIC_API_KEY
    auth_header: x-api-key
    auth_prefix: ""
  openai:
    api_base: https://api.openai.com/v1
    auth_env: OPENAI_API_KEY
    auth_header: Authorization
    auth_prefix: "Bearer "
models:
  claude-sonnet:
    provider: anthropic
    provider_model: claude-sonnet-4-20250514
  claude-opus:
    provider: anthropic
    provider_model: claude-opus-4-20250514
  claude-haiku:
    provider: anthropic
    provider_model: claude-haiku-4-5-20251001
  gpt-4o:
    provider: openai
    provider_model: gpt-4o
  gpt-4o-mini:
    provider: openai
    provider_model: gpt-4o-mini
`
