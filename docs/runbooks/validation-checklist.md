# Validation Checklist

> Status: Mixed operator checklist. Use the core sections as the default
> `0.2.x` release and health path. Hub, missions, notifications, registry, and
> other broader platform checks are experimental or internal and should only be
> validated when those surfaces are intentionally enabled.

## Trigger

Post-deployment, post-upgrade, periodic health verification, or after any significant infrastructure change.

## Automated Validation

### Go test suite (no Docker required)

```bash
go test ./...
```

Expected: all packages pass. Covers: route wiring, auth enforcement, config auto-generation, module isolation, conditional registration, unauthenticated paths.

### E2E test (Docker required)

```bash
go build -o agency ./cmd/gateway/
./test_e2e.sh
```

Expected: all phases pass. Covers the default setup, infrastructure, agent
lifecycle, credentials, knowledge, and auth path. Experimental surfaces should
be validated separately when enabled.

## Manual Validation Checklist

Run through each section. Mark each item as you verify it.

### Infrastructure

- [ ] `agency infra status` — all components healthy (including gateway-proxy)
- [ ] `agency admin doctor` — all checks pass (including `host_capacity`, `network_pool`)
- [ ] `agency infra capacity` — shows available agent slots, correct host profiling
- [ ] `curl -sf http://localhost:8200/api/v1/health` — returns OK
- [ ] Gateway daemon running: `pgrep -af "agency.*serve"` shows a process
- [ ] Hub network exists: `docker network inspect agency-gateway` — Internal:true

### Auth

- [ ] Authenticated request works: `curl -sf -H "Authorization: Bearer $(grep '^token:' ~/.agency/config.yaml | awk '{print $2}')" http://localhost:8200/api/v1/agents`
- [ ] Unauthenticated request rejected: `curl -s -o /dev/null -w '%{http_code}' http://localhost:8200/api/v1/agents` returns 401
- [ ] Health endpoint works without auth: `curl -sf http://localhost:8200/api/v1/health`

### Agent Lifecycle

- [ ] `agency create validation-test` — agent directory created
- [ ] `agency start validation-test` — agent starts, containers running
- [ ] `agency show validation-test` — shows running state
- [ ] `agency send validation-test "Hello"` — message delivered (check `agency comms read dm-validation-test`)
- [ ] `agency halt validation-test --tier supervised --reason "validation"` — agent halts (status shows "paused")
- [ ] `agency resume validation-test` — agent resumes
- [ ] `agency stop validation-test` — agent stops
- [ ] `agency delete validation-test` — agent removed

### Credentials

- [ ] `agency creds set validation-test-key --value test123 --kind internal` — stored
- [ ] `agency creds list` — shows the key
- [ ] `agency creds show validation-test-key` — retrievable
- [ ] `agency creds delete validation-test-key` — removed

### Knowledge Graph

- [ ] `agency graph stats` — accessible, shows counts
- [ ] `agency graph ontology show` — returns ontology types

### Hub

- [ ] `agency hub update` — fetches registry
- [ ] `agency hub search <query>` — returns results or empty

### Missions

- [ ] `agency mission list` — accessible
- [ ] Create, assign, and delete a test mission (see E2E script for exact commands)

### Version Consistency

- [ ] `agency --version` — shows expected version
- [ ] `agency status` — no version mismatches between binary and containers

### Error Verification

After running the lifecycle checks above, verify no errors occurred:

**Gateway log:**
- [ ] No errors: `tail -100 ~/.agency/gateway.log | grep -c ERRO` returns 0
- [ ] No panics: `tail -100 ~/.agency/gateway.log | grep -c panic` returns 0
- [ ] No container crashes: `tail -100 ~/.agency/gateway.log | grep "container died"` returns nothing

**Infra container logs:**
- [ ] No errors in knowledge: `docker logs --since 1h agency-infra-knowledge 2>&1 | grep -ciE "^ERROR:|Traceback"` returns 0
- [ ] No errors in egress: `docker logs --since 1h agency-infra-egress 2>&1 | grep -ciE "^ERROR:|Traceback"` returns 0
- [ ] No errors in comms: `docker logs --since 1h agency-infra-comms 2>&1 | grep -ciE "^ERROR:|Traceback"` returns 0
- [ ] No errors in intake: `docker logs --since 1h agency-infra-intake 2>&1 | grep -ciE "^ERROR:|Traceback"` returns 0

**Docker state:**
- [ ] No exited infra containers: `docker ps -a --filter "label=agency.managed=true" --filter "status=exited"` shows only cleanup artifacts
- [ ] No restarting containers: `docker ps -a --filter "label=agency.managed=true" --filter "status=restarting"` is empty

**Docker socket audit:**
- [ ] No containers with Docker socket mounts: `agency admin doctor` passes the `docker_socket_audit` check (gateway runs `AuditDockerSocket()` at startup)

**Platform health:**
- [ ] `agency admin doctor` reports zero failures (no ✗ lines)
- [ ] `agency admin usage` shows zero errors (or no calls if no provider keys configured)

### Routing & Providers

- [ ] `agency infra providers` — lists configured providers
- [ ] `agency infra routing stats` — routing data present (if calls have been made)
- [ ] `agency infra routing suggestions` — no critical suggestions pending

### Notifications

- [ ] `agency notifications list` — shows expected destinations
- [ ] `agency notifications test <name>` — delivery works

### Capabilities

- [ ] `agency cap list` — shows expected capabilities
- [ ] Web-fetch service operational (if enabled): `agency infra status` includes web-fetch

### Registry

- [ ] `agency registry list` — shows expected principals
- [ ] No suspended principals that should be active

## Periodic Health Schedule

| Frequency | What to Run |
|-----------|-------------|
| Every deploy | Full checklist above |
| Daily | `agency admin doctor` + `agency infra status` + `agency infra capacity` |
| Weekly | `go test ./...` + review audit logs + `agency infra routing suggestions` |
| Monthly | Full checklist + credential rotation review + `agency admin usage` review |
