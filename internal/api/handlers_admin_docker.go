package api

import (
	"context"
	"fmt"
	"strings"
)

// checkResult mirrors the inline type in adminDoctor so both functions share
// the same JSON shape. It is redeclared here for use by runDockerChecks.
type dockerCheckResult struct {
	Name   string `json:"name"`
	Agent  string `json:"agent,omitempty"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
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

	pass := func(name, detail string) dockerCheckResult {
		return dockerCheckResult{Name: name, Status: "pass", Detail: detail}
	}
	warn := func(name, detail string) dockerCheckResult {
		return dockerCheckResult{Name: name, Status: "warn", Detail: detail}
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
		all, err := h.dc.ListAgencyContainers(ctx, false /* all, not just running */)
		if err != nil {
			results = append(results, warn("docker_orphan_containers",
				"Cannot list containers: "+err.Error()))
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
			results = append(results, warn("docker_orphan_containers",
				fmt.Sprintf("%d orphan workspace container(s): %s", len(orphans), strings.Join(orphans, ", "))))
		} else {
			results = append(results, pass("docker_orphan_containers", "No orphan workspace containers"))
		}
	}()

	// ── 2. Orphan networks ────────────────────────────────────────────────────
	func() {
		nets, err := h.dc.ListNetworksByLabel(ctx, "agency.managed=true")
		if err != nil {
			results = append(results, warn("docker_orphan_networks",
				"Cannot list networks: "+err.Error()))
			return
		}
		var orphans []string
		for _, n := range nets {
			if len(n.Containers) == 0 {
				orphans = append(orphans, n.Name)
			}
		}
		if len(orphans) > 0 {
			results = append(results, warn("docker_orphan_networks",
				fmt.Sprintf("%d orphan network(s) with no connected endpoints: %s",
					len(orphans), strings.Join(orphans, ", "))))
		} else {
			results = append(results, pass("docker_orphan_networks",
				"No orphan agency-managed networks"))
		}
	}()

	// ── 3. Dangling images ────────────────────────────────────────────────────
	func() {
		imgs, err := h.dc.ListAgencyImages(ctx)
		if err != nil {
			results = append(results, warn("docker_dangling_images",
				"Cannot list images: "+err.Error()))
			return
		}
		var dangling []string
		for _, img := range imgs {
			for _, tag := range img.RepoTags {
				// Dangling = has an agency- prefix but is NOT tagged :latest
				if !strings.HasSuffix(tag, ":latest") {
					dangling = append(dangling, tag)
				}
			}
			// Untagged images (RepoTags is empty) are also dangling
			if len(img.RepoTags) == 0 {
				dangling = append(dangling, "<untagged>")
			}
		}
		if len(dangling) > 0 {
			results = append(results, warn("docker_dangling_images",
				fmt.Sprintf("%d dangling agency image(s)", len(dangling))))
		} else {
			results = append(results, pass("docker_dangling_images",
				"No dangling agency images"))
		}
	}()

	// ── 4. Container log sizes ────────────────────────────────────────────────
	func() {
		running, err := h.dc.ListAgencyContainers(ctx, true /* running only */)
		if err != nil {
			results = append(results, warn("docker_log_sizes",
				"Cannot list running containers: "+err.Error()))
			return
		}
		const warnThreshold = 500 * 1024 * 1024 // 500 MB
		var totalBytes int64
		for _, ctr := range running {
			name := strings.TrimPrefix(ctr.Names[0], "/")
			size, err := h.dc.LogFileSize(ctx, name)
			if err == nil {
				totalBytes += size
			}
		}
		totalMB := float64(totalBytes) / (1024 * 1024)
		if totalBytes > warnThreshold {
			results = append(results, warn("docker_log_sizes",
				fmt.Sprintf("Total log size across agency containers: %.1f MB (threshold 500 MB)", totalMB)))
		} else {
			results = append(results, pass("docker_log_sizes",
				fmt.Sprintf("Total log size: %.1f MB", totalMB)))
		}
	}()

	// ── 5. PID limits ─────────────────────────────────────────────────────────
	func() {
		var violations []string
		for _, agentName := range runningAgents {
			wsName := "agency-" + agentName + "-workspace"
			info, err := h.dc.ContainerInspectRaw(ctx, wsName)
			if err != nil {
				violations = append(violations, agentName+"(inspect error: "+err.Error()+")")
				continue
			}
			if info.HostConfig == nil || info.HostConfig.PidsLimit == nil || *info.HostConfig.PidsLimit <= 0 {
				violations = append(violations, agentName)
			}
		}
		if len(violations) > 0 {
			results = append(results, fail("docker_pid_limits",
				fmt.Sprintf("Workspace container(s) missing PID limit: %s", strings.Join(violations, ", "))))
		} else if len(runningAgents) == 0 {
			results = append(results, pass("docker_pid_limits", "No running agents to check"))
		} else {
			results = append(results, pass("docker_pid_limits",
				"All workspace containers have PID limits set"))
		}
	}()

	// ── 6. Network isolation ──────────────────────────────────────────────────
	func() {
		var violations []string
		for _, agentName := range runningAgents {
			// Agent networks follow the pattern agency-{name}-* excluding mediation and egress
			nets, err := h.dc.ListNetworksByLabel(ctx, "agency.agent="+agentName)
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
			results = append(results, fail("docker_network_isolation",
				fmt.Sprintf("Agent network(s) not set to Internal=true: %s", strings.Join(violations, ", "))))
		} else {
			results = append(results, pass("docker_network_isolation",
				"All agent networks are internally isolated"))
		}
	}()

	// ── 7. Log rotation ───────────────────────────────────────────────────────
	func() {
		running, err := h.dc.ListAgencyContainers(ctx, true /* running only */)
		if err != nil {
			results = append(results, warn("docker_log_rotation",
				"Cannot list running containers: "+err.Error()))
			return
		}
		var missing []string
		for _, ctr := range running {
			name := strings.TrimPrefix(ctr.Names[0], "/")
			info, err := h.dc.ContainerInspectRaw(ctx, name)
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
			results = append(results, warn("docker_log_rotation",
				fmt.Sprintf("%d container(s) missing log rotation (max-size): %s",
					len(missing), strings.Join(missing, ", "))))
		} else {
			results = append(results, pass("docker_log_rotation",
				"All agency containers have log rotation configured"))
		}
	}()

	return results
}
