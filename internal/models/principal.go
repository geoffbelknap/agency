// agency-gateway/internal/models/principal.go
package models

// HumanPrincipal represents a human principal with roles and access control.
type HumanPrincipal struct {
	ID               string   `yaml:"id" validate:"required"`
	Name             string   `yaml:"name" validate:"required"`
	Roles            []string `yaml:"roles" validate:"required"`
	Created          string   `yaml:"created" validate:"required"`
	Status           string   `yaml:"status" default:"active"`
	ExceptionDomains []string `yaml:"exception_domains"`
}

// AgentPrincipal represents an agent principal.
type AgentPrincipal struct {
	ID     string `yaml:"id" validate:"required"`
	Name   string `yaml:"name" validate:"required"`
	Type   string `yaml:"type" default:"standard"`
	Status string `yaml:"status" default:"active"`
}

// TeamPrincipal represents a team principal with members.
type TeamPrincipal struct {
	ID      string   `yaml:"id" validate:"required"`
	Name    string   `yaml:"name" validate:"required"`
	Members []string `yaml:"members"`
}

// ExceptionRoute maps exception domains to approving principals.
type ExceptionRoute struct {
	Domain              string   `yaml:"domain" validate:"required"`
	Approvers           []string `yaml:"approvers" validate:"required"`
	RequiresDualApproval bool    `yaml:"requires_dual_approval"`
}

// PrincipalsConfig is the schema for principals.yaml.
type PrincipalsConfig struct {
	Version         string           `yaml:"version" default:"0.1"`
	Humans          []HumanPrincipal `yaml:"humans"`
	Agents          []AgentPrincipal `yaml:"agents"`
	Teams           []TeamPrincipal  `yaml:"teams"`
	ExceptionRoutes []ExceptionRoute `yaml:"exception_routes"`
}

// Validate applies defaults to nested structs and validates cross-field constraints.
func (c *PrincipalsConfig) Validate() error {
	// Apply defaults to humans
	for i := range c.Humans {
		if c.Humans[i].Status == "" {
			c.Humans[i].Status = "active"
		}
	}

	// Apply defaults to agents
	for i := range c.Agents {
		if c.Agents[i].Type == "" {
			c.Agents[i].Type = "standard"
		}
		if c.Agents[i].Status == "" {
			c.Agents[i].Status = "active"
		}
	}

	// Apply defaults to exception routes (RequiresDualApproval defaults to false by Go zero value)

	return nil
}
