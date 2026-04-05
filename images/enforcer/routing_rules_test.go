package main

import "testing"

func TestEvaluateRules_CostSource(t *testing.T) {
	rules := []RoutingRule{
		{Match: RuleMatch{CostSource: "reflection"}, Tier: "fast"},
		{Match: RuleMatch{CostSource: "memory_capture"}, Tier: "mini"},
	}
	tier, matched := evaluateRules(rules, RuleContext{CostSource: "reflection"})
	if !matched || tier != "fast" {
		t.Errorf("expected fast, got %s", tier)
	}
	_, matched = evaluateRules(rules, RuleContext{CostSource: "agent_task"})
	if matched {
		t.Error("expected no match")
	}
}

func TestEvaluateRules_StepIndex(t *testing.T) {
	rules := []RoutingRule{{Match: RuleMatch{StepIndexGT: intPtr(10)}, Tier: "fast"}}
	tier, matched := evaluateRules(rules, RuleContext{StepIndex: 15})
	if !matched || tier != "fast" {
		t.Errorf("expected fast")
	}
	_, matched = evaluateRules(rules, RuleContext{StepIndex: 5})
	if matched {
		t.Error("expected no match")
	}
}

func TestEvaluateRules_ToolOutput(t *testing.T) {
	rules := []RoutingRule{{Match: RuleMatch{LastMessageRole: "tool", ToolOutputTokensGT: intPtr(5000)}, Tier: "fast"}}
	tier, matched := evaluateRules(rules, RuleContext{LastMessageRole: "tool", ToolOutputTokensEst: 8000})
	if !matched || tier != "fast" {
		t.Errorf("expected fast")
	}
}

func TestEvaluateRules_FirstMatchWins(t *testing.T) {
	rules := []RoutingRule{
		{Match: RuleMatch{CostSource: "reflection"}, Tier: "mini"},
		{Match: RuleMatch{CostSource: "reflection"}, Tier: "nano"},
	}
	tier, _ := evaluateRules(rules, RuleContext{CostSource: "reflection"})
	if tier != "mini" {
		t.Errorf("expected mini, got %s", tier)
	}
}

func TestEvaluateRules_HasToolCalls(t *testing.T) {
	yes := true
	rules := []RoutingRule{{Match: RuleMatch{HasToolCalls: &yes}, Tier: "standard"}}
	tier, matched := evaluateRules(rules, RuleContext{HasToolCalls: true})
	if !matched || tier != "standard" {
		t.Errorf("expected standard, got %s", tier)
	}
	_, matched = evaluateRules(rules, RuleContext{HasToolCalls: false})
	if matched {
		t.Error("expected no match when HasToolCalls is false")
	}
}

func TestEvaluateRules_NoRules(t *testing.T) {
	_, matched := evaluateRules(nil, RuleContext{CostSource: "anything"})
	if matched {
		t.Error("expected no match with empty rules")
	}
}

func TestBuildRuleContext(t *testing.T) {
	body := map[string]interface{}{
		"tools": []interface{}{map[string]interface{}{"type": "function"}},
		"messages": []interface{}{
			map[string]interface{}{"role": "tool", "content": "x"},
		},
	}
	ctx := buildRuleContext(body, "agent_task", 3, "fast")
	if !ctx.HasToolCalls {
		t.Error("expected HasToolCalls")
	}
	if ctx.LastMessageRole != "tool" {
		t.Error("expected tool role")
	}
	if ctx.StepIndex != 3 {
		t.Error("expected step 3")
	}
	if ctx.ModelTierHint != "fast" {
		t.Error("expected fast hint")
	}
}

func TestBuildRuleContext_NoToolsNoMessages(t *testing.T) {
	body := map[string]interface{}{}
	ctx := buildRuleContext(body, "reflection", 0, "")
	if ctx.HasToolCalls {
		t.Error("expected no tool calls")
	}
	if ctx.LastMessageRole != "" {
		t.Error("expected empty role")
	}
	if ctx.CostSource != "reflection" {
		t.Error("expected reflection cost source")
	}
}

func intPtr(i int) *int { return &i }
