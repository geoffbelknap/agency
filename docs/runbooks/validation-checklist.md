# Validation Checklist

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

Expected: all phases pass. Covers: setup, infrastructure, agent lifecycle, credentials, missions, hub, knowledge, auth enforcement.

## Manual Validation Checklist

Run through each section. Mark each item as you verify it.

### Infrastructure

- [ ] `agency infra status` — all components healthy
- [ ] `agency admin doctor` — all checks pass
- [ ] `curl -sf http://localhost:8200/api/v1/health` — returns OK
- [ ] Gateway PID file exists: `cat ~/.agency/gateway.pid`

### Auth

- [ ] Authenticated request works: `curl -sf -H "Authorization: Bearer $(grep '^token:' ~/.agency/config.yaml | awk '{print $2}')" http://localhost:8200/api/v1/agents`
- [ ] Unauthenticated request rejected: `curl -s -o /dev/null -w '%{http_code}' http://localhost:8200/api/v1/agents` returns 401
- [ ] Health endpoint works without auth: `curl -sf http://localhost:8200/api/v1/health`

### Agent Lifecycle

- [ ] `agency create validation-test` — agent directory created
- [ ] `agency start validation-test` — agent starts, containers running
- [ ] `agency show validation-test` — shows running state
- [ ] `agency send validation-test "Hello"` — message delivered
- [ ] `agency halt validation-test --tier supervised --reason "validation"` — agent halts
- [ ] `agency resume validation-test` — agent resumes
- [ ] `agency stop validation-test` — agent stops
- [ ] `agency delete validation-test` — agent removed

### Credentials

- [ ] `agency creds set --name validation-test-key --value test123 --kind internal --protocol api-key --scope platform` — stored
- [ ] `agency creds list` — shows the key
- [ ] `agency creds show validation-test-key` — retrievable
- [ ] `agency creds delete validation-test-key` — removed

### Knowledge Graph

- [ ] `agency knowledge stats` — accessible, shows counts
- [ ] `agency knowledge ontology show` — returns ontology types

### Hub

- [ ] `agency hub update` — fetches registry
- [ ] `agency hub search <query>` — returns results or empty

### Missions

- [ ] `agency mission list` — accessible
- [ ] Create, assign, and delete a test mission (see E2E script for exact commands)

### Version Consistency

- [ ] `agency --version` — shows expected version
- [ ] `agency status` — no version mismatches between binary and containers

## Periodic Health Schedule

| Frequency | What to Run |
|-----------|-------------|
| Every deploy | Full checklist above |
| Daily | `agency admin doctor` + `agency infra status` |
| Weekly | `go test ./...` + review audit logs |
| Monthly | Full checklist + credential rotation review |
