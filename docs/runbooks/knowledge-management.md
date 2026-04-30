# Knowledge Management

> Status: Mixed operator runbook. Core graph query, stats, export, ingest, and
> retrieval workflows are part of the supported `0.2.x` path. Ontology,
> quarantine, communities, review, and broader governance flows are
> experimental.

## Trigger

Ingesting content into the knowledge graph, managing classification tiers, reviewing ontology, handling quarantine/release, or troubleshooting graph issues.

## Ingestion

### Ingest a file or URL

```bash
agency graph ingest /path/to/document.md
agency graph ingest https://example.com/docs/api-reference
```

The universal ingestion pipeline (`POST /api/v1/graph/ingest`) accepts any content type. `SourceClassifier` routes to deterministic extractors first (markdown, config, code, HTML, PDF, structured data). LLM synthesis only runs when the `MergeBuffer` decides it adds value beyond deterministic extraction.

### Watch mode

For automatic ingestion of files dropped into a directory:

```bash
agency graph ingest --watch /path/to/auto-ingest/
```

### Verify ingestion

```bash
agency graph stats
agency graph query "content from the ingested document"
```

## Classification

Four-tier access control for knowledge graph nodes:

| Tier | Description | Default Scope |
|------|------------|---------------|
| `public` | Visible to all principals | `*` |
| `internal` | Visible to org members | Operator-configurable |
| `restricted` | Need-to-know basis | Specific principals |
| `confidential` | Highest sensitivity | Named principals only |

### View current config

```bash
agency graph classification show
```

Config lives at `~/.agency/knowledge/classification.yaml`. Tier-to-principal mappings are operator-configurable. Classification is auto-applied at `add_node()` time via scope merge.

### Updating classification

Edit `~/.agency/knowledge/classification.yaml` to change tier→principal mappings, then reload:

```bash
agency infra reload
```

## Ontology _(experimental)_

The ontology defines valid entity types and relationship types in the knowledge graph.

### View ontology

```bash
agency graph ontology show          # Full merged ontology
agency graph ontology types         # Entity types with descriptions
agency graph ontology relationships # Relationship types
```

### Validate graph against ontology

```bash
agency graph ontology validate
```

Reports nodes with types not in the ontology.

### Promote/reject candidate types

Agents may create nodes with types not yet in the ontology. These appear as candidates:

```bash
agency admin knowledge ontology candidates       # List candidates
agency admin knowledge ontology promote <value>   # Add to ontology
agency admin knowledge ontology reject <value>    # Reject
```

### Migrate node types

If a type needs renaming:

```bash
agency graph ontology migrate <old-type> <new-type>
```

### Custom ontology extensions

Operator-defined ontology extensions go in `~/.agency/knowledge/ontology.d/`. The agentic memory types (procedure, episode) are defined in `~/.agency/knowledge/ontology.d/agentic-memory.yaml`.

Hub-managed ontology files are overwritten by `agency hub update`. Custom extensions in `ontology.d/` are preserved.

## Communities and Hubs _(experimental)_

Community detection (Louvain algorithm) runs in the curator cycle (every 6th cycle). Results are queryable:

```bash
agency graph query "list communities"
```

Via API:

- `GET /api/v1/graph/communities` — List all communities
- `GET /api/v1/graph/communities/{id}` — Show community detail
- `GET /api/v1/graph/hubs` — List hub nodes (high degree centrality)

Hub nodes are high-connectivity entities. Bridge nodes (high betweenness centrality) connect otherwise separate clusters. Both are useful for understanding graph structure.

## Edge Provenance

Every knowledge graph edge has a provenance tier:

| Tier | Source | Reliability |
|------|--------|------------|
| `EXTRACTED` | Deterministic parser | Highest |
| `INFERRED` | Curator inference | Medium |
| `AMBIGUOUS` | LLM synthesis | Lowest |

Use `min_provenance` filter on queries to control quality:

```bash
agency graph query "security findings" --min-provenance EXTRACTED
```

## Query Feedback Loop

Agents can save insights that become first-class knowledge:

```bash
agency graph insight "The API gateway rate-limits at 600 req/min per agent"
```

Creates a finding node with `DERIVED_FROM` edges (INFERRED provenance). Scope is the intersection of source node scopes (ASK Tenet 12).

## Quarantine _(experimental)_

When an agent's knowledge contributions are suspected compromised:

### Quarantine

```bash
# All contributions from an agent
agency admin graph quarantine --agent <agent-name>

# Only contributions after a specific time
agency admin graph quarantine --agent <agent-name> --since <timestamp>
```

Quarantined nodes are excluded from all retrieval immediately.

### List quarantined nodes

```bash
agency admin knowledge quarantine-list
```

### Release after investigation

```bash
# Release all nodes from an agent
agency admin knowledge quarantine-release --agent <agent-name>

# Release specific nodes (by node ID)
agency admin knowledge quarantine-release --node <node-id>
```

## Graph Export/Import

### Export

```bash
agency graph export /path/to/backup.json
```

### Restore

```bash
agency graph restore /path/to/backup.json
agency graph stats   # verify counts
```

## Curation

The knowledge service runs periodic curation cycles. View the curation log:

```bash
agency admin knowledge curate   # trigger manual curation
```

Via API: `GET /api/v1/graph/curation-log`

## Pending Reviews

Org-structural knowledge contributions may require operator review:

```bash
# List pending reviews
# Via API: GET /api/v1/graph/pending

# Approve or reject
# Via API: POST /api/v1/graph/review/{id}
```

## Durable Memory Lifecycle

Durable agent memory is graph-backed. Agents and the body runtime may submit
memory proposals, but the knowledge manager owns promotion into durable memory
and operators own review/revocation. This keeps preference, identity, and
knowledge boundaries visible and auditable.

Memory proposals use `kind=memory_proposal` and include:

- `memory_type`: `semantic`, `episodic`, or `procedural`
- `status`: `pending_review`, `needs_review`, `approved`, or `rejected`
- `confidence`, `reason`, `entities`, `evidence_message_ids`
- provenance fields such as `agent`, `task_id`, `channel`, and `participant`

Promoted durable memories are graph nodes with kind:

- `fact` for semantic memory
- `episode` for episodic memory
- `procedure` for procedural memory

The promoted node keeps `properties.promoted_from`, `memory_type`,
`approved_by`, evidence IDs, source channels, and agent/task provenance.

### Review proposals

```bash
# List proposals that require operator review
# Via API: GET /api/v1/graph/memory/proposals?status=needs_review&limit=100

# Approve or reject a proposal
# Via API: POST /api/v1/graph/memory/proposals/{id}/review
# Body: {"action":"approve","reason":"operator confirmed"}
# Body: {"action":"reject","reason":"not durable enough"}
```

High-confidence low-risk proposals may be approved by the knowledge manager.
Preference-affecting memories require review even when the proposed memory type
is procedural.

### List approved memories

```bash
# All durable memories
# Via API: GET /api/v1/graph/memory

# Filter by memory type or contributing agent
# Via API: GET /api/v1/graph/memory?type=procedural&agent=jarvis&limit=50
```

### Revoke durable memory

Revocation is a soft delete that leaves a curation audit trail and removes the
memory from retrieval.

```bash
# Via API: POST /api/v1/graph/memory/{id}/actions
# Body: {"action":"revoke","reason":"operator superseded this preference"}
```

The web UI exposes the same lifecycle in Knowledge administration:

- `Memory Review` lists proposals awaiting operator decision.
- `Durable Memory` lists promoted memories and exposes `Revoke`.

## GraphRAG Security

All knowledge graph content injected into agent system prompts is:

1. Wrapped in `[KNOWLEDGE_GRAPH_CONTEXT]` delimiters with node ID provenance
2. XPIA-scanned by the enforcer before reaching the agent
3. Scope-checked against the requesting agent's authorization

Cached results (`cached_result` nodes) are also XPIA-scanned before reuse.

## Troubleshooting

### Graph appears empty after ingestion

```bash
agency graph stats
```

If nodes/edges are 0 after ingestion:

1. Check knowledge service status: `agency infra status`
2. Check gateway logs for ingestion errors: `tail -50 ~/.agency/gateway.log`
3. Check if the file type is supported (markdown, config, code, HTML, PDF, structured data)

### Ontology validation failures

```bash
agency graph ontology validate
```

If nodes fail validation, either promote the candidate types or migrate them:

```bash
agency admin knowledge ontology candidates
agency admin knowledge ontology promote <type>
```

## Verification

- [ ] `agency graph stats` shows expected node/edge counts
- [ ] `agency graph query <text>` returns relevant results
- [ ] `agency graph ontology validate` reports no issues
- [ ] `agency graph classification show` shows correct tier config
- [ ] Quarantined nodes are excluded from queries

## See Also

- [Security Incident Response](security-incident-response.md) — quarantine during incidents
- [Backup & Restore](backup-restore.md) — graph export/import
- [Principal Management](principal-management.md) — scope and authorization
