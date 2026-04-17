package hostadapter

import "testing"

func TestNewAdapter(t *testing.T) {
	if got := NewAdapter("docker", nil, nil); got == nil || got.Backend() != "docker" {
		t.Fatalf("docker adapter = %#v", got)
	}
	if got := NewAdapter("podman", nil, nil); got == nil || got.Backend() != "podman" {
		t.Fatalf("podman adapter = %#v", got)
	}
	if got := NewAdapter("probe", nil, nil); got != nil {
		t.Fatalf("probe adapter = %#v, want nil", got)
	}
}
