package admin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"

	"github.com/geoffbelknap/agency/internal/config"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	agencysecurity "github.com/geoffbelknap/agency/internal/security"
)

var (
	appleVFGOOS       = func() string { return goruntime.GOOS }
	appleVFGOARCH     = func() string { return goruntime.GOARCH }
	appleVFStat       = os.Stat
	appleVFLookPath   = exec.LookPath
	appleVFHealthFunc = hostruntimebackend.AppleVFHelperHealthStatus
)

func (h *handler) adminDoctorAppleVF(ctx context.Context) doctorReport {
	report := h.adminDoctorRuntimeContract(ctx)
	appendAppleVFDoctorChecks(ctx, &report, h.deps.Config)
	h.appendMicroVMHostInfraDoctorChecks(ctx, &report)
	report.RuntimeChecks, report.BackendChecks = splitDoctorChecks(report.Checks, report.Backend)
	return report
}

func appendAppleVFDoctorChecks(ctx context.Context, report *doctorReport, cfg *config.Config) {
	add := func(check doctorCheckResult) {
		if check.Status != agencysecurity.FindingPass {
			report.AllPassed = false
		}
		report.Checks = append(report.Checks, check)
	}

	add(appleVFHostOSCheck())
	add(appleVFArchitectureCheck())

	helperPath := appleVFConfiguredPath(cfg, "helper_binary", "AGENCY_APPLE_VF_HELPER_BIN", "")
	helperCheck := appleVFExecutableCheck("apple_vf_helper_binary", helperPath, "Apple VF helper binary is present at ", "set hub.deployment_backend_config.helper_binary or AGENCY_APPLE_VF_HELPER_BIN to agency-apple-vf-helper")
	add(helperCheck)
	if helperCheck.Status == agencysecurity.FindingPass {
		add(appleVFHelperHealthCheck(ctx, helperPath))
	} else {
		add(appleVFBackendCheck("apple_vf_helper_health", agencysecurity.FindingFail, "Apple VF helper health was not run because the helper binary is unavailable", "build tools/apple-vf-helper and configure hub.deployment_backend_config.helper_binary"))
	}

	add(appleVFStateDirCheck(appleVFConfiguredStateDir(cfg)))
	add(appleVFKernelCheck(appleVFConfiguredKernelPath(cfg)))
	add(appleVFExecutableCheck("apple_vf_mke2fs", appleVFConfiguredMke2fsPath(cfg), "Apple VF mke2fs is present at ", "install e2fsprogs with Homebrew or set hub.deployment_backend_config.mke2fs_path/AGENCY_MKE2FS"))
	add(appleVFExecutableCheck("apple_vf_enforcer_binary", appleVFConfiguredPath(cfg, "enforcer_binary_path", "AGENCY_APPLE_VF_ENFORCER_BIN", ""), "Apple VF host enforcer binary is present at ", "build images/enforcer with 'go build -o /tmp/agency-enforcer-host ./images/enforcer' and set hub.deployment_backend_config.enforcer_binary_path/AGENCY_APPLE_VF_ENFORCER_BIN"))
	add(appleVFExecutableCheck("apple_vf_vsock_bridge_binary", appleVFConfiguredPath(cfg, "vsock_bridge_binary_path", "AGENCY_APPLE_VF_VSOCK_BRIDGE_BIN", ""), "Apple VF guest vsock bridge binary is present at ", "build the Linux ARM64 guest bridge with 'GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /tmp/agency-vsock-http-bridge-linux-arm64 ./cmd/agency-vsock-http-bridge' and set hub.deployment_backend_config.vsock_bridge_binary_path/AGENCY_APPLE_VF_VSOCK_BRIDGE_BIN"))
}

func appleVFConfiguredPath(cfg *config.Config, key, envName, fallback string) string {
	if cfg != nil {
		if value := strings.TrimSpace(cfg.Hub.DeploymentBackendConfig[key]); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
		return value
	}
	return fallback
}

func appleVFConfiguredStateDir(cfg *config.Config) string {
	if cfg != nil {
		if value := strings.TrimSpace(cfg.Hub.DeploymentBackendConfig["state_dir"]); value != "" {
			return value
		}
		return hostruntimebackend.DefaultAppleVFStateDir(cfg.Home)
	}
	return hostruntimebackend.DefaultAppleVFStateDir("")
}

func appleVFConfiguredKernelPath(cfg *config.Config) string {
	if cfg != nil {
		if value := strings.TrimSpace(cfg.Hub.DeploymentBackendConfig["kernel_path"]); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(os.Getenv("AGENCY_APPLE_VF_KERNEL")); value != "" {
		return value
	}
	if cfg != nil {
		return hostruntimebackend.DefaultAppleVFKernelPath(cfg.Home)
	}
	return hostruntimebackend.DefaultAppleVFKernelPath("")
}

func appleVFConfiguredMke2fsPath(cfg *config.Config) string {
	if value := appleVFConfiguredPath(cfg, "mke2fs_path", "AGENCY_MKE2FS", ""); strings.TrimSpace(value) != "" {
		return value
	}
	const homebrewMke2fs = "/opt/homebrew/opt/e2fsprogs/sbin/mke2fs"
	if info, err := appleVFStat(homebrewMke2fs); err == nil && !info.IsDir() {
		return homebrewMke2fs
	}
	return "mke2fs"
}

func appleVFHostOSCheck() doctorCheckResult {
	if appleVFGOOS() != "darwin" {
		return appleVFBackendCheck("apple_vf_host_os", agencysecurity.FindingFail, "apple-vf-microvm requires macOS; host OS is "+appleVFGOOS(), "run apple-vf-microvm on macOS Apple silicon or select firecracker on Linux")
	}
	return appleVFBackendCheck("apple_vf_host_os", agencysecurity.FindingPass, "Host OS is macOS", "")
}

func appleVFArchitectureCheck() doctorCheckResult {
	switch appleVFGOARCH() {
	case "arm64":
		return appleVFBackendCheck("apple_vf_architecture", agencysecurity.FindingPass, "Host architecture is Apple silicon", "")
	default:
		return appleVFBackendCheck("apple_vf_architecture", agencysecurity.FindingFail, "apple-vf-microvm requires Apple silicon; host architecture is "+appleVFGOARCH(), "run apple-vf-microvm on Apple silicon or select a supported backend for this host")
	}
}

func appleVFExecutableCheck(name, path, passPrefix, missingFix string) doctorCheckResult {
	if strings.TrimSpace(path) == "" {
		return appleVFBackendCheck(name, agencysecurity.FindingFail, name+" path is not configured", missingFix)
	}
	resolved := path
	if !strings.ContainsRune(path, os.PathSeparator) {
		found, err := appleVFLookPath(path)
		if err != nil {
			return appleVFBackendCheck(name, agencysecurity.FindingFail, name+" was not found on PATH: "+err.Error(), missingFix)
		}
		resolved = found
	}
	info, err := appleVFStat(resolved)
	if err != nil {
		return appleVFBackendCheck(name, agencysecurity.FindingFail, name+" is not present at "+resolved+": "+err.Error(), missingFix)
	}
	if info.IsDir() {
		return appleVFBackendCheck(name, agencysecurity.FindingFail, name+" path is a directory: "+resolved, missingFix)
	}
	if info.Mode()&0o111 == 0 {
		return appleVFBackendCheck(name, agencysecurity.FindingFail, name+" is not executable: "+resolved, "run 'chmod +x "+resolved+"' or "+missingFix)
	}
	return appleVFBackendCheck(name, agencysecurity.FindingPass, passPrefix+resolved, "")
}

func appleVFHelperHealthCheck(ctx context.Context, helperPath string) doctorCheckResult {
	health, err := appleVFHealthFunc(ctx, helperPath)
	if err != nil {
		return appleVFBackendCheck("apple_vf_helper_health", agencysecurity.FindingFail, "Apple VF helper health failed: "+err.Error(), "run 'scripts/readiness/apple-vf-helper-build.sh' and configure a helper with Virtualization.framework support")
	}
	return appleVFBackendCheck("apple_vf_helper_health", agencysecurity.FindingPass, fmt.Sprintf("Apple VF helper health succeeded: version=%s darwin=%s arch=%s virtualization=%t", health.Version, health.Darwin, health.Arch, health.VirtualizationAvailable), "")
}

func appleVFStateDirCheck(path string) doctorCheckResult {
	if strings.TrimSpace(path) == "" {
		return appleVFBackendCheck("apple_vf_state_dir", agencysecurity.FindingFail, "Apple VF state directory is not configured", "set hub.deployment_backend_config.state_dir or AGENCY_HOME")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return appleVFBackendCheck("apple_vf_state_dir", agencysecurity.FindingFail, "Apple VF state directory cannot be created at "+path+": "+err.Error(), "set hub.deployment_backend_config.state_dir to a writable directory")
	}
	probe, err := os.CreateTemp(path, ".agency-apple-vf-doctor-*")
	if err != nil {
		return appleVFBackendCheck("apple_vf_state_dir", agencysecurity.FindingFail, "Apple VF state directory is not writable at "+path+": "+err.Error(), "set hub.deployment_backend_config.state_dir to a writable directory")
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return appleVFBackendCheck("apple_vf_state_dir", agencysecurity.FindingFail, "Apple VF state directory probe could not be closed: "+err.Error(), "set hub.deployment_backend_config.state_dir to a writable directory")
	}
	_ = os.Remove(name)
	return appleVFBackendCheck("apple_vf_state_dir", agencysecurity.FindingPass, "Apple VF state directory is writable at "+path, "")
}

func appleVFKernelCheck(path string) doctorCheckResult {
	if strings.TrimSpace(path) == "" {
		return appleVFBackendCheck("apple_vf_kernel", agencysecurity.FindingFail, "Apple VF guest kernel path is not configured", "place the Linux-built Image at $AGENCY_HOME/runtime/apple-vf-microvm/artifacts/Image or set hub.deployment_backend_config.kernel_path/AGENCY_APPLE_VF_KERNEL")
	}
	info, err := appleVFStat(path)
	if err != nil {
		return appleVFBackendCheck("apple_vf_kernel", agencysecurity.FindingFail, "Apple VF guest kernel is not readable at "+path+": "+err.Error(), "run 'scripts/readiness/apple-vf-artifacts.sh --verify-existing' after placing the Linux-built Image there, or set hub.deployment_backend_config.kernel_path/AGENCY_APPLE_VF_KERNEL")
	}
	if info.IsDir() {
		return appleVFBackendCheck("apple_vf_kernel", agencysecurity.FindingFail, "Apple VF guest kernel path is a directory: "+path, "replace it with the Linux-built Image or set hub.deployment_backend_config.kernel_path/AGENCY_APPLE_VF_KERNEL")
	}
	return appleVFBackendCheck("apple_vf_kernel", agencysecurity.FindingPass, "Apple VF guest kernel is readable at "+path, "")
}

func appleVFBackendCheck(name string, status agencysecurity.FindingStatus, detail, fix string) doctorCheckResult {
	return doctorCheckResult{
		Name:    name,
		Scope:   "backend",
		Backend: hostruntimebackend.BackendAppleVFMicroVM,
		Status:  status,
		Detail:  detail,
		Fix:     fix,
	}
}
