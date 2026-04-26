# Release Gates

This file defines what should block release or merge decisions. Dev-only
harnesses can provide evidence, but they are not gates unless listed here.

## Default Required Checks

```bash
go test ./...
./scripts/python-image-tests.sh
make web-test-all
./scripts/verify-required-status-checks.sh
```

Required CI/status checks should include:

- `go-test`
- `python-unit-test`
- `python-knowledge-test`
- `web-test`
- `docker-smoke`
- `podman-smoke`
- `containerd-smoke`

## Runtime Release Evidence

For runtime, lifecycle, body, enforcer, or adapter changes, add:

```bash
./scripts/runtime-contract-smoke.sh --agent <agent>
```

Use the matching backend readiness lane when the backend adapter changed.

## Web Release Evidence

For web route, API client, setup, or operator-flow changes, add:

```bash
./scripts/e2e-live-disposable.sh --skip-build
```

Use risky or danger lanes only when the changed surface requires them.

## Dev-Only Evidence

These are valuable for development and diagnosis, but not release gates by
default:

- `./scripts/dev-agent-loop-eval.sh --mode replay`
- `./scripts/dev-agent-loop-eval.sh --mode live --fixture <fixture>`
- `./scripts/e2e-live-danger-disposable.sh`
- Apple Container smoke validation

Promote any of these to a gate only after documenting environment dependencies,
runtime cost, cleanup semantics, and failure ownership.
