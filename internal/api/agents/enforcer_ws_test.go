package agents

import (
	"testing"

	"github.com/docker/go-connections/nat"
)

func TestEnforcerWSURLFromBindingsPrefersHostConfigPortBinding(t *testing.T) {
	defaultURL := "ws://agency-agent-enforcer:8081/ws"
	hostBindings := nat.PortMap{
		nat.Port("8081/tcp"): []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "53804"}},
	}

	got := enforcerWSURLFromBindings(defaultURL, hostBindings, nil)
	if got != "ws://127.0.0.1:53804/ws" {
		t.Fatalf("expected host binding URL, got %q", got)
	}
}

func TestEnforcerWSURLFromBindingsFallsBackToDefaultURL(t *testing.T) {
	defaultURL := "ws://agency-agent-enforcer:8081/ws"
	got := enforcerWSURLFromBindings(defaultURL, nil, nil)
	if got != defaultURL {
		t.Fatalf("expected default URL, got %q", got)
	}
}
