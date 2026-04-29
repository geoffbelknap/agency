# Apple VF MicroVM Runtime Backend

## Status

Draft. Proposed 2026-04-29 as the strategic macOS microVM backend.
`apple-container` remains a separate experimental compatibility backend.

## Purpose

Define an Agency-owned macOS runtime backend, `apple-vf-microvm`, built on
Apple Virtualization.framework. The goal is to match the Firecracker runtime
shape on Apple silicon: one microVM per agent workload, an external per-agent
enforcer boundary, complete mediation, complete audit, and Web UI parity.

This backend is distinct from Apple Container. Apple Container is an
Apple-managed OCI/container product that happens to use
Virtualization.framework under the hood. `apple-vf-microvm` is an
Agency-managed microVM backend. It should mirror Firecracker's runtime shape,
not Apple Container's container-shaped API.

## Decision

Agency should add a new experimental backend named:

```text
apple-vf-microvm
```

The backend is macOS Apple silicon only. It is for local development, not
production. Linux production continues to target Firecracker.

The public runtime contract remains backend-neutral:

- `Ensure`
- `Stop`
- `Inspect`
- `Validate`
- `Capabilities`

Internally, `apple-vf-microvm` owns the Apple-specific VM machinery through a
small helper process.

```text
Agency Go gateway/runtime backend
  -> agency-apple-vf-helper
      -> Apple Virtualization.framework
          -> workload Linux VM
          -> optional enforcer Linux VM
```

The Go gateway remains the policy, audit, orchestration, runtime-status, and
operator authority. The helper only realizes VM lifecycle commands and emits
observations.

## Why Not Apple Container

Apple Container is useful, and the existing adapter should remain available
while it proves out macOS ergonomics. It is not the strategic microVM target
because:

- its API and lifecycle surface are container-shaped
- Agency does not directly own VM construction, devices, and state
- event/restart recovery depends on Apple Container service behavior
- network and mount semantics are mediated through a product layer
- it risks reintroducing container-shaped assumptions into generic runtime
  paths

`apple-vf-microvm` accepts more implementation work so Agency can own the
same primitives it owns in Firecracker: VM lifecycle, guest rootfs, virtio
devices, host bridges, per-agent state, and teardown.

## Implementation Language

The Agency-facing backend stays Go.

The first helper implementation should be Swift, using
Virtualization.framework directly. Swift keeps Apple SDK compatibility,
delegate/callback handling, entitlements, and codesigning close to Apple's
native toolchain.

Non-goals for the first implementation:

- adding Rust to the required build
- using raw Go cgo/Objective-C shims as the main implementation path
- depending on `Code-Hex/vz` as the committed substrate before a Darwin 25
  spike proves it works for the needed APIs

Shuru and VibeBox are useful references for Virtualization.framework VM
harness design, but neither should be adopted as a dependency for the first
Agency backend.

## ASK Requirements

The backend is acceptable only if these invariants hold:

- **External enforcement**: the enforcer runs outside the agent workload VM.
- **Complete mediation**: the workload VM reaches exactly one control-plane
  route: its assigned enforcer endpoint.
- **Complete audit**: mediated operations are written through the enforcer
  audit path with agent identity and runtime identity.
- **Explicit least privilege**: per-agent config, auth, audit, data paths,
  guest devices, and host bridge targets are generated explicitly.
- **Visible and recoverable boundaries**: operators can inspect workload VM,
  enforcer boundary, bridge, helper state, guest config revision, audit path,
  and cleanup status.

The workload VM must not receive direct host-service endpoints for gateway,
comms, knowledge, egress, provider APIs, tools, or runtime control.

## Runtime Topology

### `host-process` enforcement mode

```text
host:
  agency-gateway
  agency-web
  agency-comms
  agency-knowledge
  agency-egress
  agency-enforcer/<agent-id>
  agency-apple-vf-helper

microVM:
  agency-agent-workload/<agent-id>

workload VM -> virtio socket/host bridge -> per-agent host enforcer -> host services
```

This should be the first implementation mode because it minimizes variables
and mirrors the Firecracker parity path.

### `microvm` enforcement mode

```text
host:
  agency-gateway
  agency-web
  agency-comms
  agency-knowledge
  agency-egress
  agency-apple-vf-helper

microVM:
  agency-enforcer/<agent-id>

microVM:
  agency-agent-workload/<agent-id>

workload VM -> host-owned bridge -> enforcer VM -> host service bridges
```

This mode is the high-isolation target. It should follow after
`host-process` mode reaches Web UI parity.

## Configuration

Initial backend configuration:

```yaml
hub:
  deployment_backend: apple-vf-microvm
  deployment_backend_config:
    helper_binary: /usr/local/bin/agency-apple-vf-helper
    kernel_path: /var/lib/agency/apple-vf/vmlinux
    state_dir: /var/lib/agency/apple-vf
    enforcement_mode: host-process # host-process | microvm
    memory_mib: 512
    cpu_count: 2
```

Defaults while experimental:

- `enforcement_mode=host-process`
- `memory_mib=512`
- `cpu_count=2`
- `state_dir=$AGENCY_HOME/runtime/apple-vf-microvm`

The backend must remain opt-in and feature-gated until the readiness gates
pass on macOS Apple silicon.

## Helper Contract

The helper should expose a narrow local protocol. The exact transport may be
stdio JSONL or a local UNIX socket, but requests must be structured argv/data,
not shell strings.

Minimum commands:

| Command | Purpose |
|---|---|
| `health` | Verify helper, entitlement, SDK/runtime availability |
| `prepare` | Validate VM config, rootfs, devices, sockets |
| `start` | Start one VM and begin lifecycle observation |
| `stop` | Graceful VM stop with bounded timeout |
| `kill` | Force-stop a VM when graceful stop fails |
| `inspect` | Return normalized VM state and device/bridge health |
| `delete` | Remove helper-owned transient state |
| `events` | Stream VM started/exited/failed observations |

Every request and event must include:

- request ID
- runtime ID
- component role: `workload` or `enforcer`
- helper version
- backend name
- Agency home hash

The helper does not read constraints, provider credentials, durable memory, or
operator secrets. It receives only the per-runtime artifacts needed to create
and supervise VMs.

## Image And RootFS Strategy

Agency's source image format remains OCI. `apple-vf-microvm` should reuse the
same image realization direction as Firecracker:

1. resolve the configured OCI image
2. produce a bootable ARM64 Linux rootfs artifact
3. inject Agency's guest init contract
4. create a per-runtime writable copy or overlay
5. pass the rootfs to Virtualization.framework as a block device

The first implementation can use a conservative full-copy rootfs per runtime.
Layered base images, sparse overlays, and snapshot resume are later
optimizations.

The guest init contract should be shared with Firecracker where possible:

- mount `/proc`, `/sys`, and required device paths
- read generated runtime config
- start the agent body or enforcer process
- forward signals and exit status predictably
- keep audit and health paths explicit

## Transport And Mediation

Virtualization.framework supports guest-host socket devices through its virtio
socket APIs. The backend should expose the same logical transport shape as
Firecracker:

```text
vsock_http
```

The guest-visible endpoint may be implemented with Apple virtio sockets, but
the backend-neutral runtime manifest should stay at the logical transport
level. Handler and Web UI code should not learn Apple-specific socket names.

For `host-process` mode:

- workload VM receives only the enforcer control endpoint
- host bridge forwards that endpoint to the per-agent host enforcer
- no host service targets are exposed to the workload VM

For `microvm` mode:

- workload VM reaches only the enforcer VM
- enforcer VM receives explicit bridges to gateway, comms, knowledge, egress,
  provider routing, and any enabled services
- the host owns all bridge configuration and teardown

## Capabilities

Initial capabilities:

```text
Name: apple-vf-microvm
SupportedTransportTypes: [vsock_http]
SupportsRootless: false
SupportsComposeLike: false
Isolation: microvm
RequiresKVM: false
RequiresAppleVirtualization: true
SupportsSnapshots: false
```

Snapshot support should remain false until a measured snapshot/checkpoint path
is implemented and included in restart/cleanup validation.

## Lifecycle

`Ensure(runtime)`:

1. compile per-agent enforcer config, auth, audit, data, and service metadata
2. start or update the enforcer boundary
3. realize the workload OCI image as an ARM64 bootable rootfs
4. start the host bridge exposing only the enforcer endpoint
5. ask the helper to start the workload VM
6. validate helper state, VM state, bridge state, enforcer health, and DM
   readiness before reporting healthy

`Stop(runtime)`:

1. stop workload VM with bounded graceful timeout
2. stop and unlink workload bridge sockets
3. stop the enforcer boundary
4. remove transient VM state, rootfs copy, sockets, pid/state files
5. leave durable agent state and audit intact

`Inspect(runtime)` reports:

- backend name
- enforcement mode
- workload VM state
- enforcer state
- bridge state
- helper state
- last error
- restart/crash counters
- rootfs/config paths
- audit path

`Validate(runtime)` fails closed if any required VM, enforcer, bridge, audit,
or mediation component is missing.

## Doctor Checks

When `apple-vf-microvm` is selected, `agency admin doctor` should check:

- macOS on Apple silicon
- Darwin/macOS version support, including Darwin 25
- helper binary exists and is executable
- helper is codesigned with the virtualization entitlement
- Virtualization.framework can construct a minimal VM configuration
- configured kernel exists and is parseable
- configured state directory is writable
- virtio socket support is available
- no owned stale VM/helper/socket/rootfs state remains for stopped agents
- runtime health is separate from backend hygiene

Doctor must fail closed when helper health or entitlement checks fail.

## Web UI Parity Target

The initial parity target is the same operator flow used for Firecracker:

1. create or select an agent
2. start it using `apple-vf-microvm`
3. see healthy runtime status
4. inspect manifest/status details
5. open a DM
6. send a message
7. receive a mediated response
8. restart the agent
9. stop/delete the agent
10. confirm no workload VM, enforcer process/VM, bridge socket, rootfs copy,
    helper state, or transient runtime artifact remains

This validation is manual macOS Apple silicon coverage until a suitable hosted
macOS virtualization environment exists.

## Mac Codex Handoff

Current repo state for a macOS Apple silicon implementation agent:

- `apple-vf-microvm` is already the strategic default backend on macOS.
- The backend is registered in `RuntimeSupervisor` when selected, even without
  experimental surfaces.
- `internal/hostadapter/runtimebackend/apple_vf_microvm.go` exists as a
  capability-reporting skeleton with not-implemented lifecycle verbs.
- Host infra (`egress`, `comms`, `knowledge`, `web`) runs as host services for
  `apple-vf-microvm`, matching the Firecracker microVM model.
- `agency admin doctor` currently has Firecracker microVM host-infra checks;
  Apple VF doctor checks still need to be added.
- `tools/apple-vf-helper` now contains the first Swift helper scaffold.
  `health` validates Apple Virtualization.framework availability and Apple
  silicon architecture. Lifecycle commands intentionally return structured
  not-implemented responses.

The Mac-side implementation agent should work in these commit-sized chunks:

1. **Helper health and doctor hookup**
   - Build `tools/apple-vf-helper` on macOS Apple silicon.
   - Add `agency admin doctor` checks for `apple-vf-microvm`:
     - GOOS/Darwin host is macOS
     - architecture is Apple silicon
     - helper binary exists and is executable
     - helper `health` returns `ok=true`
     - configured state directory is writable
     - kernel path is configured and readable
   - Add unit tests for JSON parsing and failure reporting.
   - Manual validation:
     - `scripts/readiness/apple-vf-helper-build.sh`
     - `agency admin doctor`

2. **Helper protocol types**
   - Define request/response/event JSON structs in the Swift helper.
   - Define matching Go-side helper client structs under
     `internal/hostadapter/runtimebackend/`.
   - Keep commands structured. Do not invoke shell strings.
   - Required commands: `health`, `prepare`, `start`, `stop`, `kill`,
     `inspect`, `delete`, `events`.
   - Unit-test Go parsing without requiring macOS.

3. **Minimal VM configuration smoke**
   - Implement helper `prepare` for one workload VM config.
   - Construct and validate `VZVirtualMachineConfiguration`.
   - Do not boot yet.
   - Validate kernel path and rootfs block-device path.
   - Return normalized config/device diagnostics.

4. **First boot/stop/delete**
   - Implement helper `start`, `inspect`, `stop`, `kill`, and `delete` for a
     minimal ARM64 Linux rootfs.
   - Keep all state under `state_dir/<runtime-id>/`.
   - Ensure bounded stop and force-kill cleanup.
   - Emit lifecycle events in JSONL.

5. **Go backend lifecycle wiring**
   - Replace `Ensure`, `Stop`, `Inspect`, and `Validate` not-implemented
     errors in `apple_vf_microvm.go` with calls to the helper client.
   - Mirror Firecracker details fields where possible:
     `workload_vm_state`, `enforcer_state`, `bridge_state`, `state_dir`,
     `enforcement_mode`, `last_error`.
   - Preserve backend-neutral handler/API contracts.

6. **RootFS and init contract**
   - Reuse the Firecracker OCI-to-rootfs direction, adapted for ARM64 macOS.
   - First pass may accept a prebuilt rootfs path in backend config.
   - Follow-up should realize OCI image to bootable rootfs and inject Agency
     guest init.

7. **Host-process enforcer + bridge**
   - Start the per-agent host enforcer exactly as Firecracker host-process mode
     does.
   - Expose only the enforcer endpoint into the workload VM.
   - Do not expose gateway, comms, knowledge, egress, provider APIs, tools, or
     runtime control directly to the workload VM.

8. **Web UI parity smoke**
   - Add `scripts/e2e/apple-vf-microvm-webui-smoke.sh`.
   - Add a Playwright operator flow mirroring Firecracker:
     create, start, inspect, DM, restart recovery, reload, stop/delete,
     cleanup assertions.
   - Keep it manual macOS-only until hosted macOS virtualization is reliable.

Suggested first command on the Mac:

```bash
scripts/readiness/apple-vf-helper-build.sh
```

If that passes, start chunk 1 by wiring helper health into
`agency admin doctor` for `apple-vf-microvm`.

Do not work on Apple Container for this track. It is compatibility code and
must not shape `apple-vf-microvm`.

## Implementation Sequence

1. **Spec and registry skeleton**
   - add `apple-vf-microvm` as an experimental backend name
   - report microVM capabilities
   - return not-implemented lifecycle errors
   - add feature registry entry

2. **Swift helper spike**
   - build a signed helper that runs `health`
   - validate entitlement and Darwin 25 behavior
   - construct a minimal `VZVirtualMachineConfiguration`
   - no guest boot required yet

3. **First guest boot**
   - boot a minimal ARM64 Linux kernel/rootfs
   - report start/stop/inspect events
   - teardown leaves no helper state

4. **OCI rootfs realization**
   - adapt the Firecracker image-store pattern for ARM64 macOS
   - inject the shared Agency init contract
   - produce per-runtime writable rootfs copies

5. **Host-process enforcer mode**
   - start per-agent host enforcer
   - expose only the enforcer endpoint to the workload VM
   - validate DM path and audit path

6. **Web UI parity harness**
   - add `apple-vf-microvm` operator smoke
   - validate create/inspect/DM/restart/stop/delete
   - add cleanup assertions

7. **MicroVM enforcer mode**
   - boot separate enforcer VM
   - bridge workload VM only to enforcer VM
   - bridge enforcer VM to explicit host services
   - compare reliability and resource cost with host-process mode

## Readiness Gates

Do not promote beyond experimental until all pass on a real macOS Apple
silicon host:

- `go test ./...`
- `go build ./cmd/gateway`
- helper build and codesign validation
- helper health on Darwin 25+
- minimal VM boot/stop/delete smoke
- runtime contract smoke against an `apple-vf-microvm` disposable agent
- Web UI operator flow
- gateway restart recovery
- cleanup drift validation
- doctor separates runtime health from backend hygiene
- ASK checklist passes for mediation, audit, least privilege, and boundary
  visibility

## Non-Goals

- replacing Firecracker on Linux
- making macOS a production backend
- making `apple-vf-microvm` default before parity evidence exists
- running the enforcer inside the workload VM
- exposing Apple-specific VM details through generic API handlers
- adopting Shuru, VibeBox, or Apple Container as the backend implementation
- adding Rust to the required build for the first implementation

## Open Questions

- Should the helper run as a short-lived command per lifecycle operation or a
  long-lived supervisor process?
- Which Linux kernel should be the shared macOS guest kernel, and how should it
  be distributed?
- Can Firecracker's guest init contract be shared byte-for-byte, or does Apple
  VF need a small platform branch?
- What is the fastest safe ARM64 OCI-to-rootfs path on macOS?
- Is virtio socket behavior close enough to keep the public transport named
  `vsock_http`, or should the contract grow a more generic `vm_socket_http`
  alias while preserving existing Firecracker semantics?
- When is snapshot support worth implementing for local developer workflows?
