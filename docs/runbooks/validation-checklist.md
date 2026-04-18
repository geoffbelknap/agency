# Validation Checklist

> Status: Mixed operator checklist. Use the core sections as the default
> `0.2.x` release and health path. Hub, missions, notifications, registry, and
> other broader platform checks are experimental or internal and should only be
> validated when those surfaces are intentionally enabled.

## Trigger

Post-deployment, post-upgrade, periodic health verification, or after any significant infrastructure change.

## Automated Validation

Before validating a local patch, make sure the binary you are exercising is the
one you just built. `agency` on `$PATH`, `./agency`, and the running daemon may
point at different builds.

Quick check:

```bash
./agency --version
agency --version
agency status
```

If you are validating a patched local binary, use `./agency` consistently or
stop the installed daemon before starting a disposable/local runtime.

### Go test suite (no Docker required)

```bash
go test ./...
```

Expected: all packages pass. Covers: route wiring, auth enforcement, config auto-generation, module isolation, conditional registration, unauthenticated paths.

### Branch protection verification

```bash
make verify-required-status-checks
```

Expected: the required checks on `main` still include `go-test`,
`python-unit-test`, `python-knowledge-test`, `web-test`, `docker-smoke`,
`podman-smoke`, and `containerd-smoke`.

### E2E test (Docker required)

```bash
go build -o agency ./cmd/gateway/
./test_e2e.sh
```

Expected: all phases pass. Covers the default setup, infrastructure, agent
lifecycle, credentials, knowledge, and auth path. Experimental surfaces should
be validated separately when enabled.

### Disposable live web E2E (recommended after runtime/lifecycle changes)

```bash
./scripts/e2e-live-disposable.sh --skip-build
```

Expected: all live tests pass against a disposable Agency home.

Covers:
- initialized app shell and operator routes
- runtime-backed agent create/start/pause/resume/restart flows
- first useful DM reply for a running agent
- webhook, notifications, presets, missions, and team flows

If Docker reports `all predefined address pools have been fully subnetted`,
clean leaked disposable runtimes before retrying:

```bash
AGENCY_BIN=./agency ./scripts/cleanup-live-test-runtimes.sh --apply
```

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
- [ ] `curl -sf -H "Authorization: Bearer $(grep '^token:' ~/.agency/config.yaml | awk '{print $2}')" http://localhost:8200/api/v1/agents/validation-test/runtime/status` — runtime status reports `phase=running`
- [ ] `curl -sf -H "Authorization: Bearer $(grep '^token:' ~/.agency/config.yaml | awk '{print $2}')" http://localhost:8200/api/v1/agents/validation-test/runtime/manifest` — runtime manifest exists and includes backend + transport
- [ ] `curl -sf -X POST -H "Authorization: Bearer $(grep '^token:' ~/.agency/config.yaml | awk '{print $2}')" http://localhost:8200/api/v1/agents/validation-test/runtime/validate` — runtime validates cleanly
- [ ] `agency send validation-test "Hello"` — message delivered (check `agency comms read dm-validation-test`)
- [ ] `agency halt validation-test --tier supervised --reason "validation"` — agent halts (status shows "paused")
- [ ] `agency resume validation-test` — agent resumes
- [ ] `agency restart validation-test` — agent re-enters the canonical startup flow and returns healthy
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

- [ ] `./agency --version` — shows expected local build when validating a patch
- [ ] `agency --version` — matches the installed/operator binary you intend to use
- [ ] `agency status` — no unintended version mismatches between binary and containers
- [ ] `make verify-required-status-checks` — branch protection still enforces the expected smoke and test gates on `main`

### Error Verification

After running the lifecycle checks above, verify no errors occurred:

**Gateway log:**
- [ ] No errors: `tail -100 ~/.agency/gateway.log | grep -c ERRO` returns 0
- [ ] No panics: `tail -100 ~/.agency/gateway.log | grep -c panic` returns 0
- [ ] No container crashes: `tail -100 ~/.agency/gateway.log | grep "container died"` returns nothing

**Infra container logs:**
- [ ] No errors in knowledge: `<backend-cli> logs --since 1h agency-infra-knowledge 2>&1 | grep -ciE "^ERROR:|Traceback"` returns 0
- [ ] No errors in egress: `<backend-cli> logs --since 1h agency-infra-egress 2>&1 | grep -ciE "^ERROR:|Traceback"` returns 0
- [ ] No errors in comms: `<backend-cli> logs --since 1h agency-infra-comms 2>&1 | grep -ciE "^ERROR:|Traceback"` returns 0
- [ ] No errors in intake: `<backend-cli> logs --since 1h agency-infra-intake 2>&1 | grep -ciE "^ERROR:|Traceback"` returns 0

Use `docker` or `podman` for `<backend-cli>` based on the selected container backend.

**Container backend state:**
- [ ] No exited infra containers: `<backend-cli> ps -a --filter "label=agency.managed=true" --filter "status=exited"` shows only cleanup artifacts
- [ ] No restarting containers: `<backend-cli> ps -a --filter "label=agency.managed=true" --filter "status=restarting"` is empty

**Docker socket audit:**
- [ ] If Docker is the selected backend, `agency admin doctor` passes the `docker_socket_audit` check (gateway runs `AuditDockerSocket()` at startup)

**Platform health:**
- [ ] `agency admin doctor` reports zero failures (no ✗ lines)
- [ ] `agency admin usage` shows zero errors (or no calls if no provider keys configured)
- [ ] DM reply path works for a disposable agent without duplicate follow-on execution

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
