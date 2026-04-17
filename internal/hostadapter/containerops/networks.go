package containerops

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

type (
	NetworkingConfig = network.NetworkingConfig
	EndpointSettings = network.EndpointSettings
	Inspect          = network.Inspect
	InspectOptions   = network.InspectOptions
	CreateOptions    = network.CreateOptions
	CreateResponse   = network.CreateResponse
	Port             = nat.Port
	PortSet          = nat.PortSet
	PortMap          = nat.PortMap
	PortBinding      = nat.PortBinding
)

type NetworkAPI interface {
	NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
	NetworkRemove(ctx context.Context, networkID string) error
}

func CreateInternalNetwork(ctx context.Context, cli NetworkAPI, name string, labels map[string]string) error {
	_, err := cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:   "bridge",
		Internal: true,
		Labels:   mergeLabels(labels),
	})
	return err
}

func CreateEgressNetwork(ctx context.Context, cli NetworkAPI, name string, labels map[string]string) error {
	_, err := cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:   "bridge",
		Internal: false,
		Labels:   mergeLabels(labels),
	})
	return err
}

func CreateOperatorNetwork(ctx context.Context, cli NetworkAPI, name string, labels map[string]string) error {
	_, err := cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:   "bridge",
		Internal: false,
		Labels:   mergeLabels(labels),
	})
	return err
}

func CreateMediationNetwork(ctx context.Context, cli NetworkAPI, name string, labels map[string]string) error {
	_, err := cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:   "bridge",
		Internal: true,
		Labels:   mergeLabels(labels),
	})
	return err
}

func RemoveNetwork(ctx context.Context, cli NetworkAPI, name string) error {
	err := cli.NetworkRemove(ctx, name)
	if err != nil && !IsNetworkNotFound(err) {
		return err
	}
	return nil
}

func mergeLabels(labels map[string]string) map[string]string {
	merged := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		merged[k] = v
	}
	merged["agency.managed"] = "true"
	return merged
}

func IsNetworkNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such network") ||
		strings.Contains(msg, "not found")
}

func IsNetworkAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists")
}
