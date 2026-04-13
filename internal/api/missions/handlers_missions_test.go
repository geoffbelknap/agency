package missions

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

type recordingCommsClient struct {
	requests []commsRequest
}

type commsRequest struct {
	Method string
	Path   string
	Body   map[string]interface{}
}

func (r *recordingCommsClient) CommsRequest(_ context.Context, method, path string, body interface{}) ([]byte, error) {
	req := commsRequest{Method: method, Path: path}
	if body != nil {
		data, _ := json.Marshal(body)
		_ = json.Unmarshal(data, &req.Body)
	}
	r.requests = append(r.requests, req)
	return []byte(`{}`), nil
}

type recordingSignalSender struct {
	signals []string
}

func (r *recordingSignalSender) SignalContainer(_ context.Context, containerName, signal string) error {
	r.signals = append(r.signals, containerName+":"+signal)
	return nil
}

func writeTestAgent(t *testing.T, home, name string) {
	t.Helper()
	agentDir := filepath.Join(home, "agents", name)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("name: "+name+"\n"), 0o644); err != nil {
		t.Fatalf("write agent %s: %v", name, err)
	}
}

func writeTestTeam(t *testing.T, home string, cfg models.TeamConfig) {
	t.Helper()
	teamDir := filepath.Join(home, "teams", cfg.Name)
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		t.Fatalf("mkdir team %s: %v", cfg.Name, err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal team %s: %v", cfg.Name, err)
	}
	if err := os.WriteFile(filepath.Join(teamDir, "team.yaml"), data, 0o644); err != nil {
		t.Fatalf("write team %s: %v", cfg.Name, err)
	}
}

func withMissionName(req *http.Request, name string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("name", name)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
	return req.WithContext(ctx)
}

func TestClaimMissionEvent(t *testing.T) {
	home := t.TempDir()
	mm := orchestrate.NewMissionManager(home)
	if err := mm.Create(&models.Mission{
		Name:         "claimable-mission",
		Description:  "Claim test",
		Instructions: "Handle one event at a time.",
	}); err != nil {
		t.Fatalf("create mission: %v", err)
	}

	h := &handler{deps: Deps{
		MissionManager: mm,
		Claims:         orchestrate.NewMissionClaimRegistry(),
	}}

	makeReq := func(agent string) *http.Request {
		body := bytes.NewBufferString(`{"event_key":"INC-123","agent_name":"` + agent + `"}`)
		return withMissionName(httptest.NewRequest(http.MethodPost, "/api/v1/missions/claimable-mission/claim", body), "claimable-mission")
	}

	rec := httptest.NewRecorder()
	h.claimMissionEvent(rec, makeReq("agent-a"))
	if rec.Code != http.StatusOK {
		t.Fatalf("first claim status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var first map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if claimed, _ := first["claimed"].(bool); !claimed {
		t.Fatalf("expected first claim to succeed: %v", first)
	}
	if holder, _ := first["holder"].(string); holder != "agent-a" {
		t.Fatalf("expected holder agent-a, got %q", holder)
	}

	rec = httptest.NewRecorder()
	h.claimMissionEvent(rec, makeReq("agent-b"))
	if rec.Code != http.StatusOK {
		t.Fatalf("second claim status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var second map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if claimed, _ := second["claimed"].(bool); claimed {
		t.Fatalf("expected second claim to fail: %v", second)
	}
	if holder, _ := second["holder"].(string); holder != "agent-a" {
		t.Fatalf("expected holder agent-a after contention, got %q", holder)
	}
}

func TestCheckCoordinatorFailoverAssignsCoverageAndAlertsOperator(t *testing.T) {
	home := t.TempDir()
	mm := orchestrate.NewMissionManager(home)
	for _, agent := range []string{"coord-agent", "coverage-agent"} {
		writeTestAgent(t, home, agent)
	}
	writeTestTeam(t, home, models.TeamConfig{
		Name:        "alpha-team",
		Coordinator: "coord-agent",
		Coverage:    "coverage-agent",
		Members: []models.TeamMember{
			{Name: "coord-agent", Type: "agent", AgentType: "coordinator"},
			{Name: "coverage-agent", Type: "agent"},
		},
	})

	mission := &models.Mission{
		Name:         "team-failover",
		Description:  "Failover proof",
		Instructions: "Coordinate a team mission.",
	}
	if err := mm.Create(mission); err != nil {
		t.Fatalf("create mission: %v", err)
	}
	teamCfg, err := mm.LoadTeamConfig("alpha-team")
	if err != nil {
		t.Fatalf("load team config: %v", err)
	}
	if err := mm.AssignToTeam("team-failover", "alpha-team", teamCfg); err != nil {
		t.Fatalf("assign team mission: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, "agents", "coverage-agent", "mission.yaml")); !os.IsNotExist(err) {
		t.Fatalf("coverage agent should not have mission copy before failover, err=%v", err)
	}

	comms := &recordingCommsClient{}
	signals := &recordingSignalSender{}
	audit := logs.NewWriter(home)

	CheckCoordinatorFailover(context.Background(), "coord-agent", Deps{
		MissionManager: mm,
		Claims:         orchestrate.NewMissionClaimRegistry(),
		Audit:          audit,
		Config:         &config.Config{Home: home, BuildID: "test-build"},
		Logger:         slog.Default(),
		Comms:          comms,
		Signal:         signals,
	})

	if _, err := os.Stat(filepath.Join(home, "agents", "coverage-agent", "mission.yaml")); err != nil {
		t.Fatalf("expected coverage mission copy after failover: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(signals.signals) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(signals.signals) != 1 || signals.signals[0] != "agency-coverage-agent-enforcer:SIGHUP" {
		t.Fatalf("expected coverage enforcer SIGHUP, got %v", signals.signals)
	}
	if len(comms.requests) != 1 {
		t.Fatalf("expected one operator comms alert, got %d", len(comms.requests))
	}
	if comms.requests[0].Method != http.MethodPost || comms.requests[0].Path != "/channels/operator/messages" {
		t.Fatalf("unexpected comms request: %+v", comms.requests[0])
	}
	content, _ := comms.requests[0].Body["content"].(string)
	if !strings.Contains(content, "coverage-agent") || !strings.Contains(content, "team-failover") {
		t.Fatalf("unexpected operator alert content: %q", content)
	}

	auditData, err := os.ReadFile(filepath.Join(home, "audit", "coord-agent", "gateway.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(auditData), `"event":"mission_coordinator_failover"`) {
		t.Fatalf("expected failover audit entry, got %s", auditData)
	}
}
