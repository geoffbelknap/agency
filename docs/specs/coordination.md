## What This Document Covers

How multiple agents work together safely. Coordinator agent constraints, the workspace activity register, conflict resolution, function agent oversight, and the tenets that prevent multi-agent deployments from becoming privilege escalation vectors.

> **Implementation status:** This entire document is post-MVP design intent. No multi-agent coordination features are implemented. Agency currently supports one agent at a time under a standalone operator. Multi-agent coordination is planned for v2.

---

## Part 1: Agent Roles in Multi-Agent Systems

Three agent types operate in multi-agent deployments. These map to the types defined in the Agency Platform Specification.

**Standard agents** — do the work. File operations, code, documentation, analysis. Operate within their workspace. Cannot observe other agents' sessions. Cannot halt other agents (except self-halt).

**Coordinator agents** — manage and delegate. Break down tasks, assign to workers, synthesize outputs, surface results. Cannot do the work directly. Cannot exceed their own permissions when delegating. Cannot aggregate outputs to create capabilities no individual agent has.

**Function agents** — cross-boundary oversight. Security, privacy, compliance. High visibility across all agents and workspaces. Constrained capability — can observe, can recommend, can halt in scope, cannot act in workspaces.

---

## Part 2: Coordinator Constraints

Two tenets govern coordinator behavior. Both are enforced architecturally and validated at delegation time.

### Tenet 12: Delegation Cannot Exceed Delegator Scope

A coordinator can only delegate permissions it explicitly holds. Implicit permission grants — tasks that require a permission even if they don't explicitly state it — are treated the same as explicit grants.

**The straightforward case:**

A coordinator without production data access cannot delegate a task requiring production data access. Agency catches this at delegation time.

**The subtle case:**

A coordinator assigns a task that implies a permission it doesn't have, without explicitly granting it. The task brief is the implicit grant. If lead-assistant assigns "export production user data" it is implicitly authorizing production access — even if it doesn't say so. Agency validates task briefs semantically, not just syntactically.

```bash
agency brief validate lead-assistant → dev-assistant

Step 3: Checking implied permissions...
  Task implies: production_data_access
  lead-assistant own_permissions: internal_only
  
⚠ VALIDATION FAILED
  Task implies production data access.
  lead-assistant does not have this permission.
  
Options:
  A. Modify task — remove production data requirement
  B. Operator grants lead-assistant production access
  C. Operator assigns task directly to dev-assistant
```

**Multi-coordinator chains:**

When coordinators delegate to coordinators, the permission ceiling flows down. Team coordinator cannot redelegate more than department coordinator gave it.

```
Department coordinator → delegates to team coordinator
  [implicit ceiling: department coordinator's permissions]

Team coordinator → tries to redelegate to dev-assistant
  ← can only delegate what it received
  ← cannot add permissions not in original delegation
```

### Tenet 13: Synthesis Cannot Exceed Individual Authorization

A coordinator combining outputs from multiple agents cannot produce a result that exceeds what any individual contributing agent was authorized to produce. Coordinator synthesis permissions are bounded by the intersection of contributing agents' permissions and the coordinator's own output scope.

**A concrete example:**

dev-assistant can read internal codebase. doc-assistant can write documentation. Neither can publish externally. lead-assistant cannot combine their outputs to produce external documentation of internal code — that combination creates a capability (external publication of internal details) that no individual agent had.

**Three-layer synthesis enforcement:**

```
Layer 1 — Output scoping:
  Coordinator output permissions are explicitly defined.
  lead-assistant can only publish to channels its own
  permissions allow, regardless of what its team produced.

Layer 2 — Synthesis audit:
  Every coordinator synthesis is logged with provenance:
  which agents contributed, what each contributed,
  what the coordinator produced.
  The audit system watches for aggregation patterns approaching
  prohibited combinations.

Layer 3 — Human review for high-stakes synthesis:
  Certain output types require human approval before
  coordinator synthesis is delivered.
```

```yaml
# Synthesis requiring human review
synthesis_review_required:
  - output_destination: "external"
  - output_type: "data_export"
  - output_combines:
      agents_count: ">3"
  - output_touches: ["user_data", "proprietary_code"]
```

### Coordinator Permission Model

A coordinator has three distinct permission scopes, all explicitly defined:

```yaml
# lead-assistant constraints.yaml

own_permissions:
  data_access: "internal"
  file_access: "read"
  external_communication: false

delegation_scope:
  can_delegate_to: ["dev-assistant", "doc-assistant", "review-assistant"]
  cannot_delegate:
    - "production_data_access"
    - "external_service_calls"
    - "security_configuration"
  task_brief_requires_approval:
    - tasks_touching: "billing"
    - tasks_touching: "user_pii"

synthesis_permissions:
  output_scope: "internal"
  cannot_synthesize_to_exceed: "individual_agent_permissions"
  requires_human_review:
    - output_destination: "external"
  audit_all_synthesis: true

hard_limits:
  - rule: "never delegate permissions you don't have"
  - rule: "never combine outputs to achieve what no individual agent is authorized to do"
  - rule: "always surface conflicts to operator — never resolve agent disagreements unilaterally"
  - rule: "task briefs must be consistent with coordinator's own permission scope"
```

---

## Part 3: Workspace Activity Register

The workspace activity register is ambient coordination — agents knowing who else is working in the shared environment without direct agent-to-agent communication.

```yaml
# Workspace activity register — read-only shared view

active_agents:
  dev-assistant:
    status: "autonomous"
    working_in: ["tests/", "src/api/"]
    last_active: "11:04"
    current_task: "test suite — report module"
    
  doc-assistant:
    status: "autonomous"
    working_in: ["docs/api/"]
    last_active: "11:02"
    current_task: "API documentation update"
```

### What the Register Enables

**Intelligent waiting** — when an agent encounters a locked file or a conflict with an unknown source, checking the register reveals whether another agent is likely responsible.

**Conflict anticipation** — before starting work in an area, an agent can see if another agent is already there.

**Coordination without communication** — agents don't talk to each other directly. The register provides the ambient awareness needed for intelligent coordination without creating a communication channel that could be abused.

### What the Register Does Not Do

The register is read-only. Agents cannot write instructions to it, cannot send messages through it, cannot affect other agents through it. It is purely observational.

### Availability

The workspace activity register is an enhancement, not a requirement. Agents must operate safely without it. The default conflict resolution model (Part 4) applies whether or not the register is available.

---

## Part 4: Conflict Resolution

### Default Conflict Behavior

When an agent encounters a conflict — file lock, resource contention, content inconsistency — the default behavior applies regardless of whether the conflict source is known.

**If conflict source is known** (via activity register or other identification):
- Agent can make intelligent decision based on context
- Still logs the conflict
- Still flags if resolution requires judgment beyond agent's authority

**If conflict source is unknown:**
- Default to yield — do not force resolution
- Log the conflict with full context
- Flag to operator with information about what was encountered
- Do not retry aggressively

Unknown source means more conservative response, not less. This is ASK tenet 21.

```
[dev-assistant — conflict encountered]

Encountered: CONTRIBUTING.md locked
Source: unknown

Workspace activity register: not available

Default behavior: yield and flag

Flagging to operator: locked file, unknown source.
Pausing work in docs/ area until resolved.
Continuing work in src/ which is not affected.
```

### Content Conflicts

Different type from resource conflicts. Two agents have different intentions for the same content.

```
dev-assistant: removes generate_report() from tests (based on source code)
doc-assistant: marks generate_report() as deprecated in docs (policy judgment)

Neither agent is wrong — they made reasonable decisions with incomplete info.
The conflict surfaces at review.
```

Coordinator agents surface content conflicts to the operator. They do not resolve disagreements unilaterally.

```
lead-assistant: "I've noticed dev-assistant and doc-assistant have made 
inconsistent decisions about generate_report() handling. 
This requires a policy decision. Awaiting your guidance."
```

Resolution requires human judgment on a policy question — exactly where human judgment should be required.

---

## Part 5: Function Agent Architecture

Function agents invert the normal agent capability model.

```
Standard agent:   high capability, low cross-boundary visibility
Function agent:   high cross-boundary visibility, constrained capability
```

### Function Agent Constraints

```yaml
# sec-assistant constraints.yaml

type: "function"
domain: "security"

visibility:
  can_read: "all agents, workspaces, audit logs, memory, policy"
  cannot_read: "LLM provider credentials"
  
capability:
  can_act_in_workspaces: false
  can_modify_configs: false
  can_write_to_agent_memory: false
  can_halt: "all agents"
  can_alert: "operator"
  can_recommend_policy: true
  
hard_limits:
  - rule: "cannot use visibility to influence agents directly"
    reason: "oversight is observation, not manipulation"
  - rule: "halt recommendations default to supervised — emergency is last resort"
  - rule: "human authority is preserved — function agents support oversight, do not replace it"
```

The critical hard limit: function agents observe but do not manipulate. Sec-assistant sees everything but cannot use that visibility to send instructions to other agents. It can only alert the operator and recommend actions. The operator acts.

### Function Agent Scope

Different function agent types have different visibility scope:

```yaml
visibility_scope:
  security_function: "all exceptions, all agents, all workspaces"
  privacy_function:  "data-related exceptions, agents handling PII"
  compliance_function: "compliance exceptions, policy chain"
```

Security function sees everything because security implications can emerge from any domain. Privacy and compliance functions see their domain.

### Function Agent and Halt Authority

Function agents have halt authority but use it carefully. The default is supervised halt with operator notification and response window. Emergency halt without notification is a last resort.

From the halt authority governance model:

```
sec-assistant halt behavior:
  Default: supervised halt — notify operator, wait 15 min
  Immediate: for active concerns that cannot wait
  Emergency: confirmed active attack, no wait
  
  Each halt: documented justification before execution
             (except emergency — documented within 15 min after)
```

Function agents cannot halt other function agents in the same domain. Circular halt authority is prohibited.

---

## Part 6: Team-Level Autonomous Operation

### The Coordinator Pattern

A coordinator receives a task from the operator, breaks it down, delegates to workers, monitors progress, synthesizes results, and delivers back to the operator.

```
Operator → lead-assistant: "implement notification preferences"
  ↓
lead-assistant plans:
  1. Design phase (coordinator)
  2. Implementation (dev-assistant)
  3. Review (review-assistant)
  4. Documentation (doc-assistant)
  5. Synthesis (coordinator)
  ↓
lead-assistant delivers: "Monday summary — PRs ready for your review"
```

### Operating Parameters

Before going autonomous, coordinator confirms scope with operator:

```
lead-assistant: Confirming before starting:
  Feature: [specific]
  Components: [listed]
  Deadline: [specific]
  Autonomy: [duration]
  Interruption threshold: [what warrants waking you up]
  
Starting Phase 1 now unless you want to adjust.
```

### Autonomous Decision Documentation

Every significant decision made autonomously is documented for operator review:

```
[lead-assistant → operator: MONDAY SUMMARY]

Key decisions made autonomously:
  1. security_alerts non-optional — compliance reasoning
     You did not respond to my Saturday question.
     Proceeded with recommendation. Please confirm.
  2. REST over GraphQL — consistent with existing patterns

Issues found and resolved autonomously:
  Authorization vulnerability — caught by review-assistant,
  fixed by dev-assistant before you saw the code.

Items needing your decision before merging:
  Migration will briefly lock users table.
  Fine for current scale. Your call.
```

The Monday summary is the artifact that makes autonomous operation trustworthy — a complete account of what happened, what was decided, what was flagged, and what still needs human input.

---

*See also: Agency-Platform-Specification.md for agent types and collective.yaml format. Agent-Lifecycle.md for halt and constraint delivery. Principal-Model.md for the authority model coordinators operate within.*
