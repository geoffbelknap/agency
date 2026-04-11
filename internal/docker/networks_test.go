package docker

import "testing"

func TestIsGatewayMediationNetwork(t *testing.T) {
	tests := []struct {
		name string
		net  string
		want bool
	}{
		{name: "current gateway network", net: "agency-gateway", want: true},
		{name: "scoped gateway network", net: "agency-gateway-danger-home-123", want: true},
		{name: "legacy mediation network", net: "agency-mediation", want: true},
		{name: "agent internal", net: "agency-agent-internal", want: false},
		{name: "egress internal", net: "agency-egress-int", want: false},
		{name: "operator", net: "agency-operator", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsGatewayMediationNetwork(tt.net); got != tt.want {
				t.Fatalf("IsGatewayMediationNetwork(%q) = %v, want %v", tt.net, got, tt.want)
			}
		})
	}
}

func TestEnforcerHasOperatorOverridePath(t *testing.T) {
	tests := []struct {
		name     string
		networks []string
		want     bool
	}{
		{
			name:     "current topology",
			networks: []string{"agency-demo-internal", "agency-egress-int", "agency-gateway", "agency-operator"},
			want:     true,
		},
		{
			name:     "legacy mediation topology",
			networks: []string{"agency-demo-internal", "agency-mediation"},
			want:     true,
		},
		{
			name:     "missing mediation plane",
			networks: []string{"agency-demo-internal", "agency-egress-int", "agency-operator"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EnforcerHasOperatorOverridePath(tt.networks); got != tt.want {
				t.Fatalf("EnforcerHasOperatorOverridePath(%v) = %v, want %v", tt.networks, got, tt.want)
			}
		})
	}
}

func TestEnforcerUnexpectedExternalNetworks(t *testing.T) {
	tests := []struct {
		name     string
		networks []string
		want     []string
	}{
		{
			name:     "no external networks",
			networks: []string{"agency-demo-internal", "agency-egress-int", "agency-gateway"},
			want:     nil,
		},
		{
			name:     "operator network flagged",
			networks: []string{"agency-demo-internal", "agency-gateway", "agency-operator"},
			want:     []string{"agency-operator"},
		},
		{
			name:     "scoped operator and egress ext flagged",
			networks: []string{"agency-demo-internal", "agency-gateway", "agency-operator-danger-home-123", "agency-egress-ext"},
			want:     []string{"agency-operator-danger-home-123", "agency-egress-ext"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnforcerUnexpectedExternalNetworks(tt.networks)
			if len(got) != len(tt.want) {
				t.Fatalf("EnforcerUnexpectedExternalNetworks(%v) = %v, want %v", tt.networks, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("EnforcerUnexpectedExternalNetworks(%v) = %v, want %v", tt.networks, got, tt.want)
				}
			}
		})
	}
}
