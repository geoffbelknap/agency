package hub

import "github.com/geoffbelknap/agency/internal/hubclient"

import "time"

// UpdateReport is returned by hub update.
type UpdateReport struct {
	Sources   []SourceUpdate     `json:"sources"`
	Available []AvailableUpgrade `json:"available,omitempty"`
	Warnings  []string           `json:"warnings,omitempty"`
}

type SourceUpdate struct {
	Name        string `json:"name"`
	OldCommit   string `json:"old_commit"`
	NewCommit   string `json:"new_commit"`
	CommitCount int    `json:"commit_count"`
}

type AvailableUpgrade struct {
	Name             string `json:"name"`
	Kind             string `json:"kind"`               // "connector", "pack", "managed"
	Category         string `json:"category,omitempty"` // for managed: "ontology", "routing", "services"
	InstalledVersion string `json:"installed_version"`
	AvailableVersion string `json:"available_version"`
	Summary          string `json:"summary,omitempty"`
}

// UpgradeReport is returned by hub upgrade.
type UpgradeReport struct {
	Files      []FileUpgrade      `json:"files,omitempty"`
	Components []ComponentUpgrade `json:"components,omitempty"`
	Warnings   []string           `json:"warnings,omitempty"`
}

type FileUpgrade struct {
	Category string `json:"category"` // "ontology", "routing", "services"
	Path     string `json:"path"`
	Status   string `json:"status"` // "upgraded", "unchanged", "added", "error"
	Summary  string `json:"summary,omitempty"`
}

type ComponentUpgrade struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	OldVersion string `json:"old_version"`
	NewVersion string `json:"new_version"`
	Status     string `json:"status"` // "upgraded", "unchanged", "error"
	Error      string `json:"error,omitempty"`
}

// InstalledPackage records a package installed into the local hub registry.
type InstalledPackage struct {
	Kind                string                         `json:"kind"`
	Name                string                         `json:"name"`
	Version             string                         `json:"version"`
	Trust               string                         `json:"trust"`
	Installed           time.Time                      `json:"installed"`
	Path                string                         `json:"path"`
	Spec                map[string]any                 `json:"spec,omitempty"`
	Assurance           []string                       `json:"assurance,omitempty"`
	AssuranceStatements []hubclient.AssuranceStatement `json:"assurance_statements,omitempty"`
	Publisher           string                         `json:"publisher,omitempty"`
	ReviewScope         string                         `json:"review_scope,omitempty"`
}
