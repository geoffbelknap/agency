# Initial Setup

## Trigger

First-time deployment, new machine, or fresh environment after `agency admin destroy`.

## Prerequisites

- Runtime host checks pass (`agency admin doctor`)
- Go binary built or installed via Homebrew (`agency --version`)
- Host dependencies installed:
  - `.venv/bin/mitmdump` plus Agency's egress addon Python dependencies for host-managed egress
  - `mke2fs` from e2fsprogs for microVM rootfs creation

For source installs, run:

```bash
./scripts/install/host-dependencies.sh --check
```

If dependencies are missing, install them with:

```bash
./scripts/install/host-dependencies.sh
```

Runtime backend artifacts are verified by `agency setup` and `agency
quickstart`. Source checkouts can prepare them before setup with:

```bash
make apple-vf-helpers
./scripts/readiness/apple-vf-artifacts.sh

make firecracker-helpers
./scripts/readiness/firecracker-artifacts.sh
./scripts/readiness/firecracker-kernel-artifacts.sh
```

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

### 5. Verify capacity

```bash
agency infra capacity
```

Expected: shows host memory/CPU, max agents, available slots. Capacity is profiled during `agency setup` and written to `~/.agency/capacity.yaml`.

### 6. Run doctor

```bash
agency admin doctor
```

Expected: all checks pass including `host_capacity` and `network_pool`. No agents exist yet, so agent-specific checks are skipped.

### 7. Configure provider

```bash
agency creds set ANTHROPIC_API_KEY --value sk-ant-...
```

### 8. Create and start a test agent

```bash
agency create test-agent
agency start test-agent
agency show test-agent
```

Expected: agent status shows running/healthy.

### 9. Send a test message

```bash
agency send test-agent "Hello, confirm you're working."
```

Wait 10-15 seconds, then check the DM channel:

```bash
agency comms read dm-test-agent
```

### 10. Clean up test agent

```bash
agency stop test-agent
agency delete test-agent
```

## Verification

All of the following must be true:
- [ ] `~/.agency/` directory exists with `config.yaml`
- [ ] `config.yaml` has non-empty `token` and `egress_token`
- [ ] `~/.agency/capacity.yaml` exists with correct host profiling
- [ ] `agency infra status` shows all components healthy
- [ ] `agency infra capacity` shows available agent slots
- [ ] `agency admin doctor` passes all checks (including `host_capacity`, `network_pool`)
- [ ] At least one LLM provider credential is configured
- [ ] Test agent started, responded, and cleaned up successfully

## Next Steps

After initial setup is verified:

- [Routing & Providers](routing-and-providers.md) — add additional LLM providers
- [Notifications & Webhooks](experimental/notifications-and-webhooks.md) — set up operator alerting
- [Hub & Capabilities](experimental/hub-and-capabilities.md) — install connectors and capabilities
- [Mission Management](experimental/mission-management.md) — create standing instructions for agents

## Rollback

If setup fails partway through:

```bash
agency admin destroy --yes  # or pipe: echo y | agency admin destroy
agency setup
```
