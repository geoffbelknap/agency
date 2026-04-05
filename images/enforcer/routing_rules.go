package main

// RoutingRule defines a condition → tier reroute.
type RoutingRule struct {
	Match  RuleMatch `yaml:"match"`
	Tier   string    `yaml:"tier"`
	Reason string    `yaml:"reason,omitempty"`
}

type RuleMatch struct {
	CostSource         string `yaml:"cost_source,omitempty"`
	LastMessageRole    string `yaml:"last_message_role,omitempty"`
	ToolOutputTokensGT *int   `yaml:"tool_output_tokens_gt,omitempty"`
	HasToolCalls       *bool  `yaml:"has_tool_calls,omitempty"`
	StepIndexGT        *int   `yaml:"step_index_gt,omitempty"`
	ModelTierHint      string `yaml:"model_override,omitempty"`
}

type RuleContext struct {
	CostSource          string
	LastMessageRole     string
	ToolOutputTokensEst int
	HasToolCalls        bool
	StepIndex           int
	ModelTierHint       string
}

func evaluateRules(rules []RoutingRule, ctx RuleContext) (string, bool) {
	for _, rule := range rules {
		if matchesRule(rule.Match, ctx) {
			return rule.Tier, true
		}
	}
	return "", false
}

func matchesRule(m RuleMatch, ctx RuleContext) bool {
	if m.CostSource != "" && m.CostSource != ctx.CostSource {
		return false
	}
	if m.LastMessageRole != "" && m.LastMessageRole != ctx.LastMessageRole {
		return false
	}
	if m.ToolOutputTokensGT != nil && ctx.ToolOutputTokensEst <= *m.ToolOutputTokensGT {
		return false
	}
	if m.HasToolCalls != nil && *m.HasToolCalls != ctx.HasToolCalls {
		return false
	}
	if m.StepIndexGT != nil && ctx.StepIndex <= *m.StepIndexGT {
		return false
	}
	if m.ModelTierHint != "" && m.ModelTierHint != ctx.ModelTierHint {
		return false
	}
	return true
}

func buildRuleContext(reqBody map[string]interface{}, costSource string, stepIndex int, modelTierHint string) RuleContext {
	ctx := RuleContext{
		CostSource:    costSource,
		StepIndex:     stepIndex,
		ModelTierHint: modelTierHint,
	}
	if tools, ok := reqBody["tools"].([]interface{}); ok && len(tools) > 0 {
		ctx.HasToolCalls = true
	}
	if messages, ok := reqBody["messages"].([]interface{}); ok && len(messages) > 0 {
		last, ok := messages[len(messages)-1].(map[string]interface{})
		if ok {
			ctx.LastMessageRole, _ = last["role"].(string)
			if ctx.LastMessageRole == "tool" {
				content, _ := last["content"].(string)
				ctx.ToolOutputTokensEst = len(content) / 4
			}
		}
	}
	return ctx
}
