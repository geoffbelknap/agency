package runtimebackend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

const (
	BackendFirecracker                    = "firecracker"
	FirecrackerEnforcementModeHostProcess = "host-process"
	FirecrackerEnforcementModeMicroVM     = "microvm"
	defaultFirecrackerMemoryMiB           = 512

	FirecrackerEnforcerProxyTargetEnv   = "AGENCY_FIRECRACKER_ENFORCER_PROXY_TARGET"
	FirecrackerEnforcerControlTargetEnv = "AGENCY_FIRECRACKER_ENFORCER_CONTROL_TARGET"
)

type FirecrackerRuntimeBackend struct {
	BinaryPath      string
	KernelPath      string
	StateDir        string
	MemoryMiB       int64
	EnforcementMode string
	Images          *FirecrackerImageStore
	Tasks           *FirecrackerVMSupervisor
	Vsock           *FirecrackerVsockListenerFactory

	configErr error
}

func NewFirecrackerRuntimeBackend(home string, cfg map[string]string) *FirecrackerRuntimeBackend {
	stateDir := strings.TrimSpace(cfg["state_dir"])
	if stateDir == "" {
		if strings.TrimSpace(home) != "" {
			stateDir = filepath.Join(home, "firecracker")
		} else {
			stateDir = filepath.Join(os.TempDir(), "agency-firecracker")
		}
	}
	binaryPath := strings.TrimSpace(cfg["binary_path"])
	if binaryPath == "" {
		binaryPath = "firecracker"
	}
	enforcementMode, modeErr := parseFirecrackerEnforcementMode(cfg["enforcement_mode"])
	backend := &FirecrackerRuntimeBackend{
		BinaryPath:      binaryPath,
		KernelPath:      strings.TrimSpace(cfg["kernel_path"]),
		StateDir:        stateDir,
		MemoryMiB:       parseInt64Config(cfg["memory_mib"], defaultFirecrackerMemoryMiB),
		EnforcementMode: enforcementMode,
		configErr:       modeErr,
	}
	backend.Images = &FirecrackerImageStore{
		StateDir:          stateDir,
		PodmanPath:        strings.TrimSpace(cfg["podman_path"]),
		Mke2fsPath:        strings.TrimSpace(cfg["mke2fs_path"]),
		SizeMiB:           parseInt64Config(cfg["rootfs_size_mib"], defaultFirecrackerRootFSMiB),
		VsockBridgeBinary: strings.TrimSpace(cfg["vsock_bridge_binary_path"]),
	}
	backend.Tasks = &FirecrackerVMSupervisor{
		BinaryPath:  binaryPath,
		LogDir:      filepath.Join(stateDir, "logs"),
		PIDDir:      filepath.Join(stateDir, "pids"),
		StopTimeout: parseDurationConfig(cfg["stop_timeout"], 10*time.Second),
	}
	backend.Vsock = &FirecrackerVsockListenerFactory{StateDir: stateDir}
	return backend
}

func (b *FirecrackerRuntimeBackend) Name() string {
	return BackendFirecracker
}

func (b *FirecrackerRuntimeBackend) Ensure(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	if err := b.validateConfig(); err != nil {
		return err
	}
	if strings.TrimSpace(b.KernelPath) == "" {
		return fmt.Errorf("firecracker backend: kernel path is not configured")
	}
	if strings.TrimSpace(spec.RuntimeID) == "" {
		return fmt.Errorf("firecracker backend: runtime id is required")
	}
	rootfs, err := b.imageStore().PrepareTaskRootFS(ctx, spec)
	if err != nil {
		return err
	}
	targets, err := firecrackerEnforcerTargets(spec)
	if err != nil {
		return err
	}
	bridge, err := b.vsockFactory().Start(ctx, spec.RuntimeID, targets)
	if err != nil {
		return err
	}
	_ = os.Remove(bridge.UDSBase)
	configPath, err := b.writeConfig(spec, rootfs.Path, bridge.UDSBase)
	if err != nil {
		b.vsockFactory().Stop(spec.RuntimeID)
		return err
	}
	if err := b.supervisor().Start(ctx, spec, []string{"--no-api", "--config-file", configPath}); err != nil {
		b.vsockFactory().Stop(spec.RuntimeID)
		return err
	}
	return nil
}

func (b *FirecrackerRuntimeBackend) Stop(ctx context.Context, runtimeID string) error {
	if err := b.supervisor().Stop(ctx, runtimeID); err != nil {
		return err
	}
	b.vsockFactory().Stop(runtimeID)
	return b.cleanupRuntimeState(runtimeID)
}

func (b *FirecrackerRuntimeBackend) Inspect(ctx context.Context, runtimeID string) (runtimecontract.BackendStatus, error) {
	_ = ctx
	status, err := b.supervisor().Inspect(runtimeID)
	if err != nil {
		return runtimecontract.BackendStatus{}, err
	}
	out := runtimecontract.BackendStatus{
		RuntimeID: runtimeID,
		Details: map[string]string{
			"vm_state":         status.State,
			"pid":              strconv.Itoa(status.PID),
			"exit_code":        strconv.Itoa(status.ExitCode),
			"crashes":          strconv.Itoa(status.Crashes),
			"restarts":         strconv.Itoa(status.Restarts),
			"enforcement_mode": b.enforcementMode(),
			"log_path":         status.LogPath,
		},
	}
	if status.LastError != "" {
		out.Details["last_error"] = status.LastError
	}
	switch status.State {
	case FirecrackerVMRunning:
		out.Phase = runtimecontract.RuntimePhaseRunning
		out.Healthy = true
	case FirecrackerVMStarting:
		out.Phase = runtimecontract.RuntimePhaseStarting
	case FirecrackerVMStopping, FirecrackerVMStopped:
		out.Phase = runtimecontract.RuntimePhaseStopped
	case FirecrackerVMCrashed:
		out.Phase = runtimecontract.RuntimePhaseFailed
		if out.Details["last_error"] == "" {
			out.Details["last_error"] = "microVM exited unexpectedly"
		}
	default:
		out.Phase = runtimecontract.RuntimePhaseStopped
	}
	return out, nil
}

func (b *FirecrackerRuntimeBackend) Validate(ctx context.Context, runtimeID string) error {
	_ = ctx
	if err := b.validateConfig(); err != nil {
		return err
	}
	status, err := b.Inspect(ctx, runtimeID)
	if err != nil {
		return err
	}
	if !status.Healthy {
		return fmt.Errorf("firecracker runtime %q is not running: %s", runtimeID, status.Phase)
	}
	bridge := b.vsockFactory().Bridge(runtimeID)
	if bridge == nil {
		return fmt.Errorf("firecracker runtime %q vsock bridge is not running", runtimeID)
	}
	for _, path := range bridge.Paths {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("firecracker runtime %q vsock bridge: %w", runtimeID, err)
		}
	}
	return nil
}

func (b *FirecrackerRuntimeBackend) Capabilities(ctx context.Context) (runtimecontract.BackendCapabilities, error) {
	_ = ctx
	if err := b.validateConfig(); err != nil {
		return runtimecontract.BackendCapabilities{}, err
	}
	return runtimecontract.BackendCapabilities{
		SupportedTransportTypes: []string{runtimecontract.TransportTypeVsockHTTP},
		SupportsRootless:        false,
		SupportsComposeLike:     false,
		Isolation:               runtimecontract.IsolationMicroVM,
		RequiresKVM:             true,
		SupportsSnapshots:       true,
	}, nil
}

func (b *FirecrackerRuntimeBackend) imageStore() *FirecrackerImageStore {
	if b.Images != nil {
		return b.Images
	}
	b.Images = &FirecrackerImageStore{StateDir: b.StateDir}
	return b.Images
}

func (b *FirecrackerRuntimeBackend) supervisor() *FirecrackerVMSupervisor {
	if b.Tasks != nil {
		return b.Tasks
	}
	b.Tasks = &FirecrackerVMSupervisor{BinaryPath: b.BinaryPath, LogDir: filepath.Join(b.StateDir, "logs")}
	return b.Tasks
}

func (b *FirecrackerRuntimeBackend) vsockFactory() *FirecrackerVsockListenerFactory {
	if b.Vsock != nil {
		return b.Vsock
	}
	b.Vsock = &FirecrackerVsockListenerFactory{StateDir: b.StateDir}
	return b.Vsock
}

func (b *FirecrackerRuntimeBackend) cleanupRuntimeState(runtimeID string) error {
	runtimeID = strings.TrimSpace(runtimeID)
	if runtimeID == "" {
		return nil
	}
	for _, path := range []string{
		filepath.Join(b.StateDir, runtimeID),
		filepath.Join(b.StateDir, "tasks", runtimeID),
		filepath.Join(b.StateDir, "pids", runtimeID+".pid"),
	} {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove firecracker runtime state %s: %w", path, err)
		}
	}
	return nil
}

func (b *FirecrackerRuntimeBackend) writeConfig(spec runtimecontract.RuntimeSpec, rootfsPath, udsBase string) (string, error) {
	dir := filepath.Join(b.StateDir, spec.RuntimeID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create firecracker config dir: %w", err)
	}
	cfg := firecrackerConfig{
		BootSource: firecrackerBootSource{
			KernelImagePath: b.KernelPath,
			BootArgs:        "console=ttyS0 reboot=k panic=1 pci=off init=" + firecrackerInitPath,
		},
		Drives: []firecrackerDrive{{
			DriveID:      "rootfs",
			PathOnHost:   rootfsPath,
			IsRootDevice: true,
			IsReadOnly:   false,
		}},
		Vsock: firecrackerVsockConfig{
			VsockID:  "vsock0",
			GuestCID: 3,
			UDSPath:  udsBase,
		},
		MachineConfig: firecrackerMachineConfig{
			VCPUCount:  1,
			MemSizeMiB: b.memoryMiB(),
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "firecracker.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write firecracker config: %w", err)
	}
	return path, nil
}

func (b *FirecrackerRuntimeBackend) validateConfig() error {
	if b.configErr != nil {
		return b.configErr
	}
	if _, err := parseFirecrackerEnforcementMode(b.EnforcementMode); err != nil {
		return err
	}
	return nil
}

func (b *FirecrackerRuntimeBackend) memoryMiB() int64 {
	if b.MemoryMiB > 0 {
		return b.MemoryMiB
	}
	return defaultFirecrackerMemoryMiB
}

func (b *FirecrackerRuntimeBackend) enforcementMode() string {
	mode, err := parseFirecrackerEnforcementMode(b.EnforcementMode)
	if err != nil {
		return b.EnforcementMode
	}
	return mode
}

type firecrackerConfig struct {
	BootSource    firecrackerBootSource    `json:"boot-source"`
	Drives        []firecrackerDrive       `json:"drives"`
	Vsock         firecrackerVsockConfig   `json:"vsock"`
	MachineConfig firecrackerMachineConfig `json:"machine-config"`
}

type firecrackerBootSource struct {
	KernelImagePath string `json:"kernel_image_path"`
	BootArgs        string `json:"boot_args"`
}

type firecrackerDrive struct {
	DriveID      string `json:"drive_id"`
	PathOnHost   string `json:"path_on_host"`
	IsRootDevice bool   `json:"is_root_device"`
	IsReadOnly   bool   `json:"is_read_only"`
}

type firecrackerVsockConfig struct {
	VsockID  string `json:"vsock_id"`
	GuestCID uint32 `json:"guest_cid"`
	UDSPath  string `json:"uds_path"`
}

type firecrackerMachineConfig struct {
	VCPUCount  int64 `json:"vcpu_count"`
	MemSizeMiB int64 `json:"mem_size_mib"`
}

func firecrackerEnforcerTarget(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("firecracker backend: enforcer endpoint is not configured")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" {
		return endpoint, nil
	}
	switch parsed.Scheme {
	case "http", "https", "tcp":
		return parsed.Host, nil
	case "unix":
		return "unix://" + parsed.Path, nil
	default:
		return "", fmt.Errorf("firecracker backend: unsupported enforcer endpoint %q", endpoint)
	}
}

func firecrackerEnforcerTargets(spec runtimecontract.RuntimeSpec) (map[int]string, error) {
	proxyEndpoint := strings.TrimSpace(spec.Package.Env[FirecrackerEnforcerProxyTargetEnv])
	controlEndpoint := strings.TrimSpace(spec.Package.Env[FirecrackerEnforcerControlTargetEnv])
	if proxyEndpoint == "" && controlEndpoint == "" {
		target, err := firecrackerEnforcerTarget(spec.Transport.Enforcer.Endpoint)
		if err != nil {
			return nil, err
		}
		return map[int]string{9999: target}, nil
	}
	targets := make(map[int]string, 2)
	if proxyEndpoint != "" {
		target, err := firecrackerEnforcerTarget(proxyEndpoint)
		if err != nil {
			return nil, err
		}
		targets[3128] = target
	}
	if controlEndpoint != "" {
		target, err := firecrackerEnforcerTarget(controlEndpoint)
		if err != nil {
			return nil, err
		}
		targets[8081] = target
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("firecracker backend: no enforcer targets configured")
	}
	return targets, nil
}

func parseInt64Config(raw string, fallback int64) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func parseDurationConfig(raw string, fallback time.Duration) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	if value, err := time.ParseDuration(raw); err == nil && value > 0 {
		return value
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func parseFirecrackerEnforcementMode(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return FirecrackerEnforcementModeHostProcess, nil
	case FirecrackerEnforcementModeHostProcess:
		return FirecrackerEnforcementModeHostProcess, nil
	case FirecrackerEnforcementModeMicroVM:
		return FirecrackerEnforcementModeMicroVM, nil
	default:
		return "", fmt.Errorf("firecracker backend: unsupported enforcement_mode %q", raw)
	}
}
