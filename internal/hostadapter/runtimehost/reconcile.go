package runtimehost

import (
	"context"
	"strings"

	dockerclient "github.com/docker/docker/client"
	"log/slog"

	containers "github.com/geoffbelknap/agency/internal/orchestrate/containers"
)

func Reconcile(ctx context.Context, cli *dockerclient.Client, knownAgents []string, logger *slog.Logger) {
	known := make(map[string]bool, len(knownAgents))
	for _, agent := range knownAgents {
		known[agent] = true
	}

	ctrs, err := cli.ContainerList(ctx, containersListOptions())
	if err != nil {
		logger.Warn("reconcile: cannot list containers", "err", err)
		return
	}

	timeoutSecs := 5
	for _, ctr := range ctrs {
		agentType := ctr.Labels["agency.type"]
		agentName := ctr.Labels["agency.agent"]
		if agentName == "" {
			agentName = extractAgentNameFromNames(ctr.Names)
		}

		if strings.Contains(agentType, "meeseeks") {
			ctrName := primaryName(ctr.Names)
			logger.Warn("reconcile: removing orphaned meeseeks container", "container", ctrName)
			if err := containers.StopAndRemove(ctx, cli, ctrName, timeoutSecs); err != nil {
				logger.Warn("reconcile: could not remove meeseeks container", "container", ctrName, "err", err)
			}
			continue
		}

		if agentName != "" && !known[agentName] {
			ctrName := primaryName(ctr.Names)
			logger.Warn("reconcile: removing orphaned agent container", "container", ctrName, "agent", agentName)
			if err := containers.StopAndRemove(ctx, cli, ctrName, timeoutSecs); err != nil {
				logger.Warn("reconcile: could not remove agent container", "container", ctrName, "err", err)
			}
		}
	}

	nets, err := cli.NetworkList(ctx, networksListOptions())
	if err != nil {
		logger.Warn("reconcile: cannot list networks", "err", err)
		return
	}

	infraNets := map[string]bool{
		gatewayNetName():   true,
		egressIntNetName(): true,
		egressExtNetName(): true,
		operatorNetName():  true,
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

func primaryName(names []string) string {
	for _, name := range names {
		return strings.TrimPrefix(name, "/")
	}
	return ""
}

func extractAgentNameFromNames(names []string) string {
	for _, name := range names {
		name = strings.TrimPrefix(name, "/")
		for _, suffix := range []string{"-workspace", "-enforcer"} {
			if strings.HasPrefix(name, prefix+"-") && strings.HasSuffix(name, suffix) {
				return strings.TrimSuffix(strings.TrimPrefix(name, prefix+"-"), suffix)
			}
		}
	}
	return ""
}
