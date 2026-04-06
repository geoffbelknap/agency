---
description: "XPIA scanning of GraphRAG-injected briefings, provenance-based quarantine, and attack surface documentation."
status: "Draft"
---

# GraphRAG Security

*Close the GraphRAG XPIA attack surface — scan graph-injected content before it reaches agents, quarantine compromised subgraphs on demand.*

**Date:** 2026-04-06
**Status:** Draft

---

## Overview

GraphRAG automatically injects knowledge graph content into agent context as briefing blocks. This is a cross-agent prompt injection vector: a compromised agent that poisons the knowledge graph can influence any agent that queries related topics. Before this spec, GraphRAG-retrieved content entered the LLM context without XPIA scanning — the primary undefended propagation path in the platform.

This spec closes the gap with two mechanisms: enforcer-side scanning of GraphRAG content (runtime defense) and operator-initiated provenance-based quarantine (incident response).

---

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Scanning location | Enforcer (existing XPIA scanner) | Scanning in the enforcer is ASK Tenet 1 compliant (enforcement separate from agent). No new scanning infrastructure. |
| Content tagging | Body runtime wraps GraphRAG content in delimiters | Tags signal enhanced scrutiny. Base scan covers all content regardless of tags (defense-in-depth, not gating). |
| Quarantine trigger | Operator-initiated only | Automatic quarantine risks false positives with current anomaly detection maturity. Operator decides after alert. |
| Quarantine granularity | Per-agent, per-agent+time-window, per-node release | Broad containment first, surgical restoration after review. |
| Edge quarantine | Quarantine edges where either endpoint is quarantined | Prevents traversal into quarantine zone from any direction. |

---

## XPIA Scanning of GraphRAG Content

### How It Works

1. **Body runtime** retrieves knowledge context via `_retrieve_knowledge_context()` (existing)
2. **Body runtime** wraps the retrieved content in tagged delimiters before including in system prompt:
   ```
   [KNOWLEDGE_GRAPH_CONTEXT]
   {retrieved nodes, edges, briefing content}
   [/KNOWLEDGE_GRAPH_CONTEXT]
   ```
3. **System prompt** is assembled and sent through the enforcer's LLM proxy (existing path)
4. **Enforcer** scans the full message content for XPIA patterns (existing scanner)
5. **Enforcer** additionally flags the `[KNOWLEDGE_GRAPH_CONTEXT]` section for enhanced scrutiny — applies injection detection patterns specifically tuned for graph-sourced content
6. If XPIA detected:
   - Flagged content is stripped from the context
   - Security event emitted: `{event: "graphrag_xpia_detected", agent: name, node_ids: [...], pattern: "..."}`
   - Event routed to operator notifications via existing event bus
   - The LLM call proceeds with the sanitized context

### What Tags Are (and Aren't)

Tags are **defense-in-depth**, not a gating mechanism. The enforcer's base XPIA scan covers ALL content entering the LLM context regardless of tags. The tags provide:
- A signal for enhanced scanning patterns specific to graph-injected content
- Identification of which content came from the graph (for logging and forensics)
- A boundary the enforcer can use to strip flagged content surgically

A compromised body that strips tags does NOT bypass scanning — the base scan still runs on the full message. Tags add a layer; they don't replace the base.

### Enforcer Changes

The enforcer's XPIA scanner (`images/enforcer/xpia.go` or equivalent) gains:
- A pattern matcher for `[KNOWLEDGE_GRAPH_CONTEXT]` delimiters
- Enhanced scanning within delimited sections (higher sensitivity for instruction-like patterns)
- A stripping function that removes flagged content between delimiters while preserving the rest of the system prompt
- Security event emission with node IDs extracted from the flagged content

### Body Runtime Changes

In `images/body/knowledge_tools.py` (or wherever `_retrieve_knowledge_context()` assembles the briefing):
- Wrap retrieved content in `[KNOWLEDGE_GRAPH_CONTEXT]` / `[/KNOWLEDGE_GRAPH_CONTEXT]` delimiters
- Include node IDs as metadata comments within the delimited section so the enforcer can trace flagged content back to source nodes

---

## Provenance-Based Quarantine

### Quarantine Mechanism

New `curation_status` value: `"quarantined"`. Existing retrieval exclusion logic already filters out nodes where `curation_status` is not null (except `"flagged"`), so quarantined nodes are automatically excluded from:
- `find_nodes()` (FTS + vector search)
- `get_subgraph()` (BFS traversal)
- `get_neighbors()` / `get_neighbors_subgraph()`
- `find_path()` (BFS path finding)
- GraphRAG briefing retrieval
- Community detection and hub analysis

### Edge Quarantine

When nodes are quarantined, edges where either endpoint is a quarantined node are also excluded from traversal. This prevents reaching quarantined content via edges that weren't directly contributed by the compromised agent (e.g., edges created by the curator's relationship inference or LLM synthesis).

Implementation: `get_edges()` adds a subquery check:
```sql
AND source_id NOT IN (SELECT id FROM nodes WHERE curation_status = 'quarantined')
AND target_id NOT IN (SELECT id FROM nodes WHERE curation_status = 'quarantined')
```

### CLI Commands

**Quarantine by agent:**
```
agency admin knowledge quarantine --agent <name>
```
Sets `curation_status = 'quarantined'` on all nodes where `properties->>'contributed_by' = <name>`. Logs action in `curation_log` with details: agent name, node count, timestamp.

**Quarantine by agent + time window:**
```
agency admin knowledge quarantine --agent <name> --since 2026-04-06T12:00:00Z
```
Same but only nodes with `created_at >= <timestamp>`.

**Release individual node:**
```
agency admin knowledge quarantine-release --node <id>
```
Clears `curation_status` back to null. Logs release in `curation_log`.

**Release all for agent:**
```
agency admin knowledge quarantine-release --agent <name>
```
Clears quarantine on all nodes originally quarantined for this agent.

**List quarantined nodes:**
```
agency admin knowledge quarantine-list [--agent <name>]
```
Shows quarantined nodes with their provenance metadata.

### Server Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/quarantine` | Quarantine nodes by agent (+ optional since timestamp) |
| `POST` | `/quarantine/release` | Release nodes (by node ID or agent) |
| `GET` | `/quarantine` | List quarantined nodes |

### Operator Notification

When the curator's anomaly detection (burst/dominance) flags suspicious contribution patterns, it emits an operator alert via the existing notification system. The alert includes the agent name and recent contribution count, prompting the operator to investigate and potentially quarantine.

This is not automatic quarantine — the operator reviews the alert and decides. The latency gap between detection and quarantine is accepted for this spec to avoid false positive risk.

---

## Attack Surface Documentation

### GraphRAG Injection Kill Chain

```
1. INJECTION    Compromised agent → contribute_knowledge() or save_insight()
                → malicious content enters knowledge graph as a node

2. STORAGE      Malicious node stored with provenance metadata
                → curator may flag via anomaly detection (not guaranteed)

3. RETRIEVAL    Target agent calls query_knowledge() or starts a task
                → GraphRAG retrieves the malicious node as part of briefing

4. PROPAGATION  Malicious content injected into target agent's LLM context
                → enforcer XPIA scanner intercepts (THIS SPEC)

5. EXECUTION    If injection passes scanning, target agent may follow
                injected instructions within its authorized scope

6. EXFILTRATION If execution succeeds, egress proxy limits what data
                can leave the agent's network boundary
```

### Defenses at Each Stage

| Stage | Defense | Status |
|---|---|---|
| Injection | Provenance tracking on all contributions (Phase 1) | Implemented |
| Injection | Scope enforcement — agents can only contribute within their scope (Phase 1b) | Implemented |
| Storage | Curator anomaly detection — burst/dominance flagging | Implemented |
| Storage | Operator quarantine of compromised agent's contributions | **This spec** |
| Retrieval | Scope enforcement — agents can only query within their scope (Phase 1b) | Implemented |
| Propagation | Enforcer XPIA scanning of GraphRAG content | **This spec** |
| Execution | Enforcer tool/command gating — agent scope limits blast radius | Existing |
| Execution | Permission middleware — agent can only call APIs within its permissions (ACL spec) | Implemented |
| Exfiltration | Egress proxy — all outbound traffic mediated | Existing |

### Residual Risk

Even with all defenses, a sophisticated injection that:
1. Passes XPIA pattern matching (novel pattern not in scanner)
2. Stays within the target agent's authorized scope
3. Produces output that looks legitimate

...could succeed. This is the same residual risk as any prompt injection — defense-in-depth reduces probability, scope enforcement limits impact, audit trails enable detection and response.

---

## ASK Compliance

| Tenet | How this spec complies |
|---|---|
| Tenet 1 (Enforcement separate) | XPIA scanning in the enforcer (separate container), not in the body runtime. |
| Tenet 2 (Audit trail) | Flagged content logged with node IDs. Quarantine actions in curation_log. Security events via event bus. |
| Tenet 13 (Guardrails before agent) | **Gap closed.** GraphRAG content now scanned before reaching agent's LLM context. |
| Tenet 14 (Kill chain interrupted) | Propagation stage defended (enforcer scanning). Containment via quarantine. |
| Tenet 15 (Untrusted content restricted) | GraphRAG content tagged as untrusted, scanned with enhanced scrutiny. |
| Tenet 17 (Human review mechanism) | Operator quarantine CLI with granular control. |

---

## Implementation Phases

### Phase 1: GraphRAG Content Tagging

- Body runtime wraps knowledge context in delimiters
- Node IDs included as metadata within delimited section
- Existing XPIA scanner continues to scan full content (no change needed for base coverage)

### Phase 2: Enhanced Enforcer Scanning

- Enforcer identifies `[KNOWLEDGE_GRAPH_CONTEXT]` sections
- Enhanced pattern matching within delimited sections
- Content stripping on XPIA detection
- Security event emission with node IDs

### Phase 3: Quarantine Mechanism

- `curation_status = 'quarantined'` support in KnowledgeStore
- Edge quarantine (exclude edges touching quarantined nodes)
- Server endpoints: POST/GET /quarantine, POST /quarantine/release
- CLI: quarantine, quarantine-release, quarantine-list

### Phase 4: Gateway Integration

- Go proxy methods for quarantine endpoints
- CLI commands wired to gateway API
- Anomaly detection alerts linked to operator notification system

---

## Testing

### Phase 1 Tests
- Body runtime includes delimiters around GraphRAG content
- Node IDs present in delimited section
- Delimiters don't break system prompt assembly

### Phase 2 Tests
- Enforcer detects XPIA in delimited content
- Flagged content stripped, rest of prompt preserved
- Security event emitted with correct node IDs
- Base scan still runs on non-delimited content (tags are additive)

### Phase 3 Tests
- Quarantine by agent marks all nodes
- Quarantine by agent+time-window limits scope
- Quarantined nodes excluded from find_nodes()
- Quarantined nodes excluded from get_subgraph() traversal
- Edges touching quarantined nodes excluded
- Release individual node restores to retrieval
- Release by agent restores all
- Curation log records quarantine and release actions

### Phase 4 Tests
- Gateway proxy forwards quarantine requests
- CLI quarantine/release/list commands work
- Anomaly detection alert includes agent name
