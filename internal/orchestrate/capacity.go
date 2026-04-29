package orchestrate

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"gopkg.in/yaml.v3"
)

// Memory budget constants (MB).
const (
	containerAgentSlotMB        = 640 // workspace 512 + enforcer 128
	firecrackerHostAgentSlotMB  = 640
	defaultFirecrackerMemoryMiB = 512
	meeseeksSlotMB              = 640

	minReserveMB = 2048

	infraWithEmbeddings    = 4336
	infraWithoutEmbeddings = 1264
)

// CapacityConfig describes the host resource budget for agents and meeseeks.
type CapacityConfig struct {
	HostMemoryMB          int    `yaml:"host_memory_mb"          json:"host_memory_mb"`
	HostCPUCores          int    `yaml:"host_cpu_cores"          json:"host_cpu_cores"`
	SystemReserveMB       int    `yaml:"system_reserve_mb"       json:"system_reserve_mb"`
	InfraOverheadMB       int    `yaml:"infra_overhead_mb"       json:"infra_overhead_mb"`
	RuntimeBackend        string `yaml:"runtime_backend,omitempty" json:"runtime_backend,omitempty"`
	EnforcementMode       string `yaml:"enforcement_mode,omitempty" json:"enforcement_mode,omitempty"`
	MaxAgents             int    `yaml:"max_agents"              json:"max_agents"`
	MaxConcurrentMeesks   int    `yaml:"max_concurrent_meesks"   json:"max_concurrent_meesks"`
	AgentSlotMB           int    `yaml:"agent_slot_mb"           json:"agent_slot_mb"`
	MeeseeksSlotMB        int    `yaml:"meeseeks_slot_mb"        json:"meeseeks_slot_mb"`
	NetworkPoolConfigured bool   `yaml:"network_pool_configured" json:"network_pool_configured"`
}

// ComputeCapacity derives agent/meeseeks capacity from total host memory.
func ComputeCapacity(totalMemoryMB, cpuCores int, hasEmbeddings bool) CapacityConfig {
	return ComputeCapacityForRuntime(totalMemoryMB, cpuCores, hasEmbeddings, "", nil)
}

func ComputeCapacityForRuntime(totalMemoryMB, cpuCores int, hasEmbeddings bool, backend string, backendConfig map[string]string) CapacityConfig {
	reserve := totalMemoryMB / 5
	if reserve < minReserveMB {
		reserve = minReserveMB
	}

	infraOverhead := infraWithoutEmbeddings
	if hasEmbeddings {
		infraOverhead = infraWithEmbeddings
	}

	available := totalMemoryMB - reserve - infraOverhead
	if available < 0 {
		available = 0
	}

	agentSlot := AgentSlotMBForRuntime(backend, backendConfig)
	meeseeksSlot := meeseeksSlotMB
	maxAgents := available / agentSlot
	maxMeeseeks := available / meeseeksSlot

	return CapacityConfig{
		HostMemoryMB:          totalMemoryMB,
		HostCPUCores:          cpuCores,
		SystemReserveMB:       reserve,
		InfraOverheadMB:       infraOverhead,
		RuntimeBackend:        normalizedCapacityBackend(backend),
		EnforcementMode:       firecrackerCapacityEnforcementMode(backend, backendConfig),
		MaxAgents:             maxAgents,
		MaxConcurrentMeesks:   maxMeeseeks,
		AgentSlotMB:           agentSlot,
		MeeseeksSlotMB:        meeseeksSlot,
		NetworkPoolConfigured: false,
	}
}

// ProfileHost detects host resources and computes capacity.
func ProfileHost(hasEmbeddings bool) (CapacityConfig, error) {
	return ProfileHostForRuntime(hasEmbeddings, "", nil)
}

func ProfileHostForRuntime(hasEmbeddings bool, backend string, backendConfig map[string]string) (CapacityConfig, error) {
	mem, err := hostMemoryMB()
	if err != nil {
		return CapacityConfig{}, fmt.Errorf("detect host memory: %w", err)
	}
	cores := runtime.NumCPU()
	return ComputeCapacityForRuntime(mem, cores, hasEmbeddings, backend, backendConfig), nil
}

func AgentSlotMBForRuntime(backend string, backendConfig map[string]string) int {
	if normalizedCapacityBackend(backend) != hostruntimebackend.BackendFirecracker {
		return containerAgentSlotMB
	}
	mode := firecrackerCapacityEnforcementMode(backend, backendConfig)
	if mode == hostruntimebackend.FirecrackerEnforcementModeMicroVM {
		return firecrackerMemoryMiB(backendConfig) * 2
	}
	return firecrackerHostAgentSlotMB
}

func ApplyRuntimeCapacityProfile(cfg CapacityConfig, backend string, backendConfig map[string]string) CapacityConfig {
	if cfg.HostMemoryMB <= 0 {
		return cfg
	}
	available := cfg.HostMemoryMB - cfg.SystemReserveMB - cfg.InfraOverheadMB
	if available < 0 {
		available = 0
	}
	cfg.RuntimeBackend = normalizedCapacityBackend(backend)
	cfg.EnforcementMode = firecrackerCapacityEnforcementMode(backend, backendConfig)
	cfg.AgentSlotMB = AgentSlotMBForRuntime(backend, backendConfig)
	if cfg.MeeseeksSlotMB <= 0 {
		cfg.MeeseeksSlotMB = meeseeksSlotMB
	}
	cfg.MaxAgents = available / cfg.AgentSlotMB
	cfg.MaxConcurrentMeesks = available / cfg.MeeseeksSlotMB
	return cfg
}

func normalizedCapacityBackend(backend string) string {
	backend = strings.TrimSpace(backend)
	if backend == "" {
		return runtimehost.BackendDocker
	}
	if strings.EqualFold(backend, hostruntimebackend.BackendFirecracker) {
		return hostruntimebackend.BackendFirecracker
	}
	return runtimehost.NormalizeContainerBackend(backend)
}

func firecrackerCapacityEnforcementMode(backend string, backendConfig map[string]string) string {
	if normalizedCapacityBackend(backend) != hostruntimebackend.BackendFirecracker {
		return ""
	}
	mode := strings.TrimSpace(strings.ToLower(backendConfig["enforcement_mode"]))
	if mode == hostruntimebackend.FirecrackerEnforcementModeMicroVM {
		return hostruntimebackend.FirecrackerEnforcementModeMicroVM
	}
	return hostruntimebackend.FirecrackerEnforcementModeHostProcess
}

func firecrackerMemoryMiB(backendConfig map[string]string) int {
	raw := strings.TrimSpace(backendConfig["memory_mib"])
	if raw == "" {
		return defaultFirecrackerMemoryMiB
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultFirecrackerMemoryMiB
	}
	return value
}

// hostMemoryMB returns total physical memory in megabytes.
func hostMemoryMB() (int, error) {
	switch runtime.GOOS {
	case "linux":
		return linuxMemoryMB()
	case "darwin":
		return darwinMemoryMB()
	default:
		return 0, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// linuxMemoryMB reads /proc/meminfo for MemTotal.
func linuxMemoryMB() (int, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		// Format: "MemTotal:       16384000 kB"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("unexpected MemTotal format: %s", line)
		}
		kb, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0, fmt.Errorf("parse MemTotal: %w", err)
		}
		return kb / 1024, nil
	}
	return 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
}

// darwinMemoryMB uses sysctl to read hw.memsize (bytes).
func darwinMemoryMB() (int, error) {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0, fmt.Errorf("sysctl hw.memsize: %w", err)
	}
	bytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse hw.memsize: %w", err)
	}
	return int(bytes / (1024 * 1024)), nil
}

// SaveCapacity writes a CapacityConfig to YAML with a header comment.
func SaveCapacity(path string, c CapacityConfig) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal capacity config: %w", err)
	}
	content := "# Generated by agency setup\n" + string(data)
	return os.WriteFile(path, []byte(content), 0644)
}

// LoadCapacity reads a CapacityConfig from a YAML file.
func LoadCapacity(path string) (CapacityConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CapacityConfig{}, fmt.Errorf("read capacity config: %w", err)
	}
	var c CapacityConfig
	if err := yaml.Unmarshal(data, &c); err != nil {
		return CapacityConfig{}, fmt.Errorf("parse capacity config: %w", err)
	}
	return c, nil
}

// CheckSlotAvailable returns nil if there is room for one more agent.
func CheckSlotAvailable(cfg CapacityConfig, runningAgents, runningMeeseeks int) error {
	if cfg.MaxAgents <= 0 {
		return fmt.Errorf("capacity not configured — run agency setup")
	}
	if runningAgents+runningMeeseeks >= cfg.MaxAgents {
		return fmt.Errorf("no agent slots available (%d agents + %d meeseeks = %d, max %d)",
			runningAgents, runningMeeseeks, runningAgents+runningMeeseeks, cfg.MaxAgents)
	}
	return nil
}

// CheckMeeseeksSlotAvailable returns nil if there is room for one more meeseeks.
func CheckMeeseeksSlotAvailable(cfg CapacityConfig, runningAgents, runningMeeseeks int) error {
	if err := CheckSlotAvailable(cfg, runningAgents, runningMeeseeks); err != nil {
		return err
	}
	if runningMeeseeks >= cfg.MaxConcurrentMeesks {
		return fmt.Errorf("meeseeks concurrency limit reached (%d/%d)",
			runningMeeseeks, cfg.MaxConcurrentMeesks)
	}
	return nil
}

// CapacityChanged reports whether two configs differ in operational limits.
func CapacityChanged(old, new CapacityConfig) bool {
	return old.MaxAgents != new.MaxAgents ||
		old.MaxConcurrentMeesks != new.MaxConcurrentMeesks ||
		old.AgentSlotMB != new.AgentSlotMB ||
		old.MeeseeksSlotMB != new.MeeseeksSlotMB ||
		old.RuntimeBackend != new.RuntimeBackend ||
		old.EnforcementMode != new.EnforcementMode ||
		old.NetworkPoolConfigured != new.NetworkPoolConfigured
}
