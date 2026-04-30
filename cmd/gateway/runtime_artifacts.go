package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
)

func verifyMicroVMRuntimeArtifacts(backend string, cfg map[string]string) error {
	switch backend {
	case hostruntimebackend.BackendAppleVFMicroVM:
		return verifyAppleVFRuntimeArtifacts(cfg)
	case hostruntimebackend.BackendFirecracker:
		return verifyFirecrackerRuntimeArtifacts(cfg)
	default:
		return nil
	}
}

func verifyAppleVFRuntimeArtifacts(cfg map[string]string) error {
	var missing []string
	requireExecutable(&missing, "Apple VF helper", cfg["helper_binary"], "run make apple-vf-helpers or set AGENCY_APPLE_VF_HELPER_BIN/hub.deployment_backend_config.helper_binary")
	requireReadable(&missing, "Apple VF kernel", cfg["kernel_path"], "run scripts/readiness/apple-vf-artifacts.sh or set AGENCY_APPLE_VF_KERNEL/hub.deployment_backend_config.kernel_path")
	requireExecutable(&missing, "mke2fs", cfg["mke2fs_path"], "install e2fsprogs with Homebrew or set AGENCY_MKE2FS/hub.deployment_backend_config.mke2fs_path")
	requireExecutable(&missing, "Apple VF host enforcer", cfg["enforcer_binary_path"], "run make apple-vf-helpers or set AGENCY_APPLE_VF_ENFORCER_BIN/hub.deployment_backend_config.enforcer_binary_path")
	requireExecutable(&missing, "Apple VF guest vsock bridge", cfg["vsock_bridge_binary_path"], "run make apple-vf-helpers or set AGENCY_APPLE_VF_VSOCK_BRIDGE_BIN/hub.deployment_backend_config.vsock_bridge_binary_path")
	return artifactError(hostruntimebackend.BackendAppleVFMicroVM, missing)
}

func verifyFirecrackerRuntimeArtifacts(cfg map[string]string) error {
	var missing []string
	requireExecutable(&missing, "Firecracker binary", cfg["binary_path"], "run scripts/readiness/firecracker-artifacts.sh or set AGENCY_FIRECRACKER_BIN/hub.deployment_backend_config.binary_path")
	requireReadable(&missing, "Firecracker kernel", cfg["kernel_path"], "run scripts/readiness/firecracker-kernel-artifacts.sh or set AGENCY_FIRECRACKER_KERNEL/hub.deployment_backend_config.kernel_path")
	requireExecutable(&missing, "mke2fs", cfg["mke2fs_path"], "install e2fsprogs or set AGENCY_MKE2FS/hub.deployment_backend_config.mke2fs_path")
	if strings.TrimSpace(cfg["enforcement_mode"]) != hostruntimebackend.FirecrackerEnforcementModeMicroVM {
		requireExecutable(&missing, "Firecracker host enforcer", cfg["enforcer_binary_path"], "run make firecracker-helpers or set AGENCY_FIRECRACKER_ENFORCER_BIN/hub.deployment_backend_config.enforcer_binary_path")
	}
	requireExecutable(&missing, "Firecracker guest vsock bridge", cfg["vsock_bridge_binary_path"], "run make firecracker-helpers or set AGENCY_FIRECRACKER_VSOCK_BRIDGE_BIN/hub.deployment_backend_config.vsock_bridge_binary_path")
	return artifactError(hostruntimebackend.BackendFirecracker, missing)
}

func requireReadable(missing *[]string, label, path, fix string) {
	resolved, err := resolveArtifactPath(path)
	if err != nil {
		*missing = append(*missing, fmt.Sprintf("%s: %v; %s", label, err, fix))
		return
	}
	info, err := os.Stat(resolved)
	if err != nil {
		*missing = append(*missing, fmt.Sprintf("%s: %s is not readable: %v; %s", label, resolved, err, fix))
		return
	}
	if info.IsDir() {
		*missing = append(*missing, fmt.Sprintf("%s: %s is a directory; %s", label, resolved, fix))
	}
}

func requireExecutable(missing *[]string, label, path, fix string) {
	resolved, err := resolveArtifactPath(path)
	if err != nil {
		*missing = append(*missing, fmt.Sprintf("%s: %v; %s", label, err, fix))
		return
	}
	info, err := os.Stat(resolved)
	if err != nil {
		*missing = append(*missing, fmt.Sprintf("%s: %s is not present: %v; %s", label, resolved, err, fix))
		return
	}
	if info.IsDir() {
		*missing = append(*missing, fmt.Sprintf("%s: %s is a directory; %s", label, resolved, fix))
		return
	}
	if info.Mode()&0111 == 0 {
		*missing = append(*missing, fmt.Sprintf("%s: %s is not executable; run chmod +x %s or %s", label, resolved, resolved, fix))
	}
}

func resolveArtifactPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path is not configured")
	}
	if strings.ContainsRune(path, os.PathSeparator) {
		return filepath.Clean(path), nil
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return "", fmt.Errorf("%q was not found on PATH: %w", path, err)
	}
	return resolved, nil
}

func artifactError(backend string, missing []string) error {
	if len(missing) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s runtime artifacts are not ready. Setup is fail-closed so Agency does not create an agent runtime with missing mediation or guest transport artifacts.", backend)
	for _, item := range missing {
		fmt.Fprintf(&b, "\n  - %s", item)
	}
	return errors.New(b.String())
}
