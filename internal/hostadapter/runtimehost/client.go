package runtimehost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	dockerimage "github.com/docker/docker/api/types/image"
	dockernetwork "github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"

	"github.com/geoffbelknap/agency/internal/comms"
	"github.com/geoffbelknap/agency/internal/infratier"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

type Client struct {
	cli     *RawClient
	backend string
}

type (
	BackendHandle       = Client
	ContainerState      = dockercontainer.Summary
	ListOptions         = dockercontainer.ListOptions
	FilterArgs          = dockerfilters.Args
	ImageSummary        = dockerimage.Summary
	ImageListOptions    = dockerimage.ListOptions
	ImageRemoveOptions  = dockerimage.RemoveOptions
	ImageDeleteResponse = dockerimage.DeleteResponse
	ImagePullOptions    = dockerimage.PullOptions
	ImageBuildOptions   = dockertypes.ImageBuildOptions
	ImageInspect        = dockerimage.InspectResponse
	ImageBuildResponse  = dockertypes.ImageBuildResponse
	InfraComponent      struct {
		Name        string `json:"name"`
		State       string `json:"state"`
		Health      string `json:"health"`
		BuildID     string `json:"build_id,omitempty"`
		ComponentID string `json:"component_id,omitempty"`
		ContainerID string `json:"container_id,omitempty"`
		Uptime      string `json:"uptime,omitempty"`
	}
	ContainerInspect struct {
		Name     string
		State    string
		Health   string
		Env      []string
		Mounts   []MountInfo
		Networks []string
		Labels   map[string]string
	}
	MountInfo struct {
		Source      string
		Destination string
		RW          bool
	}
	Status struct {
		available   atomic.Bool
		OnReconnect func()
	}
)

const (
	BackendDocker         = "docker"
	BackendPodman         = "podman"
	BackendContainerd     = "containerd"
	BackendAppleContainer = "apple-container"
)

func NormalizeContainerBackend(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	switch name {
	case "", BackendDocker:
		return BackendDocker
	case BackendPodman, BackendContainerd, BackendAppleContainer:
		return name
	case "apple", "container", "macos-container":
		return BackendAppleContainer
	default:
		return name
	}
}

func IsContainerBackend(name string) bool {
	switch NormalizeContainerBackend(name) {
	case BackendDocker, BackendPodman, BackendContainerd, BackendAppleContainer:
		return true
	default:
		return false
	}
}

// RequiresCreateTimeNetworkTopology reports whether a backend needs complete
// network membership declared before container creation. Docker-compatible
// backends can attach networks after create; containerd/nerdctl and Apple
// Container need the desired topology realized up front.
func RequiresCreateTimeNetworkTopology(backend string) bool {
	switch NormalizeContainerBackend(backend) {
	case BackendContainerd, BackendAppleContainer:
		return true
	default:
		return false
	}
}

func HostGatewayAliases(backend string) []string {
	switch NormalizeContainerBackend(backend) {
	case BackendPodman:
		return []string{"host.containers.internal", "host.docker.internal"}
	default:
		// apple-container falls into this default branch and has not been
		// verified to resolve either alias under Apple's container runtime.
		// See "Apple Container Open Items" in
		// tests/checklists/backend-adapter-release-checklist.md.
		return []string{"host.docker.internal", "host.containers.internal"}
	}
}

func HostGatewayAliasesEnv(backend string) string {
	return strings.Join(HostGatewayAliases(backend), ",")
}

var _ comms.Client = (*Client)(nil)

func (c *Client) Backend() string {
	if c == nil || strings.TrimSpace(c.backend) == "" {
		return BackendDocker
	}
	return NormalizeContainerBackend(c.backend)
}

func desktopDockerHost() string {
	if os.Getenv("DOCKER_HOST") != "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	socketPath := filepath.Join(home, ".docker", "run", "docker.sock")
	if _, err := os.Stat(socketPath); err == nil {
		return "unix://" + socketPath
	}
	return ""
}

func defaultPodmanHost() string {
	if host := strings.TrimSpace(os.Getenv("PODMAN_HOST")); host != "" {
		return host
	}
	if host := strings.TrimSpace(os.Getenv("CONTAINER_HOST")); host != "" {
		return host
	}
	if host := strings.TrimSpace(os.Getenv("DOCKER_HOST")); host != "" {
		return host
	}

	var candidates []string
	if runtime.GOOS == "linux" {
		if xdg := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); xdg != "" {
			candidates = append(candidates, filepath.Join(xdg, "podman", "podman.sock"))
		}
		if currentUser, err := user.Current(); err == nil && strings.TrimSpace(currentUser.Uid) != "" {
			candidates = append(candidates, filepath.Join("/run/user", currentUser.Uid, "podman", "podman.sock"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates,
			filepath.Join(home, ".local", "share", "containers", "podman", "machine", "podman.sock"),
			filepath.Join(home, ".config", "containers", "podman", "machine", "podman.sock"),
		)
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return hostFromPath(candidate)
		}
	}
	return ""
}

func EnsureUsableHostEnv() string {
	return EnsureUsableHostEnvForBackend(BackendDocker, nil)
}

func EnsureUsableHostEnvForBackend(backend string, backendConfig map[string]string) string {
	if NormalizeContainerBackend(backend) != BackendDocker {
		return resolveBackendHost(backend, backendConfig)
	}
	if host := resolveBackendHost(backend, backendConfig); host != "" {
		_ = os.Setenv("DOCKER_HOST", host)
		return host
	}
	return os.Getenv("DOCKER_HOST")
}

func ResolvedBackendEndpoint(backend string, backendConfig map[string]string) string {
	return resolveBackendHost(backend, backendConfig)
}

func ResolvedBackendMode(backend string, backendConfig map[string]string) string {
	return backendModeFromEndpoint(backend, resolveBackendHost(backend, backendConfig))
}

func ValidateBackendConfig(backend string, backendConfig map[string]string) error {
	backend = NormalizeContainerBackend(backend)
	if backend == BackendAppleContainer {
		if err := validateAppleContainerPlatform(runtime.GOOS, runtime.GOARCH); err != nil {
			return err
		}
		return validateAppleContainerConfig(backendConfig)
	}
	if backend != BackendContainerd || backendConfig == nil {
		return nil
	}
	if host := strings.TrimSpace(backendConfig["host"]); host != "" {
		return fmt.Errorf("containerd backend does not accept generic host config; use native_socket or address for the native containerd socket")
	}
	if socket := strings.TrimSpace(backendConfig["socket"]); socket != "" {
		return fmt.Errorf("containerd backend does not accept generic socket config; use native_socket or address for the native containerd socket")
	}
	endpoint := resolveContainerdHost(backendConfig)
	return validateContainerdAddress(endpoint)
}

func NewClient() (*Client, error) {
	return NewClientForBackend(BackendDocker, nil)
}

func NewClientForBackend(backend string, backendConfig map[string]string) (*Client, error) {
	backend = NormalizeContainerBackend(backend)
	cli, err := newRawClientForBackend(backend, backendConfig)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to %s: %w", backend, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("%s not responding: %w", backend, err)
	}
	return &Client{cli: cli, backend: backend}, nil
}

func TryNewClient(logger interface {
	Warn(msg string, keyvals ...any)
}) *Client {
	return TryNewClientForBackend(BackendDocker, nil, logger)
}

func TryNewClientForBackend(backend string, backendConfig map[string]string, logger interface {
	Warn(msg string, keyvals ...any)
}) *Client {
	backend = NormalizeContainerBackend(backend)
	cli, err := newRawClientForBackend(backend, backendConfig)
	if err != nil {
		logger.Warn("container backend client unavailable", "backend", backend, "err", err)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		logger.Warn("container backend not responding", "backend", backend, "err", err)
		return nil
	}
	return &Client{cli: cli, backend: backend}
}

func NewRawClient() (*RawClient, error) {
	return NewRawClientForBackend(BackendDocker, nil)
}

func NewRawClientForBackend(backend string, backendConfig map[string]string) (*RawClient, error) {
	return newRawClientForBackend(backend, backendConfig)
}

func AppleContainerStatus(ctx context.Context, backendConfig map[string]string) error {
	cli, err := newAppleContainerRawClient(backendConfig)
	if err != nil {
		return err
	}
	_, err = cli.Ping(ctx)
	return err
}

func newRawClientForBackend(backend string, backendConfig map[string]string) (*RawClient, error) {
	backend = NormalizeContainerBackend(backend)
	if !IsContainerBackend(backend) {
		return nil, fmt.Errorf("unsupported container backend %q", backend)
	}
	if backend == BackendAppleContainer {
		return newAppleContainerRawClient(backendConfig)
	}
	if backend == BackendContainerd {
		return newNerdctlRawClient(backendConfig)
	}
	return newDockerRawClient(backend, resolveBackendHost(backend, backendConfig))
}

func resolveBackendHost(backend string, backendConfig map[string]string) string {
	backend = NormalizeContainerBackend(backend)
	if backend == BackendContainerd {
		return resolveContainerdHost(backendConfig)
	}
	if backend == BackendAppleContainer {
		return "container://local"
	}
	if backendConfig != nil {
		if host := strings.TrimSpace(backendConfig["host"]); host != "" {
			return hostFromPath(host)
		}
		if socket := strings.TrimSpace(backendConfig["socket"]); socket != "" {
			return hostFromPath(socket)
		}
	}
	switch backend {
	case BackendPodman:
		return defaultPodmanHost()
	case BackendDocker:
		if host := desktopDockerHost(); host != "" {
			return host
		}
		return strings.TrimSpace(os.Getenv("DOCKER_HOST"))
	default:
		return ""
	}
}

func resolveContainerdHost(backendConfig map[string]string) string {
	if backendConfig != nil {
		for _, key := range []string{"native_socket", "address", "containerd_address"} {
			if value := strings.TrimSpace(backendConfig[key]); value != "" {
				return hostFromPath(value)
			}
		}
	}
	if host := strings.TrimSpace(os.Getenv("CONTAINERD_HOST")); host != "" {
		return hostFromPath(host)
	}
	if host := strings.TrimSpace(os.Getenv("CONTAINER_HOST")); host != "" {
		return hostFromPath(host)
	}
	return "unix:///run/containerd/containerd.sock"
}

func backendModeFromEndpoint(backend, endpoint string) string {
	backend = NormalizeContainerBackend(backend)
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	path := strings.TrimPrefix(endpoint, "unix://")
	switch backend {
	case BackendContainerd:
		switch {
		case strings.HasPrefix(path, "/run/user/"):
			return "rootless"
		case strings.HasPrefix(path, "/run/containerd/"):
			return "rootful"
		default:
			return "unknown"
		}
	case BackendAppleContainer:
		return "macos-vm"
	case BackendPodman:
		switch {
		case strings.HasPrefix(path, "/run/user/"):
			return "rootless"
		case strings.HasPrefix(path, "/run/podman/"), strings.HasPrefix(path, "/var/run/podman/"):
			return "rootful"
		default:
			return ""
		}
	default:
		return ""
	}
}

func hostFromPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		return value
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, ".") {
		return "unix://" + value
	}
	return value
}

func IsErrNotFound(err error) bool {
	if err == nil {
		return false
	}
	if dockerclient.IsErrNotFound(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no such image") ||
		strings.Contains(msg, "no such object") ||
		strings.Contains(msg, "no such container") ||
		strings.Contains(msg, "no such network")
}

func NewStatus(dc *Client) *Status {
	s := &Status{}
	s.available.Store(dc != nil)
	return s
}

func (s *Status) Available() bool { return s.available.Load() }
func (s *Status) RecordSuccess() {
	was := s.available.Swap(true)
	if !was && s.OnReconnect != nil {
		s.OnReconnect()
	}
}
func (s *Status) RecordError(err error) {
	if err == nil {
		return
	}
	if isContainerBackendUnavailable(err) {
		s.available.Store(false)
	}
}

func isContainerBackendUnavailable(err error) bool {
	msg := strings.ToLower(err.Error())
	patterns := []string{
		"cannot connect to the docker daemon",
		"connection refused",
		"no such host",
		"i/o timeout",
		"context deadline exceeded",
		"docker not responding",
		"containerd not responding",
		"nerdctl",
		"container system status",
		"dial unix",
		"executable file not found",
		"no such file or directory",
	}
	for _, p := range patterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func FilterArg(key, value string) dockerfilters.KeyValuePair { return dockerfilters.Arg(key, value) }
func NewFilterArgs(args ...dockerfilters.KeyValuePair) FilterArgs {
	return dockerfilters.NewArgs(args...)
}

func IsGatewayMediationNetwork(name string) bool {
	return strings.HasPrefix(name, "agency-gateway") || strings.Contains(name, "mediation")
}

func EnforcerHasOperatorOverridePath(networks []string) bool {
	for _, net := range networks {
		if IsGatewayMediationNetwork(net) {
			return true
		}
	}
	return false
}

func EnforcerUnexpectedExternalNetworks(networks []string) []string {
	var unexpected []string
	for _, net := range networks {
		if strings.HasPrefix(net, "agency-operator") || strings.HasPrefix(net, "agency-egress-ext") {
			unexpected = append(unexpected, net)
		}
	}
	return unexpected
}

type AgentStatus struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Workspace string `json:"workspace"`
	Enforcer  string `json:"enforcer"`
}

func (c *Client) ListAgents(ctx context.Context) ([]AgentStatus, error) {
	containers, err := c.cli.ContainerList(ctx, dockercontainer.ListOptions{
		All: true,
		Filters: dockerfilters.NewArgs(
			dockerfilters.Arg("label", "agency.type=workspace"),
		),
	})
	if err != nil {
		return nil, err
	}
	var agents []AgentStatus
	for _, ctr := range containers {
		name := extractAgentName(ctr.Names)
		if name == "" {
			continue
		}
		a := AgentStatus{Name: name, Workspace: ctr.State}
		enforcer, _ := c.containerState(ctx, prefix+"-"+name+"-enforcer")
		a.Enforcer = enforcer
		switch {
		case ctr.State == "running":
			a.Status = "running"
		case ctr.State == "paused":
			a.Status = "paused"
		default:
			a.Status = "stopped"
		}
		agents = append(agents, a)
	}
	return agents, nil
}

func (c *Client) InfraStatus(ctx context.Context) ([]InfraComponent, error) {
	components := infratier.StatusComponents()
	containers, err := c.cli.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true,
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("label", "agency.role=infra")),
	})
	if err != nil {
		var result []InfraComponent
		for _, comp := range components {
			result = append(result, InfraComponent{Name: comp, State: "missing", Health: "none"})
		}
		return result, nil
	}
	stateMap := make(map[string]InfraComponent)
	for _, ctr := range containers {
		for _, n := range ctr.Names {
			n = strings.TrimPrefix(n, "/")
			for _, comp := range components {
				expected := infraContainerName(comp)
				if n == expected {
					id := shortContainerID(ctr.ID)
					ic := InfraComponent{Name: comp, State: ctr.State, ComponentID: id, ContainerID: id, Uptime: formatContainerUptime(ctr.Created, ctr.State, ctr.Status)}
					ic.Health = infraHealthFromStatus(ctr.Status)
					if ic.Health == "none" {
						if inspect, err := c.cli.ContainerInspect(ctx, expected); err == nil && inspect.State != nil && inspect.State.Health != nil && inspect.State.Health.Status != "" {
							ic.Health = inspect.State.Health.Status
						}
					}
					if bid, ok := ctr.Labels["agency.build.gateway"]; ok {
						ic.BuildID = bid
					}
					stateMap[comp] = ic
				}
			}
		}
	}
	var result []InfraComponent
	for _, comp := range components {
		if ic, ok := stateMap[comp]; ok {
			result = append(result, ic)
		} else {
			result = append(result, InfraComponent{Name: comp, State: "missing", Health: "none"})
		}
	}
	return result, nil
}

func infraHealthFromStatus(status string) string {
	status = strings.ToLower(status)
	if strings.Contains(status, "unhealthy") {
		return "unhealthy"
	}
	if strings.Contains(status, "healthy") {
		return "healthy"
	}
	return "none"
}

func (c *Client) InfraLogs(ctx context.Context, component string, tail int) (string, error) {
	component = strings.TrimSpace(component)
	if !validInfraComponent(component) {
		return "", fmt.Errorf("unknown infrastructure component %q", component)
	}
	if c == nil || c.cli == nil {
		return "", fmt.Errorf("container client unavailable")
	}
	return c.cli.ContainerLogs(ctx, infraContainerName(component), tail)
}

func validInfraComponent(component string) bool {
	for _, candidate := range infratier.StatusComponents() {
		if component == candidate {
			return true
		}
	}
	return false
}

func infraContainerName(component string) string {
	name := prefix + "-infra-" + component
	instance := strings.Trim(strings.ToLower(strings.TrimSpace(os.Getenv("AGENCY_INFRA_INSTANCE"))), "-")
	if instance != "" {
		name += "-" + instance
	}
	return name
}

func shortContainerID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func formatContainerUptime(created int64, state, status string) string {
	if state != "running" {
		return ""
	}
	if strings.HasPrefix(status, "Up ") {
		uptime := strings.TrimPrefix(status, "Up ")
		if idx := strings.Index(uptime, " ("); idx >= 0 {
			uptime = uptime[:idx]
		}
		return strings.TrimSpace(uptime)
	}
	if created <= 0 {
		return ""
	}
	duration := time.Since(time.Unix(created, 0)).Round(time.Second)
	if duration < 0 {
		return ""
	}
	return duration.String()
}

func (c *Client) Exec(ctx context.Context, ref runtimecontract.InstanceRef, cmd []string) (string, error) {
	if c == nil || c.cli == nil {
		return "", fmt.Errorf("runtime backend client unavailable")
	}
	instanceName := runtimeContainerNameFor(ref)
	return c.cli.Exec(ctx, instanceName, "", cmd)
}

func (c *Client) Signal(ctx context.Context, ref runtimecontract.InstanceRef, signal string) error {
	if c == nil || c.cli == nil {
		return fmt.Errorf("runtime backend client unavailable")
	}
	instanceName := runtimeContainerNameFor(ref)
	return c.cli.ContainerKill(ctx, instanceName, signal)
}

func (c *Client) CommsRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	port := os.Getenv("AGENCY_GATEWAY_PROXY_PORT")
	if port == "" {
		port = "8202"
	}
	url := "http://localhost:" + port + path
	httpClient := &http.Client{Timeout: 15 * time.Second}
	var req *http.Request
	var err error
	if body != nil && (method == "POST" || method == "PUT" || method == "DELETE") {
		jsonBody, _ := json.Marshal(body)
		req, err = http.NewRequestWithContext(ctx, method, url, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("comms request %s %s: %w", method, path, err)
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, fmt.Errorf("comms request %s %s: %w", method, path, err)
		}
	}
	if strings.Contains(path, "grant-access") || strings.Contains(path, "archive") {
		req.Header.Set("X-Agency-Platform", "true")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("comms request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("comms request %s %s: read body: %w", method, path, err)
	}
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("comms returned %d: %s", resp.StatusCode, string(out))
	}
	return out, nil
}

func (c *Client) InspectContainer(ctx context.Context, name string) (*ContainerInspect, error) {
	info, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return nil, err
	}
	ci := &ContainerInspect{Name: name, State: info.State.Status}
	if info.State.Health != nil {
		ci.Health = info.State.Health.Status
	} else {
		ci.Health = "none"
	}
	if info.Config != nil {
		ci.Env = info.Config.Env
		ci.Labels = info.Config.Labels
	}
	for _, m := range info.Mounts {
		ci.Mounts = append(ci.Mounts, MountInfo{Source: m.Source, Destination: m.Destination, RW: m.RW})
	}
	ci.Networks = c.containerNetworkNames(ctx, info)
	return ci, nil
}

func (c *Client) containerNetworkNames(ctx context.Context, info dockercontainer.InspectResponse) []string {
	resolve := func(networkID string) string {
		if strings.TrimSpace(networkID) == "" || c == nil || c.cli == nil {
			return ""
		}
		inspect, err := c.cli.NetworkInspect(ctx, networkID, dockernetwork.InspectOptions{})
		if err != nil {
			return ""
		}
		return strings.TrimSpace(inspect.Name)
	}
	var networks map[string]*dockernetwork.EndpointSettings
	if info.NetworkSettings != nil {
		networks = info.NetworkSettings.Networks
	}
	names := normalizeContainerNetworkNames(networks, resolve)
	if len(names) > 0 && hasNonSyntheticNetworkName(names) {
		return names
	}
	if fromLabels := containerNetworkNamesFromLabels(info.Config); len(fromLabels) > 0 {
		return fromLabels
	}
	return names
}

func normalizeContainerNetworkNames(networks map[string]*dockernetwork.EndpointSettings, resolve func(string) string) []string {
	if len(networks) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(networks))
	for key, endpoint := range networks {
		name := strings.TrimSpace(key)
		if looksLikeSyntheticNetworkName(name) && endpoint != nil {
			if resolved := resolve(endpoint.NetworkID); resolved != "" {
				name = resolved
			}
		}
		if name == "" && endpoint != nil {
			name = resolve(endpoint.NetworkID)
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func looksLikeSyntheticNetworkName(name string) bool {
	if name == "" {
		return true
	}
	return strings.HasPrefix(name, "unknown-") || strings.HasPrefix(name, "eth")
}

func hasNonSyntheticNetworkName(names []string) bool {
	for _, name := range names {
		if !looksLikeSyntheticNetworkName(name) {
			return true
		}
	}
	return false
}

func containerNetworkNamesFromLabels(config *dockercontainer.Config) []string {
	if config == nil || config.Labels == nil {
		return nil
	}
	raw := strings.TrimSpace(config.Labels["nerdctl/networks"])
	if raw == "" {
		return nil
	}
	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil {
		return nil
	}
	out := make([]string, 0, len(names))
	seen := map[string]struct{}{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (c *Client) ListAgentWorkspaces(ctx context.Context) ([]string, error) {
	containers, err := c.cli.ContainerList(ctx, dockercontainer.ListOptions{
		All: false,
		Filters: dockerfilters.NewArgs(
			dockerfilters.Arg("label", "agency.type=workspace"),
		),
	})
	if err != nil {
		return nil, err
	}
	var agents []string
	for _, ctr := range containers {
		name := extractAgentName(ctr.Names)
		if name != "" {
			agents = append(agents, name)
		}
	}
	return agents, nil
}

func (c *Client) containerState(ctx context.Context, name string) (string, error) {
	info, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return "missing", err
	}
	return info.State.Status, nil
}

func (c *Client) ShortID(ctx context.Context, ref runtimecontract.InstanceRef) string {
	name := runtimeContainerNameFor(ref)
	info, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return ""
	}
	if len(info.ID) >= 12 {
		return info.ID[:12]
	}
	return info.ID
}

func runtimeContainerNameFor(ref runtimecontract.InstanceRef) string {
	return fmt.Sprintf("%s-%s-%s", prefix, ref.RuntimeID, ref.Role)
}

func (c *Client) RawClient() *RawClient {
	if c == nil {
		return nil
	}
	return c.cli
}

func (c *Client) ContainerInspectRaw(ctx context.Context, name string) (*dockercontainer.InspectResponse, error) {
	info, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) ListAgencyContainers(ctx context.Context, runningOnly bool) ([]dockercontainer.Summary, error) {
	opts := dockercontainer.ListOptions{All: !runningOnly, Filters: dockerfilters.NewArgs(dockerfilters.Arg("label", "agency.managed=true"))}
	containers, err := c.cli.ContainerList(ctx, opts)
	if err != nil {
		return nil, err
	}
	var result []dockercontainer.Summary
	for _, ctr := range containers {
		for k := range ctr.Labels {
			if strings.HasPrefix(k, "agency.") {
				result = append(result, ctr)
				break
			}
		}
	}
	return result, nil
}

func (c *Client) ListNetworksByLabel(ctx context.Context, label string) ([]dockernetwork.Summary, error) {
	return c.cli.NetworkList(ctx, dockernetwork.ListOptions{Filters: dockerfilters.NewArgs(dockerfilters.Arg("label", label))})
}

func (c *Client) NetworkInspectRaw(ctx context.Context, name string) (*dockernetwork.Inspect, error) {
	info, err := c.cli.NetworkInspect(ctx, name, dockernetwork.InspectOptions{})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) ListAgencyImages(ctx context.Context) ([]dockerimage.Summary, error) {
	return c.cli.ImageList(ctx, dockerimage.ListOptions{Filters: dockerfilters.NewArgs(dockerfilters.Arg("reference", "agency-*"))})
}

func (c *Client) ListDanglingAgencyImages(ctx context.Context) ([]dockerimage.Summary, error) {
	imgs, err := c.cli.ImageList(ctx, dockerimage.ListOptions{Filters: dockerfilters.NewArgs(dockerfilters.Arg("dangling", "true"))})
	if err != nil {
		return nil, err
	}
	var result []dockerimage.Summary
	for _, img := range imgs {
		if img.Labels == nil {
			continue
		}
		if _, ok := img.Labels["agency.build.id"]; ok {
			result = append(result, img)
		}
	}
	return result, nil
}

func (c *Client) RemoveImage(ctx context.Context, imageRef string) ([]dockerimage.DeleteResponse, error) {
	return c.cli.ImageRemove(ctx, imageRef, dockerimage.RemoveOptions{PruneChildren: true})
}

func (c *Client) LogFileSize(ctx context.Context, name string) (int64, error) {
	info, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return 0, err
	}
	if info.LogPath == "" {
		return 0, nil
	}
	fi, err := os.Stat(info.LogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return fi.Size(), nil
}

func extractAgentName(names []string) string {
	for _, n := range names {
		n = strings.TrimPrefix(n, "/")
		if strings.HasPrefix(n, prefix+"-") && strings.HasSuffix(n, "-workspace") {
			name := strings.TrimPrefix(n, prefix+"-")
			name = strings.TrimSuffix(name, "-workspace")
			if name == "infra" {
				continue
			}
			return name
		}
	}
	return ""
}

func (c *Client) WaitRunning(ctx context.Context, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if info, err := c.cli.ContainerInspect(ctx, name); err == nil && info.State != nil {
			if info.State.Running {
				return nil
			}
			if info.State.Status == "exited" || info.State.Status == "dead" {
				return fmt.Errorf("container %s exited before becoming running", name)
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("container %s did not start within %v", name, timeout)
		}
	}
}

func (c *Client) WaitHealthy(ctx context.Context, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if info, err := c.cli.ContainerInspect(ctx, name); err == nil && info.State != nil {
			if info.State.Health == nil && info.State.Running {
				return nil
			}
			if info.State.Health != nil && info.State.Health.Status == "healthy" {
				return nil
			}
			if info.State.Status == "exited" || info.State.Status == "dead" {
				return fmt.Errorf("container %s exited before becoming healthy", name)
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("container %s did not become healthy within %v", name, timeout)
		}
	}
}
