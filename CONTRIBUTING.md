# Contributing to Agency

Agency welcomes contributions. The project implements the [ASK framework](https://askframework.org) — all 27 tenets apply to every change.

## Ground rules

- **No tenet violations.** If a proposed change requires violating an ASK tenet, the change is wrong. Redesign the approach.
- **Enforcement is external.** Never move enforcement logic inside the agent boundary.
- **Fail closed.** If enforcement is unavailable, the agent cannot act. Don't add fallbacks that bypass this.

## How to contribute

1. Open an issue describing what you want to change and why.
2. Fork the repo and create a branch.
3. Make your changes. Run `make test` before submitting.
4. Open a pull request. Reference the issue.

## Branch and merge policy

- Keep `main` releasable. Short-lived feature branches are preferred over long-running divergence.
- Prefer `Rebase and merge` or merge commits for normal PRs. This preserves ancestry so local and CI branch cleanup can reliably use Git history.
- Avoid `Squash and merge` as the default. Use it only when a branch history is intentionally messy and you are explicitly trading ancestry for a cleaner single commit.
- Enable automatic branch deletion after merge in repository settings where possible.
- Before cutting a release, update local `main`, prune remotes, and delete local branches that are either:
  - already merged by ancestry, or
  - patch-equivalent to `main` and tied to a merged PR.
- If a PR branch is behind `main`, refresh it before merge so auto-merge and release triage stay predictable.

## Building and testing

```bash
# Build the Go binary and install to ~/.agency/bin/
make install

# Build all container images
make images

# Run Go tests
make test

# Run Python tests (body runtime, container images)
pytest tests/
```

## Code organization

- **Go gateway** (`agency-gateway/`) is the primary codebase. CLI, REST API, MCP server, orchestration.
- **Python** (`agency_core/`) contains container image sources, Pydantic models, and the body runtime.
- **Specs** (`docs/specs/`) are architectural reference. Read the relevant spec before modifying a subsystem.

## What makes a good contribution

- Bug fixes with a test that reproduces the issue.
- Security improvements that strengthen enforcement boundaries.
- Connector and pack contributions to the [Hub](https://github.com/geoffbelknap/agency-hub).
- Documentation fixes.

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 license.
