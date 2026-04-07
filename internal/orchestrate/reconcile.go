package orchestrate

import (
	"context"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"

	containers "github.com/geoffbelknap/agency/internal/orchestrate/containers"
)

// Reconcile cleans up orphaned containers and networks from previous gateway runs.
// Called once at gateway startup after Docker client creation, before the HTTP
// server starts. Errors are logged at WARN level but never returned — a failed
// reconcile must not prevent the gateway from starting.
func Reconcile(ctx context.Context, cli *client.Client, knownAgents []string, logger *log.Logger) {
	known := make(map[string]bool, len(knownAgents))
	for _, a := range knownAgents {
		known[a] = true
	}

	// 1. List all agency-managed containers (stopped or running).
	ctrs, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "agency.managed=true")),
	})
	if err != nil {
		logger.Warn("reconcile: cannot list containers", "err", err)
		return
	}

	timeoutSecs := 5
	for _, ctr := range ctrs {
		agentType := ctr.Labels["agency.type"]
		agentName := ctr.Labels["agency.agent"]

		// Derive agent name from container name if label is absent.
		if agentName == "" {
			agentName = extractAgentNameFromNames(ctr.Names)
		}

		// Meeseeks containers: always remove on startup (they are ephemeral).
		if strings.Contains(agentType, "meeseeks") {
			ctrName := primaryName(ctr.Names)
			logger.Warn("reconcile: removing orphaned meeseeks container", "container", ctrName)
			if err := containers.StopAndRemove(ctx, cli, ctrName, timeoutSecs); err != nil {
				logger.Warn("reconcile: could not remove meeseeks container", "container", ctrName, "err", err)
			}
			continue
		}

		// Agent containers: remove if the agent directory no longer exists.
		if agentName != "" && !known[agentName] {
			ctrName := primaryName(ctr.Names)
			logger.Warn("reconcile: removing orphaned agent container", "container", ctrName, "agent", agentName)
			if err := containers.StopAndRemove(ctx, cli, ctrName, timeoutSecs); err != nil {
				logger.Warn("reconcile: could not remove agent container", "container", ctrName, "err", err)
			}
		}
	}

	// 2. List all agency-managed networks and remove those with zero connected containers.
	nets, err := cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "agency.managed=true")),
	})
	if err != nil {
		logger.Warn("reconcile: cannot list networks", "err", err)
		return
	}

	// Shared infrastructure networks are created by infra up and are expected
	// to be empty when no agents are running — they are not orphans.
	infraNets := map[string]bool{
		"agency-mediation": true, "agency-egress-net": true,
		"agency-internal": true, "agency-operator": true,
	}
	for _, net := range nets {
		if len(net.Containers) == 0 && !infraNets[net.Name] {
			logger.Warn("reconcile: removing empty managed network", "network", net.Name)
			if err := containers.RemoveNetwork(ctx, cli, net.Name); err != nil {
				logger.Warn("reconcile: could not remove network", "network", net.Name, "err", err)
			}
		}
	}
}

// primaryName returns the first non-empty container name, stripped of the leading slash.
func primaryName(names []string) string {
	for _, n := range names {
		return strings.TrimPrefix(n, "/")
	}
	return ""
}

// extractAgentNameFromNames parses the agent name from container name patterns
// agency-{name}-workspace or agency-{name}-enforcer.
func extractAgentNameFromNames(names []string) string {
	for _, n := range names {
		n = strings.TrimPrefix(n, "/")
		for _, suffix := range []string{"-workspace", "-enforcer"} {
			if strings.HasPrefix(n, "agency-") && strings.HasSuffix(n, suffix) {
				return strings.TrimSuffix(strings.TrimPrefix(n, "agency-"), suffix)
			}
		}
	}
	return ""
}
