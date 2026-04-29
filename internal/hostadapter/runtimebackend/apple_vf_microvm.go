package runtimebackend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

const (
	BackendAppleVFMicroVM = "apple-vf-microvm"
)

type AppleVFMicroVMRuntimeBackend struct {
	HelperBinary    string
	KernelPath      string
	StateDir        string
	MemoryMiB       int64
	CPUCount        int64
	EnforcementMode string
	Images          *MicroVMImageStore
}

func NewAppleVFMicroVMRuntimeBackend(home string, cfg map[string]string) *AppleVFMicroVMRuntimeBackend {
	stateDir := strings.TrimSpace(cfg["state_dir"])
	if stateDir == "" {
		if strings.TrimSpace(home) != "" {
			stateDir = filepath.Join(home, "runtime", "apple-vf-microvm")
		} else {
			stateDir = filepath.Join(os.TempDir(), "agency-apple-vf-microvm")
		}
	}
	enforcementMode, err := parseFirecrackerEnforcementMode(cfg["enforcement_mode"])
	if err != nil {
		enforcementMode = FirecrackerEnforcementModeHostProcess
	}
	backend := &AppleVFMicroVMRuntimeBackend{
		HelperBinary:    strings.TrimSpace(cfg["helper_binary"]),
		KernelPath:      strings.TrimSpace(cfg["kernel_path"]),
		StateDir:        stateDir,
		MemoryMiB:       parseInt64Config(cfg["memory_mib"], defaultFirecrackerMemoryMiB),
		CPUCount:        parseInt64Config(cfg["cpu_count"], 2),
		EnforcementMode: enforcementMode,
	}
	backend.Images = &MicroVMImageStore{
		StateDir:          stateDir,
		PodmanPath:        strings.TrimSpace(cfg["podman_path"]),
		Mke2fsPath:        strings.TrimSpace(cfg["mke2fs_path"]),
		SizeMiB:           parseInt64Config(cfg["rootfs_size_mib"], defaultFirecrackerRootFSMiB),
		VsockBridgeBinary: strings.TrimSpace(cfg["vsock_bridge_binary_path"]),
		OverlayBaseDir:    strings.TrimSpace(home),
	}
	return backend
}

func (b *AppleVFMicroVMRuntimeBackend) Name() string {
	return BackendAppleVFMicroVM
}

func (b *AppleVFMicroVMRuntimeBackend) Ensure(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	_ = ctx
	_ = spec
	return fmt.Errorf("apple-vf-microvm backend: Ensure not implemented")
}

func (b *AppleVFMicroVMRuntimeBackend) Stop(ctx context.Context, runtimeID string) error {
	_ = ctx
	_ = runtimeID
	return fmt.Errorf("apple-vf-microvm backend: Stop not implemented")
}

func (b *AppleVFMicroVMRuntimeBackend) Inspect(ctx context.Context, runtimeID string) (runtimecontract.BackendStatus, error) {
	_ = ctx
	_ = runtimeID
	return runtimecontract.BackendStatus{}, fmt.Errorf("apple-vf-microvm backend: Inspect not implemented")
}

func (b *AppleVFMicroVMRuntimeBackend) Validate(ctx context.Context, runtimeID string) error {
	_ = ctx
	_ = runtimeID
	return fmt.Errorf("apple-vf-microvm backend: Validate not implemented")
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
	return b.imageStore().PrepareTaskRootFS(ctx, spec)
}

func (b *AppleVFMicroVMRuntimeBackend) imageStore() *MicroVMImageStore {
	if b.Images != nil {
		return b.Images
	}
	stateDir := strings.TrimSpace(b.StateDir)
	if stateDir == "" {
		stateDir = filepath.Join(os.TempDir(), "agency-apple-vf-microvm")
	}
	b.Images = &MicroVMImageStore{
		StateDir: stateDir,
		SizeMiB:  defaultFirecrackerRootFSMiB,
	}
	return b.Images
}
