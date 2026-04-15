// Package routing provides LLM routing metrics aggregation.
//
// It reads enforcer audit logs (JSONL) from the agency audit directory and
// computes per-agent, per-model, and per-provider usage summaries including
// token counts, estimated cost, latency percentiles, and error rates.
package routing

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MetricsQuery controls which audit logs are scanned.
type MetricsQuery struct {
	Agent string // empty = all agents
	Since string // RFC3339 or YYYY-MM-DD; empty = last 24h
	Until string // RFC3339 or YYYY-MM-DD; empty = now
}

// Summary is the top-level metrics response.
type Summary struct {
	Period         Period            `json:"period"`
	Totals         Totals            `json:"totals"`
	ByAgent        map[string]Totals `json:"by_agent"`
	ByModel        map[string]Totals `json:"by_model"`
	ByProvider     map[string]Totals `json:"by_provider"`
	BySource       map[string]Totals `json:"by_source"`
	ByProviderTool map[string]Totals `json:"by_provider_tool,omitempty"`
	RecentErrors   []ErrorEntry      `json:"recent_errors,omitempty"`
}

// Period describes the time window of the metrics.
type Period struct {
	Since string `json:"since"`
	Until string `json:"until"`
}

// Totals holds aggregate counters for a group of LLM requests.
type Totals struct {
	Requests                  int     `json:"requests"`
	InputTokens               int64   `json:"input_tokens"`
	OutputTokens              int64   `json:"output_tokens"`
	TotalTokens               int64   `json:"total_tokens"`
	EstCostUSD                float64 `json:"est_cost_usd"`
	Errors                    int     `json:"errors"`
	AvgLatencyMs              int64   `json:"avg_latency_ms"`
	P95LatencyMs              int64   `json:"p95_latency_ms"`
	TTFTP50Ms                 int64   `json:"ttft_p50_ms,omitempty"`
	TTFTP95Ms                 int64   `json:"ttft_p95_ms,omitempty"`
	TPOTP50Ms                 float64 `json:"tpot_p50_ms,omitempty"`
	TPOTP95Ms                 float64 `json:"tpot_p95_ms,omitempty"`
	ToolCalls                 int     `json:"tool_calls,omitempty"`
	ToolHallucinations        int     `json:"tool_hallucinations,omitempty"`
	ProviderToolCalls         int     `json:"provider_tool_calls,omitempty"`
	ProviderToolCostUSD       float64 `json:"provider_tool_cost_usd,omitempty"`
	ProviderToolUnpricedCalls int     `json:"provider_tool_unpriced_calls,omitempty"`
	RetryCostUSD              float64 `json:"retry_cost_usd,omitempty"`
	// internal — not serialised
	latencies []int64   `json:"-"`
	costAcc   float64   `json:"-"`
	ttfts     []int64   `json:"-"`
	tpots     []float64 `json:"-"`
}

// ErrorEntry captures a recent LLM error for operator visibility.
type ErrorEntry struct {
	Timestamp     string `json:"ts"`
	Agent         string `json:"agent"`
	Model         string `json:"model"`
	ProviderModel string `json:"provider_model,omitempty"`
	Status        int    `json:"status"`
	Error         string `json:"error"`
}

// ModelCost holds per-million-token cost rates for a model alias.
type ModelCost struct {
	CostPerMTokIn       float64
	CostPerMTokOut      float64
	CostPerMTokCached   float64
	ProviderToolCosts   map[string]float64
	ProviderToolPricing map[string]ProviderToolPrice
}

type ProviderToolPrice struct {
	Unit        string  `json:"unit" yaml:"unit"`
	USDPerUnit  float64 `json:"usd_per_unit" yaml:"usd_per_unit"`
	Source      string  `json:"source" yaml:"source"`
	Confidence  string  `json:"confidence" yaml:"confidence"`
	Description string  `json:"description,omitempty" yaml:"description,omitempty"`
}

// auditRecord is the subset of enforcer/gateway audit fields we need.
// Enforcer logs use "ts"/"type"; gateway logs use "timestamp"/"event".
// UnmarshalJSON handles both formats transparently.
type auditRecord struct {
	Timestamp                    string
	Type                         string
	Agent                        string
	Source                       string
	Model                        string
	ProviderModel                string
	Status                       int
	Error                        string
	DurationMs                   int64
	InputTokens                  int
	OutputTokens                 int
	TTFTMs                       int64
	TPOTMs                       float64
	StepIndex                    int
	ToolCallValid                *bool
	RetryOf                      string
	ContextTokens                int64
	ProviderToolCallCount        int
	ProviderToolEstimatedCostUSD float64
	ProviderToolCapabilities     string
	ProviderToolUnpricedCount    int
}

func (r auditRecord) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Ts                           string  `json:"ts"`
		Type                         string  `json:"type"`
		Agent                        string  `json:"agent"`
		Source                       string  `json:"source"`
		Model                        string  `json:"model"`
		ProviderModel                string  `json:"provider_model"`
		Status                       int     `json:"status"`
		Error                        string  `json:"error,omitempty"`
		DurationMs                   int64   `json:"duration_ms"`
		InputTokens                  int     `json:"input_tokens"`
		OutputTokens                 int     `json:"output_tokens"`
		TTFTMs                       int64   `json:"ttft_ms,omitempty"`
		TPOTMs                       float64 `json:"tpot_ms,omitempty"`
		StepIndex                    int     `json:"step_index,omitempty"`
		ToolCallValid                *bool   `json:"tool_call_valid,omitempty"`
		RetryOf                      string  `json:"retry_of,omitempty"`
		ContextTokens                int64   `json:"context_tokens,omitempty"`
		ProviderToolCallCount        int     `json:"provider_tool_call_count,omitempty"`
		ProviderToolEstimatedCostUSD float64 `json:"provider_tool_estimated_cost_usd,omitempty"`
		ProviderToolCapabilities     string  `json:"provider_tool_capabilities,omitempty"`
		ProviderToolUnpricedCount    int     `json:"provider_tool_unpriced_count,omitempty"`
	}{r.Timestamp, r.Type, r.Agent, r.Source, r.Model, r.ProviderModel, r.Status, r.Error, r.DurationMs, r.InputTokens, r.OutputTokens, r.TTFTMs, r.TPOTMs, r.StepIndex, r.ToolCallValid, r.RetryOf, r.ContextTokens, r.ProviderToolCallCount, r.ProviderToolEstimatedCostUSD, r.ProviderToolCapabilities, r.ProviderToolUnpricedCount})
}

func (r *auditRecord) UnmarshalJSON(data []byte) error {
	var raw struct {
		// Enforcer field names
		Ts   string `json:"ts"`
		Type string `json:"type"`
		// Gateway field names
		Timestamp string `json:"timestamp"`
		Event     string `json:"event"`
		// Common fields
		Agent                        string            `json:"agent"`
		Source                       string            `json:"source"`
		Caller                       string            `json:"caller"` // legacy gateway field, maps to source
		Model                        string            `json:"model"`
		ProviderModel                string            `json:"provider_model"`
		Status                       int               `json:"status"`
		Error                        string            `json:"error"`
		DurationMs                   int64             `json:"duration_ms"`
		InputTokens                  int               `json:"input_tokens"`
		OutputTokens                 int               `json:"output_tokens"`
		TTFTMs                       int64             `json:"ttft_ms"`
		TPOTMs                       float64           `json:"tpot_ms"`
		StepIndex                    int               `json:"step_index"`
		ToolCallValid                *bool             `json:"tool_call_valid"`
		RetryOf                      string            `json:"retry_of"`
		ContextTokens                int64             `json:"context_tokens"`
		ProviderToolCallCount        interface{}       `json:"provider_tool_call_count"`
		ProviderToolEstimatedCostUSD interface{}       `json:"provider_tool_estimated_cost_usd"`
		ProviderToolCapabilities     string            `json:"provider_tool_capabilities"`
		ProviderToolUnpricedCount    interface{}       `json:"provider_tool_unpriced_count"`
		Extra                        map[string]string `json:"extra"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Timestamp = raw.Ts
	if r.Timestamp == "" {
		r.Timestamp = raw.Timestamp
	}
	r.Type = raw.Type
	if r.Type == "" {
		r.Type = raw.Event
	}
	r.Agent = raw.Agent
	r.Source = raw.Source
	// Legacy gateway events stored caller identity in "caller" and set
	// "source" to the hardcoded value "gateway". Prefer caller when source
	// is missing or is the generic "gateway" string.
	if raw.Caller != "" && (r.Source == "" || r.Source == "gateway") {
		r.Source = raw.Caller
	}
	r.Model = raw.Model
	r.ProviderModel = raw.ProviderModel
	r.Status = raw.Status
	r.Error = raw.Error
	r.DurationMs = raw.DurationMs
	r.InputTokens = raw.InputTokens
	r.OutputTokens = raw.OutputTokens
	r.TTFTMs = raw.TTFTMs
	r.TPOTMs = raw.TPOTMs
	r.StepIndex = raw.StepIndex
	r.ToolCallValid = raw.ToolCallValid
	r.RetryOf = raw.RetryOf
	r.ContextTokens = raw.ContextTokens
	r.ProviderToolCallCount = intFromAny(raw.ProviderToolCallCount)
	r.ProviderToolEstimatedCostUSD = floatFromAny(raw.ProviderToolEstimatedCostUSD)
	r.ProviderToolCapabilities = raw.ProviderToolCapabilities
	r.ProviderToolUnpricedCount = intFromAny(raw.ProviderToolUnpricedCount)
	if raw.Extra != nil {
		if r.ProviderToolCallCount == 0 {
			r.ProviderToolCallCount = intFromAny(raw.Extra["provider_tool_call_count"])
		}
		if r.ProviderToolEstimatedCostUSD == 0 {
			r.ProviderToolEstimatedCostUSD = floatFromAny(raw.Extra["provider_tool_estimated_cost_usd"])
		}
		if r.ProviderToolCapabilities == "" {
			r.ProviderToolCapabilities = raw.Extra["provider_tool_capabilities"]
		}
		if r.ProviderToolUnpricedCount == 0 {
			r.ProviderToolUnpricedCount = intFromAny(raw.Extra["provider_tool_unpriced_count"])
		}
	}
	return nil
}

func intFromAny(v interface{}) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	default:
		return 0
	}
}

func floatFromAny(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		n, _ := strconv.ParseFloat(x, 64)
		return n
	default:
		return 0
	}
}

// Collect scans enforcer audit logs under homeDir and returns aggregated metrics.
func Collect(homeDir string, q MetricsQuery) (*Summary, error) {
	return CollectWithCosts(homeDir, q, nil)
}

// CollectWithCosts is like Collect but also estimates cost using the given
// model cost rates (keyed by model alias). If costs is nil, EstCostUSD is 0.
func CollectWithCosts(homeDir string, q MetricsQuery, costs map[string]ModelCost) (*Summary, error) {
	sinceT, untilT := resolveWindow(q.Since, q.Until)

	records, err := readAuditRecords(homeDir, q.Agent, sinceT, untilT)
	if err != nil {
		return nil, err
	}

	s := &Summary{
		Period: Period{
			Since: sinceT.UTC().Format(time.RFC3339),
			Until: untilT.UTC().Format(time.RFC3339),
		},
		ByAgent:        make(map[string]Totals),
		ByModel:        make(map[string]Totals),
		ByProvider:     make(map[string]Totals),
		BySource:       make(map[string]Totals),
		ByProviderTool: make(map[string]Totals),
	}

	for _, rec := range records {
		isErr := rec.Status >= 400 || rec.Error != ""

		providerToolCost := calcProviderToolCost(rec, costs)
		cost := calcTokenCost(rec, costs) + providerToolCost
		accumWithCost(&s.Totals, rec, cost, providerToolCost)
		accumMapWithCost(s.ByAgent, rec.Agent, rec, cost, providerToolCost)
		accumMapWithCost(s.ByModel, rec.Model, rec, cost, providerToolCost)

		provider := inferProvider(rec.ProviderModel)
		accumMapWithCost(s.ByProvider, provider, rec, cost, providerToolCost)
		accumMapWithCost(s.BySource, rec.Source, rec, cost, providerToolCost)
		for cap, capCost := range calcProviderToolCostsByCapability(rec, costs) {
			capRec := rec
			capRec.ProviderToolCallCount = 1
			if rec.ProviderToolUnpricedCount > 0 && capCost == 0 {
				capRec.ProviderToolUnpricedCount = 1
			} else {
				capRec.ProviderToolUnpricedCount = 0
			}
			accumMapWithCost(s.ByProviderTool, cap, capRec, capCost, capCost)
		}

		if isErr {
			s.RecentErrors = append(s.RecentErrors, ErrorEntry{
				Timestamp:     rec.Timestamp,
				Agent:         rec.Agent,
				Model:         rec.Model,
				ProviderModel: rec.ProviderModel,
				Status:        rec.Status,
				Error:         rec.Error,
			})
		}
	}

	// Finalise latency stats.
	finalise(&s.Totals)
	for k, v := range s.ByAgent {
		finalise(&v)
		s.ByAgent[k] = v
	}
	for k, v := range s.ByModel {
		finalise(&v)
		s.ByModel[k] = v
	}
	for k, v := range s.ByProvider {
		finalise(&v)
		s.ByProvider[k] = v
	}
	for k, v := range s.BySource {
		finalise(&v)
		s.BySource[k] = v
	}
	for k, v := range s.ByProviderTool {
		finalise(&v)
		s.ByProviderTool[k] = v
	}

	// Cap recent errors to last 25.
	if len(s.RecentErrors) > 25 {
		s.RecentErrors = s.RecentErrors[len(s.RecentErrors)-25:]
	}

	return s, nil
}

// calcTokenCost computes estimated USD token cost for a single LLM request.
func calcTokenCost(rec auditRecord, costs map[string]ModelCost) float64 {
	if costs == nil {
		return 0
	}
	mc, ok := costs[rec.Model]
	if !ok {
		return 0
	}
	return (float64(rec.InputTokens)*mc.CostPerMTokIn +
		float64(rec.OutputTokens)*mc.CostPerMTokOut) / 1_000_000
}

func calcProviderToolCost(rec auditRecord, costs map[string]ModelCost) float64 {
	providerToolCost := rec.ProviderToolEstimatedCostUSD
	if providerToolCost == 0 && rec.ProviderToolCallCount > 0 && rec.ProviderToolCapabilities != "" {
		if costs == nil {
			return 0
		}
		mc, ok := costs[rec.Model]
		if !ok {
			return 0
		}
		for _, cap := range strings.Split(rec.ProviderToolCapabilities, ",") {
			providerToolCost += providerToolUnitCost(mc, strings.TrimSpace(cap))
		}
	}
	return providerToolCost
}

func calcProviderToolCostsByCapability(rec auditRecord, costs map[string]ModelCost) map[string]float64 {
	if rec.ProviderToolCapabilities == "" {
		return nil
	}
	caps := strings.Split(rec.ProviderToolCapabilities, ",")
	out := make(map[string]float64, len(caps))
	if rec.ProviderToolEstimatedCostUSD > 0 && len(caps) == 1 {
		out[strings.TrimSpace(caps[0])] = rec.ProviderToolEstimatedCostUSD
		return out
	}
	var mc ModelCost
	var ok bool
	if costs != nil {
		mc, ok = costs[rec.Model]
	}
	for _, rawCap := range caps {
		cap := strings.TrimSpace(rawCap)
		if cap == "" {
			continue
		}
		if ok {
			out[cap] = providerToolUnitCost(mc, cap)
		} else {
			out[cap] = 0
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func providerToolUnitCost(mc ModelCost, cap string) float64 {
	if p, ok := mc.ProviderToolPricing[cap]; ok {
		return p.USDPerUnit
	}
	if mc.ProviderToolCosts != nil {
		return mc.ProviderToolCosts[cap]
	}
	return 0
}

// resolveWindow parses since/until strings, defaulting to last 24h.
func resolveWindow(since, until string) (time.Time, time.Time) {
	now := time.Now().UTC()
	untilT := now
	sinceT := now.Add(-24 * time.Hour)

	if since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			sinceT = t
		} else if t, err := time.Parse("2006-01-02", since); err == nil {
			sinceT = t
		}
	}
	if until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			untilT = t
		} else if t, err := time.Parse("2006-01-02", until); err == nil {
			untilT = t.Add(24*time.Hour - time.Nanosecond)
		}
	}
	return sinceT, untilT
}

// readAuditRecords reads enforcer JSONL files for the given agent (or all agents).
func readAuditRecords(homeDir, agent string, since, until time.Time) ([]auditRecord, error) {
	auditDir := filepath.Join(homeDir, "audit")
	if _, err := os.Stat(auditDir); err != nil {
		return nil, nil // no audit dir yet — not an error
	}

	var agents []string
	if agent != "" {
		agents = []string{agent}
	} else {
		entries, err := os.ReadDir(auditDir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				agents = append(agents, e.Name())
			}
		}
	}

	var records []auditRecord
	for _, ag := range agents {
		// Enforcer logs live in audit/{agent}/enforcer/enforcer-YYYY-MM-DD.jsonl
		enforcerDir := filepath.Join(auditDir, ag, "enforcer")
		recs := readJSONLDir(enforcerDir, ag, since, until)
		records = append(records, recs...)

		// Also check top-level agent dir for enforcer-*.jsonl (alternate layout)
		agentDir := filepath.Join(auditDir, ag)
		recs = readJSONLDir(agentDir, ag, since, until)
		records = append(records, recs...)
	}

	return records, nil
}

// readJSONLDir reads all enforcer-*.jsonl files in dir and returns LLM-related records.
func readJSONLDir(dir, agentName string, since, until time.Time) []auditRecord {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var records []auditRecord
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		// Quick date filter: skip files outside the time window by filename date.
		if strings.HasPrefix(name, "enforcer-") {
			dateStr := strings.TrimPrefix(name, "enforcer-")
			dateStr = strings.TrimSuffix(dateStr, ".jsonl")
			if fileDate, err := time.ParseInLocation("2006-01-02", dateStr, time.UTC); err == nil {
				if fileDate.Before(since.Truncate(24*time.Hour)) || fileDate.After(until.Truncate(24*time.Hour).Add(24*time.Hour)) {
					continue
				}
			}
		}

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}

		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var rec auditRecord
			if json.Unmarshal([]byte(line), &rec) != nil {
				continue
			}
			// Only include LLM request records.
			if !isLLMType(rec.Type) {
				continue
			}
			// Apply time filter.
			if ts := parseTS(rec.Timestamp); !ts.IsZero() {
				if ts.Before(since) || ts.After(until) {
					continue
				}
			}
			if rec.Agent == "" {
				rec.Agent = agentName
			}
			records = append(records, rec)
		}
	}
	return records
}

// isLLMType returns true for audit event types related to LLM requests.
// Handles both enforcer types (LLM_DIRECT*) and legacy gateway types (infra_llm_*).
func isLLMType(t string) bool {
	switch t {
	case "LLM_DIRECT", "LLM_DIRECT_STREAM", "LLM_DIRECT_ERROR",
		"LLM_UNKNOWN_MODEL", "LLM_BUDGET_EXCEEDED",
		"infra_llm_call", "infra_llm_error":
		return true
	}
	return false
}

func parseTS(s string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

// inferProvider guesses the provider name from the provider_model string.
func inferProvider(providerModel string) string {
	pm := strings.ToLower(providerModel)
	switch {
	case strings.HasPrefix(pm, "claude"):
		return "anthropic"
	case strings.HasPrefix(pm, "gpt") || strings.HasPrefix(pm, "o1") || strings.HasPrefix(pm, "o3") || strings.HasPrefix(pm, "o4"):
		return "openai"
	case strings.HasPrefix(pm, "gemini"):
		return "google"
	case strings.HasPrefix(pm, "mistral") || strings.HasPrefix(pm, "codestral"):
		return "mistral"
	case strings.HasPrefix(pm, "deepseek"):
		return "deepseek"
	default:
		if providerModel == "" {
			return "unknown"
		}
		return "other"
	}
}

// accumWithCost adds a record's values and cost into a Totals bucket.
func accumWithCost(t *Totals, rec auditRecord, cost float64, providerToolCost float64) {
	t.Requests++
	t.InputTokens += int64(rec.InputTokens)
	t.OutputTokens += int64(rec.OutputTokens)
	t.EstCostUSD += cost
	t.ProviderToolCalls += rec.ProviderToolCallCount
	t.ProviderToolCostUSD += providerToolCost
	t.ProviderToolUnpricedCalls += rec.ProviderToolUnpricedCount
	if rec.Status >= 400 || rec.Error != "" {
		t.Errors++
	}
	if rec.DurationMs > 0 {
		t.latencies = append(t.latencies, rec.DurationMs)
	}
	if rec.TTFTMs > 0 {
		t.ttfts = append(t.ttfts, rec.TTFTMs)
	}
	if rec.TPOTMs > 0 {
		t.tpots = append(t.tpots, rec.TPOTMs)
	}
	if rec.ToolCallValid != nil {
		t.ToolCalls++
		if !*rec.ToolCallValid {
			t.ToolHallucinations++
		}
	}
	if rec.RetryOf != "" {
		t.RetryCostUSD += cost
	}
}

func accumMapWithCost(m map[string]Totals, key string, rec auditRecord, cost float64, providerToolCost float64) {
	if key == "" {
		key = "unknown"
	}
	t := m[key]
	accumWithCost(&t, rec, cost, providerToolCost)
	m[key] = t
}

// finalise computes derived fields (total tokens, average, p95).
func finalise(t *Totals) {
	t.TotalTokens = t.InputTokens + t.OutputTokens
	if len(t.latencies) > 0 {
		var sum int64
		for _, l := range t.latencies {
			sum += l
		}
		t.AvgLatencyMs = sum / int64(len(t.latencies))
		t.P95LatencyMs = percentile(t.latencies, 95)
	}
	// TTFT/TPOT percentiles.
	if len(t.ttfts) > 0 {
		t.TTFTP50Ms = percentile(t.ttfts, 50)
		t.TTFTP95Ms = percentile(t.ttfts, 95)
	}
	if len(t.tpots) > 0 {
		sort.Float64s(t.tpots)
		idx50 := int(float64(len(t.tpots)) * 0.50)
		idx95 := int(float64(len(t.tpots)) * 0.95)
		if idx50 >= len(t.tpots) {
			idx50 = len(t.tpots) - 1
		}
		if idx95 >= len(t.tpots) {
			idx95 = len(t.tpots) - 1
		}
		t.TPOTP50Ms = t.tpots[idx50]
		t.TPOTP95Ms = t.tpots[idx95]
	}
	// Round cost to 6 decimal places.
	t.EstCostUSD = math.Round(t.EstCostUSD*1e6) / 1e6
	t.ProviderToolCostUSD = math.Round(t.ProviderToolCostUSD*1e6) / 1e6
	t.RetryCostUSD = math.Round(t.RetryCostUSD*1e6) / 1e6
	t.latencies = nil // free memory
	t.ttfts = nil
	t.tpots = nil
}

func percentile(data []int64, pct int) int64 {
	if len(data) == 0 {
		return 0
	}
	sorted := make([]int64, len(data))
	copy(sorted, data)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(math.Ceil(float64(pct)/100.0*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
