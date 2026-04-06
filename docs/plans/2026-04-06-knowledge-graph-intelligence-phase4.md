# Knowledge Graph Intelligence — Phase 4: Query Feedback Loop

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the `save_insight` tool so agents can persist synthesized conclusions back into the knowledge graph, creating a compounding feedback loop.

**Architecture:** New `save_insight` body runtime tool → `POST /insight` knowledge server endpoint → creates `finding` node + `DERIVED_FROM` edges with INFERRED provenance. Scope is the intersection of source node scopes (ASK Tenet 12). Go gateway proxy + CLI for operator access.

**Tech Stack:** Python (knowledge service, body runtime), Go (gateway/CLI)

**Spec:** `docs/specs/knowledge-graph-intelligence.md` — Phase 4 section

**Depends on:** Phase 1 (provenance, scope) — completed

---

## File Structure

### Files to Create

| File | Purpose |
|------|---------|
| `images/tests/test_save_insight.py` | Tests for insight creation, scope intersection, validation, dedup |

### Files to Modify

| File | Changes |
|------|---------|
| `images/knowledge/server.py` | Add `POST /insight` endpoint |
| `images/knowledge/store.py` | Add `save_insight()` method with scope intersection and validation |
| `images/body/knowledge_tools.py` | Add `save_insight` tool registration |
| `internal/knowledge/proxy.go` | Add SaveInsight proxy method |
| `internal/api/routes.go` | Add insight route |
| `internal/api/handlers_hub.go` | Add insight handler |
| `internal/apiclient/client.go` | Add KnowledgeSaveInsight client method |
| `internal/cli/commands.go` | Add `agency knowledge insight` subcommand |

---

## Task 1: save_insight() Store Method

**Files:**
- Modify: `images/knowledge/store.py`
- Test: `images/tests/test_save_insight.py`

- [ ] **Step 1: Write failing tests**

```python
# images/tests/test_save_insight.py
"""Tests for the save_insight feedback loop."""
import json
import os
import sys
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from store import KnowledgeStore


class TestSaveInsight:
    @pytest.fixture
    def store(self, tmp_path):
        return KnowledgeStore(tmp_path)

    @pytest.fixture
    def populated_store(self, store):
        """Store with source nodes for insights."""
        store.add_node("nginx 1.24", "software", "Web server",
                       scope={"channels": ["security"], "principals": ["agent:sec-1"]})
        store.add_node("CVE-2023-44487", "vulnerability", "HTTP/2 rapid reset",
                       scope={"channels": ["security"], "principals": ["agent:sec-1"]})
        store.add_node("prod-web", "system", "Production web server",
                       scope={"channels": ["infra"], "principals": ["agent:sec-1"]})
        store._db.commit()
        return store

    def test_creates_finding_node(self, populated_store):
        src_ids = [r["id"] for r in populated_store.find_nodes("nginx")]
        result = populated_store.save_insight(
            insight="prod-web is exposed via nginx 1.24",
            source_node_ids=src_ids,
            confidence="high",
            tags=["risk"],
            agent_name="security-agent",
        )
        assert result["node_id"] is not None
        node = populated_store.get_node(result["node_id"])
        assert node["kind"] == "finding"
        assert "exposed" in node["summary"]

    def test_creates_derived_from_edges(self, populated_store):
        src_ids = [r["id"] for r in populated_store.find_nodes("nginx")]
        src_ids += [r["id"] for r in populated_store.find_nodes("CVE")]
        result = populated_store.save_insight(
            insight="nginx is vulnerable to rapid reset",
            source_node_ids=src_ids,
            confidence="high",
            agent_name="sec-agent",
        )
        edges = populated_store.get_edges(result["node_id"], direction="outgoing")
        derived = [e for e in edges if e["relation"] == "DERIVED_FROM"]
        assert len(derived) == len(src_ids)

    def test_edges_have_inferred_provenance(self, populated_store):
        src_ids = [r["id"] for r in populated_store.find_nodes("nginx")]
        result = populated_store.save_insight(
            insight="test insight",
            source_node_ids=src_ids,
            confidence="medium",
            agent_name="agent",
        )
        edges = populated_store.get_edges(result["node_id"], direction="outgoing")
        for edge in edges:
            if edge["relation"] == "DERIVED_FROM":
                assert edge["provenance"] == "INFERRED"

    def test_scope_is_intersection_of_sources(self, populated_store):
        # nginx has channels=["security"], prod-web has channels=["infra"]
        nginx_ids = [r["id"] for r in populated_store.find_nodes("nginx")]
        prodweb_ids = [r["id"] for r in populated_store.find_nodes("prod-web")]
        result = populated_store.save_insight(
            insight="cross-scope insight",
            source_node_ids=nginx_ids + prodweb_ids,
            confidence="high",
            agent_name="agent",
        )
        node = populated_store.get_node(result["node_id"])
        scope = json.loads(node.get("scope", "{}"))
        # Intersection: channels overlap is empty (security ∩ infra = [])
        # But principals overlap: agent:sec-1 is in both
        assert "agent:sec-1" in scope.get("principals", [])

    def test_confidence_stored_in_properties(self, populated_store):
        src_ids = [r["id"] for r in populated_store.find_nodes("nginx")]
        result = populated_store.save_insight(
            insight="test",
            source_node_ids=src_ids,
            confidence="high",
            agent_name="agent",
        )
        node = populated_store.get_node(result["node_id"])
        props = json.loads(node.get("properties", "{}"))
        assert props.get("confidence") == "high"

    def test_tags_stored_in_properties(self, populated_store):
        src_ids = [r["id"] for r in populated_store.find_nodes("nginx")]
        result = populated_store.save_insight(
            insight="tagged insight",
            source_node_ids=src_ids,
            confidence="medium",
            tags=["risk", "internet-facing"],
            agent_name="agent",
        )
        node = populated_store.get_node(result["node_id"])
        props = json.loads(node.get("properties", "{}"))
        assert "risk" in props.get("tags", [])

    def test_agent_name_in_properties(self, populated_store):
        src_ids = [r["id"] for r in populated_store.find_nodes("nginx")]
        result = populated_store.save_insight(
            insight="test",
            source_node_ids=src_ids,
            confidence="low",
            agent_name="security-auditor",
        )
        node = populated_store.get_node(result["node_id"])
        props = json.loads(node.get("properties", "{}"))
        assert props.get("contributed_by") == "security-auditor"

    def test_rejects_nonexistent_source_nodes(self, store):
        with pytest.raises(ValueError, match="not found"):
            store.save_insight(
                insight="invalid",
                source_node_ids=["nonexistent-id"],
                confidence="high",
                agent_name="agent",
            )

    def test_empty_source_nodes_rejected(self, store):
        with pytest.raises(ValueError, match="source"):
            store.save_insight(
                insight="no sources",
                source_node_ids=[],
                confidence="high",
                agent_name="agent",
            )

    def test_invalid_confidence_rejected(self, populated_store):
        src_ids = [r["id"] for r in populated_store.find_nodes("nginx")]
        with pytest.raises(ValueError, match="confidence"):
            populated_store.save_insight(
                insight="test",
                source_node_ids=src_ids,
                confidence="very_high",
                agent_name="agent",
            )

    def test_insight_chaining(self, populated_store):
        """An insight can reference a previous insight as a source node."""
        src_ids = [r["id"] for r in populated_store.find_nodes("nginx")]
        first = populated_store.save_insight(
            insight="nginx is outdated",
            source_node_ids=src_ids,
            confidence="high",
            agent_name="agent",
        )
        # Chain: second insight references the first
        second = populated_store.save_insight(
            insight="outdated nginx creates risk for prod-web",
            source_node_ids=[first["node_id"]],
            confidence="medium",
            agent_name="agent",
        )
        assert second["node_id"] != first["node_id"]
        edges = populated_store.get_edges(second["node_id"], direction="outgoing")
        derived = [e for e in edges if e["relation"] == "DERIVED_FROM"]
        assert any(e["target_id"] == first["node_id"] for e in derived)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/geoff/agency-workspace/agency && python3 -m pytest images/tests/test_save_insight.py -v`

- [ ] **Step 3: Implement save_insight() on KnowledgeStore**

Add to `images/knowledge/store.py`:

```python
def save_insight(self, insight, source_node_ids, confidence, tags=None, agent_name=""):
    """Save an agent's synthesized insight back into the graph.

    Creates a finding node linked to source nodes via DERIVED_FROM edges.
    Scope is the intersection of source node scopes (ASK Tenet 12).
    """
    # Validate
    valid_confidence = ("high", "medium", "low")
    if confidence not in valid_confidence:
        raise ValueError(f"confidence must be one of {valid_confidence}, got '{confidence}'")
    if not source_node_ids:
        raise ValueError("source_node_ids must be non-empty")

    # Validate all source nodes exist
    source_nodes = []
    for sid in source_node_ids:
        node = self.get_node(sid)
        if not node:
            raise ValueError(f"Source node '{sid}' not found")
        source_nodes.append(node)

    # Compute scope intersection
    from scope import Scope
    scopes = [Scope.from_dict(json.loads(n.get("scope", "{}"))) for n in source_nodes]
    result_scope = scopes[0]
    for s in scopes[1:]:
        result_scope = result_scope.intersection(s)

    # Create finding node
    properties = {
        "confidence": confidence,
        "contributed_by": agent_name,
        "source_count": len(source_node_ids),
        "insight_type": "agent_synthesis",
    }
    if tags:
        properties["tags"] = tags

    node_id = self.add_node(
        label=insight[:100],  # Truncate label, full text in summary
        kind="finding",
        summary=insight,
        properties=properties,
        source_type="agent",
        scope=result_scope.to_dict(),
    )

    # Create DERIVED_FROM edges
    for sid in source_node_ids:
        self.add_edge(
            source_id=node_id,
            target_id=sid,
            relation="DERIVED_FROM",
            provenance="INFERRED",
        )

    self._db.commit()
    return {"node_id": node_id, "edges_created": len(source_node_ids)}
```

- [ ] **Step 4: Run tests to verify they pass**

- [ ] **Step 5: Commit**

---

## Task 2: POST /insight Server Endpoint

**Files:**
- Modify: `images/knowledge/server.py`

- [ ] **Step 1: Add endpoint handler**

```python
async def handle_save_insight(request):
    """POST /insight — save an agent's synthesized insight.

    Body: {
        "insight": "...",           # Required
        "source_nodes": ["id1"],    # Required — node IDs
        "confidence": "high",       # Required — high/medium/low
        "tags": ["risk"],           # Optional
        "agent_name": "..."         # Optional
    }
    """
    store = request.app["store"]
    body = await request.json()

    insight = body.get("insight", "")
    source_nodes = body.get("source_nodes", [])
    confidence = body.get("confidence", "medium")
    tags = body.get("tags", [])
    agent_name = body.get("agent_name", "")

    if not insight:
        return web.json_response({"error": "insight is required"}, status=400)
    if not source_nodes:
        return web.json_response({"error": "source_nodes is required"}, status=400)

    try:
        result = store.save_insight(
            insight=insight,
            source_node_ids=source_nodes,
            confidence=confidence,
            tags=tags,
            agent_name=agent_name,
        )
        return web.json_response(result)
    except ValueError as e:
        return web.json_response({"error": str(e)}, status=400)
```

Register: `app.router.add_post("/insight", handle_save_insight)`

- [ ] **Step 2: Verify imports, commit**

---

## Task 3: save_insight Body Runtime Tool

**Files:**
- Modify: `images/body/knowledge_tools.py`

- [ ] **Step 1: Add tool registration**

In `register_knowledge_tools()`, add the `save_insight` tool following the same pattern as `contribute_knowledge`:

```python
def _save_insight(base_url, agent_name, args, active_mission=None):
    """Save a synthesized insight back to the knowledge graph."""
    insight = args.get("insight", "")
    source_nodes = args.get("source_nodes", [])
    confidence = args.get("confidence", "medium")
    tags = args.get("tags", [])

    payload = {
        "insight": insight,
        "source_nodes": source_nodes,
        "confidence": confidence,
        "tags": tags,
        "agent_name": agent_name,
    }
    # POST to knowledge service
    resp = httpx.post(f"{base_url}/insight", json=payload, timeout=30)
    ...
```

Tool parameters:
- insight: string (required) — the conclusion or synthesis
- source_nodes: array of strings (required) — node IDs from prior query_knowledge results
- confidence: string (required) — "high", "medium", or "low"
- tags: array of strings (optional)

- [ ] **Step 2: Verify imports, commit**

---

## Task 4: Go Gateway Insight Proxy + CLI

**Files:**
- Modify: `internal/knowledge/proxy.go`
- Modify: `internal/api/routes.go`
- Modify: `internal/api/handlers_hub.go`
- Modify: `internal/apiclient/client.go`
- Modify: `internal/cli/commands.go`

- [ ] **Step 1: Add proxy method**

```go
func (p *Proxy) SaveInsight(ctx context.Context, insight string, sourceNodes []string, confidence string, tags []string, agentName string) (json.RawMessage, error)
```

- [ ] **Step 2: Add route + handler**

`r.Post("/knowledge/insight", h.knowledgeSaveInsight)`

- [ ] **Step 3: Add CLI**

`agency knowledge insight "insight text" --sources id1,id2 --confidence high --tags risk,security`

- [ ] **Step 4: Build, verify, commit**

---

## Task 5: Full Test Suite Validation

- [ ] **Step 1: Run Phase 4 tests**

```bash
python3 -m pytest images/tests/test_save_insight.py -v
```

- [ ] **Step 2: Run all previous phase tests**

```bash
python3 -m pytest images/tests/test_graph_intelligence.py images/tests/test_edge_provenance.py images/tests/test_principal_registry.py images/tests/test_scope_model.py images/tests/test_extractors.py images/tests/test_source_classifier.py images/tests/test_merge_buffer.py images/tests/test_ingestion_pipeline.py images/tests/test_html_extractor.py images/tests/test_code_extractor.py images/tests/test_pdf_extractor.py images/tests/test_watcher.py -v
```

- [ ] **Step 3: Build Go gateway**

---

## Summary

| Component | What it adds |
|-----------|-------------|
| **save_insight() store method** | Creates finding node + DERIVED_FROM edges, scope intersection, validation |
| **POST /insight endpoint** | Knowledge server endpoint for insight submission |
| **save_insight body tool** | Agent-facing tool for persisting synthesized conclusions |
| **Gateway + CLI** | Go proxy, route, handler, `agency knowledge insight` command |

This completes the Knowledge Graph Intelligence spec — all four phases delivered.
