# Backend Adapter Release Checklist

Use this runbook before declaring a new runtime/host adapter ready for regular
use. It is the repeatable release-style gate for Docker, Podman, and future
backend-neutral runtime adapters.

## Trigger

- introducing a new host adapter
- changing runtime supervisor or backend-selection logic
- changing backend-neutral runtime contract behavior
- changing host-side artifact, lifecycle, signal, or network handling

## Exit Criteria

Do not mark an adapter ready until all of the following are true:

- the adapter passes the automated test gates
- the adapter passes the runtime contract gates
- the adapter passes lifecycle and operator control checks
- `agency admin doctor` distinguishes runtime failures from backend hygiene
- fail-closed behavior is preserved when backend capabilities are missing
- validation evidence is recorded for the adapter and build under test

## Test Identity

Record this before you start:

- adapter name:
- build under test:
- binary path used:
- Agency home used:
- host OS:
- runtime mode:
- operator:
- date:

Backend selection reminder:

- set `hub.deployment_backend` in `config.yaml` to the adapter under test
- for Podman, set `hub.deployment_backend_config.host` or `hub.deployment_backend_config.socket` if auto-detection is not enough
- for `containerd`, set `hub.deployment_backend_config.native_socket` or `hub.deployment_backend_config.address` to the native containerd socket when auto-detection is not enough
- for `containerd`, do not use generic `host` or `socket` keys; those remain Docker/Podman-shaped and are rejected to avoid mixing native and Docker-compatible sockets
- common Podman socket sources:
  `podman info --format json | jq -r '.host.remoteSocket.path'`
- current `containerd` slice is Linux-only and nerdctl-backed

Examples:

Rootless containerd:

```yaml
hub:
  deployment_backend: containerd
  deployment_backend_config:
    native_socket: /run/user/1000/containerd/containerd.sock
```

Rootful containerd:

```yaml
hub:
  deployment_backend: containerd
  deployment_backend_config:
    native_socket: /run/containerd/containerd.sock
```

Misconfiguration guard:

- do not point `containerd` at Docker-compatible API sockets such as `.../containerd-rootless/api.sock`
- if you do, Agency should fail fast with a native-socket error instead of silently degrading to an unsafe fallback

If you are validating a local patch, make sure the binary you are exercising is
the one you just built:

```bash
./agency --version
agency --version
agency status
go build ./cmd/gateway
```

## 1. Static Gates

- [ ] `go test ./...`
- [ ] `go build ./cmd/gateway`
- [ ] no production imports of Docker SDK packages outside `internal/hostadapter`
- [ ] no production imports of legacy backend-specific helper packages outside the host boundary

Suggested checks:

```bash
rg -n "github.com/docker|docker/go-connections" internal --glob '!internal/hostadapter/**' --glob '!**/*_test.go'
rg -n "internal/docker" internal --glob '!**/*_test.go'
```

Expected result:

- both searches return no production-code matches

## 2. Startup Gates

- [ ] gateway starts successfully with the adapter selected
- [ ] `curl -sf http://127.0.0.1:8200/api/v1/health`
- [ ] `agency infra status`
- [ ] `agency admin doctor`

Record:

- backend reported by doctor:
- startup warnings:
- backend hygiene warnings:

## 3. Runtime Contract Gates

Run the packaged smoke path first:

```bash
bash ./scripts/runtime-contract-smoke.sh --agent <agent-name>
```

Then validate the first-class runtime surfaces explicitly:

- [ ] `agency runtime manifest <agent>`
- [ ] `agency runtime status <agent>`
- [ ] `agency runtime validate <agent>`

API equivalents:

```bash
curl -fsS -H "Authorization: Bearer $(awk '/^token:[[:space:]]*/ {print $2; exit}' ~/.agency/config.yaml)" \
  http://127.0.0.1:8200/api/v1/agents/<agent>/runtime/manifest

curl -fsS -H "Authorization: Bearer $(awk '/^token:[[:space:]]*/ {print $2; exit}' ~/.agency/config.yaml)" \
  http://127.0.0.1:8200/api/v1/agents/<agent>/runtime/status

curl -fsS -X POST -H "Authorization: Bearer $(awk '/^token:[[:space:]]*/ {print $2; exit}' ~/.agency/config.yaml)" \
  http://127.0.0.1:8200/api/v1/agents/<agent>/runtime/validate
```

Expected result:

- manifest exists and includes backend + transport
- status reports the projected backend/runtime phase correctly
- validate succeeds when healthy and fails closed when unhealthy

## 4. Lifecycle Gates

Use a disposable agent name for this run:

- [ ] create agent
- [ ] start agent
- [ ] stop agent
- [ ] restart agent
- [ ] supervised halt
- [ ] resume
- [ ] delete agent

Suggested commands:

```bash
agency create <agent>
agency start <agent>
agency stop <agent>
agency restart <agent>
agency halt <agent> --tier supervised --reason "adapter validation"
agency resume <agent>
agency delete <agent>
```

Expected result:

- all lifecycle operations complete through the backend-neutral path
- restart re-enters the canonical startup flow
- halt/resume preserve fail-closed semantics
- cleanup removes runtime artifacts owned by the adapter

## 5. Operator and Comms Gates

- [ ] DM establishment works: `POST /api/v1/agents/{name}/dm`
- [ ] a single DM round-trip works without duplicate follow-on execution
- [ ] results access works
- [ ] trajectory access works
- [ ] config reload path works
- [ ] mission reload path works if missions are enabled

Minimum DM check:

```bash
agency send <agent> "Reply with exactly: adapter-check-ok"
agency comms read dm-<agent>
```

Expected result:

- exactly one correct reply appears
- no duplicate downstream task is emitted for the same DM

## 6. Doctor and Hygiene Gates

- [ ] `agency admin doctor` passes runtime checks for the adapter
- [ ] backend-specific hygiene warnings are classified as backend hygiene, not generic runtime failure
- [ ] doctor still fails closed on actual runtime contract failures

Record separately:

- runtime check failures:
- backend hygiene warnings:

## 7. Capability Degradation Gates

Force at least one missing-capability case for the adapter and verify the
platform degrades correctly.

Examples:

- no internal-network support
- no signal delivery
- no health inspection
- no published-port support

Verify:

- [ ] the system returns explicit backend/capability errors
- [ ] the system does not silently fall back to unsafe behavior
- [ ] the runtime contract remains authoritative

## 8. Disposable Live E2E

Run the disposable live path after runtime or lifecycle changes:

```bash
./scripts/e2e-live-disposable.sh --skip-build
```

- [ ] live disposable flow passes
- [ ] cleanup leaves no leaked adapter-managed runtime artifacts

Release gate policy:

- Docker, Podman, and `containerd` smoke must stay automated
- the `containerd` smoke lane is automated on Linux against a native containerd socket via `nerdctl`
- `main` branch protection should require the per-PR smoke checks:
  `go-test`, `python-unit-test`, `python-knowledge-test`, `web-test`, `docker-smoke`, `podman-smoke`, and `containerd-smoke`
- do not enable auto-merge for adapter work unless the smoke lanes above are enforced as required checks
- Docker smoke local equivalent:

```bash
make docker-readiness
```

- full Podman disposable E2E is a release requirement, not a per-PR requirement
- the supported manual CI path is:

```bash
gh workflow run "Podman Readiness" --ref main -f full_e2e=true
```

- local equivalent:

```bash
make podman-readiness-full
```

If the backend has a dedicated readiness or cleanup script, run it here and
record the result.

Current automated `containerd` rootless smoke:

```bash
make containerd-readiness
```

Manual rootful `containerd` release gate:

```bash
make containerd-readiness-rootful
gh workflow run "Containerd Rootful Readiness" --ref main
```

- the rootful lane is Linux-only and manual by design
- it expects a self-hosted Linux runner or host with `nerdctl` and a usable native rootful socket at `/run/containerd/containerd.sock`
- this lane validates the native rootful path, not a Docker-compatible API socket

## 9. Recovery Gates

- [ ] restart gateway and confirm runtime manifests remain authoritative
- [ ] `agency admin doctor` still reports correct adapter/runtime state after restart
- [ ] agent can be resumed or restarted after gateway restart
- [ ] no orphaned runtime artifacts remain after teardown

## 10. Evidence

Capture this in the release note, PR description, or adapter validation record:

- adapter name and build tested
- exact commands run
- pass/fail result for each section
- known limitations
- capability gaps accepted for this release
- follow-up issues required before broader rollout

### Podman Validation Record

Use this as the current known-good reference until superseded by a newer run.

- adapter name: `podman`
- host OS: `macOS (Darwin arm64)`
- container runtime: `Podman 5.8.2`
- socket used: `unix:///Users/geoffbelknap/.local/share/containers/podman/machine/podman.sock`
- backend config:

```yaml
hub:
  deployment_backend: podman
  deployment_backend_config:
    host: unix:///Users/geoffbelknap/.local/share/containers/podman/machine/podman.sock
```

- binary path used: `./agency`
- validation date: `2026-04-17`

Validated commands:

```bash
go test ./...
go build ./cmd/gateway
podman info --format json
bash ./scripts/runtime-contract-smoke.sh --agent <agent-name>
AGENCY_SOURCE_HOME=/tmp/agency-podman-seed.<id> ./scripts/e2e-live-disposable.sh --skip-build
gh workflow run "Podman Readiness" --ref main -f full_e2e=true
```

Repeatable automation lane:

```bash
make podman-readiness       # smoke + disposable runtime contract path
make podman-readiness-full  # smoke + full disposable live E2E
```

Recorded results:

- runtime smoke: passed
- disposable live E2E: passed
- final disposable live result: `22 passed (2.3m)`
- cleanup: passed with Podman-aware scoped cleanup and no fallback Docker socket failure

Notes:

- on this machine, Playwright browser validation must be run outside the Codex app sandbox because bundled Chromium may fail there with a macOS `mach_port_rendezvous` permission error
- that browser-launch issue is environment-specific and not a Podman backend failure

## Minimum Ship Gate

An adapter is not ready to ship unless all of these are true:

- [ ] static gates passed
- [ ] startup gates passed
- [ ] runtime contract gates passed
- [ ] lifecycle gates passed
- [ ] operator/comms gates passed
- [ ] doctor/hygiene gates passed
- [ ] capability degradation gates passed
- [ ] disposable live E2E passed
- [ ] recovery gates passed
- [ ] evidence captured

## Notes

- Use [Runtime Smoke](runtime-smoke.md) for the focused runtime-contract path.
- Use [Validation Checklist](validation-checklist.md) for broader operator and platform validation.
- Use this runbook as the adapter-specific release gate that ties those checks together.
