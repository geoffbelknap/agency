---
description: "---"
status: "Approved"
---

# Platform Features 1-6: Connectors, Graph Ingest, Query, Audit Threading, Metrics

**Date:** 2026-03-27
**Status:** Approved

---

## Feature 1: Poll Source Cron + Transform + Auth

### Summary

Extend the existing `poll` source type with three new optional fields: `cron` (alternative trigger to `interval`), `transform` (dot-path field extraction), and `auth` (named service grant for authenticated endpoints).

### Model Changes (agency_core/models/connector.py)

Add to `ConnectorSource`:

- `cron: Optional[str]` — Cron expression (e.g., `"*/5 * * * *"`). When present, replaces `interval` as the trigger mechanism. **Mutually exclusive with `interval`** — model validation enforces that exactly one of `cron` or `interval` is set when `type == "poll"`.
- `transform: Optional[str]` — Dot-path extraction applied to the response body before routing. Uses the same semantics as the existing `response_key` field: `$` = root is list, `$.data.results` = nested path. Applied **after** `response_key`, so they compose: `response_key` picks the list from the response, `transform` reshapes each item.
- `auth: Optional[str]` — References a named service grant. The intake poller resolves this to credentials via the egress credential swap. Injected as `Authorization: Bearer {token}` header on the poll HTTP request.

### Intake Changes

**scheduler.py:**
- When a poll-type connector has `cron` instead of `interval`, register it in the schedule evaluation loop (60-second tick) instead of the poll loop (10-second tick).
- On cron match, call `_poll_once()` for that connector — reuses existing fetch + hash dedup + route logic.
- Dedup via `ScheduleStateStore.set_last_fired()` prevents double-fire within the same minute.

**poller.py:**
- New function `apply_transform(items, transform_path) -> list` — applies dot-path extraction to each item in the list. Reuses the same path parsing logic as `extract_items()`.
- Auth credential injection: when `source.auth` is set, poll HTTP requests route through the egress proxy which performs credential swap — the intake service never holds real API keys. The `auth` field maps to a named service grant; the egress proxy resolves the scoped token to the real credential at the proxy boundary (ASK Tenet 4).

**server.py SIGHUP handler:**
- Already reloads connectors from disk. Cron-scheduled polls picked up on next schedule loop tick. No additional changes needed.

### Goroutine / Thread Safety

- No new threads. Cron-triggered polls execute within the existing schedule loop.
- Poll state (hashes, failure counts) stored in SQLite with per-connector isolation.
- Deactivating a connector clears its poll state and schedule state.

### Tests

- Model validation: `cron` + `interval` mutual exclusion, valid cron expressions
- `apply_transform()`: nested paths, missing keys, empty results
- Auth header injection in poll requests
- Cron-triggered poll execution (mock HTTP + verify routing)
- SIGHUP picks up new cron-poll connectors

---

## Feature 2: Connector graph_ingest Block

### Summary

Add an optional `graph_ingest` block to connector YAML. Maps incoming event payloads to knowledge graph node/edge upserts without routing to an agent.

### Model Changes (agency_core/models/connector.py)

New models:

```python
class GraphIngestNode(BaseModel):
    kind: str                          # Entity type (e.g., "Alert", "Device")
    label: str                         # Jinja2 template: "{{payload.id}}"
    properties: dict[str, str] = {}    # Jinja2 templates for values

class GraphIngestEdge(BaseModel):
    relation: str                      # e.g., "REFERENCES"
    from_label: str                    # Jinja2 template
    to_kind: str
    to_label: str                      # Jinja2 template

class GraphIngestRule(BaseModel):
    match: Optional[dict] = None       # Same semantics as route match; None = all events
    nodes: list[GraphIngestNode] = []
    edges: list[GraphIngestEdge] = []

# Add to ConnectorConfig:
class ConnectorConfig(BaseModel):
    ...
    graph_ingest: list[GraphIngestRule] = []
```

A connector can have both `routes` and `graph_ingest` — they are independent. Routes deliver events to agents; graph_ingest writes structured data to the knowledge graph.

### Intake Changes

**New module: `graph_ingest.py`**

`evaluate_graph_ingest(rules, payload, knowledge_url) -> int`:
1. For each rule, check `match` against payload using existing `match_route()` from `router.py`.
2. Render node/edge templates with **sandboxed Jinja2**: `jinja2.sandbox.SandboxedEnvironment` with `undefined=Undefined` (missing fields become empty string). No custom filters, no attribute access beyond dict keys.
3. POST each node to `{knowledge_url}/ingest/nodes` with body:
   ```json
   {"label": "...", "kind": "...", "summary": "", "source_type": "rule", "properties": {"_provenance_connector": "...", "_provenance_work_item": "...", ...}}
   ```
   Every graph_ingest write includes `_provenance_connector` and `_provenance_work_item` in properties for audit traceability (ASK Tenet 2).
4. For edges, resolve `from_label` and `to_label` to node IDs by querying existing nodes, then POST to `{knowledge_url}/ingest/edges`.
5. Returns count of upserted nodes.

**Source priority:** `source_type: "rule"` cannot overwrite `source_type: "agent"` summaries due to the existing source priority system (agent > llm > local/rule). This prevents XPIA-crafted webhook payloads from corrupting agent-contributed knowledge.

**server.py:**
- After `_route_and_deliver()`, call `evaluate_graph_ingest()` for matching rules.
- graph_ingest failures are logged but do not affect routing or work item status.

### Jinja2 Sandboxing

The `SandboxedEnvironment` from Jinja2's sandbox module prevents:
- Attribute access to dunder methods (`__class__`, `__subclasses__`)
- Function calls beyond simple dict/list access
- Import statements
- Any code execution beyond template variable interpolation

Templates receive `payload` (the event data dict) as the only context variable. Attempting `{{ ''.__class__ }}` raises `SecurityError`.

### Tests

- Model validation for graph_ingest rules
- Template rendering with real payloads
- Match evaluation (matching rule, non-matching rule, no-match = all)
- Node upsert HTTP calls (mocked knowledge API)
- Edge upsert with label-to-ID resolution
- Sandbox security: reject `{{ ''.__class__.__mro__ }}`
- Connector with both routes and graph_ingest

---

## Feature 3: In-Memory Windowed Cross-Source Correlation

### Summary

Extend graph_ingest rules with an optional `correlate` block that joins event data from a second connector within a time window before writing to the graph.

### Model Changes (agency_core/models/connector.py)

```python
class CorrelateConfig(BaseModel):
    source: str              # Name of another active connector
    on: str                  # Field name to join on (dot-path in payload)
    window_seconds: int = 60 # How far back to look

# Add to GraphIngestRule:
class GraphIngestRule(BaseModel):
    ...
    correlate: Optional[CorrelateConfig] = None
```

### Intake Changes

**New module: `correlation.py`**

```python
class EventBuffer:
    """In-memory rolling buffer of recent events per connector."""

    def __init__(self, default_window: int = 60):
        self._buffers: dict[str, deque] = {}  # connector -> deque of (timestamp, payload)
        self._locks: dict[str, threading.Lock] = {}
        self._default_window = default_window

    def record(self, connector_name: str, payload: dict) -> None:
        """Record an event. Evicts expired entries."""

    def lookup(self, connector_name: str, field: str, value: Any, window_seconds: int) -> Optional[dict]:
        """Find most recent event within window where payload[field] == value."""

    def drop(self, connector_name: str) -> None:
        """Clear buffer for a deactivated connector."""
```

- **Eviction:** Lazy on `record()` and `lookup()`. Entries older than `max(default_window, any active correlate window)` are dropped.
- **Thread safety:** One `threading.Lock` per connector name. No global lock.
- **Memory bound:** Each buffer entry is a (float, dict) tuple. Practical limit: thousands of events per connector per minute is fine for in-memory deques.

**server.py:**
- On every incoming event (webhook, poll, schedule), call `buffer.record(connector_name, payload)` **before** routing and graph_ingest.
- On SIGHUP, call `buffer.drop(name)` for deactivated connectors.

**graph_ingest.py:**
- When a rule has `correlate`, call `buffer.lookup(correlate.source, correlate.on, payload[correlate.on], correlate.window_seconds)`.
- If match found: add `correlated` dict to Jinja2 template context alongside `payload`.
- If no match: **skip the rule** (don't upsert partial data). Log at debug level.

### Tests

- Buffer record and lookup (exact match, expired entries)
- Lazy eviction behavior
- Thread safety (concurrent record + lookup)
- Correlation template rendering with `{{ correlated.field }}`
- No-match skip behavior (rule skipped, not errored)
- SIGHUP drops deactivated connector buffers

---

## Feature 4: query_graph Agent Tool

### Summary

Add a `query_graph` built-in tool for structured knowledge graph traversal. Distinct from existing `query_knowledge` (FTS-based similarity search).

### Tool Definition

```python
{
    "name": "query_graph",
    "description": "Query the knowledge graph by entity ID, relationships, or property filters. Returns structured nodes and edges, not text search results.",
    "parameters": {
        "type": "object",
        "properties": {
            "pattern": {
                "type": "string",
                "enum": ["get_entity", "get_neighbors", "filter_entities"],
                "description": "Query pattern to use"
            },
            "id": {
                "type": "string",
                "description": "Node ID (required for get_entity, get_neighbors)"
            },
            "relation": {
                "type": "string",
                "description": "Edge relation type (optional for get_neighbors)"
            },
            "kind": {
                "type": "string",
                "description": "Entity kind to filter (required for filter_entities)"
            },
            "property": {
                "type": "string",
                "description": "Property name to filter on (required for filter_entities)"
            },
            "value": {
                "type": "string",
                "description": "Property value to match (required for filter_entities)"
            }
        },
        "required": ["pattern"]
    }
}
```

### Knowledge Store Changes (store.py)

New methods:

- `filter_nodes_by_property(kind, property_name, value, limit=50) -> list[dict]` — SQL: `SELECT * FROM nodes WHERE kind = ? AND json_extract(properties, '$.' || ?) = ? LIMIT ?`
- `get_neighbors_subgraph(node_id, relation=None, limit=50) -> dict` — Fetches edges (filtered by relation if provided), resolves neighbor nodes, returns `{"nodes": [...], "edges": [...]}` bounded to `limit` nodes.

### Knowledge Server Changes (server.py in knowledge image)

New HTTP endpoints:

- `GET /graph/node/{id}` — Returns single node with properties, or 404
- `GET /graph/neighbors/{id}?relation=TYPE` — Returns subgraph of neighbors
- `GET /graph/filter?kind=K&property=P&value=V` — Returns matching nodes with inter-edges

All responses capped at 50 nodes. Response format:
```json
{
    "nodes": [{"id": "...", "label": "...", "kind": "...", "properties": {...}}],
    "edges": [{"source_id": "...", "target_id": "...", "relation": "..."}]
}
```

### Body Runtime Changes (knowledge_tools.py)

Register `query_graph` tool alongside existing knowledge tools. Handler:
1. Validate `pattern` parameter
2. Validate required params per pattern (e.g., `get_entity` requires `id`)
3. Dispatch to correct knowledge service endpoint via enforcer mediation, passing `agent` name for authorization scope filtering (ASK Tenet 24)
4. Return JSON response

All three graph endpoints accept an `agent` query parameter. The knowledge store filters returned nodes by `visible_channels` using the same authorization model as `query_knowledge`. Agents cannot see knowledge outside their scope.

### Tests

- Tool dispatch for each pattern
- 50-node response cap
- Missing/invalid entity handling (404 → helpful error message)
- Property filter SQL correctness
- Parameter validation (missing required params → error)

---

## Feature 5: Event ID Threading into Enforcer Audit

### Summary

Propagate the event envelope ID (`evt-*`) through the full activation chain so every enforcer audit entry for an event-triggered activation includes the `event_id`.

### Change Chain

1. **Gateway** (`internal/events/deliver_agent.go`):
   - Include `event_id` in message metadata: `metadata: {"event_id": event.ID}` in the POST to comms.

2. **Comms** (`images/comms/server.py`):
   - When writing session context for the body runtime, extract `event_id` from message metadata if present and include it in the context payload.

3. **Body runtime** (`images/body/body.py`):
   - Read `event_id` from session context at startup.
   - Include `X-Agency-Event-Id: {event_id}` header on all HTTP requests to the enforcer.

4. **Enforcer** (`images/enforcer/`):
   - `audit.go`: Add field to AuditEntry:
     ```go
     EventID string `json:"event_id,omitempty"`
     ```
   - `proxy.go` / `llm.go` / `mediation_proxy.go`: Read `X-Agency-Event-Id` header from incoming requests and populate `EventID` on every audit entry.

### Backward Compatibility

- `event_id` field is `omitempty` — old audit files without it parse correctly.
- Non-event activations (operator DMs, manual `agency send`) have no `event_id` in session context, so the header is absent and the audit field is null/omitted.
- No changes to audit log format version — additive field only.
- HMAC signature computation includes `event_id` when present (naturally, since it signs the full JSON entry).

### Tests

- Gateway delivery includes `event_id` in metadata
- Comms passes `event_id` to session context
- Body sends `X-Agency-Event-Id` header
- Enforcer writes `event_id` on audit entries
- Missing `event_id` (non-event activation) produces valid entries without the field
- Old audit files without `event_id` parse correctly

---

## Feature 6: Audit Summarization Job -> MissionMetrics

### Summary

Background goroutine in the gateway reads enforcer JSONL audit files, groups entries by `event_id`, computes per-mission-per-day metrics, and upserts `MissionMetrics` nodes to the knowledge graph.

### MissionMetrics Node Structure

```yaml
kind: MissionMetrics
label: "{mission_name}:{YYYY-MM-DD}"
properties:
  mission: string
  date: string                          # YYYY-MM-DD
  activations: int                      # count of unique event_ids
  total_input_tokens: int
  total_output_tokens: int
  estimated_cost_usd: float
  avg_tokens_per_activation: float
  model: string                         # most common model across activations
  escalation_count: null                # v2 — requires channel event_id threading
  findings_count: null                  # v2 — requires channel event_id threading
```

`escalation_count` and `findings_count` are nullable, defaulting to null in v1. They require channel message event_id threading to implement reliably — a future pass when Feature 5's threading is extended to comms channel messages.

### Gateway Changes

**New file: `internal/audit/summarizer.go`**

```go
type AuditSummarizer struct {
    ticker         *time.Ticker        // default 15 min
    missionMgr     *orchestrate.MissionManager  // in-process current state
    homeDir        string              // ~/.agency
    knowledgeURL   string              // http://knowledge:18092
    logger         *log.Logger
}

func (s *AuditSummarizer) Start(ctx context.Context)
func (s *AuditSummarizer) Summarize() ([]MissionMetric, error)
```

**Summarize() flow:**
1. Scan `{homeDir}/audit/*/enforcer-{date}.jsonl` for today and yesterday.
2. Parse each line as `AuditEntry`. Skip unparseable lines (log warning).
3. Filter to `type == "LLM_DIRECT"` entries (these have token counts and model info).
4. Group entries by `event_id`. Entries without `event_id` are grouped as "unattributed".
5. Map agent → mission:
   - **Current:** Query in-process mission manager (`missionMgr.GetAgentMission(agentName)`)
   - **Historical:** Read `{homeDir}/missions/*.yaml`, parse agent assignments. Cache parsed missions for the duration of the summarize call.
6. Aggregate per mission per day: sum tokens, count activations, compute cost, find modal model.
7. **Cost estimation:** Resolve pricing from routing/catalog model metadata.
   Unknown model aliases: log warning, skip entry's cost contribution (don't
   crash). Tokens still counted.
8. **HMAC verification:** When `ENFORCER_AUDIT_HMAC_KEY` is available, verify each line's signature before including it in metric computation. Skip lines with invalid signatures (log warning). When the key is not set, skip verification.
9. POST each `MissionMetrics` node to `{knowledgeURL}/ingest/nodes` with `source_type: "rule"`.

### CLI Command

`agency audit summarize` — Calls `POST /api/v1/admin/audit/summarize`. Returns summary table of missions + metrics. Endpoint calls `summarizer.Summarize()` synchronously.

### Goroutine Safety

- Summarizer runs as single goroutine with ticker. No concurrent summarize calls.
- File reads are sequential (no parallel JSONL parsing needed for reasonable file sizes).
- Context cancellation stops the ticker on gateway shutdown.

### Tests

- JSONL parsing (valid entries, malformed lines, missing fields)
- Metric aggregation (tokens, cost, activation count, modal model)
- Mission mapping: in-memory hit, disk fallback, unknown agent
- Cost estimation: known model pricing, unknown model warning + skip
- MissionMetrics node upsert structure
- CLI trigger via REST endpoint
- Entries without event_id grouped as "unattributed"

---

## Cross-Cutting Constraints

- **No broken tests.** Each feature's tests pass before starting the next.
- **Goroutine/thread safety.** Poll cron scheduling reuses existing loops. Correlation buffer uses per-connector locks. Summarizer is single-goroutine.
- **Jinja2 sandboxing.** graph_ingest templates use `SandboxedEnvironment` — field access only, no code execution.
- **query_graph response bound.** Max 50 nodes per response. Never returns full graph.
- **event_id backward compat.** Additive `omitempty` field. Old audit files parse correctly.
- **Cost estimation resilience.** Unknown models log warning and skip cost contribution. Summarizer never crashes on bad data.
