// agency-gateway/internal/models/org.go
package models

// OrgConfig is the schema for org.yaml.
type OrgConfig struct {
	Version        string `yaml:"version" validate:"required" default:"0.1"`
	Name           string `yaml:"name" validate:"required"`
	Operator       string `yaml:"operator" validate:"required"`
	Created        string `yaml:"created" validate:"required"`
	DeploymentMode string `yaml:"deployment_mode" validate:"required,oneof=standalone team enterprise" default:"standalone"`
}
