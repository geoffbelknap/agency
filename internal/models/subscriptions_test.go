// agency-gateway/internal/models/subscriptions_test.go
package models

import (
	"strings"
	"testing"
)

// TestExpertiseDeclaration_TooManyKeywords verifies that more than 30 keywords returns an error.
func TestExpertiseDeclaration_TooManyKeywords(t *testing.T) {
	kws := make([]string, 31)
	for i := range kws {
		kws[i] = "keyword"
	}
	e := &ExpertiseDeclaration{
		Tier:     ExpertiseTierBase,
		Keywords: kws,
	}
	if err := e.Validate(); err == nil {
		t.Fatal("expected error for > 30 keywords, got nil")
	}
}

// TestExpertiseDeclaration_ShortKeywordFiltering verifies that keywords shorter than 3 chars are removed.
func TestExpertiseDeclaration_ShortKeywordFiltering(t *testing.T) {
	e := &ExpertiseDeclaration{
		Tier:     ExpertiseTierBase,
		Keywords: []string{"go", "ab", "python", "x", "rust"},
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "go", "ab", "x" are < 3 chars and should be filtered
	if len(e.Keywords) != 2 {
		t.Errorf("expected 2 keywords after filtering, got %d: %v", len(e.Keywords), e.Keywords)
	}
	for _, kw := range e.Keywords {
		if len(kw) < 3 {
			t.Errorf("keyword %q should have been filtered (len < 3)", kw)
		}
	}
}

// TestExpertiseDeclaration_ExactLimit verifies that exactly 30 keywords is valid.
func TestExpertiseDeclaration_ExactLimit(t *testing.T) {
	kws := make([]string, 30)
	for i := range kws {
		kws[i] = "key"
	}
	e := &ExpertiseDeclaration{
		Tier:     ExpertiseTierStanding,
		Keywords: kws,
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("expected no error for exactly 30 keywords, got: %v", err)
	}
}

// TestInterestDeclaration_TooManyKeywords verifies that more than 20 keywords returns an error.
func TestInterestDeclaration_TooManyKeywords(t *testing.T) {
	kws := make([]string, 21)
	for i := range kws {
		kws[i] = "keyword"
	}
	d := &InterestDeclaration{
		TaskID:   "task-1",
		Keywords: kws,
	}
	err := d.Validate()
	if err == nil {
		t.Fatal("expected error for > 20 keywords, got nil")
	}
	if !strings.Contains(err.Error(), "20") {
		t.Errorf("error message should mention 20, got: %v", err)
	}
}

// TestInterestDeclaration_ShortKeywordFiltering verifies that keywords shorter than 3 chars are removed.
func TestInterestDeclaration_ShortKeywordFiltering(t *testing.T) {
	d := &InterestDeclaration{
		TaskID:   "task-1",
		Keywords: []string{"ai", "go", "machine-learning", "ml", "nlp"},
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "ai", "go", "ml" are < 3 chars; "nlp" is exactly 3 and should be kept
	if len(d.Keywords) != 2 {
		t.Errorf("expected 2 keywords after filtering, got %d: %v", len(d.Keywords), d.Keywords)
	}
}

// TestInterestDeclaration_KnowledgeFilterLimit verifies that more than 10 total entries returns an error.
func TestInterestDeclaration_KnowledgeFilterLimit(t *testing.T) {
	d := &InterestDeclaration{
		TaskID: "task-1",
		KnowledgeFilter: map[string][]string{
			"domain-a": {"e1", "e2", "e3", "e4", "e5"},
			"domain-b": {"e6", "e7", "e8", "e9", "e10", "e11"},
		},
	}
	if err := d.Validate(); err == nil {
		t.Fatal("expected error for > 10 knowledge filter entries, got nil")
	}
}

// TestInterestDeclaration_KnowledgeFilterExactLimit verifies that exactly 10 entries is valid.
func TestInterestDeclaration_KnowledgeFilterExactLimit(t *testing.T) {
	d := &InterestDeclaration{
		TaskID: "task-1",
		KnowledgeFilter: map[string][]string{
			"domain-a": {"e1", "e2", "e3", "e4", "e5"},
			"domain-b": {"e6", "e7", "e8", "e9", "e10"},
		},
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("expected no error for exactly 10 knowledge filter entries, got: %v", err)
	}
}

// TestDefaultCommsPolicy_FourRules verifies that DefaultCommsPolicy returns exactly 4 rules.
func TestDefaultCommsPolicy_FourRules(t *testing.T) {
	p := DefaultCommsPolicy()
	if len(p.Rules) != 4 {
		t.Fatalf("expected 4 rules, got %d", len(p.Rules))
	}
}

// TestDefaultCommsPolicy_RuleContents verifies the content of the default rules.
func TestDefaultCommsPolicy_RuleContents(t *testing.T) {
	p := DefaultCommsPolicy()

	// Rule 0: direct + urgent/blocker flags → interrupt
	r0 := p.Rules[0]
	if r0.Match != MatchClassificationDirect {
		t.Errorf("rule 0 match: expected %q, got %q", MatchClassificationDirect, r0.Match)
	}
	if r0.Action != "interrupt" {
		t.Errorf("rule 0 action: expected %q, got %q", "interrupt", r0.Action)
	}
	if len(r0.Flags) != 2 {
		t.Errorf("rule 0 flags: expected 2, got %d: %v", len(r0.Flags), r0.Flags)
	}

	// Rule 1: direct → notify_at_pause
	r1 := p.Rules[1]
	if r1.Match != MatchClassificationDirect {
		t.Errorf("rule 1 match: expected %q, got %q", MatchClassificationDirect, r1.Match)
	}
	if r1.Action != "notify_at_pause" {
		t.Errorf("rule 1 action: expected %q, got %q", "notify_at_pause", r1.Action)
	}

	// Rule 2: interest_match → notify_at_pause
	r2 := p.Rules[2]
	if r2.Match != MatchClassificationInterestMatch {
		t.Errorf("rule 2 match: expected %q, got %q", MatchClassificationInterestMatch, r2.Match)
	}
	if r2.Action != "notify_at_pause" {
		t.Errorf("rule 2 action: expected %q, got %q", "notify_at_pause", r2.Action)
	}

	// Rule 3: ambient → queue
	r3 := p.Rules[3]
	if r3.Match != MatchClassificationAmbient {
		t.Errorf("rule 3 match: expected %q, got %q", MatchClassificationAmbient, r3.Match)
	}
	if r3.Action != "queue" {
		t.Errorf("rule 3 action: expected %q, got %q", "queue", r3.Action)
	}
}

// TestDefaultCommsPolicy_Defaults verifies numeric and string defaults on CommsPolicy.
func TestDefaultCommsPolicy_Defaults(t *testing.T) {
	p := DefaultCommsPolicy()
	if p.MaxInterruptsPerTask != 3 {
		t.Errorf("expected MaxInterruptsPerTask=3, got %d", p.MaxInterruptsPerTask)
	}
	if p.CooldownSeconds != 60 {
		t.Errorf("expected CooldownSeconds=60, got %d", p.CooldownSeconds)
	}
	if p.IdleAction != "queue" {
		t.Errorf("expected IdleAction=%q, got %q", "queue", p.IdleAction)
	}
	if p.CircuitBreakerMinActionRate != 0.2 {
		t.Errorf("expected CircuitBreakerMinActionRate=0.2, got %f", p.CircuitBreakerMinActionRate)
	}
	if p.CircuitBreakerWindowSize != 20 {
		t.Errorf("expected CircuitBreakerWindowSize=20, got %d", p.CircuitBreakerWindowSize)
	}
}

// TestWSEvent_Struct verifies that WSEvent can be constructed and defaults correctly.
func TestWSEvent_Struct(t *testing.T) {
	eventType := "message"
	channel := "general"
	ev := WSEvent{
		V:       1,
		Type:    eventType,
		Channel: &channel,
	}
	if ev.V != 1 {
		t.Errorf("expected V=1, got %d", ev.V)
	}
	if ev.Type != eventType {
		t.Errorf("expected Type=%q, got %q", eventType, ev.Type)
	}
	if ev.Channel == nil || *ev.Channel != channel {
		t.Errorf("expected Channel=%q, got %v", channel, ev.Channel)
	}
	if ev.Match != nil {
		t.Errorf("expected Match=nil, got %v", ev.Match)
	}
	if ev.Message != nil {
		t.Errorf("expected Message=nil, got %v", ev.Message)
	}
}

// TestWSEvent_WithData verifies WSEvent with generic data maps.
func TestWSEvent_WithData(t *testing.T) {
	ev := WSEvent{
		V:    1,
		Type: "task_update",
		Task: map[string]interface{}{
			"id":     "task-42",
			"status": "running",
		},
		Data: map[string]interface{}{
			"progress": 75,
		},
	}
	if ev.Task["id"] != "task-42" {
		t.Errorf("expected task id=task-42, got %v", ev.Task["id"])
	}
	if ev.Data["progress"] != 75 {
		t.Errorf("expected progress=75, got %v", ev.Data["progress"])
	}
}
