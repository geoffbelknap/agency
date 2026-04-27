package runtimehost

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	dockertypes "github.com/docker/docker/api/types"
	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	dockerimage "github.com/docker/docker/api/types/image"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

func TestNerdctlJSONOutputPrefersStdout(t *testing.T) {
	stdout := []byte(" \n[{\"Id\":\"sha256:1\"}]\n")
	stderr := []byte("ignored")
	got := nerdctlJSONOutput(stdout, stderr)
	if !bytes.Equal(got, []byte("[{\"Id\":\"sha256:1\"}]")) {
		t.Fatalf("nerdctlJSONOutput() = %q", got)
	}
}

func TestNerdctlJSONOutputFallsBackToStderr(t *testing.T) {
	stdout := []byte(" \n ")
	stderr := []byte("\n[{\"Name\":\"agency-net\"}]\n")
	got := nerdctlJSONOutput(stdout, stderr)
	if !bytes.Equal(got, []byte("[{\"Name\":\"agency-net\"}]")) {
		t.Fatalf("nerdctlJSONOutput() = %q", got)
	}
}

func TestSupportsEventStream(t *testing.T) {
	if (&RawClient{backend: BackendDocker}).SupportsEventStream() != true {
		t.Fatal("docker client should support event streams")
	}
	if (&RawClient{backend: BackendContainerd, nerdctl: &nerdctlConfig{}}).SupportsEventStream() != false {
		t.Fatal("containerd nerdctl client should not report event stream support")
	}
	if (&RawClient{backend: BackendAppleContainer, appleContainer: &appleContainerConfig{}}).SupportsEventStream() != false {
		t.Fatal("apple-container client should not report event stream support without helper")
	}
	if (&RawClient{backend: BackendAppleContainer, appleContainer: &appleContainerConfig{helper: &appleContainerHelperClient{}}}).SupportsEventStream() != true {
		t.Fatal("apple-container client should report command event stream support with helper")
	}
}

func TestValidateContainerdAddressRejectsDockerCompatibleSocket(t *testing.T) {
	err := validateContainerdAddress("unix:///run/user/1000/containerd-rootless/api.sock")
	if err == nil {
		t.Fatal("expected Docker-compatible api.sock to be rejected")
	}
}

func TestValidateContainerdAddressAcceptsNativeSocket(t *testing.T) {
	if err := validateContainerdAddress("unix:///run/user/1000/containerd/containerd.sock"); err != nil {
		t.Fatalf("expected native socket to pass validation, got %v", err)
	}
}

func TestUntarBuildContextRejectsTraversal(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	dir := t.TempDir()
	if err := untarBuildContext(dir, &buf); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "escape")); err == nil {
		t.Fatal("unexpected file written outside build context")
	}
}

func TestValidateAppleContainerPlatform(t *testing.T) {
	if err := validateAppleContainerPlatform("darwin", "arm64"); err != nil {
		t.Fatalf("expected darwin/arm64 to pass validation, got %v", err)
	}
	if err := validateAppleContainerPlatform("linux", "arm64"); err == nil {
		t.Fatal("expected non-macOS host to be rejected")
	}
	if err := validateAppleContainerPlatform("darwin", "amd64"); err == nil {
		t.Fatal("expected non-Apple-silicon host to be rejected")
	}
}

func TestValidateAppleContainerConfigRejectsSocketShapedKeys(t *testing.T) {
	for _, key := range []string{"host", "socket", "native_socket", "address", "containerd_address", "namespace", "data_root"} {
		t.Run(key, func(t *testing.T) {
			if err := validateAppleContainerConfig(map[string]string{key: "value"}); err == nil {
				t.Fatalf("expected %s to be rejected", key)
			}
		})
	}
}

func TestValidateAppleContainerConfigAcceptsBinaryOverride(t *testing.T) {
	if err := validateAppleContainerConfig(map[string]string{"binary": "/usr/local/bin/container"}); err != nil {
		t.Fatalf("expected binary override to pass validation, got %v", err)
	}
	if err := validateAppleContainerConfig(map[string]string{"helper_binary": "/usr/local/bin/agency-apple-container-helper"}); err != nil {
		t.Fatalf("expected helper binary override to pass validation, got %v", err)
	}
	if err := validateAppleContainerConfig(map[string]string{"wait_helper_binary": "/usr/local/bin/agency-apple-container-wait-helper"}); err != nil {
		t.Fatalf("expected wait helper binary override to pass validation, got %v", err)
	}
}

func TestAppleContainerReadOnlyDiscovery(t *testing.T) {
	sample := []byte(`[
		{
			"status": "running",
			"networks": [{"ipv4Address": "192.168.64.3/24", "ipv4Gateway": "192.168.64.1", "hostname": "agency-henry-workspace", "network": "default"}],
			"configuration": {
				"id": "agency-henry-workspace",
				"hostname": "agency-henry-workspace",
				"image": {"reference": "docker.io/library/alpine:latest"},
				"initProcess": {"environment": ["AGENCY_HOME=/home/agency"]},
				"labels": {"agency.type": "workspace", "agency.agent": "henry"},
				"mounts": [{"source": "/host/constraints.yaml", "target": "/agency/constraints.yaml", "readonly": true}]
			}
		},
		{
			"status": "stopped",
			"configuration": {
				"id": "agency-old-workspace",
				"labels": {"agency.type": "workspace", "agency.agent": "old"}
			}
		}
	]`)
	client := &RawClient{
		backend: BackendAppleContainer,
		appleContainer: &appleContainerConfig{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			if !reflect.DeepEqual(args, []string{"list", "--format", "json"}) {
				t.Fatalf("args = %#v", args)
			}
			return sample, nil, nil
		}},
	}

	containers, err := client.ContainerList(context.Background(), dockercontainer.ListOptions{
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("label", "agency.type=workspace")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(containers) != 1 {
		t.Fatalf("containers len = %d, want 1: %#v", len(containers), containers)
	}
	got := containers[0]
	if got.ID != "agency-henry-workspace" || got.State != "running" || got.Labels["agency.agent"] != "henry" {
		t.Fatalf("unexpected summary: %#v", got)
	}
	if len(got.Names) != 1 || got.Names[0] != "/agency-henry-workspace" {
		t.Fatalf("names = %#v", got.Names)
	}
	if got.NetworkSettings == nil || got.NetworkSettings.Networks["default"].IPAddress != "192.168.64.3" {
		t.Fatalf("network settings = %#v", got.NetworkSettings)
	}
}

func TestAppleContainerInspectAndLogs(t *testing.T) {
	sample := []byte(`[
		{
			"status": "running",
			"networks": [{"ipv4Address": "192.168.64.3/24", "ipv4Gateway": "192.168.64.1", "hostname": "agency-henry-workspace", "network": "default"}],
			"configuration": {
				"id": "agency-henry-workspace",
				"hostname": "agency-henry-workspace",
				"image": {"reference": "docker.io/library/alpine:latest"},
				"initProcess": {"environment": ["AGENCY_HOME=/home/agency"]},
				"labels": {"agency.type": "workspace", "agency.agent": "henry"},
				"mounts": [{"source": "/host/constraints.yaml", "target": "/agency/constraints.yaml", "readonly": true}]
			}
		}
	]`)
	client := &RawClient{
		backend: BackendAppleContainer,
		appleContainer: &appleContainerConfig{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			switch strings.Join(args, " ") {
			case "inspect agency-henry-workspace":
				return sample, nil, nil
			case "logs -n 25 agency-henry-workspace":
				return []byte("hello\n"), nil, nil
			default:
				t.Fatalf("unexpected args: %#v", args)
			}
			return nil, nil, nil
		}},
	}

	inspect, err := client.ContainerInspect(context.Background(), "agency-henry-workspace")
	if err != nil {
		t.Fatal(err)
	}
	if inspect.State == nil || !inspect.State.Running || inspect.State.Status != "running" {
		t.Fatalf("state = %#v", inspect.State)
	}
	if inspect.Config == nil || inspect.Config.Labels["agency.agent"] != "henry" || inspect.Config.Env[0] != "AGENCY_HOME=/home/agency" {
		t.Fatalf("config = %#v", inspect.Config)
	}
	if len(inspect.Mounts) != 1 || inspect.Mounts[0].RW {
		t.Fatalf("mounts = %#v", inspect.Mounts)
	}

	logs, err := client.ContainerLogs(context.Background(), "agency-henry-workspace", 25)
	if err != nil {
		t.Fatal(err)
	}
	if logs != "hello\n" {
		t.Fatalf("logs = %q", logs)
	}
}

func TestAppleContainerExec(t *testing.T) {
	var calls [][]string
	client := &RawClient{
		backend: BackendAppleContainer,
		appleContainer: &appleContainerConfig{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			calls = append(calls, append([]string(nil), args...))
			if got := strings.Join(args, " "); got != "exec --user root agency-henry-workspace sh -c id" {
				t.Fatalf("args = %q", got)
			}
			return []byte("uid=0(root)\n"), nil, nil
		}},
	}

	out, err := client.Exec(context.Background(), "agency-henry-workspace", "root", []string{"sh", "-c", "id"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "uid=0(root)\n" {
		t.Fatalf("out = %q", out)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestAppleContainerLifecycleCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENCY_HOME", home)
	var calls [][]string
	client := &RawClient{
		backend: BackendAppleContainer,
		appleContainer: &appleContainerConfig{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "create":
				return []byte("agency-smoke-workspace\n"), nil, nil
			default:
				return []byte(strings.Join(args[1:], " ")), nil, nil
			}
		}},
	}

	timeout := 7
	resp, err := client.ContainerCreate(
		context.Background(),
		&dockercontainer.Config{
			Image:      "alpine:latest",
			Env:        []string{"AGENCY_HOME=/home/agency"},
			Labels:     map[string]string{"agency.managed": "true", "agency.type": "workspace", "agency.agent": "smoke"},
			Cmd:        []string{"/bin/sh", "-c", "sleep 120"},
			WorkingDir: "/workspace",
			User:       "1000:1000",
		},
		&dockercontainer.HostConfig{
			ReadonlyRootfs: true,
			Binds:          []string{"/host/constraints.yaml:/agency/constraints.yaml:ro"},
			Tmpfs:          map[string]string{"/tmp": "size=64M"},
			Resources: dockercontainer.Resources{
				Memory:   128 * 1024 * 1024,
				NanoCPUs: 500_000_000,
			},
			PortBindings: nat.PortMap{
				"8080/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "18080"}},
			},
		},
		nil,
		nil,
		"agency-smoke-workspace",
	)
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != "agency-smoke-workspace" {
		t.Fatalf("id = %q", resp.ID)
	}
	if err := client.ContainerStart(context.Background(), resp.ID, dockercontainer.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := client.ContainerStop(context.Background(), resp.ID, dockercontainer.StopOptions{Timeout: &timeout}); err != nil {
		t.Fatal(err)
	}
	if err := client.ContainerRemove(context.Background(), resp.ID, dockercontainer.RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}

	if len(calls) != 4 {
		t.Fatalf("calls len = %d, want 4: %#v", len(calls), calls)
	}
	create := strings.Join(calls[0], " ")
	for _, want := range []string{
		"create",
		"--name agency-smoke-workspace",
		"--env AGENCY_HOME=/home/agency",
		"--label agency.type=workspace",
		"--label agency.agent=smoke",
		"--label agency.backend=apple-container",
		"--label agency.home=" + appleContainerHomeHash(),
		"--user 1000:1000",
		"--workdir /workspace",
		"--read-only",
		"--memory 209715200",
		"--cpus 1",
		"--mount type=bind,source=/host/constraints.yaml,target=/agency/constraints.yaml,readonly",
		"--tmpfs /tmp",
		"--publish 127.0.0.1:18080:8080/tcp",
		"alpine:latest /bin/sh -c sleep 120",
	} {
		if !strings.Contains(create, want) {
			t.Fatalf("create args missing %q in %q", want, create)
		}
	}
	if got := strings.Join(calls[1], " "); got != "start agency-smoke-workspace" {
		t.Fatalf("start = %q", got)
	}
	if got := strings.Join(calls[2], " "); got != "stop --time 7 agency-smoke-workspace" {
		t.Fatalf("stop = %q", got)
	}
	if got := strings.Join(calls[3], " "); got != "delete --force agency-smoke-workspace" {
		t.Fatalf("delete = %q", got)
	}
}

func TestAppleContainerMountFromFileBindUsesVolume(t *testing.T) {
	file := t.TempDir() + "/routing.yaml"
	if err := os.WriteFile(file, []byte("routes: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	flag, value, err := appleContainerMountFromBind(file + ":/app/routing.yaml:ro")
	if err != nil {
		t.Fatal(err)
	}
	if flag != "--volume" || value != file+":/app/routing.yaml:ro" {
		t.Fatalf("appleContainerMountFromBind() = %q %q", flag, value)
	}
}

func TestAppleContainerMountFromNamedVolumeUsesManagedHostPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENCY_HOME", home)

	flag, value, err := appleContainerMountFromBind("agency-smoke-workspace-data:/workspace:rw")
	if err != nil {
		t.Fatal(err)
	}
	if flag != "--mount" {
		t.Fatalf("flag = %q, want --mount; value=%q", flag, value)
	}
	wantSource := filepath.Join(home, "runtime", "apple-container", "volumes", "agency-smoke-workspace-data")
	if !strings.Contains(value, "source="+wantSource) {
		t.Fatalf("mount value = %q, want source %q", value, wantSource)
	}
	if !strings.Contains(value, "target=/workspace") {
		t.Fatalf("mount value = %q, want /workspace target", value)
	}
	if _, err := os.Stat(wantSource); err != nil {
		t.Fatalf("managed volume path not created: %v", err)
	}
}

func TestAppleContainerCreateArgsDedupeNetworks(t *testing.T) {
	args, err := appleContainerCreateArgs(
		&dockercontainer.Config{Image: "alpine:latest"},
		&dockercontainer.HostConfig{NetworkMode: "agency-henry-internal"},
		&dockernetwork.NetworkingConfig{EndpointsConfig: map[string]*dockernetwork.EndpointSettings{
			"agency-henry-internal": {},
			"agency-gateway":        {},
			"agency-egress-int":     {},
		}},
		nil,
		"agency-henry-enforcer",
	)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--network" {
			counts[args[i+1]]++
		}
	}
	for _, networkName := range []string{"agency-henry-internal", "agency-gateway", "agency-egress-int"} {
		if counts[networkName] != 1 {
			t.Fatalf("network %q count = %d, want 1 in %#v", networkName, counts[networkName], args)
		}
	}
}

func TestAppleContainerCreateArgsSkipsEmptyLabels(t *testing.T) {
	args, err := appleContainerCreateArgs(
		&dockercontainer.Config{
			Image:  "alpine:latest",
			Labels: map[string]string{"agency.instance": "", "agency.managed": "true"},
		},
		nil,
		nil,
		nil,
		"agency-smoke",
	)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "agency.instance=") {
		t.Fatalf("args include empty label: %q", joined)
	}
	if !strings.Contains(joined, "--label agency.managed=true") {
		t.Fatalf("args missing non-empty label: %q", joined)
	}
}

func TestAppleContainerNetworkCreateArgsSkipsEmptyLabels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENCY_HOME", home)
	args := appleContainerNetworkCreateArgs("agency-gateway", dockernetwork.CreateOptions{
		Labels: map[string]string{"agency.instance": "", "agency.managed": "true"},
	})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "agency.instance=") {
		t.Fatalf("args include empty label: %q", joined)
	}
	if !strings.Contains(joined, "--label agency.managed=true") {
		t.Fatalf("args missing non-empty label: %q", joined)
	}
	if !strings.Contains(joined, "--label agency.backend=apple-container") || !strings.Contains(joined, "--label agency.home="+appleContainerHomeHash()) {
		t.Fatalf("args missing ownership labels: %q", joined)
	}
}

func TestAppleContainerCommandEnvDropsAgencyHome(t *testing.T) {
	got := appleContainerCommandEnv([]string{
		"PATH=/bin",
		"AGENCY_HOME=/tmp/agency-test",
		"HOME=/Users/test",
	})
	want := []string{"PATH=/bin", "HOME=/Users/test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("appleContainerCommandEnv() = %#v, want %#v", got, want)
	}
}

func TestAppleContainerImageCommands(t *testing.T) {
	listJSON := []byte(`[{
		"reference":"docker.io/library/alpine:latest",
		"fullSize":"4.2 MB",
		"descriptor":{"digest":"sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11","mediaType":"application/vnd.oci.image.index.v1+json","size":9218}
	}]`)
	inspectJSON := []byte(`[{
		"name":"docker.io/library/alpine:latest",
		"index":{"digest":"sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11","size":9218,"mediaType":"application/vnd.oci.image.index.v1+json"},
		"variants":[{
			"size":4201522,
			"config":{"created":"2026-04-15T20:01:25Z","os":"linux","architecture":"arm64","variant":"v8","config":{"Env":["PATH=/bin"],"WorkingDir":"/","Cmd":["/bin/sh"],"Labels":{"agency.build.id":"abc123"}}},
			"platform":{"variant":"v8","architecture":"arm64","os":"linux"}
		}]
	}]`)
	var calls [][]string
	client := &RawClient{
		backend: BackendAppleContainer,
		appleContainer: &appleContainerConfig{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			calls = append(calls, append([]string(nil), args...))
			switch strings.Join(args, " ") {
			case "image list --format json":
				return listJSON, nil, nil
			case "image inspect docker.io/library/alpine:latest":
				return inspectJSON, nil, nil
			default:
				t.Fatalf("unexpected args: %#v", args)
			}
			return nil, nil, nil
		}},
	}
	images, err := client.ImageList(context.Background(), dockerimage.ListOptions{
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("reference", "alpine")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0].RepoTags[0] != "docker.io/library/alpine:latest" {
		t.Fatalf("images = %#v", images)
	}
	inspect, _, err := client.ImageInspectWithRaw(context.Background(), "docker.io/library/alpine:latest")
	if err != nil {
		t.Fatal(err)
	}
	if inspect.ID != "sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11" || inspect.Architecture != "arm64" || inspect.Os != "linux" {
		t.Fatalf("inspect = %#v", inspect)
	}
	if inspect.Config == nil || inspect.Config.Labels["agency.build.id"] != "abc123" {
		t.Fatalf("inspect labels = %#v", inspect.Config)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestAppleContainerImageMutationCommands(t *testing.T) {
	var calls [][]string
	client := &RawClient{
		backend: BackendAppleContainer,
		appleContainer: &appleContainerConfig{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			calls = append(calls, append([]string(nil), args...))
			return []byte(strings.Join(args, " ")), nil, nil
		}},
	}
	reader, err := client.ImagePull(context.Background(), "docker.io/library/alpine:latest", dockerimage.PullOptions{Platform: "linux/arm64"})
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	if err := client.ImageTag(context.Background(), "docker.io/library/alpine:latest", "agency-alpine:latest"); err != nil {
		t.Fatal(err)
	}
	buildArg := "abc123"
	resp, err := client.ImageBuild(context.Background(), testTarContext(t), dockertypes.ImageBuildOptions{
		Tags:       []string{"agency-test:latest"},
		Dockerfile: "Dockerfile",
		Platform:   "linux/arm64",
		BuildArgs:  map[string]*string{"BUILD_ID": &buildArg},
		Labels:     map[string]string{"agency.build.id": "abc123"},
		NoCache:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(body) {
		t.Fatalf("build response is not docker JSON stream: %q", string(body))
	}
	if len(calls) != 3 {
		t.Fatalf("calls = %#v", calls)
	}
	if got := strings.Join(calls[0], " "); got != "image pull --progress none --platform linux/arm64 docker.io/library/alpine:latest" {
		t.Fatalf("pull args = %q", got)
	}
	if got := strings.Join(calls[1], " "); got != "image tag docker.io/library/alpine:latest agency-alpine:latest" {
		t.Fatalf("tag args = %q", got)
	}
	build := strings.Join(calls[2], " ")
	for _, want := range []string{
		"build --progress plain",
		"--platform linux/arm64",
		"-f ",
		"-t agency-test:latest",
		"--build-arg BUILD_ID=abc123",
		"--label agency.build.id=abc123",
		"--no-cache",
	} {
		if !strings.Contains(build, want) {
			t.Fatalf("build args missing %q in %q", want, build)
		}
	}
}

func testTarContext(t *testing.T) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("FROM scratch\n")
	if err := tw.WriteHeader(&tar.Header{Name: "Dockerfile", Mode: 0o644, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestAppleContainerNetworkCommands(t *testing.T) {
	sample := []byte(`[
		{
			"id": "agency-smoke-net",
			"state": "running",
			"config": {
				"id": "agency-smoke-net",
				"mode": "hostOnly",
				"labels": {"agency.agent": "smoke", "agency.type": "internal"}
			},
			"status": {
				"ipv4Subnet": "192.168.128.0/24",
				"ipv4Gateway": "192.168.128.1",
				"ipv6Subnet": "fd0a:c0a8:1d80:435f::/64"
			}
		}
	]`)
	var calls [][]string
	client := &RawClient{
		backend: BackendAppleContainer,
		appleContainer: &appleContainerConfig{run: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			calls = append(calls, append([]string(nil), args...))
			if len(args) >= 3 && args[0] == "network" && args[1] == "create" {
				joined := strings.Join(args, " ")
				for _, want := range []string{
					"--label agency.agent=smoke",
					"--label agency.type=internal",
					"--internal",
					"--subnet 192.168.128.0/24",
					"agency-smoke-net",
				} {
					if !strings.Contains(joined, want) {
						t.Fatalf("network create args missing %q in %q", want, joined)
					}
				}
				return []byte("agency-smoke-net\n"), nil, nil
			}
			switch strings.Join(args, " ") {
			case "network list --format json", "network inspect agency-smoke-net":
				return sample, nil, nil
			case "network delete agency-smoke-net":
				return []byte("agency-smoke-net\n"), nil, nil
			default:
				t.Fatalf("unexpected args: %#v", args)
			}
			return nil, nil, nil
		}},
	}

	resp, err := client.NetworkCreate(context.Background(), "agency-smoke-net", dockernetwork.CreateOptions{
		Internal: true,
		Labels:   map[string]string{"agency.agent": "smoke", "agency.type": "internal"},
		IPAM: &dockernetwork.IPAM{Config: []dockernetwork.IPAMConfig{{
			Subnet: "192.168.128.0/24",
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != "agency-smoke-net" {
		t.Fatalf("network id = %q", resp.ID)
	}
	networks, err := client.NetworkList(context.Background(), dockernetwork.ListOptions{
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("label", "agency.agent=smoke")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(networks) != 1 || !networks[0].Internal || networks[0].Labels["agency.type"] != "internal" {
		t.Fatalf("networks = %#v", networks)
	}
	inspect, err := client.NetworkInspect(context.Background(), "agency-smoke-net", dockernetwork.InspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if inspect.Name != "agency-smoke-net" || !inspect.Internal || inspect.IPAM.Config[0].Gateway != "192.168.128.1" {
		t.Fatalf("inspect = %#v", inspect)
	}
	if err := client.NetworkRemove(context.Background(), "agency-smoke-net"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 {
		t.Fatalf("calls len = %d, want 4: %#v", len(calls), calls)
	}
}

func TestNerdctlCreateArgsIncludeLogConfig(t *testing.T) {
	config := &dockercontainer.Config{Image: "agency-body:latest"}
	hostConfig := &dockercontainer.HostConfig{
		LogConfig: dockercontainer.LogConfig{
			Type:   "json-file",
			Config: map[string]string{"max-size": "10m", "max-file": "3"},
		},
	}
	args, err := nerdctlCreateArgs(config, hostConfig, nil, nil, "test")
	if err != nil {
		t.Fatalf("nerdctlCreateArgs() error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--log-driver json-file", "--log-opt max-file=3", "--log-opt max-size=10m"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("nerdctlCreateArgs() missing %q in %q", want, joined)
		}
	}
}

func TestNormalizeContainerNetworkNamesResolvesSyntheticNames(t *testing.T) {
	networks := map[string]*dockernetwork.EndpointSettings{
		"unknown-eth0":      {NetworkID: "net-1"},
		"agency-egress-int": {},
	}
	got := normalizeContainerNetworkNames(networks, func(id string) string {
		if id == "net-1" {
			return "agency-gateway"
		}
		return ""
	})
	want := []string{"agency-egress-int", "agency-gateway"}
	if len(got) != len(want) {
		t.Fatalf("normalizeContainerNetworkNames() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeContainerNetworkNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestContainerNetworkNamesFromLabels(t *testing.T) {
	config := &dockercontainer.Config{
		Labels: map[string]string{
			"nerdctl/networks": "[\"agency-gateway\",\"agency-agent-internal\",\"agency-egress-int\"]",
		},
	}
	got := containerNetworkNamesFromLabels(config)
	want := []string{"agency-agent-internal", "agency-egress-int", "agency-gateway"}
	if len(got) != len(want) {
		t.Fatalf("containerNetworkNamesFromLabels() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("containerNetworkNamesFromLabels()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestHasNonSyntheticNetworkName(t *testing.T) {
	if hasNonSyntheticNetworkName([]string{"unknown-eth0", "eth1"}) {
		t.Fatal("expected only synthetic network names to be treated as synthetic")
	}
	if !hasNonSyntheticNetworkName([]string{"unknown-eth0", "agency-gateway"}) {
		t.Fatal("expected a real network name to be detected")
	}
}
