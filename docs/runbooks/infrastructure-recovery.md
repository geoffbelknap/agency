# Infrastructure Recovery

## Trigger

One or more infrastructure components are down, unresponsive, or in an error state. Symptoms: agents can't start, "connection refused" errors, `agency infra status` shows unhealthy components.

## Diagnosis

### 1. Check infrastructure status

```bash
agency infra status
```

Identify which components are down: egress, comms, knowledge, intake, web-fetch, web, embeddings, gateway-proxy.

### 2. Check container backend

```bash
agency infra status
```

If the selected container backend is Docker, verify Docker directly:
```bash
docker info
docker ps -a --filter "label=agency.managed=true"
```

If the selected container backend is Podman, verify Podman directly:

```bash
podman info --format json
podman ps -a --filter "label=agency.managed=true"
```

If the backend is not running, start it first before retrying recovery.

### 3. Check for port conflicts

```bash
lsof -i :8200  # gateway REST API
lsof -i :8280  # web UI
```

Note: knowledge and comms run inside the selected container backend and communicate over internal managed networks — they don't expose ports on the host.

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

# Clean orphaned Docker resources
docker ps -a --filter "label=agency.managed=true" -q | xargs -r docker rm -f
docker network ls --filter "label=agency.managed=true" -q | xargs -r docker network rm

# Or clean orphaned Podman resources
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

The startup sequence recreates the hub-and-spoke network topology: `agency-gateway` (internal bridge) as the hub connecting gateway-proxy, comms, knowledge, and intake; `agency-egress-int` (internal) for services needing egress access; `agency-egress-ext` (external) for egress proxy internet access; `agency-operator` (external) for web UI and relay; per-agent `agency-<name>-internal` networks for workspace↔enforcer. Internal networks enforce `Internal:true` (no external route).

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

- [Hub & Capabilities](hub-and-capabilities.md) — connector and web-fetch issues
- [Routing & Providers](routing-and-providers.md) — provider connectivity, egress
- [Notifications & Webhooks](notifications-and-webhooks.md) — event bus, intake

## Escalation

If infrastructure repeatedly fails to start:

1. Check Docker resource limits (`docker system df`, `docker system info`)
2. Check disk space (`df -h ~/.agency/`)
3. Check Docker logs: `docker logs agency-infra-comms` (or other component name)
4. Full reset: `agency admin destroy --yes && agency setup`
