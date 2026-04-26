# Validation Runbooks

These runbooks are operator validation guides, not a shadow test suite. Use
them to decide which existing automated lane or manual probe is appropriate for
a change.

The Go gateway is the source of truth. Validate through the REST/CLI/web
surfaces that the gateway exposes. MCP validation is useful only when the MCP
tool itself is the surface under test.

## Taxonomy

| Lane | Primary runner | When to use |
|------|----------------|-------------|
| Release gates | [release-gates.md](release-gates.md) | Before merging release-blocking or broad platform changes |
| Smoke | [smoke.md](smoke.md) | Quick local sanity check after setup, upgrade, or small patches |
| Runtime contract | [runtime-contract.md](runtime-contract.md) | Agent lifecycle, runtime supervisor, transport, backend-neutral health |
| ASK security | [ask-security.md](ask-security.md) | Enforcement, mediation, audit, credential, policy, and memory boundary changes |
| Backend adapters | [backend-adapters.md](backend-adapters.md) | Docker, Podman, containerd, or Apple Container adapter work |
| Comms and DM | [comms-and-dm.md](comms-and-dm.md) | Channels, direct messages, event delivery, agent response loops |
| Knowledge and memory | [knowledge-memory.md](knowledge-memory.md) | Graph-backed memory, proposals, review, revocation, knowledge graph |
| Policy and governance | [policy-governance.md](policy-governance.md) | Policy resolution, trust, principals, teams, authority |
| Hub and integration | [hub-and-integration.md](hub-and-integration.md) | Hub catalog, connectors, packs, intake, provider tools |
| Web live | [web-live.md](web-live.md) | Web UI routes, live browser flows, setup wizard, destructive UI guardrails |
| Model and schema | [model-schema.md](model-schema.md) | Go model validation, OpenAPI drift, strict schema behavior |

## Defaults

For most PRs, run the smallest lane that covers the changed behavior.

Recommended baseline before a normal PR:

```bash
go test ./...
./scripts/dev/python-image-tests.sh
make web-test-all
```

Recommended baseline before a runtime or lifecycle PR:

```bash
go test ./...
./scripts/dev/python-image-tests.sh body
./scripts/readiness/runtime-contract-smoke.sh --agent <agent>
```

Recommended baseline before a web or operator-flow PR:

```bash
make web-test-all
./scripts/e2e/e2e-live-disposable.sh --skip-build
```

## Hard Rules

- Do not treat Docker-specific probes as generic runtime failures. Runtime
  health is judged through manifest, status, validate, and `agency admin doctor`.
- Do not make dev-only harnesses branch-protection or release gates without
  explicitly documenting their environment assumptions.
- Do not validate durable memory by direct file edits. Durable memory is
  graph-backed and operator-owned.
- Do not validate preference-affecting memory without an operator review step.
- Do not weaken ASK boundaries to make tests easier.

## Cleanup

For live local runs, prefer disposable homes and explicit cleanup:

```bash
./scripts/dev/cleanup-live-test-runtimes.sh
```

Use `--apply` only when you intend to stop matched leaked test runtimes:

```bash
AGENCY_BIN=./agency ./scripts/dev/cleanup-live-test-runtimes.sh --apply
```
