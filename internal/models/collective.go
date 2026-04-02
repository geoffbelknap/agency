// agency-gateway/internal/models/collective.go
package models

import "fmt"

// DelegationScope defines what a team member can and cannot delegate.
type DelegationScope struct {
	CanDelegateTo             []string            `yaml:"can_delegate_to"`
	CannotDelegate            []string            `yaml:"cannot_delegate"`
	TaskRequiresApproval []map[string]string `yaml:"task_requires_approval"`
}

// SynthesisPermissions defines output and review permissions for a team member.
type SynthesisPermissions struct {
	OutputScope          string              `yaml:"output_scope" default:"internal"`
	RequiresHumanReview  []map[string]string `yaml:"requires_human_review"`
	AuditAllSynthesis    bool                `yaml:"audit_all_synthesis" default:"true"`
}

// CrossBoundaryAccess defines cross-boundary workspace visibility for a team member.
type CrossBoundaryAccess struct {
	CanRead []string `yaml:"can_read"`
	ReadAll bool     `yaml:"read_all"`
	Paths   []string `yaml:"paths"`
}

// HaltAuthority defines a team member's authority to halt other agents.
type HaltAuthority struct {
	CanHalt        []string `yaml:"can_halt"`
	HaltAll        bool     `yaml:"halt_all"`
	HaltTypes      []string `yaml:"halt_types"`
	RequiresReason bool     `yaml:"requires_reason" default:"true"`
}

// TeamMember represents a human or agent member of a team.
type TeamMember struct {
	Name                 string                `yaml:"name" validate:"required"`
	Type                 string                `yaml:"type" default:"agent"`
	Role                 string                `yaml:"role"`
	AgentType            string                `yaml:"agent_type" default:"standard"`
	DelegationScope      *DelegationScope      `yaml:"delegation_scope"`
	SynthesisPermissions *SynthesisPermissions `yaml:"synthesis_permissions"`
	CrossBoundaryAccess  *CrossBoundaryAccess  `yaml:"cross_boundary_access"`
	HaltAuthority        *HaltAuthority        `yaml:"halt_authority"`
}

// ActivityEntry tracks the current activity state of an agent in a team.
type ActivityEntry struct {
	Agent       string   `yaml:"agent" validate:"required"`
	Status      string   `yaml:"status" default:"idle"`
	WorkingIn   []string `yaml:"working_in"`
	CurrentTask string   `yaml:"current_task"`
	LastActive  string   `yaml:"last_active"`
}

// TeamConfig is the schema for team.yaml files.
type TeamConfig struct {
	Version                 string              `yaml:"version" default:"0.1"`
	Name                    string              `yaml:"name" validate:"required"`
	Description             string              `yaml:"description"`
	Coordinator             string              `yaml:"coordinator"`
	Coverage                string              `yaml:"coverage,omitempty"` // agent that assumes coordination if coordinator goes down (ASK tenet 14)
	Members                 []TeamMember        `yaml:"members"`
	SharedWorkspace         string              `yaml:"shared_workspace"`
	ConflictResolution      string              `yaml:"conflict_resolution" default:"operator"`
	SynthesisReviewRequired []map[string]string `yaml:"synthesis_review_required"`
}

// Validate implements cross-field validation for TeamConfig.
func (tc *TeamConfig) Validate() error {
	if len(tc.Name) < 2 {
		return fmt.Errorf("team name must be at least 2 characters")
	}
	if !ValidateHierarchyName(tc.Name) {
		return fmt.Errorf("team name must be lowercase alphanumeric with hyphens, got: %s", tc.Name)
	}

	// Validate coverage agent: must be a member and different from coordinator.
	if tc.Coverage != "" {
		if tc.Coverage == tc.Coordinator {
			return fmt.Errorf("coverage agent must differ from coordinator, got: %s", tc.Coverage)
		}
		found := false
		for _, m := range tc.Members {
			if m.Name == tc.Coverage {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("coverage agent %q is not a team member", tc.Coverage)
		}
	}

	// Apply defaults to HaltAuthority fields that require slice initialization.
	for i := range tc.Members {
		if tc.Members[i].Type == "" {
			tc.Members[i].Type = "agent"
		}
		if tc.Members[i].AgentType == "" {
			tc.Members[i].AgentType = "standard"
		}
		if tc.Members[i].HaltAuthority != nil {
			ha := tc.Members[i].HaltAuthority
			if len(ha.HaltTypes) == 0 {
				ha.HaltTypes = []string{"supervised", "immediate"}
			}
		}
	}

	return nil
}

// GetMember returns the TeamMember with the given name, or nil if not found.
func (tc *TeamConfig) GetMember(agentName string) *TeamMember {
	for i := range tc.Members {
		if tc.Members[i].Name == agentName {
			return &tc.Members[i]
		}
	}
	return nil
}

// MemberNames returns the names of all team members.
func (tc *TeamConfig) MemberNames() []string {
	names := make([]string, len(tc.Members))
	for i, m := range tc.Members {
		names[i] = m.Name
	}
	return names
}

// Coordinators returns all members with agent_type "coordinator".
func (tc *TeamConfig) Coordinators() []TeamMember {
	var result []TeamMember
	for _, m := range tc.Members {
		if m.AgentType == "coordinator" {
			result = append(result, m)
		}
	}
	return result
}

// FunctionAgents returns all members with agent_type "function".
func (tc *TeamConfig) FunctionAgents() []TeamMember {
	var result []TeamMember
	for _, m := range tc.Members {
		if m.AgentType == "function" {
			result = append(result, m)
		}
	}
	return result
}
