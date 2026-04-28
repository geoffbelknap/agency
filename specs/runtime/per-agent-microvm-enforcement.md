# Per-Agent MicroVM Enforcement

## Status

Draft. Architectural decision recorded 2026-04-28. This spec supersedes
the earlier assumption that the Firecracker parity path should recreate
the container-era `workspace` + `enforcer` pair as containers or as one
combined VM.

Update 2026-04-28: `host-process` mode has reached Web UI parity for the
Firecracker backend. The next implementation track is `microvm` mode:
one Firecracker microVM for the agent workload and one separate
Firecracker microVM for that agent's enforcer boundary.

## Decision

Agency's Linux production runtime target is:

- one Firecracker microVM per agent workload
- one external enforcer boundary per agent
- shared Agency infrastructure as host services

The enforcer boundary is not optional. The agent workload VM must not
contain its own enforcer, and it must not reach comms, knowledge, provider
APIs, tools, egress, or gateway service sockets directly.

Two compliant enforcer substrates will be evaluated:

- `host-process`: one host process per agent, supervised by Agency
- `microvm`: one additional Firecracker microVM per agent, supervised by
  Agency

Both modes must expose the same runtime lifecycle to the rest of Agency:
`Ensure`, `Stop`, `Inspect`, `Validate`, reload, and status reporting. The
generic runtime contract must not know which enforcement substrate is in
use.

## ASK Constraints

The runtime is compliant only if these invariants hold:

- **External enforcement**: the enforcer runs outside the agent workload
  VM. A compromised agent VM cannot modify, bypass, or terminate its
  enforcer.
- **Complete mediation**: the agent VM has exactly one control-plane route:
  vsock to its assigned enforcer. All comms, knowledge, LLM/provider,
  tool, runtime, and egress operations go through that enforcer.
- **Complete audit**: mediated operations are written through the
  enforcer audit path with agent identity, runtime ID, enforcer instance
  identity, request ID where available, and target service.
- **Explicit least privilege**: each per-agent enforcer receives only that
  agent's scoped auth material, policy, services metadata, routing config,
  audit directory, and data directory.
- **Visible and recoverable boundaries**: operators can inspect workload
  VM state, enforcer state, vsock bridge state, config revision, audit
  path, and teardown status per agent.

The `shared-enforcer` and `enforcer-inside-agent-vm` shapes are not default
targets. `enforcer-inside-agent-vm` is not ASK-compliant for production
because enforcement would share the agent compromise boundary.

## Host-Service Infra

The long-term default should remove container dependency from shared
infrastructure:

- gateway
- web
- comms
- knowledge
- egress
- supervisor/runtime manager
- optional services such as web-fetch when enabled

These can be host services while preserving ASK if each service has
explicit identity, filesystem scope, socket ownership, health, logs,
restart policy, and audit responsibilities. Containers may remain as a
transition mechanism, but containerization is not the architecture.

For the first Firecracker Web UI parity milestone, containerized infra may
remain in place to reduce variables. The production target is host-service
infra plus per-agent workload microVMs and per-agent external enforcers.

## Runtime Topology

### `host-process` mode

```text
host:
  agency-gateway
  agency-web
  agency-comms
  agency-knowledge
  agency-egress
  agency-supervisor
  agency-enforcer/<agent-id>

microVM:
  agency-agent-workload/<agent-id>

agent workload VM --vsock--> per-agent host enforcer --> host services
```

Expected strengths:

- fastest path to Web UI parity
- low memory overhead
- simple logs, process supervision, and health checks
- easier development and debugging

Expected risks:

- weaker isolation than an enforcer microVM
- depends on OS process identity, file permissions, and socket ownership
  being correct
- a host-process enforcer bug has host-process blast radius, although it
  is still outside the agent VM

### `microvm` mode

```text
host:
  agency-gateway
  agency-web
  agency-comms
  agency-knowledge
  agency-egress
  agency-supervisor

microVM:
  agency-enforcer/<agent-id>

microVM:
  agency-agent-workload/<agent-id>

agent workload VM --controlled bridge--> enforcer VM --> host services
```

Expected strengths:

- stronger per-agent enforcement isolation
- clearer high-assurance story
- lower host-process blast radius

Expected risks:

- roughly doubles VM count for agents
- more complex vsock routing, startup order, health, logs, and cleanup
- higher cold-start and memory cost

## Configuration

Initial Firecracker config should carry the enforcement-mode choice under
backend-specific config:

```yaml
hub:
  deployment_backend: firecracker
  deployment_backend_config:
    binary_path: /usr/local/bin/firecracker
    kernel_path: /var/lib/agency/firecracker/vmlinux
    state_dir: /var/lib/agency/firecracker
    enforcement_mode: host-process # host-process | microvm
```

Default while experimental: `host-process`.

Graduation requirement: both modes are benchmarked and the production
default is chosen with evidence. If `microvm` is not ready when Firecracker
graduates, it remains experimental as a high-isolation mode.

## Lifecycle

For both modes, `Ensure(agent)` must:

1. prepare per-agent enforcer config, auth material, audit/data dirs, and
   service metadata
2. start the per-agent enforcer boundary
3. start the vsock bridge that exposes only the enforcer control plane to
   the workload VM
4. realize the workload OCI image as a bootable rootfs
5. start the workload VM
6. validate enforcer health, VM state, and bridge reachability

`Stop(agent)` must:

1. stop the workload VM with finite SIGTERM/SIGKILL escalation
2. stop and unlink the vsock bridge
3. stop the per-agent enforcer boundary
4. remove ephemeral per-agent runtime state
5. leave durable agent state and audit intact

`Inspect(agent)` must report at least:

- workload state
- enforcer state
- bridge state
- last error
- restart/crash counters
- enforcement mode

`Validate(agent)` fails closed if any required boundary is missing or if
the workload VM can reach a host service without its enforcer.

## Web UI Parity Target

The Web UI parity milestone is complete when an operator can:

1. create or select an agent
2. start it using the Firecracker backend
3. see healthy runtime status
4. open a DM
5. send a message
6. receive a mediated response
7. restart the agent
8. stop the agent
9. confirm no workload VM, enforcer process/VM, vsock socket, or transient
   state remains

The same checklist applies to `host-process` and `microvm` enforcement
modes.

## Evaluation Criteria

Both modes should be compared with the same benchmark and parity harness:

- cold start to healthy runtime
- time to first DM response
- steady-state RSS per agent
- max concurrent agents on a representative Linux host
- restart recovery
- teardown completeness
- orphan process/VM/socket rate
- audit completeness
- credential boundary clarity
- operator debuggability

## Implementation Sequence

1. Add explicit Firecracker `enforcement_mode` parsing and status reporting.
2. Extract container enforcer setup into a substrate-neutral per-agent
   enforcer spec: env, paths, ports, health, reload, auth material.
3. Implement `host-process` enforcer supervisor using the existing Go
   enforcer binary.
4. Wire Firecracker `Ensure` to start host-process enforcer before the
   workload VM and bridge workload vsock only to that enforcer.
5. Make Firecracker runtime compile emit vsock-aware workload config.
6. Validate Web UI DM parity in `host-process` mode.
7. Implement `microvm` enforcer mode using the existing enforcer OCI image
   and the Firecracker image store/supervisor.
8. Run the same parity and scale harness against both modes.

## MicroVM Mode Implementation Track

The `microvm` mode should be developed as an implementation detail of the
Firecracker runtime backend, not as a new generic runtime shape. The
backend-neutral contract still exposes one agent runtime with one
transport endpoint and one status object. Internally, Firecracker owns two
component VMs for that runtime:

- `workload`: the agent body VM
- `enforcer`: the external mediation VM

The first implementation should land in small chunks:

1. **Component state model**
   - Add Firecracker-internal component roles for workload and enforcer
     VM state.
   - Keep public runtime status as one status object, with details such
     as `workload_vm_state`, `enforcer_vm_state`, `vsock_bridge_state`,
     and `enforcement_mode=microvm`.
   - Preserve current `host-process` status keys for compatibility while
     adding the clearer component keys.

2. **Enforcer VM spec compiler**
   - Convert the existing per-agent enforcer launch spec into an
     enforcer microVM boot spec.
   - Reuse `agency-enforcer:latest` as the OCI source image.
   - Deliver config/auth/service metadata through a read-only per-agent
     config artifact, not through host-only environment leakage.
   - Keep per-agent audit/data paths explicit and visible.

3. **Two-sided vsock bridge**
   - Workload VM sees only `vsock://2:<port>` endpoints for its assigned
     enforcer.
   - Host bridge forwards workload VM UDS ports only to that agent's
     enforcer VM ports.
   - Enforcer VM reaches host services through a separate host-side bridge
     whose targets are gateway, comms, knowledge, egress, and explicitly
     enabled services.
   - Workload VM must never receive direct host-service targets.

4. **Lifecycle ordering**
   - `Ensure` starts or updates the enforcer VM first.
   - Start the workload-to-enforcer bridge only after the enforcer VM is
     healthy.
   - Start the workload VM after the bridge is ready.
   - Validate workload VM, enforcer VM, and both bridge sides before
     reporting healthy.

5. **Stop and recovery**
   - Stop workload VM first, then bridge, then enforcer VM.
   - Cleanup removes both component configs, both task rootfs copies, PID
     files, and UDS sockets.
   - If the daemon restarts, the runtime must degrade visibly until the
     operator restarts or a recovery mechanism re-attaches.

6. **Parity and scale harness**
   - Run the existing Web UI smoke suite in `host-process` and `microvm`
     modes.
   - Capture cold start, first DM latency, steady RSS, disk usage, and
     cleanup results for both modes.

### MicroVM Mode Acceptance Criteria

`microvm` mode is acceptable for comparison only when:

- the agent workload VM cannot reach gateway, comms, knowledge, egress,
  provider APIs, or tool services except through its enforcer VM
- the enforcer VM has scoped per-agent config/auth/data/audit material
- runtime status shows workload VM, enforcer VM, and bridge health
- Web UI create/manage/DM/restart/stop/delete parity passes
- delete leaves no workload VM, enforcer VM, PID file, UDS socket,
  component config, or per-task rootfs behind
- audit records still show mediated DM and provider activity under the
  agent identity

### MicroVM Mode Open Decisions

- **VM-to-VM addressing**: Firecracker does not provide direct guest to
  guest vsock routing. The first version should route through explicit
  host-owned UDS bridges rather than adding a guest network.
- **Config delivery**: the enforcer VM needs read-only config/auth/service
  metadata. The safest first version is a generated per-agent config disk
  or rootfs injection; virtio-fs can be evaluated later.
- **Audit delivery**: audit can be written through a host-owned bridge or
  a writable per-agent audit disk. The first version should prefer the
  simpler path that preserves complete audit and teardown evidence.
- **CID allocation**: static guest CIDs are fine for one VM, but two VMs
  per agent require deterministic allocation and collision checks.
- **Egress path**: outbound network remains mediated by the enforcer; do
  not add workload VM tap/NAT direct egress as part of `microvm` mode.

## Non-Goals

- Making Firecracker default before Web UI parity and teardown checks pass.
- Running the enforcer inside the agent workload VM.
- Introducing a shared default enforcer for multiple agents.
- Moving every shared infra component to microVMs as part of the first
  agent parity milestone.
