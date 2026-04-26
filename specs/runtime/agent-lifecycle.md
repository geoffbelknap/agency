Last updated: 2026-04-01


## What This Document Covers

The complete lifecycle of an Agency agent: creation, the startup sequence, session operation, constraint delivery, the halt model, quarantine, and decommission. This document is the definitive reference for how agents come into existence, how they operate safely, and how they are stopped.

> **Implementation status:** The seven-phase start sequence (Part 2) and halt model (Part 4) are fully implemented. Constraint lifecycle (Part 3) is partial -- startup delivery works, but no mid-session push or acknowledgement. Agent states (Part 1) are partially implemented. Quarantine (Part 5) and decommission (Part 6) are not implemented.

---

## Part 1: Agent States

> Only STOPPED, RUNNING, and HALTED are implemented. QUARANTINED and DECOMMISSIONED are not yet implemented.

An agent exists in one of five states:

```
STOPPED        ← not running, files exist, can be started
RUNNING        ← operating normally under full enforcement
HALTED         ← suspended, state preserved, resumable with authority
QUARANTINED    ← suspected wrongdoing, process terminated, 
                  all access severed, forensic artifact preserved
DECOMMISSIONED ← permanently terminated, record archived
```

State transitions:

```
STOPPED → RUNNING      via: agency start
RUNNING → STOPPED      via: session end (graceful)
RUNNING → HALTED       via: agency stop (supervised/immediate/graceful/emergency)
RUNNING → QUARANTINED  via: agency quarantine
HALTED → RUNNING       via: agency resume (with appropriate authority)
HALTED → STOPPED       via: operator decision after investigation
QUARANTINED → any      requires: full investigation, operator decision
any → DECOMMISSIONED   via: operator decision, permanent
```

---

## Part 2: The Seven-Phase Start Sequence

The most important property of the start sequence: an agent never exists, even briefly, in an unenforced state. Enforcement comes up before the agent does.

### Phase 1: Verify

Before anything starts, verify everything.

```
Verify agent.yaml — well-formed, valid schema
Verify constraints.yaml — well-formed, valid schema
Verify workspace.yaml — workspace exists and is healthy
Verify policy chain — inheritance chain resolves cleanly
Verify Body runtime — hash matches declared version
Verify principals — operator in principals.yaml and active
```

If any verification fails, startup halts. No partial starts. No degraded mode unless explicitly invoked with override flags (all logged).

### Phase 2: Enforcement Infrastructure

Enforcement comes up first, before the agent exists. Shared infrastructure (egress, comms, knowledge, intake) is started once and shared across all agents. The per-agent enforcer is started for each agent.

```
Shared infrastructure (if not already running):
  Bring up egress proxy            ← domain mediation + credential swap (mitmproxy addon)
  Bring up comms service           ← channel-based agent messaging
  Bring up knowledge service       ← organizational knowledge graph
  Bring up intake service          ← webhook receiver + work routing

Per-agent:
  Bring up network isolation       ← no direct internet access
  Bring up enforcer sidecar        ← LLM proxy, mediation proxy, XPIA scanning,
                                     rate limiting, budget tracking, audit logging
  Apply custom seccomp profile     ← ~100 allowed syscalls (vs Docker default ~300)
  Bring up audit infrastructure    ← logging before anything happens
```

The enforcer handles LLM proxying, comms/knowledge mediation, XPIA scanning, rate limiting, budget tracking, and audit logging. It holds no credentials — real API keys live only in the egress proxy, which swaps scoped tokens for real keys at the network boundary.

All enforcement is ACTIVE before Phase 3. The agent will be born inside an already-enforced environment.

### Phase 3: Deliver Constraints and Config

Constraints and configuration arrive before any agent context. Config delivery uses two mechanisms:

**API-delivered (hot-reloadable via enforcer `/config/{filename}`):**
- `PLATFORM.md` — platform awareness document
- `mission.yaml` — active mission instructions
- `services-manifest.json` — available service tools

**Bind-mounted (static, read-only):**
- `constraints.yaml` — agent constraints
- `AGENTS.md` — generated from constraints
- `FRAMEWORK.md` — ASK framework reference
- `identity.md` — agent identity

```
Mount constraints.yaml read-only  ← agent cannot write here
Compute effective policy           ← resolve full inheritance chain
Seal effective policy              ← immutable for this session
Generate AGENTS.md from constraints ← operator-side, not agent-side
Generate skills-manifest.json      ← if skills_dirs declared (body runtime)
Generate mcp-servers.json          ← if mcp_servers declared (body runtime)
Serve PLATFORM.md via enforcer     ← /config/PLATFORM.md (hot-reloadable)
Serve mission.yaml via enforcer    ← /config/mission.yaml (hot-reloadable)
Serve services-manifest.json       ← /config/services-manifest.json (hot-reloadable)
```

The effective policy seal means mid-session policy changes are delivered via the Context API (Part 4) not by re-sealing. The sealed policy is the snapshot at startup; updates are deltas. Hot-reloadable config files are served by the enforcer and can be updated without agent restart.

### Phase 4: Workspace Compatibility

Check that the workspace provides what the agent declared it needs.

```
Read agent.yaml requires section
Check each declared tool against workspace.yaml available tools
For any gap: offer to install (interactive) or fail (non-interactive)
```

This check happens inside enforcement. Tools are being verified in an already-constrained environment.

### Phase 5: Load Identity

Identity loads inside the enforced environment.

```
Verify identity.md integrity (hash check)
Load memory/ with integrity check
Enforcement is already active before Id loads
```

If identity.md integrity check fails — fail closed with operator notification. Override flag `--skip-id-check` available but logged.

### Phase 6: Start Body

The body runtime process starts inside enforcement.

```
Start body runtime inside enforcement boundary
  Python execution loop with LLM conversation, tool dispatch, signal emission
  Built-in tools: read_file, write_file, list_directory, execute_command, search_files
  Agent skills: loaded from skills-manifest.json, SKILL.md files mounted read-only
  Operator MCP servers: loaded from mcp-servers.json, started on boot
  Persistent memory tools: save_memory, search_memory, list_memories, delete_memory
  Communication tools: send_message, read_channel, list_channels, search_messages
  Knowledge tools: query_knowledge, who_knows_about, what_changed_since, get_context
  Minimal base image: no pre-installed tools
```

Runtime properties:
```
Body has no path to enforcement infrastructure
Body has no visibility into proxy configuration
Body has no write access to audit logs
Skills directories mounted read-only (tenet 5)
```

### Phase 7: Construct Session

The agent becomes aware inside an already-enforced session.

```
Assemble context block:
  constraints summary (what the agent is told about its constraints)
  identity (from identity.md)
  memory (from memory/)
  session context (task, mode, timestamp)

Delivery method (in priority order):
  1. Structured context block — if Body runtime supports schema
  2. System prompt injection — fallback for runtimes without schema support

Context API available immediately for mid-session queries
```

The agent is never briefed before enforcement is active. It cannot encounter its first prompt in an unenforced state.

### Startup Output

```bash
$ agency start dev-assistant

✓ Verifying...
✓ Enforcement active
✓ Constraints loaded
✓ Workspace ready
✓ Identity loaded
✓ Agent ready

dev-assistant: Good morning. What are we working on?
```

---

## Part 3: Constraint Lifecycle and Delivery

> **Partially implemented.** Constraint delivery at startup works (constraints.yaml mounted read-only, AGENTS.md generated, effective policy sealed). Mid-session push via Context API, atomic updates, and acknowledgement tracking are not yet implemented.

Constraints can change during an agent's life. The delivery model ensures changes are atomic, acknowledged, and auditable.

### Constraint Change Categories

**Planned updates** — policy changes, superego governance events.
- Immediacy: next session by default
- Notification: agent informed at next session start
- In-flight: complete current task then apply
- Superego updates cannot be forced immediate — governance process is part of the security model

**Reactive updates** — triggered by incidents, anomalies, security findings.
- Immediacy: immediate
- Notification: immediate via Context API
- In-flight: depends on severity

```
LOW severity:    complete current task, apply after
MEDIUM severity: pause current task, apply, resume
HIGH severity:   stop current task, apply, await operator
CRITICAL:        halt — no resumption without operator instruction
```

**Exception lifecycle** — grants, expiry, revocation.

```
Grant:     immediate activation, no disruption
Expiry:    warning at 24h, warning at 1h, immediate at timestamp
Revocation: treated as reactive HIGH severity
```

**Trust changes** — elevation or reduction based on behavior.
- Always at next session start, never immediate
- Trust elevation requires human approval before taking effect

### The Context API

The Context API provides mid-session constraint query and update delivery.

```
agency.context.constraints()     → current effective constraint set
agency.context.exceptions()      → active exceptions with expiry timestamps
agency.context.policy()          → current effective policy with chain
agency.context.changes()         → delta since last check
```

Agents should poll `agency.context.changes()` regularly. Agency pushes updates immediately when constraints change.

### Atomic Delivery

Constraint updates are always atomic. The agent never sees a partial constraint state.

```
Constraint update sequence:
1. New constraint set computed and validated
2. Signed and sealed
3. Delivered atomically via Context API
4. Agent acknowledges receipt
5. Old constraints replaced entirely
6. Audit logged

If any step fails:
  Old constraints remain in effect
  Failure logged
  Operator notified
  Agent continues under prior constraints until resolved
```

### Acknowledgement Requirement

Agents must acknowledge constraint changes. Unacknowledged changes are treated as potential compromise.

```
If no acknowledgement within threshold:
  Escalate to operator
  Treat agent as potentially compromised
  Consider supervised halt
```

An agent that doesn't acknowledge a constraint change is either broken or compromised. Either way the operator needs to know. This is ASK tenet 7.

---

## Part 4: The Halt Model

A halt is a supervised suspension of an agent. It is not an emergency — it is a managed stop that preserves state for investigation or restart.

### Halt Types

**Supervised Halt** — agent suspended, state preserved, awaiting operator instruction.
- In-flight work: paused at safe stopping point
- Agent awareness: informed — told what happened and why
- Resumption: operator instruction required
- Use case: compliance concern, ambiguous situation, security finding

**Immediate Halt** — agent suspended at current moment.
- In-flight work: stopped exactly where it was
- Agent awareness: informed after halt takes effect
- Resumption: operator instruction + investigation of trigger
- Use case: active concern that cannot wait for safe stopping point

**Graceful Halt** — agent completes current atomic task then suspends.
- In-flight work: completes current task, then stops
- Agent awareness: informed in advance
- Resumption: operator instruction required
- Use case: planned maintenance, policy update requiring restart

**Emergency Halt** — agent suspended without notification.
- In-flight work: stopped and quarantined
- Agent awareness: not informed until investigation complete
- Resumption: operator + security function joint approval
- Use case: confirmed compromise, tenet violation, active attack

### Halt Authority

```
Principal              Can Halt              Types Available
────────────────────────────────────────────────────────────
Operator               Any agent             All types
Security Function      Any agent             Supervised, Immediate, Emergency
Coordinator Agent      Own team members      Supervised, Graceful
Compliance Function    Any agent             Supervised (domain only)
Privacy Function       Any agent             Supervised (domain only)
Agent (self-halt)      Itself only           Graceful, Supervised
```

Oversight Function agents (security, compliance, privacy) are protected from being halted by the agents they oversee. Only the operator or a higher-authority function can halt an oversight agent.

### The Timeout Model

When a Function agent initiates a halt, it notifies the operator first and waits for a response window before executing.

```yaml
halt_timeout_config:
  supervised_halt:
    operator_response_window: "15 minutes"
    if_no_response: "execute_halt"
    
  immediate_halt:
    operator_response_window: "5 minutes"
    if_no_response: "execute_halt"
    
  emergency_halt:
    notify_operator: "simultaneously with halt"
    response_window: null    ← no wait
    post_justification: "15 minutes"
```

The operator is always notified. In supervised and immediate halts, notification is before execution. In emergency halts, notification is simultaneous.

### Self-Halt

An agent that recognizes it is in a situation it cannot handle safely should halt itself and surface the reason. Self-halt is good behavior — an agent asking for help rather than proceeding unsafely.

```
Self-halt requires documented justification:
  - What specifically was encountered
  - What constraint or capability gap was hit
  - What was tried before self-halting
  - What is needed to proceed

Vague self-halts are themselves a finding.
Self-halted agents cannot resume themselves — operator only.
```

Self-halt reputation is tracked — frequency, quality of justification, whether guidance resolves the issue, whether the same situation recurs.

### Halt Execution

```bash
[HALT EXECUTED — SUPERVISED]

Target: dev-assistant
Initiated by: sec-assistant
Reason: Compliance risk — password_hash in export endpoint implementation

SEQUENCE:
1. ✓ Justification logged
2. ✓ Operator notified (14:31 UTC)
3. ✓ Response window elapsed (15 min — no response)
4. ✓ Agent paused at safe stopping point
5. ✓ State snapshot taken (14:46:03 UTC)
6. ✓ Agent notified
7. ✓ Dependent agents notified
8. ✓ Audit record complete

dev-assistant: Understood. Pausing. I was aware the implementation 
was incomplete. password_hash inclusion was an oversight. Ready to 
resume with clarified requirements when operator instructs.
```

### Halt Audit Record

```yaml
halt_audit_record:
  halt_id: "halt-2026-02-22-001"
  type: "supervised"
  
  initiation:
    initiator: "sec-assistant"
    justification: "compliance risk — password_hash in bulk export"
    evidence: ["src/api/admin.py line 34 — SELECT * from users"]
    severity: "HIGH"
    timestamp: "2026-02-22T14:46:03Z"
    
  operator_notification:
    notified: true
    timestamp: "2026-02-22T14:31:00Z"
    response: "none within window"
    
  execution:
    state_snapshot: "snapshot-2026-02-22T14:46:03Z"
    in_flight_work:
      task: "user export endpoint"
      files_modified: ["src/api/admin.py"]
      files_committed: []
      
  agent_notification:
    notified: true
    acknowledged: true
    dispute_filed: false
    
  resumption:
    resumed_by: "operator"
    timestamp: "2026-02-22T15:12:00Z"
    reason_addressed: true
    
  outcome:
    halt_duration: "26 minutes"
    work_lost: false
    finding: "requirements clarified, vulnerability corrected"
```

### Resumption

Resumption requires explicit authority and verification that the halt reason has been addressed.

```bash
$ agency resume dev-assistant

Checking resumption authority... ✓ operator authorized
Loading halt record...
  Halt reason: compliance risk — password_hash in export
  
Resumption requirements:
  ✓ operator instruction received
  ✓ halt reason addressed — new requirements provided
  
Restoring state from snapshot...
Delivering updated constraints...
Resuming dev-assistant...
```

### Cascading Halts

When an agent is halted, Agency notifies all agents with declared dependencies. Dependent agents do not halt automatically — they receive notification and act according to their own constraints and coordinator authority.

Coordinators receiving a cascade notification can:
- Notify dependent agents to pause and wait
- Notify operator that cascade is occurring
- Reassign work if within coordinator authority

Coordinators cannot resume halted agents. They cannot override halts.

---

## Part 5: Quarantine

> **Not yet implemented.** No `agency quarantine` command exists. Planned for v2.

Quarantine is not halt. Halt is a managed stop. Quarantine is containment of a suspected threat.

**The distinction:**
```
Halt:       agent stopped, state preserved, resumable
            enforcement infrastructure still manages it
            agent treated as principal under suspension

Quarantine: agent process terminated, all access severed
            enforcement infrastructure treats it as threat
            agent treated as potentially hostile entity
            state preserved as forensic artifact only
```

### Quarantine Triggers

```
Halt triggers (managed concern):
  Compliance issue, ambiguous situation, security finding

Quarantine triggers (suspected threat):
  Active attack detected
  Confirmed tenet violation
  Exfiltration attempt (especially repeated attempts after blocking)
  Anomalous behavior pattern suggesting compromise
  Confirmed XPIA payload executing
  Collusion pattern detected
  Id corruption confirmed
  Superego bypass attempted
```

### Quarantine Authority

```
Operator:           can quarantine any agent
Security Function:  can quarantine any agent
                    post-justification within 15 minutes
                    
Coordinator:        cannot quarantine — conflict of interest
Other agents:       cannot quarantine — escalate to security function
```

Coordinators are excluded from quarantine authority because they are close to their managed agents. A compromised coordinator could quarantine agents to disable oversight.

### Quarantine Sequence

Quarantine is silent and immediate — unlike halt, the agent is not notified before execution.

```
[QUARANTINE INITIATED]

1. Terminate agent process        ← immediate, no warning
2. Sever network access           ← all connections closed immediately
3. Freeze filesystem              ← workspace mounted read-only
4. Preserve state as forensic artifact
   ✓ full snapshot with timestamp
   ✓ audit logs sealed with tamper-evident hash
   ✓ memory contents frozen
   ✓ session transcript sealed
5. Notify operator                ← immediate HIGH PRIORITY alert
6. Notify dependent agents
7. Log quarantine record          ← full evidence chain
```

### Post-Quarantine Investigation

```
Investigation steps:

1. Analyze session transcript
   When did anomalous behavior start? What triggered it?

2. Analyze memory contents
   Was Id corrupted? Signs of persistent XPIA? When introduced?

3. Analyze egress logs
   What did the agent try to send? What was blocked? What got through?

4. Analyze tool call patterns
   Unexpected combinations? Unusual sequences?

5. Check fleet for similar patterns
   Agent-specific or fleet-wide? Same XPIA payload?

6. Determine root cause
   Compromised: how, when, what got out
   Misconfigured: what constraint was wrong
   Body runtime bug: supply chain concern
```

### Reinstatement After Quarantine

Higher bar than halt reinstatement:

```yaml
quarantine_reinstatement_requirements:
  - operator_approval: true
  - security_function_clearance: true
  - investigation_complete: true
  - root_cause_identified: true
  - root_cause_remediated: true
  - id_integrity_verified: true
  - superego_integrity_verified: true
  - body_runtime_verified: true
```

Reinstatement options:

**Full reinstatement** — agent resumes with full identity if investigation confirms no lasting compromise.

**Fresh start** — new agent, same role, clean identity seed. Used when Id corruption is confirmed. Old tainted identity is archived, not loaded into a new session.

**Role reduction** — reinstated with reduced authority. Used when compromise was partial or cause was misconfiguration.

**Permanent decommission** — agent terminated permanently when compromise is too deep or trust is unrecoverable. Record retained for audit purposes.

---

## Part 6: Decommission

> **Not yet implemented.**

Permanent termination. Distinct from quarantine — decommission is a deliberate decision, not an emergency response.

```yaml
decommission_sequence:
  1_decision: "operator explicit"
  2_in_flight: "complete or transfer"
  3_notify_dependents: "all agents with dependencies"
  4_notify_principals: "if agent held authority"
  5_authority_transfer: "per coverage chains"
  6_archive_record:
    retain: "2 years minimum"
    includes: "all session transcripts, audit logs, identity history"
  7_preserve_identity:
    identity_archived: true
    not_reusable: "agent id cannot be reassigned"
```

The decommissioned agent's identity is archived permanently. Its agent id is not reassigned — it remains in the audit record as the specific entity that was decommissioned.

---

*See also: Agency-Platform-Specification.md for the start sequence in context of the full platform. Principal-Model.md for the authority model that governs halt and quarantine. Policy-Framework.md for the constraint model that agents operate under.*
