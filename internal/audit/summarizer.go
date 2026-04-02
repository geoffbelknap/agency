package audit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// AuditEntry mirrors the enforcer's audit entry (subset needed for summarization).
type AuditEntry struct {
	Timestamp    string `json:"ts"`
	Type         string `json:"type"`
	Agent        string `json:"agent,omitempty"`
	Model        string `json:"model,omitempty"`
	EventID      string `json:"event_id,omitempty"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
}

// MissionMetric holds aggregated metrics for one mission on one date.
type MissionMetric struct {
	Mission           string  `json:"mission"`
	Date              string  `json:"date"`
	Activations       int     `json:"activations"`
	TotalInputTokens  int     `json:"total_input_tokens"`
	TotalOutputTokens int     `json:"total_output_tokens"`
	EstimatedCostUSD  float64 `json:"estimated_cost_usd"`
	AvgTokensPerAct   float64 `json:"avg_tokens_per_activation"`
	Model             string  `json:"model"`
	EscalationCount   *int    `json:"escalation_count"`  // v2 — null for now
	FindingsCount     *int    `json:"findings_count"`    // v2 — null for now
}

// AuditSummarizer aggregates enforcer audit logs into per-mission metrics.
type AuditSummarizer struct {
	ticker       *time.Ticker
	homeDir      string
	knowledgeURL string
	missionMap   map[string]string // agent -> mission
	logger       *log.Logger
}

// NewAuditSummarizer creates a summarizer that reads audit logs from homeDir.
func NewAuditSummarizer(homeDir, knowledgeURL string, logger *log.Logger) *AuditSummarizer {
	return &AuditSummarizer{
		ticker:       time.NewTicker(15 * time.Minute),
		homeDir:      homeDir,
		knowledgeURL: knowledgeURL,
		missionMap:   make(map[string]string),
		logger:       logger,
	}
}

// Start begins periodic summarization in a background goroutine.
func (s *AuditSummarizer) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				s.ticker.Stop()
				return
			case <-s.ticker.C:
				if _, err := s.Summarize(); err != nil {
					s.logger.Warn("audit summarization failed", "err", err)
				}
			}
		}
	}()
}

// Summarize aggregates audit logs for today and yesterday, returning per-mission metrics.
// Upserts each metric as a MissionMetrics node in the knowledge graph.
func (s *AuditSummarizer) Summarize() ([]MissionMetric, error) {
	s.loadMissionMap()
	now := time.Now()
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	var all []MissionMetric
	for _, date := range []string{yesterday, today} {
		metrics, err := s.summarizeDate(date)
		if err != nil {
			s.logger.Warn("summarize date failed", "date", date, "err", err)
			continue
		}
		all = append(all, metrics...)
	}

	// Upsert metrics as MissionMetrics nodes in the knowledge graph
	if s.knowledgeURL != "" && len(all) > 0 {
		s.upsertMetricsToKnowledge(all)
	}

	return all, nil
}

// upsertMetricsToKnowledge POSTs MissionMetrics nodes to the knowledge graph.
func (s *AuditSummarizer) upsertMetricsToKnowledge(metrics []MissionMetric) {
	var nodes []map[string]interface{}
	for _, m := range metrics {
		nodes = append(nodes, map[string]interface{}{
			"label":       fmt.Sprintf("%s:%s", m.Mission, m.Date),
			"kind":        "MissionMetrics",
			"summary":     "",
			"source_type": "rule",
			"properties": map[string]interface{}{
				"mission":                m.Mission,
				"date":                   m.Date,
				"activations":            m.Activations,
				"total_input_tokens":     m.TotalInputTokens,
				"total_output_tokens":    m.TotalOutputTokens,
				"estimated_cost_usd":     m.EstimatedCostUSD,
				"avg_tokens_per_act":     m.AvgTokensPerAct,
				"model":                  m.Model,
			},
		})
	}

	body, err := json.Marshal(map[string]interface{}{"nodes": nodes})
	if err != nil {
		s.logger.Warn("marshal mission metrics nodes", "err", err)
		return
	}

	resp, err := http.Post(s.knowledgeURL+"/ingest/nodes", "application/json", bytes.NewReader(body))
	if err != nil {
		s.logger.Warn("upsert mission metrics to knowledge", "err", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		s.logger.Warn("upsert mission metrics to knowledge", "status", resp.StatusCode)
	}
}

func parseAuditLine(line []byte) (AuditEntry, error) {
	var entry AuditEntry
	err := json.Unmarshal(line, &entry)
	return entry, err
}

func (s *AuditSummarizer) summarizeDate(date string) ([]MissionMetric, error) {
	auditDir := filepath.Join(s.homeDir, "audit")
	pattern := filepath.Join(auditDir, "*", fmt.Sprintf("enforcer-%s.jsonl", date))
	files, _ := filepath.Glob(pattern)

	type activation struct {
		inputTokens  int
		outputTokens int
		models       []string
		cost         float64
	}
	missionActs := map[string]map[string]*activation{} // mission -> event_id -> activation

	for _, fpath := range files {
		f, err := os.Open(fpath)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			entry, err := parseAuditLine(scanner.Bytes())
			if err != nil {
				continue
			}
			if entry.Type != "LLM_DIRECT" && entry.Type != "LLM_DIRECT_STREAM" {
				continue
			}
			mission := s.missionMap[entry.Agent]
			if mission == "" {
				mission = "unattributed"
			}
			eventID := entry.EventID
			if eventID == "" {
				eventID = "no-event-id"
			}
			if missionActs[mission] == nil {
				missionActs[mission] = map[string]*activation{}
			}
			act := missionActs[mission][eventID]
			if act == nil {
				act = &activation{}
				missionActs[mission][eventID] = act
			}
			act.inputTokens += entry.InputTokens
			act.outputTokens += entry.OutputTokens
			act.models = append(act.models, entry.Model)
			cost := EstimateCost(entry.Model, entry.InputTokens, entry.OutputTokens)
			if cost == 0 && entry.Model != "" {
				s.logger.Warn("unknown model for cost estimation", "model", entry.Model)
			}
			act.cost += cost
		}
		f.Close()
	}

	var metrics []MissionMetric
	for mission, activations := range missionActs {
		var totalIn, totalOut int
		var totalCost float64
		modelCounts := map[string]int{}
		for _, act := range activations {
			totalIn += act.inputTokens
			totalOut += act.outputTokens
			totalCost += act.cost
			for _, m := range act.models {
				modelCounts[m]++
			}
		}
		modalModel := ""
		maxCount := 0
		for m, c := range modelCounts {
			if c > maxCount {
				modalModel = m
				maxCount = c
			}
		}
		numActs := len(activations)
		avg := float64(0)
		if numActs > 0 {
			avg = float64(totalIn+totalOut) / float64(numActs)
		}
		metrics = append(metrics, MissionMetric{
			Mission: mission, Date: date, Activations: numActs,
			TotalInputTokens: totalIn, TotalOutputTokens: totalOut,
			EstimatedCostUSD: totalCost, AvgTokensPerAct: avg, Model: modalModel,
		})
	}
	sort.Slice(metrics, func(i, j int) bool { return metrics[i].Mission < metrics[j].Mission })
	return metrics, nil
}

func (s *AuditSummarizer) loadMissionMap() {
	missionsDir := filepath.Join(s.homeDir, "missions")
	files, _ := filepath.Glob(filepath.Join(missionsDir, "*.yaml"))
	s.missionMap = make(map[string]string)
	for _, fpath := range files {
		data, err := os.ReadFile(fpath)
		if err != nil {
			continue
		}
		missionName := ""
		inAgents := false
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "name:") {
				missionName = strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
				missionName = strings.Trim(missionName, "\"'")
			}
			if strings.HasPrefix(trimmed, "agents:") {
				inAgents = true
				continue
			}
			if inAgents {
				if strings.HasPrefix(trimmed, "- ") {
					agent := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
					agent = strings.Trim(agent, "\"'")
					if missionName != "" && agent != "" {
						s.missionMap[agent] = missionName
					}
				} else if !strings.HasPrefix(trimmed, "#") && trimmed != "" {
					inAgents = false
				}
			}
		}
	}
}
