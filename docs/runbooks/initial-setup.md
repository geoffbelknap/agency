# Initial Setup

## Trigger

First-time deployment, new machine, or fresh environment after `agency admin destroy`.

## Prerequisites

- Docker running (`docker info` succeeds)
- Go binary built or installed via Homebrew (`agency --version`)

## Steps

### 1. Run setup

```bash
agency setup
```

This creates `~/.agency/`, generates auth tokens, starts the daemon, and brings up infrastructure.

### 2. Verify daemon is running

```bash
curl -sf http://localhost:8200/api/v1/health
```

Expected: `{"status":"ok","version":"...","build_id":"..."}`

### 3. Verify infrastructure

```bash
agency infra status
```

Expected: egress, comms, knowledge, intake, web-fetch, web, embeddings all showing healthy/running.

### 4. Verify auth token exists

```bash
grep '^token:' ~/.agency/config.yaml
```

Expected: non-empty hex string. If missing, `config.Load()` auto-generates one on next daemon start.

### 5. Run doctor

```bash
agency admin doctor
```

Expected: all checks pass. No agents exist yet, so agent-specific checks are skipped.

### 6. Configure provider

```bash
agency creds set --name ANTHROPIC_API_KEY --value sk-ant-... --kind provider --protocol api-key --scope platform
```

### 7. Create and start a test agent

```bash
agency create test-agent
agency start test-agent
agency show test-agent
```

Expected: agent status shows running/healthy.

### 8. Send a test message

```bash
agency send test-agent "Hello, confirm you're working."
```

Wait 10-15 seconds, then check:

```bash
agency channel read general
```

### 9. Clean up test agent

```bash
agency stop test-agent
agency delete test-agent
```

## Verification

All of the following must be true:
- [ ] `~/.agency/` directory exists with `config.yaml`
- [ ] `config.yaml` has non-empty `token` and `egress_token`
- [ ] `agency infra status` shows all components healthy
- [ ] `agency admin doctor` passes all checks
- [ ] At least one LLM provider credential is configured
- [ ] Test agent started, responded, and cleaned up successfully

## Rollback

If setup fails partway through:

```bash
agency admin destroy --yes
agency setup
```
