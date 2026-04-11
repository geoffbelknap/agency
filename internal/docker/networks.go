package docker

import "strings"

// IsGatewayMediationNetwork reports whether a Docker network is the shared
// service mediation plane used by enforcers to reach gateway-mediated services.
// It accepts the current hub-and-spoke gateway network name and the older
// mediation network names for backwards compatibility.
func IsGatewayMediationNetwork(name string) bool {
	return strings.HasPrefix(name, "agency-gateway") || strings.Contains(name, "mediation")
}

// EnforcerHasOperatorOverridePath reports whether the enforcer is attached to
// a network that preserves operator override via the gateway mediation plane.
func EnforcerHasOperatorOverridePath(networks []string) bool {
	for _, net := range networks {
		if IsGatewayMediationNetwork(net) {
			return true
		}
	}
	return false
}

// EnforcerUnexpectedExternalNetworks reports external-facing networks that an
// enforcer should not require for operator override or service mediation.
func EnforcerUnexpectedExternalNetworks(networks []string) []string {
	var unexpected []string
	for _, net := range networks {
		if strings.HasPrefix(net, "agency-operator") || strings.HasPrefix(net, "agency-egress-ext") {
			unexpected = append(unexpected, net)
		}
	}
	return unexpected
}
