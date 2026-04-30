# `0.2.x` Core Release Checklist

Use this runbook as the procedural validation walk for cutting any `0.2.x`
core release. It pairs with [Release Gates 0.2.x](release-gates-0.2.x.md)
which carries the gate-level definitions.

Related:

- [Release Gates 0.2.x](release-gates-0.2.x.md)
- [MicroVM Release Checklist](microvm-release-checklist.md)

## Goal

Ship an installable `0.2.x` release that early users can:

- install through the supported release path
- complete setup with one provider
- bring the local stack up safely
- create and run an agent
- use the DM workflow
- see core graph, audit, and usage behavior through the supported product
  surfaces

## What `0.2.x` Means

`0.2.x` is not a patch line on top of the older broad alpha story.

It is the first intentionally scoped core Agency line:

- governed single-agent runtime
- microVM-only runtime execution: Firecracker on Linux/WSL and
  `apple-vf-microvm` on macOS Apple silicon
- event-driven execution
- graph-backed context in a trimmed form
- auditable provider-backed work
- honest default docs, CLI, web, OpenAPI, and MCP surfaces

## Release Prerequisites

Before cutting a `v0.2.x` tag:

- confirm `main` reflects the current core tiering and release-gate decisions
- confirm no known blocker remains in:
  - setup path
  - microVM backend readiness
  - agent create/start/show/stop
  - DM path
  - provider setup and execution
  - audit/usage visibility
  - graph query/retrieval/stats core
- confirm Homebrew publishing access still works
- confirm GHCR publishing still works from the release workflow
- confirm default product surfaces still hide experimental areas unless
  explicitly enabled

## Required Release Validation

### 1. Core build and version validation

Validate before tag:

- `go build ./cmd/gateway`
- `./agency --version`
- `./scripts/ci/verify-required-status-checks.sh`
- complete [MicroVM Release Checklist](microvm-release-checklist.md)
- `./scripts/release/release-readiness-check.sh preflight --version 0.2.x`

Validate after tag:

- GitHub release exists with expected archives
- checksums file is present
- macOS arm64 archive downloads and runs

### 2. Install and setup validation

Validate after tag/release:

- formula updated in `geoffbelknap/homebrew-tap`
- on a clean shell or machine:
  - `brew tap geoffbelknap/tap`
  - `brew install agency`
  - `agency --version`
  - `agency setup`
- smoke:
  - configure one recommended provider
  - `agency -q infra up`
  - `agency admin doctor`
  - confirm the selected backend is Firecracker on Linux/WSL or
    `apple-vf-microvm` on macOS Apple silicon

### 3. Core product smoke

Run on the tagged release path:

- create a `researcher` or `generalist` agent
- start the agent
- send one DM task
- confirm reply
- inspect:
  - `agency show <name>`
  - `agency admin usage --agent <name>`
  - `agency log <name>`
- confirm graph query or stats return usable state

### 4. Core web/API/MCP smoke

Validate after the local stack is up:

- open web UI
- confirm:
  - Setup
  - Overview
  - Agents
  - DM or channel path used for direct agent work
  - basic activity/audit visibility
- confirm experimental nav remains hidden by default
- confirm `/api/v1/openapi-core.yaml` is served
- confirm `/api/v1/mcp/tools` defaults to the core discovery view

### 5. MicroVM runtime smoke

Validate on the supported runtime path:

- `bash ./scripts/readiness/runtime-contract-smoke.sh --agent <agent-name>`
- on macOS Apple silicon, when validating Apple VF changes:
  - `./scripts/readiness/apple-vf-microvm-smoke.sh --skip-helper-build`
  - `./scripts/readiness/apple-vf-lifecycle-smoke.sh --skip-helper-build`
- on Linux/WSL, when validating Firecracker changes:
  - `agency admin doctor`
  - one disposable Firecracker-backed agent start/status/validate/stop cycle

### 6. GHCR runtime artifact validation

Validate after tag/release:

- `ghcr.io/geoffbelknap/agency-runtime-body:v0.2.x` is publicly pullable
- `ghcr.io/geoffbelknap/agency-runtime-enforcer:v0.2.x` is publicly pullable
- amd64 and arm64 manifests exist for both runtime artifacts
- mutable `latest` tags are not required and must not be release gates
- image metadata/build IDs are present and non-unknown

## Known Follow-Up Items That Should Not Block `0.2.x` Unless They Regress

- missions, teams, and coordinator UX
- packs, hub, and package lifecycle polish
- connector breadth, Slack, and Drive productization
- graph governance, ontology, and review tooling
- routing optimizer workflows
- relay-hosted distribution path

Those areas can continue behind experimental or internal boundaries without
blocking the first core release line.

## Recommended Release Sequence

1. Re-run the core readiness scripts and focused live checks.
2. Complete the microVM release checklist.
3. Confirm docs, CLI help, web nav, OpenAPI, and MCP still agree on the core
   contract.
4. Cut a release branch or release commit only if needed.
5. Tag `v0.2.x`.
6. Watch the GitHub release and runtime artifact publish workflows.
7. Validate one clean Homebrew install path.
8. Validate one full core product smoke on the tagged build.
9. Publish tester instructions centered on the core path only.

## Suggested Tester Path

Assuming release artifacts succeed:

```bash
brew tap geoffbelknap/tap
brew install agency
agency setup
agency -q infra up
agency create my-researcher --preset researcher
agency -q start my-researcher
agency send my-researcher "Research and summarize: <topic>"
```

## Open Questions Before Tag

- Which provider should be the default recommendation for first `0.2.x` users?
- Do we require one clean-machine Homebrew smoke or two before the first
  `v0.2.0` tag?
- Do we want an explicit release note section listing the major surfaces that
  are still experimental by design?
