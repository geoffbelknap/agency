package evaluation

import (
	"math"
	"strings"
	"testing"
)

// --- tokenize ---

func TestTokenize_Basic(t *testing.T) {
	tokens := tokenize("The quick brown fox")
	if tokens["the"] {
		t.Error("stop word 'the' should be removed")
	}
	if !tokens["quick"] || !tokens["brown"] || !tokens["fox"] {
		t.Error("content words should be present")
	}
}

func TestTokenize_Punctuation(t *testing.T) {
	tokens := tokenize("hello, world! (test)")
	if !tokens["hello"] || !tokens["world"] || !tokens["test"] {
		t.Error("punctuation should be stripped")
	}
}

func TestTokenize_SingleCharRemoved(t *testing.T) {
	tokens := tokenize("a b c word")
	if tokens["a"] || tokens["b"] || tokens["c"] {
		t.Error("single-char tokens should be removed")
	}
	if !tokens["word"] {
		t.Error("multi-char token should remain")
	}
}

// --- jaccardSimilarity ---

func TestJaccard_Identical(t *testing.T) {
	a := map[string]bool{"hello": true, "world": true}
	if math.Abs(jaccardSimilarity(a, a)-1.0) > 0.001 {
		t.Error("identical sets should have similarity 1.0")
	}
}

func TestJaccard_Disjoint(t *testing.T) {
	a := map[string]bool{"hello": true}
	b := map[string]bool{"world": true}
	if jaccardSimilarity(a, b) != 0.0 {
		t.Error("disjoint sets should have similarity 0.0")
	}
}

func TestJaccard_Partial(t *testing.T) {
	a := map[string]bool{"hello": true, "world": true}
	b := map[string]bool{"hello": true, "earth": true}
	// intersection=1, union=3
	expected := 1.0 / 3.0
	if math.Abs(jaccardSimilarity(a, b)-expected) > 0.001 {
		t.Errorf("expected %f, got %f", expected, jaccardSimilarity(a, b))
	}
}

func TestJaccard_BothEmpty(t *testing.T) {
	a := map[string]bool{}
	b := map[string]bool{}
	if jaccardSimilarity(a, b) != 1.0 {
		t.Error("both empty should return 1.0")
	}
}

// --- EvaluateChecklist ---

func TestChecklist_AllPass(t *testing.T) {
	criteria := []CriterionItem{
		{ID: "severity", Description: "Severity level assigned with justification", Required: true},
		{ID: "tagged", Description: "Responder tagged", Required: true},
	}
	result := EvaluateChecklist("Assigned P2 severity based on impact. Tagged @oncall-infra as responder.", criteria, 0.15)
	if !result.Passed {
		t.Error("expected overall pass")
	}
	if result.EvaluationMode != "checklist_only" {
		t.Errorf("expected checklist_only, got %s", result.EvaluationMode)
	}
}

func TestChecklist_RequiredFails(t *testing.T) {
	criteria := []CriterionItem{
		{ID: "escalation", Description: "P1 tickets escalated to operator immediately", Required: true},
	}
	result := EvaluateChecklist("Assigned P3, tagged team, posted to channel", criteria, 0.3)
	if result.Passed {
		t.Error("expected overall fail — required criterion not met")
	}
}

func TestChecklist_OptionalFailsStillPasses(t *testing.T) {
	criteria := []CriterionItem{
		{ID: "severity", Description: "Severity assigned", Required: true},
		{ID: "notes", Description: "Additional investigation notes provided", Required: false},
	}
	result := EvaluateChecklist("Assigned P2 severity level.", criteria, 0.2)
	if !result.Passed {
		t.Error("optional failure should not fail overall result")
	}
}

func TestChecklist_EmptySummary(t *testing.T) {
	criteria := []CriterionItem{
		{ID: "test", Description: "Something was done", Required: true},
	}
	result := EvaluateChecklist("", criteria, 0.3)
	if result.Passed {
		t.Error("empty summary should fail")
	}
}

func TestChecklist_CustomThreshold(t *testing.T) {
	criteria := []CriterionItem{
		{ID: "test", Description: "severity assigned", Required: true},
	}
	// With very low threshold, even vaguely related text should pass
	result := EvaluateChecklist("We assigned a severity rating", criteria, 0.1)
	if !result.Passed {
		t.Error("low threshold should be lenient")
	}
}

// --- BuildEvaluationPrompt ---

func TestBuildPrompt_ContainsCriteria(t *testing.T) {
	criteria := []CriterionItem{
		{ID: "sev", Description: "Severity assigned", Required: true},
		{ID: "tag", Description: "Responder tagged", Required: false},
	}
	prompt := BuildEvaluationPrompt("Task done", criteria)
	if !containsAll(prompt, "sev", "Severity assigned", "required", "tag", "optional", "Task done") {
		t.Error("prompt missing expected content")
	}
}

func TestBuildPrompt_ContainsJSONTemplate(t *testing.T) {
	prompt := BuildEvaluationPrompt("summary", []CriterionItem{{ID: "a", Description: "b"}})
	if !containsAll(prompt, "criteria_results", "passed", "reasoning") {
		t.Error("prompt should contain JSON response template")
	}
}

// --- ParseEvaluationResponse ---

func TestParseResponse_AllPass(t *testing.T) {
	resp := `{"criteria_results": [{"id": "sev", "passed": true, "reasoning": "Found"}, {"id": "tag", "passed": true, "reasoning": "Found"}]}`
	criteria := []CriterionItem{
		{ID: "sev", Required: true},
		{ID: "tag", Required: true},
	}
	result, err := ParseEvaluationResponse(resp, criteria)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected pass")
	}
	if result.EvaluationMode != "llm" {
		t.Error("expected llm mode")
	}
}

func TestParseResponse_RequiredFails(t *testing.T) {
	resp := `{"criteria_results": [{"id": "sev", "passed": false, "reasoning": "Not found"}]}`
	criteria := []CriterionItem{{ID: "sev", Required: true}}
	result, err := ParseEvaluationResponse(resp, criteria)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail — required criterion not met")
	}
}

func TestParseResponse_OptionalFail(t *testing.T) {
	resp := `{"criteria_results": [{"id": "notes", "passed": false, "reasoning": "Missing"}]}`
	criteria := []CriterionItem{{ID: "notes", Required: false}}
	result, err := ParseEvaluationResponse(resp, criteria)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("optional failure should not fail overall")
	}
}

func TestParseResponse_MalformedJSON(t *testing.T) {
	_, err := ParseEvaluationResponse("not json", nil)
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

// helper
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
