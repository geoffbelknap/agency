package deployments

import "time"

type PackRef struct {
	Name      string `yaml:"name" json:"name"`
	Version   string `yaml:"version,omitempty" json:"version,omitempty"`
	HubSource string `yaml:"hub_source,omitempty" json:"hub_source,omitempty"`
}

type CredRef struct {
	Key          string `yaml:"key" json:"key"`
	CredstoreID  string `yaml:"credstore_id" json:"credstore_id"`
	ExportPolicy string `yaml:"export_policy,omitempty" json:"export_policy,omitempty"`
}

type InstanceBinding struct {
	Component  string `yaml:"component" json:"component"`
	InstanceID string `yaml:"instance_id" json:"instance_id"`
	Role       string `yaml:"role" json:"role"`
}

type OwnerRef struct {
	AgencyID   string    `yaml:"agency_id" json:"agency_id"`
	AgencyName string    `yaml:"agency_name" json:"agency_name"`
	ClaimedAt  time.Time `yaml:"claimed_at" json:"claimed_at"`
	Heartbeat  time.Time `yaml:"heartbeat" json:"heartbeat"`
}

type Deployment struct {
	ID            string                 `yaml:"id" json:"id"`
	Name          string                 `yaml:"name" json:"name"`
	Pack          PackRef                `yaml:"pack" json:"pack"`
	SchemaVersion int                    `yaml:"schema_version" json:"schema_version"`
	Config        map[string]interface{} `yaml:"config" json:"config"`
	CredRefs      map[string]CredRef     `yaml:"credrefs" json:"credrefs"`
	Instances     []InstanceBinding      `yaml:"instances" json:"instances"`
	Owner         OwnerRef               `yaml:"owner" json:"owner"`
	CreatedAt     time.Time              `yaml:"created_at" json:"created_at"`
	UpdatedAt     time.Time              `yaml:"updated_at" json:"updated_at"`
	AuditLogPath  string                 `yaml:"audit_log_path" json:"audit_log_path"`
}

type AuditEntry struct {
	Timestamp    time.Time              `yaml:"ts" json:"ts"`
	Actor        map[string]interface{} `yaml:"actor,omitempty" json:"actor,omitempty"`
	Action       string                 `yaml:"action" json:"action"`
	DeploymentID string                 `yaml:"deployment_id" json:"deployment_id"`
	ConfigDiff   map[string]interface{} `yaml:"config_diff,omitempty" json:"config_diff,omitempty"`
	Result       string                 `yaml:"result" json:"result"`
	Metadata     map[string]interface{} `yaml:"metadata,omitempty" json:"metadata,omitempty"`
}

type Bundle struct {
	Deployment *Deployment  `yaml:"deployment" json:"deployment"`
	Schema     *Schema      `yaml:"schema" json:"schema"`
	CredRefs   map[string]CredRef `yaml:"credrefs" json:"credrefs"`
	Bindings   []InstanceBinding `yaml:"bindings" json:"bindings"`
	Audit      [][]byte      `yaml:"-" json:"-"`
}
