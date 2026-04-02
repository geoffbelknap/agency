// agency-gateway/internal/models/mission.go
package models

import (
	"fmt"
	"regexp"

	"github.com/google/uuid"
)

var reMissionName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// Valid status values for a Mission.
var validMissionStatuses = map[string]bool{
	"unassigned": true,
	"active":     true,
	"paused":     true,
	"completed":  true,
}

// Valid trigger source values for a MissionTrigger.
var validTriggerSources = map[string]bool{
	"connector": true,
	"channel":   true,
	"schedule":  true,
	"webhook":   true,
	"platform":  true,
}

// MissionTrigger defines an event source that activates a mission.
type MissionTrigger struct {
	Source    string `yaml:"source,omitempty" json:"source,omitempty"`
	Connector string `yaml:"connector,omitempty" json:"connector,omitempty"`
	Channel   string `yaml:"channel,omitempty" json:"channel,omitempty"`
	EventType string `yaml:"event_type,omitempty" json:"event_type,omitempty"`
	Match     string `yaml:"match,omitempty" json:"match,omitempty"`
	Name      string `yaml:"name,omitempty" json:"name,omitempty"`
	Cron      string `yaml:"cron,omitempty" json:"cron,omitempty"`
}

// MissionRequires lists capability and channel dependencies for a mission.
type MissionRequires struct {
	Capabilities []string `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Channels     []string `yaml:"channels,omitempty" json:"channels,omitempty"`
}

// MissionHealth defines health monitoring configuration for a mission.
type MissionHealth struct {
	Indicators   []string `yaml:"indicators,omitempty" json:"indicators,omitempty"`
	BusinessHours string  `yaml:"business_hours,omitempty" json:"business_hours,omitempty"`
}

// MissionBudget defines spending limits for a mission.
type MissionBudget struct {
	Daily   float64 `yaml:"daily,omitempty" json:"daily,omitempty"`
	Monthly float64 `yaml:"monthly,omitempty" json:"monthly,omitempty"`
	PerTask float64 `yaml:"per_task,omitempty" json:"per_task,omitempty"`
}

// MissionReflection configures post-task reflection rounds for a mission.
type MissionReflection struct {
	Enabled   bool     `yaml:"enabled" json:"enabled"`
	MaxRounds int      `yaml:"max_rounds,omitempty" json:"max_rounds,omitempty"`
	Criteria  []string `yaml:"criteria,omitempty" json:"criteria,omitempty"`
}

// SuccessCriterionItem is a single checklist item for success evaluation.
type SuccessCriterionItem struct {
	ID          string `yaml:"id" json:"id"`
	Description string `yaml:"description" json:"description"`
	Required    bool   `yaml:"required" json:"required"`
}

// SuccessEvaluation configures how success criteria are evaluated.
type SuccessEvaluation struct {
	Enabled            bool    `yaml:"enabled" json:"enabled"`
	Mode               string  `yaml:"mode,omitempty" json:"mode,omitempty"`
	Model              string  `yaml:"model,omitempty" json:"model,omitempty"`
	OnFailure          string  `yaml:"on_failure,omitempty" json:"on_failure,omitempty"`
	MaxRetries         int     `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`
	ChecklistThreshold float64 `yaml:"checklist_threshold,omitempty" json:"checklist_threshold,omitempty"`
}

// MissionSuccessCriteria defines what constitutes mission success.
type MissionSuccessCriteria struct {
	Checklist  []SuccessCriterionItem `yaml:"checklist,omitempty" json:"checklist,omitempty"`
	Evaluation *SuccessEvaluation     `yaml:"evaluation,omitempty" json:"evaluation,omitempty"`
}

// FallbackAction is a single step in a fallback strategy.
type FallbackAction struct {
	Action       string `yaml:"action" json:"action"`
	MaxAttempts  int    `yaml:"max_attempts,omitempty" json:"max_attempts,omitempty"`
	Backoff      string `yaml:"backoff,omitempty" json:"backoff,omitempty"`
	DelaySeconds int    `yaml:"delay_seconds,omitempty" json:"delay_seconds,omitempty"`
	Tool         string `yaml:"tool,omitempty" json:"tool,omitempty"`
	Hint         string `yaml:"hint,omitempty" json:"hint,omitempty"`
	Severity     string `yaml:"severity,omitempty" json:"severity,omitempty"`
	Message      string `yaml:"message,omitempty" json:"message,omitempty"`
}

// FallbackPolicy defines a trigger condition and the strategy to execute when it fires.
type FallbackPolicy struct {
	Trigger    string           `yaml:"trigger" json:"trigger"`
	Tool       string           `yaml:"tool,omitempty" json:"tool,omitempty"`
	Capability string           `yaml:"capability,omitempty" json:"capability,omitempty"`
	Threshold  int              `yaml:"threshold,omitempty" json:"threshold,omitempty"`
	Count      int              `yaml:"count,omitempty" json:"count,omitempty"`
	Minutes    int              `yaml:"minutes,omitempty" json:"minutes,omitempty"`
	Strategy   []FallbackAction `yaml:"strategy" json:"strategy"`
}

// MissionFallback configures fallback behavior for a mission.
type MissionFallback struct {
	Policies      []FallbackPolicy `yaml:"policies,omitempty" json:"policies,omitempty"`
	DefaultPolicy *FallbackPolicy  `yaml:"default_policy,omitempty" json:"default_policy,omitempty"`
}

// MissionProceduralMemory configures procedural memory capture and retrieval.
type MissionProceduralMemory struct {
	Capture                bool `yaml:"capture" json:"capture"`
	Retrieve               bool `yaml:"retrieve" json:"retrieve"`
	MaxRetrieved           int  `yaml:"max_retrieved,omitempty" json:"max_retrieved,omitempty"`
	IncludeFailures        bool `yaml:"include_failures,omitempty" json:"include_failures,omitempty"`
	ConsolidationThreshold int  `yaml:"consolidation_threshold,omitempty" json:"consolidation_threshold,omitempty"`
}

// MissionEpisodicMemory configures episodic memory capture and retrieval.
type MissionEpisodicMemory struct {
	Capture       bool `yaml:"capture" json:"capture"`
	Retrieve      bool `yaml:"retrieve" json:"retrieve"`
	MaxRetrieved  int  `yaml:"max_retrieved,omitempty" json:"max_retrieved,omitempty"`
	RetentionDays int  `yaml:"retention_days,omitempty" json:"retention_days,omitempty"`
	ToolEnabled   bool `yaml:"tool_enabled,omitempty" json:"tool_enabled,omitempty"`
}

// Mission represents managed standing instructions for an agent.
type Mission struct {
	ID           string           `yaml:"id" json:"id"`
	Name         string           `yaml:"name" json:"name"`
	Description  string           `yaml:"description" json:"description"`
	Version      int              `yaml:"version" json:"version"`
	Status       string           `yaml:"status" json:"status"`
	AssignedTo   string           `yaml:"assigned_to,omitempty" json:"assigned_to,omitempty"`
	AssignedType string           `yaml:"assigned_type,omitempty" json:"assigned_type,omitempty"`
	Instructions string           `yaml:"instructions" json:"instructions"`
	Triggers     []MissionTrigger `yaml:"triggers,omitempty" json:"triggers,omitempty"`
	Requires     *MissionRequires `yaml:"requires,omitempty" json:"requires,omitempty"`
	Health       *MissionHealth   `yaml:"health,omitempty" json:"health,omitempty"`
	Budget        *MissionBudget   `yaml:"budget,omitempty" json:"budget,omitempty"`
	Meeseeks        bool                     `yaml:"meeseeks,omitempty" json:"meeseeks,omitempty"`
	MeeseeksLimit   int                      `yaml:"meeseeks_limit,omitempty" json:"meeseeks_limit,omitempty"`
	MeeseeksModel   string                   `yaml:"meeseeks_model,omitempty" json:"meeseeks_model,omitempty"`
	MeeseksBudget   float64                  `yaml:"meeseeks_budget,omitempty" json:"meeseeks_budget,omitempty"`
	Reflection      *MissionReflection       `yaml:"reflection,omitempty" json:"reflection,omitempty"`
	SuccessCriteria *MissionSuccessCriteria  `yaml:"success_criteria,omitempty" json:"success_criteria,omitempty"`
	Fallback        *MissionFallback         `yaml:"fallback,omitempty" json:"fallback,omitempty"`
	ProceduralMemory *MissionProceduralMemory `yaml:"procedural_memory,omitempty" json:"procedural_memory,omitempty"`
	EpisodicMemory  *MissionEpisodicMemory   `yaml:"episodic_memory,omitempty" json:"episodic_memory,omitempty"`
	CostMode        string                   `yaml:"cost_mode,omitempty" json:"cost_mode,omitempty"`
	MinTaskTier     string                   `yaml:"min_task_tier,omitempty" json:"min_task_tier,omitempty"`
}

// NewMission creates a new Mission with a generated UUID, version 1, and status unassigned.
func NewMission() *Mission {
	return &Mission{
		ID:      uuid.New().String(),
		Version: 1,
		Status:  "unassigned",
	}
}

// Validate checks all Mission fields for correctness.
func (m *Mission) Validate() error {
	// Name: required, lowercase alphanumeric with hyphens, 2–63 chars
	if m.Name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if len(m.Name) < 2 || len(m.Name) > 63 {
		return fmt.Errorf("name must be between 2 and 63 characters, got %d", len(m.Name))
	}
	if !reMissionName.MatchString(m.Name) {
		return fmt.Errorf("name must be lowercase alphanumeric with hyphens and cannot start or end with a hyphen, got: %s", m.Name)
	}

	// Description: required
	if m.Description == "" {
		return fmt.Errorf("description must not be empty")
	}

	// Instructions: required
	if m.Instructions == "" {
		return fmt.Errorf("instructions must not be empty")
	}

	// Status: if set must be a known value
	if m.Status != "" && !validMissionStatuses[m.Status] {
		return fmt.Errorf("status must be one of: unassigned, active, paused, completed; got: %s", m.Status)
	}

	// Trigger sources: if set must be a known value
	for i, trigger := range m.Triggers {
		if trigger.Source != "" && !validTriggerSources[trigger.Source] {
			return fmt.Errorf("trigger[%d].source must be one of: connector, channel, schedule, webhook, platform; got: %s", i, trigger.Source)
		}
	}

	if m.Reflection != nil {
		if m.Reflection.MaxRounds != 0 && (m.Reflection.MaxRounds < 1 || m.Reflection.MaxRounds > 10) {
			return fmt.Errorf("reflection.max_rounds must be between 1 and 10")
		}
	}
	if m.SuccessCriteria != nil {
		seen := make(map[string]bool)
		for _, c := range m.SuccessCriteria.Checklist {
			if !regexp.MustCompile(`^[a-z0-9_-]+$`).MatchString(c.ID) {
				return fmt.Errorf("success_criteria.checklist.id %q must be lowercase alphanumeric with hyphens/underscores", c.ID)
			}
			if seen[c.ID] {
				return fmt.Errorf("success_criteria.checklist.id %q is duplicated", c.ID)
			}
			seen[c.ID] = true
			if c.Description == "" {
				return fmt.Errorf("success_criteria.checklist.id %q has empty description", c.ID)
			}
		}
		if m.SuccessCriteria.Evaluation != nil {
			e := m.SuccessCriteria.Evaluation
			if e.Mode != "" && e.Mode != "llm" && e.Mode != "checklist_only" {
				return fmt.Errorf("success_criteria.evaluation.mode must be 'llm' or 'checklist_only'")
			}
			if e.OnFailure != "" && e.OnFailure != "flag" && e.OnFailure != "retry" && e.OnFailure != "block" {
				return fmt.Errorf("success_criteria.evaluation.on_failure must be 'flag', 'retry', or 'block'")
			}
			if e.MaxRetries < 0 {
				return fmt.Errorf("success_criteria.evaluation.max_retries must be non-negative")
			}
		}
	}
	if m.Fallback != nil {
		validTriggers := map[string]bool{"tool_error": true, "capability_unavailable": true, "budget_warning": true, "consecutive_errors": true, "timeout": true, "no_progress": true}
		validActions := map[string]bool{"retry": true, "alternative_tool": true, "degrade": true, "simplify": true, "complete_partial": true, "pause_and_assess": true, "escalate": true}
		validateStrategy := func(strategy []FallbackAction, label string) error {
			if len(strategy) == 0 {
				return fmt.Errorf("%s.strategy must not be empty", label)
			}
			for i, a := range strategy {
				if !validActions[a.Action] {
					return fmt.Errorf("%s.strategy.action %q is not a valid action type", label, a.Action)
				}
				if a.Action == "escalate" && i != len(strategy)-1 {
					return fmt.Errorf("escalate action must be the last step in a strategy chain")
				}
			}
			return nil
		}
		for _, p := range m.Fallback.Policies {
			if !validTriggers[p.Trigger] {
				return fmt.Errorf("fallback.policies.trigger %q is not a valid trigger type", p.Trigger)
			}
			if err := validateStrategy(p.Strategy, "fallback.policies"); err != nil {
				return err
			}
		}
		if m.Fallback.DefaultPolicy != nil {
			if err := validateStrategy(m.Fallback.DefaultPolicy.Strategy, "fallback.default_policy"); err != nil {
				return err
			}
		}
	}
	if m.CostMode != "" && m.CostMode != "frugal" && m.CostMode != "balanced" && m.CostMode != "thorough" {
		return fmt.Errorf("cost_mode must be 'frugal', 'balanced', or 'thorough'")
	}
	if m.MinTaskTier != "" && m.MinTaskTier != "minimal" && m.MinTaskTier != "standard" && m.MinTaskTier != "full" {
		return fmt.Errorf("min_task_tier must be 'minimal', 'standard', or 'full'")
	}

	return nil
}
