package runtime

import "time"

const (
	ManifestAPIVersion = "agency/v2"
	ManifestKind       = "runtime_manifest"
	PlannerVersion     = "v2alpha1"

	ReconcileStatePending      = "pending"
	ReconcileStateMaterialized = "materialized"
	ReconcileStateFailed       = "failed"
	ReconcileStateStale        = "stale"

	NodeStateMaterialized = "materialized"
	NodeStateActive       = "active"
	NodeStateStopped      = "stopped"
	NodeStateFailed       = "failed"
)

type Manifest struct {
	APIVersion string         `yaml:"api_version" json:"api_version"`
	Kind       string         `yaml:"kind" json:"kind"`
	Metadata   ManifestMeta   `yaml:"metadata" json:"metadata"`
	Source     ManifestSource `yaml:"source" json:"source"`
	Runtime    RuntimeSpec    `yaml:"runtime" json:"runtime"`
	Status     ManifestStatus `yaml:"status" json:"status"`
}

type ManifestMeta struct {
	ManifestID   string    `yaml:"manifest_id" json:"manifest_id"`
	InstanceID   string    `yaml:"instance_id" json:"instance_id"`
	InstanceName string    `yaml:"instance_name" json:"instance_name"`
	CompiledAt   time.Time `yaml:"compiled_at" json:"compiled_at"`
	Planner      string    `yaml:"planner_version" json:"planner_version"`
}

type ManifestSource struct {
	InstanceRevision time.Time `yaml:"instance_revision" json:"instance_revision"`
}

type RuntimeSpec struct {
	Nodes      []RuntimeNode      `yaml:"nodes,omitempty" json:"nodes,omitempty"`
	Bindings   []RuntimeBinding   `yaml:"bindings,omitempty" json:"bindings,omitempty"`
	Operations []RuntimeOperation `yaml:"operations,omitempty" json:"operations,omitempty"`
}

type RuntimeNode struct {
	NodeID             string            `yaml:"node_id" json:"node_id"`
	Kind               string            `yaml:"kind" json:"kind"`
	Package            RuntimePackageRef `yaml:"package" json:"package"`
	Tools              []string          `yaml:"tools,omitempty" json:"tools,omitempty"`
	CredentialBindings []string          `yaml:"credential_bindings,omitempty" json:"credential_bindings,omitempty"`
	GrantSubjects      []string          `yaml:"grant_subjects,omitempty" json:"grant_subjects,omitempty"`
	ConsentActions     []string          `yaml:"consent_actions,omitempty" json:"consent_actions,omitempty"`
	Materialization    string            `yaml:"materialization_path" json:"materialization_path"`
}

type RuntimePackageRef struct {
	Kind    string `yaml:"kind" json:"kind"`
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
}

type RuntimeBinding struct {
	Name string `yaml:"name" json:"name"`
	Type string `yaml:"type" json:"type"`
}

type RuntimeOperation struct {
	Type   string `yaml:"type" json:"type"`
	NodeID string `yaml:"node_id" json:"node_id"`
	Path   string `yaml:"path" json:"path"`
}

type ManifestStatus struct {
	ReconcileState   string     `yaml:"reconcile_state" json:"reconcile_state"`
	LastReconciledAt *time.Time `yaml:"last_reconciled_at,omitempty" json:"last_reconciled_at,omitempty"`
}

type NodeStatus struct {
	NodeID      string     `yaml:"node_id" json:"node_id"`
	State       string     `yaml:"state" json:"state"`
	UpdatedAt   time.Time  `yaml:"updated_at" json:"updated_at"`
	StartedAt   *time.Time `yaml:"started_at,omitempty" json:"started_at,omitempty"`
	StoppedAt   *time.Time `yaml:"stopped_at,omitempty" json:"stopped_at,omitempty"`
	PID         int        `yaml:"pid,omitempty" json:"pid,omitempty"`
	Port        int        `yaml:"port,omitempty" json:"port,omitempty"`
	URL         string     `yaml:"url,omitempty" json:"url,omitempty"`
	LastError   string     `yaml:"last_error,omitempty" json:"last_error,omitempty"`
	RuntimePath string     `yaml:"runtime_path,omitempty" json:"runtime_path,omitempty"`
}
