# Firecracker ASK Compliance Checklist

This checklist validates the Firecracker runtime path for the scoped 0.2.x core product line.

## Required Invariants

- [ ] Enforcement remains external to the agent workload VM.
- [ ] The workload VM reaches host services only through the per-agent vsock bridge to its enforcer.
- [ ] The vsock bridge exposes only the per-agent enforcer proxy/control targets.
- [ ] Host-only enforcer target endpoints are not projected into the workload VM environment.
- [ ] The enforcer runs with scoped per-agent config, auth, data, and audit paths.
- [ ] Runtime status exposes VM, enforcer, bridge, and body websocket health.
- [ ] Startup and restart fail closed when mediation or body readiness is unavailable.
- [ ] Stop/delete removes the VM process, enforcer process, vsock sockets, PID file, runtime config dir, and per-task rootfs.
- [ ] Audit records start, restart, mediation, model, and failure/recovery events.
- [ ] Credentials stay outside the workload VM except for the scoped runtime token projected through the per-agent contract.

## Automated Evidence

- Unit: `go test ./internal/hostadapter/agentruntime ./internal/hostadapter/runtimebackend ./internal/orchestrate`
- Web unit: `npm test -- Agents.test.tsx`
- Web build: `npm run build`
- Live Firecracker DM parity: `./scripts/e2e/firecracker-webui-smoke.sh manage`
- Live degraded restart recovery: `./scripts/e2e/firecracker-webui-smoke.sh recover`
- Live stop/delete cleanup: `./scripts/e2e/firecracker-webui-smoke.sh cleanup`
- Full live Web UI parity: `./scripts/e2e/firecracker-webui-smoke.sh all`
- Enforcer mode comparison: `./scripts/e2e/firecracker-enforcer-mode-compare.sh`
  - Captures timing/resource evidence and security evidence for both
    `host-process` and `microvm` modes.
  - Fails if workload transport is not `vsock_http`, the endpoint is not
    `vsock://2:<port>`, enforcer/bridge/body health is missing, host-only
    env targets are exposed in the workload manifest, or mediated DM audit
    evidence is absent. It records whether LLM audit markers are visible in
    the agent log API as secondary evidence.

## Current Evidence Map

- External enforcement: host enforcer supervisor starts one process per agent; Firecracker workload VM does not embed the enforcer.
- Complete mediation: workload transport is `vsock_http`; VM connects to CID 2 and the host UDS bridge forwards only to configured enforcer targets.
- Boundary hygiene: Firecracker host target env keys are filtered before guest rootfs init injection; guest env keeps only the vsock-facing runtime contract.
- Complete audit: live smoke verifies DM traffic through mediation; `agency log <agent>` should show `MEDIATION_PROXY`, `MEDIATION_WS`, security scan, and LLM events.
- Least privilege: enforcer launch spec uses per-agent auth/data/audit paths and loopback-only host ports.
- Visible/recoverable boundaries: `/runtime/status` reports `vm_state`, `enforcer_state`, `vsock_bridge_state`, `body_ws_connected`, process IDs, restart/crash counters, and last error.
- Stop/delete cleanup: agent deletion uses the backend-neutral runtime stop path before removing the agent directory, so Firecracker VM, enforcer, bridge, PID, config, and task rootfs cleanup runs for deletes as well as explicit stops.

## Manual Operator Checks

- [ ] `agency runtime status <agent>` reports `backend=firecracker`, `phase=running`, and `healthy=true`.
- [ ] Runtime details include `vm_state=running`, `enforcer_state=running`, `vsock_bridge_state=running`, and `body_ws_connected=true`.
- [ ] `agency runtime validate <agent>` returns valid.
- [ ] After `agency serve restart`, runtime status degrades with `not tracked`; web UI restart returns it to healthy.
- [ ] After `agency stop <agent>`, VM PID and enforcer PID from the prior manifest are no longer alive.
- [ ] After stop/delete, no files remain under `~/.agency/firecracker/<agent>`, `~/.agency/firecracker/tasks/<agent>`, or `~/.agency/firecracker/pids/<agent>.pid`.
