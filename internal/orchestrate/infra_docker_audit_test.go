package orchestrate

import (
	"testing"

	"github.com/docker/docker/api/types/container"
)

func TestCheckDockerSocketMounts_Clean(t *testing.T) {
	containers := []container.Summary{
		{
			Names:  []string{"/agency-infra-egress"},
			Labels: map[string]string{"agency.managed": "true"},
			Mounts: []container.MountPoint{
				{Source: "/home/user/.agency/run", Destination: "/run"},
			},
		},
	}
	violations := checkDockerSocketMounts(containers)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations, got %d", len(violations))
	}
}

func TestCheckDockerSocketMounts_Violation(t *testing.T) {
	containers := []container.Summary{
		{
			Names:  []string{"/agency-infra-evil"},
			Labels: map[string]string{"agency.managed": "true"},
			Mounts: []container.MountPoint{
				{Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock"},
			},
		},
	}
	violations := checkDockerSocketMounts(containers)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
}

func TestCheckDockerSocketMounts_SkipsUnmanaged(t *testing.T) {
	containers := []container.Summary{
		{
			Names:  []string{"/some-other"},
			Labels: map[string]string{},
			Mounts: []container.MountPoint{
				{Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock"},
			},
		},
	}
	violations := checkDockerSocketMounts(containers)
	if len(violations) != 0 {
		t.Fatalf("expected 0 for unmanaged, got %d", len(violations))
	}
}
