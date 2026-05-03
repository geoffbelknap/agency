package runtimebackend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/geoffbelknap/agency/internal/pkg/pathsafety"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	BackendAppleVFMicroVM    = "apple-vf-microvm"
	legacyAgencyBodyLocalTag = "agency-body:latest"
)

func DefaultAppleVFStateDir(home string) string {
	home = strings.TrimSpace(home)
	if home == "" {
		return filepath.Join(os.TempDir(), "agency-apple-vf-microvm")
	}
	return filepath.Join(home, "runtime", "apple-vf-microvm")
}

func DefaultAppleVFKernelPath(home string) string {
	return filepath.Join(DefaultAppleVFStateDir(home), "artifacts", "Image")
}

type AppleVFMicroVMRuntimeBackend struct {
	HelperBinary    string
	KernelPath      string
	StateDir        string
	MemoryMiB       int64
	CPUCount        int64
	EnforcementMode string
	BodyImageRef    string
	RootFSBuilder   appleVFRootFSBuilder
}

type appleVFRootFSBuilder interface {
	Build(ctx context.Context, imageRef, outPath string, env map[string]string) (MicroVMOCIRootFSResult, error)
}

func NewAppleVFMicroVMRuntimeBackend(home string, cfg map[string]string) *AppleVFMicroVMRuntimeBackend {
	stateDir := strings.TrimSpace(cfg["state_dir"])
	if stateDir == "" {
		stateDir = DefaultAppleVFStateDir(home)
	}
	kernelPath := strings.TrimSpace(cfg["kernel_path"])
	if kernelPath == "" {
		kernelPath = DefaultAppleVFKernelPath(home)
	}
	enforcementMode, err := parseFirecrackerEnforcementMode(cfg["enforcement_mode"])
	if err != nil {
		enforcementMode = FirecrackerEnforcementModeHostProcess
	}
	backend := &AppleVFMicroVMRuntimeBackend{
		HelperBinary:    strings.TrimSpace(cfg["helper_binary"]),
		KernelPath:      kernelPath,
		StateDir:        stateDir,
		MemoryMiB:       parseInt64Config(cfg["memory_mib"], defaultFirecrackerMemoryMiB),
		CPUCount:        parseInt64Config(cfg["cpu_count"], 2),
		EnforcementMode: enforcementMode,
		BodyImageRef:    firstNonEmptyConfig(cfg, "rootfs_oci_ref", "body_oci_ref"),
	}
	backend.RootFSBuilder = &MicroVMOCIRootFSBuilder{
		StateDir:          stateDir,
		Mke2fsPath:        strings.TrimSpace(cfg["mke2fs_path"]),
		SizeMiB:           parseInt64Config(cfg["rootfs_size_mib"], defaultFirecrackerRootFSMiB),
		VsockBridgeBinary: strings.TrimSpace(cfg["vsock_bridge_binary_path"]),
		OverlayBaseDir:    strings.TrimSpace(home),
		Platform:          ocispec.Platform{OS: "linux", Architecture: "arm64"},
	}
	return backend
}

func (b *AppleVFMicroVMRuntimeBackend) Name() string {
	return BackendAppleVFMicroVM
}

func (b *AppleVFMicroVMRuntimeBackend) Ensure(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	runtimeID, err := pathsafety.Segment("apple-vf runtime id", spec.RuntimeID)
	if err != nil {
		return fmt.Errorf("apple-vf-microvm backend: %w", err)
	}
	spec.RuntimeID = runtimeID
	rootfs, err := b.PrepareRootFS(ctx, spec)
	if err != nil {
		_ = b.cleanupRuntimeState(runtimeID)
		return err
	}
	if _, err := AppleVFHelperStart(ctx, b.HelperBinary, b.PrepareHelperRequest(spec, rootfs)); err != nil {
		_ = b.cleanupHelperState(ctx, runtimeID)
		_ = b.cleanupRuntimeState(runtimeID)
		return fmt.Errorf("apple-vf-microvm backend start %q: %w", spec.RuntimeID, err)
	}
	return nil
}

func (b *AppleVFMicroVMRuntimeBackend) Stop(ctx context.Context, runtimeID string) error {
	req, err := b.runtimeHelperRequest(runtimeID)
	if err != nil {
		return err
	}
	var errs []error
	if _, err := AppleVFHelperStop(ctx, b.HelperBinary, req); err != nil && !appleVFHelperStateMissing(err) {
		errs = append(errs, fmt.Errorf("apple-vf-microvm backend stop %q: %w", runtimeID, err))
	}
	if err := b.cleanupHelperState(ctx, runtimeID); err != nil {
		errs = append(errs, err)
	}
	if err := b.cleanupRuntimeState(runtimeID); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (b *AppleVFMicroVMRuntimeBackend) Inspect(ctx context.Context, runtimeID string) (runtimecontract.BackendStatus, error) {
	req, err := b.runtimeHelperRequest(runtimeID)
	if err != nil {
		return runtimecontract.BackendStatus{}, err
	}
	resp, err := AppleVFHelperInspect(ctx, b.HelperBinary, req)
	if err != nil {
		return runtimecontract.BackendStatus{}, fmt.Errorf("apple-vf-microvm backend inspect %q: %w", runtimeID, err)
	}
	return appleVFBackendStatus(runtimeID, resp), nil
}

func (b *AppleVFMicroVMRuntimeBackend) Validate(ctx context.Context, runtimeID string) error {
	status, err := b.Inspect(ctx, runtimeID)
	if err != nil {
		return err
	}
	if !status.Healthy {
		return fmt.Errorf("apple-vf-microvm runtime %q is not running: %s", runtimeID, status.Phase)
	}
	return nil
}

func (b *AppleVFMicroVMRuntimeBackend) Capabilities(ctx context.Context) (runtimecontract.BackendCapabilities, error) {
	_ = ctx
	return runtimecontract.BackendCapabilities{
		SupportedTransportTypes:     []string{runtimecontract.TransportTypeVsockHTTP},
		SupportsRootless:            false,
		SupportsComposeLike:         false,
		Isolation:                   runtimecontract.IsolationMicroVM,
		RequiresKVM:                 false,
		RequiresAppleVirtualization: true,
		SupportsSnapshots:           false,
	}, nil
}

func (b *AppleVFMicroVMRuntimeBackend) PrepareRootFS(ctx context.Context, spec runtimecontract.RuntimeSpec) (MicroVMRootFS, error) {
	if b.RootFSBuilder == nil {
		return MicroVMRootFS{}, fmt.Errorf("apple-vf-microvm rootfs builder is not configured")
	}
	runtimeID, err := pathsafety.Segment("apple-vf runtime id", spec.RuntimeID)
	if err != nil {
		return MicroVMRootFS{}, fmt.Errorf("apple-vf-microvm rootfs: %w", err)
	}
	taskDir, err := pathsafety.Join(b.stateDir(), "tasks", runtimeID)
	if err != nil {
		return MicroVMRootFS{}, err
	}
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return MicroVMRootFS{}, fmt.Errorf("create apple-vf task rootfs dir: %w", err)
	}
	taskPath, err := pathsafety.Join(taskDir, "rootfs.ext4")
	if err != nil {
		return MicroVMRootFS{}, err
	}
	imageRef, err := b.appleVFOCIImageRef(spec.Package.Image)
	if err != nil {
		return MicroVMRootFS{}, err
	}
	result, err := b.RootFSBuilder.Build(ctx, imageRef, taskPath, spec.Package.Env)
	if err != nil {
		return MicroVMRootFS{}, err
	}
	return MicroVMRootFS{
		ImageRef: result.ImageRef,
		Digest:   result.Manifest.Digest.String(),
		Path:     result.RootFSPath,
		InitPath: result.InitPath,
	}, nil
}

func (b *AppleVFMicroVMRuntimeBackend) appleVFOCIImageRef(imageRef string) (string, error) {
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" || imageRef == legacyAgencyBodyLocalTag {
		configured := strings.TrimSpace(b.BodyImageRef)
		if configured == "" {
			return "", fmt.Errorf("apple-vf-microvm rootfs OCI artifact is not configured; set hub.deployment_backend_config.rootfs_oci_ref to a versioned OCI artifact reference")
		}
		return validateAppleVFOCIImageRef(configured)
	}
	return validateAppleVFOCIImageRef(imageRef)
}

func validateAppleVFOCIImageRef(imageRef string) (string, error) {
	return validateMicroVMOCIImageRef("apple-vf-microvm", imageRef)
}

func validateMicroVMOCIImageRef(backend, imageRef string) (string, error) {
	imageRef = strings.TrimSpace(imageRef)
	if strings.HasSuffix(imageRef, ":latest") {
		return "", fmt.Errorf("%s rootfs OCI artifact must not use mutable :latest tag: %s", backend, imageRef)
	}
	return imageRef, nil
}

func firstNonEmptyConfig(cfg map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(cfg[key]); value != "" {
			return value
		}
	}
	return ""
}

func (b *AppleVFMicroVMRuntimeBackend) PrepareHelperRequest(spec runtimecontract.RuntimeSpec, rootfs MicroVMRootFS) AppleVFHelperRequest {
	return AppleVFHelperRequest{
		RequestID:      "prepare-" + strings.TrimSpace(spec.RuntimeID),
		RuntimeID:      strings.TrimSpace(spec.RuntimeID),
		Role:           AppleVFRoleWorkload,
		Backend:        BackendAppleVFMicroVM,
		AgencyHomeHash: appleVFAgencyHomeHash(b.stateDir()),
		Config: &AppleVFHelperVMConfig{
			KernelPath:      strings.TrimSpace(b.KernelPath),
			RootFSPath:      strings.TrimSpace(rootfs.Path),
			StateDir:        filepath.Join(b.stateDir(), "vms", strings.TrimSpace(spec.RuntimeID)),
			MemoryMiB:       b.MemoryMiB,
			CPUCount:        b.CPUCount,
			EnforcementMode: b.EnforcementMode,
			VsockListeners:  appleVFVsockListeners(spec),
		},
	}
}

func (b *AppleVFMicroVMRuntimeBackend) runtimeHelperRequest(runtimeID string) (AppleVFHelperRequest, error) {
	runtimeID, err := pathsafety.Segment("apple-vf runtime id", runtimeID)
	if err != nil {
		return AppleVFHelperRequest{}, fmt.Errorf("apple-vf-microvm backend: %w", err)
	}
	return AppleVFHelperRequest{
		RequestID:      "runtime-" + runtimeID,
		RuntimeID:      runtimeID,
		Role:           AppleVFRoleWorkload,
		Backend:        BackendAppleVFMicroVM,
		AgencyHomeHash: appleVFAgencyHomeHash(b.stateDir()),
		Config: &AppleVFHelperVMConfig{
			StateDir: filepath.Join(b.stateDir(), "vms", runtimeID),
		},
	}, nil
}

func (b *AppleVFMicroVMRuntimeBackend) cleanupHelperState(ctx context.Context, runtimeID string) error {
	req, err := b.runtimeHelperRequest(runtimeID)
	if err != nil {
		return err
	}
	if _, err := AppleVFHelperDelete(ctx, b.HelperBinary, req); err != nil && !appleVFHelperStateMissing(err) {
		return fmt.Errorf("apple-vf-microvm backend delete %q: %w", runtimeID, err)
	}
	return nil
}

func (b *AppleVFMicroVMRuntimeBackend) cleanupRuntimeState(runtimeID string) error {
	runtimeID, err := pathsafety.Segment("apple-vf runtime id", runtimeID)
	if err != nil {
		return fmt.Errorf("apple-vf-microvm backend cleanup: %w", err)
	}
	taskDir, err := pathsafety.Join(b.stateDir(), "tasks", runtimeID)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(taskDir); err != nil {
		return fmt.Errorf("remove apple-vf task state: %w", err)
	}
	return nil
}

func appleVFHelperStateMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "couldn’t be opened") ||
		strings.Contains(msg, "couldn't be opened")
}

func appleVFBackendStatus(runtimeID string, resp AppleVFHelperResponse) runtimecontract.BackendStatus {
	details := map[string]string{
		"vm_state":          resp.VMState,
		"workload_vm_state": resp.VMState,
		"enforcement_mode":  "",
	}
	for key, value := range resp.Details {
		details[key] = value
	}
	out := runtimecontract.BackendStatus{
		RuntimeID: runtimeID,
		Details:   details,
	}
	switch resp.VMState {
	case "running":
		out.Phase = runtimecontract.RuntimePhaseRunning
		out.Healthy = true
	case "starting":
		out.Phase = runtimecontract.RuntimePhaseStarting
	case "stopped", "killed", "deleted":
		out.Phase = runtimecontract.RuntimePhaseStopped
	case "failed", "start_failed", "inspect_failed", "stop_failed", "kill_failed", "delete_failed":
		out.Phase = runtimecontract.RuntimePhaseFailed
	default:
		out.Phase = runtimecontract.RuntimePhaseStopped
	}
	if resp.Error != "" {
		details["last_error"] = resp.Error
	}
	return out
}

func appleVFVsockListeners(spec runtimecontract.RuntimeSpec) []AppleVFHelperVsockListener {
	targets, err := firecrackerEnforcerTargets(spec)
	if err != nil {
		return nil
	}
	ports := make([]int, 0, len(targets))
	for port := range targets {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	listeners := make([]AppleVFHelperVsockListener, 0, len(ports))
	for _, port := range ports {
		listeners = append(listeners, AppleVFHelperVsockListener{
			Port:   int64(port),
			Target: targets[port],
		})
	}
	return listeners
}

func (b *AppleVFMicroVMRuntimeBackend) PrepareWithHelper(ctx context.Context, spec runtimecontract.RuntimeSpec) (AppleVFHelperResponse, error) {
	rootfs, err := b.PrepareRootFS(ctx, spec)
	if err != nil {
		return AppleVFHelperResponse{}, err
	}
	return AppleVFHelperPrepare(ctx, b.HelperBinary, b.PrepareHelperRequest(spec, rootfs))
}

func (b *AppleVFMicroVMRuntimeBackend) stateDir() string {
	stateDir := strings.TrimSpace(b.StateDir)
	if stateDir != "" {
		return stateDir
	}
	return filepath.Join(os.TempDir(), "agency-apple-vf-microvm")
}

func appleVFAgencyHomeHash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}
