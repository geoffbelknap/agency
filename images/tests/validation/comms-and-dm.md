# Comms And DM Validation

Use this lane for channels, message delivery, direct-message establishment,
event subscriptions, body event handling, and agent response loops.

## Automated Lane

```bash
go test ./internal/events ./internal/api ./internal/orchestrate
./scripts/dev/python-image-tests.sh body
./scripts/dev/dev-agent-loop-eval.sh --mode replay
```

For live answer behavior:

```bash
./scripts/dev/dev-agent-loop-eval.sh --mode live --fixture current_info_terminates_after_retry
```

The agent-loop eval harness is dev-only. It should expose response text,
diagnosis, signals, and result JSON, but it is not a shipped runtime feature.

## DM Contract

Primary backend surface:

- `POST /api/v1/agents/{name}/dm`

Expected:

- DM establishment is performed by the gateway.
- UI and tools do not reconstruct DM channel state ad hoc.
- A delivered DM task produces at most one actionable runtime task.
- The agent response appears in the DM channel and is visible to the web UI.

## Manual Probe

```bash
agency create dm-check --preset generalist
agency start dm-check
agency runtime validate dm-check
agency send dm-check "Reply with exactly: dm-check-ok"
agency comms read dm-dm-check --limit 10
agency delete dm-check
```

Expected:

- One visible agent reply.
- No duplicate follow-on task for the same operator message.
- Audit/signals show task seen, accepted, model request, and terminal outcome.

## Web Visibility

When web UI display changed, pair this lane with [web-live.md](web-live.md).
The browser must show the actual task, response, and result context; a score or
status alone is not enough.
