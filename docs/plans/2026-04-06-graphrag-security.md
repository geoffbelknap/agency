# GraphRAG Security — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the GraphRAG XPIA attack surface — tag and scan graph-injected briefings in the enforcer, add operator-initiated provenance-based quarantine with per-agent/time-window/node granularity.

**Architecture:** Body runtime wraps GraphRAG content in delimiters. Enforcer's existing XPIA scanner enhanced to identify and scan delimited sections. New `quarantined` curation status in KnowledgeStore with edge exclusion. CLI + server endpoints for quarantine operations. Gateway proxy + Go CLI.

**Tech Stack:** Python (body runtime, knowledge service), Go (enforcer, gateway, CLI)

**Spec:** `docs/specs/graphrag-security.md`

---

## File Structure

### Files to Create

| File | Purpose |
|------|---------|
| `images/tests/test_quarantine.py` | Tests for quarantine mechanism |

### Files to Modify

| File | Changes |
|------|---------|
| `images/body/knowledge_tools.py` | Wrap GraphRAG content in delimiters with node ID metadata |
| `images/enforcer/xpia.go` | Detect delimiters, enhanced scanning, content stripping, security event |
| `images/knowledge/store.py` | Quarantine/release methods, edge exclusion for quarantined nodes |
| `images/knowledge/server.py` | Quarantine endpoints: POST/GET /quarantine, POST /quarantine/release |
| `internal/knowledge/proxy.go` | Quarantine proxy methods |
| `internal/api/routes.go` | Quarantine routes |
| `internal/api/handlers_hub.go` | Quarantine handlers |
| `internal/cli/commands.go` | quarantine/quarantine-release/quarantine-list CLI commands |

---

## Task 1: Quarantine Store Methods

**Files:**
- Modify: `images/knowledge/store.py`
- Test: `images/tests/test_quarantine.py`

- [ ] **Step 1: Write failing tests**

```python
# images/tests/test_quarantine.py
"""Tests for provenance-based quarantine."""
import json
import os
import sys
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from store import KnowledgeStore


class TestQuarantineByAgent:
    def test_quarantine_marks_agent_nodes(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n1 = store.add_node("finding-1", "finding", "bad finding",
                            properties={"contributed_by": "bad-agent"})
        n2 = store.add_node("finding-2", "finding", "good finding",
                            properties={"contributed_by": "good-agent"})
        store._db.commit()

        stats = store.quarantine_by_agent("bad-agent")
        assert stats["quarantined"] == 1

        row = store._db.execute("SELECT curation_status FROM nodes WHERE id = ?", (n1,)).fetchone()
        assert row[0] == "quarantined"
        row2 = store._db.execute("SELECT curation_status FROM nodes WHERE id = ?", (n2,)).fetchone()
        assert row2[0] is None

    def test_quarantined_nodes_excluded_from_find_nodes(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node("secret-finding", "finding", "compromised data",
                       properties={"contributed_by": "bad-agent"})
        store.add_node("safe-finding", "finding", "legitimate data",
                       properties={"contributed_by": "good-agent"})
        store._db.commit()

        store.quarantine_by_agent("bad-agent")
        results = store.find_nodes("finding")
        labels = [r["label"] for r in results]
        assert "secret-finding" not in labels
        assert "safe-finding" in labels


class TestQuarantineByAgentWithTimeWindow:
    def test_quarantine_with_since(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        # Create nodes at different times
        old = store.add_node("old-finding", "finding", "before compromise",
                            properties={"contributed_by": "agent-x"})
        # Manually set created_at to the past
        store._db.execute("UPDATE nodes SET created_at = '2026-01-01T00:00:00Z' WHERE id = ?", (old,))
        new = store.add_node("new-finding", "finding", "after compromise",
                            properties={"contributed_by": "agent-x"})
        store._db.commit()

        stats = store.quarantine_by_agent("agent-x", since="2026-04-01T00:00:00Z")
        assert stats["quarantined"] == 1

        # Old node should NOT be quarantined
        row = store._db.execute("SELECT curation_status FROM nodes WHERE id = ?", (old,)).fetchone()
        assert row[0] is None
        # New node should be quarantined
        row2 = store._db.execute("SELECT curation_status FROM nodes WHERE id = ?", (new,)).fetchone()
        assert row2[0] == "quarantined"


class TestQuarantineRelease:
    def test_release_individual_node(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n = store.add_node("quarantined-node", "finding", "was bad",
                          properties={"contributed_by": "agent"})
        store._db.commit()
        store.quarantine_by_agent("agent")

        store.quarantine_release_node(n)
        row = store._db.execute("SELECT curation_status FROM nodes WHERE id = ?", (n,)).fetchone()
        assert row[0] is None

    def test_release_by_agent(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node("node-1", "finding", "a", properties={"contributed_by": "agent"})
        store.add_node("node-2", "finding", "b", properties={"contributed_by": "agent"})
        store._db.commit()
        store.quarantine_by_agent("agent")

        stats = store.quarantine_release_agent("agent")
        assert stats["released"] == 2


class TestQuarantineEdgeExclusion:
    def test_edges_touching_quarantined_nodes_excluded(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        bad = store.add_node("bad-node", "finding", "compromised",
                            properties={"contributed_by": "bad-agent"})
        good = store.add_node("good-node", "fact", "legitimate")
        store.add_edge(bad, good, "relates_to", provenance="INFERRED")
        store._db.commit()

        store.quarantine_by_agent("bad-agent")

        edges = store.get_edges(good, direction="both")
        # Edge touching quarantined node should be excluded
        assert len(edges) == 0


class TestQuarantineList:
    def test_list_quarantined(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node("q1", "finding", "a", properties={"contributed_by": "agent"})
        store.add_node("q2", "finding", "b", properties={"contributed_by": "agent"})
        store._db.commit()
        store.quarantine_by_agent("agent")

        quarantined = store.list_quarantined()
        assert len(quarantined) == 2

    def test_list_quarantined_by_agent(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node("a1", "finding", "x", properties={"contributed_by": "agent-a"})
        store.add_node("b1", "finding", "y", properties={"contributed_by": "agent-b"})
        store._db.commit()
        store.quarantine_by_agent("agent-a")
        store.quarantine_by_agent("agent-b")

        result = store.list_quarantined(agent="agent-a")
        assert len(result) == 1


class TestQuarantineCurationLog:
    def test_quarantine_logged(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node("logged-node", "finding", "x", properties={"contributed_by": "agent"})
        store._db.commit()
        store.quarantine_by_agent("agent")

        logs = store.get_curation_log(action="quarantine")
        assert len(logs) >= 1

    def test_release_logged(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        n = store.add_node("logged-node", "finding", "x", properties={"contributed_by": "agent"})
        store._db.commit()
        store.quarantine_by_agent("agent")
        store.quarantine_release_node(n)

        logs = store.get_curation_log(action="quarantine_release")
        assert len(logs) >= 1
```

- [ ] **Step 2: Implement quarantine methods on KnowledgeStore**

Add to `images/knowledge/store.py`:

```python
def quarantine_by_agent(self, agent_name, since=None):
    """Quarantine all nodes contributed by an agent."""
    query = """
        UPDATE nodes SET curation_status = 'quarantined', curation_reason = ?,
        curation_at = ?
        WHERE json_extract(properties, '$.contributed_by') = ?
        AND (curation_status IS NULL OR curation_status = 'flagged')
    """
    params = [f"Quarantined: agent {agent_name}", now_iso(), agent_name]
    if since:
        query += " AND created_at >= ?"
        params.append(since)

    cursor = self._db.execute(query, params)
    count = cursor.rowcount
    self._db.commit()

    self.log_curation("quarantine", "__agent__",
                      {"agent": agent_name, "count": count, "since": since})
    return {"quarantined": count}

def quarantine_release_node(self, node_id):
    """Release a single node from quarantine."""
    self._db.execute(
        "UPDATE nodes SET curation_status = NULL, curation_reason = NULL, curation_at = NULL WHERE id = ?",
        (node_id,)
    )
    self._db.commit()
    self.log_curation("quarantine_release", node_id, {"type": "node"})

def quarantine_release_agent(self, agent_name):
    """Release all quarantined nodes for an agent."""
    cursor = self._db.execute(
        """UPDATE nodes SET curation_status = NULL, curation_reason = NULL, curation_at = NULL
           WHERE curation_status = 'quarantined'
           AND json_extract(properties, '$.contributed_by') = ?""",
        (agent_name,)
    )
    count = cursor.rowcount
    self._db.commit()
    self.log_curation("quarantine_release", "__agent__",
                      {"agent": agent_name, "count": count})
    return {"released": count}

def list_quarantined(self, agent=None):
    """List quarantined nodes."""
    query = "SELECT id, label, kind, summary, properties, curation_reason, curation_at FROM nodes WHERE curation_status = 'quarantined'"
    params = []
    if agent:
        query += " AND json_extract(properties, '$.contributed_by') = ?"
        params.append(agent)
    query += " ORDER BY curation_at DESC"
    rows = self._db.execute(query, params).fetchall()
    return [{"id": r[0], "label": r[1], "kind": r[2], "summary": r[3],
             "properties": r[4], "reason": r[5], "quarantined_at": r[6]} for r in rows]
```

Also update `get_edges()` to exclude edges touching quarantined nodes:

```python
# In get_edges(), add to the WHERE clause:
AND source_id NOT IN (SELECT id FROM nodes WHERE curation_status = 'quarantined')
AND target_id NOT IN (SELECT id FROM nodes WHERE curation_status = 'quarantined')
```

- [ ] **Step 3: Run tests, commit**

---

## Task 2: GraphRAG Content Tagging in Body Runtime

**Files:**
- Modify: `images/body/knowledge_tools.py`

- [ ] **Step 1: Find and update the GraphRAG retrieval path**

Read `images/body/knowledge_tools.py` to find where knowledge context is retrieved and assembled into the system prompt. Look for `_retrieve_knowledge_context` or similar.

Wrap the retrieved content in delimiters:

```python
GRAPHRAG_START = "[KNOWLEDGE_GRAPH_CONTEXT]"
GRAPHRAG_END = "[/KNOWLEDGE_GRAPH_CONTEXT]"

# In the knowledge retrieval function, after getting results:
if results:
    node_ids = [n.get("id", "") for n in results]
    tagged_content = f"{GRAPHRAG_START}\n"
    tagged_content += f"<!-- node_ids: {','.join(node_ids)} -->\n"
    tagged_content += formatted_knowledge
    tagged_content += f"\n{GRAPHRAG_END}"
    return tagged_content
```

- [ ] **Step 2: Verify the file parses, commit**

---

## Task 3: Quarantine Server Endpoints

**Files:**
- Modify: `images/knowledge/server.py`

- [ ] **Step 1: Add handlers**

```python
async def handle_quarantine(request):
    """POST /quarantine — quarantine nodes by agent."""
    store = request.app["store"]
    body = await request.json()
    agent = body.get("agent", "")
    since = body.get("since")
    if not agent:
        return web.json_response({"error": "agent required"}, status=400)
    stats = store.quarantine_by_agent(agent, since=since)
    return web.json_response(stats)

async def handle_quarantine_release(request):
    """POST /quarantine/release — release nodes."""
    store = request.app["store"]
    body = await request.json()
    node_id = body.get("node_id")
    agent = body.get("agent")
    if node_id:
        store.quarantine_release_node(node_id)
        return web.json_response({"released": 1, "node_id": node_id})
    elif agent:
        stats = store.quarantine_release_agent(agent)
        return web.json_response(stats)
    return web.json_response({"error": "node_id or agent required"}, status=400)

async def handle_quarantine_list(request):
    """GET /quarantine — list quarantined nodes."""
    store = request.app["store"]
    agent = request.query.get("agent")
    nodes = store.list_quarantined(agent=agent)
    return web.json_response({"quarantined": nodes})
```

Register routes:
```python
app.router.add_post("/quarantine", handle_quarantine)
app.router.add_post("/quarantine/release", handle_quarantine_release)
app.router.add_get("/quarantine", handle_quarantine_list)
```

- [ ] **Step 2: Verify imports, commit**

---

## Task 4: Go Gateway Proxy + CLI

**Files:**
- Modify: `internal/knowledge/proxy.go`
- Modify: `internal/api/routes.go`
- Modify: `internal/api/handlers_hub.go`
- Modify: `internal/cli/commands.go`

- [ ] **Step 1: Add proxy methods**

```go
func (p *Proxy) Quarantine(ctx context.Context, agent, since string) (json.RawMessage, error)
func (p *Proxy) QuarantineRelease(ctx context.Context, nodeID, agent string) (json.RawMessage, error)
func (p *Proxy) QuarantineList(ctx context.Context, agent string) (json.RawMessage, error)
```

- [ ] **Step 2: Add routes and handlers**

```go
r.Post("/knowledge/quarantine", h.knowledgeQuarantine)
r.Post("/knowledge/quarantine/release", h.knowledgeQuarantineRelease)
r.Get("/knowledge/quarantine", h.knowledgeQuarantineList)
```

- [ ] **Step 3: Add CLI commands**

```
agency admin knowledge quarantine --agent <name> [--since timestamp]
agency admin knowledge quarantine-release --node <id>
agency admin knowledge quarantine-release --agent <name>
agency admin knowledge quarantine-list [--agent <name>]
```

Add as subcommands under the existing `adminCmd` → `knowledge` group.

- [ ] **Step 4: Build, verify, commit**

---

## Task 5: Full Validation

- [ ] **Step 1: Run quarantine tests**

```bash
python3 -m pytest images/tests/test_quarantine.py -v
```

- [ ] **Step 2: Run all previous tests**

```bash
python3 -m pytest images/tests/test_edge_provenance.py images/tests/test_scope_model.py images/tests/test_save_insight.py images/tests/test_principal_registry.py -q
```

- [ ] **Step 3: Build Go**

```bash
go build ./cmd/gateway/
```

- [ ] **Step 4: Commit and push**

---

## Summary

| Task | What it delivers |
|------|-----------------|
| **Task 1** | Quarantine store methods: by-agent, by-agent+time, release, list, edge exclusion |
| **Task 2** | Body runtime GraphRAG content tagging with delimiters and node IDs |
| **Task 3** | Knowledge server quarantine endpoints |
| **Task 4** | Go gateway proxy, routes, handlers, CLI commands |
| **Task 5** | Full validation |

**Note:** Enforcer XPIA scanner enhancement (enhanced pattern matching within delimiters) is documented in the spec but deferred to implementation alongside the Go enforcer team — the base XPIA scan already covers all content. The delimiter tagging and quarantine mechanism are the immediate deliverables.
