package evaluation

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type CriterionItem struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type CriterionResult struct {
	ID        string `json:"id"`
	Passed    bool   `json:"passed"`
	Required  bool   `json:"required"`
	Reasoning string `json:"reasoning"`
}

type EvaluationResult struct {
	Passed          bool              `json:"passed"`
	CriteriaResults []CriterionResult `json:"criteria_results"`
	EvaluationMode  string            `json:"evaluation_mode"`
	ModelUsed       string            `json:"model_used,omitempty"`
	TokensUsed      int               `json:"tokens_used,omitempty"`
	EvaluatedAt     time.Time         `json:"evaluated_at"`
}

var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "to": true,
	"of": true, "in": true, "for": true, "and": true, "or": true,
	"with": true, "on": true, "at": true, "by": true, "from": true,
	"that": true, "this": true, "it": true, "not": true, "but": true,
	"if": true, "has": true, "have": true, "had": true, "do": true,
	"does": true, "did": true, "will": true, "would": true, "could": true,
	"should": true, "may": true, "might": true, "must": true, "shall": true,
	"can": true, "need": true, "each": true, "every": true, "all": true,
	"any": true, "both": true, "few": true, "more": true, "most": true,
	"other": true, "some": true, "such": true, "no": true, "only": true,
	"own": true, "same": true, "so": true, "than": true, "too": true,
	"very": true, "just": true, "because": true, "as": true, "until": true,
	"while": true, "about": true, "between": true, "through": true,
	"during": true, "before": true, "after": true, "above": true,
	"below": true, "up": true, "down": true, "out": true, "off": true,
	"over": true, "under": true, "again": true, "further": true, "then": true,
	"once": true, "here": true, "there": true, "when": true, "where": true,
	"why": true, "how": true, "what": true, "which": true, "who": true,
	"whom": true, "these": true, "those": true, "am": true, "being": true,
	"having": true, "doing": true,
}

func tokenize(text string) map[string]bool {
	tokens := make(map[string]bool)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		word = strings.Trim(word, ".,;:!?\"'()[]{}/-_")
		if len(word) > 1 && !stopWords[word] {
			tokens[word] = true
		}
	}
	return tokens
}

func jaccardSimilarity(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// EvaluateChecklist performs keyword-based evaluation without an LLM call.
// A criterion passes if the Jaccard similarity between its description tokens
// and the summary tokens meets or exceeds the threshold.
// The overall result passes if all required criteria pass.
func EvaluateChecklist(summary string, criteria []CriterionItem, threshold float64) EvaluationResult {
	summaryTokens := tokenize(summary)
	allRequiredPassed := true
	results := make([]CriterionResult, 0, len(criteria))

	for _, c := range criteria {
		criterionTokens := tokenize(c.Description)
		sim := jaccardSimilarity(summaryTokens, criterionTokens)
		passed := sim >= threshold

		reasoning := fmt.Sprintf("Jaccard similarity %.3f (threshold %.3f)", sim, threshold)
		results = append(results, CriterionResult{
			ID:        c.ID,
			Passed:    passed,
			Required:  c.Required,
			Reasoning: reasoning,
		})

		if c.Required && !passed {
			allRequiredPassed = false
		}
	}

	return EvaluationResult{
		Passed:          allRequiredPassed,
		CriteriaResults: results,
		EvaluationMode:  "checklist_only",
		EvaluatedAt:     time.Now(),
	}
}

const evaluationPromptTemplate = `You are an evaluation assistant. Your job is to assess whether a task completion summary satisfies a set of success criteria.

## Success Criteria

%s

## Task Completion Summary

%s

## Instructions

For each criterion, determine whether the task summary demonstrates that the criterion has been satisfied. Assess based on evidence in the summary, not assumptions about what the agent might have done.

Respond with JSON only. No explanation outside the JSON.

{
  "criteria_results": [
    {
      "id": "<criterion_id>",
      "passed": true|false,
      "reasoning": "<one sentence explaining your assessment>"
    }
  ]
}`

// BuildEvaluationPrompt constructs the LLM prompt for evaluating a task summary
// against the given criteria.
func BuildEvaluationPrompt(summary string, criteria []CriterionItem) string {
	var criteriaLines []string
	for _, c := range criteria {
		req := "required"
		if !c.Required {
			req = "optional"
		}
		criteriaLines = append(criteriaLines, fmt.Sprintf("- [%s] %s (%s)", c.ID, c.Description, req))
	}
	return fmt.Sprintf(evaluationPromptTemplate, strings.Join(criteriaLines, "\n"), summary)
}

// ParseEvaluationResponse parses the LLM JSON response and builds an EvaluationResult.
// The required flag for each criterion is populated from the criteria list.
// The overall result passes if all required criteria pass.
func ParseEvaluationResponse(responseJSON string, criteria []CriterionItem) (EvaluationResult, error) {
	// Strip markdown code fences — LLMs often wrap JSON in ```json ... ```
	cleaned := strings.TrimSpace(responseJSON)
	if strings.HasPrefix(cleaned, "```") {
		// Remove opening fence (```json or ```)
		if idx := strings.Index(cleaned, "\n"); idx >= 0 {
			cleaned = cleaned[idx+1:]
		}
		// Remove closing fence
		if idx := strings.LastIndex(cleaned, "```"); idx >= 0 {
			cleaned = cleaned[:idx]
		}
		cleaned = strings.TrimSpace(cleaned)
	}

	var parsed struct {
		CriteriaResults []CriterionResult `json:"criteria_results"`
	}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return EvaluationResult{}, fmt.Errorf("failed to parse evaluation response: %w", err)
	}

	// Build lookup for required flag
	requiredMap := make(map[string]bool)
	for _, c := range criteria {
		requiredMap[c.ID] = c.Required
	}

	allRequiredPassed := true
	for i, cr := range parsed.CriteriaResults {
		if req, ok := requiredMap[cr.ID]; ok {
			parsed.CriteriaResults[i].Required = req
			if req && !cr.Passed {
				allRequiredPassed = false
			}
		}
	}

	return EvaluationResult{
		Passed:          allRequiredPassed,
		CriteriaResults: parsed.CriteriaResults,
		EvaluationMode:  "llm",
		EvaluatedAt:     time.Now(),
	}, nil
}
