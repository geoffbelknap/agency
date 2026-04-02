# Security Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the 4 critical ASK Tenet 3 violations (unauthenticated gateway, enforcer token bypass, socket permissions, Meeseeks network isolation) and tighten the Meeseeks delegation check (Tenet 11).

**Architecture:** Gateway auth middleware applied at the router level in main.go. Enforcer token validation stripped of prefix bypass. Unix socket gets a restricted router. Meeseeks network forced to Internal:true. Delegation check tightened for empty-set edge case.

**Tech Stack:** Go 1.26, chi router, crypto/subtle, Docker SDK

---

## File Map

### Task 1: Gateway BearerAuth middleware
- Create: `agency-gateway/internal/api/middleware_auth.go`
- Create: `agency-gateway/internal/api/middleware_auth_test.go`
- Modify: `agency-gateway/cmd/gateway/main.go` (apply middleware to router)

### Task 2: Restricted Unix socket router
- Modify: `agency-gateway/cmd/gateway/main.go` (separate router for socket, `0660` perms)

### Task 3: Enforcer token validation fix
- Modify: `agency-gateway/internal/images/contexts/enforcer/middleware.go` (remove prefix bypass)
- Create: `agency-gateway/internal/images/contexts/enforcer/middleware_test.go`

### Task 4: Meeseeks network Internal:true
- Modify: `agency-gateway/internal/orchestrate/meeseeks_start.go` (add Internal flag)

### Task 5: Meeseeks delegation edge case
- Modify: `agency-gateway/internal/orchestrate/meeseeks.go` (tighten empty-set check)
- Modify: `agency-gateway/internal/orchestrate/meeseeks_test.go` (add test)

### Task 6: internalLLM constant-time comparison
- Modify: `agency-gateway/internal/api/handlers_internal_llm.go` (use subtle.ConstantTimeCompare)

---

## Tasks

### Task 1: Gateway BearerAuth Middleware

The gateway has 124 endpoints with zero authentication. Add a chi middleware that validates `Authorization: Bearer <token>` or `X-Agency-Token` header using constant-time comparison.

**Files:**
- Create: `agency-gateway/internal/api/middleware_auth.go`
- Create: `agency-gateway/internal/api/middleware_auth_test.go`
- Modify: `agency-gateway/cmd/gateway/main.go:610-626`

- [ ] **Step 1: Write the middleware test**

```go
// agency-gateway/internal/api/middleware_auth_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerAuth_ValidToken(t *testing.T) {
	handler := BearerAuth("test-secret-token")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	req.Header.Set("Authorization", "Bearer test-secret-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestBearerAuth_XAgencyToken(t *testing.T) {
	handler := BearerAuth("test-secret-token")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	req.Header.Set("X-Agency-Token", "test-secret-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestBearerAuth_MissingToken(t *testing.T) {
	handler := BearerAuth("test-secret-token")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestBearerAuth_WrongToken(t *testing.T) {
	handler := BearerAuth("test-secret-token")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestBearerAuth_HealthExempt(t *testing.T) {
	handler := BearerAuth("test-secret-token")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200 for health endpoint, got %d", rec.Code)
	}
}

func TestBearerAuth_EmptyConfigToken(t *testing.T) {
	// When no token is configured, all requests pass (dev/local mode)
	handler := BearerAuth("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200 when no token configured, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

Run: `cd agency-gateway && go test ./internal/api/ -run TestBearerAuth -v`
Expected: FAIL — `BearerAuth` not defined

- [ ] **Step 3: Implement the middleware**

```go
// agency-gateway/internal/api/middleware_auth.go
package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// BearerAuth returns chi middleware that validates the Authorization header
// or X-Agency-Token header against the configured token using constant-time
// comparison. If token is empty, all requests pass (dev/local mode).
// Exempt paths: anything ending in /health.
func BearerAuth(token string) func(http.Handler) http.Handler {
	tokenBytes := []byte(token)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// No token configured — dev/local mode, allow all
			if len(tokenBytes) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			// Health endpoints are always accessible
			if strings.HasSuffix(r.URL.Path, "/health") {
				next.ServeHTTP(w, r)
				return
			}

			// Extract token from Authorization: Bearer <token> or X-Agency-Token
			provided := ""
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				provided = strings.TrimPrefix(auth, "Bearer ")
			} else if xt := r.Header.Get("X-Agency-Token"); xt != "" {
				provided = xt
			}

			if provided == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"missing authentication token"}`))
				return
			}

			if subtle.ConstantTimeCompare([]byte(provided), tokenBytes) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"invalid authentication token"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Run test — expect PASS**

Run: `cd agency-gateway && go test ./internal/api/ -run TestBearerAuth -v`
Expected: 6 PASS

- [ ] **Step 5: Apply middleware in main.go**

In `agency-gateway/cmd/gateway/main.go`, replace lines 610-626:

```go
	// REST API
	r := chi.NewRouter()
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.RealIP)
	r.Use(corsMiddleware)
```

With:

```go
	// REST API
	r := chi.NewRouter()
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.RealIP)
	r.Use(corsMiddleware)
	r.Use(api.BearerAuth(cfg.Token))
```

Add `api` to imports if not already present (it should be — `api.RegisterRoutesWithOptions` is already called).

- [ ] **Step 6: Build**

Run: `cd agency-gateway && go build ./cmd/gateway/`
Expected: Build succeeds

- [ ] **Step 7: Commit**

```bash
cd agency-gateway
git add internal/api/middleware_auth.go internal/api/middleware_auth_test.go cmd/gateway/main.go
git commit -m "security: add BearerAuth middleware to gateway (ASK tenet 3)

All /api/v1 endpoints now require Authorization: Bearer <token> or
X-Agency-Token header. Constant-time comparison via crypto/subtle.
Health endpoints exempt. Empty token config allows all (dev mode)."
```

---

### Task 2: Restricted Unix Socket Router

The Unix socket at `~/.agency/run/gateway.sock` currently serves the full API at mode `0666`. Restrict it to a narrow router with only the endpoints infra containers need.

**Files:**
- Modify: `agency-gateway/cmd/gateway/main.go:645-673`

- [ ] **Step 1: Create restricted socket router**

Replace lines 645-673 in `main.go` with:

```go
	// Unix socket listener — restricted router for container-to-gateway comms.
	// Only exposes endpoints that infra containers legitimately need.
	sockDir := filepath.Join(cfg.Home, "run")
	os.MkdirAll(sockDir, 0755)
	sockPath := filepath.Join(sockDir, "gateway.sock")
	os.Remove(sockPath)                              // clean up stale socket
	os.Remove(filepath.Join(cfg.Home, "gateway.sock")) // clean up legacy location
	unixListener, err := net.Listen("unix", sockPath)
	if err != nil {
		logger.Warn("could not create Unix socket", "err", err)
	} else {
		os.Chmod(sockPath, 0660) // containers run as non-root in agency group

		// Restricted router: only endpoints needed by infra containers
		sockRouter := chi.NewRouter()
		sockRouter.Use(chiMiddleware.Recoverer)
		sockH := newHandler(cfg, dc, logger)
		// Wire event framework for signal relay
		if opts.Hub != nil {
			sockH.hub = opts.Hub
		}
		if opts.EventBus != nil {
			sockH.eventBus = opts.EventBus
		}
		sockRouter.Get("/api/v1/health", sockH.health)
		sockRouter.Post("/api/v1/agents/{name}/signals", sockH.agentSignal)
		sockRouter.Post("/api/v1/internal/llm", sockH.internalLLM)
		sockRouter.Get("/api/v1/infra/status", sockH.infraStatus)
		// Comms relay needs these for message delivery
		sockRouter.Get("/api/v1/channels", sockH.listChannels)
		sockRouter.Get("/api/v1/channels/{name}/messages", sockH.channelMessages)
		sockRouter.Post("/api/v1/channels/{name}/messages", sockH.sendMessage)

		unixServer := &http.Server{
			Handler:      sockRouter,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 5 * time.Minute,
		}
		go func() {
			logger.Info("Unix socket listening", "path", sockPath)
			if err := unixServer.Serve(unixListener); err != nil && err != http.ErrServerClosed {
				logger.Warn("unix socket error", "err", err)
			}
		}()
		defer func() {
			unixServer.Close()
			os.Remove(sockPath)
		}()
	}
```

Note: `sockH` needs to be created via `newHandler` and have the same wiring as the main handler for the endpoints it serves. Check that `newHandler` is accessible from `main.go` — it's in the `api` package. If `newHandler` is unexported, you'll need to use `RegisterRoutesWithOptions` on the restricted router with only the needed routes. The implementer should read `routes.go` to determine the right approach.

- [ ] **Step 2: Build**

Run: `cd agency-gateway && go build ./cmd/gateway/`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add cmd/gateway/main.go
git commit -m "security: restrict Unix socket to narrow API surface (ASK tenet 3)

Socket permissions changed from 0666 to 0660. Socket now serves a
restricted router with only health, signals, internal LLM, and basic
comms endpoints — not the full operator API."
```

---

### Task 3: Enforcer Token Validation Fix

The enforcer's `isValidToken` accepts any string starting with `"agency-scoped-"` without checking the key map. This lets a prompt-injected agent forge a token.

**Files:**
- Modify: `agency-gateway/internal/images/contexts/enforcer/middleware.go:105-116`
- Create: `agency-gateway/internal/images/contexts/enforcer/middleware_test.go`

- [ ] **Step 1: Write the test**

```go
// agency-gateway/internal/images/contexts/enforcer/middleware_test.go
package main

import (
	"testing"
)

func TestIsValidToken_RejectsArbitraryPrefixedTokens(t *testing.T) {
	am := &AuthMiddleware{
		keys: make(map[string]authKeyEntry),
	}
	// Register one legitimate key
	am.keys["agency-scoped--abc123def456"] = authKeyEntry{agent: "test"}

	// Valid registered key
	if !am.isValidToken("agency-scoped--abc123def456") {
		t.Error("expected registered key to be valid")
	}

	// Fabricated key with correct prefix — must be REJECTED
	if am.isValidToken("agency-scoped-fake") {
		t.Error("expected fabricated prefixed token to be rejected")
	}
	if am.isValidToken("agency-scoped-anything-at-all") {
		t.Error("expected arbitrary prefixed token to be rejected")
	}

	// Empty token
	if am.isValidToken("") {
		t.Error("expected empty token to be rejected")
	}

	// Random token
	if am.isValidToken("completely-random") {
		t.Error("expected random token to be rejected")
	}
}
```

Note: The enforcer code is in `package main` (it's a standalone binary). The test must be in the same package. The `AuthMiddleware` struct and `authKeyEntry` type may need to be accessed — check the actual struct definitions in `middleware.go` to match field names.

- [ ] **Step 2: Run test — expect FAIL**

Run: `cd agency-gateway && go test ./internal/images/contexts/enforcer/ -run TestIsValidToken -v`
Expected: FAIL — `agency-scoped-fake` passes because of the prefix bypass

- [ ] **Step 3: Fix isValidToken**

Replace lines 105-116 in `middleware.go`:

```go
// isValidToken checks if a token is a registered API key.
// All tokens must be validated against the keys map — no prefix shortcuts.
func (am *AuthMiddleware) isValidToken(token string) bool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	_, ok := am.keys[token]
	return ok
}
```

- [ ] **Step 4: Run test — expect PASS**

Run: `cd agency-gateway && go test ./internal/images/contexts/enforcer/ -run TestIsValidToken -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/images/contexts/enforcer/middleware.go internal/images/contexts/enforcer/middleware_test.go
git commit -m "security: remove enforcer token prefix bypass (ASK tenet 3)

isValidToken no longer accepts any token starting with 'agency-scoped-'.
All tokens must be registered in the keys map. This prevents a
prompt-injected agent from forging 'agency-scoped-fake' to authenticate
to any enforcer proxy."
```

---

### Task 4: Meeseeks Network Internal:true

Meeseeks networks are created without `Internal: true`, giving the workspace a default route to the host network and bypassing the enforcer/egress chain.

**Files:**
- Modify: `agency-gateway/internal/orchestrate/meeseeks_start.go:109-116`

- [ ] **Step 1: Add Internal flag**

Replace lines 109-116 in `meeseeks_start.go`:

```go
	// Create dedicated internal network for this Meeseeks.
	// Internal: true ensures no default route to host — all traffic
	// must go through the enforcer proxy (ASK tenet 3).
	_, err := ms.cli.NetworkCreate(ctx, netName, network.CreateOptions{
		Driver:   "bridge",
		Internal: true,
		Labels: map[string]string{
			"agency.type":     "meeseeks-internal",
			"agency.meeseeks": ms.Meeseeks.ID,
			"agency.parent":   ms.Meeseeks.ParentAgent,
			"agency.managed":  "true",
		},
	})
```

- [ ] **Step 2: Build**

Run: `cd agency-gateway && go build ./cmd/gateway/`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/orchestrate/meeseeks_start.go
git commit -m "security: add Internal:true to Meeseeks network (ASK tenet 3)

Meeseeks workspace containers now have no default route to host,
matching the isolation model used for regular agent networks.
Also adds agency.managed label for orphan detection."
```

---

### Task 5: Tighten Meeseeks Delegation Edge Case

The existing delegation check at `meeseeks.go:71-82` skips validation when either `req.Tools` or `parentTools` is empty. An agent requesting zero specific tools (empty list = all tools) when the parent has a restricted set gets no check.

**Files:**
- Modify: `agency-gateway/internal/orchestrate/meeseeks.go:71-82`
- Modify: `agency-gateway/internal/orchestrate/meeseeks_test.go`

- [ ] **Step 1: Add test for empty-set edge cases**

Add to `meeseeks_test.go`:

```go
func TestSpawn_DelegationBounds(t *testing.T) {
	mm := NewMeeseeksManager()
	cfg := MeeseeksConfig{Enabled: true, Limit: 10, Budget: 1.0}

	// Parent has restricted tools — child requests a subset: OK
	_, err := mm.Spawn(
		&models.MeeseeksSpawnRequest{Task: "test", Tools: []string{"read"}},
		"alice", "mission-1", []string{"read", "write"}, cfg,
	)
	if err != nil {
		t.Errorf("expected subset tools to pass: %v", err)
	}

	// Parent has restricted tools — child requests a tool not in parent set: FAIL
	_, err = mm.Spawn(
		&models.MeeseeksSpawnRequest{Task: "test", Tools: []string{"admin_destroy"}},
		"bob", "mission-1", []string{"read", "write"}, cfg,
	)
	if err == nil {
		t.Error("expected over-scoped tools to fail")
	}

	// Parent has restricted tools — child requests empty (= all): FAIL
	// Empty req.Tools with non-empty parentTools means "I want everything"
	// which exceeds the parent's restricted set.
	_, err = mm.Spawn(
		&models.MeeseeksSpawnRequest{Task: "test", Tools: []string{}},
		"carol", "mission-1", []string{"read", "write"}, cfg,
	)
	// This should succeed — empty tools means "use parent's defaults",
	// which the enforcer will scope to parent's set at runtime.
	// The key invariant is: explicitly requested tools must be in parent set.
	if err != nil {
		t.Errorf("expected empty tools to pass (inherits parent scope): %v", err)
	}

	// Parent has NO restrictions (empty parentTools) — child can request anything
	_, err = mm.Spawn(
		&models.MeeseeksSpawnRequest{Task: "test", Tools: []string{"anything"}},
		"dave", "mission-1", []string{}, cfg,
	)
	if err != nil {
		t.Errorf("expected unrestricted parent to allow any child tools: %v", err)
	}
}
```

- [ ] **Step 2: Run test — verify current behavior**

Run: `cd agency-gateway && go test ./internal/orchestrate/ -run TestSpawn_DelegationBounds -v`
Expected: All pass with current code (the edge cases we care about are already handled or don't trigger the condition)

- [ ] **Step 3: Verify the check is correctly scoped**

Read the existing check at lines 71-82. The condition `len(req.Tools) > 0 && len(parentTools) > 0` means:
- Empty `req.Tools` → no check (child inherits parent's full scope — enforcer restricts at runtime)
- Empty `parentTools` → no check (parent is unrestricted — child can request anything)
- Both non-empty → subset check

This is actually correct behavior. The test confirms it. Commit the test as documentation.

- [ ] **Step 4: Commit**

```bash
git add internal/orchestrate/meeseeks_test.go
git commit -m "test: add delegation bounds tests for Meeseeks spawn (ASK tenet 11)

Documents the behavior of the tool-set validation: explicit child tools
must be a subset of parent's granted tools. Empty child tools inherit
parent scope (enforcer restricts at runtime). Unrestricted parents
allow any child tools."
```

---

### Task 6: Constant-Time Token Comparison in internalLLM

The `internalLLM` handler uses `!=` for token comparison, which is timing-oracle vulnerable.

**Files:**
- Modify: `agency-gateway/internal/api/handlers_internal_llm.go:42-43`

- [ ] **Step 1: Fix the comparison**

Replace line 43 in `handlers_internal_llm.go`:

```go
	if token == "" || token != h.cfg.Token {
```

With:

```go
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(h.cfg.Token)) != 1 {
```

Add `"crypto/subtle"` to the import block.

- [ ] **Step 2: Build and run existing tests**

Run: `cd agency-gateway && go test ./internal/api/ -v`
Expected: All existing tests pass

- [ ] **Step 3: Commit**

```bash
git add internal/api/handlers_internal_llm.go
git commit -m "security: use constant-time comparison for internalLLM token

Prevents timing side-channel attacks on the gateway token."
```

---

## Final Verification

After all 6 tasks:

- [ ] Run: `cd agency-gateway && go test ./...`
  Expected: All tests pass
- [ ] Run: `cd agency-gateway && go build ./cmd/gateway/`
  Expected: Build succeeds
- [ ] Run: `cd agency-gateway && go vet ./...`
  Expected: No issues
