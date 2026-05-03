# MicroVM Release Checklist

Use this checklist before treating a release as microVM-ready. This is the
runtime gate for the supported path: Agency on microagent. Microagent uses
Firecracker on Linux/WSL and Apple's Virtualization framework on macOS Apple
silicon.

## Scope

This checklist covers runtime execution, backend hygiene, lifecycle behavior,
and release-gate alignment. It does not validate legacy Docker, Podman,
containerd, or Apple Container execution backends.

OCI image definitions remain in scope only as artifact build inputs for
microVM root filesystems.

## Required Checks

- [ ] `agency setup --help` and `agency quickstart --help` mention only
      supported microVM backend selection.
- [ ] Linux/WSL release readiness defaults to microagent.
- [ ] macOS Apple silicon release readiness defaults to microagent.
- [ ] Container execution backend names are rejected by setup, quickstart, and
      configured gateway startup.
- [ ] Runtime supervisor rejects container execution backends for new runtime
      specs.
- [ ] `agency admin doctor` reports Firecracker KVM/vsock/kernel/helper issues
      clearly on Linux/WSL.
- [ ] Microagent live smoke uses versioned body and enforcer OCI artifacts.
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

Microagent release smoke:

```bash
./scripts/readiness/microvm-smoke.sh \
  --backend microagent \
  --rootfs-oci-ref ghcr.io/geoffbelknap/agency-runtime-body:v0.2.x \
  --enforcer-oci-ref ghcr.io/geoffbelknap/agency-runtime-enforcer:v0.2.x
```

The wrapper defaults to microagent on supported hosts. Use `--backend` only
when you are intentionally validating a direct backend adapter, `--skip-core`
to skip static/unit gates, and `--web` to include the backend Web UI smoke.

Backend-neutral runtime contract smoke:

```bash
bash ./scripts/readiness/runtime-contract-smoke.sh --agent <agent-name>
```

Direct Apple VF adapter validation on macOS Apple silicon:

```bash
./scripts/readiness/apple-vf-microvm-smoke.sh --skip-helper-build
./scripts/readiness/apple-vf-lifecycle-smoke.sh --skip-helper-build
```

Direct Firecracker adapter validation on Linux/WSL:

```bash
scripts/readiness/firecracker-artifacts.sh
scripts/readiness/firecracker-kernel-artifacts.sh
scripts/readiness/firecracker-artifacts.sh --verify-existing
scripts/readiness/firecracker-kernel-artifacts.sh --verify-existing
./scripts/readiness/firecracker-microvm-smoke.sh
agency admin doctor
```

To run the external runtime contract smoke against the same disposable direct
Firecracker agent, keep the smoke runtime alive and run the printed contract
smoke command from another shell:

```bash
./scripts/readiness/firecracker-microvm-smoke.sh --keep-agent
bash ./scripts/readiness/runtime-contract-smoke.sh --agent <printed-agent-name> --home <printed-agency-home> --start-gateway --skip-tests --skip-doctor
```

Default direct Firecracker artifact paths:

```text
$AGENCY_HOME/runtime/firecracker/artifacts/v1.12.1/firecracker-v1.12.1-x86_64
$AGENCY_HOME/runtime/firecracker/artifacts/vmlinux
```

For direct Firecracker adapter validation, the Firecracker binary must come
from the pinned upstream Firecracker release artifact. The kernel must come
from Agency's Linux build artifact pipeline as an uncompressed ELF `vmlinux`;
do not use a random host distro kernel. For the release path, the rootfs is
derived from an explicit, versioned OCI artifact reference such as
`ghcr.io/geoffbelknap/agency-runtime-body:v0.2.x` through microagent. The
enforcer runtime artifact is published separately as
`ghcr.io/geoffbelknap/agency-runtime-enforcer:v0.2.x`. Mutable `:latest`
runtime image tags are not release gates.

`scripts/readiness/firecracker-artifacts.sh` fetches and verifies only the
pinned upstream Firecracker binary. `scripts/readiness/firecracker-kernel-artifacts.sh`
builds the Agency Firecracker `vmlinux` artifact through Buildroot. Neither
script downloads a demo kernel or rootfs.

## Release Decision

Block the release if any required microVM backend cannot start a disposable
agent, cannot report runtime status, cannot validate mediation, or leaves stale
runtime state after stop/cleanup.

Do not block a microVM release on archived legacy container readiness checks.
