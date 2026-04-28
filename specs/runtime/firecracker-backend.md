# Firecracker Runtime Backend — Implementation Sketch

## Status

Draft. Pre-implementation design note. Companion to
`specs/runtime/microvm-backends.md` and informed by the validation in
`agency-workspace/firecracker-spike/`.

Update 2026-04-28: the Firecracker parity target is now per-agent workload
microVM plus per-agent external enforcer. The enforcer may run as a
host process or a separate microVM; it must never run inside the agent
workload VM. See `specs/runtime/per-agent-microvm-enforcement.md`.

## Purpose

Sketch how a `FirecrackerRuntimeBackend` would slot into Agency's existing
`runtime/contract.Backend` interface, identify the small interface
evolutions required, and surface implementation tradeoffs before code is
written.

## What already exists

The runtime-backend abstraction is in place but is being actively
de-leaked from Docker shape. A Firecracker backend should not extend that
shape further — it should align with the cleaner abstraction the
de-leaking work is moving toward.

- `internal/runtime/contract/types.go` defines `Backend`, `RuntimeSpec`,
  `BackendStatus`, `BackendCapabilities`.
- `internal/runtime/backend/registry.go` exposes a `Registry` keyed by
  name; backends register and the manager builds them by name.
- `internal/hostadapter/runtimebackend/docker.go` is the existing
  Docker/Podman implementation.

### Docker-shape leaks to avoid

The current `Backend` interface and Docker implementation contain shape
that is not backend-neutral and that would make Firecracker awkward:

- `EnsureEnforcer` and `EnsureWorkspace` as separate sub-verbs — these
  encode the "two cooperating containers per runtime" pattern from the
  Docker world. MicroVMs may want one VM with multiple processes, two
  VMs, or N — the runtime manager should not bake the choice in.
- `BackendCapabilities.SupportsComposeLike` — explicitly Docker Compose-shaped.
- Hardcoded container-name patterns like `agency-%s-workspace` /
  `agency-%s-enforcer` in `Stop` and `Inspect`. These are Docker
  identifiers; Firecracker has no analog.
- `Inspect` returning `workspace_state`/`enforcer_state` in `Details` —
  this assumes the two-container model.
- `RuntimeStorageSpec` as three host paths bind-mounted in — assumes the
  Docker bind-mount model, not virtio-fs / virtio-blk / read-only base
  with overlay.

A Firecracker backend implemented against today's interface would either
have to fake these (awkward) or push more leaks into the contract (worse).

### Reference: containerd's shim-v2 / TaskService

The cleaner abstraction to model on is **containerd's runtime v2 / shim
TaskService**, not Docker's. containerd already runs runc (containers),
Kata (microVMs), gVisor (sandboxes), and Firecracker (via
`firecracker-containerd`) under one shim API.

Key shape differences worth borrowing:

| containerd concept | Why it's better than the Docker shape |
|---|---|
| Image (OCI artifact) and Bundle (on-disk filesystem layout) are separate from runtime | Backends never touch image management |
| Container = configuration; Task = running process | Lifecycle verbs are uniform regardless of what's running them |
| TaskService verbs (`Create`, `Start`, `Wait`, `Kill`, `Delete`, `State`, `Exec`, `Pids`, `Stats`) | Each maps cleanly to runc, Firecracker, Kata, etc. |
| One shim binary per runtime type, registered by name (`io.containerd.runc.v2`, `io.containerd.kata.v2`, `aws.firecracker`) | Registry pattern that scales without Docker leaks |

The Agency `Backend` interface, as it de-leaks, should converge toward
that shape: small, uniform per-task verbs; image/bundle handling owned by
the manager (or a separate component), not the backend; capability flags
that describe the boundary type (process / hardware-VM / language-VM)
rather than the daemon's feature set.

This spec note **does not propose finalizing that interface** — that's the
in-flight de-leak work. It assumes the de-leak lands first, then the
Firecracker backend is built against the cleaner contract.

## What survives the de-leak

Independent of the interface refactor, several spike findings are
runtime-shape-neutral:

- **Image format**: OCI. Agency's twelve existing Dockerfiles are the
  source of truth; backends consume OCI image artifacts. This is the
  containerd shape too.
- **Control plane**: vsock for VM↔host control flows. Validated in the
  spike. Independent of how the backend interface ends up looking.
- **Outbound agent traffic**: tap + NAT through the egress proxy. Same
  as today, just attached to a microVM instead of a container netns.
- **Cold-start / memory metrics**: ~1.4 s cold start, ~53 MB host RSS for
  the enforcer. These set the operational expectations regardless of
  interface shape.

These are the parts of the backend implementation that don't depend on
the in-flight interface evolution.

## What the cleaner contract should express

These are the Firecracker-driven requirements that the de-leaked interface
needs to accommodate. They are stated as requirements, not as patches
against the current shape, because patching the current shape is exactly
what we don't want.

### Transport: vsock as a first-class option

The control-plane transport between agent and mediation must support
vsock alongside loopback HTTP and UNIX HTTP. Concretely: an enum that
includes `vsock`, with addressing semantics of "UDS base path + port"
(Firecracker translates guest-port `N` to host UDS `<base>_N`).
Spike-validated; not negotiable.

### Storage: image rootfs and operator mounts are different concerns

The Docker shape collapses both into `{configPath, statePath,
workspacePath}` bind mounts. Containerd separates them: an *image* (OCI
artifact, content-addressed, immutable) and a *bundle* (on-disk
filesystem layout for one running task, ephemeral). The runtime manager
owns image handling; the backend just receives a bundle path.

For Agency:

- **Image**: an OCI reference. Backend-neutral. Image-to-rootfs
  realization (ext4 build, snapshot, layered overlay) is a backend
  implementation detail, not part of the interface.
- **Operator mounts**: a list of `{HostPath, GuestPath, Mode}` plus an
  optional `Transport` hint (`virtiofs`, `virtioblk`, `bind`). Container
  backends ignore the hint and bind-mount; microVM backends honor it.

### Capabilities: describe the boundary, not the daemon's features

Replace docker-flavored flags (`SupportsRootless`, `SupportsComposeLike`)
with characteristics of the isolation primitive itself:

- `Isolation`: enum of `process` (namespace/cgroup), `hardware-vm`
  (Firecracker, Apple Container, Hyper-V), `language-vm` (Wasm/Hyperlight,
  forward-looking).
- `RequiresKVM` / `RequiresHVF` / `RequiresHyperV`: host requirement
  flags so `agency admin doctor` can pre-flight.
- `SupportsSnapshots`: whether the backend can resume from a frozen state.
- `MaxConcurrentInstances`: optional hint (Firecracker has no daemon
  bottleneck; Docker has a daemon scaling ceiling).

Rootless/compose flags can stay on a Docker-specific extension struct if
they're still needed by the Docker backend internally — but not on the
generic capability surface.

### Lifecycle verbs: containerd shim shape, not Docker

Per the de-leak direction, the backend exposes uniform per-task verbs:
`Create`, `Start`, `Wait`, `Kill`, `Delete`, `State`, plus `Exec` and
`Pids`/`Stats` if needed. Each backend implements these against its
isolation primitive — runc/podman against namespaces, Firecracker against
microVMs, future Apple Container against Virtualization.framework.

`EnsureEnforcer` + `EnsureWorkspace` is *not* part of this surface.
Whether a runtime is "two cooperating processes" or "one VM with two
processes" is a deployment shape decided at the manager layer (or via
multiple `Create` calls that share a configuration), not encoded in the
backend interface.

## Implementation sketch — FirecrackerRuntimeBackend

A Go-level skeleton is deferred until the de-leaked `Backend` interface
lands. Drafting one against today's interface would just bake in more of
the shape we're trying to remove. The pieces below stay true regardless
of how the interface settles.

### Backend struct (shape, not signatures)

```go
type FirecrackerRuntimeBackend struct {
    BinaryPath string                  // path to firecracker binary
    KernelPath string                  // path to vmlinux guest kernel
    StateDir   string                  // per-task VM artifacts
    Images     ImageStore              // OCI image realization (ext4, overlays)
    Tasks      *TaskSupervisor         // task lifecycle, restart, crash detection
    Vsock      VsockListenerFactory    // host UDS listener per task
}
```

Supporting components are concrete types co-located with the backend.
They are not interfaces — premature abstraction was the cautionary tale
from the SRI spike.

### Per-task flow (containerd-shim shape)

For each `Create`/`Start` call the backend:

1. Resolves the OCI image to a rootfs realization via `ImageStore`
   (cached by digest; layered base + per-task overlay).
2. Allocates a per-task UDS path under `StateDir/<task-id>/vsock.sock`
   and starts a vsock listener that bridges agent-side connections to
   the appropriate host-side mediation services.
3. Renders a Firecracker JSON config: kernel, rootfs drive, optional
   data drives, vsock device, machine config. No network-interfaces
   block for the control plane (vsock-only); a tap is added separately
   for outbound paths if the task needs them.
4. Launches `firecracker --no-api --config-file <cfg>` under
   `TaskSupervisor`, which records the OS PID, watches for exit, and
   re-launches per restart policy.

`Wait` blocks on the supervisor; `Kill` sends SIGTERM (or SIGKILL on
escalation) to the Firecracker process, which causes the guest to be
torn down; `Delete` unlinks the vsock UDS, removes the per-task state
directory, and releases the rootfs overlay. `State` reads supervisor
state plus a vsock health probe.

## Where things are bigger than they look

Three areas where the interface fits but the implementation is non-trivial:

### OCI → ext4 (the `ImageStore`)

Spike used `podman create | podman export | tar -x | mke2fs -d`. That's
fine for a one-shot, not great for a hot-path. Production needs:

- A content-addressed cache (image digest → ext4 path).
- Layered builds: the OCI base image becomes a read-only ext4; per-runtime
  state is an overlay (likely `dm-snapshot` or a separate writable disk).
- Build-on-demand vs. pre-warmed. Probably both, with an LRU eviction on
  the cache.

This is the largest single chunk of new code for the Firecracker backend.

### vsock listener fan-out (the `VsockListenerFactory`)

One UDS path per runtime. Each listener accepts the agent's control-plane
handshake and bridges to the host-side mediation services (audit, policy,
budget, capability registry). The bridge is small — protobuf or
JSON-over-UDS — but every endpoint the agent uses must have a vsock
adapter on the host.

### Process supervision (the `VMSupervisor`)

Firecracker exits when the guest reboots. Restart policy enforcement,
crash detection, and recovery semantics live in the supervisor, not in
Firecracker itself. Mirrors what Docker daemon does for containers, except
Agency owns the supervisor here. Probably ~200-300 LOC of careful code.

## Migration plan

This sketch is implementation-ready *after* the in-flight Docker-leak
purge lands. Rollout is incremental and never regresses the Podman path.

1. **Wait for / land the de-leaked `Backend` interface** as part of the
   ongoing leak-purge work. Containerd's shim-v2 / TaskService is the
   reference, not Docker's API. This spec aligns with that direction.
2. **Add `FirecrackerRuntimeBackend` behind an experimental flag**
   (existing pattern in `internal/features/registry.go`). Backend is
   selectable but not default. Built directly against the de-leaked
   interface.
3. **Implement vsock-bridged enforcer** so it can be reached over
   loopback HTTP, UNIX HTTP, or vsock based on the runtime's transport.
4. **Ship the OCI image store and overlay rootfs** as the first
   non-trivial chunk. Largest single piece of new code.
5. **Doctor checks**: `agency admin doctor` learns to detect KVM
   availability, vsock support, and the isolation/capability surface.
6. **Graduate the experimental flag** when integration tests pass on
   reference hosts.

No step in this plan changes existing Podman behavior. The Firecracker
backend lives entirely beside the existing one until graduation.

## Open questions

- **Where does the OCI-to-ext4 cache live on disk?** Current Agency state
  is under `~/.agency/`; rootfs blobs are larger and may need a separate
  configurable path.
- **vsock authentication**: a CID identifies a VM, but the host should
  also bind a token to the UDS so a misbehaving agent can't trivially
  spoof its identity by reconnecting. Probably a per-runtime ephemeral
  token written into the VM's read-only config drive at boot.
- **Compose-like multi-component agents**: Docker backend's
  `EnsureEnforcer` + `EnsureWorkspace` model assumes two coordinated
  containers per runtime. For microVMs, do we want one VM per component
  (two VMs) or one VM with both processes (cheaper, weaker isolation
  between enforcer and workspace)? Most production deployments will want
  separate VMs; defer this decision until the first implementation works.
- **Audit log paths**: the Docker backend writes to host-mounted volumes.
  For microVMs, audit must flow over vsock or through virtio-fs. Vsock is
  simpler.
