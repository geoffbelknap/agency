package api

// validation_test.go covers integration-level validation areas for the API layer:
//
//  1. Auth validation  — hardened BearerAuth middleware edge cases
//  2. Config token auto-generation — config.Load() generates and persists tokens
//  3. Module isolation — RegisterRoutes does not panic with all-nil deps
//  4. Conditional registration — optional deps change handler behavior (not wiring)
//  5. Unauthenticated paths — exact set of exempt paths
//
// TestRouteWiring_AllModulesRegistered is in routes_wiring_test.go — not duplicated.
// TestBearerAuth covering core auth cases is in middleware_auth_test.go — not duplicated.
// TestStartup_NilDocker_ReturnsError is in startup_test.go — not duplicated.
//
// TestNonLocalhostWarning is excluded — it is a log statement in main.go that
// cannot be meaningfully asserted in a unit test. Manual verification required.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

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

// ─── 1. Auth validation ──────────────────────────────────────────────────────

// TestAuth_Validation supplements TestBearerAuth (middleware_auth_test.go) with
// additional edge cases: websocket path, agency config path, and empty-token fail-closed
// checks against additional non-exempt paths.
func TestAuth_Validation(t *testing.T) {
	const testToken = "test-token-auth-validation"
	const egressToken = "test-egress-token-auth-validation"

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name       string
		cfgToken   string
		egressTok  string
		path       string
		method     string
		authHeader string
		wantCode   int
	}{
		// Exempt paths — no token required
		{
			name:     "websocket path bypasses auth",
			cfgToken: testToken, egressTok: egressToken,
			path: "/ws", method: http.MethodGet,
			wantCode: http.StatusOK,
		},
		{
			name:     "agent websocket path does not bypass auth",
			cfgToken: testToken, egressTok: egressToken,
			path: "/api/v1/agents/test-agent/context/ws", method: http.MethodGet,
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "agency config endpoint bypasses auth",
			cfgToken: testToken, egressTok: egressToken,
			path: "/__agency/config", method: http.MethodGet,
			wantCode: http.StatusOK,
		},
		{
			name:     "health endpoint bypasses auth",
			cfgToken: testToken, egressTok: egressToken,
			path: "/api/v1/health", method: http.MethodGet,
			wantCode: http.StatusOK,
		},

		// Empty config token — fail-closed for non-exempt paths
		{
			name:     "empty config token rejects non-exempt path",
			cfgToken: "", egressTok: "",
			path: "/api/v1/agents", method: http.MethodGet,
			authHeader: "",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:     "empty config token still allows health",
			cfgToken: "", egressTok: "",
			path: "/api/v1/health", method: http.MethodGet,
			wantCode: http.StatusOK,
		},
		{
			name:     "empty config token still allows websocket",
			cfgToken: "", egressTok: "",
			path: "/ws", method: http.MethodGet,
			wantCode: http.StatusOK,
		},
		{
			name:     "empty config token still allows agency config",
			cfgToken: "", egressTok: "",
			path: "/__agency/config", method: http.MethodGet,
			wantCode: http.StatusOK,
		},

		// Valid bearer token grants access
		{
			name:     "valid bearer token on agents endpoint",
			cfgToken: testToken, egressTok: egressToken,
			path: "/api/v1/agents", method: http.MethodGet,
			authHeader: "Bearer " + testToken,
			wantCode:   http.StatusOK,
		},

		// Wrong token — rejected
		{
			name:     "wrong token is rejected with 401",
			cfgToken: testToken, egressTok: egressToken,
			path: "/api/v1/missions", method: http.MethodGet,
			authHeader: "Bearer notthetoken",
			wantCode:   http.StatusUnauthorized,
		},

		// Scoped egress token — already covered in middleware_auth_test.go,
		// but we include the key invariants here for completeness.
		{
			name:     "egress token on correct endpoint and method",
			cfgToken: testToken, egressTok: egressToken,
			path: "/api/v1/creds/internal/resolve", method: http.MethodGet,
			authHeader: "Bearer " + egressToken,
			wantCode:   http.StatusOK,
		},
		{
			name:     "egress token on wrong endpoint returns 403 not 401",
			cfgToken: testToken, egressTok: egressToken,
			path: "/api/v1/agents", method: http.MethodGet,
			authHeader: "Bearer " + egressToken,
			wantCode:   http.StatusForbidden,
		},
		{
			name:     "egress token with POST on cred resolve returns 403",
			cfgToken: testToken, egressTok: egressToken,
			path: "/api/v1/creds/internal/resolve", method: http.MethodPost,
			authHeader: "Bearer " + egressToken,
			wantCode:   http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			middleware := BearerAuth(tc.cfgToken, tc.egressTok, nil)
			handler := middleware(ok)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tc.wantCode {
				t.Errorf("path=%s method=%s: got status %d, want %d", tc.path, tc.method, rr.Code, tc.wantCode)
			}
		})
	}
}

// ─── 2. Config token auto-generation ─────────────────────────────────────────

// TestConfig_TokenAutoGeneration verifies that config.Load() auto-generates
// tokens when they are absent and persists them to disk.
//
// config.Load() performs auto-generation for both missing and incomplete
// config.yaml files so first-run CLI and daemon auth stay in sync.
func TestConfig_TokenAutoGeneration(t *testing.T) {
	t.Run("empty config.yaml produces non-empty Token", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("AGENCY_HOME", home)

		// Create a minimal (but valid) config.yaml so Load() doesn't return early.
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("{}\n"), 0600); err != nil {
			t.Fatalf("setup: %v", err)
		}

		cfg := config.Load()
		if cfg.Token == "" {
			t.Error("expected auto-generated Token, got empty string")
		}
	})

	t.Run("empty config.yaml produces non-empty EgressToken", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("AGENCY_HOME", home)

		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("{}\n"), 0600); err != nil {
			t.Fatalf("setup: %v", err)
		}

		cfg := config.Load()
		if cfg.EgressToken == "" {
			t.Error("expected auto-generated EgressToken, got empty string")
		}
	})

	t.Run("Token and EgressToken are distinct", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("AGENCY_HOME", home)

		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("{}\n"), 0600); err != nil {
			t.Fatalf("setup: %v", err)
		}

		cfg := config.Load()
		if cfg.Token == cfg.EgressToken {
			t.Errorf("Token and EgressToken must differ, both are %q", cfg.Token)
		}
	})

	t.Run("tokens are persisted to config.yaml", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("AGENCY_HOME", home)

		cfgPath := filepath.Join(home, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte("{}\n"), 0600); err != nil {
			t.Fatalf("setup: %v", err)
		}

		first := config.Load()

		// Re-read the YAML file to confirm the token was written.
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("reading config.yaml after Load: %v", err)
		}

		var cf struct {
			Token       string `yaml:"token"`
			EgressToken string `yaml:"egress_token"`
		}
		if err := yaml.Unmarshal(data, &cf); err != nil {
			t.Fatalf("parsing config.yaml: %v", err)
		}

		if cf.Token == "" {
			t.Error("token not persisted to config.yaml")
		}
		if cf.Token != first.Token {
			t.Errorf("persisted token %q differs from in-memory token %q", cf.Token, first.Token)
		}
		if cf.EgressToken != first.EgressToken {
			t.Errorf("persisted egress_token %q differs from in-memory EgressToken %q", cf.EgressToken, first.EgressToken)
		}
	})

	t.Run("existing token in config.yaml is not overwritten", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("AGENCY_HOME", home)

		const myToken = "my-existing-token-do-not-overwrite"
		cfgPath := filepath.Join(home, "config.yaml")
		initial := map[string]string{"token": myToken}
		data, _ := yaml.Marshal(initial)
		if err := os.WriteFile(cfgPath, data, 0600); err != nil {
			t.Fatalf("setup: %v", err)
		}

		cfg := config.Load()
		if cfg.Token != myToken {
			t.Errorf("existing token was overwritten: got %q, want %q", cfg.Token, myToken)
		}
	})

	t.Run("missing config.yaml creates persisted non-empty tokens", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "new-agency-home")
		t.Setenv("AGENCY_HOME", home)

		cfg := config.Load()
		if cfg == nil {
			t.Fatal("config.Load() returned nil")
		}
		if cfg.Token == "" {
			t.Fatal("expected non-empty token on first run")
		}
		if cfg.EgressToken == "" {
			t.Fatal("expected non-empty egress token on first run")
		}
		if _, err := os.Stat(filepath.Join(home, "config.yaml")); err != nil {
			t.Fatalf("expected config.yaml to be created: %v", err)
		}
	})
}

// ─── 3. Module isolation ──────────────────────────────────────────────────────

// TestModuleIsolation verifies that each module's RegisterRoutes does not panic
// during route registration when deps are all-nil (only Config is populated).
//
// Panics only occur when handlers actually run, not during registration.
// A panic here means a module is accessing deps at registration time, which is wrong.
func TestModuleIsolation(t *testing.T) {
	cfg := &config.Config{Home: t.TempDir(), Version: "test", Token: "test-token"}

	modules := []struct {
		name string
		fn   func()
	}{
		{
			name: "platform — nil optional deps",
			fn: func() {
				r := chi.NewRouter()
				platform.RegisterRoutes(r, platform.Deps{Config: cfg})
			},
		},
		{
			name: "agents — nil optional deps",
			fn: func() {
				r := chi.NewRouter()
				apiagents.RegisterRoutes(r, apiagents.Deps{Config: cfg})
			},
		},
		{
			name: "missions — nil optional deps",
			fn: func() {
				r := chi.NewRouter()
				apimissions.RegisterRoutes(r, apimissions.Deps{Config: cfg})
			},
		},
		{
			name: "graph — nil optional deps",
			fn: func() {
				r := chi.NewRouter()
				graph.RegisterRoutes(r, graph.Deps{Config: cfg})
			},
		},
		{
			name: "hub — nil optional deps",
			fn: func() {
				r := chi.NewRouter()
				apihub.RegisterRoutes(r, apihub.Deps{Config: cfg})
			},
		},
		{
			name: "comms — nil optional deps",
			fn: func() {
				r := chi.NewRouter()
				apicomms.RegisterRoutes(r, apicomms.Deps{Config: cfg})
			},
		},
		{
			name: "creds — nil optional deps",
			fn: func() {
				r := chi.NewRouter()
				creds.RegisterRoutes(r, creds.Deps{Config: cfg})
			},
		},
		{
			name: "events — all nil deps",
			fn: func() {
				r := chi.NewRouter()
				apievents.RegisterRoutes(r, apievents.Deps{})
			},
		},
		{
			name: "admin — nil optional deps",
			fn: func() {
				r := chi.NewRouter()
				apiadmin.RegisterRoutes(r, apiadmin.Deps{Config: cfg})
			},
		},
		{
			name: "infra — nil optional deps",
			fn: func() {
				r := chi.NewRouter()
				apiinfra.RegisterRoutes(r, apiinfra.Deps{Config: cfg})
			},
		},
	}

	for _, m := range modules {
		t.Run(m.name, func(t *testing.T) {
			panicked := false
			var panicVal any
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						panicked = true
						panicVal = rec
					}
				}()
				m.fn()
			}()
			if panicked {
				t.Errorf("RegisterRoutes panicked during registration (not during handler execution): %v", panicVal)
			}
		})
	}
}

// ─── 4. Conditional behavior (nil deps change handler response, not wiring) ──

// TestConditionalRegistration verifies the behavior of handlers when optional deps
// are nil. Routes are always registered; nil deps change what the handler returns.
//
// - Events module: when EventBus is nil, listEvents returns 503 (not wired away)
// - Creds module: when CredStore is nil, credential handlers return 500/503
func TestConditionalRegistration(t *testing.T) {
	cfg := &config.Config{Home: t.TempDir(), Version: "test", Token: "test-token"}

	t.Run("events route registered regardless of nil EventBus", func(t *testing.T) {
		r := chi.NewRouter()
		// Register with nil EventBus — routes are still wired.
		apievents.RegisterRoutes(r, apievents.Deps{EventBus: nil})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
		rr := httptest.NewRecorder()

		func() {
			defer func() { recover() }()
			r.ServeHTTP(rr, req)
		}()

		// Route is registered — handler runs and returns non-404.
		// Handler should return 503 (event bus not initialized), never 404.
		if rr.Code == http.StatusNotFound {
			t.Error("events route returned 404 — route is not registered, but it should always be")
		}
	})

	t.Run("creds route registered regardless of nil CredStore", func(t *testing.T) {
		r := chi.NewRouter()
		// Register with nil CredStore.
		creds.RegisterRoutes(r, creds.Deps{Config: cfg, CredStore: nil})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/creds", nil)
		rr := httptest.NewRecorder()

		func() {
			defer func() { recover() }()
			r.ServeHTTP(rr, req)
		}()

		// Route is registered — handler ran (not a 404).
		if rr.Code == http.StatusNotFound {
			t.Error("creds route returned 404 — route is not registered, but it should always be")
		}
	})

	t.Run("platform websocket route skipped when WSHub is nil", func(t *testing.T) {
		// Platform module conditionally skips /ws when WSHub is nil.
		// This is the one case of true conditional route registration.
		r := chi.NewRouter()
		platform.RegisterRoutes(r, platform.Deps{Config: cfg, WSHub: nil})

		req := httptest.NewRequest(http.MethodGet, "/ws", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected /ws to return 404 when WSHub is nil, got %d", rr.Code)
		}
	})
}

// ─── 5. Unauthenticated paths ─────────────────────────────────────────────────

// TestUnauthenticatedPaths verifies the exact set of paths that bypass auth.
// Any path not in this list must require a valid token.
func TestUnauthenticatedPaths(t *testing.T) {
	const token = "test-token-for-unauth-paths"

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := BearerAuth(token, "", nil)
	handler := middleware(ok)

	// These paths must be accessible without a token.
	exemptPaths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/health"},
		{http.MethodGet, "/__agency/config"},
		{http.MethodGet, "/ws"},
	}

	for _, ep := range exemptPaths {
		t.Run("exempt: "+ep.method+" "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			// No auth header — should still succeed.
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("%s %s without token: got %d, want 200", ep.method, ep.path, rr.Code)
			}
		})
	}

	// These paths must require a valid token.
	protectedPaths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/agents"},
		{http.MethodGet, "/api/v1/missions/"},
		{http.MethodGet, "/api/v1/creds/"},
		{http.MethodGet, "/api/v1/events"},
		{http.MethodGet, "/api/v1/graph/query"},
		{http.MethodGet, "/api/v1/admin/registry"},
		{http.MethodGet, "/api/v1/infra/status"},
		{http.MethodGet, "/api/v1/admin/doctor"},
	}

	for _, pp := range protectedPaths {
		t.Run("protected: "+pp.method+" "+pp.path, func(t *testing.T) {
			// Without token — must be rejected.
			req := httptest.NewRequest(pp.method, pp.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code == http.StatusOK {
				t.Errorf("%s %s without token: got 200, want 401", pp.method, pp.path)
			}
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("%s %s without token: got %d, want 401", pp.method, pp.path, rr.Code)
			}

			// With valid token — must be allowed through middleware.
			req2 := httptest.NewRequest(pp.method, pp.path, nil)
			req2.Header.Set("Authorization", "Bearer "+token)
			rr2 := httptest.NewRecorder()

			panicked := false
			func() {
				defer func() {
					if recover() != nil {
						panicked = true
					}
				}()
				handler.ServeHTTP(rr2, req2)
			}()

			// A panic means the handler ran (middleware passed it through) — PASS.
			// A 404 from middleware would never happen (middleware is transparent for the route path).
			// We just verify the middleware didn't return 401 with a valid token.
			if !panicked && rr2.Code == http.StatusUnauthorized {
				t.Errorf("%s %s with valid token: got 401, middleware should have passed it through", pp.method, pp.path)
			}
		})
	}
}
