# MicroVM Backends — Direction

## Status

Draft. Architectural direction set 2026-04-27. Updated 2026-04-29:
Agency is microVM-first. Firecracker is the Linux production implementation.
`apple-vf-microvm` is the strategic macOS microVM target. Docker, Podman,
containerd, and `apple-container` are transitional compatibility paths, not
strategic runtime architectures.

## Purpose

This spec records the decision to move Agency's agent execution backend from
container daemons (Docker/Podman) to per-platform microVM runtimes that
consume Agency's existing OCI images. It supersedes prior assumptions that
Agency would add per-OS native sandboxing (e.g., the Linux-only SRI spike) as
a long-term direction.

## Background

Agency currently runs agent workloads in containers on Linux via rootless
Podman, and has an experimental Apple Container adapter on macOS. Apple
Container is VM-backed, but its product and API surface are container-shaped.
The container approach has worked, but cross-platform support has been
painful: each platform's "container" is a different implementation (Linux
namespaces, Apple Virtualization.framework, Windows Hyper-V), and the surface
area to maintain has grown.

The `agency-sri` Linux spike (`agency-workspace/agency-sri/`) answered "can
we sandbox without a container runtime on Linux?" The answer is yes, but at
material cost: ~14-22 weeks for Linux-only, with seccomp-notify overhead
measured at 2.5-5x slower than the container path on syscall-heavy workloads.
SRI is therefore retained as research, not pursued as a product direction.

## Direction

Agency moves to a per-platform microVM execution model. Each backend boots a
small Linux VM and runs an Agency OCI image as its rootfs. The mediation
layer (Enforcer, egress proxy, LLM proxy, audit, policy) remains
cross-platform Go, backend-agnostic, and unchanged.

Container runtime backends are no longer product targets. They may stay
temporarily as scaffolding for image validation, regression comparison, and
transition work, but new runtime design should be driven by Firecracker and
`apple-vf-microvm`.

Per-platform mapping, with explicit role:

- **Linux — production / cloud runtime.** Firecracker (KVM), running Agency
  OCI images. Replaces the rootless Podman path. This is the only backend
  that needs to meet production-grade operational rigor, performance, and
  scale. Firecracker was selected over libkrun on 2026-04-27 based on a
  hands-on evaluation; libkrun's main argument was cross-platform reach,
  which lost weight once Mac and WSL2 were scoped to dev-only. See
  `firecracker-spike/docs/findings.md` for the evaluation.
- **macOS — local development only.** `apple-vf-microvm`, an Agency-owned
  Apple Virtualization.framework backend, running Agency OCI images in Linux
  microVMs. The existing `apple-container` adapter stays experimental as a
  compatibility path, but it is not the strategic microVM architecture.
- **Windows — local development only.** WSL2 with the Linux runtime nested
  inside. Requires `nestedVirtualization=true` in `.wslconfig`. No native
  Windows backend is built. **Cloud Windows hosts are explicitly out of
  scope** — production deployments target Linux.

Image format remains OCI. Agency's twelve existing Dockerfiles in `images/`
continue to be the source of truth for component rootfs. Registries, signing,
layer reuse, and vulnerability scanning continue to apply.

## Non-goals

- A native Windows backend using Hyper-V / `hcsshim`. WSL2 is the Windows
  path.
- Production-hardened SRI. The spike is reference material; no further
  investment.
- A custom or non-OCI image format. Adopting any backend that requires its
  own image format (e.g., shuru's checkpoint snapshots) is rejected.
- Running shared core infrastructure in per-agent microVMs. Shared
  mediation services can become host services. The per-agent enforcer is
  different: it remains an external enforcement boundary and may run as a
  per-agent host process or a per-agent microVM. See
  `specs/runtime/per-agent-microvm-enforcement.md`.
- Keeping Docker, Podman, or containerd as supported long-term runtime
  execution backends. See `specs/runtime/container-backend-sunset.md`.

## Tradeoffs

The microVM model accepts costs that the container model did not pay:

- **Cold start**: ~1.4 s end-to-end on commodity Linux for a small Agency
  component (measured: Firecracker invoke → enforcer listening on `:3128`).
  ~38 ms of that is VMM init; the rest is kernel boot. Snapshot/checkpoint
  patterns are a known optimization for cold-start-sensitive workloads
  (AWS publishes ~125 ms for snapshot resume).
- **Memory floor**: ~50 MB host RSS per VM for a small Agency component.
  The Firecracker spike measured ~53 MB host RSS while running the enforcer
  in a 256 MiB-allocated VM, because Firecracker pages on demand. Larger
  components scale up with their actual working set; the per-VM overhead
  beyond the workload is small. This is materially better than the
  pre-spike rough estimate of 50-200 MB and comparable to a rootless
  container.
- **Hardware boundary**: a feature, not a bug. Hardware isolation is the
  reason ASK Layer 6 properties hold without seccomp-notify, FUSE, or
  Landlock policy work.

These costs align with Agency's value-per-token positioning. The target
workload is a small number of deep agents, not a fleet of short-lived
helpers. Cold-start cost amortizes over meaningful agent work.

## References — projects considered

- **Apple Virtualization.framework** (Apple): macOS primitive for
  `apple-vf-microvm`. Agency owns the helper and VM lifecycle directly.
- **Apple Container** (Apple): VM-backed, OCI-native compatibility path.
  Existing in-tree experimental adapter; useful for comparison but
  container-shaped.
- **Firecracker** (AWS): Linux candidate. Production-proven at scale, narrow
  guest support, fast cold start. Bridging tools exist for OCI rootfs.
- **libkrun / krunvm** (Red Hat): Linux + macOS candidate. OCI-native via
  krunvm. Less battle-tested than Firecracker but cross-platform.
- **Hyperlight** (Microsoft, CNCF sandbox): out of scope as agent runtime.
  Custom guest binaries only, no general Linux userspace, no OCI. Tracked as
  a possible future capability for per-tool sandboxes (deterministic tool
  offloading pillar).
- **Shuru** (superhq-ai/shuru, Apache 2.0): out of scope as foundation. Same
  Virtualization.framework primitive, but uses checkpoint snapshots instead of
  Agency's OCI-first runtime shape. Useful reference for
  Virtualization.framework integration patterns and snapshot-based cold
  start.
- **VibeBox** (robcholz/vibebox, MIT): out of scope as foundation. Useful
  reference for agent-oriented Apple Virtualization.framework harness design.

## Sequencing

This spec does not prescribe a detailed plan. The Linux backend is the
production target and carries the deepest engineering investment; Mac and
WSL2 are local-dev backends and can be "good enough" for longer.

Rough order:

1. Linux Firecracker reaches Web UI parity and becomes the production
   microVM reference.
2. `apple-vf-microvm` reaches macOS local-development parity using the same
   backend-neutral runtime contract and ASK checklist.
3. Apple Container remains available as an experimental compatibility backend
   until evidence shows whether it should be retained, deprecated, or kept as
   a lower-level comparison target.
4. Linux Firecracker replaces Podman as the default
   Linux backend. This is the production-grade work and carries the
   strictest operational, performance, and observability requirements.
5. WSL2 nested-virt validation on a real Windows Pro host. Document
   supported hardware and `.wslconfig` requirements. Scope is local dev only.
6. Mediation-layer integration tests run uniformly across all three
   backends.

Each step follows the existing experimental-flag-then-graduate pattern used
for backend changes.

## Related specs

- `specs/adapter-architecture.md` — defines what Agency owns vs. what
  adapters may vary
- `specs/runtime/per-agent-microvm-enforcement.md` — defines the
  ASK-compliant per-agent workload VM plus external enforcer target
- `specs/runtime/apple-vf-microvm-backend.md` — defines the Agency-owned
  macOS Apple Virtualization.framework backend
- `specs/runtime/container-backend-sunset.md` — tracks the Docker, Podman,
  and containerd runtime sunset
- `specs/graceful-docker-degradation.md` — current Docker degradation
  semantics, relevant during the transition
- `specs/runtime/agent-lifecycle.md` — agent lifecycle that backends must
  implement

## Open questions

- Networking and overlay rootfs: not validated in the initial Firecracker
  spike. Need a tap-device path to the mediation plane and a read-only
  base + per-VM overlay model rather than a full ext4 build per launch.
- Init contract: Agency images are built as container CMDs and so lack a
  `/sbin/init`. Need a small Agency-supplied init that mounts /proc, /sys,
  /dev, attaches operator volumes (virtio-fs or extra block devices), and
  execs the image's CMD. Same shape across all microVM backends.
- ~~vsock control plane: evaluate `vsock` for the Agency-internal control
  channel instead of TCP-over-virtio-net.~~ **Resolved 2026-04-27**: the
  spike validated bidirectional vsock between guest and host with zero
  firewall changes. The Agency control plane (agent ↔ mediation layer)
  should ride on vsock; only agent-initiated outbound traffic needs a tap
  + NAT path. See `firecracker-spike/docs/findings.md` for the validation.
- Snapshot/checkpoint-based cold-start optimization: when does this become
  worth implementing for the Linux production path? Baseline is ~1.4 s;
  AWS-published snapshot resume is ~125 ms.
- Image build differences: do any of Agency's twelve existing Dockerfiles
  need adjustments for microVM boot? Initial spike found the
  `agency-enforcer` image runs unmodified — but it's a Go binary; Python
  workspaces and other components have not been validated.
- Dev-environment ergonomics: what level of polish does `apple-vf-microvm`
  need for the developer-laptop experience to feel native? Same question for
  WSL2 nested-virt setup on Windows.
