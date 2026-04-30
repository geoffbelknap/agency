# Semantic Caching

**Status:** Draft
**Depends on:** Economics observability (for cache hit metrics)

## Problem

Agents investigate the same types of issues repeatedly. An alert triage agent that investigates "failed SSH login from 203.0.113.42" today may investigate "failed SSH login from 203.0.113.45" tomorrow. Both follow the same procedure, query the same tools, and produce structurally identical reports — but the second run burns the same tokens as the first.

There is no mechanism to recognize that an incoming task is functionally similar to a recently completed one, or to reuse past results when they're still fresh.

## What Already Exists

**Knowledge graph** (`services/knowledge/store.py`):
- SQLite + sqlite-vec + FTS5
- Hybrid search: vector ANN + full-text with reciprocal rank fusion
- Node deduplication by (label, kind) on ingest

**Embedding infrastructure** (`services/knowledge/embedding.py`):
- 4 providers: Ollama (local, default), OpenAI, Voyage, NoOp
- Configurable per deployment
- Used for node embedding on ingest and query embedding on search

**Procedural memory** (`images/body/post_task.py`):
- Captures approach, tools used, outcome, lessons after task completion
- Stored as `procedure` entities in the knowledge graph
- Retrieved and injected into system prompt at next task start
- Already answers "how did I solve something like this before?"

**Episodic memory**:
- Captures narrative summary, notable events, entities after task completion
- Stored as `episode` entities
- Searchable via `recall_episodes` tool

**Intake deduplication** (`images/intake/poller.py`):
- Hash-based change detection for poll sources
- Prevents re-triggering on unchanged data
- Not semantic — exact match only

## Design

### What a semantic cache entry is

A cache entry is a completed task result stored as a new knowledge graph entity type (`cached_result`) with:

- The task's semantic fingerprint (embedding of the task description + trigger context)
- The task's final output (the result delivered to the operator/channel)
- Metadata: agent, mission, timestamp, duration, cost, tools used, outcome
- TTL: configurable freshness window (default: 24 hours)
- Confidence threshold: minimum similarity score for cache hits (default: 0.92)

This builds on the existing knowledge graph infrastructure — no separate vector DB.

### Cache write: after task completion

After a task completes successfully, in `_finalize_task()` (`body.py`):

1. Build cache key text: `"{mission_name}: {task_description} | trigger: {trigger_summary}"`
2. Generate embedding via existing embedding provider
3. Store as `cached_result` node in knowledge graph:

```python
{
    "label": "cache:{agent}:{task_hash_prefix}",
    "kind": "cached_result",
    "summary": task_result_summary,  # The deliverable
    "properties": {
        "task_description": original_task_text,
        "trigger_context": trigger_data_summary,
        "agent": agent_name,
        "mission": mission_name,
        "tools_used": ["tool_a", "tool_b"],
        "outcome": "success",
        "cost_usd": 0.37,
        "duration_s": 45,
        "steps": 5,
        "ttl_hours": 24,
        "created_at": "2026-04-04T14:30:00Z",
        "full_result": full_task_output,  # Complete result text
    }
}
```

The knowledge graph's existing embedding pipeline generates and stores the vector automatically.

### Cache read: before task execution

When a task arrives at the body runtime, before entering the conversation loop:

1. Build the same cache key text from the incoming task
2. Query the knowledge graph for similar `cached_result` nodes:
   - Vector search with the task embedding
   - Filter: `kind=cached_result`, `agent={self.agent_name}`, `outcome=success`
   - Filter: `created_at` within TTL window
3. If the top result exceeds the confidence threshold (similarity >= 0.92):
   - **XPIA scan the cached result** before use (send through enforcer's XPIA scanner via mediation endpoint). A cached result that fails scanning is evicted and treated as a cache miss.
   - Return the cached result directly
   - Emit a `cache_hit` signal (for economics tracking)
   - Log the cache hit in audit (original task + matched cache entry)
   - Skip the conversation loop entirely
4. If no match or below threshold:
   - Proceed normally (cache miss)
   - After completion, write a new cache entry (step above)

### Where cache lookup happens: the body runtime

The cache check runs in the body runtime at the start of `run_task()`, before the conversation loop. This is the right location because:

- The body runtime owns task execution and knows the task context
- Cache hits skip the LLM entirely — no enforcer involvement needed
- Cache misses proceed normally with zero overhead (one vector query)
- The knowledge graph is already accessible via enforcer mediation

### Similarity matching

The knowledge graph's existing hybrid search handles similarity. For cache lookups specifically:

- **Vector-only search** (no FTS) — semantic similarity is what matters, not keyword overlap
- **Distance threshold**: configurable, default 0.92 cosine similarity
- **Scope**: same agent, same mission (optional), within TTL
- **Ranking**: closest match only (top-1)

### Cache invalidation

Entries expire based on TTL. Additional invalidation:

- **Operator can flush**: `agency cache clear --agent {name}` / REST endpoint
- **Mission change invalidates**: when a mission's instructions change (SIGHUP), clear cached results for that mission
- **Failed task clears matching cache**: if a task fails that previously had a cache hit, the cache entry is removed (prevents stale results from persisting)
- **Knowledge graph curation**: operators can flag/remove cached_result nodes like any other entity

### Partial cache hits

Not all cache hits are complete bypasses. For tasks that are *similar but not identical*:

- Similarity 0.92+: full cache hit, return result directly
- Similarity 0.80-0.92: **cache-assisted** — XPIA scan the cached result, then inject as context in the system prompt: "A similar task was completed recently with this result: {cached_result}. Verify and update as needed." If the scan fails, treat as a cache miss and evict the entry.
- Similarity < 0.80: cache miss, no injection

Cache-assisted runs still execute the full loop but start with strong context, reducing steps-to-resolution.

### Ontology extension

Add `cached_result` to the agentic memory ontology (`~/.agency/knowledge/ontology.d/agentic-memory.yaml`):

```yaml
entity_types:
  cached_result:
    description: "Cached task result for semantic deduplication"
    fields:
      - task_description
      - trigger_context
      - agent
      - mission
      - outcome
      - cost_usd
      - duration_s
      - steps
      - tools_used
      - ttl_hours
      - full_result
      - created_at
    relationships:
      - produced_by: procedure  # Link to the procedure that generated it
      - triggered_by: episode   # Link to the episode that captured it
```

Add `cached_result` to the default `KNOWLEDGE_EMBED_KINDS` list so embeddings are generated automatically.

### Knowledge graph access control (ASK Tenet 27)

Cache entries contain investigation results that may exceed other agents' authorization scope. To prevent cross-agent information leakage:

- Cache entries are written with `source_channels` set to the originating agent's private channel only (e.g., `["dm-{agent_name}"]`).
- The knowledge service's existing `visible_channels` filtering ensures other agents cannot discover cache entries through `query_knowledge`, `who_knows_about`, or other knowledge graph queries.
- `cached_result` is added to a set of **agent-private entity kinds** that are excluded from cross-agent knowledge search results, even if channel filtering is not applied (defense in depth).
- Operators can query all cache entries via the gateway API (they are not hidden from governance).

### Configuration

In mission config or platform defaults:

```yaml
cache:
  enabled: true                    # Can be disabled per-agent or per-mission
  ttl_hours: 24                    # How long cached results stay valid
  confidence_threshold: 0.92       # Minimum similarity for full cache hit
  assist_threshold: 0.80           # Minimum similarity for cache-assisted run
  max_entries_per_mission: 100     # Prevent unbounded growth
  scope: mission                   # "mission" (same mission only) or "agent" (any mission)
```

### Signals and economics

**New signal**: `agent_signal_cache_hit` emitted when a cache hit skips execution:

```json
{
  "type": "agent_signal_cache_hit",
  "agent": "alert-triage",
  "data": {
    "task_id": "...",
    "cache_entry_id": "...",
    "similarity": 0.96,
    "hit_type": "full",
    "saved_cost_estimate_usd": 0.37,
    "saved_steps_estimate": 5
  }
}
```

**Economics observability** integration:
- Cache hit rate: percentage of tasks served from cache
- Estimated cost saved: sum of `saved_cost_estimate_usd` across cache hits
- Cache-assisted vs. full hit breakdown

### Meeseeks caching

Meeseeks are ephemeral and short-lived, but they often do repetitive subtasks. Meeseeks cache entries are scoped to the parent agent's mission and have a shorter default TTL (1 hour). The parent agent's cache is checked, not the meeseeks' own (since meeseeks don't persist).

**Scope enforcement (ASK Tenet 19 — delegation cannot exceed delegator scope):** Meeseeks are spawned with a subset of the parent's tools. Cache lookups for meeseeks must filter by `tools_used` — only return entries where every tool used in the cached workflow is within the meeseeks' delegated tool set. A cache entry produced using tools the meeseeks isn't authorized to use must not be returned, even if the task description is semantically identical.

## What This Does NOT Include

- **Cross-agent caching** — cache entries are scoped per-agent (or per-mission). Cross-agent result sharing is a coordination problem, not a caching problem.
- **Deterministic tool output caching** — caching raw tool outputs (e.g., "cache the API response from this endpoint") is a different layer. This spec caches *completed task results*, not intermediate tool calls.
- **External vector database** — builds on the existing knowledge graph. No Qdrant/Pinecone/etc.

## Sequencing

1. Add `cached_result` entity type to agentic memory ontology
2. Add `cached_result` to default `KNOWLEDGE_EMBED_KINDS`
3. Implement cache write in `_finalize_task()` after successful completion
4. Implement cache read in `run_task()` before conversation loop
5. Add partial cache hit (cache-assisted) injection
6. Add `cache_hit` signal and audit logging
7. Add cache invalidation (TTL expiry, mission change, manual flush)
8. Add configuration support (thresholds, TTL, enable/disable)
9. Add cache metrics to economics observability

Steps 1-4 are the core. Steps 5-9 are enhancements.
