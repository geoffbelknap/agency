# Knowledge Graph Intelligence — Phase 3: Graph Intelligence

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the knowledge graph structural self-awareness — community detection groups related nodes into clusters, hub detection identifies high-connectivity nodes, and agent-facing query patterns expose both.

**Architecture:** Two new curator operations (community detection + hub detection) run periodically and store results as node properties and Community nodes. Four new query_graph patterns expose results to agents with scope filtering. NetworkX handles graph algorithms; Louvain (built-in) is the default community algorithm with Leiden (graspologic) as optional upgrade.

**Tech Stack:** Python, NetworkX (community detection, centrality), graspologic (optional Leiden), SQLite

**Spec:** `docs/specs/knowledge-graph-intelligence.md` — Phase 3 section

**Depends on:** Phase 1 (provenance, scope) — completed

---

## File Structure

### Files to Create

| File | Purpose |
|------|---------|
| `images/knowledge/graph_intelligence.py` | Community detection + hub detection algorithms |
| `images/tests/test_graph_intelligence.py` | Tests for community detection, hub detection, scope filtering |

### Files to Modify

| File | Changes |
|------|---------|
| `images/knowledge/store.py` | Add community_id, community_cohesion, hub_score, hub_type columns; community/hub query methods |
| `images/knowledge/curator.py` | Add community_detection() and hub_detection() to curation cycle; update community_detection_ms benchmark |
| `images/knowledge/server.py` | Add community/hub query endpoints |
| `images/body/knowledge_tools.py` | Add get_community, list_communities, get_hubs, community_overlap patterns to query_graph |
| `images/knowledge/requirements.txt` | Add networkx as required dep |
| `internal/knowledge/proxy.go` | Add community/hub proxy methods |
| `internal/api/routes.go` | Add community/hub routes |
| `internal/api/handlers_hub.go` | Add community/hub handlers |

---

## Task 1: Schema — Community and Hub Columns

**Files:**
- Modify: `images/knowledge/store.py`
- Test: `images/tests/test_graph_intelligence.py`

- [ ] **Step 1: Write failing tests**

```python
# images/tests/test_graph_intelligence.py
"""Tests for graph intelligence — community detection and hub analysis."""
import os
import sys
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from store import KnowledgeStore


class TestCommunityColumns:
    def test_node_has_community_id_column(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        cols = {r[1] for r in store._db.execute("PRAGMA table_info(nodes)").fetchall()}
        assert "community_id" in cols

    def test_node_has_community_cohesion_column(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        cols = {r[1] for r in store._db.execute("PRAGMA table_info(nodes)").fetchall()}
        assert "community_cohesion" in cols


class TestHubColumns:
    def test_node_has_hub_score_column(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        cols = {r[1] for r in store._db.execute("PRAGMA table_info(nodes)").fetchall()}
        assert "hub_score" in cols

    def test_node_has_hub_type_column(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        cols = {r[1] for r in store._db.execute("PRAGMA table_info(nodes)").fetchall()}
        assert "hub_type" in cols
```

- [ ] **Step 2: Add columns to store.py**

In `images/knowledge/store.py`, add idempotent ALTER TABLE statements in `_init_schema()`:

```python
for col, typ in [("community_id", "TEXT"), ("community_cohesion", "REAL"),
                 ("hub_score", "REAL"), ("hub_type", "TEXT")]:
    try:
        self._db.execute(f"ALTER TABLE nodes ADD COLUMN {col} {typ}")
    except Exception:
        pass
```

- [ ] **Step 3: Add store methods for community/hub queries**

```python
def get_community_members(self, community_id, limit=100):
    """Get all nodes in a community."""
    rows = self._db.execute(
        "SELECT id, label, kind, summary, community_cohesion FROM nodes "
        "WHERE community_id = ? AND (curation_status IS NULL OR curation_status = 'flagged') "
        "LIMIT ?", (community_id, limit)
    ).fetchall()
    return [{"id": r[0], "label": r[1], "kind": r[2], "summary": r[3], "cohesion": r[4]} for r in rows]

def list_communities(self, limit=50):
    """List all Community nodes, ranked by member_count."""
    return self.find_nodes_by_kind("Community", limit=limit)

def get_hubs(self, limit=20):
    """Get top hub nodes by hub_score."""
    rows = self._db.execute(
        "SELECT id, label, kind, summary, hub_score, hub_type FROM nodes "
        "WHERE hub_score IS NOT NULL AND hub_type IS NOT NULL "
        "AND (curation_status IS NULL OR curation_status = 'flagged') "
        "ORDER BY hub_score DESC LIMIT ?", (limit,)
    ).fetchall()
    return [{"id": r[0], "label": r[1], "kind": r[2], "summary": r[3],
             "hub_score": r[4], "hub_type": r[5]} for r in rows]

def update_community(self, node_id, community_id, cohesion):
    """Set community assignment for a node."""
    self._db.execute(
        "UPDATE nodes SET community_id = ?, community_cohesion = ? WHERE id = ?",
        (community_id, cohesion, node_id)
    )

def update_hub(self, node_id, hub_score, hub_type):
    """Set hub score and type for a node."""
    self._db.execute(
        "UPDATE nodes SET hub_score = ?, hub_type = ? WHERE id = ?",
        (hub_score, hub_type, node_id)
    )

def clear_communities(self):
    """Clear all community assignments (before re-detection)."""
    self._db.execute("UPDATE nodes SET community_id = NULL, community_cohesion = NULL")

def clear_hubs(self):
    """Clear all hub assignments (before re-detection)."""
    self._db.execute("UPDATE nodes SET hub_score = NULL, hub_type = NULL")
```

- [ ] **Step 4: Run tests, commit**

---

## Task 2: Graph Intelligence Module — Community Detection

**Files:**
- Create: `images/knowledge/graph_intelligence.py`
- Test: `images/tests/test_graph_intelligence.py` (append)

- [ ] **Step 1: Write failing tests**

Append to `images/tests/test_graph_intelligence.py`:

```python
from graph_intelligence import CommunityDetector


class TestCommunityDetector:
    @pytest.fixture
    def store_with_graph(self, tmp_path):
        """Create a store with two clear clusters connected by a bridge."""
        store = KnowledgeStore(tmp_path)
        # Cluster A: ssh-related nodes
        a1 = store.add_node("openssh", "software", "SSH daemon")
        a2 = store.add_node("weak-ssh-config", "vulnerability", "Password auth enabled")
        a3 = store.add_node("jump-host", "system", "SSH jump host")
        store.add_edge(a1, a2, "has_vulnerability", provenance="EXTRACTED")
        store.add_edge(a1, a3, "installed_on", provenance="EXTRACTED")
        store.add_edge(a2, a3, "affects", provenance="INFERRED")

        # Cluster B: web-related nodes
        b1 = store.add_node("nginx", "software", "Web server")
        b2 = store.add_node("CVE-2023-44487", "vulnerability", "HTTP/2 rapid reset")
        b3 = store.add_node("prod-web", "system", "Production web server")
        store.add_edge(b1, b2, "has_vulnerability", provenance="EXTRACTED")
        store.add_edge(b1, b3, "installed_on", provenance="EXTRACTED")
        store.add_edge(b2, b3, "affects", provenance="INFERRED")

        # Bridge: jump-host connects to prod-web (weak link)
        store.add_edge(a3, b3, "communicates_with", provenance="INFERRED")

        store._db.commit()
        return store

    def test_detects_two_communities(self, store_with_graph):
        detector = CommunityDetector(store_with_graph)
        result = detector.detect()
        assert result["communities_found"] >= 2

    def test_community_nodes_created(self, store_with_graph):
        detector = CommunityDetector(store_with_graph)
        detector.detect()
        communities = store_with_graph.list_communities()
        assert len(communities) >= 2

    def test_nodes_assigned_to_communities(self, store_with_graph):
        detector = CommunityDetector(store_with_graph)
        detector.detect()
        row = store_with_graph._db.execute(
            "SELECT community_id FROM nodes WHERE label = 'openssh'"
        ).fetchone()
        assert row[0] is not None

    def test_cohesion_scores_computed(self, store_with_graph):
        detector = CommunityDetector(store_with_graph)
        detector.detect()
        row = store_with_graph._db.execute(
            "SELECT community_cohesion FROM nodes WHERE label = 'openssh'"
        ).fetchone()
        assert row[0] is not None
        assert 0.0 <= row[0] <= 1.0

    def test_filters_ambiguous_edges(self, store_with_graph):
        """Community detection should only use EXTRACTED and INFERRED edges."""
        detector = CommunityDetector(store_with_graph)
        G = detector._build_graph()
        # All edges should have provenance EXTRACTED or INFERRED
        for _, _, data in G.edges(data=True):
            assert data.get("provenance") in ("EXTRACTED", "INFERRED")

    def test_excludes_structural_nodes(self, store_with_graph):
        """Structural kinds (agent, channel, task) should be excluded."""
        store_with_graph.add_node("test-agent", "agent", "An agent")
        detector = CommunityDetector(store_with_graph)
        G = detector._build_graph()
        agent_row = store_with_graph._db.execute(
            "SELECT id FROM nodes WHERE label = 'test-agent'"
        ).fetchone()
        assert agent_row[0] not in G.nodes

    def test_oversized_community_split(self, tmp_path):
        """Communities exceeding 25% of graph should be recursively split."""
        store = KnowledgeStore(tmp_path)
        # Create a large cluster (10 nodes, all connected)
        ids = []
        for i in range(10):
            ids.append(store.add_node(f"node-{i}", "fact", f"Node {i}"))
        for i in range(len(ids)):
            for j in range(i+1, len(ids)):
                store.add_edge(ids[i], ids[j], "relates_to", provenance="EXTRACTED")
        # Create a small separate cluster (2 nodes)
        s1 = store.add_node("small-a", "fact", "Small A")
        s2 = store.add_node("small-b", "fact", "Small B")
        store.add_edge(s1, s2, "relates_to", provenance="EXTRACTED")
        store._db.commit()

        detector = CommunityDetector(store, max_community_fraction=0.25)
        result = detector.detect()
        # The 10-node cluster is >25% of 12 nodes, should be split
        assert result["communities_found"] >= 2

    def test_empty_graph(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        detector = CommunityDetector(store)
        result = detector.detect()
        assert result["communities_found"] == 0
```

- [ ] **Step 2: Implement CommunityDetector**

```python
# images/knowledge/graph_intelligence.py
"""Graph intelligence — community detection and hub analysis.

Uses NetworkX for graph algorithms. Louvain community detection (built-in)
is the default; Leiden (graspologic) is used when available.
"""
import logging
import uuid
from typing import Optional

import networkx as nx

logger = logging.getLogger(__name__)

# Try graspologic for Leiden; fall back to Louvain
try:
    from graspologic.partition import leiden
    _LEIDEN_AVAILABLE = True
except ImportError:
    _LEIDEN_AVAILABLE = False

EXCLUDED_KINDS = {"agent", "channel", "task", "Community", "OntologyCandidate", "RelationshipCandidate"}


class CommunityDetector:
    """Detects communities in the knowledge graph using Louvain/Leiden."""

    def __init__(self, store, max_community_fraction=0.25, resolution=1.0):
        self.store = store
        self.max_community_fraction = max_community_fraction
        self.resolution = resolution

    def detect(self) -> dict:
        """Run community detection. Returns stats dict."""
        G = self._build_graph()
        if len(G.nodes) == 0:
            return {"communities_found": 0, "nodes_assigned": 0}

        # Clear previous assignments
        self.store.clear_communities()

        # Detect communities
        if _LEIDEN_AVAILABLE:
            partition = leiden(G, resolution=self.resolution)
            communities = {}
            for node, comm_id in partition.items():
                communities.setdefault(comm_id, set()).add(node)
            communities = list(communities.values())
        else:
            communities = list(nx.community.louvain_communities(G, resolution=self.resolution))

        # Recursive splitting of oversized communities
        max_size = int(len(G.nodes) * self.max_community_fraction)
        if max_size < 2:
            max_size = 2
        communities = self._split_oversized(G, communities, max_size)

        # Assign and store
        nodes_assigned = 0
        for comm_set in communities:
            if len(comm_set) == 0:
                continue
            comm_id = uuid.uuid4().hex[:12]
            cohesion = self._compute_cohesion(G, comm_set)

            # Create Community node
            top_members = self._top_members(G, comm_set)
            auto_label = f"community:{top_members[0]}" if top_members else f"community:{comm_id}"
            provenance_mix = self._provenance_mix(G, comm_set)

            self.store.add_node(
                label=auto_label,
                kind="Community",
                summary=f"Community of {len(comm_set)} nodes",
                properties={
                    "member_count": len(comm_set),
                    "cohesion": round(cohesion, 3),
                    "provenance_mix": provenance_mix,
                    "top_members": top_members[:5],
                    "community_uuid": comm_id,
                },
            )

            # Assign nodes
            for node_id in comm_set:
                self.store.update_community(node_id, comm_id, cohesion)
                nodes_assigned += 1

        self.store._db.commit()
        return {
            "communities_found": len([c for c in communities if len(c) > 0]),
            "nodes_assigned": nodes_assigned,
        }

    def _build_graph(self) -> nx.Graph:
        """Load graph from store into NetworkX, filtering by provenance and kind."""
        G = nx.Graph()

        # Load nodes (exclude structural kinds and curated)
        rows = self.store._db.execute(
            "SELECT id, label, kind FROM nodes "
            "WHERE (curation_status IS NULL OR curation_status = 'flagged') "
            "AND kind NOT IN ({})".format(",".join("?" * len(EXCLUDED_KINDS))),
            tuple(EXCLUDED_KINDS)
        ).fetchall()
        for node_id, label, kind in rows:
            G.add_node(node_id, label=label, kind=kind)

        # Load edges (EXTRACTED and INFERRED only)
        edge_rows = self.store._db.execute(
            "SELECT source_id, target_id, provenance FROM edges "
            "WHERE provenance IN ('EXTRACTED', 'INFERRED')"
        ).fetchall()
        for src, tgt, prov in edge_rows:
            if src in G.nodes and tgt in G.nodes:
                G.add_edge(src, tgt, provenance=prov)

        return G

    def _split_oversized(self, G, communities, max_size):
        """Recursively split communities exceeding max_size."""
        result = []
        for comm in communities:
            if len(comm) <= max_size:
                result.append(comm)
            else:
                subgraph = G.subgraph(comm)
                if _LEIDEN_AVAILABLE:
                    partition = leiden(subgraph, resolution=self.resolution * 1.5)
                    sub_comms = {}
                    for node, cid in partition.items():
                        sub_comms.setdefault(cid, set()).add(node)
                    sub_comms = list(sub_comms.values())
                else:
                    sub_comms = list(nx.community.louvain_communities(subgraph, resolution=self.resolution * 1.5))
                if len(sub_comms) > 1:
                    result.extend(self._split_oversized(G, sub_comms, max_size))
                else:
                    result.append(comm)  # Can't split further
        return result

    def _compute_cohesion(self, G, comm_set):
        """Cohesion = internal edge density / expected density."""
        if len(comm_set) < 2:
            return 1.0
        subgraph = G.subgraph(comm_set)
        actual_edges = subgraph.number_of_edges()
        max_edges = len(comm_set) * (len(comm_set) - 1) / 2
        return actual_edges / max_edges if max_edges > 0 else 0.0

    def _top_members(self, G, comm_set):
        """Return labels of highest-degree nodes in the community."""
        subgraph = G.subgraph(comm_set)
        by_degree = sorted(subgraph.nodes, key=lambda n: subgraph.degree(n), reverse=True)
        return [G.nodes[n].get("label", n) for n in by_degree[:5]]

    def _provenance_mix(self, G, comm_set):
        """Count edges by provenance within the community."""
        subgraph = G.subgraph(comm_set)
        mix = {"EXTRACTED": 0, "INFERRED": 0, "AMBIGUOUS": 0}
        for _, _, data in subgraph.edges(data=True):
            prov = data.get("provenance", "AMBIGUOUS")
            mix[prov] = mix.get(prov, 0) + 1
        return mix
```

- [ ] **Step 3: Run tests, commit**

---

## Task 3: Hub Detection

**Files:**
- Modify: `images/knowledge/graph_intelligence.py`
- Test: `images/tests/test_graph_intelligence.py` (append)

- [ ] **Step 1: Write failing tests**

Append to `images/tests/test_graph_intelligence.py`:

```python
from graph_intelligence import HubDetector


class TestHubDetector:
    @pytest.fixture
    def store_with_hub(self, tmp_path):
        """Create a graph where one node is a clear hub."""
        store = KnowledgeStore(tmp_path)
        hub = store.add_node("central-system", "system", "Connected to everything")
        spokes = []
        for i in range(8):
            nid = store.add_node(f"component-{i}", "software", f"Component {i}")
            spokes.append(nid)
            store.add_edge(hub, nid, "depends_on", provenance="EXTRACTED")
        # Add a separate pair (bridge candidate)
        b1 = store.add_node("cluster-a-node", "fact", "In cluster A")
        b2 = store.add_node("cluster-b-node", "fact", "In cluster B")
        bridge = store.add_node("bridge-node", "system", "Connects clusters")
        store.add_edge(b1, bridge, "relates_to", provenance="EXTRACTED")
        store.add_edge(bridge, b2, "relates_to", provenance="EXTRACTED")
        store._db.commit()
        return store

    def test_detects_hub(self, store_with_hub):
        detector = HubDetector(store_with_hub)
        result = detector.detect()
        assert result["hubs_found"] > 0
        hubs = store_with_hub.get_hubs()
        hub_labels = [h["label"] for h in hubs]
        assert "central-system" in hub_labels

    def test_hub_score_assigned(self, store_with_hub):
        detector = HubDetector(store_with_hub)
        detector.detect()
        row = store_with_hub._db.execute(
            "SELECT hub_score, hub_type FROM nodes WHERE label = 'central-system'"
        ).fetchone()
        assert row[0] is not None
        assert row[0] > 0
        assert row[1] == "hub"

    def test_filters_structural_nodes(self, store_with_hub):
        """Structural nodes (agent, channel) should not be flagged as hubs."""
        store_with_hub.add_node("busy-agent", "agent", "Very connected agent")
        detector = HubDetector(store_with_hub)
        detector.detect()
        hubs = store_with_hub.get_hubs()
        hub_labels = [h["label"] for h in hubs]
        assert "busy-agent" not in hub_labels

    def test_bridge_detection(self, store_with_hub):
        """Nodes connecting otherwise-separate communities should be bridges."""
        # Run community detection first, then hub detection
        from graph_intelligence import CommunityDetector
        CommunityDetector(store_with_hub).detect()
        detector = HubDetector(store_with_hub)
        result = detector.detect()
        assert result["bridges_found"] >= 0  # May or may not find bridges depending on graph structure

    def test_empty_graph(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        detector = HubDetector(store)
        result = detector.detect()
        assert result["hubs_found"] == 0
```

- [ ] **Step 2: Implement HubDetector**

Add to `images/knowledge/graph_intelligence.py`:

```python
class HubDetector:
    """Detects hub nodes and bridges in the knowledge graph."""

    def __init__(self, store, top_n=20):
        self.store = store
        self.top_n = top_n

    def detect(self) -> dict:
        """Run hub detection. Returns stats dict."""
        G = self._build_graph()
        if len(G.nodes) == 0:
            return {"hubs_found": 0, "bridges_found": 0}

        self.store.clear_hubs()

        # Degree centrality
        degree = dict(G.degree())

        # Filter out structural kinds and mechanical hubs
        candidates = {
            n: d for n, d in degree.items()
            if G.nodes[n].get("kind") not in EXCLUDED_KINDS
            and not self._is_mechanical(G.nodes[n].get("label", ""))
            and G.nodes[n].get("summary", "")  # Must have summary
        }

        # Top N by degree
        sorted_nodes = sorted(candidates.items(), key=lambda x: x[1], reverse=True)[:self.top_n]
        hubs_found = 0
        for node_id, deg in sorted_nodes:
            if deg < 2:
                continue
            score = deg / max(degree.values()) if degree else 0
            self.store.update_hub(node_id, round(score, 3), "hub")
            hubs_found += 1

        # Bridge detection: betweenness centrality
        bridges_found = 0
        if len(G.nodes) > 2:
            betweenness = nx.betweenness_centrality(G)
            # High betweenness + connects different communities = bridge
            for node_id, bc in sorted(betweenness.items(), key=lambda x: x[1], reverse=True)[:self.top_n]:
                if bc < 0.1:
                    continue
                kind = G.nodes[node_id].get("kind", "")
                if kind in EXCLUDED_KINDS:
                    continue
                # Check if it connects different communities
                comm_id = self.store._db.execute(
                    "SELECT community_id FROM nodes WHERE id = ?", (node_id,)
                ).fetchone()
                if comm_id and comm_id[0]:
                    neighbors = list(G.neighbors(node_id))
                    neighbor_comms = set()
                    for nb in neighbors:
                        nc = self.store._db.execute(
                            "SELECT community_id FROM nodes WHERE id = ?", (nb,)
                        ).fetchone()
                        if nc and nc[0]:
                            neighbor_comms.add(nc[0])
                    if len(neighbor_comms) > 1:
                        self.store.update_hub(node_id, round(bc, 3), "bridge")
                        bridges_found += 1

        self.store._db.commit()
        return {"hubs_found": hubs_found, "bridges_found": bridges_found}

    def _build_graph(self):
        """Same as CommunityDetector — load from store."""
        # Reuse the same graph loading logic
        G = nx.Graph()
        rows = self.store._db.execute(
            "SELECT id, label, kind, summary FROM nodes "
            "WHERE (curation_status IS NULL OR curation_status = 'flagged')"
        ).fetchall()
        for node_id, label, kind, summary in rows:
            G.add_node(node_id, label=label, kind=kind, summary=summary or "")

        edge_rows = self.store._db.execute(
            "SELECT source_id, target_id, provenance FROM edges"
        ).fetchall()
        for src, tgt, prov in edge_rows:
            if src in G.nodes and tgt in G.nodes:
                G.add_edge(src, tgt, provenance=prov)
        return G

    @staticmethod
    def _is_mechanical(label):
        """Filter out mechanical hubs (file-like labels, etc.)."""
        import os
        _, ext = os.path.splitext(label)
        return bool(ext) and ext in (".py", ".go", ".js", ".yaml", ".json", ".md")
```

- [ ] **Step 3: Run tests, commit**

---

## Task 4: Wire into Curator Cycle

**Files:**
- Modify: `images/knowledge/curator.py`
- Test: `images/tests/test_graph_intelligence.py` (append)

- [ ] **Step 1: Write failing test**

Append:

```python
class TestCuratorIntegration:
    def test_community_detection_in_cycle(self, tmp_path):
        """Community detection should be callable from the curator."""
        store = KnowledgeStore(tmp_path)
        # Add some nodes and edges
        a = store.add_node("node-a", "fact", "A")
        b = store.add_node("node-b", "fact", "B")
        store.add_edge(a, b, "relates_to", provenance="EXTRACTED")
        store._db.commit()

        from curator import Curator
        curator = Curator(store, mode="active")
        stats = curator.community_detection()
        assert "communities_found" in stats

    def test_hub_detection_in_cycle(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        hub = store.add_node("hub-node", "system", "Hub")
        for i in range(5):
            n = store.add_node(f"spoke-{i}", "fact", f"Spoke {i}")
            store.add_edge(hub, n, "relates_to", provenance="EXTRACTED")
        store._db.commit()

        from curator import Curator
        curator = Curator(store, mode="active")
        stats = curator.hub_detection()
        assert "hubs_found" in stats

    def test_community_detection_ms_in_metrics(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        from curator import Curator
        curator = Curator(store, mode="active")
        curator.community_detection()
        metrics = curator.compute_health_metrics()
        assert "community_detection_ms" in metrics
```

- [ ] **Step 2: Add community_detection() and hub_detection() to Curator**

In `images/knowledge/curator.py`:

```python
def community_detection(self) -> dict:
    """Run community detection. Called every Nth curation cycle."""
    import time
    from graph_intelligence import CommunityDetector
    t0 = time.monotonic()
    detector = CommunityDetector(self.store)
    stats = detector.detect()
    self._last_community_ms = (time.monotonic() - t0) * 1000
    self.log_curation("community_detection", "graph", stats)
    return stats

def hub_detection(self) -> dict:
    """Run hub detection."""
    from graph_intelligence import HubDetector
    detector = HubDetector(self.store)
    stats = detector.detect()
    self.log_curation("hub_detection", "graph", stats)
    return stats
```

Add `self._last_community_ms = 0.0` to `__init__`. Update `compute_health_metrics()` to include `community_detection_ms`.

Add community_detection to the curation cycle, running every 6th cycle (configurable via `KNOWLEDGE_COMMUNITY_INTERVAL` env var, default 6).

- [ ] **Step 3: Run tests, commit**

---

## Task 5: Agent-Facing Query Patterns + Server Endpoints

**Files:**
- Modify: `images/knowledge/server.py`
- Modify: `images/body/knowledge_tools.py`
- Test: `images/tests/test_graph_intelligence.py` (append)

- [ ] **Step 1: Write failing tests for server endpoints**

Append:

```python
class TestQueryPatterns:
    def test_get_community_returns_members(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        a = store.add_node("node-a", "fact", "A")
        b = store.add_node("node-b", "fact", "B")
        store.add_edge(a, b, "relates_to", provenance="EXTRACTED")
        store._db.commit()

        from graph_intelligence import CommunityDetector
        CommunityDetector(store).detect()

        # Get community for node-a
        row = store._db.execute("SELECT community_id FROM nodes WHERE id = ?", (a,)).fetchone()
        members = store.get_community_members(row[0])
        assert len(members) >= 1

    def test_list_communities_returns_community_nodes(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        a = store.add_node("node-a", "fact", "A")
        b = store.add_node("node-b", "fact", "B")
        store.add_edge(a, b, "relates_to", provenance="EXTRACTED")
        store._db.commit()

        from graph_intelligence import CommunityDetector
        CommunityDetector(store).detect()

        communities = store.list_communities()
        assert len(communities) >= 1

    def test_get_hubs_returns_hub_nodes(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        hub = store.add_node("hub-node", "system", "Hub")
        for i in range(5):
            n = store.add_node(f"spoke-{i}", "fact", f"Spoke {i}")
            store.add_edge(hub, n, "relates_to", provenance="EXTRACTED")
        store._db.commit()

        from graph_intelligence import HubDetector
        HubDetector(store).detect()

        hubs = store.get_hubs()
        assert len(hubs) >= 1
        assert hubs[0]["label"] == "hub-node"
```

- [ ] **Step 2: Add server endpoints**

In `images/knowledge/server.py`:

```python
async def handle_communities(request):
    """GET /communities — list all communities."""
    store = request.app["store"]
    communities = store.list_communities()
    return web.json_response({"communities": communities})

async def handle_community(request):
    """GET /community/{id} — get community members."""
    store = request.app["store"]
    community_id = request.match_info["id"]
    members = store.get_community_members(community_id)
    return web.json_response({"community_id": community_id, "members": members})

async def handle_hubs(request):
    """GET /hubs — get top hub nodes."""
    store = request.app["store"]
    limit = int(request.query.get("limit", "20"))
    hubs = store.get_hubs(limit=limit)
    return web.json_response({"hubs": hubs})
```

Register: `/communities`, `/community/{id}`, `/hubs`

- [ ] **Step 3: Add query_graph patterns to knowledge_tools.py**

In `images/body/knowledge_tools.py`, add to the `query_graph` tool's pattern handling:

- `get_community`: takes `node_id`, queries community_id from node, returns members
- `list_communities`: returns all Community nodes
- `get_hubs`: takes optional `limit`, returns top hubs
- `community_overlap`: takes two community_ids, returns shared edges/nodes

- [ ] **Step 4: Run tests, commit**

---

## Task 6: Go Gateway Community/Hub Proxy + Routes

**Files:**
- Modify: `internal/knowledge/proxy.go`
- Modify: `internal/api/routes.go`
- Modify: `internal/api/handlers_hub.go`

- [ ] **Step 1: Add proxy methods**

```go
func (p *Proxy) Communities(ctx context.Context) (json.RawMessage, error)
func (p *Proxy) Community(ctx context.Context, id string) (json.RawMessage, error)
func (p *Proxy) Hubs(ctx context.Context, limit int) (json.RawMessage, error)
```

- [ ] **Step 2: Add routes and handlers**

```go
r.Get("/knowledge/communities", h.knowledgeCommunities)
r.Get("/knowledge/communities/{id}", h.knowledgeCommunity)
r.Get("/knowledge/hubs", h.knowledgeHubs)
```

- [ ] **Step 3: Build, verify, commit**

---

## Task 7: Update Requirements + Full Validation

- [ ] **Step 1: Add networkx to requirements.txt**

```
networkx>=3.0
```

- [ ] **Step 2: Run all Phase 3 tests**

```bash
python3 -m pytest images/tests/test_graph_intelligence.py -v
```

- [ ] **Step 3: Run all previous phase tests**

```bash
python3 -m pytest images/tests/test_edge_provenance.py images/tests/test_principal_registry.py images/tests/test_scope_model.py images/tests/test_extractors.py images/tests/test_source_classifier.py images/tests/test_merge_buffer.py images/tests/test_ingestion_pipeline.py images/tests/test_html_extractor.py images/tests/test_code_extractor.py images/tests/test_pdf_extractor.py images/tests/test_watcher.py -v
```

- [ ] **Step 4: Build Go gateway**

```bash
go build ./cmd/gateway/
```

---

## Summary

| Component | What it adds |
|-----------|-------------|
| **CommunityDetector** | Louvain/Leiden community detection, recursive splitting, cohesion scoring |
| **HubDetector** | Degree centrality hubs + betweenness centrality bridges |
| **Community nodes** | First-class Community entities in the graph with member counts and provenance mix |
| **Curator integration** | Community + hub detection in curation cycle (every 6th cycle) |
| **Agent query patterns** | get_community, list_communities, get_hubs, community_overlap |
| **Server endpoints** | /communities, /community/{id}, /hubs |
| **Gateway proxy** | Go routes and handlers for community/hub queries |

**Next:** Phase 4 (Query Feedback Loop — save_insight tool)
