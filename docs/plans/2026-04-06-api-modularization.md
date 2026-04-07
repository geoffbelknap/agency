# API Modularization & Startup Health Contract — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the monolithic API layer into 10 compiler-isolated modules, introduce a typed startup health contract, and decouple service communication from the container runtime.

**Architecture:** Per-module `Deps` structs with `RegisterRoutes` functions, a `Startup()` function that hard-fails on core components, and four service interfaces (`CommsClient`, `SignalSender`, `DiagnosticsRuntime`, `InfraRuntime`) that abstract Docker away from modules that don't need container management.

**Tech Stack:** Go 1.26, chi router, table-driven tests with httptest

**Spec:** `docs/specs/2026-04-06-api-modularization-design.md`

---

## Phase 1: Startup Function + CommsClient Extraction

### Task 1: Define the CommsClient interface and extract from docker.Client

**Files:**
- Create: `internal/comms/client.go`
- Create: `internal/comms/client_test.go`

This task creates the `CommsClient` interface and a concrete implementation that wraps the existing `docker.Client.CommsRequest` logic. The interface lives in its own package so both `internal/api/` and `internal/orchestrate/` can import it without circular dependencies.

- [ ] **Step 1: Write the failing test**

```go
// internal/comms/client_test.go
package comms

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClient_CommsRequest_GET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/channels" {
			t.Errorf("expected /channels, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	data, err := c.CommsRequest(context.Background(), "GET", "/channels", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Errorf("unexpected body: %s", string(data))
	}
}

func TestHTTPClient_CommsRequest_POST_WithBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content-type")
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "general" {
			t.Errorf("expected name=general, got %s", body["name"])
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"created":true}`))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	data, err := c.CommsRequest(context.Background(), "POST", "/channels", map[string]string{"name": "general"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != `{"created":true}` {
		t.Errorf("unexpected body: %s", string(data))
	}
}

func TestHTTPClient_CommsRequest_PlatformHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Agency-Platform") != "true" {
			t.Errorf("expected X-Agency-Platform header on grant-access path")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	_, err := c.CommsRequest(context.Background(), "POST", "/channels/general/grant-access", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_CommsRequest_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`not found`))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL)
	_, err := c.CommsRequest(context.Background(), "GET", "/missing", nil)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd agency && go test ./internal/comms/ -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Write the implementation**

```go
// internal/comms/client.go
package comms

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client sends HTTP requests to the comms service.
// This is a service client, not a container interface — comms happens
// to run in a container today but the caller doesn't need to know that.
type Client interface {
	CommsRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error)
}

// HTTPClient implements Client via direct HTTP to a base URL.
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewHTTPClient creates a comms client pointing at the given base URL.
// For the default Agency deployment, baseURL is "http://localhost:8202".
func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *HTTPClient) CommsRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	url := c.baseURL + path

	var req *http.Request
	var err error

	if body != nil && (method == "POST" || method == "PUT" || method == "DELETE") {
		jsonBody, _ := json.Marshal(body)
		req, err = http.NewRequestWithContext(ctx, method, url, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("comms request %s %s: %w", method, path, err)
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, fmt.Errorf("comms request %s %s: %w", method, path, err)
		}
	}

	// Platform-only endpoints need this header.
	if strings.Contains(path, "grant-access") || strings.Contains(path, "archive") {
		req.Header.Set("X-Agency-Platform", "true")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("comms request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("comms request %s %s: read body: %w", method, path, err)
	}
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("comms returned %d: %s", resp.StatusCode, string(out))
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd agency && go test ./internal/comms/ -v`
Expected: PASS — all 4 tests pass

- [ ] **Step 5: Commit**

```bash
cd agency && git add internal/comms/ && git commit -m "feat: extract CommsClient interface and HTTPClient implementation"
```

---

### Task 2: Make docker.Client implement CommsClient

**Files:**
- Modify: `internal/docker/client.go` — add a method or adapter so existing code continues to work during migration

The existing `docker.Client.CommsRequest` method already matches the `comms.Client` interface signature. We need to verify this and add an explicit interface satisfaction check.

- [ ] **Step 1: Verify the existing method signature matches**

Read `internal/docker/client.go` and confirm the `CommsRequest` method on `*Client` has the signature:
```go
func (c *Client) CommsRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error)
```

This already satisfies `comms.Client`. No code change needed for the method itself.

- [ ] **Step 2: Add compile-time interface check**

Add to `internal/docker/client.go` near the top, after the type definition:

```go
// Verify docker.Client satisfies comms.Client at compile time.
var _ comms.Client = (*Client)(nil)
```

Add the import: `"github.com/geoffbelknap/agency/internal/comms"`

- [ ] **Step 3: Run tests to verify nothing breaks**

Run: `cd agency && go test ./internal/docker/ -v`
Expected: PASS — compile-time check confirms interface satisfaction

- [ ] **Step 4: Commit**

```bash
cd agency && git add internal/docker/client.go && git commit -m "feat: docker.Client satisfies comms.Client interface"
```

---

### Task 3: Define SignalSender interface

**Files:**
- Create: `internal/api/interfaces.go`
- Create: `internal/api/interfaces_test.go`

The `SignalSender` interface abstracts container signal delivery (SIGHUP for config reload). `docker.Client.RawClient().ContainerKill()` is the current implementation path.

- [ ] **Step 1: Write the interface and compile-time check**

```go
// internal/api/interfaces.go
package api

import "context"

// SignalSender sends OS signals to named containers.
// Used by modules that need to SIGHUP enforcers for config reload.
type SignalSender interface {
	SignalContainer(ctx context.Context, containerName, signal string) error
}
```

- [ ] **Step 2: Write a test to verify docker.Client can wrap SignalSender**

```go
// internal/api/interfaces_test.go
package api

import (
	"context"
	"testing"
)

// mockSignalSender verifies the interface is implementable.
type mockSignalSender struct {
	called    bool
	lastName  string
	lastSig   string
}

func (m *mockSignalSender) SignalContainer(_ context.Context, name, signal string) error {
	m.called = true
	m.lastName = name
	m.lastSig = signal
	return nil
}

func TestSignalSender_MockImplementation(t *testing.T) {
	var s SignalSender = &mockSignalSender{}
	err := s.SignalContainer(context.Background(), "agent-enforcer", "SIGHUP")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mock := s.(*mockSignalSender)
	if !mock.called || mock.lastName != "agent-enforcer" || mock.lastSig != "SIGHUP" {
		t.Errorf("mock not called correctly: %+v", mock)
	}
}
```

- [ ] **Step 3: Run test to verify it passes**

Run: `cd agency && go test ./internal/api/ -run TestSignalSender -v`
Expected: PASS

- [ ] **Step 4: Add a docker-backed SignalSender adapter**

Add to `internal/api/interfaces.go`:

```go
// DockerSignalSender adapts docker.Client to the SignalSender interface.
type DockerSignalSender struct {
	RawClient interface {
		ContainerKill(ctx context.Context, containerID, signal string) error
	}
}

func (d *DockerSignalSender) SignalContainer(ctx context.Context, containerName, signal string) error {
	return d.RawClient.ContainerKill(ctx, containerName, signal)
}
```

- [ ] **Step 5: Commit**

```bash
cd agency && git add internal/api/interfaces.go internal/api/interfaces_test.go && git commit -m "feat: define SignalSender interface with docker adapter"
```

---

### Task 4: Create Startup function with hard-fail semantics

**Files:**
- Create: `internal/api/startup.go`
- Create: `internal/api/startup_test.go`

Replace `newHandler` with `Startup()` that returns `(*StartupResult, error)`. Core component failures are fatal.

- [ ] **Step 1: Write the failing test**

```go
// internal/api/startup_test.go
package api

import (
	"testing"

	"github.com/geoffbelknap/agency/internal/config"
)

func TestStartup_NilDocker_ReturnsError(t *testing.T) {
	cfg := &config.Config{Home: t.TempDir(), Version: "test"}
	_, err := Startup(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error when docker client is nil")
	}
}

func TestStartup_CoreFailure_ReturnsError(t *testing.T) {
	// With a valid docker client but invalid home dir that will cause
	// orchestration init to fail, Startup should return an error.
	cfg := &config.Config{Home: "/nonexistent/path", Version: "test"}
	_, err := Startup(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error for core component init failure")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd agency && go test ./internal/api/ -run TestStartup -v`
Expected: FAIL — `Startup` not defined

- [ ] **Step 3: Write the Startup function**

```go
// internal/api/startup.go
package api

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/log"

	agencyctx "github.com/geoffbelknap/agency/internal/context"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	"github.com/geoffbelknap/agency/internal/profiles"
)

// StartupResult holds all initialized components. Core fields are guaranteed
// non-nil when Startup returns without error. Optional fields may be nil —
// modules that depend on them should not be registered.
type StartupResult struct {
	// Core — guaranteed non-nil after successful Startup.
	Infra           *orchestrate.Infra
	AgentManager    *orchestrate.AgentManager
	HaltController  *orchestrate.HaltController
	Audit           *logs.Writer
	CtxMgr          *agencyctx.Manager
	MissionManager  *orchestrate.MissionManager
	MeeseeksManager *orchestrate.MeeseeksManager
	Claims          *orchestrate.MissionClaimRegistry
	Knowledge       *knowledge.Proxy
	MCPReg          *MCPToolRegistry

	// Optional — nil means feature disabled, dependent routes not registered.
	CredStore     *credstore.Store
	ProfileStore  *profiles.Store
}

// Startup initializes all gateway components. Core component failures return
// an error — the gateway must not start in a degraded state. Optional component
// failures log warnings and leave fields nil.
func Startup(cfg *config.Config, dc *docker.Client, logger *log.Logger) (*StartupResult, error) {
	if dc == nil {
		return nil, fmt.Errorf("docker client is required")
	}

	infra, err := orchestrate.NewInfra(cfg.Home, cfg.Version, dc, logger, cfg.HMACKey)
	if err != nil {
		return nil, fmt.Errorf("infra init: %w", err)
	}
	infra.SourceDir = cfg.SourceDir
	infra.BuildID = cfg.BuildID
	infra.GatewayAddr = cfg.GatewayAddr
	infra.GatewayToken = cfg.Token
	infra.EgressToken = cfg.EgressToken

	agents, err := orchestrate.NewAgentManager(cfg.Home, dc, logger)
	if err != nil {
		return nil, fmt.Errorf("agent manager init: %w", err)
	}

	halt, err := orchestrate.NewHaltController(cfg.Home, cfg.Version, dc, logger)
	if err != nil {
		return nil, fmt.Errorf("halt controller init: %w", err)
	}

	audit := logs.NewWriter(cfg.Home)
	ctxMgr := agencyctx.NewManager(audit)

	// ASK Tenet 9: unacknowledged constraint changes trigger agent halt.
	ctxMgr.SetHaltFunc(func(agent, changeID, reason string) error {
		return halt.HaltForUnackedConstraint(
			context.Background(), // halt must not be canceled by request lifecycle
			agent, changeID, reason,
		)
	})

	mcpReg := NewMCPToolRegistry()
	registerMCPTools(mcpReg)

	// Optional: credential store.
	var cs *credstore.Store
	storePath := filepath.Join(cfg.Home, "credentials", "store.enc")
	keyPath := filepath.Join(cfg.Home, "credentials", ".key")
	if fb, err := credstore.NewFileBackend(storePath, keyPath); err != nil {
		if logger != nil {
			logger.Warn("credential store init failed", "err", err)
		}
	} else if fb != nil {
		cs = credstore.NewStore(fb, cfg.Home)
	}

	// Optional: profile store.
	ps := profiles.NewStore(filepath.Join(cfg.Home, "profiles"))

	return &StartupResult{
		Infra:           infra,
		AgentManager:    agents,
		HaltController:  halt,
		Audit:           audit,
		CtxMgr:          ctxMgr,
		MissionManager:  orchestrate.NewMissionManager(cfg.Home),
		MeeseeksManager: orchestrate.NewMeeseeksManager(),
		Claims:          orchestrate.NewMissionClaimRegistry(),
		Knowledge:       knowledge.NewProxy(),
		MCPReg:          mcpReg,
		CredStore:       cs,
		ProfileStore:    ps,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd agency && go test ./internal/api/ -run TestStartup -v`
Expected: PASS — nil docker returns error, bad home dir returns error

- [ ] **Step 5: Commit**

```bash
cd agency && git add internal/api/startup.go internal/api/startup_test.go && git commit -m "feat: Startup function with hard-fail on core components"
```

---

### Task 5: Wire Startup into RegisterRoutesWithOptions

**Files:**
- Modify: `internal/api/routes.go` — replace `newHandler` call with `Startup` result
- Modify: `cmd/gateway/main.go` — call `Startup` and handle error before route registration

This is the integration point. The existing `newHandler` is replaced by `Startup`, and `RegisterRoutesWithOptions` takes a `*StartupResult` instead of constructing its own handler.

- [ ] **Step 1: Update RegisterRoutesWithOptions signature**

In `internal/api/routes.go`, change the three registration functions to accept `*StartupResult`:

```go
func RegisterRoutesWithOptions(r chi.Router, cfg *config.Config, dc *docker.Client, logger *log.Logger, startup *StartupResult, opts RouteOptions) {
	h := &handler{
		cfg: cfg, dc: dc, log: logger,
		infra: startup.Infra, agents: startup.AgentManager,
		halt: startup.HaltController, audit: startup.Audit,
		ctxMgr: startup.CtxMgr, mcpReg: startup.MCPReg,
		knowledge: startup.Knowledge, missions: startup.MissionManager,
		meeseeks: startup.MeeseeksManager, claims: startup.Claims,
		credStore: startup.CredStore, profileStore: startup.ProfileStore,
	}
	// ... rest of function unchanged
}
```

Do the same for `RegisterSocketRoutes` and `RegisterCredentialSocketRoutes`.

- [ ] **Step 2: Update cmd/gateway/main.go**

Find where `api.RegisterRoutesWithOptions` is called (around line 887). Add `Startup` call before it:

```go
startup, err := api.Startup(cfg, dc, logger)
if err != nil {
	logger.Fatal("gateway startup failed", "err", err)
}
api.RegisterRoutesWithOptions(r, cfg, dc, logger, startup, routeOpts)
```

- [ ] **Step 3: Delete newHandler function**

Remove the `newHandler` function from `routes.go` (lines 454-501). It's fully replaced by `Startup`.

- [ ] **Step 4: Run full test suite**

Run: `cd agency && go test ./... 2>&1 | tail -20`
Expected: PASS — all tests still pass. The handler struct is unchanged; only its initialization moved.

- [ ] **Step 5: Run the gateway to verify it starts**

Run: `cd agency && go build ./cmd/gateway/ && echo "Build OK"`
Expected: Build succeeds with no errors.

- [ ] **Step 6: Commit**

```bash
cd agency && git add internal/api/routes.go cmd/gateway/main.go && git commit -m "feat: wire Startup into route registration, remove newHandler"
```

---

### Task 6: Hub migration — move from newHandler to Startup

**Files:**
- Modify: `internal/api/startup.go` — add hub migration logic

The hub migration that currently runs inside `newHandler` needs to move to `Startup`.

- [ ] **Step 1: Add hub migration to Startup**

At the end of the `Startup` function, before the return, add:

```go
	// Migrate flat-file hub installations to instance-directory model.
	hubMgr := hub.NewManager(cfg.Home)
	if migrated, err := hubMgr.Registry.MigrateIfNeeded(); err != nil {
		if logger != nil {
			logger.Warn("hub migration failed", "err", err)
		}
	} else if migrated > 0 {
		if logger != nil {
			logger.Info("migrated hub instances from flat files", "count", migrated)
		}
	}
```

Add `"github.com/geoffbelknap/agency/internal/hub"` to imports.

- [ ] **Step 2: Verify build**

Run: `cd agency && go build ./cmd/gateway/ && echo "Build OK"`
Expected: Build succeeds.

- [ ] **Step 3: Commit**

```bash
cd agency && git add internal/api/startup.go && git commit -m "feat: move hub migration from newHandler to Startup"
```

---

## Phase 2: Module Extraction

Each task extracts one module. The pattern is identical: create the package, define `Deps`, move handler methods, update `RegisterRoutesWithOptions` to delegate.

### Task 7: Extract `graph` module

**Files:**
- Create: `internal/api/graph/routes.go`
- Create: `internal/api/graph/handlers.go`
- Create: `internal/api/graph/handlers_review.go`
- Create: `internal/api/graph/handlers_ontology.go`
- Modify: `internal/api/routes.go` — remove graph routes, delegate to module

This is the first extraction. It establishes the pattern all subsequent tasks follow.

- [ ] **Step 1: Create the module with Deps and RegisterRoutes**

```go
// internal/api/graph/routes.go
package graph

import (
	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
)

// Deps declares the minimal dependencies for the graph module.
type Deps struct {
	Knowledge *knowledge.Proxy
	Config    *config.Config
	Logger    *log.Logger
	Audit     *logs.Writer
}

type handler struct {
	deps Deps
}

func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	r.Route("/api/v1/knowledge", func(r chi.Router) {
		r.Post("/query", h.knowledgeQuery)
		r.Get("/who-knows", h.knowledgeWhoKnows)
		r.Get("/stats", h.knowledgeStats)
		r.Get("/export", h.knowledgeExport)
		r.Get("/changes", h.knowledgeChanges)
		r.Get("/context", h.knowledgeContext)
		r.Get("/neighbors", h.knowledgeNeighbors)
		r.Get("/path", h.knowledgePath)
		r.Get("/flags", h.knowledgeFlags)
		r.Post("/restore", h.knowledgeRestore)
		r.Get("/curation-log", h.knowledgeCurationLog)
		// Review
		r.Get("/pending", h.handleKnowledgePending)
		r.Post("/review/{id}", h.handleKnowledgeReview)
		// Ontology
		r.Get("/ontology", h.knowledgeOntology)
		r.Get("/ontology/types", h.knowledgeOntologyTypes)
		r.Get("/ontology/relationships", h.knowledgeOntologyRelationships)
		r.Post("/ontology/validate", h.knowledgeOntologyValidate)
		r.Post("/ontology/migrate", h.knowledgeOntologyMigrate)
	})

	r.Route("/api/v1/ontology", func(r chi.Router) {
		r.Get("/candidates", h.listOntologyCandidates)
		r.Post("/promote", h.promoteOntologyCandidate)
		r.Post("/reject", h.rejectOntologyCandidate)
	})
}
```

- [ ] **Step 2: Move handler methods**

Move the handler methods from `handlers_memory.go` (knowledge-related ones), `handlers_knowledge_review.go`, and `handlers_ontology.go` into the new package. Each method changes its receiver from `(h *handler)` with `h.knowledge` / `h.cfg` / `h.log` / `h.audit` to `(h *handler)` with `h.deps.Knowledge` / `h.deps.Config` / `h.deps.Logger` / `h.deps.Audit`.

Create `internal/api/graph/handlers.go` with the knowledge query/stats/export methods.
Create `internal/api/graph/handlers_review.go` with pending/review methods.
Create `internal/api/graph/handlers_ontology.go` with ontology methods.

- [ ] **Step 3: Update routes.go to delegate**

In `RegisterRoutesWithOptions`, remove the knowledge and ontology route blocks. Add:

```go
graph.RegisterRoutes(r, graph.Deps{
	Knowledge: startup.Knowledge,
	Config:    cfg,
	Logger:    logger,
	Audit:     startup.Audit,
})
```

Add import: `"github.com/geoffbelknap/agency/internal/api/graph"`

- [ ] **Step 4: Run tests**

Run: `cd agency && go test ./internal/api/... -v 2>&1 | tail -20`
Expected: PASS — all existing tests still pass. Graph routes now served by the new module.

- [ ] **Step 5: Verify build**

Run: `cd agency && go build ./cmd/gateway/ && echo "Build OK"`
Expected: Build succeeds.

- [ ] **Step 6: Commit**

```bash
cd agency && git add internal/api/graph/ internal/api/routes.go && git commit -m "refactor: extract graph module from monolithic API handler"
```

---

### Task 8: Extract `creds` module

**Files:**
- Create: `internal/api/creds/routes.go`
- Create: `internal/api/creds/handlers.go`
- Modify: `internal/api/routes.go` — remove credential routes, delegate

Follow the same pattern as Task 7. Deps: `CredStore`, `Audit`, `Config`, `Logger`.

- [ ] **Step 1: Create module with Deps and RegisterRoutes**

Routes to move: `/api/v1/credentials` CRUD, rotate, test, groups, `/api/v1/internal/credentials/resolve`.

- [ ] **Step 2: Move handler methods from handlers_credentials.go**

Change receiver field access: `h.credStore` → `h.deps.CredStore`, etc.

- [ ] **Step 3: Update routes.go to delegate**

Conditionally register only if `startup.CredStore != nil`.

- [ ] **Step 4: Run tests and verify build**

Run: `cd agency && go test ./internal/api/... -v && go build ./cmd/gateway/`

- [ ] **Step 5: Commit**

```bash
cd agency && git add internal/api/creds/ internal/api/routes.go && git commit -m "refactor: extract creds module from monolithic API handler"
```

---

### Task 9: Extract `comms` module

**Files:**
- Create: `internal/api/comms/routes.go`
- Create: `internal/api/comms/handlers.go`
- Modify: `internal/api/routes.go` — remove channel/messaging routes, delegate

Deps: `comms.Client` (the interface from Task 1), `Config`, `Logger`. This is the first module that uses the new `CommsClient` interface instead of `*docker.Client`.

- [ ] **Step 1: Create module**

Routes to move: `/api/v1/channels` CRUD, messages, reactions, search, `/api/v1/unreads`, mark-read. These handlers are currently inline in `routes.go` (not in a separate handler file).

- [ ] **Step 2: Move handler methods from routes.go**

The channel handlers (listChannels, createChannel, readMessages, sendMessage, editMessage, deleteMessage, addReaction, removeReaction, archiveChannel, searchMessages, getUnreads, markRead) are currently defined as methods on `*handler` in `routes.go`. Move them to `internal/api/comms/handlers.go`. Replace `h.dc.CommsRequest(...)` calls with `h.deps.Comms.CommsRequest(...)`.

- [ ] **Step 3: Update routes.go to delegate**

- [ ] **Step 4: Run tests and verify build**

- [ ] **Step 5: Commit**

```bash
cd agency && git add internal/api/comms/ internal/api/routes.go && git commit -m "refactor: extract comms module, first to use CommsClient interface"
```

---

### Task 10: Extract `platform` module

**Files:**
- Create: `internal/api/platform/routes.go`
- Create: `internal/api/platform/handlers.go`
- Modify: `internal/api/routes.go`

Deps: `WSHub`, `Config`, `Logger`. Routes: OpenAPI spec, init, WebSocket, audit summarization.

- [ ] **Step 1-5: Same pattern as Tasks 7-9**

- [ ] **Step 6: Commit**

```bash
cd agency && git add internal/api/platform/ internal/api/routes.go && git commit -m "refactor: extract platform module"
```

---

### Task 11: Extract `events` module

**Files:**
- Create: `internal/api/events/routes.go`
- Create: `internal/api/events/handlers.go`
- Modify: `internal/api/routes.go`

Deps: `EventBus`, `WebhookMgr`, `Scheduler`, `NotifStore`, `Audit`. Conditionally registered when `EventBus != nil`.

- [ ] **Step 1-5: Same pattern**

Routes: events, webhooks, notifications, subscriptions. Move from `handlers_events.go`.

- [ ] **Step 6: Commit**

```bash
cd agency && git add internal/api/events/ internal/api/routes.go && git commit -m "refactor: extract events module (conditional on EventBus)"
```

---

### Task 12: Extract `hub` module

**Files:**
- Create: `internal/api/hub/routes.go`
- Create: `internal/api/hub/handlers.go`
- Modify: `internal/api/routes.go`

Deps: `CredStore`, `Audit`, `Knowledge`, `Config`, `Logger`, `SignalSender`. The hub's single Docker usage (SIGHUP to intake container) goes through `SignalSender`.

- [ ] **Step 1-5: Same pattern**

Move from `handlers_hub.go`, `handlers_connector_setup.go`, `handlers_presets.go`. Replace `h.dc.RawClient().ContainerKill(ctx, intakeName, "SIGHUP")` with `h.deps.Signal.SignalContainer(ctx, intakeName, "SIGHUP")`.

- [ ] **Step 6: Commit**

```bash
cd agency && git add internal/api/hub/ internal/api/routes.go && git commit -m "refactor: extract hub module, uses SignalSender instead of docker.Client"
```

---

### Task 13: Extract `infra` module

**Files:**
- Create: `internal/api/infra/routes.go`
- Create: `internal/api/infra/handlers.go`
- Modify: `internal/api/routes.go`

Deps: `Infra`, `DockerStatus`, `Config`, `Logger`, plus `*docker.Client` for `InfraStatus` (this module legitimately needs the container runtime).

- [ ] **Step 1-5: Same pattern**

Move from `handlers_infra.go`, `handlers_internal_llm.go`, `handlers_routing.go`.

- [ ] **Step 6: Commit**

```bash
cd agency && git add internal/api/infra/ internal/api/routes.go && git commit -m "refactor: extract infra module"
```

---

### Task 14: Extract `admin` module

**Files:**
- Create: `internal/api/admin/routes.go`
- Create: `internal/api/admin/handlers.go`
- Create: `internal/api/admin/handlers_docker.go`
- Modify: `internal/api/routes.go`

Deps: `AgentManager`, `Infra`, `Knowledge`, `Audit`, `ProfileStore`, `Config`, `Logger`, `DiagnosticsRuntime` (interface for container inspection). Includes teams, profiles, capabilities, policy, egress.

- [ ] **Step 1: Define DiagnosticsRuntime interface**

Add to `internal/api/interfaces.go`:

```go
// DiagnosticsRuntime provides read-only container inspection for admin/audit.
type DiagnosticsRuntime interface {
	ListAgentWorkspaces(ctx context.Context) ([]docker.AgentWorkspace, error)
	InspectContainer(ctx context.Context, name string) (*docker.ContainerInfo, error)
	ListAgencyContainers(ctx context.Context, all bool) ([]docker.ContainerSummary, error)
	ListNetworksByLabel(ctx context.Context, label string) ([]docker.NetworkSummary, error)
	ListAgencyImages(ctx context.Context) ([]docker.ImageSummary, error)
	LogFileSize(ctx context.Context, name string) (int64, error)
	ContainerInspectRaw(ctx context.Context, name string) (map[string]interface{}, error)
}
```

- [ ] **Step 2-5: Same pattern as other extractions**

Move from `handlers_admin.go`, `handlers_admin_docker.go`, `handlers_capabilities.go`, `handlers_profiles.go`.

- [ ] **Step 6: Commit**

```bash
cd agency && git add internal/api/admin/ internal/api/interfaces.go internal/api/routes.go && git commit -m "refactor: extract admin module with DiagnosticsRuntime interface"
```

---

### Task 15: Extract `missions` module

**Files:**
- Create: `internal/api/missions/routes.go`
- Create: `internal/api/missions/handlers.go`
- Modify: `internal/api/routes.go`

Deps: `MissionManager`, `Claims`, `HealthMonitor`, `Scheduler`, `EventBus`, `Knowledge`, `CredStore`, `Audit`, `Config`, `Logger`, `comms.Client`, `SignalSender`.

- [ ] **Step 1-5: Same pattern**

Move from `handlers_missions.go`, `handlers_canvas.go`. Replace `h.dc.CommsRequest(...)` with `h.deps.Comms.CommsRequest(...)` and `h.dc.RawClient().ContainerKill(...)` with `h.deps.Signal.SignalContainer(...)`.

- [ ] **Step 6: Commit**

```bash
cd agency && git add internal/api/missions/ internal/api/routes.go && git commit -m "refactor: extract missions module"
```

---

### Task 16: Extract `agents` module

**Files:**
- Create: `internal/api/agents/routes.go`
- Create: `internal/api/agents/handlers.go`
- Create: `internal/api/agents/handlers_config.go`
- Create: `internal/api/agents/handlers_context.go`
- Create: `internal/api/agents/handlers_meeseeks.go`
- Modify: `internal/api/routes.go`

Deps: `AgentManager`, `HaltController`, `CtxMgr`, `Audit`, `EventBus`, `Config`, `Logger`, `comms.Client`, `SignalSender`. This is the largest module and the last extraction.

- [ ] **Step 1-5: Same pattern**

Move from `handlers_agent.go`, `handlers_agent_config.go`, `handlers_grants.go`, `handlers_budget.go`, `handlers_cache.go`, `handlers_economics.go`, `handlers_trajectory.go`, `handlers_meeseeks.go`, `handlers_context.go`.

The `contextHandler` struct already exists as a separate type — move it into `handlers_context.go` in the agents module.

- [ ] **Step 6: Commit**

```bash
cd agency && git add internal/api/agents/ internal/api/routes.go && git commit -m "refactor: extract agents module (final extraction)"
```

---

## Phase 3: Cleanup

### Task 17: Create RegisterAll and remove monolithic handler struct

**Files:**
- Modify: `internal/api/routes.go` — replace `RegisterRoutesWithOptions` with `RegisterAll`

After all modules are extracted, `routes.go` should contain only:
- `StartupResult` type (or import from `startup.go`)
- `RouteOptions` struct
- `RegisterAll()` function (~80 lines of module wiring)
- `RegisterSocketRoutes()` and `RegisterCredentialSocketRoutes()` (unchanged)

- [ ] **Step 1: Rename and simplify**

Replace `RegisterRoutesWithOptions` with `RegisterAll` that takes `*StartupResult` and wires each module:

```go
func RegisterAll(r chi.Router, cfg *config.Config, dc *docker.Client, logger *log.Logger, startup *StartupResult, opts RouteOptions) {
	graph.RegisterRoutes(r, graph.Deps{...})
	creds.RegisterRoutes(r, creds.Deps{...})
	comms.RegisterRoutes(r, comms.Deps{...})
	platform.RegisterRoutes(r, platform.Deps{...})
	if opts.EventBus != nil {
		events.RegisterRoutes(r, events.Deps{...})
	}
	hub.RegisterRoutes(r, hub.Deps{...})
	infra.RegisterRoutes(r, infra.Deps{...})
	admin.RegisterRoutes(r, admin.Deps{...})
	missions.RegisterRoutes(r, missions.Deps{...})
	agents.RegisterRoutes(r, agents.Deps{...})
}
```

- [ ] **Step 2: Delete the monolithic handler struct**

Remove the `type handler struct` definition from `routes.go`. Each module now has its own handler struct.

- [ ] **Step 3: Clean up unused imports**

Remove imports that were only needed by the old handler struct.

- [ ] **Step 4: Update cmd/gateway/main.go**

Change `api.RegisterRoutesWithOptions(...)` → `api.RegisterAll(...)`.

- [ ] **Step 5: Run full test suite**

Run: `cd agency && go test ./... 2>&1 | tail -30`
Expected: PASS

- [ ] **Step 6: Verify build and gateway startup**

Run: `cd agency && go build ./cmd/gateway/ && echo "Build OK"`

- [ ] **Step 7: Commit**

```bash
cd agency && git add internal/api/routes.go cmd/gateway/main.go && git commit -m "refactor: RegisterAll replaces monolithic handler — API modularization complete"
```

---

### Task 18: Split MCP tool registrations by module

**Files:**
- Create MCP registration functions in each module that owns tools
- Modify: `internal/api/agents/mcp.go`, `internal/api/graph/mcp.go`, etc.
- Modify: `internal/api/mcp_register.go` — remove tool registrations that moved to modules

Currently `mcp_register.go` (38KB) and `mcp_register_ext.go` (29KB) register all MCP tools in one place. Each module should own its MCP tool registrations.

- [ ] **Step 1: Add MCPToolRegistry to Deps of modules that register tools**

Each module's `Deps` gets an optional `MCPReg *MCPToolRegistry` field. Modules register their tools in `RegisterRoutes` if `MCPReg` is non-nil.

- [ ] **Step 2: Move tool registration functions to their modules**

Agent tools → `agents/mcp.go`, knowledge tools → `graph/mcp.go`, etc.

- [ ] **Step 3: Slim down mcp_register.go**

Only keep tools that are truly cross-cutting (e.g., infra status). Everything domain-specific moves to its module.

- [ ] **Step 4: Run tests**

Run: `cd agency && go test ./internal/api/... -v`

- [ ] **Step 5: Commit**

```bash
cd agency && git add internal/api/ && git commit -m "refactor: split MCP tool registrations into owning modules"
```

---

### Task 19: Update orchestrate callers to use CommsClient

**Files:**
- Modify: `internal/orchestrate/agent.go`
- Modify: `internal/orchestrate/start.go`
- Modify: `internal/orchestrate/deploy.go`
- Modify: `internal/orchestrate/infra.go`

Replace `Docker.CommsRequest(...)` calls with a `comms.Client` field.

- [ ] **Step 1: Add comms.Client to AgentManager struct**

In `internal/orchestrate/agent.go`, add a `Comms comms.Client` field to `AgentManager`. Update `NewAgentManager` to accept it. Update `cmd/gateway/main.go` to pass it.

- [ ] **Step 2: Replace Docker.CommsRequest calls in agent.go**

Change `am.Docker.CommsRequest(...)` → `am.Comms.CommsRequest(...)`.

- [ ] **Step 3: Repeat for start.go, deploy.go, infra.go**

Each struct that uses `Docker.CommsRequest` gets a `Comms comms.Client` field. The Docker field remains for legitimate container operations.

- [ ] **Step 4: Run tests**

Run: `cd agency && go test ./internal/orchestrate/... -v`

- [ ] **Step 5: Commit**

```bash
cd agency && git add internal/orchestrate/ cmd/gateway/main.go && git commit -m "refactor: orchestrate callers use CommsClient instead of docker.Client for comms"
```

---

### Task 20: Final verification and cleanup

**Files:**
- Modify: `internal/api/routes.go` — remove any dead code
- Delete: old handler files that were fully moved (verify no remaining references)

- [ ] **Step 1: Verify routes.go is minimal**

`routes.go` should contain only: `RegisterAll`, `RegisterSocketRoutes`, `RegisterCredentialSocketRoutes`, `RouteOptions`, imports. No handler methods, no handler struct.

- [ ] **Step 2: Delete old handler files**

After confirming all methods moved, delete the empty originals:
`handlers_memory.go`, `handlers_knowledge_review.go`, `handlers_ontology.go`, `handlers_credentials.go`, `handlers_hub.go`, `handlers_connector_setup.go`, `handlers_presets.go`, `handlers_infra.go`, `handlers_internal_llm.go`, `handlers_routing.go`, `handlers_admin.go`, `handlers_admin_docker.go`, `handlers_capabilities.go`, `handlers_profiles.go`, `handlers_missions.go`, `handlers_canvas.go`, `handlers_agent.go`, `handlers_agent_config.go`, `handlers_grants.go`, `handlers_budget.go`, `handlers_cache.go`, `handlers_economics.go`, `handlers_trajectory.go`, `handlers_meeseeks.go`, `handlers_context.go`, `handlers_events.go`.

- [ ] **Step 3: Run full test suite**

Run: `cd agency && go test ./... 2>&1 | tail -30`
Expected: ALL PASS

- [ ] **Step 4: Run the gateway end-to-end**

Run: `cd agency && go build -o agency ./cmd/gateway/ && ./test_e2e.sh`

- [ ] **Step 5: Commit**

```bash
cd agency && git add -A && git commit -m "chore: remove old handler files, API modularization complete"
```
