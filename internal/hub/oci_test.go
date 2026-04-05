package hub

import (
	"testing"
)

func TestSourceTypeDefault(t *testing.T) {
	s := Source{Name: "official", URL: "https://github.com/geoffbelknap/agency-hub.git"}
	if s.EffectiveType() != "git" {
		t.Errorf("expected git, got %s", s.EffectiveType())
	}
}

func TestSourceTypeOCI(t *testing.T) {
	s := Source{Name: "official", Type: "oci", Registry: "ghcr.io/geoffbelknap/agency-hub"}
	if s.EffectiveType() != "oci" {
		t.Errorf("expected oci, got %s", s.EffectiveType())
	}
}

func TestSourceOCIRef(t *testing.T) {
	s := Source{Name: "official", Type: "oci", Registry: "ghcr.io/geoffbelknap/agency-hub"}
	ref := s.ComponentRef("connector", "limacharlie", "0.5.0")
	expected := "ghcr.io/geoffbelknap/agency-hub/connector/limacharlie:0.5.0"
	if ref != expected {
		t.Errorf("expected %s, got %s", expected, ref)
	}
}

func TestSourceOCIRefLatest(t *testing.T) {
	s := Source{Name: "official", Type: "oci", Registry: "ghcr.io/geoffbelknap/agency-hub"}
	ref := s.ComponentRef("pack", "security-ops", "")
	expected := "ghcr.io/geoffbelknap/agency-hub/pack/security-ops:latest"
	if ref != expected {
		t.Errorf("expected %s, got %s", expected, ref)
	}
}
