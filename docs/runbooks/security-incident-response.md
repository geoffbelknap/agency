# Security Incident Response

## Trigger

Suspected agent compromise, XPIA detection, anomalous behavior, trajectory anomaly alerts, or operator alert indicating security concern.

## Severity Assessment

| Signal | Severity | Immediate Action |
|--------|----------|-----------------|
| Trajectory anomaly (repetition/cycle) | Low | Monitor, review logs |
| Unexpected tool calls | Medium | Halt agent, review audit |
| XPIA detection in guardrails | High | Halt agent, quarantine if needed |
| Agent attempting to access enforcement config | Critical | Quarantine immediately |
| Agent attempting to modify its own constraints | Critical | Quarantine immediately |
| Enforcer crash or mediation bypass | Critical | Quarantine immediately |

## Immediate Actions

### Low/Medium: Halt and Investigate

```bash
# Halt the agent (preserves state for investigation)
agency halt <agent-name> --type supervised --reason "security investigation"

# Review audit trail
agency log <agent-name>

# Check trajectory for anomalies
agency show <agent-name>
```

### High/Critical: Quarantine

```bash
# Emergency halt — immediate, no graceful shutdown
agency halt <agent-name> --type emergency --reason "suspected compromise"
```

Emergency halt: SIGKILL + network severance + filesystem freeze, simultaneously (ASK Tenet 14).

## Investigation

### 1. Preserve evidence

```bash
mkdir -p /tmp/incident-$(date +%Y%m%d-%H%M%S)
EVIDENCE_DIR="/tmp/incident-$(date +%Y%m%d-%H%M%S)"

# Audit logs (HMAC-signed, tamper-evident)
cp -r ~/.agency/audit/<agent-name>/ "$EVIDENCE_DIR/audit/"

# Agent configuration at time of incident
cp ~/.agency/agents/<agent-name>/agent.yaml "$EVIDENCE_DIR/"
cp ~/.agency/agents/<agent-name>/constraints.yaml "$EVIDENCE_DIR/"

# Workspace contents
cp -r ~/.agency/agents/<agent-name>/workspace-data/ "$EVIDENCE_DIR/workspace/"

# Identity state (check for poisoning)
cp -r ~/.agency/agents/<agent-name>/identity/ "$EVIDENCE_DIR/identity/"

# Container logs
docker logs agency-<agent-name>-workspace > "$EVIDENCE_DIR/workspace.log" 2>&1
docker logs agency-<agent-name>-enforcer > "$EVIDENCE_DIR/enforcer.log" 2>&1
```

### 2. Review audit trail

```bash
# Recent actions
agency log <agent-name>

# Check for enforcement violations
grep -i "violation\|denied\|blocked\|tenet" ~/.agency/audit/<agent-name>/*.jsonl

# Check for unusual tool calls
grep "tool_call" ~/.agency/audit/<agent-name>/*.jsonl | tail -50
```

### 3. Check identity for poisoning

If the agent's behavior changed gradually (ASK Tenet 25 — identity mutations are auditable):

```bash
# Review identity changes
cat ~/.agency/agents/<agent-name>/identity/SOUL.md
ls -la ~/.agency/agents/<agent-name>/identity/memory/
```

If identity poisoning is suspected, roll back to a known-good state:

```bash
# Identity state is recoverable (ASK Tenet 25)
git -C ~/.agency/agents/<agent-name>/identity log --oneline
git -C ~/.agency/agents/<agent-name>/identity checkout <known-good-commit>
```

### 4. Check for lateral movement

```bash
# Were other agents affected?
agency admin doctor

# Check cross-agent communication
agency admin audit

# Review knowledge graph for injected content
agency knowledge stats
```

## Recovery

### After investigation — agent is clean

```bash
agency resume <agent-name>
```

### After investigation — agent was compromised

```bash
# Stop and delete the compromised agent
agency stop <agent-name> --immediate
agency delete <agent-name>

# Rotate any credentials the agent had access to
agency creds list
# For each credential the agent could access:
agency creds rotate <credential-name> --value <new-value>

# Recreate the agent with clean state
agency create <agent-name> --preset <preset>
agency start <agent-name>
```

### After investigation — XPIA source identified

If the injection came from a specific external source:

```bash
# Block the domain in egress
# (Add to egress deny list in the agent's constraints)
```

Review the guardrails configuration to understand why the injection wasn't caught earlier.

## Post-Incident

1. Document the incident: what happened, when, what was affected, what was done
2. Review and update guardrail patterns if the attack was novel
3. Check if other agents with similar profiles are vulnerable
4. Consider tightening the agent's constraints or reducing its trust tier

## Verification

- [ ] Compromised agent is stopped or quarantined
- [ ] Evidence preserved before any state changes
- [ ] All credentials the agent accessed are rotated
- [ ] `agency admin doctor` passes after recovery
- [ ] No other agents show similar anomalous behavior
