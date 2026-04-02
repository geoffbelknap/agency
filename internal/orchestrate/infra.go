package orchestrate

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	agencyDocker "github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/images"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/orchestrate/containers"
	"github.com/geoffbelknap/agency/internal/pkg/envfile"
	"github.com/geoffbelknap/agency/internal/services"
)

const (
	prefix          = "agency"
	mediationNet    = "agency-mediation"
	egressNet       = "agency-egress-net"
	internalNet     = "agency-internal"
)

var defaultImages = map[string]string{
	"egress":    "agency-egress:latest",
	"comms":     "agency-comms:latest",
	"knowledge": "agency-knowledge:latest",
	"intake":    "agency-intake:latest",
	"web-fetch": "agency-web-fetch:latest",
	"web":        "agency-web:latest",
	"embeddings": "agency-embeddings:latest",
}

var defaultHealthChecks = map[string]*container.HealthConfig{
	"egress": {
		Test:        []string{"CMD-SHELL", `python -c "import socket; s=socket.socket(); s.settimeout(2); s.connect(('127.0.0.1',3128)); s.close()"`},
		Interval:    2 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 1 * time.Second,
		Retries:     3,
	},
	"comms": {
		Test:        []string{"CMD-SHELL", `python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/health')"`},
		Interval:    2 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 1 * time.Second,
		Retries:     3,
	},
	"knowledge": {
		Test:        []string{"CMD-SHELL", `python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/health')"`},
		Interval:    2 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 1 * time.Second,
		Retries:     3,
	},
	"intake": {
		Test:        []string{"CMD-SHELL", `python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/health')"`},
		Interval:    2 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 1 * time.Second,
		Retries:     3,
	},
	"web-fetch": {
		Test:        []string{"CMD", "wget", "-q", "-O-", "http://127.0.0.1:8080/health"},
		Interval:    2 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 1 * time.Second,
		Retries:  3,
	},
	"web": {
		Test:        []string{"CMD", "wget", "-q", "-O-", "http://127.0.0.1:80/health"},
		Interval:    5 * time.Second,
		Timeout:     2 * time.Second,
		StartPeriod: 2 * time.Second,
		Retries:     3,
	},
	"embeddings": {
		Test:        []string{"CMD-SHELL", `bash -c "echo > /dev/tcp/127.0.0.1/11434"`},
		Interval:    5 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 5 * time.Second,
		Retries:     3,
	},
}

func containerName(role string) string {
	return fmt.Sprintf("%s-infra-%s", prefix, role)
}

// Infra manages shared infrastructure containers.
type Infra struct {
	Home         string
	Version      string
	SourceDir    string
	BuildID      string
	GatewayAddr  string // e.g. "127.0.0.1:8200"
	GatewayToken string // auth token from config.yaml
	Docker       *agencyDocker.Client
	cli        *client.Client
	log        *log.Logger
	hmacKey    []byte
}

// NewInfra creates a new infrastructure manager.
func NewInfra(home, version string, dc *agencyDocker.Client, logger *log.Logger, hmacKey []byte) (*Infra, error) {
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
	return &Infra{Home: home, Version: version, Docker: dc, cli: cli, log: logger, hmacKey: hmacKey}, nil
}

// serviceLabels returns Docker labels for service discovery.
func (inf *Infra) serviceLabels(ctx context.Context, imageRef, serviceName, port string) map[string]string {
	cname := containerName(serviceName)
	return map[string]string{
		services.LabelServiceEnabled: "true",
		services.LabelServiceName:    serviceName,
		services.LabelServicePort:    port,
		services.LabelServiceHealth:  "/health",
		services.LabelServiceNetwork: mediationNet,
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

	progress("networks", "Creating Docker networks")
	if err := inf.ensureNetworks(ctx); err != nil {
		return fmt.Errorf("ensure networks: %w", err)
	}

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
		{"embeddings", "Starting embeddings service (local vector embeddings)", inf.ensureEmbeddings},
	}

	// Start all components in parallel — they're independent.
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
	roles := []string{"web", "web-fetch", "intake", "knowledge", "comms", "egress", "embeddings"}

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
		"egress":    inf.ensureEgress,
		"comms":     inf.ensureComms,
		"knowledge": inf.ensureKnowledge,
		"intake":    inf.ensureIntake,
		"web-fetch": inf.ensureWebFetch,
		"web":        inf.ensureWeb,
		"embeddings": inf.ensureEmbeddings,
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
	// - agent-internal networks: Internal (set in EnsureAgentNetwork)
	// - agency-internal (shared): Internal — no agent workspaces attached
	// - mediation: NOT internal — enforcers need host-gateway for budget API
	// - egress-net: NOT internal — egress proxy needs internet access
	type netSpec struct {
		name     string
		internal bool
	}
	nets := []netSpec{
		{mediationNet, false},
		{egressNet, false},
		{internalNet, true},
	}
	for _, n := range nets {
		_, inspectErr := inf.cli.NetworkInspect(ctx, n.name, network.InspectOptions{})
		if inspectErr != nil {
			var err error
			if n.internal {
				err = containers.CreateInternalNetwork(ctx, inf.cli, n.name, nil)
			} else {
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

func (inf *Infra) ensureEgress(ctx context.Context) error {
	if err := images.Resolve(ctx, inf.cli, "egress", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve egress image: %w", err)
	}
	name := containerName("egress")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) {
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

	// Service keys
	serviceKeys := filepath.Join(infraDir, ".service-keys.env")
	if fileExists(serviceKeys) {
		binds = append(binds, serviceKeys+":/app/secrets/.service-keys.env:ro")
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

	// Mount the gateway Unix socket so egress can resolve credentials
	// from the credential store API. The socket is on the restricted
	// router (no auth needed — only infra endpoints exposed).
	runDir := filepath.Join(inf.Home, "run")
	if fileExists(filepath.Join(runDir, "gateway.sock")) {
		binds = append(binds, runDir+":/app/gateway-run:rw")
		env["GATEWAY_SOCKET"] = "/app/gateway-run/gateway.sock"
	}

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.Binds = binds
	hc.NetworkMode = container.NetworkMode(mediationNet)
	hc.ExtraHosts = []string{"host.docker.internal:host-gateway"}

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

	// Connect to egress network
	inf.connectIfNeeded(ctx, id, egressNet, []string{"egress"})

	return inf.waitHealthy(ctx, name, 30*time.Second)
}


func (inf *Infra) ensureComms(ctx context.Context) error {
	if err := images.Resolve(ctx, inf.cli, "comms", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve comms image: %w", err)
	}
	name := containerName("comms")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) {
		inf.ensureSystemChannels(ctx)
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("comms"))

	commsData := filepath.Join(inf.Home, "infrastructure", "comms", "data")
	os.MkdirAll(commsData, 0777)
	os.Chmod(commsData, 0777)
	agentsDir := filepath.Join(inf.Home, "agents")
	os.MkdirAll(agentsDir, 0755)

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.Binds = []string{
		commsData + ":/app/data:rw",
		agentsDir + ":/app/agents:rw",
	}
	hc.NetworkMode = container.NetworkMode(mediationNet)
	hc.ExtraHosts = []string{"host.docker.internal:host-gateway"}
	hc.PortBindings = nat.PortMap{
		"8080/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8202"}},
	}

	if _, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&container.Config{
			Image:        defaultImages["comms"],
			Hostname:     "comms",
			Labels:       inf.serviceLabels(ctx, defaultImages["comms"], "comms", "8080"),
			Healthcheck:  defaultHealthChecks["comms"],
			ExposedPorts: nat.PortSet{"8080/tcp": struct{}{}},
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
	inf.ensureSystemChannels(ctx)

	return nil
}

func (inf *Infra) ensureKnowledge(ctx context.Context) error {
	if err := images.Resolve(ctx, inf.cli, "knowledge", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve knowledge image: %w", err)
	}
	name := containerName("knowledge")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("knowledge"))

	knowledgeDir := filepath.Join(inf.Home, "knowledge", "data")
	os.MkdirAll(knowledgeDir, 0777)
	os.Chmod(knowledgeDir, 0777)

	env := map[string]string{
		"HTTPS_PROXY":          "http://egress:3128",
		"NO_PROXY":             "agency-infra-embeddings,localhost,127.0.0.1,host.docker.internal",
		"AGENCY_GATEWAY_TOKEN": inf.GatewayToken,
	}

	binds := []string{
		knowledgeDir + ":/data:rw",
	}

	// Gateway access: on Linux, mount the socket directory so containers
	// can reach the gateway via Unix socket (survives gateway restarts).
	// On macOS/Windows (Docker Desktop), Unix sockets can't be shared via
	// directory mounts (VirtioFS limitation), so use host.docker.internal TCP.
	if runtime.GOOS == "linux" {
		sockDir := filepath.Join(inf.Home, "run")
		sockPath := filepath.Join(sockDir, "gateway.sock")
		if fileExists(sockPath) {
			env["AGENCY_GATEWAY_URL"] = "http+unix:///run/agency/gateway.sock"
			binds = append(binds, sockDir+":/run/agency:ro")
		} else {
			env["AGENCY_GATEWAY_URL"] = "http://host.docker.internal:" + inf.gatewayPort()
		}
	} else {
		env["AGENCY_GATEWAY_URL"] = "http://host.docker.internal:" + inf.gatewayPort()
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
	hc.NetworkMode = container.NetworkMode(mediationNet)
	hc.ExtraHosts = []string{"host.docker.internal:host-gateway"}
	hc.PortBindings = nat.PortMap{
		"8080/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8204"}},
	}

	if _, err := containers.CreateAndStart(ctx, inf.cli,
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
	); err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}
	if err := inf.waitHealthy(ctx, name, 30*time.Second); err != nil {
		return err
	}

	// Connect knowledge to all existing agent networks
	inf.connectToAgentNetworks(ctx, name, "knowledge")
	return nil
}

func (inf *Infra) ensureIntake(ctx context.Context) error {
	if err := images.Resolve(ctx, inf.cli, "intake", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve intake image: %w", err)
	}
	name := containerName("intake")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("intake"))

	intakeDir := filepath.Join(inf.Home, "infrastructure", "intake", "data")
	os.MkdirAll(intakeDir, 0777)
	os.Chmod(intakeDir, 0777)
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
		"NO_PROXY":      "comms,knowledge,localhost,127.0.0.1",
		"KNOWLEDGE_URL": "http://knowledge:8080",
	}

	// Load operator config vars (LC_ORG_ID, etc.) from config.yaml and .env (legacy)
	for k, v := range envfile.Load(filepath.Join(inf.Home, ".env")) {
		env[k] = v
	}
	intakeCfg := config.Load()
	for k, v := range intakeCfg.ConfigVars {
		env[k] = v
	}

	// Load service keys
	serviceKeys := filepath.Join(inf.Home, "infrastructure", ".service-keys.env")
	if fileExists(serviceKeys) {
		for k, v := range envfile.Load(serviceKeys) {
			env[k] = v
		}
	}

	// Egress CA cert
	egressCA := filepath.Join(inf.Home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	if fileExists(egressCA) {
		binds = append(binds, egressCA+":/app/egress-ca.pem:ro")
		env["EGRESS_CA_CERT"] = "/app/egress-ca.pem"
	}

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.Binds = binds
	hc.NetworkMode = container.NetworkMode(mediationNet)
	hc.ExtraHosts = []string{"host.docker.internal:host-gateway"}
	hc.Resources.Memory = 128 * 1024 * 1024 // 128MB — intake is lightweight
	hc.PortBindings = nat.PortMap{
		"8080/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8205"}},
	}

	if _, err := containers.CreateAndStart(ctx, inf.cli,
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
	); err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}
	return inf.waitHealthy(ctx, name, 30*time.Second)
}

func (inf *Infra) ensureWebFetch(ctx context.Context) error {
	if err := images.Resolve(ctx, inf.cli, "web-fetch", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve web-fetch image: %w", err)
	}
	name := containerName("web-fetch")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) {
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
		"HTTP_PROXY":  "http://egress:3128",
		"HTTPS_PROXY": "http://egress:3128",
		"NO_PROXY":    "comms,knowledge,localhost,127.0.0.1",
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
	hc.NetworkMode = container.NetworkMode(mediationNet)
	hc.PortBindings = nat.PortMap{
		"8080/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8206"}},
	}

	if _, err := containers.CreateAndStart(ctx, inf.cli,
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
	); err != nil {
		return err
	}

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}
	return inf.waitHealthy(ctx, name, 30*time.Second)
}

func (inf *Infra) ensureWeb(ctx context.Context) error {
	// agency-web lives outside agency_core/, so image resolution only checks
	// if the image exists locally. Build via `make web` or the image resolver
	// will pull from GHCR in release mode.
	if err := images.Resolve(ctx, inf.cli, "web", inf.Version, "", inf.BuildID, inf.log); err != nil {
		inf.log.Warn("agency-web image not available, skipping", "err", err)
		return nil // non-fatal — web UI is optional
	}
	name := containerName("web")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("web"))

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.NetworkMode = "bridge"
	hc.ExtraHosts = []string{"host.docker.internal:host-gateway"}
	hc.ReadonlyRootfs = true
	hc.Tmpfs = map[string]string{
		"/var/cache/nginx": "rw,noexec,nosuid,size=16m",
		"/var/run":         "rw,noexec,nosuid,size=1m",
		"/tmp":             "rw,noexec,nosuid,size=1m",
	}
	hc.Resources.Memory = 64 * 1024 * 1024     // 64MB — nginx serving static files
	hc.Resources.NanoCPUs = 500_000_000         // 0.5 CPU
	pidsLimit := int64(64)
	hc.Resources.PidsLimit = &pidsLimit
	hc.PortBindings = nat.PortMap{
		"80/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8280"}},
	}

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
			ExposedPorts: nat.PortSet{"80/tcp": struct{}{}},
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

func (inf *Infra) ensureEmbeddings(ctx context.Context) error {
	// Conditional: only start if embedding provider is ollama (the default)
	provider := os.Getenv("KNOWLEDGE_EMBED_PROVIDER")
	if provider != "" && provider != "ollama" {
		inf.log.Info("embeddings container skipped", "provider", provider)
		return nil
	}

	upstreamRef := fmt.Sprintf("%s:%s", images.OllamaUpstream, images.OllamaVersion)
	if err := images.ResolveUpstream(ctx, inf.cli, "embeddings", inf.Version, upstreamRef, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve embeddings image: %w", err)
	}
	name := containerName("embeddings")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) {
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
	hc.NetworkMode = container.NetworkMode(mediationNet)
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

func (inf *Infra) ensureSystemChannels(ctx context.Context) {
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
		_, err := inf.Docker.CommsRequest(ctx, "POST", "/channels", body)
		if err != nil {
			inf.log.Debug("channel create", "channel", ch.name, "err", err)
		}
	}
}

// -- Helpers --

func (inf *Infra) isRunning(ctx context.Context, name string) bool {
	info, err := inf.cli.ContainerInspect(ctx, name)
	if err != nil {
		return false
	}
	return info.State.Running
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

func (inf *Infra) connectIfNeeded(ctx context.Context, containerID, netName string, aliases []string) {
	err := inf.cli.NetworkConnect(ctx, netName, containerID, &network.EndpointSettings{
		Aliases: aliases,
	})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		inf.log.Warn("network connect", "container", containerID, "network", netName, "err", err)
	}
}

func (inf *Infra) connectToAgentNetworks(ctx context.Context, containerName, alias string) {
	nets, err := inf.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", "agency-")),
	})
	if err != nil {
		return
	}

	ctr, err := inf.cli.ContainerInspect(ctx, containerName)
	if err != nil {
		return
	}
	connected := ctr.NetworkSettings.Networks

	for _, n := range nets {
		if strings.HasSuffix(n.Name, "-internal") && n.Name != internalNet {
			if _, ok := connected[n.Name]; !ok {
				inf.connectIfNeeded(ctx, ctr.ID, n.Name, []string{alias})
			}
		}
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
