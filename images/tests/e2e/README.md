# E2E Integration Tests

This directory is kept as the cross-repo pointer for end-to-end validation.
The old Python `test_e2e_*.py` suite has been retired; current browser E2E
coverage lives under `web/tests/` and is run through the repo-root scripts.

## Current Tiers

| Tier | Location | Runner | Purpose |
|------|----------|--------|---------|
| Mocked browser | `web/tests/e2e/` | `make web-test-e2e` | Deterministic route and browser smoke coverage with mocked API state |
| Live safe | `web/tests/e2e-live/` | `./scripts/e2e-live-disposable.sh --skip-build` | Real local stack coverage with temporary data and cleanup |
| Live risky | `web/tests/e2e-live-risky/` | `./scripts/e2e-live-disposable.sh --skip-build --risky` | Opt-in flows that mutate shared state more heavily |
| Live danger | `web/tests/e2e-live-danger/` | `./scripts/e2e-live-danger-disposable.sh` | Explicit destructive-flow validation against disposable state |

See `web/tests/COVERAGE_TIERS.md` for the live-safe, live-risky, and
live-danger classification rules and current coverage inventory.

## Runtime Assumptions

E2E validation should reason through Agency's runtime contract rather than
raw Docker assumptions:

- `agency admin doctor`
- `agency runtime manifest <agent>`
- `agency runtime status <agent>`
- `agency runtime validate <agent>`
- `./scripts/runtime-contract-smoke.sh --agent <agent>`

Backend-specific lanes are still useful, but they validate adapter hygiene:

- `./scripts/docker-readiness-check.sh`
- `./scripts/podman-readiness-check.sh`
- `./scripts/containerd-rootless-readiness-check.sh`
- `./scripts/containerd-rootful-readiness-check.sh`

Do not add new default E2E checks that require one specific backend unless the
test is explicitly scoped to that backend.

## Dev-Only Harnesses

Local live harnesses are development tools. They are not shipped in runtime
images, not required by release artifacts, and should not become branch
protection gates unless the environment dependencies are made explicit.

Useful dev harnesses:

- `./scripts/dev-agent-loop-eval.sh --mode replay`
- `./scripts/dev-agent-loop-eval.sh --mode live --fixture <fixture>`
- `./scripts/e2e-live-disposable.sh --skip-build`
- `./scripts/cleanup-live-test-runtimes.sh`
