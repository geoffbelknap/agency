package models

const (
	EgressModeDenylist             = "denylist"
	EgressModeAllowlist            = "allowlist"
	EgressModeSupervisedStrict     = "supervised-strict"
	EgressModeSupervisedPermissive = "supervised-permissive"
)

type EgressDomainEntry struct {
	Domain     string `yaml:"domain" validate:"required"`
	ApprovedAt string `yaml:"approved_at" validate:"required"`
	ApprovedBy string `yaml:"approved_by" validate:"required"`
	Reason     string `yaml:"reason"`
}

type AgentEgressConfig struct {
	Agent   string              `yaml:"agent" validate:"required"`
	Mode    string              `yaml:"mode" validate:"required,oneof=denylist allowlist supervised-strict supervised-permissive" default:"denylist"`
	Domains []EgressDomainEntry `yaml:"domains"`
}
