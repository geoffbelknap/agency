# Agent Recovery

## Trigger

Agent is crashed, stuck, unresponsive, halted unexpectedly, or in a corrupted state.

## Diagnosis

### 1. Check agent status

```bash
agency show <agent-name>
```

Look for: state (running/paused/stopped), halt reason, last activity timestamp. Note: halted agents show status "paused".

### 2. Check audit log

```bash
agency log <agent-name>
```

Look for: error signals, halt events, budget exhaustion, trajectory anomalies.

### 3. Check container state

```bash
docker ps -a --filter "name=agency-<agent-name>"
```

Look for: exited containers, restart loops, OOMKilled.

## Recovery Procedures

### Agent unresponsive (still running)

```bash
# Graceful halt
agency halt <agent-name> --tier supervised --reason "unresponsive recovery"

# Wait 5 seconds
agency show <agent-name>

# Resume
agency resume <agent-name>
```

### Agent stuck in halt state

```bash
# Check who halted it and why
agency show <agent-name>

# Resume (requires equal or higher authority than who halted it)
agency resume <agent-name>
```

If resume fails with authority error — the halt was initiated by a higher-authority principal. Contact the operator who halted it.

### Agent crashed (container exited)

```bash
# Check exit reason
docker inspect agency-<agent-name>-workspace --format '{{.State.ExitCode}} {{.State.OOMKilled}}'

# Restart
agency stop <agent-name>    # clean up state
agency start <agent-name>   # fresh start
```

If OOMKilled: the agent exceeded its memory limit. Check if the task requires more memory or if there's a memory leak in the tools being used.

### Agent in restart loop

```bash
# Force halt then stop
agency halt <agent-name> --tier immediate --reason "restart loop"
agency stop <agent-name>

# Check logs for the crash cause
agency log <agent-name>
docker logs agency-<agent-name>-workspace 2>&1 | tail -50

# Fix the underlying issue, then restart
agency start <agent-name>
```

### Budget exhausted

```bash
# Check budget state
agency show <agent-name>

# Budget exhaustion is a hard stop — the agent cannot continue
# Either increase the budget or create a new task
agency stop <agent-name>
agency start <agent-name>
```

### Corrupted agent state

```bash
# Rebuild derived files (manifest, services, PLATFORM.md, FRAMEWORK.md)
agency admin rebuild <agent-name>

# If that doesn't fix it, recreate the agent
agency halt <agent-name> --tier immediate --reason "corrupted state"
agency stop <agent-name>
agency delete <agent-name>
agency create <agent-name> --preset <original-preset>
agency start <agent-name>
```

Workspace data is at `~/.agency/agents/<name>/workspace-data/` and survives delete/recreate if not manually removed.

### Enforcer not running

```bash
# Check enforcer container
docker ps -a --filter "name=agency-<agent-name>-enforcer"

# Enforcer crash = mediation broken = agent must be stopped (ASK Tenet 3)
agency stop <agent-name>
agency start <agent-name>   # restart recreates enforcer
```

The workspace crash watcher detects enforcer crashes and emits operator alerts automatically.

## Verification

- [ ] `agency show <agent-name>` shows running/healthy
- [ ] `agency admin doctor` passes for this agent
- [ ] Agent responds to a test message: `agency send <agent-name> "Health check"`
- [ ] Audit log shows normal operation: `agency log <agent-name>`

## Preserving Evidence

Before destroying a problematic agent's state:

```bash
# Export audit log
cp -r ~/.agency/audit/<agent-name>/ /tmp/agent-evidence/

# Export workspace
cp -r ~/.agency/agents/<agent-name>/workspace-data/ /tmp/agent-evidence/workspace/

# Export agent config
cp ~/.agency/agents/<agent-name>/agent.yaml /tmp/agent-evidence/
cp ~/.agency/agents/<agent-name>/constraints.yaml /tmp/agent-evidence/
```
