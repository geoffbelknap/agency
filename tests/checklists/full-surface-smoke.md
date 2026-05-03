# Full Surface Smoke

Use this before an RC when the question is broader than “does the runtime
boot?” The goal is to prove that the supported Agency surfaces still line up:
CLI, MCP, REST-backed Web UI, and the microVM runtime path.

Destroy/wipe is out of scope for this pass. Ordinary delete, remove, archive,
teardown, and cleanup flows are in scope when they operate on disposable test
data.

## One Command

```bash
./scripts/readiness/full-surface-smoke.sh \
  --backend microagent \
  --rootfs-oci-ref ghcr.io/geoffbelknap/agency-runtime-body:v0.2.x \
  --enforcer-oci-ref ghcr.io/geoffbelknap/agency-runtime-enforcer:v0.2.x \
  --include-risky-web
```

Use `--skip-build` when the local `./agency` binary and images already match
the commit being tested.

## What It Covers

| Surface | Coverage |
|---------|----------|
| CLI | Command registration and help for supported read, create, start, stop, restart, send, grant/revoke, comms, infra, admin, context, policy, runtime, authz, cap, team, mission, event, webhook, notify, audit, creds, registry, package, instance, hub, deployment, intake, graph, and server commands. |
| MCP | Go tests for MCP registry, discovery, call handling, tier filtering, auth, and registered tool behavior. |
| Runtime | `microvm-smoke.sh` on the `microagent` backend, with the versioned body/rootfs and enforcer OCI artifacts. |
| Web UI | Live disposable Web UI smoke with destructive feature tests filtered out. `--include-risky-web` adds risky flows while keeping the same filter. |

## Pass Criteria

- Static checks pass: `git diff --check`, `go test ./...`, and Web unit tests.
- CLI help resolves for every listed command.
- MCP discovery/call tests pass.
- The `microagent` backend completes the lifecycle and runtime-contract smoke.
- Web UI route and operator-flow tests pass in a disposable home, with only
  destroy/wipe feature assertions excluded.

## If It Fails

Classify the failure before fixing it:

- CLI registration/help mismatch
- MCP registry, auth, or tiering mismatch
- microagent runtime/artifact failure
- Web UI route, API contract, or live-flow failure
- test harness cleanup failure

Do not waive a failure by removing the feature from the smoke list unless that
feature has also been moved out of the supported release surface.
