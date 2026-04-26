# API Modularization & Startup Health Contract

**Date:** 2026-04-06
**Status:** Draft
**Scope:** Spec 1 of 4 from the architecture review response. Covers API layer modularization, startup health contract, and service interface decoupling. Does not cover auth hardening, schema registry evolution, or degradation policy documentation.

## Problem

The API layer has accumulated 195 endpoints wired through a single 337-line `RegisterRoutesWithOptions` function and a 24-field monolithic `handler` struct. The constructor (`newHandler`) silently drops errors from three core orchestration components, allowing the gateway to start in a broken state and serve 500s. Service communication (comms) is routed through `docker.Client`, coupling unrelated modules to the container runtime.

These problems compound: an agent working on knowledge graph routes must navigate the full handler struct, risks breaking unrelated domains, and can't tell from the type system what a module actually depends on.

## Goals

1. Split the monolithic API layer into compiler-isolated domain packages with explicit, minimal dependency interfaces.
2. Make startup failures loud and immediate — core component failure prevents the gateway from starting.
3. Decouple service communication from the container runtime so modules talk to services, not containers.

## Non-Goals

- Auth middleware hardening (Spec 2).
- Schema registry evolution (Spec 3).
- Degradation policy documentation (Spec 4).
- Changing the container runtime (this spec enables future swaps but doesn't perform one).
- Refactoring `internal/orchestrate/` container management (legitimate Docker usage stays as-is).

## Design

### 1. Startup Health Contract

Replace `newHandler` with a `Startup` function that returns a typed result or an error.

```go
// internal/api/startup.go

type StartupResult struct {
    // Core — failure here is fatal, gateway refuses to start.
    Infra          *orchestrate.Infra
    AgentManager   *orchestrate.AgentManager
    HaltController *orchestrate.HaltController
    Audit          *logs.Writer
    CtxMgr         *agencyctx.Manager
    MissionManager *orchestrate.MissionManager
    MeeseeksManager *orchestrate.MeeseeksManager
    Claims         *orchestrate.MissionClaimRegistry
    Knowledge      *knowledge.Proxy
    MCPReg         *MCPToolRegistry

    // Optional — nil means feature disabled, routes not registered.
    EventBus      *events.Bus
    WebhookMgr    *events.WebhookManager
    Scheduler     *events.Scheduler
    NotifStore    *events.NotificationStore
    HealthMonitor *orchestrate.MissionHealthMonitor
    CredStore     *credstore.Store
    ProfileStore  *profiles.Store
    DockerStatus  *docker.Status
}

func Startup(cfg *config.Config, dc *docker.Client, logger *log.Logger) (*StartupResult, error) {
    infra, err := orchestrate.NewInfra(cfg.Home, cfg.Version, dc, logger, cfg.HMACKey)
    if err != nil {
        return nil, fmt.Errorf("infra init: %w", err)
    }

    agents, err := orchestrate.NewAgentManager(cfg.Home, dc, logger)
    if err != nil {
        return nil, fmt.Errorf("agent manager init: %w", err)
    }

    halt, err := orchestrate.NewHaltController(cfg.Home, cfg.Version, dc, logger)
    if err != nil {
        return nil, fmt.Errorf("halt controller init: %w", err)
    }

    // Optional components: log warnings, leave nil.
    var cs *credstore.Store
    // ... (credential store init with warning on failure, same pattern as today)

    return &StartupResult{
        Infra: infra, AgentManager: agents, HaltController: halt,
        // ... remaining fields
    }, nil
}
```

Rules:
- Core component failure returns an error. The gateway logs the error and exits.
- Optional component failure logs a warning and leaves the field nil.
- `/infra/status` reports which optional components are active vs disabled.
- The `StartupResult` is the single source of truth for what's available at runtime.

### 2. Service Interface Extraction

Four interfaces decouple modules from the container runtime:

```go
// internal/api/interfaces.go (or per-module, co-located with Deps)

// CommsClient is an HTTP client for the comms service.
// Not a container interface — comms just happens to run in a container today.
type CommsClient interface {
    CommsRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error)
}

// SignalSender sends OS signals to named containers (e.g., SIGHUP for config reload).
type SignalSender interface {
    ContainerKill(ctx context.Context, containerName, signal string) error
}

// DiagnosticsRuntime provides read-only container inspection for admin/audit.
type DiagnosticsRuntime interface {
    ListAgentWorkspaces(ctx context.Context) ([]AgentWorkspace, error)
    InspectContainer(ctx context.Context, name string) (*ContainerInfo, error)
    ListAgencyContainers(ctx context.Context, all bool) ([]ContainerSummary, error)
    ListNetworksByLabel(ctx context.Context, label string) ([]NetworkSummary, error)
    ListAgencyImages(ctx context.Context) ([]ImageSummary, error)
    LogFileSize(ctx context.Context, name string) (int64, error)
    ContainerInspectRaw(ctx context.Context, name string) (map[string]interface{}, error)
}

// InfraRuntime provides infrastructure lifecycle operations.
// This is the interface most tightly coupled to the container runtime.
type InfraRuntime interface {
    InfraStatus(ctx context.Context) (*InfraStatusResult, error)
    // Additional methods as needed for infra module.
}
```

`docker.Client` implements all four interfaces. Modules depend on the interface they need, not the concrete type.

This extraction also applies to `internal/orchestrate/`:
- `agent.go`, `start.go`, `deploy.go`, `infra.go` take `CommsClient` instead of `*docker.Client` for comms operations.
- Container lifecycle operations (create, stop, remove, pause, unpause) remain on the concrete Docker client — these are legitimate container management.

### 3. API Route Modules

Ten packages under `internal/api/`, each exporting a `Deps` struct and `RegisterRoutes` function:

#### Module Map

| Module | Package | Endpoints | Domains |
|--------|---------|-----------|---------|
| agents | `internal/api/agents` | ~35 | Agent lifecycle, config, economics, context, signals, trajectory, meeseeks |
| missions | `internal/api/missions` | ~21 | Mission CRUD, procedures, episodes, evaluations, canvas, claims |
| graph | `internal/api/graph` | ~21 | Knowledge graph, ontology, curation, review, candidate promotion |
| hub | `internal/api/hub` | ~22 | Hub management, search, install, instances, packs, presets |
| comms | `internal/api/comms` | ~12 | Messaging, reactions, search, unreads |
| creds | `internal/api/creds` | ~8 | Credential CRUD, rotation, testing, groups, internal resolve |
| events | `internal/api/events` | ~14 | Events, webhooks, notifications, subscriptions |
| admin | `internal/api/admin` | ~25 | Admin ops, teams, profiles, capabilities, policy, egress |
| infra | `internal/api/infra` | ~13 | Infrastructure control, health, routing, providers, setup, MCP, internal LLM |
| platform | `internal/api/platform` | ~5 | OpenAPI spec, init, WebSocket, audit |

#### Per-Module Dependency Example

```go
// internal/api/agents/routes.go
package agents

type Deps struct {
    AgentManager   *orchestrate.AgentManager
    HaltController *orchestrate.HaltController
    CtxMgr         *agencyctx.Manager
    Audit          *logs.Writer
    EventBus       *events.Bus       // may be nil
    Config         *config.Config
    Logger         *log.Logger
    Comms          CommsClient        // interface, not docker.Client
    Runtime        AgentRuntime       // interface for StopContainers, ContainerKill
}

func RegisterRoutes(r chi.Router, d Deps) {
    h := &handler{deps: d}
    r.Route("/api/v1/agents", func(r chi.Router) {
        r.Get("/", h.list)
        r.Post("/", h.create)
        // ...
    })
}
```

Each module's `Deps` lists only what it uses. The compiler enforces this — a module can't access dependencies it didn't declare.

#### Per-Module Dependency Map

| Module | Dependencies |
|--------|-------------|
| agents | AgentManager, HaltController, CtxMgr, Audit, EventBus, Config, Logger, CommsClient, SignalSender |
| missions | MissionManager, Claims, HealthMonitor, Scheduler, EventBus, Knowledge, CredStore, Audit, Config, Logger, CommsClient, SignalSender |
| graph | Knowledge, Config, Logger, Audit |
| hub | CredStore, Audit, Knowledge, Config, Logger, SignalSender |
| comms | CommsClient, Config, Logger |
| creds | CredStore, Audit, Config, Logger |
| events | EventBus, WebhookMgr, Scheduler, NotifStore, Audit |
| admin | AgentManager, Infra, Knowledge, Audit, ProfileStore, Config, Logger, DiagnosticsRuntime |
| infra | Infra, InfraRuntime, DockerStatus, Config, Logger |
| platform | WSHub, Config, Logger |

#### Top-Level Wiring

```go
// internal/api/routes.go (after refactor: ~80 lines)

func RegisterAll(r chi.Router, s *StartupResult, opts Options) {
    agents.RegisterRoutes(r, agents.Deps{
        AgentManager:   s.AgentManager,
        HaltController: s.HaltController,
        CtxMgr:         s.CtxMgr,
        Audit:          s.Audit,
        EventBus:       s.EventBus,
        Config:         opts.Config,
        Logger:         opts.Logger,
        Comms:          opts.CommsClient,
        Runtime:        opts.Docker,
    })

    // Optional module: only register if EventBus is available.
    if s.EventBus != nil {
        events.RegisterRoutes(r, events.Deps{
            EventBus:   s.EventBus,
            WebhookMgr: s.WebhookMgr,
            Scheduler:  s.Scheduler,
            NotifStore: s.NotifStore,
            Audit:      s.Audit,
        })
    }

    // ... remaining modules
}
```

Optional modules are conditionally registered based on which optional components initialized successfully. If `EventBus` is nil, event/webhook/notification routes don't exist — no 500s, no nil pointer panics.

### 4. Migration Strategy

Three phases, each independently shippable and testable.

#### Phase 1: Startup function + CommsClient extraction

- Replace `newHandler` with `Startup()` returning `(*StartupResult, error)`.
- Core component failures become hard errors — gateway refuses to start.
- Extract `CommsClient` interface from `docker.Client`.
- Update `internal/orchestrate/` callers (agent.go, start.go, deploy.go, infra.go) to accept `CommsClient`.
- `RegisterRoutesWithOptions` still exists but takes `*StartupResult`. Monolithic handler remains temporarily.
- This is the smallest diff with the biggest safety improvement.

#### Phase 2: Extract modules one at a time

Each extraction is a single PR: create the package, move handler methods and route registration, update `RegisterAll`.

Extraction order (least coupled first):
1. `graph` — only needs Knowledge proxy
2. `creds` — only needs CredStore
3. `comms` — only needs CommsClient
4. `platform` — only needs Config, WSHub
5. `events` — needs EventBus, WebhookMgr, Scheduler, NotifStore
6. `hub` — needs Config, CredStore, SignalSender
7. `infra` — needs Infra, InfraRuntime, DockerStatus
8. `admin` — needs multiple deps including DiagnosticsRuntime
9. `missions` — needs MissionManager, Claims, HealthMonitor, EventBus
10. `agents` — most dependencies, extracted last

The old `routes.go` shrinks with each PR. Handler files move to their module package.

#### Phase 3: Remove monolithic handler struct

Once all modules are extracted:
- Delete the 24-field `handler` struct.
- `routes.go` contains only `RegisterAll()`, `StartupResult`, and shared interface definitions.
- Each module has its own small handler struct with only its declared dependencies.

### 5. File Movement Map

Existing handler files map to modules:

| Current File | Target Module |
|-------------|---------------|
| handlers_agent.go, handlers_agent_config.go, handlers_grants.go, handlers_budget.go, handlers_cache.go, handlers_economics.go, handlers_trajectory.go, handlers_meeseeks.go | agents |
| handlers_missions.go, handlers_canvas.go | missions |
| handlers_memory.go, handlers_knowledge_review.go, handlers_ontology.go | graph |
| handlers_hub.go, handlers_connector_setup.go, handlers_presets.go | hub |
| (channel handlers currently in routes.go) | comms |
| handlers_credentials.go | creds |
| handlers_events.go | events |
| handlers_admin.go, handlers_admin_docker.go, handlers_capabilities.go, handlers_profiles.go | admin |
| handlers_infra.go, handlers_internal_llm.go, handlers_routing.go | infra |
| (WebSocket, OpenAPI, init handlers in routes.go) | platform |

MCP handler files (`mcp_register.go`, `mcp_admin.go`, `mcp_register_ext.go`, etc.) need to be split by domain during extraction — they currently register tools spanning multiple modules in single files. Each module owns its MCP tool registrations: agent tools go to `agents`, knowledge tools go to `graph`, etc. The `MCPToolRegistry` stays shared (in `internal/api/` or its own small package) and each module registers its tools into it via a callback or registration function passed through `Deps`.

### 6. Testing Strategy

- Each module gets its own `_test.go` files with mock implementations of its `Deps` interfaces.
- Integration tests in `internal/api/` test `RegisterAll` wiring with real dependencies.
- Startup tests verify that core component failures prevent startup and optional failures degrade gracefully.
- Migration: existing tests continue to pass at every phase boundary. No test goes red between PRs.

### 7. Risks and Mitigations

| Risk | Mitigation |
|------|-----------|
| Handler methods reference fields that cross module boundaries | Audit each method's field access before moving. If a method touches multiple domains, it needs refactoring or lives in the module that owns its primary concern. |
| Context handler (`contextHandler`) is a separate struct | It already has its own struct — move it to the `agents` module as-is. |
| MCP tool registration touches many domains | MCP registration functions move to their owning module. The MCPToolRegistry stays shared (in `internal/api/` or its own package). |
| Large diff risk during Phase 2 | Each module extraction is one PR. Reviewer only needs to verify: routes moved correctly, deps are minimal, tests pass. |
| `CommsClient` extraction breaks orchestration tests | Orchestration tests that mock `docker.Client` now mock `CommsClient` separately. This is a test improvement — tests become more focused. |
