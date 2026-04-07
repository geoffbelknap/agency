package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	apiadmin "github.com/geoffbelknap/agency/internal/api/admin"
	apiagents "github.com/geoffbelknap/agency/internal/api/agents"
	apicomms "github.com/geoffbelknap/agency/internal/api/comms"
	"github.com/geoffbelknap/agency/internal/api/creds"
	apievents "github.com/geoffbelknap/agency/internal/api/events"
	"github.com/geoffbelknap/agency/internal/api/graph"
	apihub "github.com/geoffbelknap/agency/internal/api/hub"
	apiinfra "github.com/geoffbelknap/agency/internal/api/infra"
	apimissions "github.com/geoffbelknap/agency/internal/api/missions"
	"github.com/geoffbelknap/agency/internal/api/platform"
	"github.com/geoffbelknap/agency/internal/config"
)

// TestRouteWiring_AllModulesRegistered verifies that every API route module is
// wired into the chi router correctly. A 404 response means the route was not
// registered — that is the failure we are detecting.
//
// Handlers will often panic or return 5xx with nil deps; that is fine.
// A panic means the handler ran (route was found), which is a PASS for wiring.
func TestRouteWiring_AllModulesRegistered(t *testing.T) {
	r := chi.NewRouter()

	cfg := &config.Config{Version: "test", Token: "test-token"}

	// platform module — health, openapi, init, websocket, audit, config endpoint
	platform.RegisterRoutes(r, platform.Deps{
		Config: cfg,
	})

	// agents module — lifecycle, config, grants, budget, context, meeseeks, memory
	apiagents.RegisterRoutes(r, apiagents.Deps{
		Config: cfg,
	})

	// missions module — CRUD, assign, pause, resume, canvas
	apimissions.RegisterRoutes(r, apimissions.Deps{
		Config: cfg,
	})

	// graph module — knowledge query, ontology, export, review
	graph.RegisterRoutes(r, graph.Deps{
		Config: cfg,
	})

	// hub module — hub install/search/instances, connectors, presets, deploy
	apihub.RegisterRoutes(r, apihub.Deps{
		Config: cfg,
	})

	// comms module — channels, messages
	apicomms.RegisterRoutes(r, apicomms.Deps{
		Config: cfg,
	})

	// creds module — always register unconditionally in the wiring test
	creds.RegisterRoutes(r, creds.Deps{
		Config: cfg,
	})

	// events module — always register unconditionally in the wiring test
	apievents.RegisterRoutes(r, apievents.Deps{})

	// admin module — doctor, teams, capabilities, profiles, policy
	apiadmin.RegisterRoutes(r, apiadmin.Deps{
		Config: cfg,
	})

	// infra module — infra status, internal LLM, routing metrics, providers
	apiinfra.RegisterRoutes(r, apiinfra.Deps{
		Config: cfg,
	})

	// MCP tools — only routes still on the api-level struct
	mcpReg := NewMCPToolRegistry()
	d := &mcpDeps{cfg: cfg, mcpReg: mcpReg}
	r.Get("/api/v1/mcp/tools", mcpToolsHandler(d.mcpReg))
	r.Post("/api/v1/mcp/call", mcpCallHandler(d.mcpReg, d))

	routes := []struct {
		name   string
		method string
		path   string
	}{
		{"agents", "GET", "/api/v1/agents"},
		{"missions", "GET", "/api/v1/missions/"},
		{"graph_query", "POST", "/api/v1/graph/query"},
		{"hub_installed", "GET", "/api/v1/hub/installed"},
		{"channels", "GET", "/api/v1/comms/channels"},
		{"credentials", "GET", "/api/v1/creds"},
		{"events", "GET", "/api/v1/events"},
		{"admin_doctor", "GET", "/api/v1/admin/doctor"},
		{"infra_status", "GET", "/api/v1/infra/status"},
		{"health", "GET", "/api/v1/health"},
		{"mcp_tools", "GET", "/api/v1/mcp/tools"},
		{"registry", "GET", "/api/v1/admin/registry"},
		{"intake_items", "GET", "/api/v1/events/intake/items"},
	}

	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()

			// Use a deferred recover so that a handler panic (caused by nil deps)
			// is treated as a PASS — the route was found and the handler ran.
			// Only a 404 is a failure: that means the route was never registered.
			panicked := false
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						panicked = true
					}
				}()
				r.ServeHTTP(rr, req)
			}()

			if !panicked && rr.Code == http.StatusNotFound {
				t.Errorf("route %s %s returned 404 — module not wired into router", tc.method, tc.path)
			}
		})
	}
}
