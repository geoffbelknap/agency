# MicroVM Release Checklist

Use this checklist before treating a release as microVM-ready. This is the
runtime gate for the supported path: Firecracker on Linux/WSL and
`apple-vf-microvm` on macOS Apple silicon.

## Scope

This checklist covers runtime execution, backend hygiene, lifecycle behavior,
and release-gate alignment. It does not validate legacy Docker, Podman,
containerd, or Apple Container execution backends.

OCI image definitions remain in scope only as artifact build inputs for
microVM root filesystems.

## Required Checks

- [ ] `agency setup --help` and `agency quickstart --help` mention only
      supported microVM backend selection.
- [ ] Linux/WSL default backend is Firecracker.
- [ ] macOS Apple silicon default backend is `apple-vf-microvm`.
- [ ] Container execution backend names are rejected by setup, quickstart, and
      configured gateway startup.
- [ ] Runtime supervisor rejects container execution backends for new runtime
      specs.
- [ ] `agency admin doctor` reports Firecracker KVM/vsock/kernel/helper issues
      clearly on Linux/WSL.
- [ ] `agency admin doctor` reports Apple VF helper/kernel/rootfs tool issues
      clearly on macOS Apple silicon.
- [ ] Runtime manifest, status, and validate endpoints work for a disposable
      microVM-backed agent.
- [ ] Start, stop, restart, and cleanup preserve fail-closed behavior and useful
      failure status.
- [ ] Required GitHub status checks do not include legacy container backend
      smokes.
- [ ] Release docs and runbooks point to microVM validation paths, not
      container readiness paths.

## Validation Commands

Core checks:

```bash
git diff --check
go test ./...
npm --prefix web test
./scripts/ci/verify-required-status-checks.sh
go run ./cmd/gateway setup --help
go run ./cmd/gateway quickstart --help
```

Runtime contract:

```bash
bash ./scripts/readiness/runtime-contract-smoke.sh --agent <agent-name>
```

Apple VF live validation on macOS Apple silicon:

```bash
./scripts/readiness/apple-vf-microvm-smoke.sh --skip-helper-build
./scripts/readiness/apple-vf-lifecycle-smoke.sh --skip-helper-build
```

Firecracker live validation on Linux/WSL:

```bash
bash ./scripts/readiness/runtime-contract-smoke.sh --agent <firecracker-agent>
agency admin doctor
```

## Release Decision

Block the release if any required microVM backend cannot start a disposable
agent, cannot report runtime status, cannot validate mediation, or leaves stale
runtime state after stop/cleanup.

Do not block a microVM release on archived legacy container readiness checks.
