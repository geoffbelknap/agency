package admin

import (
	"context"
	"fmt"
	"strings"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

func isSharedInfraNetwork(name string) bool {
	return name == "agency-gateway" ||
		strings.HasPrefix(name, "agency-gateway-") ||
		name == "agency-egress-int" ||
		strings.HasPrefix(name, "agency-egress-int-") ||
		name == "agency-egress-ext" ||
		strings.HasPrefix(name, "agency-egress-ext-") ||
		name == "agency-operator" ||
		strings.HasPrefix(name, "agency-operator-")
}

// dockerCheckResult holds the result of a single infrastructure Docker hygiene check.
type dockerCheckResult struct {
	Name   string `json:"name"`
	Agent  string `json:"agent,omitempty"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

func backendCheckName(backend, suffix string) string {
	backend = runtimehost.NormalizeContainerBackend(backend)
	if backend == "" {
		backend = runtimehost.BackendDocker
	}
	return backend + "_" + suffix
}

// runDockerChecks performs infrastructure-level Docker hygiene checks that are
// not tied to a specific agent. Results are appended to the doctor report.
//
// Checks performed:
//  1. Orphan containers — agency-labelled containers with no matching running agent
//  2. Orphan networks   — agency.managed=true networks with no connected endpoints
//  3. Dangling images   — agency- images not tagged :latest
//  4. Container log sizes — total log bytes across all agency containers
//  5. PID limits        — workspace containers must have PidsLimit > 0
//  6. Network isolation — agent networks must have Internal: true
//  7. Log rotation      — agency containers must have max-size in LogConfig
func (h *handler) runDockerChecks(ctx context.Context, runningAgents []string) []dockerCheckResult {
	var results []dockerCheckResult
	backend := configuredRuntimeBackend(h.deps.Config)

	pass := func(name, detail string) dockerCheckResult {
		return dockerCheckResult{Name: name, Status: "pass", Detail: detail}
	}
	warn := func(name, detail, fix string) dockerCheckResult {
		return dockerCheckResult{Name: name, Status: "warn", Detail: detail, Fix: fix}
	}
	fail := func(name, detail string) dockerCheckResult {
		return dockerCheckResult{Name: name, Status: "fail", Detail: detail}
	}

	// Build a set of known running agent workspace container names for orphan detection.
	knownWorkspaces := make(map[string]struct{}, len(runningAgents))
	for _, a := range runningAgents {
		knownWorkspaces["agency-"+a+"-workspace"] = struct{}{}
	}

	// ── 1. Orphan containers ──────────────────────────────────────────────────
	func() {
		all, err := h.deps.DC.ListAgencyContainers(ctx, false /* all, not just running */)
		if err != nil {
			results = append(results, warn(backendCheckName(backend, "orphan_containers"),
				"Cannot list containers: "+err.Error(), ""))
			return
		}
		var orphans []string
		for _, ctr := range all {
			// Normalise name (Docker prefixes with "/")
			name := ctr.Names[0]
			name = strings.TrimPrefix(name, "/")
			// Only flag workspace containers — enforcer/infra have different lifecycles
			if strings.HasSuffix(name, "-workspace") {
				if _, known := knownWorkspaces[name]; !known {
					orphans = append(orphans, name)
				}
			}
		}
		if len(orphans) > 0 {
			results = append(results, warn(backendCheckName(backend, "orphan_containers"),
				fmt.Sprintf("%d orphan workspace container(s): %s", len(orphans), strings.Join(orphans, ", ")), ""))
		} else {
			results = append(results, pass(backendCheckName(backend, "orphan_containers"), "No orphan workspace containers"))
		}
	}()

	// ── 2. Orphan networks ────────────────────────────────────────────────────
	func() {
		nets, err := h.deps.DC.ListNetworksByLabel(ctx, "agency.managed=true")
		if err != nil {
			results = append(results, warn(backendCheckName(backend, "orphan_networks"),
				"Cannot list networks: "+err.Error(), ""))
			return
		}
		// Shared infrastructure networks are expected to be empty when
		// no agents are running — they're created by infra up, not orphans.
		var orphans []string
		for _, n := range nets {
			inspect, err := h.deps.DC.NetworkInspectRaw(ctx, n.Name)
			if err != nil {
				results = append(results, warn(backendCheckName(backend, "orphan_networks"),
					"Cannot inspect network "+n.Name+": "+err.Error(), ""))
				return
			}
			if len(inspect.Containers) == 0 && !isSharedInfraNetwork(n.Name) {
				orphans = append(orphans, n.Name)
			}
		}
		if len(orphans) > 0 {
			results = append(results, warn(backendCheckName(backend, "orphan_networks"),
				fmt.Sprintf("%d orphan network(s) with no connected endpoints: %s",
					len(orphans), strings.Join(orphans, ", ")), ""))
		} else {
			results = append(results, pass(backendCheckName(backend, "orphan_networks"),
				"No orphan agency-managed networks"))
		}
	}()

	// ── 3. Dangling images ────────────────────────────────────────────────────
	func() {
		imgs, err := h.deps.DC.ListDanglingAgencyImages(ctx)
		if err != nil {
			results = append(results, warn(backendCheckName(backend, "dangling_images"),
				"Cannot list images: "+err.Error(), ""))
			return
		}
		if len(imgs) > 0 {
			results = append(results, warn(backendCheckName(backend, "dangling_images"),
				fmt.Sprintf("%d true dangling agency image(s)", len(imgs)),
				"Run `agency admin prune-images` to remove unused untagged Agency images."))
		} else {
			results = append(results, pass(backendCheckName(backend, "dangling_images"),
				"No dangling agency images"))
		}
	}()

	// ── 4. Container log sizes ────────────────────────────────────────────────
	func() {
		running, err := h.deps.DC.ListAgencyContainers(ctx, true /* running only */)
		if err != nil {
			results = append(results, warn(backendCheckName(backend, "log_sizes"),
				"Cannot list running containers: "+err.Error(), ""))
			return
		}
		const warnThreshold = 500 * 1024 * 1024 // 500 MB
		var totalBytes int64
		for _, ctr := range running {
			name := strings.TrimPrefix(ctr.Names[0], "/")
			size, err := h.deps.DC.LogFileSize(ctx, name)
			if err == nil {
				totalBytes += size
			}
		}
		totalMB := float64(totalBytes) / (1024 * 1024)
		if totalBytes > warnThreshold {
			results = append(results, warn(backendCheckName(backend, "log_sizes"),
				fmt.Sprintf("Total log size across agency containers: %.1f MB (threshold 500 MB)", totalMB), ""))
		} else {
			results = append(results, pass(backendCheckName(backend, "log_sizes"),
				fmt.Sprintf("Total log size: %.1f MB", totalMB)))
		}
	}()

	// ── 5. PID limits ─────────────────────────────────────────────────────────
	func() {
		var violations []string
		for _, agentName := range runningAgents {
			wsName := "agency-" + agentName + "-workspace"
			info, err := h.deps.DC.ContainerInspectRaw(ctx, wsName)
			if err != nil {
				violations = append(violations, agentName+"(inspect error: "+err.Error()+")")
				continue
			}
			if info.HostConfig == nil || info.HostConfig.PidsLimit == nil || *info.HostConfig.PidsLimit <= 0 {
				violations = append(violations, agentName)
			}
		}
		if len(violations) > 0 {
			results = append(results, fail(backendCheckName(backend, "pid_limits"),
				fmt.Sprintf("Workspace container(s) missing PID limit: %s", strings.Join(violations, ", "))))
		} else if len(runningAgents) == 0 {
			results = append(results, pass(backendCheckName(backend, "pid_limits"), "No running agents to check"))
		} else {
			results = append(results, pass(backendCheckName(backend, "pid_limits"),
				"All workspace containers have PID limits set"))
		}
	}()

	// ── 6. Network isolation ──────────────────────────────────────────────────
	func() {
		var violations []string
		for _, agentName := range runningAgents {
			// Agent networks follow the pattern agency-{name}-* excluding mediation and egress
			nets, err := h.deps.DC.ListNetworksByLabel(ctx, "agency.agent="+agentName)
			if err != nil {
				violations = append(violations, agentName+"(list error: "+err.Error()+")")
				continue
			}
			for _, n := range nets {
				// Skip mediation and egress networks — these are intentionally not internal
				if strings.Contains(n.Name, "mediation") || strings.Contains(n.Name, "egress") {
					continue
				}
				if !n.Internal {
					violations = append(violations, n.Name)
				}
			}
		}
		if len(violations) > 0 {
			results = append(results, fail(backendCheckName(backend, "network_isolation"),
				fmt.Sprintf("Agent network(s) not set to Internal=true: %s", strings.Join(violations, ", "))))
		} else {
			results = append(results, pass(backendCheckName(backend, "network_isolation"),
				"All agent networks are internally isolated"))
		}
	}()

	// ── 7. Log rotation ───────────────────────────────────────────────────────
	func() {
		running, err := h.deps.DC.ListAgencyContainers(ctx, true /* running only */)
		if err != nil {
			results = append(results, warn(backendCheckName(backend, "log_rotation"),
				"Cannot list running containers: "+err.Error(), ""))
			return
		}
		var missing []string
		for _, ctr := range running {
			name := strings.TrimPrefix(ctr.Names[0], "/")
			info, err := h.deps.DC.ContainerInspectRaw(ctx, name)
			if err != nil {
				continue
			}
			if info.HostConfig == nil {
				missing = append(missing, name)
				continue
			}
			// max-size must be configured in LogConfig options
			if _, ok := info.HostConfig.LogConfig.Config["max-size"]; !ok {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			results = append(results, warn(backendCheckName(backend, "log_rotation"),
				fmt.Sprintf("%d container(s) missing log rotation (max-size): %s",
					len(missing), strings.Join(missing, ", ")), ""))
		} else {
			results = append(results, pass(backendCheckName(backend, "log_rotation"),
				"All agency containers have log rotation configured"))
		}
	}()

	return results
}
