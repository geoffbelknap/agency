# Agency Platform

AI agent operating platform. Deploys agents in enforced isolation containers with credential scoping, network mediation, read-only constraints, and continuous audit.

Operated via the `agency` Go binary (CLI + daemon). Gateway REST API at `localhost:8200`.

## ASK Framework Tenets — Do Not Violate

These apply to all work. If a design requires violating any tenet, the design is wrong. See the [ASK Framework](https://github.com/geoffbelknap/ask) for full context.

### Foundation (1–5)
1. **Constraints are external and inviolable.** Enforcement must never run inside the agent boundary. The agent cannot perceive, influence, or circumvent enforcement.
2. **Every action leaves a trace.** Logs are written by mediation, not agents. Agents have no write access to audit logs.
3. **Mediation is complete.** No path from agent to external resource bypasses mediation. New dependencies must go through the egress proxy.
4. **Least privilege.** Capabilities, credentials, mounts, and authority scoped to minimum required. Don't add capabilities to workspace containers or mount unneeded host paths.
5. **No blind trust.** Every trust relationship is documented, visible, and auditable. No implicit trust grants.

### Constraint Lifecycle (6–7)
6. **Constraint changes are atomic and acknowledged.** Agents see old or new constraints, never a mix. Unacknowledged changes are treated as potential compromise.
7. **Constraint history is immutable and complete.** Every constraint state is logged and retrievable for forensics.

### Halt Governance (8–10)
8. **Halts are always auditable and reversible.** Complete audit record for every halt. Halted agent state is preserved.
9. **Halt authority is asymmetric.** Anyone with halt authority can halt; only equal or higher authority can resume. Agents cannot self-resume.
10. **Authority is monitored at the authority level.** Monitoring watches how principals exercise authority, not just what agents do.

### Multi-Agent Bounds (11–12)
11. **Delegation cannot exceed delegator scope.** No coordinator can grant what it doesn't hold.
12. **Synthesis cannot exceed individual authorization.** Combined outputs cannot exceed any individual contributor's authorization.

### Principal Model (13–15)
13. **Principal and agent lifecycles are independent.** Terminating a principal doesn't auto-terminate its agent, and vice versa.
14. **Authority is never orphaned.** When a principal is suspended, authority transfers immediately to a coverage principal.
15. **Trust is earned and monitored continuously.** No self-elevation. Trust reduction can be automatic; elevation always requires human approval.

### Security (16–19, 25)
16. **Quarantine is immediate, silent, and complete.** Process termination, network severance, and filesystem freeze happen simultaneously without agent notification.
17. **Instructions only come from verified principals.** External entities produce data, not instructions (XPIA defense principle).
18. **Unknown entities default to zero trust.** Ambiguous cases resolve to lower trust, not higher.
19. **External agents cannot instruct internal agents.** Authorized external agents are data sources, not commanders.
25. **Identity mutations are auditable and recoverable.** Every write to agent persistent identity is logged. Operator can roll back to any known-good state.

### Coordination (20–22)
20. **Unknown conflicts default to yield and flag.** Agents never force resolution of conflicts with unknown sources.
21. **Human principal termination is always operator-initiated.** No agent or automated process can remove a human principal.
22. **Human principals cannot be quarantined.** Quarantine is agent-specific; humans are flagged for human-to-human resolution.

### Organizational Knowledge (23–24)
23. **Organizational knowledge is durable infrastructure, not agent state.** Knowledge persists independently of any agent's lifecycle.
24. **Knowledge access is bounded by authorization scope.** Graph traversal and retrieval follow the same authorization model as every other action.

## Architecture

```
agency (single Go binary, ~17MB)
  ├── agency setup         → bootstrap ~/.agency/, start daemon, bring up infra
  ├── agency serve         → foreground daemon (REST API)
  └── agency <command>     → auto-starts daemon, REST client to daemon

Consumers (all via REST to localhost:8200):
  CLI, MCP server (native Go, 112 tools), Claude Code plugin, agency-web, third-party tools
```

Gateway REST API: 174 endpoints, 13 groups. Spec: `internal/api/openapi.yaml`.

## Running Tests

```bash
# Go tests — primary test suite
go test ./...

# Go E2E
go build -o agency ./cmd/gateway/ && ./test_e2e.sh

# Python tests (body runtime, container images, coordination)
pytest images/tests/
```

## Key Patterns

- **Go gateway** is the single source of truth. Entry: `cmd/gateway/main.go`. REST API (chi) + CLI (cobra).
- **Models**: Go structs at `internal/models/` (primary). Python Pydantic v2 models at `images/models/` (for container image use).
- **Policy engine**: Hierarchical (platform > org > department > team > agent). Lower levels restrict only. Hard floors are immutable. Go at `internal/policy/` (primary). Python policy engine at `images/tests/support/policy/` (test use only).
- **Seven-phase start**: verify → enforcement → constraints → workspace → identity → body → session. Failure at any phase tears down everything (fail closed).
- **Three-tier halt**: supervised (graceful), immediate (SIGTERM), emergency (SIGKILL + silent).
- **Body runtime**: Only agent runtime. Built-in tools, skills, operator MCP servers, comms, authority, memory tools. No one-shot responses — agents ask clarifying questions, research before answering, and save facts about principals to the knowledge graph. Budget-based cost control replaces turn limits.
- **Container topology**: Per agent: workspace + enforcer. Shared: egress, comms, knowledge, intake, embeddings (Ollama, conditional on KNOWLEDGE_EMBED_PROVIDER=ollama). Hub-and-spoke network: `agency-gateway` (internal bridge) is the hub — gateway proxy, comms, knowledge, intake, web-fetch, and enforcers all attach here. `agency-egress-int` (internal) connects enforcers and eligible services to the egress proxy. `agency-egress-ext` (external) connects egress to the internet. `agency-operator` (external) hosts web UI and relay only. Per-agent `agency-<name>-internal` networks connect workspace↔enforcer. Enforcers must not attach to `agency-operator` or any other external-facing network; host↔enforcer control still happens through loopback-published port `8081`. `CreateMediationNetwork()` and `CreateInternalNetwork()` enforce Internal:true (no external route). Container paths: `/agency/enforcer/`, `/agency/agent/`. No shell sidecar — execution-layer confinement is handled by a custom seccomp profile applied to workspace containers (~100 allowed syscalls).
- **MCP server**: Native Go, built into gateway binary (`agency mcp-server`). 112 tools, stdio, no Python dependency.
- **OpenAPI spec**: Single source of truth at `internal/api/openapi.yaml`. Served from disk at runtime (not embedded) so the spec is always current. Agency-web reads this file from the workspace for API type reference. No copies in `docs/`.
- **Enforcer**: Go HTTP proxy (32MB), credential-free and scope-aware. Routes Anthropic + OpenAI with format translation. Validates tool-level scopes via `CheckScope()` but does not inject credentials — passes `X-Agency-Service` through to egress. No longer reads `.service-keys.env`. Serves agent config files via `/config/{filename}` on port 8081 for API-based config delivery. Mediates all agent-to-infrastructure traffic (ASK Tenet 3): comms via `/mediation/comms` and knowledge via `/mediation/knowledge` on port 8081 (constraint port). Runs XPIA scanning in the LLM proxy path (auto-scans tool-role messages, cross-tool reference detection). Tracks tool definition mutations via ToolTracker. Enforces per-agent rate limits (600 req/min). Handles budget tracking in-process (hard/soft/notify modes). HMAC-signs audit log entries (ENFORCER_AUDIT_HMAC_KEY). Trajectory monitoring: sliding window of tool calls with anomaly detection (repetition, cycles, error cascades, budget velocity). Analysis service eliminated — all former analysis responsibilities now handled inside the enforcer.
- **Egress**: mitmproxy with credential swap. Only component holding real API keys. Resolves credentials from the gateway's encrypted credential store via dedicated Unix socket (`~/.agency/run/gateway-cred.sock`), not from `.service-keys.env` (deleted). `SocketKeyResolver` replaces `FileKeyResolver` as primary. JWT exchange, API key injection, and GitHub App tokens are all driven by `credential-swaps.yaml` which is generated from the credential store.
- **Credential store**: Encrypted credential store at `~/.agency/credentials/store.enc` (AES-256-GCM, key at `.key`). `SecretBackend` interface (4 methods) ready for Vault/AWS/Azure/GCP swap-in. `agency creds set/list/show/delete/rotate/test` CLI, REST endpoints at `/api/v1/creds`, MCP tools. Credential groups for shared protocol config (e.g., `limacharlie` group for JWT exchange params). Scope declarations in presets (`scopes.required/optional`) with enforcer-level validation. Real credentials never enter the enforcer — egress resolves via gateway socket (ASK Tenet 4). `agency creds group create` for shared auth config across per-agent keys.
- **Config delivery**: Hot-reloadable config (PLATFORM.md, mission.yaml, services-manifest.json) served by enforcer at `/config/{filename}` on port 8081. Body runtime fetches via HTTP instead of reading bind-mounted files. Enforcer notifies body via `/hooks/config-change` on SIGHUP. Static config files (AGENTS.md, FRAMEWORK.md, identity.md) remain as bind mounts. Platform config values (LC_ORG_ID, etc.) live in `config.yaml` `config:` section, injected as container env vars.
- **Handler organization**: API handlers organized into 10 domain subpackages under `internal/api/`: `admin`, `agents`, `comms`, `creds`, `events`, `graph`, `hub`, `infra`, `missions`, `platform`. Each has its own `handler` struct with a `deps` field and `RegisterRoutes` function. MCP tool handlers use `mcpDeps` struct in the root `api` package. Shared helpers in `mcp_helpers.go`. Single manifest generator in `manifest.go`.
- **Signals**: Body runtime emits explicit signals (processing, error, task_complete, reflection_cycle, fallback_activated) via comms → gateway WebSocket hub. Enforcer emits trajectory_anomaly signals via gateway signal relay. No polling, no heartbeats. File log (`agent-signals.jsonl`) is the audit source of truth. Budget-based cost control replaces turn limits.
- **Task completion**: Agents call `complete_task(summary=...)` to signal done. The handler sets a flag; the conversation loop checks it after each tool call. Optional reflection loop intercepts completion for self-evaluation before the signal fires.
- **Missions**: First-class managed objects for agent standing instructions. Lifecycle: create → assign → pause/resume → complete. Hot-reload via enforcer SIGHUP. Mission instructions injected into system prompt. System prompt assembly order: identity → mission → mission knowledge → procedural memory → episodic memory → memory → org context → comms → PLATFORM.md → FRAMEWORK.md → AGENTS.md → skills → task completion. `agency mission` commands. Missions support `cost_mode` (frugal/balanced/thorough), `reflection`, `success_criteria`, `fallback`, `procedural_memory`, and `episodic_memory` configuration blocks.
- **Task tiers**: Automatic classification of task complexity (minimal/standard/full) determines which features activate. DM "hi" → minimal (trajectory only, tiny prompt). Mission trigger → standard (+ fallback, memory capture). Complex mission work → full (+ reflection, evaluation, memory injection). Controlled by `cost_mode` shorthand in mission YAML. See `docs/specs/task-tier-cost-model.md`.
- **Reflection loop**: Optional self-evaluation before task completion. When enabled on a mission, the body runtime intercepts `complete_task()`, injects a reflection prompt, and parses a structured JSON verdict (APPROVED/REVISION_NEEDED). Configurable max rounds (default 3). Only activates at `full` task tier. See `docs/specs/reflection-loop.md`.
- **Success criteria evaluation**: Measurable success criteria in mission YAML (`success_criteria.checklist`) with optional platform-side evaluation after `task_complete`. Two modes: `checklist_only` (keyword matching, free) and `llm` (one-shot LLM call via gateway internal LLM endpoint). On-failure actions: `flag` (accept + tag), `retry` (reject + feedback), `block` (reject + notify operator). See `docs/specs/mission-success-criteria.md`.
- **Trajectory monitoring**: Enforcer-side pattern detection for stuck/looping agents. Sliding window of 50 tool calls with detectors: `tool_repetition`, `tool_cycle`, `error_cascade`, `budget_velocity`, `progress_stall`. Anomalies emitted to HMAC-signed audit log + gateway event bus. Always on (free, in-memory). Configurable per-agent via trajectory policy. See `docs/specs/trajectory-monitoring.md`.
- **Fallback policies**: Operator-defined recovery chains in mission YAML. Triggers: `tool_error`, `capability_unavailable`, `budget_warning`, `consecutive_errors`, `timeout`, `no_progress`. Actions: retry → alternative_tool → degrade → simplify → complete_partial → pause_and_assess → escalate. Runtime injects guidance as user-role messages. No LLM cost. See `docs/specs/fallback-policies.md`.
- **Procedural memory**: Post-task capture of task-execution patterns (approach, tools used, outcome, lessons) stored as `procedure` entities in the knowledge graph. Retrieved and injected into system prompt at session start for missions with history. Consolidation after 50+ procedures. See `docs/specs/procedural-memory.md`.
- **Episodic memory**: Per-agent recording of task episodes (what happened, what was notable, entities involved) stored as `episode` entities in the knowledge graph. `recall_episodes` tool for on-demand search. 90-day retention with monthly narrative consolidation. See `docs/specs/episodic-memory.md`.
- **Event framework**: Unified event bus in the gateway. Sources: connector, channel, schedule, webhook, platform. Subscriptions derived from missions + notification config. At-most-once delivery. 1000-event ring buffer.
- **Meeseeks**: Ephemeral single-purpose agents spawned by parent agents via `spawn_meeseeks` tool. Own enforcer, abbreviated startup, USD budget, auto-terminate on completion. "I'm Mr. Meeseeks! Look at me!"
- **Budget model**: USD-denominated cost control at task/daily/monthly granularity. Enforcer tracks per-task via X-Agency-Task-Id header, gateway persists daily/monthly state. Pre-task input validation. Graduated alerting.
- **Build versioning**: Content-aware build IDs (commit+dirty). Stamped on binary, images, containers, audit events. `agency status` shows mismatches. Stale images auto-rebuild. Image rebuild is skipped when buildID matches current commit hash (dirty suffix stripped before comparison).
- **make install / make deploy**: `make install` builds the Go binary, installs it, and auto-restarts the gateway as a daemon. `make deploy` = `make install` + `agency infra up`.
- **Capacity profiling**: `agency setup` profiles host memory/CPU and writes `~/.agency/capacity.yaml`. Limits are enforced: agent start and meeseeks spawn check available slots before proceeding. `GET /api/v1/infra/capacity` returns current capacity state. `agency infra capacity` CLI. `agency admin doctor` includes `host_capacity` and `network_pool` checks.
- **Docker socket audit**: Gateway startup runs `AuditDockerSocket()` — scans all `agency.managed` containers for Docker socket mounts and logs security violations.
- **X-Agency-Caller validation**: Socket routes (used by infra containers) are protected by `CallerValidation` middleware — per-route allowlists restrict which containers can call which endpoints (defense-in-depth, not authentication).
- **Inter-service gateway routing**: Intake and knowledge services route inter-service calls through the gateway client (`gateway_client.py`) instead of calling peer containers directly. This enforces the hub-and-spoke mediation topology (ASK Tenet 3).
- **Shared Python base image**: `images/python-base/Dockerfile` — common dependencies (httpx, aiohttp, pyyaml, pydantic) installed once. Per-service images (`FROM agency-python-base:latest`) add only their extras. See `docs/specs/container-build-standards.md`.
- **Internal event publishing**: `POST /api/v1/events/publish` — infra services (intake, knowledge) publish events to the gateway event bus, replacing direct service-to-service comms calls.
- **Parallel infra ops**: Infra startup, teardown, and agent container stop are parallelized for faster cycle times.
- **MCP proxy retry**: Exponential backoff on MCP proxy connections — survives gateway restarts transparently.
- **Workspace crash watcher**: Background watcher detects workspace container crashes and emits operator alerts.
- **Intake webhook auth**: Enforced via `AGENCY_INTAKE_REQUIRE_AUTH` env var on the intake service. Unauthenticated requests rejected when set.
- **Docker memory limits**: All containers (workspace, enforcer, comms, knowledge, intake, egress) run with explicit memory limits.
- **Capability hot-reload**: `cap enable/disable` regenerates service manifests, writes grants, copies service definitions, and SIGHUPs enforcers — no agent restart needed.
- **Economics observability**: Every LLM call records TTFT (time to first token), TPOT (time per output token), StepIndex, ToolCallValid, and RetryOf in the enforcer audit log. Gateway aggregates into per-workflow rollups: loop cost, steps to resolution, context expansion rate, tool hallucination rate, retry waste. `GET /api/v1/agents/{name}/economics` and `GET /api/v1/agents/economics/summary` expose the data. `agent_signal_workflow_economics` WebSocket signal emitted on task completion.
- **Semantic caching**: Completed task results are cached as `cached_result` nodes in the knowledge graph. Before task execution, the body checks for semantically similar cached results — full hits (similarity >= 0.92) skip the LLM entirely, partial hits (0.80-0.92) inject cached context. Cache entries are XPIA-scanned before use. TTL-based expiration (default 24h). `agency cache clear --agent <name>` flushes cache. Configuration in mission YAML `cache:` block or cost_mode defaults.

- **Principal registry**: UUID-based identity for all entities (agents, operators, teams, roles, channels). Gateway-side SQLite at `~/.agency/registry.db`. `agency registry list/show/update/delete`. Registry snapshot delivered to containers via enforcer config path. All principals get UUIDs at creation time.
- **Permission enforcement**: Two-layer — chi middleware for route-level gating (route→permission map, default-deny for unmatched routes), handler-level for resource-scoped checks (canAccessAgent). Hierarchical ceiling model: parent defines max, children ≤ parent. Wildcards: `knowledge.*`, `*`. Default: operators get `*`, agents get `knowledge.read+write`.
- **Suspension/revocation**: Suspended principals can't authenticate. Authority falls to parent (hierarchy-based coverage). No parent = fail-closed. Revocation halts governed agents. `agency registry update <name> --status suspended`.
- **Edge provenance**: Three tiers (EXTRACTED/INFERRED/AMBIGUOUS) on every knowledge graph edge. Deterministic sources tag EXTRACTED, LLM synthesis tags AMBIGUOUS, curator inference tags INFERRED. `min_provenance` filter on `get_edges()`. GraphRAG re-ranks results by provenance quality.
- **Universal ingestion pipeline**: `POST /api/v1/graph/ingest` accepts any content type. SourceClassifier routes to deterministic extractors (markdown, config, code, HTML, PDF, structured data). MergeBuffer decides if LLM synthesis adds value. `agency graph ingest <file-or-url>` CLI. Watch mode for auto-ingestion directory.
- **Community detection**: Louvain algorithm (NetworkX) in curator cycle (every 6th cycle). Recursive splitting for oversized communities. Cohesion scoring. Community nodes in graph. Agent-facing query patterns: `get_community`, `list_communities`, `get_hubs`.
- **Hub/bridge detection**: Degree centrality for hubs, betweenness centrality for bridges. Filters structural nodes. Agent-facing via `get_hubs` query pattern.
- **Query feedback loop**: `save_insight` body runtime tool. Creates finding nodes with DERIVED_FROM edges (INFERRED provenance). Scope is intersection of source node scopes (ASK Tenet 12).
- **Scope enforcement in traversal**: `get_subgraph()`, `get_neighbors()`, `find_path()` all accept `principal` param. BFS stops at scope boundaries. Edge scope inheritance validated at `add_edge()`.
- **GraphRAG content tagging**: All knowledge graph content injected into system prompts wrapped in `[KNOWLEDGE_GRAPH_CONTEXT]` delimiters with node ID provenance. Enforcer scans these sections for XPIA.
- **Provenance-based quarantine**: `agency admin graph quarantine --agent <name> [--since timestamp]`. Quarantined nodes excluded from all retrieval. Edge exclusion for edges touching quarantined nodes. Release per-node or per-agent.
- **Knowledge classification**: Four tiers (public/internal/restricted/confidential) defined in `~/.agency/knowledge/classification.yaml`. Classification maps to scope via operator-configurable tier→principal mappings. Auto-scope merge at `add_node()` time. `agency graph classification show`.
- **Dynamic routing optimizer**: Background goroutine in gateway. Aggregates LLM call data per (task_type, model). Computes success rate (absence of retry), real USD costs via CalculateCost(). Generates suggestions when cheaper model has ≥90% success, ≥30% savings, ≥20 calls. Operator approves via `agency infra routing approve <id>` → writes `routing.local.yaml`. `agency infra routing suggestions/approve/reject/stats`.

- **Security model doc**: Full threat model, enforcement boundaries, and guarantee inventory at `docs/security-model.md`. Architecture diagram at `docs/architecture-diagram.md`.
- **Hub instance registry**: Hub uses UUID IDs and human names. `agency hub instances` lists all active instances. Components are activated as named instances, not anonymous installs. `agency hub <nameOrID> activate` and `agency hub <nameOrID> config` operate on instances.
- **Hub config schema**: Component configs use `${...}` placeholders for secrets and `config:` declarations for non-secret settings. Credentials are managed via `agency creds set` and stored in the encrypted credential store — secrets never reach containers.
- **Platform awareness**: PLATFORM.md generated at startup from composable building blocks, scaled by agent type (meeseeks→coordinator). Org context queried from knowledge graph at session start. See `docs/specs/agent-platform-awareness.md`.
- **Operator notifications**: Agent signals (error, escalation, self_halt) are promoted to `operator_alert` platform events by the comms bridge. Routed to ntfy/webhook via notification subscriptions. Destinations stored in `~/.agency/notifications.yaml` — managed via REST (`/api/v1/events/notifications`), CLI (`agency notifications list/add/remove/test`), and MCP tools. Headers redacted from GET responses. Hot-reloads event bus subscriptions on add/remove. Fallback: #operator channel in agency-web. All notification delivery goes through the event bus — no direct posts to #operator.
- **Task result delivery**: Agents post results to the originating channel (not #operator). Results >25 lines generate a downloadable markdown artifact with a link. `--report` flag on `agency send` forces artifact generation.
- **Web-fetch service**: Shared infra container (`infra-web-fetch`) for agents to fetch and read web pages. Returns extracted markdown + metadata. Layered security: DNS blocklists (platform hard floor + operator), content-type allowlist, XPIA scanning, per-domain rate limiting. Agents reach it via enforcer mediation (`/mediation/web-fetch`). External requests route through the egress proxy. Audit log at `~/.agency/audit/web-fetch/`. Config at `~/.agency/web-fetch/config.yaml`. Granted as a capability (`agency cap add web-fetch`).

## Docker Management Principles

1. **Never construct HostConfig directly** — use `containers.HostConfigDefaults(role)` and overlay specific fields (Binds, NetworkMode, Tmpfs)
2. **Never call ContainerCreate without the lifecycle guard** — use `containers.CreateAndStart()` which automatically cleans up orphans on start failure
3. **Never create networks without the factory** — use `containers.CreateInternalNetwork()` (always Internal:true) or `containers.CreateEgressNetwork()` (egress proxy only)
4. **Never create a new Docker client** — use the `WithClient` constructor variant and pass the shared client from main.go
5. **All containers MUST have**: log rotation (json-file, 10m, 3 files), PID limits, CPU limits, CAP_DROP ALL, no-new-privileges
6. **Workspace/enforcer restart policy is `on-failure` max 3** — never `unless-stopped` (prevents uncontrolled restarts after gateway death)
7. **Infra containers use `unless-stopped`** — they survive gateway restarts (they're shared services)
8. **Image pruning runs after every resolve** (pull or build) — not just dev mode
9. **Gateway startup reconciles Docker state** — orphan containers/networks are cleaned up automatically
10. **Labels are the source of truth** — all agency containers/networks get `agency.managed=true` for lifecycle management
11. **Never call containers directly from the gateway** — use localhost ports routed through the gateway-proxy (8202→comms, 8204→knowledge, 8205→intake). Container IPs are not routable from the host on macOS Docker Desktop. No fallback paths — one route per service.

## Key Rules

- New features go in the Go gateway. CLI and MCP are REST API consumers.
- Policy hard floors cannot be overridden at any level.
- Two-key exception model: delegation grant from higher level AND exception exercise at agent level.
- `agency admin doctor` must pass before an agent is correctly deployed.
- Token auth required for gateway REST API. Auto-generated on `agency setup`.
- Gateway auto-starts as background daemon. PID at `~/.agency/gateway.pid`.
- No assisted/autonomous mode — agents are always autonomous within their constraints. The `ApprovalGate` was removed.
- No polling patterns — use explicit signals, Docker event streams, or WebSocket push. Heartbeats were removed. Task lifecycle is tracked via explicit signals: `processing`, `task_complete`, `error` — delivered body → comms → gateway WebSocket hub → clients.
- Build with `make all` from the repo root (binary + images). Use `make install` for Go binary only (also auto-restarts gateway daemon). Use `make deploy` for install + infra up. Use `make images` for container images only.
- Never use `docker build` directly — the Makefile passes `--build-arg BUILD_ID` for build versioning; direct builds produce unlabeled images that break staleness detection.
- No `agency connector` commands — connectors are now managed entirely under `agency hub` (instances, activate, config).
- Briefs are removed — deliver tasks via DM channels (`agency send <agent> <message>`) or channel messages routed through the event bus.
- Budget limits replace turn limits — no MAX_TURNS, no auto-continuation. Budget exhaustion is a hard stop. Cost attribution via `X-Agency-Cost-Source` header: `agent_task`, `reflection`, `evaluation`, `memory_capture`, `consolidation`, `context_summary`.
- Per-component image builds: `make body`, `make enforcer`, etc. for surgical rebuilds.
- New API endpoint groups: preset CRUD (`/api/v1/hub/presets/{name}`), agent config (`/api/v1/agents/{name}/config`), agent DM establishment (`POST /api/v1/agents/{name}/dm`), hub instances (`/api/v1/hub/instances`, `/api/v1/hub/{nameOrID}/activate`, `/api/v1/hub/{nameOrID}/config`), usage metrics (`/api/v1/infra/routing/metrics`), signal relay (`/api/v1/agents/{name}/signal` — internal, via comms), notifications CRUD (`/api/v1/events/notifications`, `/api/v1/events/notifications/{name}`, `/api/v1/events/notifications/{name}/test`), agent memory (`/api/v1/agents/{name}/procedures`, `/api/v1/agents/{name}/episodes`, `/api/v1/agents/{name}/trajectory`), mission memory (`/api/v1/missions/{name}/procedures`, `/api/v1/missions/{name}/episodes`, `/api/v1/missions/{name}/evaluations`), credentials (`/api/v1/creds` CRUD + rotate + test + groups, `/api/v1/creds/internal/resolve` for egress socket resolution), registry (`/api/v1/admin/registry` snapshot/resolve/register/update/delete, `/api/v1/admin/registry/{uuid}/effective`), routing optimizer (`/api/v1/infra/routing/suggestions` list/approve/reject, `/api/v1/infra/routing/stats`), graph quarantine (`/api/v1/graph/quarantine` quarantine/release/list), graph classification (`/api/v1/graph/classification`), graph communities (`/api/v1/graph/communities`, `/api/v1/graph/communities/{id}`), graph hubs (`/api/v1/graph/hubs`), graph ingest (`/api/v1/graph/ingest`), graph insight (`/api/v1/graph/insight`), capacity (`/api/v1/infra/capacity`), event publishing (`/api/v1/events/publish` — internal, for infra services).
- `agency creds` is the canonical credential management command. `agency notify` is the notification command. Aliases: `notifications`, `notification`.
- `agency registry` manages the principal registry: `agency registry list/show/update/delete`. `agency registry update <name> --status suspended` suspends a principal.
- `agency infra routing` manages the dynamic routing optimizer: `agency infra routing suggestions/approve/reject/stats`. `agency infra routing approve <id>` writes to `routing.local.yaml`.
- `agency graph ingest <file-or-url>` ingests content into the knowledge graph. `agency graph classification show` displays classification tiers. `agency admin graph quarantine --agent <name> [--since timestamp]` quarantines agent-produced knowledge nodes.
- `agency admin rebuild <agent>` regenerates all derived files (manifest, services.yaml, PLATFORM.md, FRAMEWORK.md, AGENTS.md) in one step.
- Dev workflow: `git pull && make all && agency start <agent>`.
- `agency setup` is the canonical first-run command (replaces `agency init`). `init` is a hidden alias for backwards compatibility. Setup checks Docker, prompts for provider/key, starts daemon, and brings up all infrastructure including agency-web.
- **`agency quickstart`** is the guided first-run wizard. Gets new users from zero to a running agent with a demo task in under 10 minutes. 5 phases: environment check, provider config, infrastructure startup, agent creation, live demo. Each phase auto-skips if already done. `agency setup` is still the idempotent infrastructure command — quickstart is the hand-holding experience.
- **Distribution**: Binary releases via GoReleaser → GitHub Releases + Homebrew tap (`brew install geoffbelknap/tap/agency`). Install script at `https://geoffbelknap.github.io/agency/install.sh`.
- **agency-web is containerized**: Runs as an infra container (`agency-web:latest`) on port 8280, started automatically by `agency setup` / `agency infra up`. Source lives in `web/` (monorepo). `make web` builds the image.
- **Claude Code plugin**: `.claude-plugin/` at repo root with MCP server config and guided skills (`/status`, `/deploy`, `/create-agent`, `/create-mission`). Auto-discovered when working inside the agency repo. Can also be installed standalone via `claude plugin add /path/to/agency`.
- **Dev mode**: `make all` rebuilds the Go binary and all container images from source. Container images are built from `images/` at build time. Images use `CACHE_BUST` build arg to force layer invalidation on every build. Build IDs (`git rev-parse --short HEAD` + `-dirty`) are stamped on the binary and all images. `agency status` shows version mismatches between the binary and running containers. Stale images auto-rebuild on `agency start` when the build ID doesn't match. Old images are pruned after every successful resolve. Per-component rebuilds (`make enforcer`, `make body`, etc.) for surgical iteration without rebuilding everything.
- **Doctor semantics**: `agency admin doctor` should pass for a clean deployment. The Docker hygiene section treats orphan networks based on full network inspect, true dangling images as untagged Agency build leftovers, and `network_pool` as a host Docker-capacity warning. On Docker Desktop, `network_pool` must be fixed in daemon settings rather than by assuming Agency can rewrite the daemon config at runtime.
- Prefer LSP tool for code navigation over grep or manual file reading.
- Hub-managed files (routing.yaml, services, ontology) are overwritten by `agency hub update`. Operator customizations go in routing.local.yaml, new service files, or ontology.d/. Never edit hub-managed files directly. Agentic memory types (procedure, episode) are defined in `~/.agency/knowledge/ontology.d/agentic-memory.yaml`.
- **Hub distribution is OCI-based**: The official hub publishes signed OCI artifacts to `ghcr.io/geoffbelknap/agency-hub`. `agency hub install` pulls from OCI and verifies cosign signatures. Third-party sources can be OCI registries (`agency hub add-source my-corp ghcr.io/my-corp/hub`) or git URLs. Signature verification is mandatory — unsigned artifacts are rejected (ASK tenet 23).
- **Model capabilities**: Every model in routing.yaml declares `capabilities: [tools, vision, streaming]`. The enforcer validates that the target model supports what the request needs. On mismatch, returns HTTP 422. Tier capabilities are the intersection of models in the tier — served to the body as `/config/tiers.json`. `agency hub provider add <name> <url>` discovers models from OpenAI-compatible endpoints.
- **Providers**: Anthropic, OpenAI, Gemini, Ollama, and OpenAI-Compatible are first-class hub providers. Gemini uses Google's OpenAI-compatible endpoint (`generativelanguage.googleapis.com/v1beta/openai`). Anthropic is the only provider requiring format translation (enforcer handles OpenAI→Anthropic and back). All others are OpenAI-format pass-through.
- Never post directly to #operator from the body runtime. Use `_emit_signal()` with severity and message fields — the event bus handles delivery.
- Web-fetch requests always flow through enforcer mediation and the egress proxy. Agents never fetch URLs directly.
- Use `go build ./cmd/gateway/` for targeted gateway builds.
- Hub `Upgrade()` must republish resolved YAML to `~/.agency/connectors/` for active connectors, not just copy to the instance directory. The intake container reads from `~/.agency/connectors/`, not from `hub-registry/`.
- Hub `activate` auto-provisions egress domains from the connector's `requires.egress_domains`. The requirements check does not gate on egress domains (informational only).
- Service definitions may use `${...}` placeholders in `api_base` (e.g., `${JIRA_DOMAIN}`). Validation expands these with `os.Expand` before `url.Parse`.
- Connector graph_ingest templates use Jinja2 `SandboxedEnvironment` which **throws** on missing keys — it does not silently return empty string. Templates must match the actual API response structure exactly. Test with real payloads.
- **Bidirectional gateway proxy** — a socat container (`agency-infra-gateway-proxy`) on the `agency-gateway` and `agency-operator` networks. Bridges traffic in both directions:
  - **Container→gateway**: TCP:8200 → UNIX:`~/.agency/run/gateway.sock`. All containers reach the gateway via `http://gateway:8200` (Docker DNS).
  - **Gateway→comms**: localhost:8202 → proxy:8202 → comms:8080. The host gateway calls `http://localhost:8202`.
  - **Gateway→knowledge**: localhost:8204 → proxy:8204 → knowledge:8080.
  - **Gateway→intake**: localhost:8205 → proxy:8205 → intake:8080.
  No `host.docker.internal` or `ExtraHosts` — works identically on Linux, macOS, and WSL. The operator network connection is required for macOS Docker Desktop to publish port bindings (internal networks don't publish). Credential resolution uses a separate socket (`~/.agency/run/gateway-cred.sock`) mounted only into egress — credentials never traverse a Docker network. See `docs/specs/gateway-socket-proxy.md`.
- Pack schema does not support missions. Missions are created and assigned separately via `agency mission create` / `agency mission assign`. Pack model has `extra="forbid"` — no unknown fields allowed.
- Agent presets for autonomous agents (e.g., alert triage) must explicitly prohibit asking clarifying questions in `hard_limits` and `identity.body`. Default agent behavior is to ask before acting.

## Docs Conventions

- **Design specs** go in `docs/specs/`. One flat directory, no subdirectories.
- **Implementation plans** go in `docs/plans/`.
- **Superpowers plugin override:** Save specs to `docs/specs/` and plans to `docs/plans/` — NOT `docs/superpowers/`.
- Delete plans once their work is fully implemented. Specs are kept as architectural reference.
