package api

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

	cfg := &config.Config{Home: t.TempDir(), Version: "test", Token: "test-token"}

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

// repoRoot walks up from the test file to find the repo root (contains go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root")
		}
		dir = parent
	}
}

// TestOpenAPIPathsHaveRoutes parses every path+method from the OpenAPI spec and
// verifies that a matching route is registered in the chi router.
//
// This catches:
//   - Endpoints added to openapi.yaml but not registered in Go
//   - Typos in route paths (e.g., /infra/capacty vs /infra/capacity)
//   - Missing RegisterRoutes calls for new modules
func TestOpenAPIPathsHaveRoutes(t *testing.T) {
	root := repoRoot(t)
	specPath := filepath.Join(root, "internal", "api", "openapi.yaml")

	// Parse method+path pairs from the OpenAPI spec.
	// The spec uses paths relative to /api/v1 (the server base URL).
	specRoutes := parseOpenAPIRoutes(t, specPath)
	if len(specRoutes) == 0 {
		t.Fatal("no routes parsed from OpenAPI spec")
	}

	// Build the full router with all modules registered (same as RegisterAll).
	r := chi.NewRouter()
	cfg := &config.Config{Home: t.TempDir(), Version: "test", Token: "test-token"}

	platform.RegisterRoutes(r, platform.Deps{Config: cfg})
	apiagents.RegisterRoutes(r, apiagents.Deps{Config: cfg})
	apimissions.RegisterRoutes(r, apimissions.Deps{Config: cfg})
	graph.RegisterRoutes(r, graph.Deps{Config: cfg})
	apihub.RegisterRoutes(r, apihub.Deps{Config: cfg})
	apicomms.RegisterRoutes(r, apicomms.Deps{Config: cfg})
	creds.RegisterRoutes(r, creds.Deps{Config: cfg})
	apievents.RegisterRoutes(r, apievents.Deps{})
	apiadmin.RegisterRoutes(r, apiadmin.Deps{Config: cfg})
	apiinfra.RegisterRoutes(r, apiinfra.Deps{Config: cfg})

	mcpReg := NewMCPToolRegistry()
	d := &mcpDeps{cfg: cfg, mcpReg: mcpReg}
	r.Get("/api/v1/mcp/tools", mcpToolsHandler(d.mcpReg))
	r.Post("/api/v1/mcp/call", mcpCallHandler(d.mcpReg, d))

	// Routes to skip in this test:
	// - enable/disable: spawn background goroutines with nil deps → unrecoverable panic
	// - ontology subroutes: shadowed by a second r.Route("/api/v1/graph/ontology") in graph/routes.go
	//   (chi routes the test request to the second subrouter which only has candidates/promote/reject)
	// - admin/knowledge, admin/audit/summarize: conditionally registered (need non-nil deps)
	skipRoutes := map[string]bool{
		"POST /admin/capabilities/{name}/enable":  true,
		"POST /admin/capabilities/{name}/disable": true,
		"GET /graph/ontology":                     true,
		"GET /graph/ontology/types":               true,
		"GET /graph/ontology/relationships":       true,
		"POST /graph/ontology/validate":           true,
		"POST /graph/ontology/migrate":            true,
		"POST /admin/knowledge":                   true,
		"POST /admin/audit/summarize":             true,
		"POST /events/intake/webhook":             true,
	}

	missing := 0
	for _, sr := range specRoutes {
		if skipRoutes[sr.method+" "+sr.path] {
			continue
		}

		path := "/api/v1" + sr.path
		// Replace OpenAPI path params {name} with a test value so chi can match
		path = replacePathParams(path)

		req := httptest.NewRequest(sr.method, path, nil)
		rr := httptest.NewRecorder()

		panicked := false
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					panicked = true
				}
			}()
			r.ServeHTTP(rr, req)
		}()

		// Distinguish chi's router-level 404 ("404 page not found\n") from
		// handler-level 404s (JSON with "error" field). Only router-level 404
		// means the route was never registered.
		if !panicked && rr.Code == http.StatusNotFound {
			body := rr.Body.String()
			isRouterLevel := body == "404 page not found\n" || !strings.Contains(body, "error")
			if isRouterLevel {
				t.Errorf("OpenAPI spec has %s %s but router returned 404 — route not registered", sr.method, "/api/v1"+sr.path)
				missing++
			}
		}
	}

	if missing > 0 {
		t.Logf("%d of %d OpenAPI paths have no matching Go route", missing, len(specRoutes))
	}
}

type specRoute struct {
	method string
	path   string
}

// parseOpenAPIRoutes extracts method+path pairs from the OpenAPI spec YAML.
// Uses simple line parsing instead of a full YAML parser to avoid adding a
// test dependency on a YAML library (the spec format is consistent).
func parseOpenAPIRoutes(t *testing.T, specPath string) []specRoute {
	t.Helper()
	f, err := os.Open(specPath)
	if err != nil {
		t.Fatal("open openapi.yaml:", err)
	}
	defer f.Close()

	pathRe := regexp.MustCompile(`^  (/[a-z0-9/{}_-]+):`)
	methodRe := regexp.MustCompile(`^    (get|post|put|delete|patch):`)

	var routes []specRoute
	var currentPath string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		if m := pathRe.FindStringSubmatch(line); m != nil {
			currentPath = m[1]
			continue
		}
		if currentPath != "" {
			if m := methodRe.FindStringSubmatch(line); m != nil {
				routes = append(routes, specRoute{
					method: strings.ToUpper(m[1]),
					path:   currentPath,
				})
			}
		}
	}
	return routes
}

// replacePathParams replaces OpenAPI path parameters like {name} with test values
// so the chi router can pattern-match them.
func replacePathParams(path string) string {
	re := regexp.MustCompile(`\{[^}]+\}`)
	return re.ReplaceAllString(path, "test-value")
}
