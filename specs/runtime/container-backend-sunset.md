# Container Backend Sunset

## Status

Draft. Strategic pivot recorded 2026-04-29. Agency is now microVM-first.
Docker, Podman, and containerd runtime backends are transition scaffolding, not
supported product targets.

## Decision

Agency should sunset container runtime execution paths before a public user
base depends on them.

Strategic runtime targets:

- Linux production: `firecracker`
- macOS local development: `apple-vf-microvm`
- Windows local development: WSL2 nested Linux runtime, expected to use
  Firecracker where nested virtualization is available

Transitional paths:

- `docker`
- `podman`
- `containerd`
- `apple-container`

The transitional paths may remain in-tree while they reduce implementation
risk, but new runtime architecture should not optimize for them and new
generic contracts should not be shaped by them.

## What Stays

OCI images stay. Dockerfiles stay as build recipes until replaced by a better
OCI build pipeline.

Agency should continue to use OCI artifacts for:

- agent body images
- enforcer images
- shared service images
- reproducible rootfs realization
- signing, provenance, scanning, and registry distribution

The sunset is about container **runtime execution**, not the OCI image format.

## Why Sunset Now

There is no installed user base to migrate. Keeping Docker/Podman/containerd
as supported runtime paths would force Agency's backend-neutral contracts to
keep carrying container-shaped compromises.

The microVM architecture gives Agency the boundary it actually wants:

- external enforcement remains outside the agent workload boundary
- mediation remains complete
- audit remains complete
- least privilege is explicit per VM/component
- runtime boundaries are visible and recoverable

Container backends are still useful as test scaffolding and image tooling
while Firecracker and `apple-vf-microvm` finish, but they should no longer be
treated as product destinations.

## Sunset Scope

In scope:

- Docker runtime backend
- Podman runtime backend
- containerd runtime backend
- container lifecycle helpers used only for runtime execution
- container-specific readiness scripts
- Docker/Podman/containerd backend selection and setup guidance
- Docker-shaped runtime status, doctor, and MCP wording
- container-specific infra orchestration when equivalent host-service paths
  exist

Out of scope:

- OCI image artifacts
- Dockerfiles as current OCI build recipes
- image tests that validate Agency images
- temporary Podman use inside rootfs/image-store implementation, until
  replaced by daemonless OCI tooling
- `apple-container` compatibility evaluation, until `apple-vf-microvm`
  reaches parity and a deletion decision is made

## Current Inventory

Major container runtime surfaces:

- `cmd/gateway/main.go` backend selection, Docker network-pool tuning,
  container client wiring, auto-restore
- `cmd/gateway/quickstart.go` container backend setup language
- `cmd/gateway/backend_install.go` Podman install guidance
- `internal/hostadapter/runtimehost/` Docker-compatible raw client,
  Docker/Podman/containerd/Apple Container probes, events, and state mapping
- `internal/hostadapter/runtimebackend/container.go`
- `internal/hostadapter/container.go`
- `internal/hostadapter/containerops/`
- `internal/orchestrate/containers/`
- `internal/orchestrate/infra*.go` containerized shared infra orchestration
- `internal/hostadapter/agentruntime/` container enforcer/workspace creation
- `internal/hostadapter/imageops/` Docker-compatible image resolver
- `scripts/readiness/docker-readiness-check.sh`
- `scripts/readiness/podman-readiness-check.sh`
- `scripts/readiness/containerd-*.sh`
- Docker/Podman/containerd references in API, MCP, doctor, setup, docs, and
  Web UI tests

Container-shaped pieces that must be replaced before deletion:

- shared infra startup model
- local setup/quickstart path
- image/rootfs build/export path
- runtime readiness and cleanup validation
- host capacity guidance
- operator diagnostics

## Required Replacements

### Runtime Backend

Firecracker must be the Linux default candidate once readiness gates pass.

`apple-vf-microvm` must become the macOS target. `apple-container` can remain
as a compatibility experiment until the Apple VF backend proves whether direct
Virtualization.framework ownership is viable.

### Shared Infra

Shared infra should move to host services:

- gateway
- web
- comms
- knowledge
- egress
- optional enabled services

Each host service needs explicit:

- identity
- config path
- data path
- socket or port ownership
- health check
- restart policy
- log path
- audit responsibility
- teardown semantics

Containerized infra may remain only until equivalent host-service supervision
exists.

### Image Realization

The Firecracker image store currently uses Podman as a practical OCI export
tool. That dependency should be replaced with daemonless OCI tooling before
Podman can be fully removed from developer/runtime prerequisites.

Acceptable directions:

- use Go OCI libraries to pull, unpack, and apply layers directly
- use a narrow checked-in helper that exports OCI rootfs without a container
  daemon
- keep Podman only as an optional fallback during transition

The generic runtime path must never require constructing a container name or
starting a container just to address a running agent.

### Operator Surface

Setup, quickstart, doctor, MCP help, OpenAPI metadata, and Web UI copy should
present microVMs as the runtime architecture.

Container backend commands and diagnostics should be hidden, deprecated, or
marked as transitional. They should not appear as the recommended path.

## Phased Plan

### Phase 0: Freeze Container Feature Work

- stop adding new product features to Docker/Podman/containerd paths
- keep fixes only when needed to preserve tests or migration scaffolding
- update specs/docs to mark them transitional
- keep container-family names only in container-specific packages

### Phase 1: Make MicroVM Selection Explicit

- add `apple-vf-microvm` registry skeleton
- keep Firecracker selectable and feature-gated until default readiness passes
- remove auto-detection language that recommends container runtimes as the
  normal path
- require explicit opt-in for container backends

### Phase 2: Replace Containerized Shared Infra

- add host-service supervisor for shared infra
- start with the smallest core set needed for Web UI DM parity
- keep service identities, sockets, data paths, logs, and restart policies
  visible in `runtime status` and `admin doctor`
- stop requiring Docker/Podman/containerd for `agency setup`

### Phase 3: Replace Daemon-Based Image Export

- remove Firecracker image-store dependence on Podman export
- use daemonless OCI layer unpacking for rootfs creation
- keep OCI references and Dockerfiles
- validate body/enforcer rootfs builds without a container runtime

### Phase 4: Deprecate Container Runtime Backends

- mark Docker/Podman/containerd backend names deprecated in CLI/API
- remove container readiness checks from release gates
- remove container runtime smoke from required validation
- keep any remaining scripts under an explicit legacy path

### Phase 5: Delete Container Runtime Execution

- delete Docker/Podman/containerd runtime backend registration
- delete container runtime lifecycle implementation
- delete container backend setup/install code
- delete Docker network-pool tuning
- delete container runtime doctor checks
- delete Docker/Podman/containerd readiness scripts
- delete tests whose only purpose was proving container runtime execution

## First Safe Patches

1. Lock the strategy in specs and contributor guidance.
2. Add `apple-vf-microvm` backend skeleton behind an experimental gate.
3. Change setup/quickstart wording so containers are no longer recommended.
4. Make container backend selection explicit instead of auto-preferred.
5. Start host-service infra supervision with one service and a clear status
   contract.
6. Replace Podman export in the Firecracker image store.

## Readiness To Delete

Container runtime execution can be deleted when:

- Firecracker passes Web UI manage/recover/reload/cleanup/operator flows
- Firecracker is the default Linux backend candidate
- `apple-vf-microvm` has at least helper health and first guest boot on macOS
- host-service shared infra can run setup, doctor, Web UI, comms, knowledge,
  and egress without a container daemon
- OCI rootfs realization does not require Docker/Podman/containerd
- `agency admin doctor` has microVM-first checks for Linux and macOS
- setup/quickstart no longer depends on a container runtime

## Non-Goals

- deleting OCI image definitions
- removing Python only because current images are Python-based
- changing ASK requirements to make migration easier
- supporting a shared or in-workload enforcer as a shortcut
