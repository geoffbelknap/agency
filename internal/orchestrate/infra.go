package orchestrate

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"log/slog"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	agencyDocker "github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/comms"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/images"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/orchestrate/containers"
	"github.com/geoffbelknap/agency/internal/pkg/envfile"
	"github.com/geoffbelknap/agency/internal/registry"
	"github.com/geoffbelknap/agency/internal/routing"
	"github.com/geoffbelknap/agency/internal/services"
)

const (
	prefix       = "agency"
	gatewayNet   = "agency-gateway"
	egressIntNet = "agency-egress-int"
	egressExtNet = "agency-egress-ext"
	operatorNet  = "agency-operator"
)

var defaultImages = map[string]string{
	"egress":    "agency-egress:latest",
	"comms":     "agency-comms:latest",
	"knowledge": "agency-knowledge:latest",
	"intake":    "agency-intake:latest",
	"web-fetch": "agency-web-fetch:latest",
	"web":            "agency-web:latest",
	"relay":          "agency-relay:latest",
	"embeddings":     "agency-embeddings:latest",
	"gateway-proxy":  "agency-gateway-proxy:latest",
}

var defaultHealthChecks = map[string]*container.HealthConfig{
	"egress": {
		Test:        []string{"CMD-SHELL", `python -c "import socket; s=socket.socket(); s.settimeout(2); s.connect(('127.0.0.1',3128)); s.close()"`},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 5 * time.Second,
		Retries:     3,
	},
	"comms": {
		Test:        []string{"CMD-SHELL", `python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/health')"`},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 5 * time.Second,
		Retries:     3,
	},
	"knowledge": {
		Test:        []string{"CMD-SHELL", `python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/health')"`},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 5 * time.Second,
		Retries:     3,
	},
	"intake": {
		Test:        []string{"CMD-SHELL", `python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/health')"`},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 5 * time.Second,
		Retries:     3,
	},
	"web-fetch": {
		Test:        []string{"CMD", "wget", "-q", "-O-", "http://127.0.0.1:8080/health"},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 5 * time.Second,
		Retries:     3,
	},
	"web": {
		Test:        []string{"CMD", "wget", "--no-check-certificate", "-q", "-O-", "https://127.0.0.1:8280/health"},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 5 * time.Second,
		Retries:     3,
	},
	"embeddings": {
		Test:        []string{"CMD-SHELL", `bash -c "echo > /dev/tcp/127.0.0.1/11434"`},
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 10 * time.Second,
		Retries:     3,
	},
	// gateway-proxy has no Docker health check. It's socat — starts instantly.
	// Readiness is verified by the gateway via waitSocketReady() instead.
	// A Docker health check here would fork socat per check, causing PID
	// exhaustion under concurrent health probes from other containers.
}

func containerName(role string) string {
	return fmt.Sprintf("%s-infra-%s", prefix, role)
}

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
	Version      string
	SourceDir    string
	BuildID      string
	GatewayAddr   string // e.g. "127.0.0.1:8200"
	GatewayToken  string // full auth token from config.yaml
	EgressToken   string // scoped token for egress credential resolution
	Registry     *registry.Registry
	Optimizer    *routing.RoutingOptimizer
	Docker       *agencyDocker.Client
	Comms        comms.Client
	cli        *client.Client
	log        *slog.Logger
	hmacKey    []byte
}

// NewInfra creates a new infrastructure manager.
func NewInfra(home, version string, dc *agencyDocker.Client, logger *slog.Logger, hmacKey []byte) (*Infra, error) {
	var cli *client.Client
	if dc != nil {
		cli = dc.RawClient()
	} else {
		var err error
		cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return nil, err
		}
	}

	reg, err := registry.Open(filepath.Join(home, "registry.db"))
	if err != nil {
		return nil, fmt.Errorf("open principal registry: %w", err)
	}

	return &Infra{Home: home, Version: version, Docker: dc, Registry: reg, Comms: dc, cli: cli, log: logger, hmacKey: hmacKey}, nil
}

// serviceLabels returns Docker labels for service discovery.
func (inf *Infra) serviceLabels(ctx context.Context, imageRef, serviceName, port string) map[string]string {
	cname := containerName(serviceName)
	return map[string]string{
		services.LabelServiceEnabled: "true",
		services.LabelServiceName:    serviceName,
		services.LabelServicePort:    port,
		services.LabelServiceHealth:  "/health",
		services.LabelServiceNetwork: gatewayNet,
		services.LabelServiceHMAC:    services.GenerateHMAC(cname, inf.hmacKey),
		"agency.build.id":            images.ImageBuildLabel(ctx, inf.cli, imageRef),
		"agency.build.gateway":       inf.BuildID,
	}
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

	progress("networks", "Creating Docker networks")
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

	components := []struct {
		name string
		desc string
		ensure func(ctx context.Context) error
	}{
		{"egress", "Starting egress proxy (credential swap, network mediation)", inf.ensureEgress},
		{"comms", "Starting comms server (channels, messaging)", inf.ensureComms},
		{"knowledge", "Starting knowledge graph", inf.ensureKnowledge},
		{"intake", "Starting intake service", inf.ensureIntake},
		{"web-fetch", "Starting web-fetch service (content extraction, security scanning)", inf.ensureWebFetch},
		{"web", "Starting web UI", inf.ensureWeb},
		{"relay", "Starting relay tunnel", inf.ensureRelay},
		{"embeddings", "Starting embeddings service (local vector embeddings)", inf.ensureEmbeddings},
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
	roles := []string{"relay", "web", "web-fetch", "intake", "knowledge", "comms", "egress", "embeddings", "gateway-proxy"}

	var wg sync.WaitGroup
	for _, role := range roles {
		role := role
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := containerName(role)
			if err := inf.stopAndRemove(ctx, name, stopTimeoutFor(role)); err != nil {
				inf.log.Warn("teardown", "container", name, "err", err)
			}
			if onProgress != nil {
				onProgress(role, "Stopped "+role)
			}
		}()
	}
	wg.Wait()

	// Clean up agency-managed networks after all containers are stopped
	inf.cleanNetworks(ctx)

	return nil
}

// cleanNetworks removes agency-managed Docker networks that have no connected endpoints.
func (inf *Infra) cleanNetworks(ctx context.Context) {
	networks, err := inf.cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return
	}
	for _, n := range networks {
		if n.Labels["agency.managed"] != "true" {
			continue
		}
		// Inspect to check for connected endpoints
		detail, err := inf.cli.NetworkInspect(ctx, n.ID, network.InspectOptions{})
		if err != nil {
			continue
		}
		if len(detail.Containers) == 0 {
			if err := inf.cli.NetworkRemove(ctx, n.ID); err != nil {
				inf.log.Debug("clean network skip", "network", n.Name, "err", err)
			} else {
				inf.log.Info("cleaned orphan network", "network", n.Name)
			}
		}
	}
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
	name := containerName(component)
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
	_, err := inf.cli.NetworkInspect(ctx, netName, network.InspectOptions{})
	if err != nil {
		if createErr := containers.CreateInternalNetwork(ctx, inf.cli, netName, nil); createErr != nil {
			inf.log.Error("failed to create agent network", "network", netName, "err", createErr)
			return createErr
		}
		inf.log.Debug("created agent network", "network", netName, "internal", true)
	}
	return nil
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
	// - gatewayNet: Internal — hub for all services and enforcers
	// - egressIntNet: Internal — services that need outbound proxy access
	// - egressExtNet: NOT internal — egress proxy reaches internet
	// - operatorNet: NOT internal — operator tools (web, relay)
	type netSpec struct {
		name     string
		internal bool
	}
	nets := []netSpec{
		{gatewayNet, true},    // Hub — all services and enforcers
		{egressIntNet, true},  // Services → egress proxy
		{egressExtNet, false}, // Egress proxy → internet
		{operatorNet, false},  // Operator tools (web, relay)
	}
	for _, n := range nets {
		_, inspectErr := inf.cli.NetworkInspect(ctx, n.name, network.InspectOptions{})
		if inspectErr != nil {
			var err error
			switch {
			case n.name == gatewayNet || n.name == egressIntNet:
				err = containers.CreateMediationNetwork(ctx, inf.cli, n.name, nil)
			case n.name == operatorNet:
				err = containers.CreateOperatorNetwork(ctx, inf.cli, n.name, nil)
			default:
				err = containers.CreateEgressNetwork(ctx, inf.cli, n.name, nil)
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
	if err := images.Resolve(ctx, inf.cli, "gateway-proxy", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve gateway-proxy image: %w", err)
	}
	name := containerName("gateway-proxy")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) && inf.isHealthyOrNoCheck(ctx, name) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("gateway-proxy"))

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.NetworkMode = container.NetworkMode(gatewayNet)
	hc.ReadonlyRootfs = true
	hc.Resources.Memory = 64 * 1024 * 1024
	hc.Resources.NanoCPUs = 500_000_000
	// No PID limit — socat forks per connection and the gateway-proxy handles
	// all inter-container traffic. A limit here causes fork() exhaustion under
	// normal load. The memory limit (64MB) is the effective constraint.
	hc.Resources.PidsLimit = nil
	// Reverse bridges: host gateway reaches services through published ports.
	// Ports must publish to the host for macOS Docker Desktop compatibility.
	hc.PortBindings = nat.PortMap{
		"8202/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8202"}},
		"8204/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8204"}},
		"8205/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8205"}},
	}
	// Mount the run directory so socat can reach the gateway Unix socket.
	// Mount the directory (not the socket file) so new sockets from daemon
	// restarts are picked up without recreating the container.
	runDir := filepath.Join(inf.Home, "run")
	hc.Binds = []string{runDir + ":/run:ro"}

	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			gatewayNet: {
				Aliases: []string{"gateway"},
			},
			operatorNet: {},
		},
	}

	if _, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&container.Config{
			Image:    defaultImages["gateway-proxy"],
			Hostname: "gateway-proxy",
			ExposedPorts: nat.PortSet{
				"8202/tcp": struct{}{},
				"8204/tcp": struct{}{},
				"8205/tcp": struct{}{},
			},
			Labels: map[string]string{
				"agency.managed":       "true",
				"agency.role":          "infra",
				"agency.component":     "gateway-proxy",
				"agency.build.id":      images.ImageBuildLabel(ctx, inf.cli, defaultImages["gateway-proxy"]),
				"agency.build.gateway": inf.BuildID,
			},
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

func (inf *Infra) ensureEgress(ctx context.Context) error {
	if err := images.Resolve(ctx, inf.cli, "egress", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve egress image: %w", err)
	}
	name := containerName("egress")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) && inf.isHealthyOrNoCheck(ctx, name) {
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
		binds = append(binds, credSockPath+":/app/gateway-cred.sock:rw")
		env["GATEWAY_SOCKET"] = "/app/gateway-cred.sock"
	}
	env["GATEWAY_URL"] = "http://gateway:8200"
	env["GATEWAY_TOKEN"] = inf.EgressToken
	env["AGENCY_CALLER"] = "egress"

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.Binds = binds
	hc.NetworkMode = container.NetworkMode(egressIntNet)

	mergeEnv(env, inf.loggingEnv("egress"))
	id, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&container.Config{
			Image:       defaultImages["egress"],
			Hostname:    "egress",
			Env:         mapToEnv(env),
			Labels:      inf.serviceLabels(ctx, defaultImages["egress"], "egress", "3128"),
			Healthcheck: defaultHealthChecks["egress"],
		},
		hc, nil,
	)
	if err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}

	// Connect to egress-ext network (internet access)
	inf.connectIfNeeded(ctx, id, egressExtNet, []string{"egress"})

	// Connect to gateway network (hub — service access)
	inf.connectIfNeeded(ctx, id, gatewayNet, []string{"egress"})

	return inf.waitHealthy(ctx, name, 30*time.Second)
}


func (inf *Infra) ensureComms(ctx context.Context) error {
	if err := images.Resolve(ctx, inf.cli, "comms", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve comms image: %w", err)
	}
	name := containerName("comms")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) && inf.isHealthyOrNoCheck(ctx, name) {
		if err := inf.ensureSystemChannels(ctx); err != nil {
			return fmt.Errorf("system channels: %w", err)
		}
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("comms"))

	commsData := filepath.Join(inf.Home, "infrastructure", "comms", "data")
	os.MkdirAll(commsData, 0777)
	os.Chmod(commsData, 0777)
	// Fix subdirectory permissions — Docker creates these as root with restrictive
	// perms, but CAP_DROP ALL removes DAC_OVERRIDE so the container can't write.
	fixDirPerms(commsData, 0777)
	agentsDir := filepath.Join(inf.Home, "agents")
	os.MkdirAll(agentsDir, 0755)

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.Binds = []string{
		commsData + ":/app/data:rw",
		agentsDir + ":/app/agents:rw",
	}
	hc.NetworkMode = container.NetworkMode(gatewayNet)
	// No host port binding — comms is reached via Docker container IP.
	// Host port publishing is unreliable on some hosts with user-defined networks.

	if _, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&container.Config{
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

	// Create system channels
	if err := inf.ensureSystemChannels(ctx); err != nil {
		return fmt.Errorf("system channels: %w", err)
	}

	return nil
}

func (inf *Infra) ensureKnowledge(ctx context.Context) error {
	if err := images.Resolve(ctx, inf.cli, "knowledge", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve knowledge image: %w", err)
	}
	name := containerName("knowledge")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) && inf.isHealthyOrNoCheck(ctx, name) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("knowledge"))

	knowledgeDir := filepath.Join(inf.Home, "knowledge", "data")
	os.MkdirAll(knowledgeDir, 0777)
	os.Chmod(knowledgeDir, 0777)
	fixDirPerms(knowledgeDir, 0777)

	env := map[string]string{
		"HTTPS_PROXY":          "http://egress:3128",
		"NO_PROXY":             "agency-infra-embeddings,localhost,127.0.0.1,gateway",
		"AGENCY_GATEWAY_TOKEN": inf.GatewayToken,
		"AGENCY_GATEWAY_URL":   "http://gateway:8200",
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
	hc.NetworkMode = container.NetworkMode(gatewayNet)
	hc.PortBindings = nat.PortMap{
		"8080/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8204"}},
	}

	mergeEnv(env, inf.loggingEnv("knowledge"))
	id, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&container.Config{
			Image:        defaultImages["knowledge"],
			Hostname:     "knowledge",
			Env:          mapToEnv(env),
			Labels:       inf.serviceLabels(ctx, defaultImages["knowledge"], "knowledge", "8080"),
			Healthcheck:  defaultHealthChecks["knowledge"],
			ExposedPorts: nat.PortSet{"8080/tcp": struct{}{}},
		},
		hc, nil,
	)
	if err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}
	if err := inf.waitHealthy(ctx, name, 30*time.Second); err != nil {
		return err
	}

	// Connect knowledge to egress-int network for outbound proxy access
	inf.connectIfNeeded(ctx, id, egressIntNet, []string{"knowledge"})
	return nil
}

func (inf *Infra) ensureIntake(ctx context.Context) error {
	if err := images.Resolve(ctx, inf.cli, "intake", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve intake image: %w", err)
	}
	name := containerName("intake")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) && inf.isHealthyOrNoCheck(ctx, name) {
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
		"HTTP_PROXY":    "http://egress:3128",
		"HTTPS_PROXY":   "http://egress:3128",
		"NO_PROXY":       "gateway,localhost,127.0.0.1",
		"GATEWAY_URL":    "http://gateway:8200",
		"GATEWAY_TOKEN":  inf.GatewayToken,
		"AGENCY_CALLER":  "intake",
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
	hc.NetworkMode = container.NetworkMode(gatewayNet)
	hc.Resources.Memory = 128 * 1024 * 1024 // 128MB — intake is lightweight
	hc.PortBindings = nat.PortMap{
		"8080/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8205"}},
	}

	mergeEnv(env, inf.loggingEnv("intake"))
	id, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&container.Config{
			Image:        defaultImages["intake"],
			Hostname:     "intake",
			Env:          mapToEnv(env),
			Labels:       inf.serviceLabels(ctx, defaultImages["intake"], "intake", "8080"),
			Healthcheck:  defaultHealthChecks["intake"],
			ExposedPorts: nat.PortSet{"8080/tcp": struct{}{}},
		},
		hc, nil,
	)
	if err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}

	// Connect intake to egress-int network for outbound proxy access
	inf.connectIfNeeded(ctx, id, egressIntNet, []string{"intake"})

	return inf.waitHealthy(ctx, name, 30*time.Second)
}

func (inf *Infra) ensureWebFetch(ctx context.Context) error {
	if err := images.Resolve(ctx, inf.cli, "web-fetch", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve web-fetch image: %w", err)
	}
	name := containerName("web-fetch")
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
		"HTTP_PROXY":    "http://egress:3128",
		"HTTPS_PROXY":   "http://egress:3128",
		"NO_PROXY":      "gateway,localhost,127.0.0.1",
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
	hc.NetworkMode = container.NetworkMode(gatewayNet)
	hc.PortBindings = nat.PortMap{
		"8080/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8206"}},
	}

	mergeEnv(env, inf.loggingEnv("web-fetch"))
	id, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&container.Config{
			Image:        defaultImages["web-fetch"],
			Hostname:     "web-fetch",
			Env:          mapToEnv(env),
			Labels:       inf.serviceLabels(ctx, defaultImages["web-fetch"], "web-fetch", "8080"),
			Healthcheck:  defaultHealthChecks["web-fetch"],
			ExposedPorts: nat.PortSet{"8080/tcp": struct{}{}},
		},
		hc, nil,
	)
	if err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}

	// Connect web-fetch to egress-int network for outbound proxy access
	inf.connectIfNeeded(ctx, id, egressIntNet, []string{"web-fetch"})

	return inf.waitHealthy(ctx, name, 30*time.Second)
}

func (inf *Infra) ensureWeb(ctx context.Context) error {
	// agency-web lives in the repo's top-level web/ directory, so it uses the
	// main source tree as the resolver entrypoint instead of images/web/.
	if err := images.Resolve(ctx, inf.cli, "web", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		inf.log.Warn("agency-web image not available, skipping", "err", err)
		return nil // non-fatal — web UI is optional
	}
	name := containerName("web")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) && inf.isHealthyOrNoCheck(ctx, name) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("web"))

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.NetworkMode = container.NetworkMode(operatorNet)
	hc.ReadonlyRootfs = true
	hc.Tmpfs = map[string]string{
		"/var/cache/nginx":    "rw,noexec,nosuid,size=16m",
		"/var/lib/nginx/logs": "rw,noexec,nosuid,size=1m",
		"/var/lib/nginx/tmp":  "rw,noexec,nosuid,size=1m",
		"/run/nginx":          "rw,noexec,nosuid,size=1m",
		"/var/run":            "rw,noexec,nosuid,size=1m",
		"/tmp":                "rw,noexec,nosuid,size=1m",
	}
	hc.PortBindings = nat.PortMap{
		"8280/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8280"}},
	}
	// Web container needs the full gateway API (not the restricted socket proxy),
	// so route to the host's TCP listener instead of the mediation-net gateway-proxy.
	hc.ExtraHosts = []string{"gateway:host-gateway"}
	hc.Resources.Memory = 64 * 1024 * 1024     // 64MB — nginx serving static files
	hc.Resources.NanoCPUs = 500_000_000         // 0.5 CPU
	pidsLimit := int64(64)
	hc.Resources.PidsLimit = &pidsLimit

	if _, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&container.Config{
			Image:    defaultImages["web"],
			Hostname: "web",
			Labels: map[string]string{
				"agency.managed":      "true",
				"agency.role":         "infra",
				"agency.component":    "web",
				"agency.build.id":     images.ImageBuildLabel(ctx, inf.cli, defaultImages["web"]),
				"agency.build.gateway": inf.BuildID,
			},
			Healthcheck:  defaultHealthChecks["web"],
			ExposedPorts: nat.PortSet{"8280/tcp": struct{}{}},
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

	if err := images.Resolve(ctx, inf.cli, "relay", inf.Version, "", inf.BuildID, inf.log); err != nil {
		inf.log.Warn("agency-relay image not available, skipping", "err", err)
		return nil // non-fatal — relay is optional
	}
	name := containerName("relay")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("relay"))

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.NetworkMode = container.NetworkMode(operatorNet)
	hc.Binds = []string{
		inf.Home + ":/home/relay/.agency:rw",
	}
	hc.ExtraHosts = []string{"gateway:host-gateway"}
	hc.Resources.Memory = 32 * 1024 * 1024 // 32MB — lightweight tunnel
	hc.Resources.NanoCPUs = 250_000_000     // 0.25 CPU
	pidsLimit := int64(32)
	hc.Resources.PidsLimit = &pidsLimit

	if _, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&container.Config{
			Image:    defaultImages["relay"],
			Hostname: "relay",
			Env: []string{
				"AGENCY_HOME=/home/relay/.agency",
			},
			Labels: map[string]string{
				"agency.managed":       "true",
				"agency.role":          "infra",
				"agency.component":     "relay",
				"agency.build.id":      images.ImageBuildLabel(ctx, inf.cli, defaultImages["relay"]),
				"agency.build.gateway": inf.BuildID,
			},
		},
		hc, nil,
	); err != nil {
		return err
	}

	return inf.waitRunning(ctx, name, 10*time.Second)
}

func (inf *Infra) ensureEmbeddings(ctx context.Context) error {
	// Conditional: only start if embedding provider is ollama (the default).
	// If provider changed away from ollama, clean up any running container.
	provider := os.Getenv("KNOWLEDGE_EMBED_PROVIDER")
	if provider != "" && provider != "ollama" {
		name := containerName("embeddings")
		if inf.isRunning(ctx, name) {
			inf.log.Info("embeddings provider changed, stopping container", "provider", provider)
			_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("embeddings"))
		}
		return nil
	}

	upstreamRef := fmt.Sprintf("%s:%s", images.OllamaUpstream, images.OllamaVersion)
	if err := images.ResolveUpstream(ctx, inf.cli, "embeddings", inf.Version, upstreamRef, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve embeddings image: %w", err)
	}
	name := containerName("embeddings")
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
	hc.NetworkMode = container.NetworkMode(gatewayNet)
	// Override memory: 3GB for model inference
	hc.Resources.Memory = 3 * 1024 * 1024 * 1024

	labels := map[string]string{
		"agency.managed":       "true",
		"agency.build.gateway": inf.BuildID,
	}

	if _, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&container.Config{
			Image:       defaultImages["embeddings"],
			Hostname:    "embeddings",
			Labels:      labels,
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
		for attempt := 0; attempt < 4; attempt++ {
			if attempt > 0 {
				time.Sleep(3 * time.Second)
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

func (inf *Infra) stopAndRemove(ctx context.Context, name string, timeoutSecs int) error {
	return containers.StopAndRemove(ctx, inf.cli, name, timeoutSecs)
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
	// Quick check — already running?
	if inf.isRunning(ctx, name) {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	eventCh, errCh := inf.cli.Events(ctx, events.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("container", name),
			filters.Arg("event", "start"),
		),
	})

	for {
		select {
		case <-eventCh:
			if inf.isRunning(ctx, name) {
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

func (inf *Infra) waitHealthy(ctx context.Context, name string, timeout time.Duration) error {
	// Quick check — already healthy?
	if info, err := inf.cli.ContainerInspect(ctx, name); err == nil {
		if info.State.Health != nil && info.State.Health.Status == "healthy" {
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	eventCh, errCh := inf.cli.Events(ctx, events.ListOptions{
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
	err := inf.cli.NetworkConnect(ctx, netName, containerID, &network.EndpointSettings{
		Aliases: aliases,
	})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		inf.log.Warn("network connect", "container", containerID, "network", netName, "err", err)
	}
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
