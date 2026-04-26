---
description: "Universal ingestion, edge provenance, community detection, hub analysis, query feedback, authorization scopes, and graph backend decision framework for the knowledge graph."
status: "Draft"
---

# Knowledge Graph Intelligence

*Patterns and enhancements that make the knowledge graph a universal, self-aware, and auditable organizational intelligence system.*

**Date:** 2026-04-05
**Status:** Draft
**Parent:** [Compounding Agent Organizations](../policy/compounding-agent-organizations.md)

---

## Overview

The knowledge graph today is a comms-only system. It learns from agent chat messages and nothing else. Connector payloads create structural nodes via graph_ingest rules, but the semantic synthesis pipeline — the part that discovers meaning — only processes comms messages. Tool outputs, documents, code, and external data are invisible to it.

Meanwhile, the graph's edges carry no formal provenance. You can't ask "how confident are we in this relationship?" The curator does sophisticated quality work but has no structural awareness of the graph's topology — no communities, no hub detection, no way to say "these 40 nodes form a coherent cluster about SSH security."

And the access model is channel-based, which works for agents but won't scale to multiple operators with different authorization scopes.

**Thesis:** The knowledge graph should ingest from any source, tag every edge with provenance, understand its own topology, learn from how it's queried, and enforce authorization at the principal level — not just the channel level. These capabilities compound: richer ingestion feeds better communities, provenance filtering improves community quality, communities inform authorization boundaries, and query feedback closes the loop.

**Inspiration:** Patterns drawn from [GitNexus](https://github.com/abhigyanpatwari/GitNexus) (Leiden community detection, hybrid search with RRF, confidence scoring on all edges) and [Graphify](https://github.com/safishamsi/graphify) (dual deterministic+semantic extraction pipeline, three-tier edge provenance, god node detection, query feedback loop). No code from either project is incorporated — only the architectural patterns.

---

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Edge provenance model | Three tiers: EXTRACTED, INFERRED, AMBIGUOUS | Maps cleanly to extraction source (deterministic vs logical vs semantic). Enables governance filtering and retrieval weighting. Aligns with ASK Tenet 2 (every action traced) and Tenet 6 (all trust explicit). |
| Authorization model | Principal UUID + scope on nodes/edges | Channel-based ACLs don't scale to multiple operators. UUIDs prevent name collisions across agents, operators, and deployments. Knowledge graph is the first consumer; platform-wide adoption follows. |
| Ingestion scope | Any source type, dual extraction | VPT principle: deterministic parsing for structured data (zero tokens), LLM synthesis only where parsers leave semantic gaps. Every source type hits the deterministic layer first. |
| Community detection | Leiden algorithm via graspologic | Improvement over Louvain. Proven at scale in both GitNexus and Graphify. Recursive splitting handles skewed distributions. |
| Community scope | Scope-aware, agent-facing | Communities computed within scope boundaries. Agents can query community membership and hubs within their authorization scope. ASK Tenet 24 prevents community structure from leaking cross-scope relationships. |
| Query feedback | Agent-initiated via `save_insight` tool | Most VPT-aligned: agents do the synthesis work and explicitly flag what's worth persisting. Cheaper than auto-ingestion, higher quality than requiring operator curation of every query. |
| Graph backend strategy | SQLite now, abstraction boundary for future swap | Decision framework with measurable triggers. `KnowledgeStore` is the swap point — all new features go through it, never raw SQL in other modules. |
| Spec structure | Layered phases, independently testable | Schema foundations → universal ingestion → graph intelligence → feedback loop. Each phase builds on the previous but is independently implementable and testable. |

---

## Phase 1: Schema Foundations

### Edge Provenance Tiers

Every edge in the graph gets a `provenance` field with one of three values:

| Tier | Value | Meaning | Source |
|---|---|---|---|
| 1 | `EXTRACTED` | Structurally derived, deterministic | AST parsing, graph_ingest rules, regex patterns, config parsing |
| 2 | `INFERRED` | Logically derived with high confidence | Call graph analysis, import resolution, curator relationship inference (tier 1 rules), cross-source correlation |
| 3 | `AMBIGUOUS` | Semantically derived, uncertain | LLM synthesis, curator tier 2/3 proposals, agent `save_insight` |

**Weight mapping:** EXTRACTED edges get weight 1.0, INFERRED get 0.8, AMBIGUOUS get 0.6. These are defaults — the curator or operator can adjust individual edges.

**Schema change on `edges` table:**

```sql
ALTER TABLE edges ADD COLUMN provenance TEXT DEFAULT 'AMBIGUOUS';
-- Constraint: CHECK(provenance IN ('EXTRACTED', 'INFERRED', 'AMBIGUOUS'))
```

**Migration:** Existing edges are classified by their `source_type` field (already present on edges): `rule` → `EXTRACTED`, `synthesis` → `AMBIGUOUS`, `inferred` → `INFERRED`. Edges with no `source_type` default to `AMBIGUOUS`. One-time migration at startup, same pattern as the ontology migration.

**How it's used:**

- GraphRAG retrieval weights results by provenance — EXTRACTED edges contribute more to briefing relevance than AMBIGUOUS ones
- Agents can filter `query_graph` by minimum provenance tier
- The curator's community detection uses provenance to compute community cohesion — a community held together by EXTRACTED edges is more reliable than one held together by AMBIGUOUS edges
- Operators can audit the graph by provenance: "show me all AMBIGUOUS edges" surfaces the least-certain knowledge for review

**ASK alignment:** Tenet 2 (every action leaves a trace) — provenance is the trace. Tenet 6 (all trust is explicit) — the confidence of every relationship is declared, not assumed.

### Principal UUID Model

Every principal referenced in the graph uses a UUID as its identifier. Human-readable names are display labels, not identifiers.

**Principal ID format:** `type:uuid` — e.g., `operator:a1b2c3d4-e5f6-7890-abcd-ef1234567890`, `agent:b2c3d4e5-f6a7-8901-bcde-f12345678901`.

**Where IDs are assigned:**

- **Agents** get a UUID at `agency agent create` time (or container ID if already present)
- **Operators** get a UUID at first authentication (`agency setup` or when the principal ACL spec lands)
- **Teams/roles** get UUIDs when created
- **Channels** get UUIDs when created — channel names become display labels

**Principal registry table:**

```sql
CREATE TABLE IF NOT EXISTS principal_registry (
    uuid TEXT PRIMARY KEY,
    type TEXT NOT NULL,        -- 'operator', 'agent', 'team', 'role', 'channel'
    name TEXT NOT NULL,        -- human-readable display label
    created_at TEXT NOT NULL,
    metadata TEXT DEFAULT '{}'  -- JSON: additional properties
);

CREATE INDEX IF NOT EXISTS idx_principal_type_name ON principal_registry(type, name);
```

All UIs and CLI output resolve UUIDs to names for display. The graph stores UUIDs; humans see names.

**Migration window:** During the transition, both `type:name` and `type:uuid` formats are accepted. Name-format references are resolved to UUIDs via the registry where possible. Unresolvable names are logged as warnings and stored as-is, pending manual mapping.

**Platform-wide adoption:** The knowledge graph is the first consumer of the UUID model. A separate platform spec will extend this pattern to comms, intake, connectors, and the gateway. This spec establishes the registry and the format; the platform spec handles the migration of existing subsystems.

### Authorization Scopes

Replace the flat `source_channels` list with a `scope` model that supports principals.

**Current state:** Nodes carry `source_channels` (JSON array of channel names). `find_nodes()` filters by `visible_channels`. This is agent-centric.

**New model:** Introduce `scope` as a first-class concept:

```python
{
    "channels": ["uuid-alpha", "uuid-beta"],    # channel visibility (existing, UUIDs)
    "principals": ["operator:uuid-geoff"],       # principal visibility (new)
    "classification": "internal"                 # informational: public, internal, restricted, confidential
}
```

**Schema changes:**

```sql
ALTER TABLE nodes ADD COLUMN scope TEXT DEFAULT '{}';  -- JSON
ALTER TABLE edges ADD COLUMN scope TEXT DEFAULT '{}';  -- JSON
-- source_channels preserved for backward compat during migration
-- scope is the authoritative field
```

**Migration:** `source_channels` values copied into `scope.channels`. Channel names resolved to UUIDs where the registry has a mapping; left as names otherwise (resolved lazily as channels are registered).

**Query-time enforcement:**

- `find_nodes()` gains a `principal` parameter alongside the existing `visible_channels`
- A node is visible if the querying principal's scope overlaps with the node's scope (channel match OR principal match)
- Graph traversal stops at scope boundaries — you can't traverse an edge into a node outside your scope, even if intermediate nodes are visible
- Community membership queries respect the same filtering

**Edge scope:** Edges inherit scope from their source node by default, but can be narrowed (never widened).

**Classification tiers** are informational for now — they don't gate access (that's for the full principal-based ACL spec). Storing them from day one means the data is there when enforcement arrives.

**What this spec doesn't cover** (deferred to principal ACL spec):

- Principal hierarchies and scope inheritance
- Delegation of graph access
- Cross-deployment scope federation
- Classification-based access enforcement
- Platform-wide channel UUID migration

**ASK alignment:** Tenet 24 (knowledge access bounded by authorization scope), Tenet 12 (synthesis cannot exceed individual authorization).

### Performance Benchmarks

Add to the curator's health metrics:

- `community_detection_ms` — wall time for Leiden pass
- `traversal_p95_ms` — 95th percentile multi-hop query time
- `graph_size` — total nodes + edges
- `scope_resolution_ms` — time spent resolving scope filters

These metrics feed the graph backend decision framework (see below) and make migration decisions data-driven.

---

## Phase 2: Universal Ingestion Pipeline

Extend the knowledge service to ingest from any source type. The dual extraction pipeline (deterministic + semantic) applies to everything, with source-type-specific deterministic parsers.

### Source Types and Extractors

| Source Type | Deterministic Extractor | What it extracts | LLM Synthesis? |
|---|---|---|---|
| Comms messages | RuleIngester (existing) | Decisions, blockers, agent/channel structure | Yes (existing) |
| Connector payloads | graph_ingest rules (existing) | Structural nodes/edges per YAML rules | Yes (new) |
| Tool outputs | Format-aware parser (new) | URLs, IPs, CVEs, error codes, structured data | Yes (new) |
| Code files | Tree-sitter AST (new) | Functions, classes, imports, call graph | Selective |
| Documents (markdown, text) | Heading/link/reference parser (new) | Document structure, cross-references, entities | Yes |
| PDFs | Text extraction + structure parser (new) | Sections, tables, references | Yes |
| URLs/web pages | HTML parser + metadata (new) | Title, links, structured data, key content | Yes |
| Config files (YAML, JSON, TOML) | Schema-aware parser (new) | Keys, values, structure, references | No — deterministic is sufficient |
| Images | None — no deterministic path | N/A | Yes (vision) |

### Architecture

```
Source → SourceClassifier → DeterministicExtractor(s) → MergeBuffer → LLMSynthesizer
                                                                          ↓
                                                                    KnowledgeStore
```

1. **SourceClassifier** — Identifies source type by MIME type, file extension, or payload structure. Routes to the appropriate deterministic extractor.
2. **DeterministicExtractor** — Per-type parser that produces `{nodes, edges}` with `provenance: EXTRACTED` or `INFERRED`. Zero token cost.
3. **MergeBuffer** — Collects deterministic output and decides whether LLM synthesis would add value. Criteria: if the deterministic layer extracted structure but not meaning (code without comments, config without context), synthesis runs. If the deterministic layer fully captured the content (simple config, well-structured data), synthesis is skipped.
4. **LLMSynthesizer** — Existing synthesizer, extended to accept any content type (not just comms messages). Produces edges with `provenance: AMBIGUOUS`.

### Ingestion Entry Points

- **Existing:** CommsSubscriber (WebSocket), graph_ingest (connector payloads via intake)
- **New: `POST /ingest`** endpoint on the knowledge service — accepts raw content with metadata (source type, scope, provenance hint). The body runtime's tools can forward outputs here. Operators can POST documents directly.
- **New: `agency graph ingest <file-or-url>`** CLI command — operator-initiated ingestion of files, directories, or URLs.
- **New: Watch mode** — configurable directory watch (`~/.agency/knowledge/watch/`) for auto-ingestion of dropped files. Optional, off by default.

### Scope Tagging

Every ingested item carries scope metadata from its source:

- Comms messages inherit channel scope (existing behavior)
- Connector payloads inherit the connector's configured scope
- Operator-ingested documents inherit the operator's principal scope
- Tool outputs inherit the contributing agent's scope

### VPT Principle

Every source type hits the deterministic layer first. LLM synthesis only fires when the deterministic layer leaves semantic gaps. Config files and well-structured data may never hit the LLM at all. Code gets AST extraction for free — LLM only runs if comments or docstrings suggest higher-level relationships worth capturing.

**ASK alignment:** Tenet 3 (mediation complete) — all ingestion goes through the knowledge service, no direct writes. Tenet 23 (knowledge is durable infrastructure) — the graph captures knowledge regardless of where it originated.

---

## Phase 3: Graph Intelligence

Two new curator operations that give the graph structural self-awareness, plus agent-facing query patterns.

### Community Detection (Leiden)

**What it does:** Groups nodes into functional communities — clusters of nodes more densely connected to each other than to the rest of the graph. "These 30 nodes are all about SSH security." "These 15 nodes form a cluster around the production database."

**Algorithm:** Leiden (improvement over Louvain). Run via NetworkX + graspologic, same approach used by Graphify. The curator already loads graph data for analysis — community detection runs on the same in-memory representation.

**When it runs:** New operation in the curator cycle, after `relationship_inference()`, before `compute_health_metrics()`. More expensive than other operations — configurable interval independent of the main cycle (default: every 6th cycle, ~60 minutes).

**Implementation:**

1. Load connected subgraph into NetworkX (exclude isolates, `OntologyCandidate`, soft-deleted nodes)
2. Filter by provenance — edges with `provenance: EXTRACTED` or `INFERRED` only. AMBIGUOUS edges introduce noise into community structure.
3. Run Leiden with resolution parameter tuned to graph size
4. Assign isolates to singleton communities
5. Recursive splitting: any community exceeding 25% of the graph gets a second Leiden pass on its subgraph
6. Compute per-community cohesion score: internal edge density / expected density

**Scope-aware:** Community detection runs within scope boundaries. Nodes from disjoint scopes cannot be in the same community. This prevents community structure from leaking cross-scope relationships.

**Storage — node properties:**

```sql
ALTER TABLE nodes ADD COLUMN community_id TEXT;
ALTER TABLE nodes ADD COLUMN community_cohesion REAL;
```

**Storage — Community nodes in the graph:**

```yaml
kind: Community
label: "community:ssh-security"  # auto-labeled from highest-degree member labels
properties:
  member_count: 30
  cohesion: 0.72
  provenance_mix: {EXTRACTED: 45, INFERRED: 20, AMBIGUOUS: 8}
  top_members: ["openssh 8.9", "weak-ssh-config", "jump-host"]
```

### Hub (God Node) Detection

**What it does:** Identifies nodes with disproportionately high connectivity — architectural bottlenecks, bridging concepts, or governance-critical entities.

**Algorithm:** Degree centrality with filtering:

1. Compute degree centrality for all nodes
2. Filter out synthetic/structural nodes: `agent`, `channel`, `task`, `Community`, `OntologyCandidate`
3. Filter out mechanical hubs: nodes whose label contains a file extension, nodes with empty summaries
4. Rank remaining nodes by degree. Top N (configurable, default 20) are flagged as hubs.
5. Bonus: bridge detection — nodes with high betweenness centrality that connect otherwise-separate communities get flagged as bridges, distinct from pure hubs.

**Storage — node properties:**

```sql
ALTER TABLE nodes ADD COLUMN hub_score REAL;
ALTER TABLE nodes ADD COLUMN hub_type TEXT;  -- 'hub', 'bridge', or null
```

**Why it matters for governance:** A hub node that gets poisoned has blast radius across the entire graph. Hub status feeds into the curator's anomaly detection — mutations to hub nodes trigger higher scrutiny. When the full principal ACL model arrives, hub nodes could require elevated authority to modify.

### Agent-Facing Query Patterns

New patterns on the existing `query_graph` tool:

| Pattern | Parameters | Returns |
|---|---|---|
| `get_community` | `node_id` | The community this node belongs to, its members, cohesion score |
| `list_communities` | `scope` (optional) | All communities visible to the querying principal, ranked by size |
| `get_hubs` | `scope` (optional), `limit` | Top hub nodes in the querying principal's scope |
| `community_overlap` | `community_id_a`, `community_id_b` | Shared edges/nodes between two communities, bridge nodes |

All queries enforce scope filtering — agents only see communities and hubs within their authorization scope.

**ASK alignment:** Tenet 24 (knowledge access bounded), Tenet 7 (least privilege — community structure doesn't leak scope boundaries).

---

## Phase 4: Query Feedback Loop

When agents combine graph retrieval with their own reasoning and arrive at a novel insight, that synthesis should flow back into the graph.

### `save_insight` Tool

New body runtime tool available to all agents:

```python
save_insight(
    insight: str,            # The conclusion or synthesis
    source_nodes: list[str], # Node IDs that informed the insight (from prior query_knowledge results)
    confidence: str,         # "high", "medium", "low" — agent's self-assessed certainty
    tags: list[str],         # Optional categorization
)
```

**Note on confidence vs provenance:** `confidence` is the agent's subjective assessment of certainty in its conclusion. `provenance` (EXTRACTED/INFERRED/AMBIGUOUS) describes the extraction method. These are orthogonal — an agent can be highly confident in an INFERRED insight. The `save_insight` tool always produces `INFERRED` provenance (agent reasoned from evidence); the `confidence` field is stored as a node property for retrieval weighting.

**What it produces:**

- A new `finding` node with the insight as summary
- `DERIVED_FROM` edges pointing back to each source node, with `provenance: INFERRED` (the agent reasoned from evidence)
- Scope inherited from the intersection of source nodes' scopes (the insight can't be more visible than its inputs — ASK Tenet 12)

**Example:** A security agent queries the graph, finds that a vulnerability affects a specific software version, and that software is installed on an internet-facing host. The agent calls:

```python
save_insight(
    insight="prod-web is exposed to CVE-2023-44487 via nginx 1.24 on an internet-facing interface",
    source_nodes=["node-cve-2023-44487", "node-nginx-1.24", "node-prod-web"],
    confidence="high",
    tags=["risk", "internet-facing"]
)
```

The graph now contains that synthesized conclusion linked to its evidence — and the next agent that queries about prod-web gets it for free.

### Feedback Quality

Not every insight is worth keeping. Quality controls:

1. **Source node validation** — All `source_nodes` must exist and be visible to the contributing agent. Can't create insights from nodes you can't see.
2. **Duplicate detection** — The curator's existing near-duplicate detection catches repeated insights via `post_ingestion_check()`.
3. **Trust weighting** — Insight nodes carry the contributing agent's trust level in provenance metadata. Low-trust agents' insights get lower retrieval weight.
4. **Decay** — Insights without inbound edges (nothing references them, no agent ever found them useful) are candidates for the orphan pruner after the standard age threshold.

### How It Compounds

The insight node participates in all normal graph operations:

- Community detection may cluster it with related findings
- Hub detection may surface it if many edges converge on it
- Other agents' `query_knowledge` calls may retrieve it
- Future `save_insight` calls may reference it as a source node — insights chain

This is the compounding loop: agents query → discover → synthesize → save → future agents benefit → query with richer context → discover deeper connections.

**ASK alignment:** Tenet 23 (knowledge is durable infrastructure — insights persist beyond agent sessions), Tenet 12 (synthesis scoped to source authorization).

---

## Graph Backend Decision Framework

The SQLite graph works today. This section defines when it stops being the right answer and what the migration path looks like.

### Current Architecture

SQLite + FTS5 + sqlite-vec. Single file at `~/.agency/knowledge/graph.db`. All operations through `KnowledgeStore` methods. Zero infrastructure dependencies.

### Stay on SQLite When All Hold

- Single deployment (one Agency instance, one machine)
- Graph under ~100K nodes / ~500K edges
- Community detection completes within 60 seconds
- Multi-hop traversals (3+ hops) return within 2 seconds
- Single operator or small team with simple scope hierarchy

### Evaluate Migration When Any Appear

- Multi-hop queries degrade past 5 seconds
- Community detection times out (>60s) regularly
- Graph exceeds 500K nodes or 2M edges
- Multiple Agency deployments need to share a knowledge graph
- Operators need ad-hoc exploratory queries (arbitrary Cypher/Gremlin) beyond what `query_graph` patterns support
- Authorization model requires graph-native access control (e.g., Neo4j's role-based security)

### Candidate Backends

| Backend | Type | Strengths | Trade-offs |
|---|---|---|---|
| **Kùzu** | Embedded | Cypher support, columnar storage, no server, drop-in SQLite replacement pattern | Young project, smaller ecosystem |
| **Neo4j** | Server | Mature, native Cypher, built-in graph algorithms (GDS), role-based security, enterprise support | Infrastructure dependency, Java runtime, licensing cost at scale |
| **Memgraph** | Server | Cypher-compatible, in-memory, fast traversals, built-in Leiden/PageRank | Requires server, memory-bound |
| **FalkorDB** | Server | Redis-compatible, Cypher subset, low latency | Smaller community, subset of Cypher |

### Recommended Migration Path

```
SQLite (now)
  → Kùzu (when graph size or query complexity demands it — maintains embedded/zero-infra model)
    → Neo4j or Memgraph (when multi-deployment federation or enterprise access control demands it)
```

### Abstraction Boundary

The `KnowledgeStore` class is the swap point. All graph operations — `add_node()`, `add_edge()`, `find_nodes()`, `get_neighbors()`, `traverse()` — go through it. No SQL leaks outside the store.

**What this spec preserves:** Every new feature (provenance tiers, scopes, communities, hubs, feedback loop) is implemented through `KnowledgeStore` methods, not raw SQL in other modules. If the backing store changes from SQLite to Kùzu to Neo4j, only `KnowledgeStore` internals change. The curator, synthesizer, ingestion pipeline, and query tools are unaffected.

**Performance benchmarks to track:** The curator's health metrics already run every cycle. The benchmarks added in Phase 1 (`community_detection_ms`, `traversal_p95_ms`, `graph_size`, `scope_resolution_ms`) make the migration decision data-driven, not gut-feel.

---

## Implementation Phases

### Phase 1: Schema Foundations

- Edge provenance tiers (`EXTRACTED`, `INFERRED`, `AMBIGUOUS`) on the `edges` table
- Principal UUID model and `principal_registry` table
- Authorization `scope` on nodes and edges
- Migration of existing data (edges get provenance based on source, `source_channels` copied into `scope.channels`, names resolved to UUIDs where possible)
- `KnowledgeStore` methods updated to accept and filter by provenance and scope
- `find_nodes()` gains `principal` parameter
- Performance benchmarks added to health metrics

**Testable independently:** Existing ingestion and retrieval continue to work. New fields default safely. Migration is idempotent.

### Phase 2: Universal Ingestion Pipeline

- `SourceClassifier` — routes content to the right extractor by type
- Deterministic extractors: tree-sitter (code), structure parser (config files), heading/link parser (markdown/text), PDF text extractor, HTML parser (web pages)
- `MergeBuffer` — decides whether LLM synthesis adds value after deterministic extraction
- `LLMSynthesizer` extended to accept any content type, not just comms
- `POST /ingest` endpoint on knowledge service
- `agency graph ingest <file-or-url>` CLI command
- Watch mode for `~/.agency/knowledge/watch/` (optional, off by default)
- All new edges tagged with appropriate provenance tier
- All new nodes tagged with scope from their source

**Testable independently:** Ingest a code file, a markdown doc, a config file. Verify deterministic extraction produces EXTRACTED edges. Verify LLM synthesis (when triggered) produces AMBIGUOUS edges. Verify scope tagging.

### Phase 3: Graph Intelligence

- Leiden community detection in curator cycle (graspologic)
- Recursive community splitting for oversized communities
- Community cohesion scoring with provenance weighting
- Hub detection with synthetic/structural node filtering
- Bridge node detection (cross-community connectors)
- `Community` nodes in the graph
- Agent-facing query patterns: `get_community`, `list_communities`, `get_hubs`, `community_overlap`
- Scope-aware community detection and querying
- Community/hub metrics added to curator health output

**Testable independently:** Populate a graph with known structure, run community detection, verify expected clusters. Verify scope filtering prevents cross-scope communities. Verify hub detection surfaces the right nodes.

### Phase 4: Feedback Loop

- `save_insight` body runtime tool
- Source node validation and scope intersection
- `DERIVED_FROM` edge type with `INFERRED` provenance
- Integration with curator: duplicate detection, orphan pruning, trust weighting
- Insight chaining (insights referencing prior insights)

**Testable independently:** Agent saves insight → verify node created with correct edges, provenance, and scope. Save duplicate insight → verify deduplication. Save insight referencing out-of-scope nodes → verify rejection.

### Dependency Chain

```
Phase 1 (schema) → Phase 2 (ingestion) → Phase 3 (intelligence) → Phase 4 (feedback)
                                        ↘ Phase 4 can start after Phase 1 if needed
```

Phase 4 only truly depends on Phase 1 (provenance and scope). It can be developed in parallel with Phase 3 if desired.

---

## ASK Compliance

| Tenet | How this spec complies |
|---|---|
| Tenet 2 (Every action traced) | Edge provenance is the trace. Every relationship declares its extraction source. Append-only curation log records all community, hub, and insight operations. |
| Tenet 3 (Mediation complete) | All ingestion goes through the knowledge service. No direct writes to the graph from agents or external sources. |
| Tenet 6 (All trust explicit) | Provenance tiers make confidence explicit. No implicit trust in any edge. AMBIGUOUS edges are declared uncertain. |
| Tenet 7 (Least privilege) | Scope filtering on all queries. Community structure doesn't leak scope boundaries. Agents only see communities and hubs within their authorization scope. |
| Tenet 12 (Synthesis bounds) | `save_insight` scope is the intersection of source node scopes. Insights can't be more visible than their inputs. Community detection runs within scope boundaries. |
| Tenet 23 (Knowledge is durable infrastructure) | Universal ingestion ensures knowledge persists regardless of source. Insights persist beyond agent sessions. Graph backend abstraction ensures knowledge survives infrastructure changes. |
| Tenet 24 (Knowledge access bounded) | Principal-based scope model replaces flat channel ACLs. UUIDs prevent identity collisions. Query-time enforcement stops traversal at scope boundaries. |

---

## Platform-Wide Implications

This spec establishes patterns that require follow-on specs:

1. **Principal ACL Spec** — Principal hierarchies, scope inheritance, delegation, classification-based enforcement. The knowledge graph's scope model is the foundation; this spec extends it platform-wide.
2. **Platform UUID Adoption Spec** — Migrate comms, intake, connectors, and the gateway to UUID-based identity for channels, agents, and operators. The `principal_registry` pattern established here is the precedent.
3. **Channel UUID Migration Spec** — Channels currently use string names across the entire platform. Moving to UUIDs requires migration across comms, intake, knowledge, and all connector configurations.

---

## Testing

### Phase 1 Tests

- Provenance field added to edges, default `AMBIGUOUS`
- Migration assigns correct provenance to existing edges based on source
- `find_nodes()` filters by provenance minimum
- `principal_registry` CRUD operations
- UUID resolution: `type:uuid` resolved to display name
- Name fallback: `type:name` resolved to UUID via registry during migration window
- Scope field on nodes and edges, default `{}`
- `find_nodes()` with `principal` parameter respects scope
- Graph traversal stops at scope boundaries
- Edge scope cannot be wider than source node scope
- Performance benchmarks recorded in health metrics

### Phase 2 Tests

- `SourceClassifier` routes each source type to correct extractor
- Tree-sitter extractor produces EXTRACTED edges from code files
- Config parser produces EXTRACTED edges, no LLM synthesis triggered
- Markdown parser extracts headings, links, cross-references
- `MergeBuffer` skips LLM synthesis for fully-extracted content
- `MergeBuffer` triggers LLM synthesis for content with semantic gaps
- `POST /ingest` accepts content with metadata, routes through pipeline
- `agency graph ingest` CLI ingests files and URLs
- Watch mode picks up new files in watch directory
- All new nodes carry correct scope from source
- All new edges carry correct provenance tier

### Phase 3 Tests

- Leiden community detection groups densely-connected nodes
- Community detection uses only EXTRACTED and INFERRED edges
- Recursive splitting breaks communities exceeding 25% of graph
- Cohesion scores computed correctly (internal density / expected density)
- Community detection respects scope boundaries — no cross-scope communities
- Hub detection filters out synthetic/structural nodes
- Bridge detection identifies cross-community connectors
- `Community` nodes created with correct metadata
- `get_community` returns members and cohesion for a node's community
- `list_communities` respects scope filtering
- `get_hubs` returns top hubs within scope
- `community_overlap` returns shared structure between communities

### Phase 4 Tests

- `save_insight` creates finding node with correct summary
- `DERIVED_FROM` edges created with `INFERRED` provenance
- Scope is intersection of source node scopes
- Source node validation: all nodes must exist and be visible to agent
- Out-of-scope source nodes rejected
- Duplicate insight detected by `post_ingestion_check()`
- Insight nodes participate in community detection
- Insight nodes participate in hub detection
- Insight chaining: insight references prior insight as source node
- Trust weighting: low-trust agent insights get lower retrieval weight
- Orphan pruner targets insights with no inbound edges after age threshold
