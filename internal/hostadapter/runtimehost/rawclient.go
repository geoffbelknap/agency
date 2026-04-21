package runtimehost

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	dockertypes "github.com/docker/docker/api/types"
	dockercontainer "github.com/docker/docker/api/types/container"
	dockerevents "github.com/docker/docker/api/types/events"
	dockerfilters "github.com/docker/docker/api/types/filters"
	dockerimage "github.com/docker/docker/api/types/image"
	dockernetwork "github.com/docker/docker/api/types/network"
	dockervolume "github.com/docker/docker/api/types/volume"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
)

type nerdctlConfig struct {
	address   string
	namespace string
	dataRoot  string
}

type appleContainerConfig struct {
	binary string
	run    func(context.Context, ...string) ([]byte, []byte, error)
}

type appleContainerInspect struct {
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	Networks  []struct {
		Address     string `json:"address"`
		IPv4Address string `json:"ipv4Address"`
		IPv6Address string `json:"ipv6Address"`
		Gateway     string `json:"gateway"`
		IPv4Gateway string `json:"ipv4Gateway"`
		Hostname    string `json:"hostname"`
		Network     string `json:"network"`
	} `json:"networks"`
	Configuration struct {
		ID       string `json:"id"`
		Hostname string `json:"hostname"`
		Image    string `json:"image"`
		ImageRef struct {
			Reference string `json:"reference"`
		} `json:"image"`
		Labels map[string]string `json:"labels"`
		Env    []string          `json:"env"`
		Init   struct {
			Environment []string `json:"environment"`
		} `json:"initProcess"`
		Mounts []struct {
			Source      string `json:"source"`
			Target      string `json:"target"`
			Destination string `json:"destination"`
			ReadOnly    bool   `json:"readonly"`
			Readonly    bool   `json:"readOnly"`
		} `json:"mounts"`
		Resources struct {
			MemoryInBytes int64 `json:"memoryInBytes"`
		} `json:"resources"`
	} `json:"configuration"`
}

// RawClient is Agency's owned container-runtime client wrapper. Docker and
// Podman delegate to the Docker-compatible SDK. Containerd uses nerdctl so the
// public backend remains `containerd` without depending on a Docker API socket.
type RawClient struct {
	backend        string
	endpoint       string
	docker         *dockerclient.Client
	nerdctl        *nerdctlConfig
	appleContainer *appleContainerConfig
}

func containerdSocketTypeError(address string) error {
	return fmt.Errorf("containerd backend requires a native containerd socket, not a Docker-compatible API socket: %s", address)
}

func validateContainerdAddress(address string) error {
	trimmed := strings.TrimSpace(strings.TrimPrefix(address, "unix://"))
	if trimmed == "" {
		return nil
	}
	switch {
	case strings.HasSuffix(trimmed, "/api.sock"),
		strings.HasSuffix(trimmed, "/docker.sock"),
		strings.HasSuffix(trimmed, "/podman.sock"):
		return containerdSocketTypeError(address)
	default:
		return nil
	}
}

func newDockerRawClient(backend, host string) (*RawClient, error) {
	opts := []dockerclient.Opt{dockerclient.WithAPIVersionNegotiation()}
	if host != "" {
		opts = append(opts, dockerclient.WithHost(host))
	} else {
		opts = append(opts, dockerclient.FromEnv)
	}
	cli, err := dockerclient.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &RawClient{backend: backend, endpoint: hostFromPath(host), docker: cli}, nil
}

func newNerdctlRawClient(backendConfig map[string]string) (*RawClient, error) {
	cfg := &nerdctlConfig{
		address:   resolveBackendHost(BackendContainerd, backendConfig),
		namespace: strings.TrimSpace(os.Getenv("CONTAINERD_NAMESPACE")),
		dataRoot:  strings.TrimSpace(os.Getenv("NERDCTL_DATA_ROOT")),
	}
	if backendConfig != nil {
		if namespace := strings.TrimSpace(backendConfig["namespace"]); namespace != "" {
			cfg.namespace = namespace
		}
		if dataRoot := strings.TrimSpace(backendConfig["data_root"]); dataRoot != "" {
			cfg.dataRoot = dataRoot
		}
	}
	if cfg.namespace == "" {
		cfg.namespace = "default"
	}
	if err := validateContainerdAddress(cfg.address); err != nil {
		return nil, err
	}
	return &RawClient{backend: BackendContainerd, endpoint: cfg.address, nerdctl: cfg}, nil
}

func validateAppleContainerPlatform(goos, goarch string) error {
	if goos != "darwin" || goarch != "arm64" {
		return fmt.Errorf("apple-container backend requires macOS on Apple silicon")
	}
	return nil
}

func validateAppleContainerConfig(backendConfig map[string]string) error {
	if backendConfig == nil {
		return nil
	}
	for _, key := range []string{"host", "socket", "native_socket", "address", "containerd_address", "namespace", "data_root"} {
		if strings.TrimSpace(backendConfig[key]) != "" {
			return fmt.Errorf("apple-container backend does not accept %q config; it uses the local Apple container CLI service", key)
		}
	}
	return nil
}

func newAppleContainerRawClient(backendConfig map[string]string) (*RawClient, error) {
	if err := validateAppleContainerPlatform(runtime.GOOS, runtime.GOARCH); err != nil {
		return nil, err
	}
	if err := validateAppleContainerConfig(backendConfig); err != nil {
		return nil, err
	}
	binary := strings.TrimSpace(os.Getenv("AGENCY_APPLE_CONTAINER_BIN"))
	if backendConfig != nil {
		if configured := strings.TrimSpace(backendConfig["binary"]); configured != "" {
			binary = configured
		}
	}
	if binary == "" {
		binary = "container"
	}
	return &RawClient{
		backend:        BackendAppleContainer,
		appleContainer: &appleContainerConfig{binary: binary},
	}, nil
}

func (c *RawClient) Backend() string {
	if c == nil || strings.TrimSpace(c.backend) == "" {
		return BackendDocker
	}
	return NormalizeContainerBackend(c.backend)
}

func (c *RawClient) Endpoint() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.endpoint)
}

func (c *RawClient) usesNerdctl() bool {
	return c != nil && c.nerdctl != nil && c.docker == nil
}

func (c *RawClient) usesAppleContainer() bool {
	return c != nil && c.appleContainer != nil && c.docker == nil
}

func (c *RawClient) Ping(ctx context.Context) (dockertypes.Ping, error) {
	if c.usesAppleContainer() {
		if _, _, err := c.runAppleContainer(ctx, "system", "status"); err != nil {
			return dockertypes.Ping{}, err
		}
		return dockertypes.Ping{APIVersion: "apple-container"}, nil
	}
	if !c.usesNerdctl() {
		return c.docker.Ping(ctx)
	}
	if _, _, err := c.runNerdctl(ctx, "info"); err != nil {
		return dockertypes.Ping{}, err
	}
	return dockertypes.Ping{APIVersion: "containerd/nerdctl"}, nil
}

func (c *RawClient) ContainerList(ctx context.Context, options dockercontainer.ListOptions) ([]dockercontainer.Summary, error) {
	if c.usesAppleContainer() {
		args := []string{"list", "--format", "json"}
		if options.All {
			args = append(args, "--all")
		}
		stdout, _, err := c.runAppleContainer(ctx, args...)
		if err != nil {
			return nil, err
		}
		containers, err := parseAppleContainerList(stdout)
		if err != nil {
			return nil, err
		}
		out := make([]dockercontainer.Summary, 0, len(containers))
		for _, ctr := range containers {
			summary := appleContainerSummary(ctr)
			if !options.All && summary.State != "running" {
				continue
			}
			if !appleContainerSummaryMatchesFilters(summary, options.Filters) {
				continue
			}
			out = append(out, summary)
		}
		return out, nil
	}
	if !c.usesNerdctl() {
		return c.docker.ContainerList(ctx, options)
	}
	args := []string{"ps", "-q", "--no-trunc"}
	if options.All {
		args = append(args, "-a")
	}
	args = append(args, nerdctlFilterArgs(options.Filters)...)
	stdout, _, err := c.runNerdctl(ctx, args...)
	if err != nil {
		return nil, err
	}
	ids := splitNonEmptyLines(string(stdout))
	if len(ids) == 0 {
		return nil, nil
	}
	inspects, err := c.inspectContainers(ctx, ids...)
	if err != nil {
		return nil, err
	}
	out := make([]dockercontainer.Summary, 0, len(inspects))
	for _, inspect := range inspects {
		out = append(out, containerSummaryFromInspect(inspect))
	}
	return out, nil
}

func (c *RawClient) ContainerInspect(ctx context.Context, containerID string) (dockercontainer.InspectResponse, error) {
	if c.usesAppleContainer() {
		stdout, _, err := c.runAppleContainer(ctx, "inspect", containerID)
		if err != nil {
			return dockercontainer.InspectResponse{}, err
		}
		containers, err := parseAppleContainerList(stdout)
		if err != nil {
			return dockercontainer.InspectResponse{}, err
		}
		if len(containers) == 0 {
			return dockercontainer.InspectResponse{}, fmt.Errorf("container %q not found", containerID)
		}
		return appleContainerInspectResponse(containers[0]), nil
	}
	if !c.usesNerdctl() {
		return c.docker.ContainerInspect(ctx, containerID)
	}
	inspects, err := c.inspectContainers(ctx, containerID)
	if err != nil {
		return dockercontainer.InspectResponse{}, err
	}
	if len(inspects) == 0 {
		return dockercontainer.InspectResponse{}, fmt.Errorf("container %q not found", containerID)
	}
	return inspects[0], nil
}

func (c *RawClient) ContainerCreate(ctx context.Context, config *dockercontainer.Config, hostConfig *dockercontainer.HostConfig, networkingConfig *dockernetwork.NetworkingConfig, platform *specs.Platform, containerName string) (dockercontainer.CreateResponse, error) {
	if c.usesAppleContainer() {
		args, err := appleContainerCreateArgs(config, hostConfig, networkingConfig, platform, containerName)
		if err != nil {
			return dockercontainer.CreateResponse{}, err
		}
		stdout, _, err := c.runAppleContainer(ctx, args...)
		if err != nil {
			return dockercontainer.CreateResponse{}, err
		}
		id := strings.TrimSpace(string(stdout))
		if id == "" {
			id = strings.TrimSpace(containerName)
		}
		if id == "" {
			return dockercontainer.CreateResponse{}, fmt.Errorf("apple-container create did not return a container id")
		}
		return dockercontainer.CreateResponse{ID: id}, nil
	}
	if !c.usesNerdctl() {
		return c.docker.ContainerCreate(ctx, config, hostConfig, networkingConfig, platform, containerName)
	}
	args, err := nerdctlCreateArgs(config, hostConfig, networkingConfig, platform, containerName)
	if err != nil {
		return dockercontainer.CreateResponse{}, err
	}
	stdout, _, err := c.runNerdctl(ctx, args...)
	if err != nil {
		return dockercontainer.CreateResponse{}, err
	}
	id := strings.TrimSpace(string(stdout))
	if id == "" {
		id = containerName
	}
	return dockercontainer.CreateResponse{ID: id}, nil
}

func (c *RawClient) ContainerStart(ctx context.Context, containerID string, options dockercontainer.StartOptions) error {
	if c.usesAppleContainer() {
		_, _, err := c.runAppleContainer(ctx, "start", containerID)
		return err
	}
	if !c.usesNerdctl() {
		return c.docker.ContainerStart(ctx, containerID, options)
	}
	_, _, err := c.runNerdctl(ctx, "start", containerID)
	return err
}

func (c *RawClient) ContainerStop(ctx context.Context, containerID string, options dockercontainer.StopOptions) error {
	if c.usesAppleContainer() {
		args := []string{"stop"}
		if options.Timeout != nil {
			args = append(args, "--time", strconv.Itoa(*options.Timeout))
		}
		args = append(args, containerID)
		_, _, err := c.runAppleContainer(ctx, args...)
		return err
	}
	if !c.usesNerdctl() {
		return c.docker.ContainerStop(ctx, containerID, options)
	}
	args := []string{"stop"}
	if options.Timeout != nil {
		args = append(args, "--time", strconv.Itoa(*options.Timeout))
	}
	args = append(args, containerID)
	_, _, err := c.runNerdctl(ctx, args...)
	return err
}

func (c *RawClient) ContainerRemove(ctx context.Context, containerID string, options dockercontainer.RemoveOptions) error {
	if c.usesAppleContainer() {
		args := []string{"delete"}
		if options.Force {
			args = append(args, "--force")
		}
		args = append(args, containerID)
		_, _, err := c.runAppleContainer(ctx, args...)
		return err
	}
	if !c.usesNerdctl() {
		return c.docker.ContainerRemove(ctx, containerID, options)
	}
	args := []string{"rm"}
	if options.Force {
		args = append(args, "--force")
	}
	if options.RemoveVolumes {
		args = append(args, "--volumes")
	}
	args = append(args, containerID)
	_, _, err := c.runNerdctl(ctx, args...)
	return err
}

func (c *RawClient) ContainerKill(ctx context.Context, containerID, signal string) error {
	if c.usesAppleContainer() {
		return c.appleContainerUnsupported("signal containers")
	}
	if !c.usesNerdctl() {
		return c.docker.ContainerKill(ctx, containerID, signal)
	}
	args := []string{"kill"}
	if strings.TrimSpace(signal) != "" {
		args = append(args, "--signal", signal)
	}
	args = append(args, containerID)
	_, _, err := c.runNerdctl(ctx, args...)
	return err
}

func (c *RawClient) ContainerPause(ctx context.Context, containerID string) error {
	if c.usesAppleContainer() {
		return c.appleContainerUnsupported("pause containers")
	}
	if !c.usesNerdctl() {
		return c.docker.ContainerPause(ctx, containerID)
	}
	_, _, err := c.runNerdctl(ctx, "pause", containerID)
	return err
}

func (c *RawClient) ContainerUnpause(ctx context.Context, containerID string) error {
	if c.usesAppleContainer() {
		return c.appleContainerUnsupported("unpause containers")
	}
	if !c.usesNerdctl() {
		return c.docker.ContainerUnpause(ctx, containerID)
	}
	_, _, err := c.runNerdctl(ctx, "unpause", containerID)
	return err
}

func (c *RawClient) ContainerExecCreate(ctx context.Context, containerID string, options dockercontainer.ExecOptions) (dockercontainer.ExecCreateResponse, error) {
	if c.usesAppleContainer() {
		return dockercontainer.ExecCreateResponse{}, c.appleContainerUnsupported("create exec sessions")
	}
	if !c.usesNerdctl() {
		return c.docker.ContainerExecCreate(ctx, containerID, options)
	}
	return dockercontainer.ExecCreateResponse{}, fmt.Errorf("containerd backend does not expose Docker exec sessions")
}

func (c *RawClient) ContainerExecAttach(ctx context.Context, execID string, config dockercontainer.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
	if c.usesAppleContainer() {
		return dockertypes.HijackedResponse{}, c.appleContainerUnsupported("attach exec sessions")
	}
	if !c.usesNerdctl() {
		return c.docker.ContainerExecAttach(ctx, execID, config)
	}
	return dockertypes.HijackedResponse{}, fmt.Errorf("containerd backend does not expose Docker exec sessions")
}

func (c *RawClient) ContainerExecInspect(ctx context.Context, execID string) (dockercontainer.ExecInspect, error) {
	if c.usesAppleContainer() {
		return dockercontainer.ExecInspect{}, c.appleContainerUnsupported("inspect exec sessions")
	}
	if !c.usesNerdctl() {
		return c.docker.ContainerExecInspect(ctx, execID)
	}
	return dockercontainer.ExecInspect{}, fmt.Errorf("containerd backend does not expose Docker exec sessions")
}

func (c *RawClient) NetworkCreate(ctx context.Context, name string, options dockernetwork.CreateOptions) (dockernetwork.CreateResponse, error) {
	if c.usesAppleContainer() {
		return dockernetwork.CreateResponse{}, c.appleContainerUnsupported("create networks")
	}
	if !c.usesNerdctl() {
		return c.docker.NetworkCreate(ctx, name, options)
	}
	args := []string{"network", "create"}
	if strings.TrimSpace(options.Driver) != "" {
		args = append(args, "--driver", options.Driver)
	}
	if options.Internal {
		args = append(args, "--internal")
	}
	for k, v := range options.Labels {
		args = append(args, "--label", k+"="+v)
	}
	args = append(args, name)
	stdout, _, err := c.runNerdctl(ctx, args...)
	if err != nil {
		return dockernetwork.CreateResponse{}, err
	}
	id := strings.TrimSpace(string(stdout))
	if id == "" {
		id = name
	}
	return dockernetwork.CreateResponse{ID: id, Warning: ""}, nil
}

func (c *RawClient) NetworkRemove(ctx context.Context, networkID string) error {
	if c.usesAppleContainer() {
		return c.appleContainerUnsupported("remove networks")
	}
	if !c.usesNerdctl() {
		return c.docker.NetworkRemove(ctx, networkID)
	}
	_, _, err := c.runNerdctl(ctx, "network", "rm", networkID)
	return err
}

func (c *RawClient) NetworkInspect(ctx context.Context, networkID string, options dockernetwork.InspectOptions) (dockernetwork.Inspect, error) {
	if c.usesAppleContainer() {
		return dockernetwork.Inspect{}, c.appleContainerUnsupported("inspect networks")
	}
	if !c.usesNerdctl() {
		return c.docker.NetworkInspect(ctx, networkID, options)
	}
	var inspect []dockernetwork.Inspect
	if stdout, stderr, err := c.runNerdctl(ctx, "network", "inspect", "--mode", "dockercompat", networkID); err != nil {
		return dockernetwork.Inspect{}, err
	} else if err := json.Unmarshal(nerdctlJSONOutput(stdout, stderr), &inspect); err != nil {
		return dockernetwork.Inspect{}, err
	}
	if len(inspect) == 0 {
		return dockernetwork.Inspect{}, fmt.Errorf("network %q not found", networkID)
	}
	return inspect[0], nil
}

func (c *RawClient) NetworkList(ctx context.Context, options dockernetwork.ListOptions) ([]dockernetwork.Summary, error) {
	if c.usesAppleContainer() {
		return nil, c.appleContainerUnsupported("list networks")
	}
	if !c.usesNerdctl() {
		return c.docker.NetworkList(ctx, options)
	}
	args := []string{"network", "ls", "-q"}
	args = append(args, nerdctlFilterArgs(options.Filters)...)
	stdout, _, err := c.runNerdctl(ctx, args...)
	if err != nil {
		return nil, err
	}
	ids := splitNonEmptyLines(string(stdout))
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]dockernetwork.Summary, 0, len(ids))
	for _, id := range ids {
		inspect, err := c.NetworkInspect(ctx, id, dockernetwork.InspectOptions{})
		if err != nil {
			return nil, err
		}
		out = append(out, dockernetwork.Summary{
			Name:     inspect.Name,
			ID:       inspect.ID,
			Driver:   inspect.Driver,
			Scope:    inspect.Scope,
			Internal: inspect.Internal,
			Labels:   inspect.Labels,
		})
	}
	return out, nil
}

func (c *RawClient) NetworkConnect(ctx context.Context, networkID, container string, config *dockernetwork.EndpointSettings) error {
	if c.usesAppleContainer() {
		return c.appleContainerUnsupported("connect networks")
	}
	if !c.usesNerdctl() {
		return c.docker.NetworkConnect(ctx, networkID, container, config)
	}
	inspect, err := c.ContainerInspect(ctx, container)
	if err == nil && inspect.NetworkSettings != nil {
		if _, ok := inspect.NetworkSettings.Networks[networkID]; ok {
			return nil
		}
	}
	return fmt.Errorf("containerd backend does not support late network attach for %q -> %q", container, networkID)
}

func (c *RawClient) VolumeCreate(ctx context.Context, options dockervolume.CreateOptions) (dockervolume.Volume, error) {
	if c.usesAppleContainer() {
		return dockervolume.Volume{}, c.appleContainerUnsupported("create volumes")
	}
	if !c.usesNerdctl() {
		return c.docker.VolumeCreate(ctx, options)
	}
	args := []string{"volume", "create"}
	for k, v := range options.Labels {
		args = append(args, "--label", k+"="+v)
	}
	if strings.TrimSpace(options.Name) != "" {
		args = append(args, options.Name)
	}
	stdout, _, err := c.runNerdctl(ctx, args...)
	if err != nil {
		return dockervolume.Volume{}, err
	}
	name := strings.TrimSpace(string(stdout))
	if name == "" {
		name = options.Name
	}
	return dockervolume.Volume{Name: name, Labels: options.Labels}, nil
}

func (c *RawClient) VolumeInspect(ctx context.Context, volumeID string) (dockervolume.Volume, error) {
	if c.usesAppleContainer() {
		return dockervolume.Volume{}, c.appleContainerUnsupported("inspect volumes")
	}
	if !c.usesNerdctl() {
		return c.docker.VolumeInspect(ctx, volumeID)
	}
	var inspect []dockervolume.Volume
	if stdout, stderr, err := c.runNerdctl(ctx, "volume", "inspect", volumeID); err != nil {
		return dockervolume.Volume{}, err
	} else if err := json.Unmarshal(nerdctlJSONOutput(stdout, stderr), &inspect); err != nil {
		return dockervolume.Volume{}, err
	}
	if len(inspect) == 0 {
		return dockervolume.Volume{}, fmt.Errorf("volume %q not found", volumeID)
	}
	return inspect[0], nil
}

func (c *RawClient) VolumeRemove(ctx context.Context, volumeID string, force bool) error {
	if c.usesAppleContainer() {
		return c.appleContainerUnsupported("remove volumes")
	}
	if !c.usesNerdctl() {
		return c.docker.VolumeRemove(ctx, volumeID, force)
	}
	args := []string{"volume", "rm"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, volumeID)
	_, _, err := c.runNerdctl(ctx, args...)
	return err
}

func (c *RawClient) ImageInspectWithRaw(ctx context.Context, image string) (dockerimage.InspectResponse, []byte, error) {
	if c.usesAppleContainer() {
		return dockerimage.InspectResponse{}, nil, c.appleContainerUnsupported("inspect images")
	}
	if !c.usesNerdctl() {
		return c.docker.ImageInspectWithRaw(ctx, image)
	}
	stdout, stderr, err := c.runNerdctl(ctx, "inspect", "--mode", "dockercompat", "--type", "image", image)
	if err != nil {
		return dockerimage.InspectResponse{}, nil, err
	}
	raw := nerdctlJSONOutput(stdout, stderr)
	if len(bytes.TrimSpace(raw)) == 0 {
		return dockerimage.InspectResponse{}, raw, fmt.Errorf("image %q not found", image)
	}
	var inspect []dockerimage.InspectResponse
	if err := json.Unmarshal(raw, &inspect); err != nil {
		return dockerimage.InspectResponse{}, raw, err
	}
	if len(inspect) == 0 {
		return dockerimage.InspectResponse{}, raw, fmt.Errorf("image %q not found", image)
	}
	return inspect[0], raw, nil
}

func (c *RawClient) ImageList(ctx context.Context, options dockerimage.ListOptions) ([]dockerimage.Summary, error) {
	if c.usesAppleContainer() {
		return nil, c.appleContainerUnsupported("list images")
	}
	if !c.usesNerdctl() {
		return c.docker.ImageList(ctx, options)
	}
	args := []string{"images", "-q", "--no-trunc"}
	args = append(args, nerdctlFilterArgs(options.Filters)...)
	stdout, _, err := c.runNerdctl(ctx, args...)
	if err != nil {
		return nil, err
	}
	ids := uniqueStrings(splitNonEmptyLines(string(stdout)))
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]dockerimage.Summary, 0, len(ids))
	for _, id := range ids {
		inspect, _, err := c.ImageInspectWithRaw(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, dockerimage.Summary{
			ID:          inspect.ID,
			RepoTags:    inspect.RepoTags,
			RepoDigests: inspect.RepoDigests,
			Size:        inspect.Size,
			Created:     inspect.Metadata.LastTagTime.Unix(),
			Labels:      labelsFromImageInspect(inspect),
		})
	}
	return out, nil
}

func (c *RawClient) ImageRemove(ctx context.Context, image string, options dockerimage.RemoveOptions) ([]dockerimage.DeleteResponse, error) {
	if c.usesAppleContainer() {
		return nil, c.appleContainerUnsupported("remove images")
	}
	if !c.usesNerdctl() {
		return c.docker.ImageRemove(ctx, image, options)
	}
	args := []string{"rmi"}
	if options.Force {
		args = append(args, "--force")
	}
	args = append(args, image)
	_, _, err := c.runNerdctl(ctx, args...)
	if err != nil {
		return nil, err
	}
	return []dockerimage.DeleteResponse{{Deleted: image}}, nil
}

func (c *RawClient) ImagePull(ctx context.Context, ref string, options dockerimage.PullOptions) (io.ReadCloser, error) {
	if c.usesAppleContainer() {
		return nil, c.appleContainerUnsupported("pull images")
	}
	if !c.usesNerdctl() {
		return c.docker.ImagePull(ctx, ref, options)
	}
	stdout, stderr, err := c.runNerdctl(ctx, "pull", ref)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(append(stdout, stderr...))), nil
}

func (c *RawClient) ImageTag(ctx context.Context, source, target string) error {
	if c.usesAppleContainer() {
		return c.appleContainerUnsupported("tag images")
	}
	if !c.usesNerdctl() {
		return c.docker.ImageTag(ctx, source, target)
	}
	_, _, err := c.runNerdctl(ctx, "tag", source, target)
	return err
}

func (c *RawClient) ImageBuild(ctx context.Context, buildContext io.Reader, options dockertypes.ImageBuildOptions) (dockertypes.ImageBuildResponse, error) {
	if c.usesAppleContainer() {
		return dockertypes.ImageBuildResponse{}, c.appleContainerUnsupported("build images")
	}
	if !c.usesNerdctl() {
		return c.docker.ImageBuild(ctx, buildContext, options)
	}
	buildDir, err := os.MkdirTemp("", "agency-containerd-build-*")
	if err != nil {
		return dockertypes.ImageBuildResponse{}, err
	}
	defer os.RemoveAll(buildDir)
	if err := untarBuildContext(buildDir, buildContext); err != nil {
		return dockertypes.ImageBuildResponse{}, err
	}
	args := []string{"build"}
	if strings.TrimSpace(options.Platform) != "" {
		args = append(args, "--platform", options.Platform)
	}
	if strings.TrimSpace(options.Dockerfile) != "" {
		args = append(args, "-f", filepath.Join(buildDir, options.Dockerfile))
	}
	for _, tag := range options.Tags {
		args = append(args, "-t", tag)
	}
	for key, value := range options.BuildArgs {
		if value == nil {
			continue
		}
		args = append(args, "--build-arg", key+"="+*value)
	}
	args = append(args, buildDir)
	stdout, stderr, err := c.runNerdctl(ctx, args...)
	if err != nil {
		return dockertypes.ImageBuildResponse{}, err
	}
	body := nerdctlBuildStream(append(stdout, stderr...))
	return dockertypes.ImageBuildResponse{Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func (c *RawClient) Events(ctx context.Context, options dockerevents.ListOptions) (<-chan dockerevents.Message, <-chan error) {
	if c.usesAppleContainer() {
		out := make(chan dockerevents.Message)
		errOut := make(chan error, 1)
		close(out)
		errOut <- c.appleContainerUnsupported("stream events")
		close(errOut)
		return out, errOut
	}
	if !c.usesNerdctl() {
		return c.docker.Events(ctx, options)
	}
	out := make(chan dockerevents.Message)
	errOut := make(chan error, 1)
	close(out)
	errOut <- fmt.Errorf("containerd backend does not yet provide an event stream")
	close(errOut)
	return out, errOut
}

func (c *RawClient) SupportsEventStream() bool {
	return !c.usesNerdctl() && !c.usesAppleContainer()
}

func (c *RawClient) Exec(ctx context.Context, containerName, user string, cmd []string) (string, error) {
	if c.usesAppleContainer() {
		return "", c.appleContainerUnsupported("exec commands")
	}
	if !c.usesNerdctl() {
		execID, err := c.ContainerExecCreate(ctx, containerName, dockercontainer.ExecOptions{
			Cmd:          cmd,
			AttachStdout: true,
			AttachStderr: true,
			User:         user,
		})
		if err != nil {
			return "", err
		}
		resp, err := c.ContainerExecAttach(ctx, execID.ID, dockercontainer.ExecAttachOptions{})
		if err != nil {
			return "", err
		}
		defer resp.Close()

		var out bytes.Buffer
		if _, err := stdcopy.StdCopy(&out, &out, resp.Reader); err != nil {
			out.Reset()
			_, _ = out.ReadFrom(resp.Reader)
		}
		inspect, err := c.ContainerExecInspect(ctx, execID.ID)
		if err != nil {
			return out.String(), err
		}
		if inspect.ExitCode != 0 {
			return out.String(), fmt.Errorf("exit code %d", inspect.ExitCode)
		}
		return out.String(), nil
	}

	args := []string{"exec"}
	if strings.TrimSpace(user) != "" {
		args = append(args, "--user", user)
	}
	args = append(args, containerName)
	args = append(args, cmd...)
	stdout, stderr, err := c.runNerdctl(ctx, args...)
	out := string(append(stdout, stderr...))
	if err != nil {
		return out, err
	}
	return out, nil
}

func (c *RawClient) ContainerLogs(ctx context.Context, containerName string, tail int) (string, error) {
	if c.usesAppleContainer() {
		args := []string{"logs"}
		if tail > 0 {
			args = append(args, "-n", strconv.Itoa(tail))
		}
		args = append(args, containerName)
		stdout, stderr, err := c.runAppleContainer(ctx, args...)
		out := string(append(stdout, stderr...))
		if err != nil {
			return out, err
		}
		return out, nil
	}
	if tail <= 0 {
		tail = 200
	}
	if !c.usesNerdctl() {
		reader, err := c.docker.ContainerLogs(ctx, containerName, dockercontainer.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Tail:       strconv.Itoa(tail),
		})
		if err != nil {
			return "", err
		}
		defer reader.Close()

		var out bytes.Buffer
		if _, err := stdcopy.StdCopy(&out, &out, reader); err != nil {
			out.Reset()
			_, _ = out.ReadFrom(reader)
		}
		return out.String(), nil
	}

	stdout, stderr, err := c.runNerdctl(ctx, "logs", "--tail", strconv.Itoa(tail), containerName)
	out := string(append(stdout, stderr...))
	if err != nil {
		return out, err
	}
	return out, nil
}

func (c *RawClient) appleContainerUnsupported(action string) error {
	return fmt.Errorf("apple-container backend cannot %s yet; runtime contract support is gated until mediation, networking, and lifecycle semantics are implemented", action)
}

func (c *RawClient) nerdctlBaseArgs() []string {
	args := make([]string, 0, 6)
	if c.nerdctl == nil {
		return args
	}
	if strings.TrimSpace(c.nerdctl.address) != "" {
		address := strings.TrimPrefix(c.nerdctl.address, "unix://")
		args = append(args, "--address", address)
	}
	if strings.TrimSpace(c.nerdctl.namespace) != "" {
		args = append(args, "--namespace", c.nerdctl.namespace)
	}
	if strings.TrimSpace(c.nerdctl.dataRoot) != "" {
		args = append(args, "--data-root", c.nerdctl.dataRoot)
	}
	return args
}

func (c *RawClient) runNerdctl(ctx context.Context, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "nerdctl", append(c.nerdctlBaseArgs(), args...)...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		if c.usesNerdctl() && strings.Contains(msg, "frame too large") && strings.Contains(msg, "HTTP/1.1 header") {
			return stdout.Bytes(), stderr.Bytes(), containerdSocketTypeError(c.nerdctl.address)
		}
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("nerdctl %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}

func (c *RawClient) runAppleContainer(ctx context.Context, args ...string) ([]byte, []byte, error) {
	if c != nil && c.appleContainer != nil && c.appleContainer.run != nil {
		return c.appleContainer.run(ctx, args...)
	}
	binary := "container"
	if c != nil && c.appleContainer != nil && strings.TrimSpace(c.appleContainer.binary) != "" {
		binary = c.appleContainer.binary
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("container %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}

func parseAppleContainerList(raw []byte) ([]appleContainerInspect, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	var list []appleContainerInspect
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var single appleContainerInspect
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, err
	}
	return []appleContainerInspect{single}, nil
}

func appleContainerSummary(ctr appleContainerInspect) dockercontainer.Summary {
	inspect := appleContainerInspectResponse(ctr)
	summary := containerSummaryFromInspect(inspect)
	if inspect.NetworkSettings != nil {
		summary.NetworkSettings = &dockercontainer.NetworkSettingsSummary{Networks: inspect.NetworkSettings.Networks}
	}
	return summary
}

func appleContainerSummaryMatchesFilters(summary dockercontainer.Summary, filters dockerfilters.Args) bool {
	if !filters.MatchKVList("label", summary.Labels) {
		return false
	}
	if !filters.ExactMatch("status", string(summary.State)) {
		return false
	}
	for _, name := range summary.Names {
		trimmed := strings.TrimPrefix(name, "/")
		if !filters.FuzzyMatch("name", trimmed) && !filters.FuzzyMatch("name", name) {
			return false
		}
	}
	return true
}

func appleContainerInspectResponse(ctr appleContainerInspect) dockercontainer.InspectResponse {
	id := strings.TrimSpace(ctr.Configuration.ID)
	hostname := strings.TrimSpace(ctr.Configuration.Hostname)
	if hostname == "" {
		hostname = id
	}
	labels := map[string]string{}
	for k, v := range ctr.Configuration.Labels {
		labels[k] = v
	}
	state := strings.TrimSpace(strings.ToLower(ctr.Status))
	if state == "" {
		state = "unknown"
	}
	networks := map[string]*dockernetwork.EndpointSettings{}
	for _, net := range ctr.Networks {
		name := strings.TrimSpace(net.Network)
		if name == "" {
			name = "default"
		}
		address, prefixLen := splitCIDRAddress(firstNonEmpty(net.IPv4Address, net.Address, net.IPv6Address))
		networks[name] = &dockernetwork.EndpointSettings{
			Gateway:     firstNonEmpty(net.IPv4Gateway, net.Gateway),
			IPAddress:   address,
			IPPrefixLen: prefixLen,
			DNSNames:    nonEmptyStrings(hostname, strings.TrimSuffix(net.Hostname, ".")),
		}
	}
	mounts := make([]dockercontainer.MountPoint, 0, len(ctr.Configuration.Mounts))
	for _, mount := range ctr.Configuration.Mounts {
		destination := strings.TrimSpace(mount.Destination)
		if destination == "" {
			destination = strings.TrimSpace(mount.Target)
		}
		mounts = append(mounts, dockercontainer.MountPoint{
			Source:      mount.Source,
			Destination: destination,
			RW:          !mount.ReadOnly && !mount.Readonly,
		})
	}
	created := strings.TrimSpace(ctr.CreatedAt)
	image := firstNonEmpty(ctr.Configuration.ImageRef.Reference, ctr.Configuration.Image)
	env := ctr.Configuration.Env
	if len(env) == 0 {
		env = ctr.Configuration.Init.Environment
	}
	return dockercontainer.InspectResponse{
		ContainerJSONBase: &dockercontainer.ContainerJSONBase{
			ID:      id,
			Created: created,
			Name:    "/" + id,
			Image:   image,
			State: &dockercontainer.State{
				Status:  dockercontainer.ContainerState(state),
				Running: state == "running",
				Paused:  state == "paused",
				Dead:    state == "dead",
			},
			HostConfig: &dockercontainer.HostConfig{},
		},
		Config: &dockercontainer.Config{
			Hostname: hostname,
			Image:    image,
			Env:      env,
			Labels:   labels,
		},
		Mounts: mounts,
		NetworkSettings: &dockercontainer.NetworkSettings{
			Networks: networks,
		},
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func splitCIDRAddress(raw string) (string, int) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0
	}
	addr, prefix, ok := strings.Cut(raw, "/")
	if !ok {
		return raw, 0
	}
	prefixLen, err := strconv.Atoi(prefix)
	if err != nil {
		return addr, 0
	}
	return addr, prefixLen
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func appleContainerCreateArgs(config *dockercontainer.Config, hostConfig *dockercontainer.HostConfig, networkingConfig *dockernetwork.NetworkingConfig, platform *specs.Platform, containerName string) ([]string, error) {
	if config == nil {
		return nil, fmt.Errorf("apple-container create requires container config")
	}
	image := strings.TrimSpace(config.Image)
	if image == "" {
		return nil, fmt.Errorf("apple-container create requires an image")
	}
	args := []string{"create"}
	if name := strings.TrimSpace(containerName); name != "" {
		args = append(args, "--name", name)
	}
	for _, env := range config.Env {
		if strings.TrimSpace(env) != "" {
			args = append(args, "--env", env)
		}
	}
	for key, value := range config.Labels {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		args = append(args, "--label", key+"="+value)
	}
	if user := strings.TrimSpace(config.User); user != "" {
		args = append(args, "--user", user)
	}
	if workdir := strings.TrimSpace(config.WorkingDir); workdir != "" {
		args = append(args, "--workdir", workdir)
	}
	if len(config.Entrypoint) > 0 {
		args = append(args, "--entrypoint", strings.Join(config.Entrypoint, " "))
	}
	if platform != nil {
		if platform.OS != "" && platform.Architecture != "" {
			args = append(args, "--platform", platform.OS+"/"+platform.Architecture)
		} else {
			if platform.OS != "" {
				args = append(args, "--os", platform.OS)
			}
			if platform.Architecture != "" {
				args = append(args, "--arch", platform.Architecture)
			}
		}
	}
	if hostConfig != nil {
		if hostConfig.ReadonlyRootfs {
			args = append(args, "--read-only")
		}
		if hostConfig.Resources.Memory > 0 {
			args = append(args, "--memory", strconv.FormatInt(appleContainerMemoryBytes(hostConfig.Resources.Memory), 10))
		}
		if hostConfig.Resources.NanoCPUs > 0 {
			args = append(args, "--cpus", formatNanoCPUs(hostConfig.Resources.NanoCPUs))
		}
		for _, bind := range hostConfig.Binds {
			if mount := appleContainerMountFromBind(bind); mount != "" {
				args = append(args, "--mount", mount)
			}
		}
		for target := range hostConfig.Tmpfs {
			target = strings.TrimSpace(target)
			if target != "" {
				args = append(args, "--tmpfs", target)
			}
		}
		if networkMode := strings.TrimSpace(string(hostConfig.NetworkMode)); networkMode != "" && networkMode != "default" {
			args = append(args, "--network", networkMode)
		}
		for port, bindings := range hostConfig.PortBindings {
			for _, binding := range bindings {
				published := appleContainerPublishSpec(string(port), binding.HostIP, binding.HostPort)
				if published != "" {
					args = append(args, "--publish", published)
				}
			}
		}
	}
	for name := range networkingConfigEndpoints(networkingConfig) {
		name = strings.TrimSpace(name)
		if name != "" && name != "default" {
			args = append(args, "--network", name)
		}
	}
	args = append(args, image)
	args = append(args, config.Cmd...)
	return args, nil
}

func appleContainerMountFromBind(bind string) string {
	parts := strings.Split(bind, ":")
	if len(parts) < 2 {
		return ""
	}
	source := strings.TrimSpace(parts[0])
	target := strings.TrimSpace(parts[1])
	if source == "" || target == "" {
		return ""
	}
	readonly := false
	for _, opt := range parts[2:] {
		if strings.TrimSpace(opt) == "ro" {
			readonly = true
		}
	}
	mount := "type=bind,source=" + source + ",target=" + target
	if readonly {
		mount += ",readonly"
	}
	return mount
}

func appleContainerPublishSpec(containerPort, hostIP, hostPort string) string {
	containerPort = strings.TrimSpace(containerPort)
	hostIP = strings.TrimSpace(hostIP)
	hostPort = strings.TrimSpace(hostPort)
	if containerPort == "" || hostPort == "" {
		return ""
	}
	port, proto, _ := strings.Cut(containerPort, "/")
	spec := hostPort + ":" + port
	if hostIP != "" {
		spec = hostIP + ":" + spec
	}
	if proto != "" {
		spec += "/" + proto
	}
	return spec
}

func formatNanoCPUs(nano int64) string {
	if nano%1_000_000_000 == 0 {
		return strconv.FormatInt(nano/1_000_000_000, 10)
	}
	// Apple container accepts whole CPU counts. Round up so a fractional Docker
	// NanoCPUs limit remains at least as permissive as requested.
	return strconv.FormatInt((nano+999_999_999)/1_000_000_000, 10)
}

func appleContainerMemoryBytes(memory int64) int64 {
	const minAppleContainerMemory = 200 * 1024 * 1024
	if memory > 0 && memory < minAppleContainerMemory {
		return minAppleContainerMemory
	}
	return memory
}

func networkingConfigEndpoints(networkingConfig *dockernetwork.NetworkingConfig) map[string]*dockernetwork.EndpointSettings {
	if networkingConfig == nil || networkingConfig.EndpointsConfig == nil {
		return nil
	}
	return networkingConfig.EndpointsConfig
}

func (c *RawClient) inspectContainers(ctx context.Context, ids ...string) ([]dockercontainer.InspectResponse, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := append([]string{"inspect", "--mode", "dockercompat", "--type", "container"}, ids...)
	stdout, stderr, err := c.runNerdctl(ctx, args...)
	if err != nil {
		return nil, err
	}
	var inspect []dockercontainer.InspectResponse
	if err := json.Unmarshal(nerdctlJSONOutput(stdout, stderr), &inspect); err != nil {
		return nil, err
	}
	return inspect, nil
}

func nerdctlJSONOutput(stdout, stderr []byte) []byte {
	if trimmed := bytes.TrimSpace(stdout); len(trimmed) > 0 {
		return trimmed
	}
	return bytes.TrimSpace(stderr)
}

func nerdctlFilterArgs(filters dockerfilters.Args) []string {
	args := make([]string, 0, filters.Len())
	for _, key := range filters.Keys() {
		for _, value := range filters.Get(key) {
			args = append(args, "--filter", key+"="+value)
		}
	}
	return args
}

func containerSummaryFromInspect(inspect dockercontainer.InspectResponse) dockercontainer.Summary {
	names := []string{}
	if inspect.Name != "" {
		name := inspect.Name
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		names = append(names, name)
	}
	labels := map[string]string{}
	if inspect.Config != nil {
		labels = inspect.Config.Labels
	}
	state := "created"
	status := state
	if inspect.State != nil {
		if inspect.State.Status != "" {
			state = inspect.State.Status
			status = inspect.State.Status
		}
		if inspect.State.Health != nil && inspect.State.Health.Status != "" {
			status = inspect.State.Health.Status
		}
	}
	networks := map[string]*dockernetwork.EndpointSettings{}
	if inspect.NetworkSettings != nil {
		for name := range inspect.NetworkSettings.Networks {
			networks[name] = &dockernetwork.EndpointSettings{}
		}
	}
	return dockercontainer.Summary{
		ID:      inspect.ID,
		Names:   names,
		Image:   imageRefFromInspect(inspect),
		ImageID: inspect.Image,
		State:   state,
		Status:  status,
		Labels:  labels,
		NetworkSettings: &dockercontainer.NetworkSettingsSummary{
			Networks: networks,
		},
	}
}

func imageRefFromInspect(inspect dockercontainer.InspectResponse) string {
	if inspect.Config != nil && strings.TrimSpace(inspect.Config.Image) != "" {
		return inspect.Config.Image
	}
	return inspect.Image
}

func labelsFromImageInspect(inspect dockerimage.InspectResponse) map[string]string {
	if inspect.Config != nil && inspect.Config.Labels != nil {
		return inspect.Config.Labels
	}
	return map[string]string{}
}

func nerdctlCreateArgs(config *dockercontainer.Config, hostConfig *dockercontainer.HostConfig, networkingConfig *dockernetwork.NetworkingConfig, platform *specs.Platform, containerName string) ([]string, error) {
	if config == nil {
		return nil, fmt.Errorf("container config is required")
	}
	args := []string{"create", "--name", containerName}
	if config.Hostname != "" {
		args = append(args, "--hostname", config.Hostname)
	}
	if config.User != "" {
		args = append(args, "--user", config.User)
	}
	if config.WorkingDir != "" {
		args = append(args, "--workdir", config.WorkingDir)
	}
	if platform != nil && platform.OS != "" && platform.Architecture != "" {
		args = append(args, "--platform", platform.OS+"/"+platform.Architecture)
	}
	if hostConfig != nil {
		for _, bind := range hostConfig.Binds {
			args = append(args, "--volume", bind)
		}
		for path, opts := range hostConfig.Tmpfs {
			if strings.TrimSpace(opts) == "" {
				args = append(args, "--tmpfs", path)
			} else {
				args = append(args, "--tmpfs", path+":"+opts)
			}
		}
		for _, cap := range hostConfig.CapAdd {
			args = append(args, "--cap-add", cap)
		}
		for _, cap := range hostConfig.CapDrop {
			args = append(args, "--cap-drop", cap)
		}
		for _, opt := range hostConfig.SecurityOpt {
			args = append(args, "--security-opt", opt)
		}
		for _, host := range hostConfig.ExtraHosts {
			args = append(args, "--add-host", host)
		}
		if hostConfig.ReadonlyRootfs {
			args = append(args, "--read-only")
		}
		if hostConfig.Resources.Memory > 0 {
			args = append(args, "--memory", strconv.FormatInt(hostConfig.Resources.Memory, 10))
		}
		if hostConfig.Resources.NanoCPUs > 0 {
			args = append(args, "--cpus", formatCPUs(hostConfig.Resources.NanoCPUs))
		}
		if hostConfig.Resources.PidsLimit != nil {
			args = append(args, "--pids-limit", strconv.FormatInt(*hostConfig.Resources.PidsLimit, 10))
		}
		if strings.TrimSpace(hostConfig.LogConfig.Type) != "" {
			args = append(args, "--log-driver", hostConfig.LogConfig.Type)
		}
		logOptKeys := make([]string, 0, len(hostConfig.LogConfig.Config))
		for key := range hostConfig.LogConfig.Config {
			logOptKeys = append(logOptKeys, key)
		}
		sort.Strings(logOptKeys)
		for _, key := range logOptKeys {
			value := strings.TrimSpace(hostConfig.LogConfig.Config[key])
			if value == "" {
				continue
			}
			args = append(args, "--log-opt", key+"="+value)
		}
		if restart := restartPolicyArg(hostConfig.RestartPolicy); restart != "" {
			args = append(args, "--restart", restart)
		}
		for port, bindings := range hostConfig.PortBindings {
			if len(bindings) == 0 {
				args = append(args, "--publish", port.Port())
				continue
			}
			for _, binding := range bindings {
				args = append(args, "--publish", portBindingArg(port.Port(), binding.HostIP, binding.HostPort))
			}
		}
		for _, networkName := range networkNames(hostConfig, networkingConfig) {
			args = append(args, "--network", networkName)
		}
	}
	for _, env := range config.Env {
		args = append(args, "--env", env)
	}
	for k, v := range config.Labels {
		args = append(args, "--label", k+"="+v)
	}
	if config.Healthcheck != nil {
		args = append(args, healthcheckArgs(config.Healthcheck)...)
	}
	if len(config.Entrypoint) > 0 {
		args = append(args, "--entrypoint", strings.Join(config.Entrypoint, " "))
	}
	args = append(args, config.Image)
	args = append(args, config.Cmd...)
	return args, nil
}

func networkNames(hostConfig *dockercontainer.HostConfig, networkingConfig *dockernetwork.NetworkingConfig) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 4)
	if hostConfig != nil {
		mode := strings.TrimSpace(string(hostConfig.NetworkMode))
		if mode != "" && mode != "default" {
			seen[mode] = true
			out = append(out, mode)
		}
	}
	if networkingConfig != nil {
		for name := range networkingConfig.EndpointsConfig {
			if seen[name] || strings.TrimSpace(name) == "" {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func formatCPUs(nano int64) string {
	value := strconv.FormatFloat(float64(nano)/1_000_000_000, 'f', 3, 64)
	value = strings.TrimRight(value, "0")
	value = strings.TrimRight(value, ".")
	if value == "" {
		return "0"
	}
	return value
}

func restartPolicyArg(policy dockercontainer.RestartPolicy) string {
	switch policy.Name {
	case "", "no":
		return ""
	case "on-failure":
		if policy.MaximumRetryCount > 0 {
			return fmt.Sprintf("on-failure:%d", policy.MaximumRetryCount)
		}
		return "on-failure"
	default:
		return string(policy.Name)
	}
}

func portBindingArg(containerPort, hostIP, hostPort string) string {
	switch {
	case hostIP != "" && hostPort != "":
		return hostIP + ":" + hostPort + ":" + containerPort
	case hostPort != "":
		return hostPort + ":" + containerPort
	default:
		return containerPort
	}
}

func healthcheckArgs(hc *dockercontainer.HealthConfig) []string {
	if hc == nil || len(hc.Test) == 0 {
		return nil
	}
	args := make([]string, 0, 8)
	switch hc.Test[0] {
	case "CMD-SHELL":
		if len(hc.Test) > 1 {
			args = append(args, "--health-cmd", hc.Test[1])
		}
	case "CMD":
		if len(hc.Test) > 1 {
			args = append(args, "--health-cmd", strings.Join(hc.Test[1:], " "))
		}
	case "NONE":
		args = append(args, "--no-healthcheck")
	}
	if hc.Interval > 0 {
		args = append(args, "--health-interval", hc.Interval.String())
	}
	if hc.Timeout > 0 {
		args = append(args, "--health-timeout", hc.Timeout.String())
	}
	if hc.StartPeriod > 0 {
		args = append(args, "--health-start-period", hc.StartPeriod.String())
	}
	if hc.Retries > 0 {
		args = append(args, "--health-retries", strconv.Itoa(hc.Retries))
	}
	return args
}

func splitNonEmptyLines(s string) []string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func untarBuildContext(dst string, src io.Reader) error {
	tr := tar.NewReader(src)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dst, hdr.Name)
		if !strings.HasPrefix(target, filepath.Clean(dst)+string(os.PathSeparator)) && filepath.Clean(target) != filepath.Clean(dst) {
			return fmt.Errorf("invalid tar path %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
}

func nerdctlBuildStream(output []byte) []byte {
	lines := splitNonEmptyLines(string(output))
	if len(lines) == 0 {
		lines = []string{"build completed"}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, line := range lines {
		_ = enc.Encode(map[string]string{"stream": line})
	}
	return buf.Bytes()
}
