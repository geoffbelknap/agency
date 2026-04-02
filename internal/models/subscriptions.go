// agency-gateway/internal/models/subscriptions.go
package models

import "fmt"

// MatchClassification constants for subscription matching.
const (
	MatchClassificationDirect        = "direct"
	MatchClassificationInterestMatch = "interest_match"
	MatchClassificationAmbient       = "ambient"
)

// ExpertiseTier constants for expertise declaration tiers.
const (
	ExpertiseTierBase     = "base"
	ExpertiseTierStanding = "standing"
	ExpertiseTierLearned  = "learned"
	ExpertiseTierTask     = "task"
)

// ExpertiseDeclaration declares an agent's expertise in a domain.
type ExpertiseDeclaration struct {
	Tier        string   `yaml:"tier" json:"tier"`
	Description string   `yaml:"description" json:"description"`
	Keywords    []string `yaml:"keywords" json:"keywords"`
	Persistent  bool     `yaml:"persistent" json:"persistent"`
}

// Validate checks keyword count and filters short keywords (< 3 chars).
func (e *ExpertiseDeclaration) Validate() error {
	if len(e.Keywords) > 30 {
		return fmt.Errorf("expertise declarations support at most 30 keywords")
	}
	filtered := e.Keywords[:0]
	for _, kw := range e.Keywords {
		if len(kw) >= 3 {
			filtered = append(filtered, kw)
		}
	}
	e.Keywords = filtered
	return nil
}

// InterestDeclaration declares an agent's interest in a topic for a task.
type InterestDeclaration struct {
	TaskID          string              `yaml:"task_id" json:"task_id"`
	Description     string              `yaml:"description" json:"description"`
	Keywords        []string            `yaml:"keywords" json:"keywords"`
	KnowledgeFilter map[string][]string `yaml:"knowledge_filter" json:"knowledge_filter"`
}

// Validate checks keyword count, filters short keywords, and checks knowledge_filter total.
func (d *InterestDeclaration) Validate() error {
	if len(d.Keywords) > 20 {
		return fmt.Errorf("interest declarations support at most 20 keywords")
	}
	filtered := d.Keywords[:0]
	for _, kw := range d.Keywords {
		if len(kw) >= 3 {
			filtered = append(filtered, kw)
		}
	}
	d.Keywords = filtered

	total := 0
	for _, entries := range d.KnowledgeFilter {
		total += len(entries)
	}
	if total > 10 {
		return fmt.Errorf("knowledge filter supports at most 10 entries")
	}
	return nil
}

// WSEvent is a WebSocket event envelope sent to subscribed clients.
type WSEvent struct {
	V               int                    `yaml:"v" json:"v"`
	Type            string                 `yaml:"type" json:"type"`
	Channel         *string                `yaml:"channel" json:"channel,omitempty"`
	Match           *string                `yaml:"match" json:"match,omitempty"`
	MatchedKeywords []string               `yaml:"matched_keywords" json:"matched_keywords,omitempty"`
	Message         map[string]interface{} `yaml:"message" json:"message,omitempty"`
	Task            map[string]interface{} `yaml:"task" json:"task,omitempty"`
	Event           *string                `yaml:"event" json:"event,omitempty"`
	Data            map[string]interface{} `yaml:"data" json:"data,omitempty"`
}

// InterruptionRule defines how a comms match classification is handled.
type InterruptionRule struct {
	Match  string   `yaml:"match" json:"match"`
	Flags  []string `yaml:"flags" json:"flags"`
	Action string   `yaml:"action" json:"action"`
}

// CommsPolicy defines how incoming messages interrupt or queue for an agent.
type CommsPolicy struct {
	Rules                       []InterruptionRule `yaml:"rules" json:"rules"`
	MaxInterruptsPerTask        int                `yaml:"max_interrupts_per_task" json:"max_interrupts_per_task"`
	CooldownSeconds             int                `yaml:"cooldown_seconds" json:"cooldown_seconds"`
	IdleAction                  string             `yaml:"idle_action" json:"idle_action"`
	CircuitBreakerMinActionRate float64            `yaml:"circuit_breaker_min_action_rate" json:"circuit_breaker_min_action_rate"`
	CircuitBreakerWindowSize    int                `yaml:"circuit_breaker_window_size" json:"circuit_breaker_window_size"`
}

// DefaultCommsPolicy returns a CommsPolicy with the standard four-rule default.
func DefaultCommsPolicy() CommsPolicy {
	return CommsPolicy{
		Rules: []InterruptionRule{
			{Match: MatchClassificationDirect, Flags: []string{"urgent", "blocker"}, Action: "interrupt"},
			{Match: MatchClassificationDirect, Flags: []string{}, Action: "notify_at_pause"},
			{Match: MatchClassificationInterestMatch, Flags: []string{}, Action: "notify_at_pause"},
			{Match: MatchClassificationAmbient, Flags: []string{}, Action: "queue"},
		},
		MaxInterruptsPerTask:        3,
		CooldownSeconds:             60,
		IdleAction:                  "queue",
		CircuitBreakerMinActionRate: 0.2,
		CircuitBreakerWindowSize:    20,
	}
}
