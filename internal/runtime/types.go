package runtime

import (
	"time"

	agencyconsent "github.com/geoffbelknap/agency/internal/consent"
)

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
	InstanceRevision    time.Time `yaml:"instance_revision" json:"instance_revision"`
	ConsentDeploymentID string    `yaml:"consent_deployment_id,omitempty" json:"consent_deployment_id,omitempty"`
}

type RuntimeSpec struct {
	Nodes      []RuntimeNode      `yaml:"nodes,omitempty" json:"nodes,omitempty"`
	Bindings   []RuntimeBinding   `yaml:"bindings,omitempty" json:"bindings,omitempty"`
	Operations []RuntimeOperation `yaml:"operations,omitempty" json:"operations,omitempty"`
}

type RuntimeNode struct {
	NodeID              string                               `yaml:"node_id" json:"node_id"`
	Kind                string                               `yaml:"kind" json:"kind"`
	Package             RuntimePackageRef                    `yaml:"package" json:"package"`
	Tools               []string                             `yaml:"tools,omitempty" json:"tools,omitempty"`
	ResourceWhitelist   []RuntimeResourceWhitelistEntry      `yaml:"resource_whitelist,omitempty" json:"resource_whitelist,omitempty"`
	CredentialBindings  []string                             `yaml:"credential_bindings,omitempty" json:"credential_bindings,omitempty"`
	GrantSubjects       []string                             `yaml:"grant_subjects,omitempty" json:"grant_subjects,omitempty"`
	ConsentActions      []string                             `yaml:"consent_actions,omitempty" json:"consent_actions,omitempty"`
	ConsentRequirements map[string]agencyconsent.Requirement `yaml:"consent_requirements,omitempty" json:"consent_requirements,omitempty"`
	Executor            *RuntimeExecutor                     `yaml:"executor,omitempty" json:"executor,omitempty"`
	Ingress             *RuntimeIngressSpec                  `yaml:"ingress,omitempty" json:"ingress,omitempty"`
	Materialization     string                               `yaml:"materialization_path" json:"materialization_path"`
}

type RuntimePackageRef struct {
	Kind    string `yaml:"kind" json:"kind"`
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
}

type RuntimeResourceWhitelistEntry struct {
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
	ID   string `yaml:"id" json:"id"`
}

type RuntimeBinding struct {
	Name   string `yaml:"name" json:"name"`
	Type   string `yaml:"type" json:"type"`
	Target string `yaml:"target,omitempty" json:"target,omitempty"`
}

type RuntimeExecutor struct {
	Kind    string                       `yaml:"kind" json:"kind"`
	BaseURL string                       `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	Actions map[string]RuntimeHTTPAction `yaml:"actions,omitempty" json:"actions,omitempty"`
	Auth    *RuntimeExecutorAuth         `yaml:"auth,omitempty" json:"auth,omitempty"`
}

type RuntimeHTTPAction struct {
	Method         string            `yaml:"method,omitempty" json:"method,omitempty"`
	Path           string            `yaml:"path" json:"path"`
	Headers        map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Query          map[string]string `yaml:"query,omitempty" json:"query,omitempty"`
	Body           map[string]string `yaml:"body,omitempty" json:"body,omitempty"`
	WhitelistField string            `yaml:"whitelist_field,omitempty" json:"whitelist_field,omitempty"`
	WhitelistKind  string            `yaml:"whitelist_kind,omitempty" json:"whitelist_kind,omitempty"`
}

type RuntimeExecutorAuth struct {
	Type    string   `yaml:"type" json:"type"`
	Binding string   `yaml:"binding,omitempty" json:"binding,omitempty"`
	Header  string   `yaml:"header,omitempty" json:"header,omitempty"`
	Prefix  string   `yaml:"prefix,omitempty" json:"prefix,omitempty"`
	Scopes  []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
}

type RuntimeIngressSpec struct {
	PublishedName string `yaml:"published_name" json:"published_name"`
	ConnectorYAML string `yaml:"connector_yaml" json:"connector_yaml"`
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
