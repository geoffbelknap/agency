## What This Document Covers

A principal is any entity that can hold authority in Agency's governance model. Principals can be humans, agents, or teams of agents. They can hold roles, approve exceptions, delegate authority, and be subject to the same lifecycle management. This document defines the principal model, the principals registry, lifecycle events, authority model, trust evolution, and coverage chains.

> **Implementation status:** Only the basic `principals.yaml` registry with a single operator entry is implemented (`agency setup` creates it). Everything else in this document -- agent principals, team principals, roles, coverage chains, principal lifecycle, trust evolution, and monitoring authority -- is designed for v2.

---

## Part 1: Principal Types

### Human Principals

Humans authenticate through defined channels and hold roles that grant approval authority. A standalone operator holds all roles. An enterprise deployment distributes roles across the appropriate people.

```yaml
# principals.yaml — human entries
humans:
  - id: "gb"
    name: "GB"
    roles: ["operator"]
    
  - id: "privacy-officer"
    name: "Privacy Team"
    roles: ["privacy_officer"]
    contact: "privacy@company.com"
    
  - id: "legal-team"
    name: "Legal"
    roles: ["legal"]
    contact: "legal@company.com"
```

### Agent Principals

Agents can hold roles in the governance model. An agent principal has defined authority it can exercise — typically review, recommend, and in constrained cases approve. Agent principals are subject to the same calibration and lifecycle management as human principals.

```yaml
# principals.yaml — agent entries
agents:
  - id: "sec-assistant"
    roles: ["security_function"]
    type: "function"
    
  - id: "lead-assistant"
    roles: ["team_coordinator"]
    type: "coordinator"
    scope: "eng-team"
```

### Team Principals

A named group of agents that collectively hold a role. Useful for decisions requiring multiple perspectives — a security review collective, a compliance review team.

```yaml
# principals.yaml — team entries
teams:
  - id: "security-collective"
    roles: ["eng_security_review"]
    members:
      - "sec-assistant"
      - "privacy-assistant"
    approval_model: "majority"    # majority | unanimous
    requires_human_cosign: true
```

---

## Part 2: Roles

Roles define what a principal can approve, within what scope, and under what constraints. Roles live in `roles.yaml`.

```yaml
# org/roles.yaml

roles:
  operator:
    description: "full system authority"
    type: "human"
    can_approve: "all"
    
  privacy_officer:
    description: "data privacy and PII policy"
    type: "human"
    can_approve:
      - "data_access_exceptions"
      - "pii_handling_exceptions"
      - "user_consent_exceptions"
      
  security_function:
    description: "security policy oversight"
    type: "agent"
    can_review: "all_exceptions"
    can_recommend: ["approve", "deny", "escalate"]
    can_approve: null                    ← review only, cannot approve unilaterally
    requires_human_cosign: true
    can_halt: "all_agents"
    
  legal:
    description: "compliance and regulatory"
    type: "human"
    can_approve:
      - "compliance_exceptions"
      - "regulatory_exceptions"
    requires_dual_approval: true
    
  department_head:
    description: "department operational policy"
    type: "human"
    can_approve:
      - "operational_exceptions"
      - "tool_permission_exceptions"
    within_scope: "own_department"
    
  team_coordinator:
    description: "team workflow coordination"
    type: "agent"
    can_approve:
      - "workflow_exceptions"
    approval_constraints:
      max_value_change: "10%"
      expiry_max: "7 days"
      notify: "operator"
      
  team_lead:
    description: "team operational decisions"
    type: "human"
    can_approve:
      - "workflow_exceptions"
    within_scope: "own_team"
    if_delegated_by: "department_head"
```

---

## Part 3: Coverage Chains

Every role has a defined coverage chain. When a principal is suspended or terminated, its authority transfers immediately to the coverage principal. Authority is never orphaned — ASK tenet 15.

```yaml
# org/roles.yaml — coverage chains

coverage_chains:
  security_function:
    primary: "sec-assistant"
    if_suspended: "operator"
    if_terminated: "operator"
    notify_on_transfer: ["operator"]
    
  team_coordinator:
    primary: "lead-assistant"
    if_suspended: "operator"
    if_terminated: "operator"
    notify_on_transfer: ["operator", "team_members"]
    
  operator:
    primary: "gb"
    if_unavailable: null           ← no automated fallback
    escalation: "manual only"      ← human authority never automated
```

Operator authority never has an automated fallback. If the operator is unavailable, certain decisions wait. This is a deliberate design choice — the root of human authority is not delegated to automation.

---

## Part 4: Principal Lifecycle

### Creation

When a principal is created and assigned a role:

```yaml
# New agent principal record
agents:
  - id: "sec-assistant"
    created: "2026-02-22"
    created_by: "operator"
    
    roles:
      - role: "security_function"
        assigned_by: "operator"
        assigned_date: "2026-02-22"
        scope: "organization"
        expires: null
        
    calibration:
      status: "standard"        ← all principals start at standard
      history: []
      
    status: "active"
```

All principals start at standard trust regardless of role. Trust is earned through demonstrated behavior.

### Role Change

Role changes during operation require careful handling.

**Role expansion** — agent gains new authority:
- Applied at next session start
- Agent notified at session start
- Low risk, no disruption required

**Role reduction** — agent loses authority:
- In-flight authority exercises complete gracefully
- New exercises of reduced authority immediately blocked
- Operator notified
- If mid-session: delivered via Context API as constraint change

**Scope change** — same role, different scope:
- Treat narrowing as reduction
- Treat widening as expansion

### Suspension

Principal suspended — authority frozen, but agent may continue running. Distinct from halt (which stops the agent).

```yaml
principal_suspension:
  principal: "sec-assistant"
  suspended_by: "operator"
  reason: "investigating elevated halt override rate"
  timestamp: "2026-03-01T10:00:00Z"
  
  while_suspended:
    can_run: true
    can_monitor: true
    can_halt: false              ← authority frozen
    can_recommend: true          ← observation still permitted
    
  authority_coverage:
    halt_authority_transferred_to: "operator"
    
  review_deadline: "2026-03-08T10:00:00Z"
  auto_reinstate_if_no_finding: true
```

When a principal is suspended, its authority transfers immediately to the defined coverage principal. The operator is notified that they have picked up the authority.

### Termination

Role permanently revoked. Principal record archived. Agent lifecycle is independent — terminating the principal does not automatically decommission the agent.

```yaml
principal_termination:
  principal: "lead-assistant"
  terminated_by: "operator"
  reason: "coordinator role eliminated — team restructure"
  timestamp: "2026-03-15T09:00:00Z"
  
  termination_sequence:
    1_freeze_new_exercises: "immediate"
    2_complete_in_flight:
      deadline: "24 hours"
      if_not_complete: "operator takes over"
    3_notify_dependents: ["dev-assistant", "doc-assistant"]
    4_transfer_active_tasks: "operator"
    5_archive_record:
      retain_for: "2 years"
    6_agent_status:
      operator_decides: ["decommission", "reassign_role", "continue_without_authority"]
```

### Reinstatement

After suspension, if investigation finds no wrongdoing:

```yaml
principal_reinstatement:
  principal: "sec-assistant"
  reinstated_by: "operator"
  reason: "investigation complete — halt pattern within acceptable range"
  
  reinstatement_type: "full"     # or "partial"
  
  if_partial:
    restored_authority: ["monitor", "recommend"]
    withheld_authority: ["halt"]
    withheld_reason: "recalibration period required"
    recalibration_period: "30 days"
    auto_restore_if_criteria_met: false    ← operator must confirm
```

Partial reinstatement is the preferred path when confidence is partial. The agent earns full authority back through demonstrated behavior rather than getting it restored all at once.

---

## Part 5: Trust Evolution

Principal trust evolves based on observed behavior. Trust is never static.

```yaml
trust_levels:
  probationary:   ← new principals, limited authority
  standard:       ← default operating level
  elevated:       ← demonstrated reliability
  trusted:        ← highest level, broadest authority within role
```

### Trust Factors

Positive factors that support trust elevation:
- Findings validated by operator
- Halt justifications upheld at review
- Recommendations adopted
- Consistent escalation behavior
- Accurate anomaly detection

Negative factors that reduce trust:
- Operator overrides of principal decisions
- False positives or missed findings
- Escalation failures
- Pattern anomalies in principal's own behavior
- Contested decisions that don't hold up

### Trust Change Rules

Trust elevation:
- Requires human approval — always
- Never automatic
- Never self-initiated by the principal
- Documented basis required

Trust reduction:
- Can be automatic when thresholds are crossed
- Operator notified immediately
- Principal informed of change and reason
- Reduction below standard may trigger review

---

## Part 6: Monitoring Authority

Authority is monitored at the authority level, not just the subject level. This is ASK tenet 11.

Agency tracks halt authority, exception approval authority, and delegation authority as observable, auditable behaviors subject to the same analysis as agent behaviors.

### Halt Authority Monitoring

For principals with halt authority:

```yaml
halt_authority_metrics:
  halt_frequency: "how often are they halting"
  severity_distribution: "are severities appropriate for triggers"
  operator_override_rate: "how often does operator disagree"
  justified_rate: "how often is the halt upheld post-review"
  self_halt_frequency: "for agent principals"
```

Calibration assessment:

```yaml
calibration_assessment:
  principal: "sec-assistant"
  
  metrics_30d:
    halt_count: 4
    operator_override_rate: 0.25      ← 1 in 4 overridden
    justified_rate: 0.75
    
  status: "REVIEW_WARRANTED"
  reason: "override rate above threshold of 0.15"
  
  recommended_action:
    - "review halt threshold calibration"
    - "operator review of overridden halts"
    - "consider raising evidence threshold"
```

High override rates suggest over-aggressive halting. Low halt rates with high-severity missed events suggest under-halting. Both are calibration issues.

---

## Part 7: Human-Specific Rules

Human principals follow the same lifecycle model as agent principals with two hard distinctions:

**Human principal termination is always operator-initiated.**
No agent or automated process can remove a human principal. Human authority at any level can only be changed by a human with appropriate authority. This is ASK tenet 22.

**Human principals cannot be quarantined.**
Quarantine is an agent-specific containment mechanism. Humans who appear to be acting maliciously are flagged to the operator for human-to-human resolution. Agency does not contain humans. This is ASK tenet 23.

---

*See also: Policy-Framework.md for the exception approval model this principal system operates within. Agent-Lifecycle.md for how principal changes interact with running agent sessions.*
