---
description: "Spec for an infrastructure service that maintains graph quality through heuristic curation."
---

# Knowledge Graph Curator

*Spec for an infrastructure service that maintains graph quality through heuristic curation.*

**Parent:** [Compounding Agent Organizations](../policy/compounding-agent-organizations.md) — spec #1 of 5.

---

## Overview

The knowledge graph compounds organizational intelligence over time — but only if what compounds is signal, not noise. The curator is a background process inside the knowledge container that maintains graph quality through deterministic, heuristic operations: duplicate detection, orphan pruning, cluster analysis, and anomaly flagging.

The curator is infrastructure, not an agent. It has no LLM dependency. It runs as a background loop alongside the existing ingestion loop, with an additional post-ingestion hook for immediate duplicate detection. All mutations are auditable and reversible.

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Deployment | Background loop inside knowledge container | Direct SQLite access, follows existing ingestion loop pattern, avoids HTTP overhead |
| Intelligence | Heuristic/algorithmic, no LLM | Zero token cost, deterministic, testable. Semantic curation (LLM) can plug in later |
| Trigger model | Event-driven + periodic hybrid | Post-ingestion hook catches duplicates immediately; periodic loop handles expensive operations |
| Quarantine authority | Flag only, never quarantine | ASK Tenet 16: quarantine authority is operator and security function only. Curator flags anomalies for operator review |
| Mutation model | Soft-delete with recovery window | Reversible by default. Hard cleanup after 7-day window. Append-only curation log for audit |
| Bootstrap safety | Observe-only mode for first 48 hours | Early graph is most vulnerable to miscalibrated curation. Operator reviews log before enabling active mode |

## Architecture

### Entry Points

**1. `post_ingestion_check(node_id)`**

Called synchronously after every `add_node()` in both the `RuleIngester` and `LLMSynthesizer`. Runs near-duplicate detection only.

Fast path: one SQL query against existing nodes with the same `kind`. Checks for normalized label matches (lowercase, stripped whitespace, collapsed punctuation). If a match is found with >90% string similarity, merges into the existing node using the store's existing merge logic (higher source priority wins for summary, properties merged, channels unioned).

This catches the most common quality issue — duplicates — before they propagate into GraphRAG briefings.

**2. `CurationLoop`**

An async background loop started alongside the ingestion loop in `server.py`. Runs on a configurable interval (default: 10 minutes). Executes the full curation pass sequentially:

```
fuzzy_duplicate_scan()
  → orphan_pruning()
    → cluster_analysis()
      → anomaly_detection()
        → compute_health_metrics()
```

Each operation is an independent method on the `Curator` class. The loop catches exceptions per-operation so a failure in one doesn't skip the rest. Each operation wraps its mutations in a SQLite transaction — agents performing retrieval never see partial curation state (per the parent strategy doc's atomicity requirement).

### Module Structure

```
agency/images/knowledge/
├── curator.py          ← Curator class, CurationLoop, post_ingestion_check()
├── store.py            ← KnowledgeStore (existing, minor changes)
├── server.py           ← HTTP server (existing, new endpoints + loop lifecycle)
├── ingester.py         ← RuleIngester (existing, adds post_ingestion_check call)
└── synthesizer.py      ← LLMSynthesizer (existing, adds post_ingestion_check call)
```

## Curation Operations

### 1. Near-Duplicate Detection (post-ingestion)

**Trigger:** Every `add_node()` call.

**Algorithm:** Query nodes with the same `kind`. Normalize labels (lowercase, strip whitespace, collapse repeated punctuation). Compare using character-level similarity ratio (`difflib.SequenceMatcher`). Threshold: >0.90 = auto-merge. Maximum 20 comparisons per check to bound latency in the ingestion path.

**Merge semantics:** The absorbed node's edges (both inbound and outbound) are repointed to the surviving node. Duplicate parallel edges after repointing are deduplicated (same source, target, and relation = keep highest weight). The absorbed node gets `curation_status = 'merged'` and `merged_into` set to the surviving node's ID. The entire operation runs in a single SQLite transaction.

**Action:** Merge into the existing node. Log the merge in `curation_log`.

**Note:** The store's `add_node()` already deduplicates by exact `(label, kind)` case-insensitively. This catches near-misses: trailing whitespace, punctuation variations, minor spelling differences. The 0.90 threshold is deliberately higher than the periodic scan's 0.85 — the post-ingestion check uses a simpler algorithm and runs in the hot path, so it's conservative. The periodic scan's richer algorithm (token overlap + sequence matching) can safely merge at a lower threshold.

### 2. Fuzzy Duplicate Scan (periodic)

**Trigger:** Every curation cycle (10 minutes).

**Algorithm:** For each `kind`, compare nodes added since the last scan against all existing nodes of that kind. Uses token overlap (split on whitespace/punctuation, compute Jaccard similarity) as the primary metric, with `difflib.SequenceMatcher` ratio as a secondary check for short labels (≤3 tokens) where token overlap is unreliable. Per-kind batch ceiling of 500 nodes; larger kinds are scanned progressively across cycles (round-robin).

**Thresholds:**
- \>0.85 similarity: auto-merge (higher source priority wins)
- 0.70–0.85 similarity: flag for operator review
- <0.70: skip

**Merge semantics:** Same as near-duplicate detection: edges repointed, parallel edges deduplicated, absorbed node marked `merged`, entire operation in a single SQLite transaction. Merges are only performed between nodes with overlapping `source_channels` sets (or where at least one has no channel restriction). This prevents cross-authorization synthesis — two nodes from completely disjoint channel scopes are flagged for operator review, not auto-merged (ASK Tenet 12).

**Action:** Merge or flag. Log all actions. The periodic scan skips pairs where both nodes were created before the last scan (already evaluated).

### 3. Orphan Pruning (periodic)

**Trigger:** Every curation cycle.

**Algorithm:** Find nodes with zero edges (not in `edges.source_id` and not in `edges.target_id`) that are older than a configurable age threshold (default: 24 hours).

**Exemptions:** Structural kinds (`agent`, `channel`, `task`) are never pruned — they're meaningful anchors even without edges.

**Action:** Soft-delete. Node remains in the database with `curation_status = 'soft_deleted'` but is excluded from retrieval.

**Hard-delete cleanup:** A separate step at the end of each curation cycle queries for nodes where `curation_status = 'soft_deleted'` and `curation_at` is older than the recovery window (default: 7 days). For each such node: (1) delete all edges where the node is source or target, (2) clear any `merged_into` references pointing to the node, (3) delete the node row, (4) write a `hard_delete` entry to `curation_log` before the row is removed. The entire per-node cleanup runs in a transaction.

### 4. Cluster Concentration (periodic)

**Trigger:** Every curation cycle.

**Algorithm:** Count nodes per `kind`. If any single kind exceeds 40% of total node count, flag it as over-concentrated.

**Action:** Flag only — surfaces the metric to operators. Does not auto-prune. Over-concentration may be legitimate (e.g., a security team generating many `vulnerability` nodes).

### 5. Anomaly Detection (periodic)

**Trigger:** Every curation cycle.

**Algorithm:** Two checks:
- **Burst detection:** For each agent, compare contribution count in the last hour to their historical hourly average. If >3x average, flag all recent contributions from that agent. Agents with fewer than 4 hours of contribution history are exempt (insufficient baseline).
- **Dominance detection:** In any rolling 1-hour window, if a single agent contributed >50% of all new nodes, flag those contributions. Exempt when total new nodes in the window is <5 (too small a sample).

**Action:** Flag only — sets `curation_status = 'flagged'` with reason. Flagged nodes remain in retrieval (they may be legitimate) but are surfaced to operators. The curator does not quarantine (ASK Tenet 16).

### 6. Health Metrics (periodic)

**Trigger:** Every curation cycle.

**Computes:**
- Orphan ratio: nodes with zero edges / total nodes
- Duplicate density: merges performed in the last 24 hours / total nodes
- Cluster distribution: node count per kind, Gini coefficient of distribution
- Flagged count: nodes currently flagged
- Soft-deleted count: nodes in soft-delete recovery window
- Total nodes, total edges

**Action:** Stored in the `curation_log` table as a `metrics` action with the full metrics JSON in the `detail` field. This provides both durability across restarts and historical metrics over time. The most recent `metrics` entry is served via the `/stats` endpoint's `curation` key.

## Data Model Changes

### Additions to `nodes` table

```sql
ALTER TABLE nodes ADD COLUMN curation_status TEXT;       -- null, 'soft_deleted', 'flagged', 'merged'
ALTER TABLE nodes ADD COLUMN curation_reason TEXT;       -- human-readable reason
ALTER TABLE nodes ADD COLUMN curation_at TEXT;           -- timestamp of action
ALTER TABLE nodes ADD COLUMN merged_into TEXT;           -- ID of surviving node (if merged)
```

`find_nodes()` and all retrieval queries add:
```sql
WHERE (curation_status IS NULL OR curation_status = 'flagged')
```

### New `curation_log` table

```sql
CREATE TABLE IF NOT EXISTS curation_log (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,        -- 'merge', 'soft_delete', 'flag', 'hard_delete', 'restore', 'unflag'
    node_id TEXT NOT NULL,
    detail TEXT DEFAULT '{}',    -- JSON: merge target, flag reason, etc.
    timestamp TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_curation_log_node ON curation_log(node_id);
CREATE INDEX IF NOT EXISTS idx_curation_log_action ON curation_log(action);
```

This table is append-only (ASK Tenet 2). The curator never deletes log entries.

## Integration Points

### Ingester and Synthesizer

After `RuleIngester.ingest_message()` writes nodes, and after `LLMSynthesizer._apply_extraction()` writes nodes, both call `curator.post_ingestion_check(node_id)`. Direct function call — same process, same DB connection.

### Server Lifecycle

```python
# In create_app():
app.router.add_get("/curation/flags", handle_curation_flags)
app.router.add_post("/curation/restore", handle_curation_restore)
app.router.add_post("/curation/unflag", handle_curation_unflag)
app.router.add_get("/curation/log", handle_curation_log)
app.on_startup.append(_start_curation_loop)
app.on_cleanup.append(_stop_curation_loop)
```

### New Endpoints

**`GET /curation/flags`** — Returns all flagged nodes with reasons. Query params: `kind` (filter by kind), `since` (filter by timestamp).

**`POST /curation/restore`** — Restores a soft-deleted node by ID. Body: `{"node_id": "..."}`. Sets `curation_status` to null. Logs a `restore` action. Returns 404 if node not found or already normal. Returns 410 if past the recovery window and hard-deleted.

**`POST /curation/unflag`** — Clears a flag on a flagged node. Body: `{"node_id": "..."}`. Sets `curation_status` to null. Logs an `unflag` action. Returns 404 if node not found or not flagged.

**`GET /curation/log`** — Paginated curation history. Query params: `node_id` (filter), `action` (filter), `since`, `limit` (default 100), `offset`.

### `/stats` Extension

The existing `/stats` endpoint response gains a `curation` key:

```json
{
  "nodes": 1234,
  "edges": 5678,
  "kinds": {"...": "..."},
  "relations": {"...": "..."},
  "top_connected": ["..."],
  "curation": {
    "orphan_ratio": 0.05,
    "duplicate_density": 0.02,
    "cluster_gini": 0.35,
    "flagged_count": 3,
    "soft_deleted_count": 12,
    "last_cycle": "2026-03-12T10:30:00Z"
  }
}
```

### CLI / MCP

A new `agency admin knowledge` subcommand (and corresponding MCP tool) provides operator access to:
- `agency admin knowledge flags` — list flagged nodes
- `agency admin knowledge restore <node-id>` — restore a soft-deleted or flagged node
- `agency admin knowledge log` — view curation history
- `agency admin knowledge health` — display current health metrics

### Bootstrap Mode

On first startup (no curation history in the log), or when `KNOWLEDGE_CURATOR_MODE=observe` is set, the curator runs in observe-only mode:
- All operations execute and log what they *would* do
- No merges, soft-deletes, or flags are applied
- Log entries are written with `action` prefixed by `observe_` (e.g., `observe_merge`, `observe_soft_delete`)
- Default observe period: 48 hours, configurable via `KNOWLEDGE_CURATOR_OBSERVE_HOURS`
- After the observe period, the curator transitions to active mode automatically. The transition writes a `mode_change` entry to `curation_log` and logs a warning-level message so operators are notified
- Operator can force active mode early: `KNOWLEDGE_CURATOR_MODE=active`

## Configuration

All configuration via environment variables, with sensible defaults:

| Variable | Default | Description |
|---|---|---|
| `KNOWLEDGE_CURATOR_INTERVAL` | `600` | Seconds between curation cycles |
| `KNOWLEDGE_CURATOR_MODE` | `auto` | `auto` (observe then active), `observe`, `active`, `disabled` |
| `KNOWLEDGE_CURATOR_OBSERVE_HOURS` | `48` | Hours to run in observe-only mode |
| `KNOWLEDGE_CURATOR_ORPHAN_AGE_HOURS` | `24` | Hours before orphan nodes are prunable |
| `KNOWLEDGE_CURATOR_RECOVERY_DAYS` | `7` | Days before soft-deleted nodes are hard-deleted |
| `KNOWLEDGE_CURATOR_FUZZY_THRESHOLD` | `0.85` | Auto-merge similarity threshold |
| `KNOWLEDGE_CURATOR_FLAG_THRESHOLD` | `0.70` | Flag-for-review similarity threshold |
| `KNOWLEDGE_CURATOR_BURST_MULTIPLIER` | `3.0` | Contribution rate multiplier for burst detection |

## ASK Compliance

| Tenet | How the curator complies |
|---|---|
| Tenet 1 (Constraints external) | Curator is infrastructure, not an agent. Does not influence enforcement. |
| Tenet 2 (Every action traced) | Append-only `curation_log` table. All mutations logged with action, node ID, detail, timestamp. |
| Tenet 3 (Mediation complete) | Runs inside the knowledge container. No external access. Graph writes go through the same DB the mediation layer manages. |
| Tenet 4 (Least privilege) | Curator operates on graph structure only. Does not read prompt content, agent credentials, or enforcement configuration. |
| Tenet 5 (No blind trust) | Anomaly detection explicitly monitors contribution patterns. Burst and dominance checks flag untrusted contribution behavior. |
| Tenet 7 (History immutable) | Curation log is append-only and never truncated or compacted. Full curation history is reconstructable from the log. |
| Tenet 12 (Synthesis bounds) | Merges are only performed between nodes with overlapping `source_channels`. Nodes from completely disjoint authorization scopes are flagged for operator review, not auto-merged. |
| Tenet 15 (Trust monitored) | Anomaly flags can feed into the trust calibration system — agents whose contributions are frequently pruned or flagged should see trust impact. (Integration point, not curator responsibility.) |
| Tenet 16 (Quarantine is operator-only) | Curator flags anomalies but never quarantines. Quarantine authority remains with operator and security function. |
| Tenet 23 (Knowledge is infrastructure) | Curator treats the graph as durable infrastructure. Soft-delete with recovery, not hard delete. Observe-only bootstrap protects early graph state. |
| Tenet 24 (Knowledge access bounded) | Curator does not bypass ACLs. Retrieval filtering unchanged — curation operates on structural metadata, not authorization scope. |

## Future: Semantic Curation Pass

The curator's interface is designed to support a future semantic curation pass that uses an LLM (local or API) for:
- Recognizing that "nginx vulnerability" and "CVE in nginx reverse proxy" are the same entity
- Evaluating whether a node's summary accurately reflects its connections
- Suggesting new edges between nodes that are semantically related but not yet connected

This would run as an additional operation in the periodic loop, after the heuristic pass. The heuristic pass remains the primary mechanism — the semantic pass is additive. This is a separate spec once local model integration (spec #2) is available.

## Testing

### Unit Tests

- **Near-duplicate detection:** exact match, case variation, whitespace, punctuation differences, below-threshold skips
- **Fuzzy duplicate scan:** above threshold auto-merges, mid-range flags, below threshold skips, empty graph is no-op
- **Orphan pruning:** zero-edge nodes pruned after age threshold, structural kinds exempt, young orphans untouched
- **Cluster concentration:** over-concentrated kind flagged, normal distribution passes, single-kind graph not flagged (too early)
- **Anomaly detection:** burst triggers flag, steady contribution passes, dominance triggers flag
- **Soft-delete lifecycle:** excluded from `find_nodes()`, visible in raw DB, restorable within window, hard-deleted after window
- **Curation log:** all actions logged, log entries never deleted, restore and unflag logged
- **Bootstrap mode:** observe-only logs actions without executing, transitions to active after observe period
- **Merge correctness:** higher source priority wins, properties merged, channels unioned, edges transferred to surviving node

### Integration Tests

- Full curation cycle: ingest nodes → run curation loop → verify merges/prunes/flags
- Post-ingestion hook: add near-duplicate node → verify immediate merge
- Restore flow: soft-delete → `POST /curation/restore` → node back in retrieval
- Flag flow: anomaly detected → node flagged → `GET /curation/flags` returns it → operator reviews
- Stats integration: run curation cycle → verify `/stats` includes curation metrics
- Bootstrap to active transition: observe-only for configured period → verify no mutations → transition → verify mutations execute
