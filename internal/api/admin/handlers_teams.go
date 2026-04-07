package admin

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/logs"
)

// ── Teams ───────────────────────────────────────────────────────────────────

func (h *handler) listTeams(w http.ResponseWriter, r *http.Request) {
	teamsDir := filepath.Join(h.deps.Config.Home, "teams")
	entries, err := os.ReadDir(teamsDir)
	if err != nil {
		writeJSON(w, 200, []interface{}{})
		return
	}

	var teams []map[string]interface{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		team := map[string]interface{}{"name": e.Name()}
		teamPath := filepath.Join(teamsDir, e.Name(), "team.yaml")
		if data, err := os.ReadFile(teamPath); err == nil {
			var t map[string]interface{}
			if yaml.Unmarshal(data, &t) == nil {
				team = t
				if team["name"] == nil {
					team["name"] = e.Name()
				}
			}
		}
		// Count members
		if members, ok := team["members"].([]interface{}); ok {
			team["member_count"] = len(members)
		} else {
			team["member_count"] = 0
		}
		teams = append(teams, team)
	}
	writeJSON(w, 200, teams)
}

func (h *handler) createTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string   `json:"name"`
		Agents []string `json:"agents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if _, ok := requireName(w, body.Name); !ok {
		return
	}

	teamDir := filepath.Join(h.deps.Config.Home, "teams", body.Name)
	if err := os.MkdirAll(teamDir, 0755); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	team := map[string]interface{}{
		"name":    body.Name,
		"members": body.Agents,
	}
	data, _ := yaml.Marshal(team)
	if err := os.WriteFile(filepath.Join(teamDir, "team.yaml"), data, 0644); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	h.deps.Logger.Info("team created", "name", body.Name, "members", body.Agents)
	writeJSON(w, 201, map[string]string{"status": "created", "name": body.Name})
}

func (h *handler) showTeam(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	teamPath := filepath.Join(h.deps.Config.Home, "teams", name, "team.yaml")
	data, err := os.ReadFile(teamPath)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "team not found: " + name})
		return
	}
	var team map[string]interface{}
	if err := yaml.Unmarshal(data, &team); err != nil {
		writeJSON(w, 500, map[string]string{"error": "invalid team config"})
		return
	}
	writeJSON(w, 200, team)
}

func (h *handler) teamActivity(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}

	// Read team config to find members
	teamPath := filepath.Join(h.deps.Config.Home, "teams", name, "team.yaml")
	data, err := os.ReadFile(teamPath)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "team not found: " + name})
		return
	}
	var team map[string]interface{}
	yaml.Unmarshal(data, &team)

	// Aggregate audit logs from all team members
	members, _ := team["members"].([]interface{})
	var activity []map[string]interface{}
	reader := logs.NewReader(h.deps.Config.Home)
	for _, m := range members {
		memberName, ok := m.(string)
		if !ok {
			continue
		}
		events, err := reader.ReadAgentLog(memberName, "", "")
		if err != nil {
			continue
		}
		// Take last 20 events per member
		start := 0
		if len(events) > 20 {
			start = len(events) - 20
		}
		for _, e := range events[start:] {
			e["agent"] = memberName
			activity = append(activity, e)
		}
	}

	writeJSON(w, 200, activity)
}
