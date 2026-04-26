# Durable Memory Lifecycle

## Status

Experimental graph governance surface. The API is documented in
`internal/api/openapi.yaml`; it is intentionally not part of the default
`openapi-core.yaml` view.

## Purpose

Agency agents need durable memory that survives individual conversations, but
memory is also a trust boundary. Agent-authored memory can shape future
behavior, so durable memory must remain mediated, auditable, and recoverable.

This lifecycle separates three concerns:

- agents and body runtimes propose candidate memories
- the knowledge manager evaluates proposals and promotes safe memories
- operators review preference-affecting or ambiguous proposals and can revoke
  promoted memory

## ASK Constraints

- Agents do not directly write promoted durable memory.
- Promotion and revocation are external to the agent boundary.
- All decisions are represented as graph state plus curation log entries.
- Preference, identity, and instruction-like memories require operator review.
- Retrieval sees only active promoted memories; revoked memories are soft-deleted
  for audit and recovery.

## Data Model

### Memory Proposal

Proposal nodes use `kind=memory_proposal`.

Required properties:

- `status`: `pending_review`, `needs_review`, `approved`, or `rejected`
- `memory_type`: `semantic`, `episodic`, or `procedural`
- `confidence`: `low`, `medium`, or `high`
- `reason`: why the candidate should be remembered
- `agent`, `task_id`, `channel`, `participant`
- `evidence_message_ids`

Decision properties:

- `decision_reason`
- `promoted_node_id` when approved

### Promoted Memory

Promoted memories are normal graph nodes:

- semantic memory -> `kind=fact`
- episodic memory -> `kind=episode`
- procedural memory -> `kind=procedure`

Promoted properties include:

- `memory_type`
- `promoted_from`
- `promotion_reason`
- `confidence`
- `evidence_message_ids`
- `entities`
- `agent`, `task_id`, `channel`, `participant`
- `approved_by`

## Evaluation Rules

The knowledge manager rejects:

- empty summaries
- invalid `memory_type`
- secret-like material

The knowledge manager requires operator review for:

- non-high-confidence proposals
- preference, identity, persona, system prompt, or instruction-affecting memory

The knowledge manager may auto-approve:

- high-confidence, low-risk memories that do not affect identity or preferences

## API Surface

All routes are under `/api/v1` and are operator-only.

### List Promoted Memory

`GET /graph/memory`

Query parameters:

- `type`: optional `semantic`, `episodic`, or `procedural`
- `agent`: optional contributing agent name
- `limit`: optional 1-250, default 100

Returns:

```json
{
  "items": [
    {
      "id": "dbaf8fdcc586",
      "kind": "procedure",
      "summary": "When _operator asks about SEC filings, prioritize primary filings from SEC EDGAR over summaries or secondary writeups",
      "properties": "{\"memory_type\":\"procedural\",\"approved_by\":\"knowledge_manager\"}"
    }
  ]
}
```

### Apply Promoted Memory Action

`POST /graph/memory/{id}/actions`

Request:

```json
{
  "action": "revoke",
  "reason": "operator superseded this preference"
}
```

Revocation soft-deletes the promoted memory and writes a `memory_revoked`
curation event.

### List Proposals

`GET /graph/memory/proposals`

Query parameters:

- `status`: `pending_review`, `needs_review`, `approved`, or `rejected`
- `limit`: optional 1-250, default 100

### Review Proposal

`POST /graph/memory/proposals/{id}/review`

Request:

```json
{
  "action": "approve",
  "reason": "operator confirmed"
}
```

or:

```json
{
  "action": "reject",
  "reason": "not durable enough"
}
```

## UI Surface

The Knowledge administration screen exposes:

- `Memory Review`: proposals in `needs_review`
- `Durable Memory`: promoted memory with provenance and `Revoke`

The browser is a REST client only. It must not infer approval, promote memory,
or hide preference-affecting review requirements.

## Operational Notes

For local development, use helper scripts that read the gateway token from
`~/.agency/config.yaml`; do not put tokens or token-bearing environment values
on the command line.

Examples:

```bash
scripts/dev-api-get.sh --gateway 'graph/memory?type=procedural&agent=jarvis'
scripts/dev-api-get.sh --gateway 'graph/memory/proposals?status=needs_review'
```

