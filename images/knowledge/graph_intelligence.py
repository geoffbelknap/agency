"""Graph intelligence: community detection and hub analysis.

Uses NetworkX for graph algorithms. Operates on the KnowledgeStore's
SQLite backend, building a transient in-memory graph for analysis.
"""

import json
import logging
import uuid
from collections import Counter

try:
    import networkx as nx
    _NETWORKX_AVAILABLE = True
except ImportError:
    _NETWORKX_AVAILABLE = False
    nx = None

logger = logging.getLogger("agency.knowledge.graph_intelligence")

# Kinds that represent infrastructure/structural entities, not knowledge
_STRUCTURAL_KINDS = {"agent", "channel", "task", "Community", "OntologyCandidate", "RelationshipCandidate"}

# Only edges with these provenance values are included in the analysis graph
_VALID_EDGE_PROVENANCE = {"EXTRACTED", "INFERRED"}


class CommunityDetector:
    """Detect communities in the knowledge graph using Louvain algorithm.

    Builds a NetworkX graph from KnowledgeStore nodes and edges, runs
    community detection, and writes Community nodes and assignments
    back to the store.
    """

    def __init__(self, store, max_community_fraction: float = 0.25, resolution: float = 1.0):
        self.store = store
        self.max_community_fraction = max_community_fraction
        self.resolution = resolution

    def _build_graph(self) -> nx.Graph:
        """Load eligible nodes and edges from the store into a NetworkX Graph."""
        G = nx.Graph()
        db = self.store._db

        # Load nodes: exclude structural kinds and curated nodes (except flagged)
        structural_placeholders = ", ".join("?" for _ in _STRUCTURAL_KINDS)
        rows = db.execute(
            f"SELECT id, label, kind FROM nodes "
            f"WHERE kind NOT IN ({structural_placeholders}) "
            f"AND (curation_status IS NULL OR curation_status = 'flagged')",
            tuple(sorted(_STRUCTURAL_KINDS)),
        ).fetchall()

        for row in rows:
            G.add_node(row["id"], label=row["label"], kind=row["kind"])

        node_ids = set(G.nodes())

        # Load edges: only EXTRACTED or INFERRED provenance
        prov_placeholders = ", ".join("?" for _ in _VALID_EDGE_PROVENANCE)
        edge_rows = db.execute(
            f"SELECT source_id, target_id, weight, provenance FROM edges "
            f"WHERE provenance IN ({prov_placeholders})",
            tuple(sorted(_VALID_EDGE_PROVENANCE)),
        ).fetchall()

        for edge in edge_rows:
            src, tgt = edge["source_id"], edge["target_id"]
            if src in node_ids and tgt in node_ids:
                G.add_edge(src, tgt, weight=edge["weight"], provenance=edge["provenance"])

        return G

    def detect(self) -> dict:
        """Run community detection and write results to the store.

        Returns:
            Dict with communities_found and nodes_assigned counts.
            Returns zeros if networkx is not available.
        """
        if not _NETWORKX_AVAILABLE:
            logger.warning("networkx not installed — community detection skipped")
            return {"communities_found": 0, "nodes_assigned": 0}

        G = self._build_graph()

        if G.number_of_nodes() == 0:
            return {"communities_found": 0, "nodes_assigned": 0}

        # Clear previous community assignments
        self.store.clear_communities()

        # Remove existing Community nodes from the store
        existing = self.store._db.execute(
            "SELECT id FROM nodes WHERE kind = 'Community'"
        ).fetchall()
        for row in existing:
            self.store._db.execute("DELETE FROM nodes WHERE id = ?", (row["id"],))
        self.store._db.commit()

        # Run Louvain community detection
        communities = nx.community.louvain_communities(G, resolution=self.resolution, seed=42)

        # Recursive splitting: communities larger than max_community_fraction
        # of the graph get a second pass at higher resolution
        total_nodes = G.number_of_nodes()
        max_size = max(1, int(total_nodes * self.max_community_fraction))
        final_communities = []

        for community in communities:
            if len(community) > max_size:
                # Build subgraph and re-run at higher resolution
                sub_G = G.subgraph(community).copy()
                sub_communities = nx.community.louvain_communities(
                    sub_G, resolution=self.resolution * 1.5, seed=42
                )
                final_communities.extend(sub_communities)
            else:
                final_communities.append(community)

        # Create Community nodes and assign members
        nodes_assigned = 0
        for members in final_communities:
            if not members:
                continue

            comm_id = uuid.uuid4().hex[:12]

            # Compute cohesion: internal edges / max possible edges
            subgraph = G.subgraph(members)
            internal_edges = subgraph.number_of_edges()
            n = len(members)
            max_edges = n * (n - 1) / 2
            cohesion = internal_edges / max_edges if max_edges > 0 else 0.0

            # Compute provenance mix from edges within the community
            provenance_mix = Counter()
            for u, v, data in subgraph.edges(data=True):
                prov = data.get("provenance", "EXTRACTED")
                provenance_mix[prov] += 1

            # Top members by degree within the community
            degrees = sorted(
                ((node, subgraph.degree(node)) for node in members),
                key=lambda x: x[1],
                reverse=True,
            )
            top_members = [
                G.nodes[node_id]["label"]
                for node_id, _ in degrees[:5]
            ]

            # Find the top member label for the community name
            top_label = top_members[0] if top_members else "unknown"

            # Create the Community node
            self.store.add_node(
                label=f"community:{top_label}",
                kind="Community",
                summary=f"Community of {len(members)} nodes",
                properties={
                    "member_count": len(members),
                    "cohesion": round(cohesion, 3),
                    "provenance_mix": dict(provenance_mix),
                    "top_members": top_members,
                    "community_uuid": comm_id,
                },
            )

            # Assign community_id to all member nodes
            for node_id in members:
                self.store.update_community(node_id, community_id=comm_id, cohesion=cohesion)
                nodes_assigned += 1

        return {
            "communities_found": len(final_communities),
            "nodes_assigned": nodes_assigned,
        }


# File-extension pattern for filtering mechanical hubs (e.g. "main.py", "config.yaml")
import re

_FILE_EXT_PATTERN = re.compile(r"\.\w{1,10}$")


class HubDetector:
    """Detect hub and bridge nodes in the knowledge graph.

    Hub nodes are highly connected knowledge entities.  Bridge nodes sit
    between communities and have high betweenness centrality.

    Unlike CommunityDetector, the graph includes ALL edges (no provenance
    filter) because hub analysis considers all connections.
    """

    def __init__(self, store, top_n: int = 20):
        self.store = store
        self.top_n = top_n

    def _build_graph(self) -> nx.Graph:
        """Load ALL nodes and ALL edges into a NetworkX graph."""
        G = nx.Graph()
        db = self.store._db

        # Load all non-curated nodes
        rows = db.execute(
            "SELECT id, label, kind, summary, community_id FROM nodes "
            "WHERE (curation_status IS NULL OR curation_status = 'flagged')"
        ).fetchall()

        for row in rows:
            G.add_node(
                row["id"],
                label=row["label"],
                kind=row["kind"],
                summary=row["summary"] or "",
                community_id=row["community_id"],
            )

        node_ids = set(G.nodes())

        edge_rows = db.execute(
            "SELECT source_id, target_id, weight FROM edges"
        ).fetchall()

        for edge in edge_rows:
            src, tgt = edge["source_id"], edge["target_id"]
            if src in node_ids and tgt in node_ids:
                G.add_edge(src, tgt, weight=edge["weight"])

        return G

    def detect(self) -> dict:
        """Run hub and bridge detection, write results to the store.

        Returns:
            Dict with hubs_found and bridges_found counts.
            Returns zeros if networkx is not available.
        """
        if not _NETWORKX_AVAILABLE:
            logger.warning("networkx not installed — hub detection skipped")
            return {"hubs_found": 0, "bridges_found": 0}

        G = self._build_graph()

        if G.number_of_nodes() == 0:
            return {"hubs_found": 0, "bridges_found": 0}

        self.store.clear_hubs()

        # --- Hub detection by degree ---
        degrees = dict(G.degree())

        # Filter: exclude structural kinds
        eligible = {
            nid: deg
            for nid, deg in degrees.items()
            if G.nodes[nid]["kind"] not in _STRUCTURAL_KINDS
        }

        # Filter: exclude mechanical hubs (labels with file extensions)
        eligible = {
            nid: deg
            for nid, deg in eligible.items()
            if not _FILE_EXT_PATTERN.search(G.nodes[nid]["label"])
        }

        # Filter: exclude nodes with empty summaries
        eligible = {
            nid: deg
            for nid, deg in eligible.items()
            if G.nodes[nid]["summary"].strip()
        }

        if not eligible:
            return {"hubs_found": 0, "bridges_found": 0}

        max_degree = max(eligible.values())
        if max_degree == 0:
            return {"hubs_found": 0, "bridges_found": 0}

        # Top N by degree → hub
        sorted_by_degree = sorted(eligible.items(), key=lambda x: x[1], reverse=True)
        top_hubs = sorted_by_degree[: self.top_n]
        hub_node_ids = set()

        for nid, deg in top_hubs:
            score = deg / max_degree
            self.store.update_hub(nid, hub_score=score, hub_type="hub")
            hub_node_ids.add(nid)

        hubs_found = len(top_hubs)

        # --- Bridge detection by betweenness centrality ---
        bridges_found = 0
        bc = nx.betweenness_centrality(G)

        for nid, centrality in bc.items():
            if centrality <= 0.1:
                continue
            if G.nodes[nid]["kind"] in _STRUCTURAL_KINDS:
                continue
            # Check if neighbors span different communities
            neighbor_communities = set()
            for neighbor in G.neighbors(nid):
                comm = G.nodes[neighbor].get("community_id")
                if comm is not None:
                    neighbor_communities.add(comm)
            if len(neighbor_communities) < 2:
                continue

            # Mark as bridge (overwrites hub if already set)
            self.store.update_hub(nid, hub_score=centrality, hub_type="bridge")
            bridges_found += 1
            if nid not in hub_node_ids:
                hubs_found += 1

        return {"hubs_found": hubs_found, "bridges_found": bridges_found}
