---
description: "The knowledge graph has orphan nodes that should be connected but aren't, because:"
status: "Implemented"
---

# Curator Relationship Inference

**Date:** 2026-03-29
**Status:** Implemented

## Problem

The knowledge graph has orphan nodes that should be connected but aren't, because:
1. Different connectors write nodes about the same entities using different identifiers
2. The synthesizer extracts standalone findings without linking them to agents or related entities
3. graph_ingest rules can only create edges within a single payload — cross-connector relationships require either pre-configured correlation or post-hoc inference

The curator already handles fuzzy duplicate detection and orphan pruning, but it doesn't infer relationships between nodes that share property values. The graph should get smarter over time without requiring operators to hard-code every possible relationship.

## Design

Three tiers of relationship inference, from deterministic to exploratory:

### Tier 1: Explicit Rules (auto-create edges)

Operator-defined property match rules. Deterministic, safe to auto-create edges.

```yaml
# In ~/.agency/knowledge/inference-rules.yaml (or ontology.d/)
inference_rules:
  - match_property: host_id
    from_kinds: [Device]
    to_kinds: [network_segment, Device]
    relation: ASSOCIATED_WITH

  - match_property: client_ip
    from_kinds: [DNSQuery, network_endpoint]
    to_kinds: [Device]
    match_to_property: ip_address
    relation: ORIGINATES_FROM

  - match_property: contributed_by
    from_kinds: [finding, fact]
    to_kinds: [agent]
    match_to_property: label
    relation: CONTRIBUTED_BY
```

These are a starting point, not a ceiling. The operator adds rules as they learn what connections matter.

### Tier 2: Automatic Property Overlap (propose candidates)

The curator scans for nodes of different kinds that share 2+ identical non-trivial property values. These are proposed as `RelationshipCandidate` nodes (following the existing `OntologyCandidate` pattern) for operator review.

**How it works:**
1. For each orphan node (or node with low edge count), extract its non-trivial properties (exclude `source`, `_provenance_*`, empty strings)
2. Query the graph for other nodes (different kind) with any matching property value
3. If 2+ properties match, create a `RelationshipCandidate` node:
   ```
   kind: RelationshipCandidate
   label: "Device:gb-desktop ↔ DNSQuery:104.9.124.68:blocked.com"
   properties:
     from_id: <node_id>
     to_id: <node_id>
     matching_properties: "client_ip=104.9.124.68, source=nextdns"
     suggested_relation: "ASSOCIATED_WITH"
     confidence: 0.8
   ```
4. Operator reviews via `agency admin knowledge relationship candidates` (or web UI ontology candidates panel)
5. Promote → creates the edge. Reject → suppresses future proposals for this pair.

**Trivial properties excluded from matching:** `source`, `source_type`, `_provenance_connector`, `_provenance_work_item`, `_ontology_version`, `_migrated`, `_original_kind`, empty strings, `"true"`, `"false"`.

### Tier 3: Semantic Similarity (propose candidates)

When hybrid retrieval is enabled (sqlite-vec with an embedding provider), the curator checks orphan nodes against the graph via vector similarity. High-similarity pairs with no edge are proposed as `RelationshipCandidate` nodes.

**How it works:**
1. For each orphan node that has an embedding vector, call `find_similar(node_id, limit=5)`
2. If any result has similarity > 0.85 and no existing edge, propose as a candidate
3. Suggested relation: `SIMILAR_TO`

This tier is free when embeddings exist (no LLM calls) and catches semantic relationships that property overlap misses (e.g., a finding about "SSH brute force" and a Device with `service: openssh`).

### Candidate Lifecycle

Same as OntologyCandidate:
- Created by Tier 2/3 scans
- Listed via `agency admin knowledge relationship candidates`
- Promoted → edge created with `source_type: "inferred"`
- Rejected → candidate soft-deleted, pair suppressed for 30 days
- Web UI: same promote/reject buttons as ontology candidates

### Implementation

Add to `curator.py`:

```python
def relationship_inference(self) -> dict:
    """Three-tier relationship inference."""
    stats = {"tier1_created": 0, "tier2_proposed": 0, "tier3_proposed": 0}

    # Tier 1: explicit rules — auto-create edges
    rules = self._load_inference_rules()
    for rule in rules:
        stats["tier1_created"] += self._apply_inference_rule(rule)

    # Tier 2: property overlap — propose candidates
    stats["tier2_proposed"] += self._scan_property_overlap()

    # Tier 3: semantic similarity — propose candidates (if embeddings available)
    if self.store.vec_available:
        stats["tier3_proposed"] += self._scan_semantic_similarity()

    return stats
```

Add `"relationship_inference"` to `_run_cycle` operations list, after `emergence_scan`.

### CLI / MCP

- `agency admin knowledge relationship candidates` — list proposed relationships
- `agency admin knowledge relationship promote <id>` — create the edge
- `agency admin knowledge relationship reject <id>` — suppress the proposal
- MCP tool: `agency_admin_knowledge` with actions `relationship_candidates`, `relationship_promote`, `relationship_reject`

### Scope

**In scope:**
- Tier 1: explicit rules, auto-create edges
- Tier 2: property overlap scan, propose candidates
- Tier 3: semantic similarity scan, propose candidates (when embeddings available)
- Candidate lifecycle (promote/reject)
- CLI and MCP tool support
- Observe mode support (log without creating in observe mode)

**Not in scope:**
- LLM-based relationship inference (synthesizer's job)
- Automatic rule generation from promoted candidates (future)
- Cross-graph inference (comparing with external knowledge bases)
