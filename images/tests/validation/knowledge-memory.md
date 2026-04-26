# Knowledge And Memory Validation

Use this lane for graph memory, knowledge review, proposal handling, ontology,
and knowledge search.

## Required Surfaces

Durable memory is graph-backed and operator-owned:

- `GET /api/v1/graph/memory`
- `POST /api/v1/graph/memory/{id}/actions`
- `GET /api/v1/graph/memory/proposals`
- `POST /api/v1/graph/memory/proposals/{id}/review`

Web coverage lives in the Knowledge screen memory review flows.

## Automated Lane

```bash
go test ./internal/api/graph ./internal/knowledge ./internal/models
./scripts/dev/python-image-tests.sh knowledge
make web-test-unit
```

Use the focused package set when a full Python lane is unnecessary.

## Manual Checks

Expected observations:

- Graph memory list returns promoted durable memories only.
- Proposal list returns pending proposals requiring review.
- Approving a proposal promotes it through the gateway/knowledge manager.
- Rejecting a proposal prevents direct mutation.
- Revoking promoted memory changes lifecycle state without deleting audit
  history.
- Preference-affecting memory requires review even when classified as
  procedural.

## Prohibited Validation Pattern

Do not validate durable memory by asking an agent to directly save preferences
and then reading local files. That bypasses the mediated ownership model this
surface is designed to preserve.
