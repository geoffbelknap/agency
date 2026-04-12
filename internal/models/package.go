package models

import (
	"fmt"
	"strings"
)

const packageAPIVersion = "hub.agency/v2"

var packageKinds = map[string]struct{}{
	"connector": {},
	"service":   {},
	"preset":    {},
	"mission":   {},
	"pack":      {},
	"skill":     {},
	"policy":    {},
	"ontology":  {},
}

type PackageConfig struct {
	APIVersion    string               `yaml:"api_version" json:"api_version"`
	Kind          string               `yaml:"kind" json:"kind"`
	Metadata      PackageMetadata      `yaml:"metadata" json:"metadata"`
	Compatibility PackageCompatibility `yaml:"compatibility,omitempty" json:"compatibility,omitempty"`
	Dependencies  PackageDependencies  `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	Trust         PackageTrust         `yaml:"trust" json:"trust"`
	Spec          map[string]any       `yaml:"spec,omitempty" json:"spec,omitempty"`
}

type PackageMetadata struct {
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version" json:"version"`
	Title   string `yaml:"title,omitempty" json:"title,omitempty"`
}

type PackageCompatibility struct {
	Agency string `yaml:"agency,omitempty" json:"agency,omitempty"`
}

type PackageDependencies struct {
	Connectors []string `yaml:"connectors,omitempty" json:"connectors,omitempty"`
	Packs      []string `yaml:"packs,omitempty" json:"packs,omitempty"`
	Presets    []string `yaml:"presets,omitempty" json:"presets,omitempty"`
	Skills     []string `yaml:"skills,omitempty" json:"skills,omitempty"`
}

type PackageTrust struct {
	Tier              string `yaml:"tier" json:"tier"`
	SignatureRequired bool   `yaml:"signature_required" json:"signature_required"`
	Executable        bool   `yaml:"executable" json:"executable"`
}

func (p *PackageConfig) Validate() error {
	if strings.TrimSpace(p.APIVersion) != packageAPIVersion {
		return fmt.Errorf("api_version must be %s", packageAPIVersion)
	}
	if _, ok := packageKinds[strings.TrimSpace(p.Kind)]; !ok {
		return fmt.Errorf("kind must be one of: connector, service, preset, mission, pack, skill, policy, ontology")
	}
	if strings.TrimSpace(p.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if strings.TrimSpace(p.Metadata.Version) == "" {
		return fmt.Errorf("metadata.version is required")
	}
	return p.Trust.Validate()
}

func (t *PackageTrust) Validate() error {
	if strings.TrimSpace(t.Tier) == "" {
		return fmt.Errorf("trust.tier is required")
	}
	return nil
}
