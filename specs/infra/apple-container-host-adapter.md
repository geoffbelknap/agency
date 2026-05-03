# Apple Container Host Adapter Lifecycle

## Status

Draft. `apple-container` remains experimental and opt-in. The current
implementation has a Go helper for health, inspect/list, verified
start/stop/kill/delete/exec, bounded reconciliation, and command-driven
lifecycle events inside the Gateway process. A separate SwiftPM wait-helper is
wired into normal container start for Apple `ClientProcess.wait()` and emits
start/exit JSONL for containers it starts. Gateway seeds each Apple event
stream from one bounded helper reconciliation snapshot, which covers restart
visibility for already-recorded stopped/exited state. Promotion still requires
a durable wait supervisor or platform support that can reattach exit
observation to already-running containers after Gateway restart.

## Purpose

This spec defines the lifecycle and helper contract required for Apple
Container to function as an ASK-compliant Agency host adapter backend.

Apple Container does not expose a Docker-compatible event stream. Agency must
therefore not pretend it can watch Apple Container through Docker semantics.
Instead, Agency owns the lifecycle it initiates and emits normalized runtime
events from a host-side helper that uses Apple Container's process wait and
exit-monitoring APIs.

## Non-Goals

- Making `apple-container` the default backend.
- Adding Apple Container smoke tests to required CI.
- Reaching exact Docker or rootless Podman feature parity.
- Supporting arbitrary out-of-band Apple Container workloads as first-class
  Agency resources.
- Moving enforcement, mediation, policy, or audit authority into the helper.

## ASK Boundary

The Apple helper is host-adapter infrastructure. It runs outside the agent
boundary and has no direct trust relationship with the agent workload.

Hard requirements:

- Gateway remains the policy, audit, and operator authority.
- The helper never reads agent constraints, identity, memory, provider
  credentials, or audit sinks.
- The helper never grants agent network access directly.
- All agent traffic still flows through the enforcer and egress paths.
- Lifecycle events emitted by the helper are observations, not authority.
  Gateway decides how those observations affect runtime status, halt, restart,
  cleanup, and operator warnings.
- Helper unavailability fails closed: Agency must not silently fall back to an
  unmediated or partially mediated runtime.

## Architecture

```
Gateway
  | lifecycle commands and owned-resource metadata
  v
Apple host adapter
  | narrow local RPC / subprocess protocol
  v
agency-apple-container-helper
  | Apple Container client APIs
  v
container-apiserver / container-runtime-linux
```

The Go host adapter owns Agency semantics. The helper owns Apple-specific
transport and lifecycle details.

The helper may be implemented as a small Swift executable linked against
Apple's Container client libraries. It should be invoked by the Go adapter over
a narrow protocol that can be tested without running Apple Container.

## Resource Ownership

Every Apple Container resource created for Agency must include stable ownership
metadata:

| Field | Purpose |
|---|---|
| `agency.managed=true` | Distinguishes Agency-owned resources from user workloads |
| `agency.backend=apple-container` | Records backend identity |
| `agency.home=<hash>` | Prevents cleanup across Agency homes |
| `agency.agent=<name>` | Maps per-agent resources |
| `agency.role=<role>` | Identifies workspace, enforcer, or infra role |
| `agency.instance=<id>` | Correlates resources for one runtime instance |

If Apple Container rejects empty label values, the adapter must omit those
labels rather than emitting malformed CLI/API arguments.

The adapter must only reconcile or delete resources that match its ownership
labels and current Agency home hash.

## Network Topology

Agency's backend-neutral lifecycle contract is declarative: the runtime asks
the host adapter to realize a desired container topology. It does not require
Docker's imperative `create` then `network connect` sequence.

Backends that support late network attach may use it internally. Backends that
require complete network membership at creation time, currently containerd and
Apple Container, must receive the complete endpoint set in the create request.

Apple Container behavior:

- infra and runtime containers are created with their final allowed networks
- post-create network attach is not part of the normal lifecycle path
- `NetworkConnect` remains an optional repair capability and fails closed when
  unsupported
- mediation networks remain explicit in the declared topology so enforcers,
  egress, and service traffic boundaries are reviewable before create

## Lifecycle Contract

Agency lifecycle operations are command-driven and verified synchronously.
Event delivery is used for asynchronous observation and reconciliation, not as
the only proof that a command succeeded.

### Create

Input:

- normalized container specification
- image reference
- mounts
- network attachments
- published ports or socket forwards
- resource limits
- labels

Behavior:

1. Validate that the Apple Container service is available.
2. Validate backend config rejects socket-shaped Docker/Podman/containerd
   fields.
3. Create the Apple container with Agency labels and complete declared network
   topology.
4. Inspect the created container.
5. Return normalized container identity and created/stopped state.

Failure:

- If create partially succeeds, cleanup is attempted only for matching owned
  resources.
- If inspect fails after create, the operation fails closed and marks the
  runtime state unknown until explicit reconciliation.

### Start

Behavior:

1. Start the container init process through the helper.
2. Register a wait/exit monitor before reporting the container as running.
3. Inspect the container and network state.
4. Emit `runtime.container.started` after verification.
5. Return only after the backend reports a running state or a bounded startup
   failure is classified.

The helper must attach a wait task to the init process for every Agency-owned
container it starts. When that process exits, the helper emits a normalized
exit event to Gateway.

### Stop

Behavior:

1. Send graceful stop with configured timeout.
2. Wait for stop completion using the helper/API where possible.
3. Inspect final state.
4. Emit `runtime.container.stopped` with reason `operator_stop` when the stop
   was requested by Agency.

Failure:

- Timeout escalates to kill only when requested by the lifecycle operation.
- If stop or kill cannot verify stopped state, runtime status becomes
  `unknown` or `degraded`, not `healthy`.

### Restart

Restart is stop plus start through the same verified lifecycle path. It must
not bypass create/start validation or event registration.

### Kill

Kill is an explicit operator or fail-closed action. It sends a signal to the
container init process, then waits for exit and inspects final state.

Kill emits `runtime.container.killed` only after verification or emits
`runtime.container.state_unknown` if verification fails.

### Delete

Behavior:

1. Stop or kill if the container is still running and the operation requested
   force semantics.
2. Delete the container.
3. Remove owned network, volume, and helper state only when labels and Agency
   home hash match.
4. Inspect/list to verify absence.
5. Emit `runtime.container.deleted`.

Delete must not remove user-created Apple Container resources without Agency
ownership labels.

### Exec

Exec is supported for operator and validation tasks. It is not an enforcement
bypass.

Requirements:

- exec is allowed only through Gateway-owned operations
- command, user, cwd, and environment are explicit
- output is bounded
- result is audited by Gateway
- failure does not imply container failure unless the caller maps it that way

## Event Model

Apple helper events are normalized platform events. They are not Docker events.

### Event Envelope

```json
{
  "id": "evt-runtime-...",
  "source_type": "platform",
  "source_name": "host-adapter/apple-container",
  "event_type": "runtime.container.exited",
  "timestamp": "2026-04-26T00:00:00Z",
  "data": {
    "backend": "apple-container",
    "container_id": "agency-agent-workspace",
    "agent": "agent-name",
    "role": "workspace",
    "instance": "runtime-instance-id",
    "exit_code": 0,
    "reason": "process_exit"
  },
  "metadata": {
    "agency_home_hash": "...",
    "owned": true
  }
}
```

### Required Events

| Event | Emitted When |
|---|---|
| `runtime.container.created` | created state is verified |
| `runtime.container.started` | running state is verified and wait monitor is registered |
| `runtime.container.exited` | helper wait task observes init-process exit |
| `runtime.container.stopped` | stop is requested and verified |
| `runtime.container.killed` | kill is requested and verified |
| `runtime.container.deleted` | delete is verified |
| `runtime.container.state_unknown` | command result cannot be verified |
| `runtime.backend.unavailable` | Apple Container service/helper becomes unavailable |

### Delivery Semantics

Initial implementation may be at-most-once while the helper process is alive.
Gateway must reconcile on startup and before lifecycle mutations so missed
events do not create unsafe healthy states.

The target implementation should persist a small helper-side event cursor,
state snapshot, or supervisor registry for Agency-owned resources so Gateway
restart can recover the latest known exit observations.

The current Go helper returns normalized events with lifecycle command
responses. Gateway maps exit/stop/kill/unknown helper events into its existing
host-state stream so intentional teardown and command failures can be observed
without polling. It intentionally does not map ordinary start responses as
auto-restarts.

The Swift wait-helper at `tools/apple-container-wait-helper` uses Apple's
`ContainerClient.bootstrap`, `ClientProcess.start`, and `ClientProcess.wait`
sequence. The normal Apple lifecycle path starts containers through this
helper when configured. The manual validation script is
`scripts/readiness/apple-container-wait-helper-smoke.sh`. That path still
needs a durable supervisor/IPC shape before it can recover wait tasks for
containers that were already running when Gateway restarted.

## Reconciliation Without Polling

Polling is not the steady-state control plane.

Reconciliation is allowed at bounded lifecycle boundaries:

- Gateway startup
- helper startup
- before create/start/stop/delete
- after command failure
- during `admin doctor`
- during explicit operator validation

Reconciliation reads Apple Container list/inspect output for Agency-owned
labels and repairs Gateway's view. It must not loop continuously as a surrogate
event stream.

## Helper Protocol

The helper should expose a stable local protocol with explicit commands and
JSON responses.

Minimum commands:

| Command | Purpose |
|---|---|
| `health` | verify helper and Apple Container service availability |
| `create` | create a container from normalized spec |
| `start` | start and register exit monitoring |
| `stop` | graceful stop |
| `kill` | signal init process |
| `delete` | delete owned container |
| `inspect` | return normalized state |
| `list-owned` | list Agency-owned resources for one home hash |
| `exec` | run bounded operator/validation command |
| `events` | stream normalized helper events to Gateway |

The protocol must carry request IDs. Every response and event must include the
request ID or lifecycle correlation ID where applicable.

The helper should not accept broad shell strings. It accepts argv arrays and
structured fields.

## Gateway Integration

The Go adapter owns these mappings:

- helper health -> `admin doctor` backend checks
- helper inspect/list -> runtime manifest/status/validate
- helper events -> Gateway platform event bus and audit records
- helper failures -> fail-closed runtime status
- resource labels -> ownership-safe cleanup

Gateway must audit:

- lifecycle command requested
- helper command submitted
- helper result
- normalized event received
- runtime status transition chosen by Gateway

## Doctor Semantics

`agency admin doctor` for `apple-container` should report:

- backend identity and opt-in status
- Apple Container service availability
- helper availability and version
- wait-helper availability and Apple Container service reachability
- lifecycle helper event support
- network capability status
- owned-resource cleanup drift
- runtime contract health separately from backend hygiene

Doctor should warn when the wait helper is missing or unhealthy. Lifecycle
event support should pass only when the helper path can emit synthetic or
reconciled events during validation.

## Readiness Gates

Do not remove the experimental gate until all of the following pass on macOS
Apple silicon with Apple Container available:

- `go test ./...`
- `go build ./cmd/gateway`
- `scripts/readiness/apple-container-smoke.sh`
- runtime contract smoke against an Apple-backed disposable agent
- create/start/stop/restart/delete lifecycle validation
- operator DM round trip
- helper exit event validation
- helper restart reconciliation validation
- cleanup drift validation for owned resources
- `agency admin doctor` separates backend hygiene from runtime health

Agency Web UI parity should be validated through the supported `microagent`
backend contract. Runner-specific Apple backend UI parity belongs outside
Agency's release gate.

If this experimental backend is revisited, validate it below the `microagent`
boundary or in the owning runner project. Agency should not expose a separate
Apple Container Web UI contract.

Because this path depends on Apple's macOS service and Virtualization.framework
stack, it remains a manual macOS Apple silicon validation gate. Linux CI should
continue to compile and unit-test Apple adapter code, but must not require the
live Apple smoke.

## Implementation Slices

### Slice 1: Current CLI Backend Hardening

- keep CLI-backed create/start/stop/delete/exec working
- keep Apple build-context compatibility fixes
- keep smoke helper flag-based and environment-clean
- report event support as incomplete in doctor

### Slice 2: Helper Skeleton

- add Go helper executable
- implement `health`, `inspect`, `list-owned`
- add Go helper client with fake transport tests
- route verified start/stop/kill/delete/exec through helper responses
- map command lifecycle events into Gateway host-state stream

### Slice 3: Helper-Owned Start And Exit Events

- Swift helper package exists for Apple `ClientProcess.wait`
- process wait is registered before reporting running
- `runtime.container.exited` is streamed for helper-started containers
- doctor validates wait-helper event support

### Slice 3b: Declared Network Topology

- complete network topology is passed at create time for backends that require
  it
- Apple Container does not use Docker-shaped post-create attach in normal
  lifecycle
- smoke evidence must show no post-create network attach warnings

### Slice 4: Full Verified Lifecycle

- move create/stop/kill/delete/exec to helper protocol
- add bounded reconciliation at startup and lifecycle boundaries
- validate cleanup and restart behavior
- update readiness checklist and remove lifecycle-specific gate warnings

### Slice 4b: Restart Recovery Hardening

- seed Apple event streams from a bounded helper reconciliation snapshot
- smoke Gateway restart against a running Apple-backed agent
- verify lifecycle controls after restart
- retain the known limitation that already-running wait tasks cannot be
  reattached without a durable helper supervisor or Apple platform support

### Slice 4c: Web UI Operator Parity

- add and keep passing a macOS-only Playwright operator flow for
  create/inspect/DM/stop/delete
- keep the test explicit-gated behind `AGENCY_E2E_APPLE_CONTAINER_WEBUI=1`
- compare its behavior to the Firecracker operator flow before graduation
- record remaining divergences as Apple backend limitations, not generic
  runtime behavior

### Slice 5: Release Decision

- decide whether `apple-container` remains manual experimental, becomes
  documented beta, or is still blocked by Apple Container platform limits.
- keep it opt-in unless network, cleanup, doctor, and runtime contract evidence
  are stable across fresh homes.
