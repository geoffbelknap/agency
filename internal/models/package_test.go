package models

import "testing"

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

func TestPackageConfig_Validate_RejectsMissingMetadata(t *testing.T) {
	cfg := PackageConfig{APIVersion: "hub.agency/v2", Kind: "connector"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for missing metadata")
	}
}
