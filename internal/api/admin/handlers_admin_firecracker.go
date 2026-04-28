package admin

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/geoffbelknap/agency/internal/config"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	agencysecurity "github.com/geoffbelknap/agency/internal/security"
)

var (
	firecrackerOpenReadWrite = func(path string) error {
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		return f.Close()
	}
	firecrackerStat     = os.Stat
	firecrackerLookPath = exec.LookPath
)

func (h *handler) adminDoctorFirecracker(ctx context.Context) doctorReport {
	report := h.adminDoctorRuntimeContract(ctx)
	appendFirecrackerDoctorChecks(&report, h.deps.Config)
	report.RuntimeChecks, report.BackendChecks = splitDoctorChecks(report.Checks, report.Backend)
	return report
}

func appendFirecrackerDoctorChecks(report *doctorReport, cfg *config.Config) {
	add := func(check doctorCheckResult) {
		if check.Status != agencysecurity.FindingPass {
			report.AllPassed = false
		}
		report.Checks = append(report.Checks, check)
	}

	add(firecrackerDeviceCheck("firecracker_kvm_device", "/dev/kvm", "KVM device is readable and writable", "run 'sudo usermod -aG kvm <user>' or 'sudo setfacl -m u:<user>:rw /dev/kvm'"))
	add(firecrackerDeviceCheck("firecracker_vsock_device", "/dev/vhost-vsock", "vhost-vsock device is readable and writable", "run 'sudo modprobe vhost_vsock' and ensure /dev/vhost-vsock is readable and writable by the Agency user"))
	add(firecrackerKVMModuleCheck())
	add(firecrackerBinaryCheck(firecrackerConfiguredPath(cfg, "binary_path", "AGENCY_FIRECRACKER_BIN", "firecracker")))
	add(firecrackerKernelCheck(firecrackerConfiguredPath(cfg, "kernel_path", "AGENCY_FIRECRACKER_KERNEL", "")))
	if firecrackerEnforcementMode(cfg) != hostruntimebackend.FirecrackerEnforcementModeMicroVM {
		add(firecrackerExecutableCheck("firecracker_enforcer_binary", firecrackerConfiguredPath(cfg, "enforcer_binary_path", "AGENCY_FIRECRACKER_ENFORCER_BIN", ""), "Firecracker host enforcer binary is present at ", "set hub.deployment_backend_config.enforcer_binary_path or run 'make firecracker-helpers'"))
	}
	add(firecrackerExecutableCheck("firecracker_vsock_bridge_binary", firecrackerConfiguredPath(cfg, "vsock_bridge_binary_path", "AGENCY_FIRECRACKER_VSOCK_BRIDGE_BIN", ""), "Firecracker guest vsock bridge binary is present at ", "set hub.deployment_backend_config.vsock_bridge_binary_path or run 'make firecracker-helpers'"))
}

func firecrackerConfiguredPath(cfg *config.Config, key, envName, fallback string) string {
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

func firecrackerDeviceCheck(name, path, passDetail, fix string) doctorCheckResult {
	if err := firecrackerOpenReadWrite(path); err != nil {
		return firecrackerBackendCheck(name, agencysecurity.FindingFail, fmt.Sprintf("%s is not readable and writable: %v", path, err), fix)
	}
	return firecrackerBackendCheck(name, agencysecurity.FindingPass, passDetail, "")
}

func firecrackerKVMModuleCheck() doctorCheckResult {
	if _, err := firecrackerStat("/sys/module/kvm"); err != nil {
		return firecrackerBackendCheck("firecracker_kvm_module", agencysecurity.FindingFail, "KVM kernel module is not loaded: "+err.Error(), "run 'sudo modprobe kvm' and enable hardware virtualization in firmware if needed")
	}
	return firecrackerBackendCheck("firecracker_kvm_module", agencysecurity.FindingPass, "KVM kernel module is loaded", "")
}

func firecrackerBinaryCheck(path string) doctorCheckResult {
	return firecrackerExecutableCheck("firecracker_binary", path, "Firecracker binary is present at ", "set hub.deployment_backend_config.binary_path or install firecracker on PATH")
}

func firecrackerExecutableCheck(name, path, passPrefix, missingFix string) doctorCheckResult {
	if strings.TrimSpace(path) == "" {
		return firecrackerBackendCheck(name, agencysecurity.FindingFail, name+" path is not configured", missingFix)
	}
	resolved := path
	if !strings.ContainsRune(path, os.PathSeparator) {
		found, err := firecrackerLookPath(path)
		if err != nil {
			return firecrackerBackendCheck(name, agencysecurity.FindingFail, name+" was not found on PATH: "+err.Error(), missingFix)
		}
		resolved = found
	}
	info, err := firecrackerStat(resolved)
	if err != nil {
		return firecrackerBackendCheck(name, agencysecurity.FindingFail, name+" is not present at "+resolved+": "+err.Error(), missingFix)
	}
	if info.IsDir() {
		return firecrackerBackendCheck(name, agencysecurity.FindingFail, name+" path is a directory: "+resolved, missingFix)
	}
	if info.Mode()&0o111 == 0 {
		return firecrackerBackendCheck(name, agencysecurity.FindingFail, name+" is not executable: "+resolved, "run 'chmod +x "+resolved+"' or "+missingFix)
	}
	return firecrackerBackendCheck(name, agencysecurity.FindingPass, passPrefix+resolved, "")
}

func firecrackerKernelCheck(path string) doctorCheckResult {
	if strings.TrimSpace(path) == "" {
		return firecrackerBackendCheck("firecracker_kernel", agencysecurity.FindingFail, "Firecracker guest kernel path is not configured", "set hub.deployment_backend_config.kernel_path to a Firecracker-compatible vmlinux kernel")
	}
	f, err := os.Open(path)
	if err != nil {
		return firecrackerBackendCheck("firecracker_kernel", agencysecurity.FindingFail, "Firecracker guest kernel is not readable at "+path+": "+err.Error(), "set hub.deployment_backend_config.kernel_path to a readable Firecracker-compatible vmlinux kernel")
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return firecrackerBackendCheck("firecracker_kernel", agencysecurity.FindingFail, "Firecracker guest kernel could not be parsed: "+err.Error(), "set hub.deployment_backend_config.kernel_path to a Firecracker-compatible vmlinux kernel")
	}
	if magic != [4]byte{0x7f, 'E', 'L', 'F'} {
		return firecrackerBackendCheck("firecracker_kernel", agencysecurity.FindingFail, "Firecracker guest kernel is not an uncompressed ELF vmlinux image", "set hub.deployment_backend_config.kernel_path to a Firecracker-compatible vmlinux kernel")
	}
	return firecrackerBackendCheck("firecracker_kernel", agencysecurity.FindingPass, "Firecracker guest kernel is readable and parseable", "")
}

func firecrackerBackendCheck(name string, status agencysecurity.FindingStatus, detail, fix string) doctorCheckResult {
	return doctorCheckResult{
		Name:    name,
		Scope:   "backend",
		Backend: hostruntimebackend.BackendFirecracker,
		Status:  status,
		Detail:  detail,
		Fix:     fix,
	}
}

func firecrackerEnforcementMode(cfg *config.Config) string {
	if cfg != nil {
		if mode := strings.TrimSpace(cfg.Hub.DeploymentBackendConfig["enforcement_mode"]); mode != "" {
			return mode
		}
	}
	return hostruntimebackend.FirecrackerEnforcementModeHostProcess
}
