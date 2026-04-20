---
title: "Troubleshooting"
description: "Common issues and how to resolve them."
---


Common issues and how to resolve them.

## First Step: Run Doctor

```bash
agency admin doctor
```

This checks all security guarantees and reports any issues. It's the fastest way to identify problems.

## Agent Won't Start

### "Infrastructure not running"

The shared infrastructure needs to be running before agents can start.

```bash
agency infra up
agency infra status        # Verify everything is healthy
```

### "Validation failed"

One of the agent's config files has an error. Check the error message — it includes the file path and what's wrong.

```bash
agency show my-agent       # Review agent configuration
```

Common causes:
- Invalid YAML syntax in `agent.yaml`, `constraints.yaml`, or `policy.yaml`
- Missing required fields
- Policy chain inconsistency (agent policy tries to expand what a higher level restricts)

### Container backend not running

Agency requires the selected container backend. Start the backend first, then retry:

```bash
# Docker on Linux
sudo systemctl start docker

# Docker on macOS / Windows
# Open Docker Desktop

# Podman
podman info --format json
```

### Podman On WSL2 Rootless Setup

If rootless Podman on WSL2 fails before Agency can start containers, first make
sure the WSL distro packages and user socket are installed:

```bash
sudo apt-get install -y podman uidmap slirp4netns fuse-overlayfs crun
systemctl --user enable --now podman.socket
curl --unix-socket "$XDG_RUNTIME_DIR/podman/podman.sock" http://d/v1.41/_ping
```

Expected output:

```text
OK
```

Then set Agency to the rootless socket:

```yaml
hub:
  deployment_backend: podman
  deployment_backend_config:
    host: /run/user/1000/podman/podman.sock
```

Replace `1000` with `id -u` if your user ID is different.

If you see a rootless port collision such as:

```text
rootlessport listen tcp 127.0.0.1:8204: bind: address already in use
```

upgrade Agency and rerun `agency infra up`. On WSL2 rootless Podman, Agency
publishes internal service access through the gateway proxy instead of
publishing duplicate direct host ports for those same services.

### Start Sequence Fails at a Specific Phase

The seven-phase start sequence is all-or-nothing. If it fails, check which phase:

| Phase | Common Issues |
|-------|--------------|
| **Verify** | Config file validation errors |
| **Enforcement** | Container backend issues, port conflicts, infrastructure not running |
| **Constraints** | Policy chain errors, missing policy templates |
| **Workspace** | Image build failures, backend resource limits |
| **Identity** | Missing or corrupted `identity.md` |
| **Body** | Image pull failures, mount permission issues |
| **Session** | Rare — usually indicates an internal error |

## Agent Not Responding

```bash
# Check if it's still running
agency status

# Check the audit log for errors
agency log my-agent

# Check for halt signals
agency show my-agent
```

If the agent is stuck:

```bash
agency stop my-agent             # Supervised halt
agency resume my-agent           # Resume
agency send my-agent "Continue"  # Re-send
```

If that doesn't work:

```bash
agency stop my-agent --immediate   # Force stop
agency start my-agent              # Fresh start
```

## Agent Blocked by ASK Tenet

When Agency blocks an operation, the error includes the ASK tenet number and explanation:

```
Error: ASK Tenet 3 violation — No unmediated path from agent to external resource.
```

This block is intentional. The right response is to adjust your approach, not work around it. Common scenarios:

| Tenet | Meaning | Resolution |
|-------|---------|------------|
| **Tenet 1** | Agent tried to modify enforcement | Change your approach — enforcement is external |
| **Tenet 2** | Action without trace | Ensure audit logging is enabled |
| **Tenet 3** | Direct external access | Route through egress proxy; use `agency grant` for credentials |
| **Tenet 4** | Excessive permissions | Reduce scope to minimum required |
| **Tenet 5** | Modifying governance | Use operator commands to change policies, not the agent |

## Infrastructure Issues

### Infrastructure Won't Start

```bash
# Check the selected container backend
agency infra status

# Optional backend-specific checks
docker info
podman info --format json

# Rebuild and retry
agency infra rebuild
agency infra up
```

### "Connection refused" Errors

Usually means infrastructure containers stopped unexpectedly:

```bash
agency infra status       # Check which components are down
agency infra up           # Restart
```

### Egress Proxy Issues

If agents can't reach external services:

```bash
# Check egress status
agency infra status

# Check domain allowlist
agency admin egress show my-agent

# Verify credentials are configured
agency cap list
```

## Credential Issues

### "Service not granted"

The agent doesn't have access to a service. Add the credential to the credential store:

```bash
agency creds set --name GITHUB_TOKEN --value ghp_... --kind service --scope agent:my-agent --protocol api-key
```

### "Invalid credentials"

The API key may be expired or incorrect. Rotate the credential:

```bash
agency creds rotate GITHUB_TOKEN --value ghp_new_value
agency creds test GITHUB_TOKEN
```

### API Key Not Working

Credentials are stored in the encrypted credential store (`~/.agency/credentials/store.enc`), not in agent containers. Check:

```bash
# List configured credentials
agency creds list

# Test a specific credential
agency creds test ANTHROPIC_API_KEY

# Show credential details (value redacted)
agency creds show ANTHROPIC_API_KEY
```

## Channel Issues

### Messages Not Appearing

```bash
# Check comms service
agency infra status

# Check channel exists
agency comms list

# Read with no filters
agency comms read my-channel
```

### Search Not Finding Results

Full-text search uses SQLite FTS5. Ensure:
- The comms service is running (`agency infra status`)
- Messages have been indexed (there may be a brief delay after sending)
- Your search query matches the message content (FTS5 uses word-based matching)

## Performance Issues

### Slow Agent Responses

Check rate limiting — the enforcer queues requests when providers return 429s:

```bash
agency admin doctor
agency infra status
```

If rate limiting is the issue:
- Reduce the number of concurrent agents
- Use lower-tier models for appropriate agents (see [Model Routing](/model-routing))
- Check your provider's rate limits

### High Memory Usage

Each agent container is limited (workspace: 512MB default, enforcer: 32MB). If you're running many agents:

```bash
agency list --active       # Check how many agents are running
agency status              # Overall resource view
```

Consider stopping idle agents or reducing concurrent agent count.

## Recovery

### Full Reset

If everything is broken and you want to start fresh:

```bash
agency admin destroy --yes    # Removes everything (preserves knowledge graph)
agency init --api-key $KEY    # Re-initialize
```

**Note:** `agency admin destroy` preserves the knowledge graph — organizational knowledge compounds over time and survives resets.

### Recovering an Agent's Work

Agent workspaces are stored at `~/.agency/agents/<name>/workspace-data/`. Even after stopping or deleting an agent, you can access files it created:

```bash
ls ~/.agency/agents/my-agent/workspace-data/
```

Audit logs are at `~/.agency/audit/<name>/` and are preserved even after agent deletion.

### Corrupt State

If an agent is in an inconsistent state:

```bash
agency stop my-agent --immediate    # Force stop
agency delete my-agent              # Remove
agency create my-agent --preset engineer  # Recreate
agency start my-agent               # Fresh start
```

The agent's workspace data and memory are preserved through delete/recreate if you don't manually remove them.

## Getting Help

```bash
agency --help                # Top-level help
agency <command> --help      # Help for specific command
agency admin doctor          # Automated health check
```
