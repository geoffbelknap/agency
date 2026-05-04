package runtimeconfig

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/runtimeprovision"
)

type ArtifactOptions struct {
	Home      string
	SourceDir string
	Version   string
}

func WithMicroVMArtifactConfig(backend string, cfg map[string]string, opts ArtifactOptions) map[string]string {
	return WithMicroagentArtifactConfig(backend, WithFirecrackerArtifactConfig(backend, WithAppleVFArtifactConfig(backend, cfg, opts), opts), opts)
}

func WithMicroagentArtifactConfig(backend string, cfg map[string]string, opts ArtifactOptions) map[string]string {
	if backend != hostruntimebackend.BackendMicroagent {
		return cfg
	}
	defaults := map[string]string{
		"binary_path":          "microagent",
		"state_dir":            filepath.Join(opts.Home, "runtime", "microagent"),
		"entrypoint":           "/app/entrypoint.sh",
		"enforcer_binary_path": filepath.Join(opts.SourceDir, "bin", "agency-enforcer-host"),
		"rootfs_oci_ref":       defaultMicroagentRootFSOCIRef(opts.Version),
		"mke2fs_path":          defaultMke2fsPath(),
	}
	envPaths := map[string]string{
		"binary_path":          strings.TrimSpace(os.Getenv("AGENCY_MICROAGENT_BIN")),
		"state_dir":            strings.TrimSpace(os.Getenv("AGENCY_MICROAGENT_STATE_DIR")),
		"entrypoint":           strings.TrimSpace(os.Getenv("AGENCY_MICROAGENT_ENTRYPOINT")),
		"enforcer_binary_path": strings.TrimSpace(os.Getenv("AGENCY_MICROAGENT_ENFORCER_BIN")),
		"rootfs_oci_ref":       strings.TrimSpace(os.Getenv("AGENCY_MICROAGENT_ROOTFS_OCI_REF")),
		"mke2fs_path":          strings.TrimSpace(os.Getenv("AGENCY_MKE2FS")),
	}
	return applyDefaults(cfg, defaults, envPaths)
}

func WithAppleVFArtifactConfig(backend string, cfg map[string]string, opts ArtifactOptions) map[string]string {
	if backend != hostruntimebackend.BackendAppleVFMicroVM {
		return cfg
	}
	defaults := map[string]string{
		"kernel_path":              hostruntimebackend.DefaultAppleVFKernelPath(opts.Home),
		"helper_binary":            filepath.Join(opts.SourceDir, "bin", "agency-apple-vf-helper"),
		"enforcer_binary_path":     filepath.Join(opts.SourceDir, "bin", "agency-enforcer-host"),
		"vsock_bridge_binary_path": filepath.Join(opts.SourceDir, "bin", "agency-vsock-http-bridge-linux-arm64"),
		"mke2fs_path":              defaultMke2fsPath(),
	}
	envPaths := map[string]string{
		"kernel_path":              strings.TrimSpace(os.Getenv("AGENCY_APPLE_VF_KERNEL")),
		"helper_binary":            strings.TrimSpace(os.Getenv("AGENCY_APPLE_VF_HELPER_BIN")),
		"enforcer_binary_path":     strings.TrimSpace(os.Getenv("AGENCY_APPLE_VF_ENFORCER_BIN")),
		"vsock_bridge_binary_path": strings.TrimSpace(os.Getenv("AGENCY_APPLE_VF_VSOCK_BRIDGE_BIN")),
		"rootfs_oci_ref":           strings.TrimSpace(os.Getenv("AGENCY_APPLE_VF_ROOTFS_OCI_REF")),
		"mke2fs_path":              strings.TrimSpace(os.Getenv("AGENCY_MKE2FS")),
	}
	return applyDefaults(cfg, defaults, envPaths)
}

func WithFirecrackerArtifactConfig(backend string, cfg map[string]string, opts ArtifactOptions) map[string]string {
	if backend != hostruntimebackend.BackendFirecracker {
		return cfg
	}
	artifactDir := filepath.Join(opts.Home, "runtime", "firecracker", "artifacts")
	defaultArch := runtime.GOARCH
	switch defaultArch {
	case "amd64":
		defaultArch = "x86_64"
	case "arm64":
		defaultArch = "aarch64"
	}
	defaultVersion := "v1.12.1"
	defaultKernelPath, err := runtimeprovision.DefaultFirecrackerKernelPath(opts.Home, defaultArch)
	if err != nil {
		defaultKernelPath = filepath.Join(artifactDir, "vmlinux")
	}
	defaults := map[string]string{
		"binary_path":              filepath.Join(artifactDir, defaultVersion, "firecracker-"+defaultVersion+"-"+defaultArch),
		"kernel_path":              defaultKernelPath,
		"enforcer_binary_path":     filepath.Join(opts.SourceDir, "bin", "enforcer"),
		"vsock_bridge_binary_path": filepath.Join(opts.SourceDir, "bin", "agency-vsock-http-bridge"),
		"mke2fs_path":              defaultMke2fsPath(),
	}
	envPaths := map[string]string{
		"binary_path":              strings.TrimSpace(os.Getenv("AGENCY_FIRECRACKER_BIN")),
		"kernel_path":              strings.TrimSpace(os.Getenv("AGENCY_FIRECRACKER_KERNEL")),
		"enforcer_binary_path":     strings.TrimSpace(os.Getenv("AGENCY_FIRECRACKER_ENFORCER_BIN")),
		"vsock_bridge_binary_path": strings.TrimSpace(os.Getenv("AGENCY_FIRECRACKER_VSOCK_BRIDGE_BIN")),
		"rootfs_oci_ref":           strings.TrimSpace(os.Getenv("AGENCY_FIRECRACKER_ROOTFS_OCI_REF")),
		"mke2fs_path":              strings.TrimSpace(os.Getenv("AGENCY_MKE2FS")),
	}
	return applyDefaults(cfg, defaults, envPaths)
}

func applyDefaults(cfg, defaults, envPaths map[string]string) map[string]string {
	out := make(map[string]string, len(cfg)+len(defaults)+len(envPaths))
	for k, v := range cfg {
		out[k] = v
	}
	for key, value := range envPaths {
		if strings.TrimSpace(out[key]) == "" && strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	for key, value := range defaults {
		if strings.TrimSpace(out[key]) == "" && strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	return out
}

func defaultMicroagentRootFSOCIRef(version string) string {
	v := strings.TrimSpace(version)
	if v == "" || v == "dev" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return "ghcr.io/geoffbelknap/agency-runtime-body:" + v
}

func defaultMke2fsPath() string {
	const homebrewMke2fs = "/opt/homebrew/opt/e2fsprogs/sbin/mke2fs"
	if info, err := os.Stat(homebrewMke2fs); err == nil && !info.IsDir() {
		return homebrewMke2fs
	}
	return "mke2fs"
}
