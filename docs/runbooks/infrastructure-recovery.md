# Infrastructure Recovery

## Trigger

One or more infrastructure components are down, unresponsive, or in an error state. Symptoms: agents can't start, "connection refused" errors, `agency infra status` shows unhealthy components.

## Diagnosis

### 1. Check infrastructure status

```bash
agency infra status
```

Identify which components are down: egress, comms, knowledge, intake, web-fetch, web, embeddings, gateway-proxy.

### 2. Check runtime and shared-service health

```bash
agency infra status
agency admin doctor
```

On Linux, Firecracker host checks should confirm KVM and vsock access. On
macOS Apple silicon, the strategic runtime path is `apple-vf-microvm` once the
Apple Virtualization backend is complete.

If a transitional container backend was selected intentionally, verify that
backend directly before retrying recovery.

### 3. Check for port conflicts

```bash
lsof -i :8200  # gateway REST API
lsof -i :8280  # web UI
```

Note: knowledge and comms are shared Agency services and should normally be
reached through the gateway path rather than direct host ports.

## Recovery Procedures

### Single component down

```bash
agency infra rebuild <component>
# e.g.: agency infra rebuild comms
```

Wait 10 seconds, then verify:
```bash
agency infra status
```

### Multiple components down

```bash
agency infra down
agency infra up
```

Wait for all components to become healthy (30-60 seconds):
```bash
agency infra status
```

### All infrastructure unresponsive

```bash
# Stop everything
agency infra down

# Legacy Docker cleanup, only when the Docker backend was explicitly selected
docker ps -a --filter "label=agency.managed=true" -q | xargs -r docker rm -f
docker network ls --filter "label=agency.managed=true" -q | xargs -r docker network rm

# Legacy Podman cleanup, only when the Podman backend was explicitly selected
podman ps -a --filter "label=agency.managed=true" -q | xargs -r podman rm -f
podman network ls --filter "label=agency.managed=true" -q | xargs -r podman network rm

# Restart
agency infra up
agency infra status
```

### Gateway daemon not running

```bash
# Check if daemon process is running
pgrep -af "agency.*serve"

# If stale PID file exists, remove it
rm -f ~/.agency/gateway.pid

# Restart daemon (any agency command auto-starts the daemon)
agency infra status
# or explicitly:
agency serve restart
```

### Network isolation broken

If `agency admin doctor` reports network isolation failures:

```bash
agency infra down
agency infra up
agency admin doctor
```

For legacy container backends, the startup sequence recreates the hub-and-spoke
network topology: `agency-gateway` (internal bridge) as the hub connecting
gateway-proxy, comms, knowledge, and intake; `agency-egress-int` (internal) for
services needing egress access; `agency-egress-ext` (external) for egress proxy
internet access; `agency-operator` (external) for web UI and relay; per-agent
`agency-<name>-internal` networks for workspace<->enforcer. Internal networks
enforce `Internal:true` (no external route).

### Capacity limit reached

If `agency start` fails with "no available agent slots":

```bash
agency infra capacity
```

Check `available_slots`. Either stop unused agents or adjust `~/.agency/capacity.yaml` if the host has more resources than initially profiled.

## Verification

- [ ] `agency infra status` shows all components healthy
- [ ] `agency infra capacity` shows available agent slots
- [ ] `agency admin doctor` passes all infrastructure checks
- [ ] `curl -sf http://localhost:8200/api/v1/health` returns OK
- [ ] Agents can be started: `agency start <test-agent>`

## See Also

- [Hub & Capabilities](experimental/hub-and-capabilities.md) — connector and web-fetch issues
- [Routing & Providers](routing-and-providers.md) — provider connectivity, egress
- [Notifications & Webhooks](experimental/notifications-and-webhooks.md) — event bus, intake

## Escalation

If infrastructure repeatedly fails to start:

1. Check runtime host readiness (`agency admin doctor`)
2. Check disk space (`df -h ~/.agency/`)
3. If using a legacy container backend, check backend logs for the affected component
4. Full reset: `agency admin destroy --yes && agency setup`
