package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackageConfig_Validate_MinimalConnectorPackage(t *testing.T) {
	cfg := PackageConfig{
		APIVersion: "hub.agency/v2",
		Kind:       "connector",
		Metadata: PackageMetadata{
			Name:    "slack-interactivity",
			Version: "1.0.0",
		},
		Trust: PackageTrust{
			Tier:              "verified",
			SignatureRequired: true,
			Executable:        true,
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(): %v", err)
	}
}

func TestPackageConfig_Validate_RejectsInvalidAPIVersion(t *testing.T) {
	cfg := PackageConfig{
		APIVersion: "foo/bar",
		Kind:       "connector",
		Metadata: PackageMetadata{
			Name:    "slack-interactivity",
			Version: "1.0.0",
		},
		Trust: PackageTrust{Tier: "verified"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid api_version")
	}
}

func TestPackageConfig_Validate_RejectsInvalidKind(t *testing.T) {
	cfg := PackageConfig{
		APIVersion: "hub.agency/v2",
		Kind:       "banana",
		Metadata: PackageMetadata{
			Name:    "slack-interactivity",
			Version: "1.0.0",
		},
		Trust: PackageTrust{Tier: "verified"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid kind")
	}
}

func TestPackageConfig_Validate_RejectsMissingTrust(t *testing.T) {
	cfg := PackageConfig{
		APIVersion: "hub.agency/v2",
		Kind:       "connector",
		Metadata: PackageMetadata{
			Name:    "slack-interactivity",
			Version: "1.0.0",
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for missing trust")
	}
}

func TestPackageConfig_Validate_RejectsMissingMetadata(t *testing.T) {
	cfg := PackageConfig{APIVersion: "hub.agency/v2", Kind: "connector", Trust: PackageTrust{Tier: "verified"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for missing metadata")
	}
}

func TestPackageLoadAndValidate_AcceptsCompatibilityAndDependencies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package.yaml")
	if err := os.WriteFile(path, []byte(`api_version: hub.agency/v2
kind: pack
metadata:
  name: security-ops
  version: 1.0.0
compatibility:
  agency: ">=2.0.0"
dependencies:
  connectors: [slack]
  packs: [base-ops]
  presets: [security-triage]
  skills: [triage]
trust:
  tier: verified
  signature_required: true
  executable: false
`), 0o644); err != nil {
		t.Fatalf("write package.yaml: %v", err)
	}

	var cfg PackageConfig
	if err := Load(path, &cfg); err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if len(cfg.Dependencies.Connectors) != 1 || cfg.Dependencies.Connectors[0] != "slack" {
		t.Fatalf("unexpected dependencies: %#v", cfg.Dependencies)
	}
	if cfg.Compatibility.Agency != ">=2.0.0" {
		t.Fatalf("unexpected compatibility: %#v", cfg.Compatibility)
	}
}
