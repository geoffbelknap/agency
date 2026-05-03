package runtimebackend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

const BackendMicroagent = "microagent"
const microagentTCPVsockListenersEnv = "MICROAGENT_VSOCK_TCP_LISTENERS"
const defaultMicroagentBodyEntrypoint = "/app/entrypoint.sh"

type MicroagentCommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type MicroagentCLIRuntimeBackend struct {
	BinaryPath    string
	StateDir      string
	Entrypoint    string
	BodyImageRef  string
	Mke2fsPath    string
	RootFSSizeMiB int64
	MemoryMiB     int64
	CPUCount      int64
	RunCommand    MicroagentCommandRunner
}

func NewMicroagentCLIRuntimeBackend(home string, cfg map[string]string) *MicroagentCLIRuntimeBackend {
	stateDir := strings.TrimSpace(cfg["state_dir"])
	if stateDir == "" {
		stateDir = filepath.Join(home, "runtime", "microagent")
	}
	binaryPath := strings.TrimSpace(cfg["binary_path"])
	if binaryPath == "" {
		binaryPath = "microagent"
	}
	entrypoint := firstNonEmptyConfig(cfg, "entrypoint", "body_entrypoint")
	if entrypoint == "" {
		entrypoint = defaultMicroagentBodyEntrypoint
	}
	return &MicroagentCLIRuntimeBackend{
		BinaryPath:    binaryPath,
		StateDir:      stateDir,
		Entrypoint:    entrypoint,
		BodyImageRef:  firstNonEmptyConfig(cfg, "rootfs_oci_ref", "body_oci_ref"),
		Mke2fsPath:    strings.TrimSpace(cfg["mke2fs_path"]),
		RootFSSizeMiB: parseInt64Config(cfg["rootfs_size_mib"], 0),
		MemoryMiB:     parseInt64Config(cfg["memory_mib"], defaultFirecrackerMemoryMiB),
		CPUCount:      parseInt64Config(cfg["cpu_count"], 2),
		RunCommand:    runMicroagentCommand,
	}
}

func (b *MicroagentCLIRuntimeBackend) Name() string {
	return BackendMicroagent
}

func (b *MicroagentCLIRuntimeBackend) Ensure(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	image, err := b.microagentOCIImageRef(spec.Package.Image)
	if err != nil {
		return err
	}
	args := []string{"create",
		"--name", spec.RuntimeID,
		"--image", image,
		"--state-dir", b.StateDir,
		"--memory", strconv.FormatInt(b.MemoryMiB, 10),
		"--cpus", strconv.FormatInt(b.CPUCount, 10),
	}
	if entrypoint := strings.TrimSpace(b.Entrypoint); entrypoint != "" {
		args = append(args, "--entrypoint", entrypoint)
	}
	if mke2fs := strings.TrimSpace(b.Mke2fsPath); mke2fs != "" {
		args = append(args, "--mke2fs", mke2fs)
	}
	if b.RootFSSizeMiB > 0 {
		args = append(args, "--size-mib", strconv.FormatInt(b.RootFSSizeMiB, 10))
	}
	vsockMappings := microagentEnforcerVsockMappings(spec.Package.Env)
	for _, entry := range microagentGuestEnvWithVsockBridge(spec.Package.Env, vsockMappings) {
		args = append(args, "--env", entry)
	}
	if _, err := b.run(ctx, args...); err != nil {
		return err
	}
	startArgs := []string{"start",
		spec.RuntimeID,
		"--state-dir", b.StateDir,
		"--memory", strconv.FormatInt(b.MemoryMiB, 10),
		"--cpus", strconv.FormatInt(b.CPUCount, 10),
	}
	for _, mapping := range vsockMappings {
		startArgs = append(startArgs, "--vsock", mapping)
	}
	if _, err := b.run(ctx, startArgs...); err != nil {
		return err
	}
	return nil
}

func (b *MicroagentCLIRuntimeBackend) microagentOCIImageRef(imageRef string) (string, error) {
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" || imageRef == legacyAgencyBodyLocalTag {
		configured := strings.TrimSpace(b.BodyImageRef)
		if configured == "" {
			return "", fmt.Errorf("microagent backend: rootfs OCI artifact is not configured; set hub.deployment_backend_config.rootfs_oci_ref to a versioned OCI artifact reference")
		}
		return validateMicroVMOCIImageRef("microagent", configured)
	}
	return validateMicroVMOCIImageRef("microagent", imageRef)
}

func microagentGuestEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		if microagentHostOnlyEnv(key) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func microagentGuestEnvWithVsockBridge(env map[string]string, mappings []string) []string {
	out := microagentGuestEnv(env)
	if strings.TrimSpace(env[microagentTCPVsockListenersEnv]) != "" || len(mappings) == 0 {
		return out
	}
	bridges := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		port, _, ok := strings.Cut(mapping, "=")
		port = strings.TrimSpace(port)
		if !ok || port == "" {
			continue
		}
		bridges = append(bridges, port+"="+port)
	}
	if len(bridges) == 0 {
		return out
	}
	return append(out, microagentTCPVsockListenersEnv+"="+strings.Join(bridges, ","))
}

func microagentHostOnlyEnv(key string) bool {
	switch key {
	case FirecrackerEnforcerProxyTargetEnv, FirecrackerEnforcerControlTargetEnv, FirecrackerRootFSOverlaysEnv:
		return true
	default:
		return strings.HasPrefix(key, FirecrackerHostServiceTargetEnvBase)
	}
}

func microagentEnforcerVsockMappings(env map[string]string) []string {
	proxyTarget := microagentTargetHostPort(env[FirecrackerEnforcerProxyTargetEnv])
	controlTarget := microagentTargetHostPort(env[FirecrackerEnforcerControlTargetEnv])
	var mappings []string
	if proxyTarget != "" {
		mappings = append(mappings, "3128="+proxyTarget)
	}
	if controlTarget != "" {
		mappings = append(mappings, "8081="+controlTarget)
	}
	return mappings
}

func microagentTargetHostPort(endpoint string) string {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Host
}

func (b *MicroagentCLIRuntimeBackend) Stop(ctx context.Context, runtimeID string) error {
	var stopErr error
	if _, err := b.run(ctx, "stop", runtimeID, "--state-dir", b.StateDir); err != nil {
		stopErr = err
	}
	if _, err := b.run(ctx, "delete", runtimeID, "--state-dir", b.StateDir); err != nil {
		if stopErr != nil {
			return fmt.Errorf("%v; delete: %w", stopErr, err)
		}
		return err
	}
	return stopErr
}

func (b *MicroagentCLIRuntimeBackend) Inspect(ctx context.Context, runtimeID string) (runtimecontract.BackendStatus, error) {
	data, err := b.run(ctx, "status", runtimeID, "--state-dir", b.StateDir)
	if err != nil {
		return runtimecontract.BackendStatus{}, err
	}
	var resp microagentStatusResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return runtimecontract.BackendStatus{}, fmt.Errorf("microagent backend inspect %q: %w", runtimeID, err)
	}
	state := strings.TrimSpace(resp.Event.State)
	details := map[string]string{
		"vm_state":          state,
		"workload_vm_state": state,
		"state_dir":         b.StateDir,
	}
	if resp.Error != "" {
		details["last_error"] = resp.Error
	}
	status := runtimecontract.BackendStatus{
		RuntimeID: runtimeID,
		Details:   details,
	}
	switch state {
	case "running":
		status.Phase = runtimecontract.RuntimePhaseRunning
		status.Healthy = true
	case "starting":
		status.Phase = runtimecontract.RuntimePhaseStarting
	case "stopped", "killed", "deleted":
		status.Phase = runtimecontract.RuntimePhaseStopped
	case "failed", "start_failed", "inspect_failed", "stop_failed", "kill_failed", "delete_failed":
		status.Phase = runtimecontract.RuntimePhaseFailed
	default:
		status.Phase = runtimecontract.RuntimePhaseStopped
	}
	return status, nil
}

func (b *MicroagentCLIRuntimeBackend) Validate(ctx context.Context, runtimeID string) error {
	status, err := b.Inspect(ctx, runtimeID)
	if err != nil {
		return err
	}
	if !status.Healthy {
		return fmt.Errorf("microagent runtime %q is not running: %s", runtimeID, status.Phase)
	}
	return nil
}

func (b *MicroagentCLIRuntimeBackend) Capabilities(ctx context.Context) (runtimecontract.BackendCapabilities, error) {
	_ = ctx
	return runtimecontract.BackendCapabilities{
		SupportedTransportTypes:     []string{runtimecontract.TransportTypeVsockHTTP},
		SupportsRootless:            false,
		SupportsComposeLike:         false,
		Isolation:                   runtimecontract.IsolationMicroVM,
		RequiresKVM:                 runtime.GOOS == "linux",
		RequiresAppleVirtualization: runtime.GOOS == "darwin",
		SupportsSnapshots:           false,
	}, nil
}

func (b *MicroagentCLIRuntimeBackend) run(ctx context.Context, args ...string) ([]byte, error) {
	runner := b.RunCommand
	if runner == nil {
		runner = runMicroagentCommand
	}
	return runner(ctx, b.BinaryPath, args...)
}

func runMicroagentCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return out, nil
}

type microagentStatusResponse struct {
	OK    bool `json:"ok"`
	Event struct {
		State string `json:"state"`
	} `json:"event"`
	Error string `json:"error,omitempty"`
}
