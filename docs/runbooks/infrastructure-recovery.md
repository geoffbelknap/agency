# Infrastructure Recovery

## Trigger

One or more infrastructure components are down, unresponsive, or in an error state. Symptoms: agents can't start, "connection refused" errors, `agency infra status` shows unhealthy components.

## Diagnosis

### 1. Check infrastructure status

```bash
agency infra status
```

Identify which components are down: egress, comms, knowledge, intake, gateway-proxy, web.

### 2. Check Docker

```bash
docker info
docker ps -a --filter "label=agency.managed=true"
```

If Docker is not running, start it first:
```bash
# macOS: open Docker Desktop
# Linux: sudo systemctl start docker
```

### 3. Check for port conflicts

```bash
lsof -i :8200  # gateway
lsof -i :8201  # knowledge
lsof -i :8202  # comms
```

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

# Restart
agency infra up
agency infra status
```

### Gateway daemon not running

```bash
# Check if PID file exists
cat ~/.agency/gateway.pid

# If stale PID, remove it
rm -f ~/.agency/gateway.pid

# Restart daemon
agency serve &
# or just run any agency command — daemon auto-starts
agency infra status
```

### Network isolation broken

If `agency admin doctor` reports network isolation failures:

```bash
agency infra down
agency infra up
agency admin doctor
```

The startup sequence recreates networks with correct isolation settings (internal networks for agents, mediation network for enforcer-to-infra communication).

## Verification

- [ ] `agency infra status` shows all components healthy
- [ ] `agency admin doctor` passes all infrastructure checks
- [ ] `curl -sf http://localhost:8200/api/v1/health` returns OK
- [ ] Agents can be started: `agency start <test-agent>`

## Escalation

If infrastructure repeatedly fails to start:

1. Check Docker resource limits (`docker system df`, `docker system info`)
2. Check disk space (`df -h ~/.agency/`)
3. Check Docker logs: `docker logs agency-infra-comms` (or other component name)
4. Full reset: `agency admin destroy --yes && agency setup`
