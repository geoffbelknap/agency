# Firecracker ASK Compliance Checklist

This checklist validates the Firecracker runtime path for the scoped 0.2.x core product line.

## Required Invariants

- [ ] Enforcement remains external to the agent workload VM.
- [ ] The workload VM reaches host services only through the per-agent vsock bridge to its enforcer.
- [ ] The vsock bridge exposes only the per-agent enforcer proxy/control targets.
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
- Live Firecracker DM parity:
  `AGENCY_E2E_FIRECRACKER_WEBUI=1 npx playwright test -c playwright.live.risky.config.ts tests/e2e-live-risky/firecracker-webui-smoke.spec.ts -g "managed and messaged"`
- Live degraded restart recovery:
  `AGENCY_E2E_FIRECRACKER_WEBUI=1 npx playwright test -c playwright.live.risky.config.ts tests/e2e-live-risky/firecracker-webui-smoke.spec.ts -g "degraded runtime"`
- Live stop/delete cleanup:
  `AGENCY_E2E_FIRECRACKER_WEBUI=1 npx playwright test -c playwright.live.risky.config.ts tests/e2e-live-risky/firecracker-webui-smoke.spec.ts -g "clean up per-agent"`

## Current Evidence Map

- External enforcement: host enforcer supervisor starts one process per agent; Firecracker workload VM does not embed the enforcer.
- Complete mediation: workload transport is `vsock_http`; VM connects to CID 2 and the host UDS bridge forwards only to configured enforcer targets.
- Complete audit: live smoke verifies DM traffic through mediation; `agency log <agent>` should show `MEDIATION_PROXY`, `MEDIATION_WS`, security scan, and LLM events.
- Least privilege: enforcer launch spec uses per-agent auth/data/audit paths and loopback-only host ports.
- Visible/recoverable boundaries: `/runtime/status` reports `vm_state`, `enforcer_state`, `vsock_bridge_state`, `body_ws_connected`, process IDs, restart/crash counters, and last error.

## Manual Operator Checks

- [ ] `agency runtime status <agent>` reports `backend=firecracker`, `phase=running`, and `healthy=true`.
- [ ] Runtime details include `vm_state=running`, `enforcer_state=running`, `vsock_bridge_state=running`, and `body_ws_connected=true`.
- [ ] `agency runtime validate <agent>` returns valid.
- [ ] After `agency serve restart`, runtime status degrades with `not tracked`; web UI restart returns it to healthy.
- [ ] After `agency stop <agent>`, VM PID and enforcer PID from the prior manifest are no longer alive.
- [ ] After stop/delete, no files remain under `~/.agency/firecracker/<agent>`, `~/.agency/firecracker/tasks/<agent>`, or `~/.agency/firecracker/pids/<agent>.pid`.
