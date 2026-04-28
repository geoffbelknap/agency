package infra

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoffbelknap/agency/internal/config"
)

func TestCapacity_UsesRuntimeContainerLabels(t *testing.T) {
	workspace := workspaceCapacityFilters()
	if !workspace.MatchKVList("label", map[string]string{"agency.type": "workspace"}) {
		t.Fatal("workspace capacity filters should match agency.type=workspace")
	}
	if workspace.MatchKVList("label", map[string]string{"agency.role": "workspace"}) {
		t.Fatal("workspace capacity filters should not use legacy agency.role label")
	}

	meeseeks := meeseeksCapacityFilters()
	if !meeseeks.MatchKVList("label", map[string]string{"agency.type": "meeseeks-workspace"}) {
		t.Fatal("meeseeks capacity filters should match agency.type=meeseeks-workspace")
	}
	if meeseeks.MatchKVList("label", map[string]string{"agency.role": "meeseeks"}) {
		t.Fatal("meeseeks capacity filters should not use legacy agency.role label")
	}
}

func TestCapacity_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	h := &handler{deps: Deps{
		Config: &config.Config{Home: tmp},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/infra/capacity", nil)
	rec := httptest.NewRecorder()

	h.infraCapacity(rec, req)

	if rec.Code != 503 {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] == "" {
		t.Error("expected non-empty error field")
	}
}

func TestCapacity_OK(t *testing.T) {
	tmp := t.TempDir()
	capYAML := `host_memory_mb: 16384
host_cpu_cores: 8
system_reserve_mb: 3276
infra_overhead_mb: 1264
max_agents: 18
max_concurrent_meesks: 18
agent_slot_mb: 640
meeseeks_slot_mb: 640
network_pool_configured: true
`
	if err := os.WriteFile(filepath.Join(tmp, "capacity.yaml"), []byte(capYAML), 0644); err != nil {
		t.Fatal(err)
	}

	h := &handler{deps: Deps{
		Config: &config.Config{Home: tmp},
		// Runtime is nil — container counts will be 0
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/infra/capacity", nil)
	rec := httptest.NewRecorder()

	h.infraCapacity(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	checks := map[string]float64{
		"host_memory_mb":        16384,
		"host_cpu_cores":        8,
		"system_reserve_mb":     3276,
		"infra_overhead_mb":     1264,
		"max_agents":            18,
		"max_concurrent_meesks": 18,
		"agent_slot_mb":         640,
		"meeseeks_slot_mb":      640,
		"running_agents":        0,
		"running_meeseeks":      0,
		"available_slots":       18,
	}
	for k, want := range checks {
		got, ok := body[k].(float64)
		if !ok {
			t.Errorf("field %q missing or not a number", k)
			continue
		}
		if got != want {
			t.Errorf("field %q: want %v, got %v", k, want, got)
		}
	}

	if npc, ok := body["network_pool_configured"].(bool); !ok || !npc {
		t.Errorf("expected network_pool_configured=true, got %v", body["network_pool_configured"])
	}
}
