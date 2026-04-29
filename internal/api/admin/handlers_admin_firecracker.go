package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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
	h.appendMicroVMHostInfraDoctorChecks(ctx, &report)
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

type hostInfraDoctorMetadata struct {
	Component string   `json:"component"`
	Service   string   `json:"service"`
	PID       int      `json:"pid"`
	PIDFile   string   `json:"pid_file"`
	Command   []string `json:"command"`
	LogFile   string   `json:"log_file,omitempty"`
	HealthURL string   `json:"health_url,omitempty"`
	StartedAt string   `json:"started_at"`
}

func (h *handler) appendMicroVMHostInfraDoctorChecks(ctx context.Context, report *doctorReport) {
	if h == nil || h.deps.Config == nil || h.deps.Infra == nil {
		return
	}
	add := func(check doctorCheckResult) {
		if check.Status != agencysecurity.FindingPass {
			report.AllPassed = false
		}
		report.Checks = append(report.Checks, check)
	}
	for _, component := range []string{"egress", "comms", "knowledge", "web"} {
		add(h.microVMHostInfraComponentCheck(ctx, component))
	}
	add(h.microVMNoLegacyInfraContainersCheck(ctx))
}

func (h *handler) microVMHostInfraComponentCheck(ctx context.Context, component string) doctorCheckResult {
	metaPath := filepath.Join(h.deps.Config.Home, "run", "agency-infra-"+component+".json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return microVMHostInfraCheck("microvm_host_infra_"+component, agencysecurity.FindingFail, "host infra metadata missing for "+component+": "+err.Error(), "run 'agency infra up' to start host infrastructure")
	}
	var meta hostInfraDoctorMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return microVMHostInfraCheck("microvm_host_infra_"+component, agencysecurity.FindingFail, "host infra metadata is not parseable for "+component+": "+err.Error(), "run 'agency infra down' then 'agency infra up' to recreate host infrastructure metadata")
	}
	if meta.Service != "agency-infra-"+component || meta.Component != component {
		return microVMHostInfraCheck("microvm_host_infra_"+component, agencysecurity.FindingFail, "host infra metadata does not identify "+component+" correctly", "run 'agency infra down' then 'agency infra up' to recreate host infrastructure metadata")
	}
	if meta.PID <= 0 {
		return microVMHostInfraCheck("microvm_host_infra_"+component, agencysecurity.FindingFail, "host infra metadata has an invalid PID for "+component, "run 'agency infra down' then 'agency infra up' to restart host infrastructure")
	}
	if err := syscall.Kill(meta.PID, 0); err != nil && !os.IsPermission(err) {
		return microVMHostInfraCheck("microvm_host_infra_"+component, agencysecurity.FindingFail, fmt.Sprintf("host infra process for %s is not alive: %v", component, err), "run 'agency infra up' to replace stale host infrastructure processes")
	}
	if strings.TrimSpace(meta.HealthURL) == "" {
		return microVMHostInfraCheck("microvm_host_infra_"+component, agencysecurity.FindingFail, "host infra metadata has no health URL for "+component, "run 'agency infra down' then 'agency infra up' to recreate host infrastructure metadata")
	}
	if component == "egress" {
		if err := microVMHostInfraTCPHealth(ctx, meta.HealthURL); err != nil {
			return microVMHostInfraCheck("microvm_host_infra_"+component, agencysecurity.FindingFail, "host infra health check failed for "+component+": "+err.Error(), "inspect "+meta.LogFile+" and run 'agency infra up'")
		}
		return microVMHostInfraCheck("microvm_host_infra_"+component, agencysecurity.FindingPass, meta.Service+" is healthy with PID "+fmt.Sprint(meta.PID), "")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meta.HealthURL, nil)
	if err != nil {
		return microVMHostInfraCheck("microvm_host_infra_"+component, agencysecurity.FindingFail, "host infra health URL is invalid for "+component+": "+err.Error(), "run 'agency infra down' then 'agency infra up' to recreate host infrastructure metadata")
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return microVMHostInfraCheck("microvm_host_infra_"+component, agencysecurity.FindingFail, "host infra health check failed for "+component+": "+err.Error(), "inspect "+meta.LogFile+" and run 'agency infra up'")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return microVMHostInfraCheck("microvm_host_infra_"+component, agencysecurity.FindingFail, fmt.Sprintf("host infra health check for %s returned HTTP %d", component, resp.StatusCode), "inspect "+meta.LogFile+" and run 'agency infra up'")
	}
	return microVMHostInfraCheck("microvm_host_infra_"+component, agencysecurity.FindingPass, meta.Service+" is healthy with PID "+fmt.Sprint(meta.PID), "")
}

func microVMHostInfraTCPHealth(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	host := parsed.Host
	if host == "" {
		return fmt.Errorf("missing host in %s", rawURL)
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (h *handler) microVMNoLegacyInfraContainersCheck(ctx context.Context) doctorCheckResult {
	if h.deps.Runtime == nil {
		return microVMHostInfraCheck("microvm_no_legacy_infra_containers", agencysecurity.FindingPass, "no container runtime is attached to microVM infrastructure", "")
	}
	status, err := h.deps.Runtime.InfraStatus(ctx)
	if err != nil {
		return microVMHostInfraCheck("microvm_no_legacy_infra_containers", agencysecurity.FindingFail, "could not inspect legacy infra containers: "+err.Error(), "stop the legacy container runtime service or run 'agency infra down'")
	}
	var running []string
	for _, component := range status {
		if component.State == "running" && !strings.HasPrefix(component.ContainerID, "host:") {
			running = append(running, component.Name)
		}
	}
	if len(running) > 0 {
		return microVMHostInfraCheck("microvm_no_legacy_infra_containers", agencysecurity.FindingFail, "legacy infra containers are running: "+strings.Join(running, ", "), "run 'agency infra down' and stop any stale agency-infra-* containers")
	}
	return microVMHostInfraCheck("microvm_no_legacy_infra_containers", agencysecurity.FindingPass, "no legacy infra containers are running", "")
}

func microVMHostInfraCheck(name string, status agencysecurity.FindingStatus, detail, fix string) doctorCheckResult {
	return doctorCheckResult{
		Name:   name,
		Scope:  "runtime",
		Status: status,
		Detail: detail,
		Fix:    fix,
	}
}
