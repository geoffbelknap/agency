package agentruntime

import (
	"testing"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

func TestUsesCreateTimeMediationNetworks(t *testing.T) {
	for _, backend := range []string{
		runtimehost.BackendContainerd,
		runtimehost.BackendAppleContainer,
		"apple",
		"container",
	} {
		if !usesCreateTimeMediationNetworks(backend) {
			t.Fatalf("usesCreateTimeMediationNetworks(%q) = false, want true", backend)
		}
	}
	for _, backend := range []string{
		runtimehost.BackendDocker,
		runtimehost.BackendPodman,
		"",
	} {
		if usesCreateTimeMediationNetworks(backend) {
			t.Fatalf("usesCreateTimeMediationNetworks(%q) = true, want false", backend)
		}
	}
}
