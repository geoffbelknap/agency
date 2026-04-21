package runtimehost

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	dockertypes "github.com/docker/docker/api/types"
)

// BackendProbe describes a container backend that Agency can run against.
// The KnownBackends registry is the source of truth; adding a new backend
// (e.g. apple-container) is a single entry in that slice.
type BackendProbe struct {
	// Name is the canonical backend name ("docker", "podman", "containerd").
	Name string
	// CLICommand is the binary name to look up on PATH — informational only;
	// Agency connects directly to the socket via the Docker/nerdctl SDK, so
	// a missing CLI is not automatically fatal.
	CLICommand string
	// Platforms lists the host OSes where this backend can run rootless.
	// Values: "linux", "darwin", "wsl". WSL is treated as linux-with-caveats.
	Platforms []string
}

// BackendDetection is the result of probing one backend.
type BackendDetection struct {
	Probe     BackendProbe
	CLIFound  bool              // exec.LookPath(probe.CLICommand) succeeded
	Reachable bool              // Client.Ping succeeded against the resolved socket
	Endpoint  string            // resolved socket URL when Reachable
	Mode      string            // "rootless"/"rootful" (podman/containerd only)
	Config    map[string]string // deployment_backend_config to persist in config.yaml
	Err       error             // Ping/construction error when !Reachable
}

// Name returns the canonical backend name (shortcut for d.Probe.Name).
func (d BackendDetection) Name() string { return d.Probe.Name }

// pingTimeout bounds how long each backend probe can take.
const pingTimeout = 3 * time.Second

// KnownBackends returns the ordered preference list of container backends
// for the current host platform.
//
// Preference order (first = default when multiple are reachable):
//  1. podman     — rootless by default, aligns with ASK least-privilege
//  2. docker     — mature, broad compatibility
//  3. apple-container — native macOS VM-backed containers
//  4. containerd — via nerdctl, for minimal/k8s-style hosts
//
// Order is deliberate — a host with both docker and podman will default
// to podman. Callers can override via config or AGENCY_CONTAINER_BACKEND.
func KnownBackends() []BackendProbe {
	return knownBackendsFor(currentPlatform())
}

// knownBackendsFor is KnownBackends with the platform injected for testability.
func knownBackendsFor(platform string) []BackendProbe {
	all := []BackendProbe{
		{
			Name:       BackendPodman,
			CLICommand: "podman",
			Platforms:  []string{"linux", "darwin", "wsl"},
		},
		{
			Name:       BackendDocker,
			CLICommand: "docker",
			Platforms:  []string{"linux", "darwin", "wsl", "windows"},
		},
		{
			Name:       BackendAppleContainer,
			CLICommand: "container",
			Platforms:  []string{"darwin"},
		},
		{
			Name:       BackendContainerd,
			CLICommand: "nerdctl",
			Platforms:  []string{"linux", "wsl"},
		},
	}
	filtered := make([]BackendProbe, 0, len(all))
	for _, p := range all {
		for _, plat := range p.Platforms {
			if plat == platform {
				filtered = append(filtered, p)
				break
			}
		}
	}
	return filtered
}

// currentPlatform returns one of "linux", "darwin", "wsl", "windows"
// suitable for matching against BackendProbe.Platforms.
func currentPlatform() string {
	switch runtime.GOOS {
	case "darwin":
		return "darwin"
	case "windows":
		return "windows"
	case "linux":
		if isWSLHost() {
			return "wsl"
		}
		return "linux"
	default:
		return runtime.GOOS
	}
}

// isWSLHost reports whether the current Linux host is running inside WSL.
func isWSLHost() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	s := strings.ToLower(string(data))
	return strings.Contains(s, "microsoft") || strings.Contains(s, "wsl")
}

// clientFactory constructs a pingable client for a backend. Broken out as
// a var so tests can inject a fake without spinning up real daemons.
var clientFactory = func(backend string, cfg map[string]string) (pingable, error) {
	c, err := newRawClientForBackend(backend, cfg)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// lookPath is indirected via a var for the same reason as clientFactory.
var lookPath = exec.LookPath

// pingable is the subset of RawClient used by the probe.
type pingable interface {
	Ping(ctx context.Context) (dockertypes.Ping, error)
}

// ProbeBackend probes a single backend: CLI presence, client construction,
// and a short Ping against the resolved socket. Never returns an error —
// populates BackendDetection.Err and leaves Reachable=false instead.
func ProbeBackend(p BackendProbe) BackendDetection {
	d := BackendDetection{Probe: p}

	if p.CLICommand != "" {
		if _, err := lookPath(p.CLICommand); err == nil {
			d.CLIFound = true
		}
	}

	cfg := probeConfig(p.Name)
	endpoint := resolveBackendHost(p.Name, cfg)
	if endpoint == "" {
		d.Err = fmt.Errorf("no socket/endpoint resolved for %s", p.Name)
		return d
	}
	d.Endpoint = endpoint
	d.Mode = backendModeFromEndpoint(p.Name, endpoint)
	d.Config = cfg

	cli, err := clientFactory(p.Name, cfg)
	if err != nil {
		d.Err = fmt.Errorf("construct %s client: %w", p.Name, err)
		return d
	}
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		d.Err = fmt.Errorf("ping %s at %s: %w", p.Name, endpoint, err)
		return d
	}
	d.Reachable = true
	return d
}

// probeConfig derives the deployment_backend_config map used to construct
// a client for probing. For podman/containerd this captures the resolved
// socket path so it can be persisted verbatim in config.yaml after
// detection — avoids drift between probe time and runtime.
func probeConfig(backend string) map[string]string {
	backend = NormalizeContainerBackend(backend)
	endpoint := resolveBackendHost(backend, nil)
	if endpoint == "" {
		return nil
	}
	switch backend {
	case BackendPodman:
		return map[string]string{"host": endpoint}
	case BackendContainerd:
		return map[string]string{"native_socket": strings.TrimPrefix(endpoint, "unix://")}
	default:
		// Docker: the SDK honours DOCKER_HOST / standard socket locations
		// implicitly, and Agency does not currently persist docker socket
		// overrides — persist empty and let the SDK resolve at runtime.
		return nil
	}
}

// ProbeAllBackends runs ProbeBackend on every entry in KnownBackends().
// Detections are returned in KnownBackends preference order.
func ProbeAllBackends() []BackendDetection {
	probes := KnownBackends()
	out := make([]BackendDetection, 0, len(probes))
	for _, p := range probes {
		out = append(out, ProbeBackend(p))
	}
	return out
}

// SelectReachable filters a detection list to the ones where Reachable=true,
// preserving input order.
func SelectReachable(detections []BackendDetection) []BackendDetection {
	out := detections[:0:0]
	for _, d := range detections {
		if d.Reachable {
			out = append(out, d)
		}
	}
	return out
}

// PreferredReachable returns the first Reachable detection in preference
// order, or (zero, false) if none are reachable.
func PreferredReachable(detections []BackendDetection) (BackendDetection, bool) {
	for _, d := range detections {
		if d.Reachable {
			return d, true
		}
	}
	return BackendDetection{}, false
}

// InstallHint returns a platform-appropriate, one-block string explaining
// how to install the backend Agency recommends for this host. Used when
// no backends are reachable and the user needs to install one before setup
// can continue.
//
// Agency's default recommendation is podman rootless everywhere; the hint
// branches on runtime.GOOS / WSL to pick the most reliable install path.
func InstallHint() string {
	switch currentPlatform() {
	case "darwin":
		return strings.Join([]string{
			"Agency recommends rootless podman on macOS.",
			"",
			"  brew install podman",
			"  podman machine init",
			"  podman machine start",
			"",
			"Alternatives: OrbStack, Docker Desktop, Colima, Podman Desktop.",
		}, "\n")
	case "wsl":
		return strings.Join([]string{
			"Agency recommends rootless podman inside your WSL distro.",
			"",
			"  sudo apt-get install -y podman          # Ubuntu/Debian WSL",
			"",
			"Alternative: Docker Desktop with WSL integration (install on Windows host).",
		}, "\n")
	case "linux":
		return strings.Join([]string{
			"Agency recommends rootless podman on Linux.",
			"",
			"  sudo apt-get install -y podman          # Ubuntu/Debian",
			"  sudo dnf install -y podman              # Fedora/RHEL",
			"  sudo pacman -S podman                   # Arch",
			"  brew install podman                     # linuxbrew",
			"",
			"Rootless podman also needs /etc/subuid and /etc/subgid entries for your user;",
			"distro packages usually add them. If podman fails with 'newuidmap' errors, run:",
			"",
			"  sudo usermod --add-subuids 100000-165535 --add-subgids 100000-165535 $USER",
		}, "\n")
	case "windows":
		return strings.Join([]string{
			"Agency on Windows runs inside WSL. Install a WSL distro (e.g. Ubuntu),",
			"then install podman inside it:",
			"",
			"  wsl --install -d Ubuntu",
			"  wsl sudo apt-get install -y podman",
			"",
			"Alternative: Docker Desktop with WSL integration.",
		}, "\n")
	default:
		return "Install a container backend: https://podman.io/docs/installation"
	}
}
