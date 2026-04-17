package runtimehost

import (
	"os"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
)

func containersListOptions() container.ListOptions {
	return container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "agency.managed=true")),
	}
}

func networksListOptions() network.ListOptions {
	return network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "agency.managed=true")),
	}
}

func infraInstanceName() string {
	instance := strings.TrimSpace(strings.ToLower(os.Getenv("AGENCY_INFRA_INSTANCE")))
	if instance == "" {
		return ""
	}
	instance = strings.NewReplacer("_", "-", ".", "-", "/", "-").Replace(instance)
	instance = strings.Trim(instance, "-")
	return instance
}

func gatewayNetName() string {
	instance := infraInstanceName()
	if instance == "" {
		return "agency-gateway"
	}
	return "agency-gateway-" + instance
}

func egressIntNetName() string {
	instance := infraInstanceName()
	if instance == "" {
		return "agency-egress-int"
	}
	return "agency-egress-int-" + instance
}

func egressExtNetName() string {
	instance := infraInstanceName()
	if instance == "" {
		return "agency-egress-ext"
	}
	return "agency-egress-ext-" + instance
}

func operatorNetName() string {
	instance := infraInstanceName()
	if instance == "" {
		return "agency-operator"
	}
	return "agency-operator-" + instance
}
