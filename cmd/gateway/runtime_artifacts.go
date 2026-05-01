package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geoffbelknap/agency/internal/config"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/runtimeprovision"
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

func ensureMicroVMRuntimeArtifacts(ctx context.Context, backend string, cfg map[string]string, logf func(string, ...any)) error {
	err := verifyMicroVMRuntimeArtifacts(backend, cfg)
	if err == nil {
		return nil
	}
	if backend != hostruntimebackend.BackendFirecracker {
		return err
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	logf("Firecracker runtime artifacts are missing; provisioning pinned artifacts")
	if provisionErr := runtimeprovision.ProvisionFirecracker(ctx, runtimeprovision.FirecrackerOptions{
		AgencyVersion:        version,
		Home:                 configHome(),
		BinaryPath:           cfg["binary_path"],
		KernelPath:           cfg["kernel_path"],
		FirecrackerBaseURL:   strings.TrimSpace(os.Getenv("AGENCY_FIRECRACKER_RELEASE_BASE_URL")),
		KernelReleaseBaseURL: strings.TrimSpace(os.Getenv("AGENCY_FIRECRACKER_KERNEL_RELEASE_BASE_URL")),
		Logf:                 logf,
	}); provisionErr != nil {
		return fmt.Errorf("%w\nAutomatic Firecracker artifact provisioning failed: %v", err, provisionErr)
	}
	return verifyMicroVMRuntimeArtifacts(backend, cfg)
}

func configHome() string {
	return config.Load().Home
}

func verifyAppleVFRuntimeArtifacts(cfg map[string]string) error {
	var missing []string
	requireExecutable(&missing, "Apple VF helper", cfg["helper_binary"], "run make apple-vf-helpers or set AGENCY_APPLE_VF_HELPER_BIN/hub.deployment_backend_config.helper_binary")
	requireReadable(&missing, "Apple VF kernel", cfg["kernel_path"], "run scripts/readiness/apple-vf-artifacts.sh or set AGENCY_APPLE_VF_KERNEL/hub.deployment_backend_config.kernel_path")
	requireExecutable(&missing, "mke2fs", cfg["mke2fs_path"], "install e2fsprogs with Homebrew or set AGENCY_MKE2FS/hub.deployment_backend_config.mke2fs_path")
	requireExecutable(&missing, "Apple VF host enforcer", cfg["enforcer_binary_path"], "run make apple-vf-helpers or set AGENCY_APPLE_VF_ENFORCER_BIN/hub.deployment_backend_config.enforcer_binary_path")
	requireExecutable(&missing, "Apple VF guest vsock bridge", cfg["vsock_bridge_binary_path"], "run make apple-vf-helpers or set AGENCY_APPLE_VF_VSOCK_BRIDGE_BIN/hub.deployment_backend_config.vsock_bridge_binary_path")
	requireVersionedArtifactRef(&missing, "Apple VF rootfs OCI artifact", firstNonEmptyConfigValue(cfg, "rootfs_oci_ref", "body_oci_ref"), "set hub.deployment_backend_config.rootfs_oci_ref to a versioned OCI artifact reference")
	return artifactError(hostruntimebackend.BackendAppleVFMicroVM, missing)
}

func verifyFirecrackerRuntimeArtifacts(cfg map[string]string) error {
	var missing []string
	requireExecutable(&missing, "Firecracker binary", cfg["binary_path"], "run scripts/readiness/firecracker-artifacts.sh or set AGENCY_FIRECRACKER_BIN/hub.deployment_backend_config.binary_path")
	requireELFKernel(&missing, "Firecracker kernel", cfg["kernel_path"], "run agency runtime provision firecracker or set AGENCY_FIRECRACKER_KERNEL/hub.deployment_backend_config.kernel_path to a verified Agency vmlinux")
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

func requireELFKernel(missing *[]string, label, path, fix string) {
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
		return
	}
	f, err := os.Open(resolved)
	if err != nil {
		*missing = append(*missing, fmt.Sprintf("%s: %s is not readable: %v; %s", label, resolved, err, fix))
		return
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		*missing = append(*missing, fmt.Sprintf("%s: %s could not be read as an uncompressed ELF vmlinux: %v; %s", label, resolved, err, fix))
		return
	}
	if string(magic[:]) != "\x7fELF" {
		*missing = append(*missing, fmt.Sprintf("%s: %s is not an uncompressed ELF vmlinux artifact; %s", label, resolved, fix))
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

func requireVersionedArtifactRef(missing *[]string, label, value, fix string) {
	ref := strings.TrimSpace(value)
	if ref == "" {
		*missing = append(*missing, fmt.Sprintf("%s: value is not configured; %s", label, fix))
		return
	}
	if strings.HasSuffix(ref, ":latest") {
		*missing = append(*missing, fmt.Sprintf("%s: %s uses mutable :latest; %s", label, ref, fix))
	}
}

func firstNonEmptyConfigValue(cfg map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(cfg[key]); value != "" {
			return value
		}
	}
	return ""
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
