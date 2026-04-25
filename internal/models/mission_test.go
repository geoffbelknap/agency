// agency-gateway/internal/models/mission_test.go
package models

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// validMission returns a Mission with all required fields populated.
func validMission() *Mission {
	return &Mission{
		ID:           "test-id",
		Name:         "my-mission",
		Description:  "A test mission",
		Version:      1,
		Status:       "unassigned",
		Instructions: "Do the thing.",
	}
}

// TestNewMission verifies that NewMission produces a valid, initialized mission.
func TestNewMission(t *testing.T) {
	m := NewMission()

	if m.ID == "" {
		t.Error("expected non-empty ID")
	}
	if m.Version != 1 {
		t.Errorf("expected version=1, got %d", m.Version)
	}
	if m.Status != "unassigned" {
		t.Errorf("expected status=unassigned, got %q", m.Status)
	}

	// Two calls should produce distinct IDs.
	m2 := NewMission()
	if m.ID == m2.ID {
		t.Error("expected distinct IDs for separate NewMission calls")
	}
}

// TestMission_Validate_Valid tests that a fully valid mission passes validation.
func TestMission_Validate_Valid(t *testing.T) {
	m := validMission()
	if err := m.Validate(); err != nil {
		t.Errorf("expected valid mission to pass, got: %v", err)
	}
}

// TestMission_Validate_Name tests name validation rules.
func TestMission_Validate_Name(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
		errFrag string
	}{
		{"empty", "", true, "name"},
		{"single_char", "a", true, "name"},
		{"two_chars", "ab", false, ""},
		{"63_chars", strings.Repeat("a", 63), false, ""},
		{"64_chars", strings.Repeat("a", 64), true, "name"},
		{"valid_hyphens", "my-mission-name", false, ""},
		{"leading_hyphen", "-mission", true, "name"},
		{"trailing_hyphen", "mission-", true, "name"},
		{"uppercase", "My-Mission", true, "name"},
		{"digits", "mission-42", false, ""},
		{"all_digits", "42", false, ""},
		{"spaces", "my mission", true, "name"},
		{"underscore", "my_mission", true, "name"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validMission()
			m.Name = tc.value
			err := m.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for name %q, got nil", tc.value)
				}
				if tc.errFrag != "" && !strings.Contains(err.Error(), tc.errFrag) {
					t.Errorf("expected error to contain %q, got: %v", tc.errFrag, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected valid name %q to pass, got: %v", tc.value, err)
				}
			}
		})
	}
}

// TestMission_Validate_Description tests that description is required.
func TestMission_Validate_Description(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		m := validMission()
		m.Description = ""
		err := m.Validate()
		if err == nil {
			t.Fatal("expected error for empty description, got nil")
		}
		if !strings.Contains(err.Error(), "description") {
			t.Errorf("expected description error, got: %v", err)
		}
	})

	t.Run("present", func(t *testing.T) {
		m := validMission()
		m.Description = "Some description"
		if err := m.Validate(); err != nil {
			t.Errorf("expected valid, got: %v", err)
		}
	})
}

// TestMission_Validate_Instructions tests that instructions are required.
func TestMission_Validate_Instructions(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		m := validMission()
		m.Instructions = ""
		err := m.Validate()
		if err == nil {
			t.Fatal("expected error for empty instructions, got nil")
		}
		if !strings.Contains(err.Error(), "instructions") {
			t.Errorf("expected instructions error, got: %v", err)
		}
	})

	t.Run("present", func(t *testing.T) {
		m := validMission()
		m.Instructions = "Monitor the queue and respond to items."
		if err := m.Validate(); err != nil {
			t.Errorf("expected valid, got: %v", err)
		}
	})
}

// TestMission_Validate_Status tests status validation.
func TestMission_Validate_Status(t *testing.T) {
	validStatuses := []string{"unassigned", "active", "paused", "completed"}
	for _, s := range validStatuses {
		t.Run("valid_"+s, func(t *testing.T) {
			m := validMission()
			m.Status = s
			if err := m.Validate(); err != nil {
				t.Errorf("expected status %q to be valid, got: %v", s, err)
			}
		})
	}

	t.Run("empty_allowed", func(t *testing.T) {
		m := validMission()
		m.Status = ""
		if err := m.Validate(); err != nil {
			t.Errorf("expected empty status to be allowed, got: %v", err)
		}
	})

	invalidStatuses := []string{"draft", "running", "stopped", "ACTIVE", "Paused"}
	for _, s := range invalidStatuses {
		t.Run("invalid_"+s, func(t *testing.T) {
			m := validMission()
			m.Status = s
			err := m.Validate()
			if err == nil {
				t.Fatalf("expected error for status %q, got nil", s)
			}
			if !strings.Contains(err.Error(), "status") {
				t.Errorf("expected status error, got: %v", err)
			}
		})
	}
}

// TestMission_Validate_TriggerSource tests trigger source validation.
func TestMission_Validate_TriggerSource(t *testing.T) {
	validSources := []string{"connector", "channel", "schedule", "webhook", "platform"}
	for _, src := range validSources {
		t.Run("valid_"+src, func(t *testing.T) {
			m := validMission()
			m.Triggers = []MissionTrigger{{Source: src}}
			if err := m.Validate(); err != nil {
				t.Errorf("expected source %q to be valid, got: %v", src, err)
			}
		})
	}

	t.Run("empty_source_allowed", func(t *testing.T) {
		m := validMission()
		m.Triggers = []MissionTrigger{{Name: "my-trigger"}}
		if err := m.Validate(); err != nil {
			t.Errorf("expected empty source to be allowed, got: %v", err)
		}
	})

	t.Run("no_triggers", func(t *testing.T) {
		m := validMission()
		m.Triggers = nil
		if err := m.Validate(); err != nil {
			t.Errorf("expected nil triggers to be allowed, got: %v", err)
		}
	})

	invalidSources := []string{"cron", "event", "timer", "CONNECTOR", "Webhook"}
	for _, src := range invalidSources {
		t.Run("invalid_"+src, func(t *testing.T) {
			m := validMission()
			m.Triggers = []MissionTrigger{{Source: src}}
			err := m.Validate()
			if err == nil {
				t.Fatalf("expected error for source %q, got nil", src)
			}
			if !strings.Contains(err.Error(), "source") {
				t.Errorf("expected source error, got: %v", err)
			}
		})
	}

	t.Run("second_trigger_invalid", func(t *testing.T) {
		m := validMission()
		m.Triggers = []MissionTrigger{
			{Source: "connector"},
			{Source: "bad-source"},
		}
		err := m.Validate()
		if err == nil {
			t.Fatal("expected error for invalid second trigger source, got nil")
		}
		if !strings.Contains(err.Error(), "trigger[1]") {
			t.Errorf("expected error to reference trigger index 1, got: %v", err)
		}
	})
}

// TestMission_Validate_OptionalFields verifies that optional fields are accepted.
func TestMission_Validate_OptionalFields(t *testing.T) {
	m := validMission()
	m.AssignedTo = "agent-abc"
	m.AssignedType = "agent"
	m.Requires = &MissionRequires{
		Capabilities: []string{"slack"},
		Channels:     []string{"#alerts"},
	}
	m.Health = &MissionHealth{
		Indicators:    []string{"queue_depth"},
		BusinessHours: "09:00-17:00",
	}
	m.Budget = &MissionBudget{
		Daily:   5.00,
		Monthly: 100.00,
		PerTask: 0.50,
	}
	m.Triggers = []MissionTrigger{
		{
			Source:    "connector",
			Connector: "slack",
			EventType: "message",
			Match:     "urgent",
			Name:      "urgent-slack",
		},
		{
			Source: "schedule",
			Cron:   "0 9 * * 1-5",
			Name:   "morning-digest",
		},
	}

	if err := m.Validate(); err != nil {
		t.Errorf("expected fully populated mission to be valid, got: %v", err)
	}
}

// --- YAML deserialization tests for new fields ---

// TestMission_YAML_Reflection verifies YAML deserialization of the reflection block.
func TestMission_YAML_Reflection(t *testing.T) {
	input := `
name: test-mission
description: test
instructions: do stuff
reflection:
  enabled: true
  max_rounds: 3
  criteria:
    - accuracy
    - completeness
`
	var m Mission
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml unmarshal failed: %v", err)
	}
	if m.Reflection == nil {
		t.Fatal("expected Reflection to be set")
	}
	if !m.Reflection.Enabled {
		t.Error("expected Reflection.Enabled=true")
	}
	if m.Reflection.MaxRounds != 3 {
		t.Errorf("expected MaxRounds=3, got %d", m.Reflection.MaxRounds)
	}
	if len(m.Reflection.Criteria) != 2 || m.Reflection.Criteria[0] != "accuracy" {
		t.Errorf("unexpected Criteria: %v", m.Reflection.Criteria)
	}
}

// TestMission_YAML_SuccessCriteria verifies YAML deserialization of the success_criteria block.
func TestMission_YAML_SuccessCriteria(t *testing.T) {
	input := `
name: test-mission
description: test
instructions: do stuff
success_criteria:
  checklist:
    - id: task-done
      description: The main task is complete
      required: true
    - id: no-errors
      description: No errors were emitted
      required: false
  evaluation:
    enabled: true
    mode: llm
    model: fast
    on_failure: retry
    max_retries: 2
    checklist_threshold: 0.8
`
	var m Mission
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml unmarshal failed: %v", err)
	}
	if m.SuccessCriteria == nil {
		t.Fatal("expected SuccessCriteria to be set")
	}
	if len(m.SuccessCriteria.Checklist) != 2 {
		t.Fatalf("expected 2 checklist items, got %d", len(m.SuccessCriteria.Checklist))
	}
	if m.SuccessCriteria.Checklist[0].ID != "task-done" {
		t.Errorf("unexpected checklist[0].id: %s", m.SuccessCriteria.Checklist[0].ID)
	}
	if !m.SuccessCriteria.Checklist[0].Required {
		t.Error("expected checklist[0].required=true")
	}
	e := m.SuccessCriteria.Evaluation
	if e == nil {
		t.Fatal("expected Evaluation to be set")
	}
	if e.Mode != "llm" {
		t.Errorf("expected mode=llm, got %q", e.Mode)
	}
	if e.OnFailure != "retry" {
		t.Errorf("expected on_failure=retry, got %q", e.OnFailure)
	}
	if e.MaxRetries != 2 {
		t.Errorf("expected max_retries=2, got %d", e.MaxRetries)
	}
	if e.ChecklistThreshold != 0.8 {
		t.Errorf("expected checklist_threshold=0.8, got %f", e.ChecklistThreshold)
	}
}

// TestMission_YAML_Fallback verifies YAML deserialization of the fallback block.
func TestMission_YAML_Fallback(t *testing.T) {
	input := `
name: test-mission
description: test
instructions: do stuff
fallback:
  policies:
    - trigger: tool_error
      tool: web-search
      threshold: 3
      strategy:
        - action: retry
          max_attempts: 2
          backoff: exponential
        - action: escalate
          severity: warning
          message: web-search unavailable
`
	var m Mission
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml unmarshal failed: %v", err)
	}
	if m.Fallback == nil {
		t.Fatal("expected Fallback to be set")
	}
	if len(m.Fallback.Policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(m.Fallback.Policies))
	}
	p := m.Fallback.Policies[0]
	if p.Trigger != "tool_error" {
		t.Errorf("expected trigger=tool_error, got %q", p.Trigger)
	}
	if len(p.Strategy) != 2 {
		t.Fatalf("expected 2 strategy steps, got %d", len(p.Strategy))
	}
	if p.Strategy[0].Action != "retry" {
		t.Errorf("expected action=retry, got %q", p.Strategy[0].Action)
	}
	if p.Strategy[1].Action != "escalate" {
		t.Errorf("expected action=escalate, got %q", p.Strategy[1].Action)
	}
}

// TestMission_YAML_ProceduralMemory verifies YAML deserialization of procedural_memory.
func TestMission_YAML_ProceduralMemory(t *testing.T) {
	input := `
name: test-mission
description: test
instructions: do stuff
procedural_memory:
  capture: true
  retrieve: true
  max_retrieved: 5
  include_failures: true
  consolidation_threshold: 10
`
	var m Mission
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml unmarshal failed: %v", err)
	}
	if m.ProceduralMemory == nil {
		t.Fatal("expected ProceduralMemory to be set")
	}
	if !m.ProceduralMemory.Capture {
		t.Error("expected Capture=true")
	}
	if !m.ProceduralMemory.Retrieve {
		t.Error("expected Retrieve=true")
	}
	if m.ProceduralMemory.MaxRetrieved != 5 {
		t.Errorf("expected MaxRetrieved=5, got %d", m.ProceduralMemory.MaxRetrieved)
	}
	if !m.ProceduralMemory.IncludeFailures {
		t.Error("expected IncludeFailures=true")
	}
	if m.ProceduralMemory.ConsolidationThreshold != 10 {
		t.Errorf("expected ConsolidationThreshold=10, got %d", m.ProceduralMemory.ConsolidationThreshold)
	}
}

// TestMission_YAML_EpisodicMemory verifies YAML deserialization of episodic_memory.
func TestMission_YAML_EpisodicMemory(t *testing.T) {
	input := `
name: test-mission
description: test
instructions: do stuff
episodic_memory:
  capture: true
  retrieve: false
  max_retrieved: 10
  retention_days: 30
  tool_enabled: true
`
	var m Mission
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml unmarshal failed: %v", err)
	}
	if m.EpisodicMemory == nil {
		t.Fatal("expected EpisodicMemory to be set")
	}
	if !m.EpisodicMemory.Capture {
		t.Error("expected Capture=true")
	}
	if m.EpisodicMemory.Retrieve {
		t.Error("expected Retrieve=false")
	}
	if m.EpisodicMemory.MaxRetrieved != 10 {
		t.Errorf("expected MaxRetrieved=10, got %d", m.EpisodicMemory.MaxRetrieved)
	}
	if m.EpisodicMemory.RetentionDays != 30 {
		t.Errorf("expected RetentionDays=30, got %d", m.EpisodicMemory.RetentionDays)
	}
	if !m.EpisodicMemory.ToolEnabled {
		t.Error("expected ToolEnabled=true")
	}
}

// TestMission_YAML_CostModeAndTier verifies YAML deserialization of cost_mode and min_task_tier.
func TestMission_YAML_CostModeAndTier(t *testing.T) {
	input := `
name: test-mission
description: test
instructions: do stuff
cost_mode: balanced
min_task_tier: standard
`
	var m Mission
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml unmarshal failed: %v", err)
	}
	if m.CostMode != "balanced" {
		t.Errorf("expected cost_mode=balanced, got %q", m.CostMode)
	}
	if m.MinTaskTier != "standard" {
		t.Errorf("expected min_task_tier=standard, got %q", m.MinTaskTier)
	}
}

// --- Validation tests for new fields ---

// TestMission_Validate_Reflection tests reflection.max_rounds bounds.
func TestMission_Validate_Reflection(t *testing.T) {
	cases := []struct {
		name      string
		maxRounds int
		wantErr   bool
		errFrag   string
	}{
		{"zero_allowed", 0, false, ""},
		{"one", 1, false, ""},
		{"ten", 10, false, ""},
		{"eleven_invalid", 11, true, "reflection.max_rounds"},
		{"negative_invalid", -1, true, "reflection.max_rounds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validMission()
			m.Reflection = &MissionReflection{Enabled: true, MaxRounds: tc.maxRounds}
			err := m.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errFrag != "" && !strings.Contains(err.Error(), tc.errFrag) {
					t.Errorf("expected error to contain %q, got: %v", tc.errFrag, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected valid, got: %v", err)
				}
			}
		})
	}
}

// TestMission_Validate_SuccessCriteria_IDSlug tests checklist ID slug format validation.
func TestMission_Validate_SuccessCriteria_IDSlug(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid_simple", "task-done", false},
		{"valid_underscore", "task_done", false},
		{"valid_digits", "step1", false},
		{"invalid_uppercase", "Task-Done", true},
		{"invalid_spaces", "task done", true},
		{"invalid_dot", "task.done", true},
		{"invalid_empty", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validMission()
			m.SuccessCriteria = &MissionSuccessCriteria{
				Checklist: []SuccessCriterionItem{
					{ID: tc.id, Description: "some description"},
				},
			}
			err := m.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for id %q, got nil", tc.id)
				}
			} else {
				if err != nil {
					t.Errorf("expected valid id %q, got: %v", tc.id, err)
				}
			}
		})
	}
}

// TestMission_Validate_SuccessCriteria_DuplicateID tests duplicate checklist ID detection.
func TestMission_Validate_SuccessCriteria_DuplicateID(t *testing.T) {
	m := validMission()
	m.SuccessCriteria = &MissionSuccessCriteria{
		Checklist: []SuccessCriterionItem{
			{ID: "step-1", Description: "first"},
			{ID: "step-1", Description: "duplicate"},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate ID, got nil")
	}
	if !strings.Contains(err.Error(), "duplicated") {
		t.Errorf("expected 'duplicated' in error, got: %v", err)
	}
}

// TestMission_Validate_SuccessCriteria_EmptyDescription tests that empty description is rejected.
func TestMission_Validate_SuccessCriteria_EmptyDescription(t *testing.T) {
	m := validMission()
	m.SuccessCriteria = &MissionSuccessCriteria{
		Checklist: []SuccessCriterionItem{
			{ID: "step-1", Description: ""},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for empty description, got nil")
	}
	if !strings.Contains(err.Error(), "empty description") {
		t.Errorf("expected 'empty description' in error, got: %v", err)
	}
}

// TestMission_Validate_SuccessCriteria_EvaluationMode tests evaluation mode values.
func TestMission_Validate_SuccessCriteria_EvaluationMode(t *testing.T) {
	cases := []struct {
		mode    string
		wantErr bool
	}{
		{"", false},
		{"llm", false},
		{"checklist_only", false},
		{"invalid", true},
		{"LLM", true},
	}
	for _, tc := range cases {
		t.Run("mode_"+tc.mode, func(t *testing.T) {
			m := validMission()
			m.SuccessCriteria = &MissionSuccessCriteria{
				Evaluation: &SuccessEvaluation{Mode: tc.mode},
			}
			err := m.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for mode %q, got nil", tc.mode)
				}
			} else {
				if err != nil {
					t.Errorf("expected valid mode %q, got: %v", tc.mode, err)
				}
			}
		})
	}
}

// TestMission_Validate_SuccessCriteria_OnFailure tests on_failure values.
func TestMission_Validate_SuccessCriteria_OnFailure(t *testing.T) {
	cases := []struct {
		onFailure string
		wantErr   bool
	}{
		{"", false},
		{"flag", false},
		{"retry", false},
		{"block", false},
		{"skip", true},
		{"abort", true},
	}
	for _, tc := range cases {
		t.Run("on_failure_"+tc.onFailure, func(t *testing.T) {
			m := validMission()
			m.SuccessCriteria = &MissionSuccessCriteria{
				Evaluation: &SuccessEvaluation{OnFailure: tc.onFailure},
			}
			err := m.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for on_failure %q, got nil", tc.onFailure)
				}
			} else {
				if err != nil {
					t.Errorf("expected valid on_failure %q, got: %v", tc.onFailure, err)
				}
			}
		})
	}
}

// TestMission_Validate_SuccessCriteria_MaxRetries tests max_retries non-negative constraint.
func TestMission_Validate_SuccessCriteria_MaxRetries(t *testing.T) {
	t.Run("zero_valid", func(t *testing.T) {
		m := validMission()
		m.SuccessCriteria = &MissionSuccessCriteria{
			Evaluation: &SuccessEvaluation{MaxRetries: 0},
		}
		if err := m.Validate(); err != nil {
			t.Errorf("expected valid, got: %v", err)
		}
	})
	t.Run("negative_invalid", func(t *testing.T) {
		m := validMission()
		m.SuccessCriteria = &MissionSuccessCriteria{
			Evaluation: &SuccessEvaluation{MaxRetries: -1},
		}
		err := m.Validate()
		if err == nil {
			t.Fatal("expected error for negative max_retries, got nil")
		}
		if !strings.Contains(err.Error(), "max_retries") {
			t.Errorf("expected 'max_retries' in error, got: %v", err)
		}
	})
}

// TestMission_Validate_Fallback_TriggerTypes tests valid and invalid fallback trigger types.
func TestMission_Validate_Fallback_TriggerTypes(t *testing.T) {
	validTriggers := []string{"tool_error", "capability_unavailable", "budget_warning", "consecutive_errors", "timeout", "no_progress"}
	for _, trigger := range validTriggers {
		t.Run("valid_"+trigger, func(t *testing.T) {
			m := validMission()
			m.Fallback = &MissionFallback{
				Policies: []FallbackPolicy{
					{Trigger: trigger, Strategy: []FallbackAction{{Action: "retry"}}},
				},
			}
			if err := m.Validate(); err != nil {
				t.Errorf("expected trigger %q to be valid, got: %v", trigger, err)
			}
		})
	}

	invalidTriggers := []string{"on_error", "fail", "critical", "TOOL_ERROR"}
	for _, trigger := range invalidTriggers {
		t.Run("invalid_"+trigger, func(t *testing.T) {
			m := validMission()
			m.Fallback = &MissionFallback{
				Policies: []FallbackPolicy{
					{Trigger: trigger, Strategy: []FallbackAction{{Action: "retry"}}},
				},
			}
			err := m.Validate()
			if err == nil {
				t.Fatalf("expected error for trigger %q, got nil", trigger)
			}
			if !strings.Contains(err.Error(), "trigger") {
				t.Errorf("expected 'trigger' in error, got: %v", err)
			}
		})
	}
}

// TestMission_Validate_Fallback_ActionTypes tests valid and invalid fallback action types.
func TestMission_Validate_Fallback_ActionTypes(t *testing.T) {
	validActions := []string{"retry", "alternative_tool", "degrade", "simplify", "complete_partial", "pause_and_assess", "escalate"}
	for _, action := range validActions {
		t.Run("valid_"+action, func(t *testing.T) {
			m := validMission()
			m.Fallback = &MissionFallback{
				Policies: []FallbackPolicy{
					{Trigger: "tool_error", Strategy: []FallbackAction{{Action: action}}},
				},
			}
			if err := m.Validate(); err != nil {
				t.Errorf("expected action %q to be valid, got: %v", action, err)
			}
		})
	}

	t.Run("invalid_action", func(t *testing.T) {
		m := validMission()
		m.Fallback = &MissionFallback{
			Policies: []FallbackPolicy{
				{Trigger: "tool_error", Strategy: []FallbackAction{{Action: "give_up"}}},
			},
		}
		err := m.Validate()
		if err == nil {
			t.Fatal("expected error for invalid action, got nil")
		}
		if !strings.Contains(err.Error(), "action") {
			t.Errorf("expected 'action' in error, got: %v", err)
		}
	})
}

// TestMission_Validate_Fallback_EmptyStrategy tests that empty strategy is rejected.
func TestMission_Validate_Fallback_EmptyStrategy(t *testing.T) {
	m := validMission()
	m.Fallback = &MissionFallback{
		Policies: []FallbackPolicy{
			{Trigger: "tool_error", Strategy: []FallbackAction{}},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for empty strategy, got nil")
	}
	if !strings.Contains(err.Error(), "strategy") {
		t.Errorf("expected 'strategy' in error, got: %v", err)
	}
}

// TestMission_Validate_Fallback_EscalateMustBeLast tests that escalate must be the final step.
func TestMission_Validate_Fallback_EscalateMustBeLast(t *testing.T) {
	t.Run("escalate_last_valid", func(t *testing.T) {
		m := validMission()
		m.Fallback = &MissionFallback{
			Policies: []FallbackPolicy{
				{Trigger: "tool_error", Strategy: []FallbackAction{
					{Action: "retry"},
					{Action: "escalate"},
				}},
			},
		}
		if err := m.Validate(); err != nil {
			t.Errorf("expected valid, got: %v", err)
		}
	})

	t.Run("escalate_not_last_invalid", func(t *testing.T) {
		m := validMission()
		m.Fallback = &MissionFallback{
			Policies: []FallbackPolicy{
				{Trigger: "tool_error", Strategy: []FallbackAction{
					{Action: "escalate"},
					{Action: "retry"},
				}},
			},
		}
		err := m.Validate()
		if err == nil {
			t.Fatal("expected error for escalate not last, got nil")
		}
		if !strings.Contains(err.Error(), "escalate") {
			t.Errorf("expected 'escalate' in error, got: %v", err)
		}
	})
}

// TestMission_Validate_CostMode tests cost_mode values.
func TestMission_Validate_CostMode(t *testing.T) {
	cases := []struct {
		value   string
		wantErr bool
	}{
		{"", false},
		{"frugal", false},
		{"balanced", false},
		{"thorough", false},
		{"cheap", true},
		{"expensive", true},
		{"FRUGAL", true},
	}
	for _, tc := range cases {
		t.Run("cost_mode_"+tc.value, func(t *testing.T) {
			m := validMission()
			m.CostMode = tc.value
			err := m.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for cost_mode %q, got nil", tc.value)
				}
				if !strings.Contains(err.Error(), "cost_mode") {
					t.Errorf("expected 'cost_mode' in error, got: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("expected valid cost_mode %q, got: %v", tc.value, err)
				}
			}
		})
	}
}

// TestMission_Validate_MinTaskTier tests min_task_tier values.
func TestMission_Validate_MinTaskTier(t *testing.T) {
	cases := []struct {
		value   string
		wantErr bool
	}{
		{"", false},
		{"minimal", false},
		{"standard", false},
		{"full", false},
		{"basic", true},
		{"heavy", true},
		{"MINIMAL", true},
	}
	for _, tc := range cases {
		t.Run("min_task_tier_"+tc.value, func(t *testing.T) {
			m := validMission()
			m.MinTaskTier = tc.value
			err := m.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for min_task_tier %q, got nil", tc.value)
				}
				if !strings.Contains(err.Error(), "min_task_tier") {
					t.Errorf("expected 'min_task_tier' in error, got: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("expected valid min_task_tier %q, got: %v", tc.value, err)
				}
			}
		})
	}
}

// TestMission_Validate_AllNewFields_Valid tests a mission with all new fields set to valid values.
func TestMission_Validate_AllNewFields_Valid(t *testing.T) {
	m := validMission()
	m.Reflection = &MissionReflection{Enabled: true, MaxRounds: 5, Criteria: []string{"accuracy"}}
	m.SuccessCriteria = &MissionSuccessCriteria{
		Checklist: []SuccessCriterionItem{
			{ID: "step-1", Description: "Task completed", Required: true},
		},
		Evaluation: &SuccessEvaluation{
			Enabled:            true,
			Mode:               "llm",
			OnFailure:          "flag",
			MaxRetries:         1,
			ChecklistThreshold: 0.9,
		},
	}
	m.Fallback = &MissionFallback{
		Policies: []FallbackPolicy{
			{
				Trigger: "tool_error",
				Strategy: []FallbackAction{
					{Action: "retry", MaxAttempts: 2},
					{Action: "escalate", Severity: "warning"},
				},
			},
		},
	}
	m.ProceduralMemory = &MissionProceduralMemory{Capture: true, Retrieve: true, MaxRetrieved: 5}
	m.EpisodicMemory = &MissionEpisodicMemory{Capture: true, Retrieve: true, RetentionDays: 30}
	m.CostMode = "balanced"
	m.MinTaskTier = "standard"

	if err := m.Validate(); err != nil {
		t.Errorf("expected fully populated mission to be valid, got: %v", err)
	}
}
