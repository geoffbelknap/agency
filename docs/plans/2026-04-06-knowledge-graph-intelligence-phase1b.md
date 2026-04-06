# Knowledge Graph Intelligence — Phase 1b: Scope Enforcement in Traversal

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enforce authorization scopes in all graph traversal methods so agents can't see nodes outside their scope, even via multi-hop queries. Also add provenance-based retrieval weighting and edge scope inheritance validation.

**Architecture:** Add `principal` parameter to `get_subgraph()`, `get_neighbors()`, `get_neighbors_subgraph()`, and `find_path()`. BFS traversal stops at scope boundaries — nodes outside scope are not added to the frontier. Edge scope inheritance is validated at `add_edge()` time.

**Tech Stack:** Python (knowledge service)

**Spec:** `docs/specs/knowledge-graph-intelligence.md` — Phase 1 deferred items

---

## Task 1: Scope enforcement in get_subgraph()

**Files:**
- Modify: `images/knowledge/store.py` (get_subgraph at ~line 705)
- Test: `images/tests/test_scope_model.py` (append)

Add `principal=None` parameter. During BFS frontier expansion, filter each frontier node by scope before adding to visited set. Nodes outside scope are not visited and their edges are not followed — traversal stops at scope boundaries.

Tests: create nodes with different scopes, verify get_subgraph with principal only returns nodes within scope, verify multi-hop traversal doesn't cross scope boundaries.

## Task 2: Scope enforcement in get_neighbors() and get_neighbors_subgraph()

**Files:**
- Modify: `images/knowledge/store.py`
- Test: `images/tests/test_scope_model.py` (append)

Add `principal=None` parameter to both methods. Post-filter neighbor nodes using `_filter_by_scope()`.

Tests: create neighbors with mixed scopes, verify only in-scope neighbors returned.

## Task 3: Scope enforcement in find_path()

**Files:**
- Modify: `images/knowledge/store.py` (find_path at ~line 927)
- Test: `images/tests/test_scope_model.py` (append)

Add `principal=None` parameter. During BFS, skip nodes that fail scope check — path finding only traverses in-scope nodes. If no in-scope path exists, return None.

Tests: create a graph where the only path crosses a scope boundary, verify find_path returns None with that principal.

## Task 4: Edge scope inheritance validation

**Files:**
- Modify: `images/knowledge/store.py` (add_edge)
- Test: `images/tests/test_scope_model.py` (append)

Validate at `add_edge()` time: if the edge has explicit scope, it must be narrower than or equal to the source node's scope (never wider). Use `Scope.is_narrower_than()`.

Tests: verify edge with wider scope than source node is rejected.

## Task 5: GraphRAG provenance weighting

**Files:**
- Modify: `images/knowledge/store.py` (find_nodes)
- Test: `images/tests/test_edge_provenance.py` (append)

When computing relevance in find_nodes(), weight results by their connected edges' provenance. Nodes connected primarily by EXTRACTED edges rank higher than nodes connected by AMBIGUOUS edges. Implement as a post-retrieval re-ranking step.

## Task 6: Body runtime query_graph min_provenance param

**Files:**
- Modify: `images/body/knowledge_tools.py`
- Test: `images/tests/test_save_insight.py` (append) or new test

Add `min_provenance` as an optional parameter to the `query_graph` tool's `get_neighbors` pattern. Pass it through to the knowledge service.

## Task 7: Server endpoint updates + validation

Wire the new `principal` parameters through the server endpoints that call these methods. Run full test suite.
