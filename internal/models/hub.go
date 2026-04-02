// agency-gateway/internal/models/hub.go
package models

// HubSource represents a git-based hub source.
type HubSource struct {
	Name   string `yaml:"name" validate:"required"`
	Type   string `yaml:"type" validate:"required,oneof=git" default:"git"`
	URL    string `yaml:"url" validate:"required"`
	Branch string `yaml:"branch" default:"main"`
}

// HubConfig is hub configuration with source registries.
type HubConfig struct {
	Sources []HubSource `yaml:"sources"`
}

// AgencyConfig is the schema for config.yaml.
type AgencyConfig struct {
	Hub HubConfig `yaml:"hub"`
}

// HubInstalledEntry tracks an installed hub component.
type HubInstalledEntry struct {
	Component   string `yaml:"component" validate:"required"`
	Kind        string `yaml:"kind" validate:"required"`
	Source      string `yaml:"source" validate:"required"`
	CommitSHA   string `yaml:"commit_sha" validate:"required"`
	InstalledAt string `yaml:"installed_at" validate:"required"`
}
