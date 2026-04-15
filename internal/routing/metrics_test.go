package routing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCollectEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := Collect(dir, MetricsQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Totals.Requests != 0 {
		t.Errorf("expected 0 requests, got %d", s.Totals.Requests)
	}
}

func TestCollectBasic(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")

	// Create enforcer audit log
	enfDir := filepath.Join(dir, "audit", "test-agent", "enforcer")
	os.MkdirAll(enfDir, 0755)

	now := time.Now().UTC()
	records := []auditRecord{
		{
			Timestamp:     now.Add(-1 * time.Hour).Format(time.RFC3339Nano),
			Type:          "LLM_DIRECT",
			Agent:         "test-agent",
			Source:        "enforcer",
			Model:         "claude-sonnet",
			ProviderModel: "claude-sonnet-4-20250514",
			Status:        200,
			DurationMs:    1200,
			InputTokens:   500,
			OutputTokens:  150,
		},
		{
			Timestamp:     now.Add(-30 * time.Minute).Format(time.RFC3339Nano),
			Type:          "LLM_DIRECT_STREAM",
			Agent:         "test-agent",
			Source:        "enforcer",
			Model:         "claude-sonnet",
			ProviderModel: "claude-sonnet-4-20250514",
			Status:        200,
			DurationMs:    3400,
			InputTokens:   2000,
			OutputTokens:  800,
		},
		{
			Timestamp:     now.Add(-10 * time.Minute).Format(time.RFC3339Nano),
			Type:          "LLM_DIRECT_ERROR",
			Agent:         "test-agent",
			Source:        "evaluator",
			Model:         "gpt-4o",
			ProviderModel: "gpt-4o",
			Status:        500,
			Error:         "upstream timeout",
			DurationMs:    30000,
			InputTokens:   0,
			OutputTokens:  0,
		},
		// Non-LLM record (should be skipped)
		{
			Timestamp: now.Add(-5 * time.Minute).Format(time.RFC3339Nano),
			Type:      "HTTP_REQUEST",
			Agent:     "test-agent",
			Status:    200,
		},
	}

	var lines []byte
	for _, rec := range records {
		b, _ := json.Marshal(rec)
		lines = append(lines, b...)
		lines = append(lines, '\n')
	}
	os.WriteFile(filepath.Join(enfDir, "enforcer-"+today+".jsonl"), lines, 0644)

	s, err := Collect(dir, MetricsQuery{})
	if err != nil {
		t.Fatal(err)
	}

	if s.Totals.Requests != 3 {
		t.Errorf("expected 3 requests, got %d", s.Totals.Requests)
	}
	if s.Totals.InputTokens != 2500 {
		t.Errorf("expected 2500 input tokens, got %d", s.Totals.InputTokens)
	}
	if s.Totals.OutputTokens != 950 {
		t.Errorf("expected 950 output tokens, got %d", s.Totals.OutputTokens)
	}
	if s.Totals.Errors != 1 {
		t.Errorf("expected 1 error, got %d", s.Totals.Errors)
	}

	// By model
	if cs, ok := s.ByModel["claude-sonnet"]; !ok {
		t.Error("missing claude-sonnet in by_model")
	} else if cs.Requests != 2 {
		t.Errorf("expected 2 claude-sonnet requests, got %d", cs.Requests)
	}

	// By provider
	if anth, ok := s.ByProvider["anthropic"]; !ok {
		t.Error("missing anthropic in by_provider")
	} else if anth.Requests != 2 {
		t.Errorf("expected 2 anthropic requests, got %d", anth.Requests)
	}
	if oai, ok := s.ByProvider["openai"]; !ok {
		t.Error("missing openai in by_provider")
	} else if oai.Errors != 1 {
		t.Errorf("expected 1 openai error, got %d", oai.Errors)
	}

	// Recent errors
	if len(s.RecentErrors) != 1 {
		t.Errorf("expected 1 recent error, got %d", len(s.RecentErrors))
	}

	// By source
	if enf, ok := s.BySource["enforcer"]; !ok {
		t.Error("missing enforcer in by_source")
	} else if enf.Requests != 2 {
		t.Errorf("expected 2 enforcer requests, got %d", enf.Requests)
	}
	if eval, ok := s.BySource["evaluator"]; !ok {
		t.Error("missing evaluator in by_source")
	} else if eval.Requests != 1 {
		t.Errorf("expected 1 evaluator request, got %d", eval.Requests)
	}

	// Latency stats
	if s.Totals.AvgLatencyMs == 0 {
		t.Error("expected non-zero avg latency")
	}
}

func TestCollectAgentFilter(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")
	now := time.Now().UTC()

	// Create logs for two agents
	for _, agent := range []string{"agent-a", "agent-b"} {
		enfDir := filepath.Join(dir, "audit", agent, "enforcer")
		os.MkdirAll(enfDir, 0755)
		rec := auditRecord{
			Timestamp:     now.Add(-1 * time.Hour).Format(time.RFC3339Nano),
			Type:          "LLM_DIRECT",
			Agent:         agent,
			Model:         "claude-sonnet",
			ProviderModel: "claude-sonnet-4-20250514",
			Status:        200,
			DurationMs:    1000,
			InputTokens:   100,
			OutputTokens:  50,
		}
		b, _ := json.Marshal(rec)
		b = append(b, '\n')
		os.WriteFile(filepath.Join(enfDir, "enforcer-"+today+".jsonl"), b, 0644)
	}

	// Filter to agent-a only
	s, err := Collect(dir, MetricsQuery{Agent: "agent-a"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Totals.Requests != 1 {
		t.Errorf("expected 1 request for agent-a, got %d", s.Totals.Requests)
	}
}

func TestCollectWithCostConfig(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")
	now := time.Now().UTC()

	enfDir := filepath.Join(dir, "audit", "agent-x", "enforcer")
	os.MkdirAll(enfDir, 0755)

	rec := auditRecord{
		Timestamp:     now.Add(-1 * time.Hour).Format(time.RFC3339Nano),
		Type:          "LLM_DIRECT",
		Agent:         "agent-x",
		Model:         "claude-sonnet",
		ProviderModel: "claude-sonnet-4-20250514",
		Status:        200,
		DurationMs:    2000,
		InputTokens:   1000000, // 1M tokens
		OutputTokens:  100000,  // 100K tokens
	}
	b, _ := json.Marshal(rec)
	b = append(b, '\n')
	os.WriteFile(filepath.Join(enfDir, "enforcer-"+today+".jsonl"), b, 0644)

	costs := map[string]ModelCost{
		"claude-sonnet": {CostPerMTokIn: 3.0, CostPerMTokOut: 15.0},
	}

	s, err := CollectWithCosts(dir, MetricsQuery{}, costs)
	if err != nil {
		t.Fatal(err)
	}

	// Cost = (1M * 3.0 + 100K * 15.0) / 1M = 3.0 + 1.5 = 4.5
	if s.Totals.EstCostUSD < 4.49 || s.Totals.EstCostUSD > 4.51 {
		t.Errorf("expected ~4.5 USD, got %f", s.Totals.EstCostUSD)
	}
}

func TestCollectWithProviderToolCost(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")
	now := time.Now().UTC()

	enfDir := filepath.Join(dir, "audit", "agent-x", "enforcer")
	os.MkdirAll(enfDir, 0755)

	rec := auditRecord{
		Timestamp:                now.Add(-1 * time.Hour).Format(time.RFC3339Nano),
		Type:                     "LLM_DIRECT",
		Agent:                    "agent-x",
		Model:                    "gemini-flash",
		ProviderModel:            "gemini-2.5-flash",
		Status:                   200,
		DurationMs:               1000,
		InputTokens:              1000,
		OutputTokens:             1000,
		ProviderToolCallCount:    1,
		ProviderToolCapabilities: "provider-web-search",
	}
	b, _ := json.Marshal(rec)
	b = append(b, '\n')
	os.WriteFile(filepath.Join(enfDir, "enforcer-"+today+".jsonl"), b, 0644)

	costs := map[string]ModelCost{
		"gemini-flash": {
			CostPerMTokIn:  1.0,
			CostPerMTokOut: 1.0,
			ProviderToolPricing: map[string]ProviderToolPrice{
				"provider-web-search": {Unit: "search", USDPerUnit: 0.01, Source: "test", Confidence: "exact"},
			},
		},
	}

	s, err := CollectWithCosts(dir, MetricsQuery{}, costs)
	if err != nil {
		t.Fatal(err)
	}
	if s.Totals.ProviderToolCalls != 1 {
		t.Fatalf("provider tool calls = %d", s.Totals.ProviderToolCalls)
	}
	if s.Totals.ProviderToolCostUSD != 0.01 {
		t.Fatalf("provider tool cost = %f", s.Totals.ProviderToolCostUSD)
	}
	if s.Totals.EstCostUSD <= 0.01 {
		t.Fatalf("estimated cost should include token and provider tool cost, got %f", s.Totals.EstCostUSD)
	}
	if s.ByProviderTool["provider-web-search"].ProviderToolCostUSD != 0.01 {
		t.Fatalf("provider tool breakdown cost = %f", s.ByProviderTool["provider-web-search"].ProviderToolCostUSD)
	}
}

func TestCollectProviderToolCostFromAuditExtra(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")
	now := time.Now().UTC()

	enfDir := filepath.Join(dir, "audit", "agent-x", "enforcer")
	os.MkdirAll(enfDir, 0755)

	line := map[string]interface{}{
		"ts":             now.Add(-1 * time.Hour).Format(time.RFC3339Nano),
		"type":           "LLM_DIRECT",
		"agent":          "agent-x",
		"model":          "claude-sonnet",
		"provider_model": "claude-sonnet-4-20250514",
		"status":         200,
		"duration_ms":    1000,
		"input_tokens":   1000,
		"output_tokens":  1000,
		"extra": map[string]string{
			"provider_tool_call_count":         "1",
			"provider_tool_capabilities":       "provider-web-search",
			"provider_tool_estimated_cost_usd": "0.01000000",
			"provider_tool_unpriced_count":     "1",
		},
	}
	b, _ := json.Marshal(line)
	b = append(b, '\n')
	os.WriteFile(filepath.Join(enfDir, "enforcer-"+today+".jsonl"), b, 0644)

	s, err := CollectWithCosts(dir, MetricsQuery{}, map[string]ModelCost{
		"claude-sonnet": {CostPerMTokIn: 1.0, CostPerMTokOut: 1.0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Totals.ProviderToolCalls != 1 {
		t.Fatalf("provider tool calls = %d", s.Totals.ProviderToolCalls)
	}
	if s.Totals.ProviderToolCostUSD != 0.01 {
		t.Fatalf("provider tool cost = %f", s.Totals.ProviderToolCostUSD)
	}
	if s.Totals.ProviderToolUnpricedCalls != 1 {
		t.Fatalf("provider tool unpriced calls = %d", s.Totals.ProviderToolUnpricedCalls)
	}
	if s.ByProviderTool["provider-web-search"].ProviderToolCostUSD != 0.01 {
		t.Fatalf("provider tool breakdown cost = %f", s.ByProviderTool["provider-web-search"].ProviderToolCostUSD)
	}
}

func TestInferProvider(t *testing.T) {
	cases := []struct {
		model    string
		expected string
	}{
		{"claude-sonnet-4-20250514", "anthropic"},
		{"claude-3-opus-20240229", "anthropic"},
		{"gpt-4o", "openai"},
		{"o1-preview", "openai"},
		{"gemini-pro", "google"},
		{"mistral-large", "mistral"},
		{"deepseek-v3", "deepseek"},
		{"", "unknown"},
		{"some-custom-model", "other"},
	}
	for _, tc := range cases {
		got := inferProvider(tc.model)
		if got != tc.expected {
			t.Errorf("inferProvider(%q) = %q, want %q", tc.model, got, tc.expected)
		}
	}
}

func TestResolveWindow(t *testing.T) {
	// Default: last 24h
	since, until := resolveWindow("", "")
	if until.Sub(since) < 23*time.Hour {
		t.Errorf("default window too short: %v", until.Sub(since))
	}

	// Specific date
	since, until = resolveWindow("2026-03-20", "2026-03-20")
	if since.Format("2006-01-02") != "2026-03-20" {
		t.Errorf("since date wrong: %v", since)
	}
}

func TestCollectBySource(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")
	now := time.Now().UTC()

	enfDir := filepath.Join(dir, "audit", "agent-x", "enforcer")
	os.MkdirAll(enfDir, 0755)

	records := []auditRecord{
		{
			Timestamp: now.Add(-3 * time.Hour).Format(time.RFC3339Nano),
			Type:      "LLM_DIRECT_STREAM", Agent: "agent-x", Source: "enforcer",
			Model: "claude-sonnet", ProviderModel: "claude-sonnet-4-20250514",
			Status: 200, DurationMs: 1000, InputTokens: 500, OutputTokens: 200,
		},
		{
			Timestamp: now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
			Type:      "LLM_DIRECT_STREAM", Agent: "agent-x", Source: "synthesizer",
			Model: "claude-haiku", ProviderModel: "claude-haiku-4-5-20251001",
			Status: 200, DurationMs: 400, InputTokens: 300, OutputTokens: 100,
		},
		{
			Timestamp: now.Add(-1 * time.Hour).Format(time.RFC3339Nano),
			Type:      "LLM_DIRECT_STREAM", Agent: "agent-x", Source: "reflector",
			Model: "claude-sonnet", ProviderModel: "claude-sonnet-4-20250514",
			Status: 200, DurationMs: 800, InputTokens: 600, OutputTokens: 300,
		},
		{
			Timestamp: now.Add(-30 * time.Minute).Format(time.RFC3339Nano),
			Type:      "LLM_DIRECT_STREAM", Agent: "agent-x", Source: "enforcer",
			Model: "claude-sonnet", ProviderModel: "claude-sonnet-4-20250514",
			Status: 200, DurationMs: 1500, InputTokens: 700, OutputTokens: 250,
		},
		// Record with no source — should bucket as "unknown"
		{
			Timestamp: now.Add(-10 * time.Minute).Format(time.RFC3339Nano),
			Type:      "LLM_DIRECT", Agent: "agent-x",
			Model: "claude-sonnet", ProviderModel: "claude-sonnet-4-20250514",
			Status: 200, DurationMs: 500, InputTokens: 100, OutputTokens: 50,
		},
	}

	var lines []byte
	for _, rec := range records {
		b, _ := json.Marshal(rec)
		lines = append(lines, b...)
		lines = append(lines, '\n')
	}
	os.WriteFile(filepath.Join(enfDir, "enforcer-"+today+".jsonl"), lines, 0644)

	s, err := Collect(dir, MetricsQuery{})
	if err != nil {
		t.Fatal(err)
	}

	// Verify by_source has the right sources
	if len(s.BySource) != 4 {
		t.Errorf("expected 4 sources (enforcer, synthesizer, reflector, unknown), got %d: %v",
			len(s.BySource), keys(s.BySource))
	}

	if enf := s.BySource["enforcer"]; enf.Requests != 2 {
		t.Errorf("enforcer: expected 2 requests, got %d", enf.Requests)
	}
	if syn := s.BySource["synthesizer"]; syn.Requests != 1 {
		t.Errorf("synthesizer: expected 1 request, got %d", syn.Requests)
	}
	if ref := s.BySource["reflector"]; ref.Requests != 1 {
		t.Errorf("reflector: expected 1 request, got %d", ref.Requests)
	}
	if unk := s.BySource["unknown"]; unk.Requests != 1 {
		t.Errorf("unknown: expected 1 request, got %d", unk.Requests)
	}

	// Verify token totals for enforcer
	enf := s.BySource["enforcer"]
	if enf.InputTokens != 1200 {
		t.Errorf("enforcer input_tokens: expected 1200, got %d", enf.InputTokens)
	}
	if enf.OutputTokens != 450 {
		t.Errorf("enforcer output_tokens: expected 450, got %d", enf.OutputTokens)
	}
	if enf.TotalTokens != 1650 {
		t.Errorf("enforcer total_tokens: expected 1650, got %d", enf.TotalTokens)
	}

	// Verify latency was finalised
	if enf.AvgLatencyMs == 0 {
		t.Error("enforcer: expected non-zero avg_latency_ms")
	}
}

func keys(m map[string]Totals) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func TestCollectGatewayInfraEvents(t *testing.T) {
	// Gateway internal LLM events use "timestamp"/"event" field names (not "ts"/"type").
	// The metrics collector must handle both formats.
	dir := t.TempDir()
	now := time.Now().UTC()

	// Gateway writes infra events to audit/_infrastructure/gateway.jsonl
	infraDir := filepath.Join(dir, "audit", "_infrastructure")
	os.MkdirAll(infraDir, 0755)

	// Write gateway-format events (timestamp/event instead of ts/type)
	events := []string{
		fmt.Sprintf(`{"timestamp":"%s","event":"LLM_DIRECT","source":"knowledge-synthesizer","agent":"_infrastructure","model":"claude-haiku","provider_model":"claude-haiku-4-5-20251001","status":200,"duration_ms":800,"input_tokens":400,"output_tokens":100}`,
			now.Add(-2*time.Hour).Format(time.RFC3339)),
		fmt.Sprintf(`{"timestamp":"%s","event":"LLM_DIRECT","source":"platform-evaluation","agent":"_infrastructure","model":"claude-haiku","provider_model":"claude-haiku-4-5-20251001","status":200,"duration_ms":600,"input_tokens":300,"output_tokens":80}`,
			now.Add(-1*time.Hour).Format(time.RFC3339)),
		fmt.Sprintf(`{"timestamp":"%s","event":"LLM_DIRECT_ERROR","source":"knowledge-curator","agent":"_infrastructure","model":"claude-haiku","provider_model":"claude-haiku-4-5-20251001","status":500,"error":"timeout","duration_ms":30000,"input_tokens":0,"output_tokens":0}`,
			now.Add(-30*time.Minute).Format(time.RFC3339)),
	}
	var content string
	for _, e := range events {
		content += e + "\n"
	}
	os.WriteFile(filepath.Join(infraDir, "gateway.jsonl"), []byte(content), 0644)

	s, err := Collect(dir, MetricsQuery{})
	if err != nil {
		t.Fatal(err)
	}

	if s.Totals.Requests != 3 {
		t.Errorf("expected 3 total requests, got %d", s.Totals.Requests)
	}
	if s.Totals.Errors != 1 {
		t.Errorf("expected 1 error, got %d", s.Totals.Errors)
	}
	if s.Totals.InputTokens != 700 {
		t.Errorf("expected 700 input tokens, got %d", s.Totals.InputTokens)
	}

	// Verify by_source has the infra callers
	if synth, ok := s.BySource["knowledge-synthesizer"]; !ok {
		t.Error("missing knowledge-synthesizer in by_source")
	} else if synth.Requests != 1 {
		t.Errorf("expected 1 knowledge-synthesizer request, got %d", synth.Requests)
	}
	if eval, ok := s.BySource["platform-evaluation"]; !ok {
		t.Error("missing platform-evaluation in by_source")
	} else if eval.Requests != 1 {
		t.Errorf("expected 1 platform-evaluation request, got %d", eval.Requests)
	}
	if cur, ok := s.BySource["knowledge-curator"]; !ok {
		t.Error("missing knowledge-curator in by_source")
	} else if cur.Errors != 1 {
		t.Errorf("expected 1 knowledge-curator error, got %d", cur.Errors)
	}

	// Verify by_agent buckets under _infrastructure
	if infra, ok := s.ByAgent["_infrastructure"]; !ok {
		t.Error("missing _infrastructure in by_agent")
	} else if infra.Requests != 3 {
		t.Errorf("expected 3 _infrastructure requests, got %d", infra.Requests)
	}
}

func TestCollectLegacyInfraEvents(t *testing.T) {
	// Old gateway events used "caller" (not "source"), "event: infra_llm_call" (not "type: LLM_DIRECT"),
	// and "source: gateway" (hardcoded by writer). Verify backward compat.
	dir := t.TempDir()
	now := time.Now().UTC()

	infraDir := filepath.Join(dir, "audit", "_infrastructure")
	os.MkdirAll(infraDir, 0755)

	events := []string{
		fmt.Sprintf(`{"timestamp":"%s","event":"infra_llm_call","source":"gateway","caller":"knowledge-synthesizer","agent":"_infrastructure","model":"claude-haiku","provider_model":"claude-haiku-4-5-20251001","status":200,"duration_ms":800,"input_tokens":400,"output_tokens":100}`,
			now.Add(-1*time.Hour).Format(time.RFC3339)),
		fmt.Sprintf(`{"timestamp":"%s","event":"infra_llm_error","source":"gateway","caller":"platform-evaluation","agent":"_infrastructure","model":"claude-haiku","provider_model":"claude-haiku-4-5-20251001","status":500,"error":"timeout","duration_ms":30000,"input_tokens":0,"output_tokens":0}`,
			now.Add(-30*time.Minute).Format(time.RFC3339)),
	}
	var content string
	for _, e := range events {
		content += e + "\n"
	}
	os.WriteFile(filepath.Join(infraDir, "gateway.jsonl"), []byte(content), 0644)

	s, err := Collect(dir, MetricsQuery{})
	if err != nil {
		t.Fatal(err)
	}

	if s.Totals.Requests != 2 {
		t.Errorf("expected 2 requests, got %d", s.Totals.Requests)
	}

	// "caller" should be used as source (not "gateway")
	if _, ok := s.BySource["gateway"]; ok {
		t.Error("source should be caller identity, not 'gateway'")
	}
	if synth, ok := s.BySource["knowledge-synthesizer"]; !ok {
		t.Error("missing knowledge-synthesizer in by_source")
	} else if synth.Requests != 1 {
		t.Errorf("expected 1 knowledge-synthesizer request, got %d", synth.Requests)
	}
	if eval, ok := s.BySource["platform-evaluation"]; !ok {
		t.Error("missing platform-evaluation in by_source")
	} else if eval.Errors != 1 {
		t.Errorf("expected 1 platform-evaluation error, got %d", eval.Errors)
	}
}

func TestPercentile(t *testing.T) {
	data := []int64{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000}
	p95 := percentile(data, 95)
	if p95 != 1000 {
		t.Errorf("p95 of 10 items should be 1000, got %d", p95)
	}
	p50 := percentile(data, 50)
	if p50 != 500 {
		t.Errorf("p50 should be 500, got %d", p50)
	}
}
