# Runtime Contract Validation

Use this lane for changes to lifecycle, runtime supervisor, host adapters,
agent transport, body/enforcer startup, or `agency admin doctor`.

## Automated Lane

```bash
go test ./...
./scripts/dev/python-image-tests.sh body
./scripts/readiness/runtime-contract-smoke.sh --agent <agent>
```

For local live behavior that includes UI and DM response paths:

```bash
./scripts/e2e/e2e-live-disposable.sh --skip-build
```

## Contract Surfaces

Primary checks:

```bash
agency runtime manifest <agent>
agency runtime status <agent>
agency runtime validate <agent>
agency admin doctor
```

REST equivalents:

- `GET /api/v1/agents/{name}/runtime/manifest`
- `GET /api/v1/agents/{name}/runtime/status`
- `POST /api/v1/agents/{name}/runtime/validate`

Expected:

- Manifest persists after start.
- Status reports the selected backend and healthy/running state.
- Validate is fail-closed and returns a clear failure if the manifest, backend,
  transport, or mediation contract is broken.
- Doctor separates runtime failures from backend-specific hygiene warnings.

## Manual Lifecycle Probe

```bash
agency create runtime-check --preset generalist
agency start runtime-check
agency runtime manifest runtime-check
agency runtime status runtime-check
agency runtime validate runtime-check
agency restart runtime-check
agency runtime validate runtime-check
agency halt runtime-check --tier supervised --reason "runtime validation"
agency resume runtime-check
agency stop runtime-check
agency delete runtime-check
```

Expected:

- Restart re-enters the canonical startup flow.
- Runtime identity, mediation, and scoped transport remain intact.
- Halt requires an explicit reason at the appropriate tier.

## Out Of Scope

- Backend-native container shape. Use [backend-adapters.md](backend-adapters.md).
- Web browser behavior. Use [web-live.md](web-live.md).
- Agent answer quality. Use the dev agent-loop eval harness.
