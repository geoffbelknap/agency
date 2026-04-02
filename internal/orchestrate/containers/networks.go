package containers

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/network"
)

// NetworkAPI is the subset of the Docker client used for network operations.
type NetworkAPI interface {
	NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
	NetworkRemove(ctx context.Context, networkID string) error
}

// CreateInternalNetwork creates a bridge network with Internal: true.
// This is the ONLY way to create agent/meeseeks networks — internal networks
// have no external route, enforcing the enforcement boundary (ASK tenet 3).
func CreateInternalNetwork(ctx context.Context, cli NetworkAPI, name string, labels map[string]string) error {
	_, err := cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:   "bridge",
		Internal: true,
		Labels:   mergeLabels(labels),
	})
	return err
}

// CreateEgressNetwork creates a non-internal bridge network.
// Used exclusively for the egress proxy container.
func CreateEgressNetwork(ctx context.Context, cli NetworkAPI, name string, labels map[string]string) error {
	_, err := cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:   "bridge",
		Internal: false,
		Labels:   mergeLabels(labels),
	})
	return err
}

// RemoveNetwork removes a network by name, ignoring "not found" errors.
func RemoveNetwork(ctx context.Context, cli NetworkAPI, name string) error {
	err := cli.NetworkRemove(ctx, name)
	if err != nil && !isNetworkNotFound(err) {
		return err
	}
	return nil
}

// mergeLabels returns a new map containing the caller-supplied labels plus
// the agency.managed=true marker that all managed networks carry.
func mergeLabels(labels map[string]string) map[string]string {
	merged := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		merged[k] = v
	}
	merged["agency.managed"] = "true"
	return merged
}

// isNetworkNotFound returns true for Docker "no such network" errors.
func isNetworkNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such network") ||
		strings.Contains(msg, "not found")
}
