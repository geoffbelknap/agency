package admin

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/policy"
)

func (h *handler) showPolicy(w http.ResponseWriter, r *http.Request) {
	agent, ok := requireName(w, chi.URLParam(r, "agent"))
	if !ok {
		return
	}
	eng := policy.NewEngine(h.deps.Config.Home)
	ep := eng.Show(agent)
	writeJSON(w, 200, ep)
}

func (h *handler) validatePolicy(w http.ResponseWriter, r *http.Request) {
	agent, ok := requireName(w, chi.URLParam(r, "agent"))
	if !ok {
		return
	}
	eng := policy.NewEngine(h.deps.Config.Home)
	ep := eng.Validate(agent)

	// Additionally enforce hard floors on the agent's constraints.yaml
	// to prevent saving policies that violate immutable safety guarantees.
	constraintsPath := filepath.Join(h.deps.Config.Home, "agents", agent, "constraints.yaml")
	if data, err := os.ReadFile(constraintsPath); err == nil {
		var constraints map[string]interface{}
		if yaml.Unmarshal(data, &constraints) == nil {
			if err := policy.ValidatePolicy(constraints); err != nil {
				ep.Valid = false
				ep.Violations = append(ep.Violations, err.Error())
			}
		}
	}

	if !ep.Valid {
		writeJSON(w, 400, ep)
		return
	}
	writeJSON(w, 200, ep)
}
