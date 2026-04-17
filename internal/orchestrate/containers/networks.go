package containers

import (
	"context"

	"github.com/geoffbelknap/agency/internal/hostadapter/containerops"
)

type (
	NetworkAPI       = containerops.NetworkAPI
	NetworkingConfig = containerops.NetworkingConfig
	EndpointSettings = containerops.EndpointSettings
	InspectOptions   = containerops.InspectOptions
	CreateOptions    = containerops.CreateOptions
	CreateResponse   = containerops.CreateResponse
	PortSet          = containerops.PortSet
	PortMap          = containerops.PortMap
	PortBinding      = containerops.PortBinding
)

func CreateInternalNetwork(ctx context.Context, cli NetworkAPI, name string, labels map[string]string) error {
	return containerops.CreateInternalNetwork(ctx, cli, name, labels)
}

func CreateEgressNetwork(ctx context.Context, cli NetworkAPI, name string, labels map[string]string) error {
	return containerops.CreateEgressNetwork(ctx, cli, name, labels)
}

func CreateOperatorNetwork(ctx context.Context, cli NetworkAPI, name string, labels map[string]string) error {
	return containerops.CreateOperatorNetwork(ctx, cli, name, labels)
}

func CreateMediationNetwork(ctx context.Context, cli NetworkAPI, name string, labels map[string]string) error {
	return containerops.CreateMediationNetwork(ctx, cli, name, labels)
}

func RemoveNetwork(ctx context.Context, cli NetworkAPI, name string) error {
	return containerops.RemoveNetwork(ctx, cli, name)
}

func IsNetworkNotFound(err error) bool      { return containerops.IsNetworkNotFound(err) }
func IsNetworkAlreadyExists(err error) bool { return containerops.IsNetworkAlreadyExists(err) }

func mergeLabels(labels map[string]string) map[string]string {
	merged := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		merged[k] = v
	}
	merged["agency.managed"] = "true"
	return merged
}
