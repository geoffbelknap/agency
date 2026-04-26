# System Tests

This directory no longer contains an executable system-test runner. The former
Python MCP-handler system suite has been retired.

Current validation entry points:

- Unit and package tests: `go test ./...`
- Python image lanes: `./scripts/dev/python-image-tests.sh`
- Web mocked browser tests: `make web-test-all`
- Live disposable web tests: `./scripts/e2e/e2e-live-disposable.sh --skip-build`
- Runtime contract smoke: `./scripts/readiness/runtime-contract-smoke.sh --agent <agent>`
- Manual/operator validation map: `images/tests/validation/`

Do not add new system-test documentation here unless a real runner is restored.
For new validation coverage, prefer a focused Go/Python/web test or one of the
explicit live harnesses documented in `images/tests/validation/`.
