## What This Document Covers

The policy framework defines how behavioral constraints flow through an organization of agents. It covers the policy hierarchy, inheritance and override rules, the two-key exception and delegation model, exception routing, and the named policy registry. It applies equally to standalone deployments and enterprise deployments — the model is identical, the complexity exposed scales with deployment size.

> **Implementation status (updated 2026-04-01):** Parts 1-3 (hierarchy, content types, resolution) are implemented in Go at `agency-gateway/internal/policy/engine.go`. The Python implementation (`agency_core/policy.py`) is legacy — the Go engine is the active implementation. Part 4 (named registry) is implemented in Go at `agency-gateway/internal/policy/registry.go` — the `policies/` directory is functional and policies can be listed, created, shown, and validated. Part 5 (two-key exceptions) is partially implemented — the engine validates exception format, checks grant_ref existence in org policy, verifies approved_by, and checks expiry. Delegation chain traversal (verifying each link in the chain) is not yet implemented. Parts 6-8 (redelegation chain resolution, exception routing, agent principal policy) are designed but not yet implemented.
>
> Policy CLI commands: `agency policy check <agent>`, `agency policy show <agent>`, `agency policy validate <agent>`, `agency policy template`, `agency policy exception`. MCP tools: `agency_policy_check`, `agency_policy_show`, `agency_policy_validate`, `agency_policy_template`, `agency_policy_exception`. Policy defaults and hard floors defined in `internal/policy/defaults.go`. Routing logic in `internal/policy/routing.go`.

---

## Part 1: Policy Hierarchy

Policies exist at multiple levels. Lower levels inherit from higher levels and can only restrict — never loosen.

```
Platform Tenets          ← baked into Agency, immovable, cannot be modified at any level
Compliance Policy            ← externally derived (legal, regulatory, contractual)
Organizational Policy        ← internal non-negotiables, governance-controlled
─────────────────────────────────── ← current definition boundary
Operational Policy           ← how we work, culturally defined (parked — framework ready)
Role/Agent Preferences       ← individual agent preferences (parked — framework ready)
```

### Platform Tenets

The twenty-five ASK tenets are the immovable floor. No policy at any level can override them. They are enforced architecturally — not by policy evaluation but by the infrastructure itself.

### Compliance Policy

Derived from external sources: legal requirements, regulatory obligations, contractual commitments. Lives in `compliance.yaml` at the org level. Owned by the legal or compliance team. Rarely changes. When it does, the change is a governance event requiring appropriate approval.

Compliance policy establishes hard floors that cannot be exempted at any level. An exception that would violate compliance policy cannot be granted — it requires the external obligation itself to change.

### Organizational Policy

Internal non-negotiables set by the organization's governance. Lives in `policy.yaml` at the org level. Owned by the operator or governance team. Changes are governance events.

Organizational policy can be more restrictive than compliance policy. It cannot be less restrictive.

### Policy at Lower Levels

Departments, teams, and individual agents have their own `policy.yaml`. Each level inherits from the level above and can only add restrictions or additions — never remove or loosen.

```
org/policy.yaml
└── departments/engineering/policy.yaml   ← inherits + can restrict
    └── teams/backend/policy.yaml         ← inherits + can restrict
        └── agents/dev-assistant/policy.yaml  ← inherits + can restrict
```

---

## Part 2: Policy Content Types

Not all policy content behaves the same way. Three distinct types:

### Hard Floors

Absolute minimums that cannot be modified at any level. Platform tenets are the ultimate hard floor. Compliance policy establishes additional hard floors.

```yaml
# Examples of hard floor declarations in compliance.yaml
hard_floors:
  - parameter: "audit_logging"
    value: "required"
    source: "regulatory-requirement-SOC2"
    cannot_be_exempted: true
    
  - parameter: "security_alerts_to_users"
    value: "always-enabled"
    source: "gdpr-article-33"
    cannot_be_exempted: true
```

### Bounded Parameters

Values that can be tuned within a defined range. Lower levels can make them more restrictive but not less restrictive than the level above.

```yaml
# org/policy.yaml
bounded_parameters:
  risk_tolerance:
    default: "medium"
    allowed_range: ["low", "medium"]    ← high not permitted at any level
    
  max_concurrent_tasks:
    default: 10
    minimum: 1
    maximum: 10                         ← cannot exceed org default
```

Lower levels can set `risk_tolerance: low` but not `risk_tolerance: high`. Once a level sets a value, lower levels can only go further in the restrictive direction — they cannot restore the parent value even though it would be within the parent's allowed range.

### Contextual Additions

Rules that lower levels can add but never remove. A team can add constraints specific to their domain. They cannot remove inherited constraints.

```yaml
# teams/backend/policy.yaml
additions:
  - rule: "all database migrations require explicit review"
  - rule: "no direct production database access"
```

These stack with inherited additions. There is no mechanism to remove an addition from a higher level.

---

## Part 3: Policy Resolution

When Agency computes effective policy for an agent, it resolves the full chain:

```bash
agency policy check dev-assistant

Resolving policy chain...
  1. Platform tenets (always applied)
  2. org/compliance.yaml
  3. org/policy.yaml
  4. departments/engineering/policy.yaml
  5. teams/backend/policy.yaml
  6. agents/dev-assistant/policy.yaml

Validating...
  ✓ No hard floors violated
  ✓ No bounded parameters loosened
  ✓ No additions removed
  ✓ All exceptions valid (both keys present)
  ✓ No conflicts

Effective policy: computed and sealed
```

If validation fails Agency reports exactly which level has a violation and refuses to start the agent until it is resolved.

### Missing Policy

If an agent has no `policy.yaml`, it inherits from the nearest level above that has one. Absence of a file is not a gap — it is inheritance. Platform defaults are the implicit floor for all parameters not explicitly defined at any level.

---

## Part 4: Named Policy Registry

> **Partially implemented.** The `policies/` directory is created by `agency setup`. The named policy registry (`internal/policy/registry.go`) supports list, create, show, and validate operations. The `extends` reference resolution in the policy engine is not yet implemented — policies must be fully defined at each level.

Common policies that apply across multiple agents, teams, or departments should live once in the named registry rather than being duplicated.

```
org/policies/
├── eng-standard-v1.yaml        ← standard engineering policy
├── compliance-baseline-v1.yaml ← minimum compliance requirements
├── security-elevated-v1.yaml   ← elevated security for sensitive agents
└── README.md                   ← documents available policies
```

A `policy.yaml` at any level can reference a named policy:

```yaml
# agents/dev-assistant/policy.yaml

# Form 1: Full reference — use named policy as-is
extends: "eng-standard-v1"

# Form 2: Extension — use named policy as base, add restrictions
extends: "eng-standard-v1"
restrictions:
  max_concurrent_tasks: 3
additions:
  - rule: "never modify infrastructure files"

# Form 3: Full definition — no reference
version: "0.1"
rules:
  - ...
```

Updates to a named policy propagate automatically to everything that references it. Versioning is explicit — `eng-standard-v1` vs `eng-standard-v2` — so references are stable until explicitly updated.

---

## Part 5: The Two-Key Exception Model

> **Partially implemented.** The Go policy engine (`internal/policy/engine.go`) validates exception format: checks `grant_ref` exists in org policy, verifies `approved_by` is present, checks expiry dates on both exceptions and delegation grants. Delegation chain traversal (verifying `delegated_through` links and redelegation authorization at each level) is not yet implemented.

Some legitimate situations require operating outside normal policy bounds. The two-key model ensures exceptions are authorized, documented, scoped, and auditable.

### The Two Keys

**Key 1 — Delegation Grant** — A higher level explicitly authorizes a lower level to approve certain types of exceptions within certain bounds. Set in advance. Lives in the policy of the granting level.

**Key 2 — Exception Exercise** — The lower level actually grants a specific exception within the scope it was delegated. Lives in the policy of the exercising level.

Both keys must be present. An exception without Key 1 (no authority to grant it) is invalid. A delegation grant without Key 2 (no exception actually granted) creates no exception.

### Delegation Grant Format

```yaml
# org/policy.yaml

delegation_grants:
  - grant_id: "eng-task-scaling"
    delegated_to: "departments/engineering"
    can_redelegate_to: "teams"
    
    scope:
      parameter: "max_concurrent_tasks"
      max_value: 20          ← ceiling they can grant up to
      
    constraints:
      requires_reason: true
      expiry_required: true
      max_expiry: "6 months"
      notify: ["operator"]
      
  - grant_id: "data-access-exceptions"
    scope:
      category: "data_access"
    approval_routing:
      default: "privacy_officer"
      escalate_to: "legal"
      
  - grant_id: "security-policy-exceptions"
    scope:
      category: "security_policy"
    approval_routing:
      default: "security_function"
      requires_human_cosign: true
      escalate_to: "legal"
```

### Exception Exercise Format

```yaml
# agents/dev-assistant/policy.yaml

exceptions:
  - exception_id: "dev-extended-tasks"
    grant_ref: "eng-task-scaling"
    delegated_through:
      - "departments/engineering"
      - "teams/backend"
    parameter: "max_concurrent_tasks"
    granted_value: 15           ← within ceiling of 20
    reason: "autonomous weekend operations require higher concurrency"
    approved_by: "operator"
    approved_date: "2026-02-22"
    expires: "2026-08-22"
    notify_on_use: ["sec-assistant"]
```

### Validation

Agency validates both keys at policy check time:

```
Exception: dev-extended-tasks
  Key 1 — Delegation grant:
    ✓ grant "eng-task-scaling" exists in org/policy.yaml
    ✓ engineering authorized ✓
    ✓ engineering redelegated to teams ✓
    ✓ backend redelegated to agents ✓
    ✓ delegation chain intact

  Key 2 — Exception exercise:
    ✓ granted_value 15 within ceiling 20 ✓
    ✓ reason documented ✓
    ✓ approved_by present ✓
    ✓ not expired ✓

  Both keys valid ✓
```

### Grant Expiry

When a delegation grant expires, all exceptions exercised under it are immediately invalidated. Exceptions cannot outlive their authorizing grant. This is a hard rule — no grandfather clause.

---

## Part 6: Redelegation

> **Not yet implemented.**

Delegation grants can authorize redelegation — passing the authority further down the hierarchy. Each redelegation is explicit.

```
Org grants Engineering: can approve tool additions
  → Engineering redelegates to Teams
    → Teams redelegate to Agents
      → Agent grants itself a specific tool exception
        within the scope Engineering originally defined
```

A level can only redelegate authority it actually has. Engineering cannot give teams more than engineering was given. The permission ceiling flows down — it cannot be raised at any level in the chain.

### Scoped vs Unscoped Grants

Grants exist on a spectrum from tightly scoped to unscoped:

```yaml
# Tightly scoped
scope:
  parameter: "max_concurrent_tasks"
  max_value: 20

# Category scoped
scope:
  category: "tool_permissions"
  type: "additive-only"

# Unscoped (within a domain)
scope:
  domain: "internal-tooling"
  type: "unrestricted"
  constraints:
    hard_floors: "still apply"    ← even unscoped cannot touch hard floors
    notify: "operator"
    audit: "all"
```

Unscoped grants require the highest approval level, mandatory notification to Function agents, and mandatory audit of all exercises. Hard floors are inviolable at every scope level.

---

## Part 7: Exception Routing

> **Not yet implemented.** Planned for v2 with multi-agent coordination.

Exceptions route to humans based on policy domain, not just hierarchy level. Different types of exceptions go to different people.

```yaml
# org/roles.yaml — approval routing by domain

exception_routing:
  security_policy:
    routes_to: "security_function"
    requires_human_cosign: true
    escalates_to: "legal"
    
  data_access:
    routes_to: "privacy_officer"
    escalates_to: "legal"
    
  compliance:
    routes_to: "legal"
    requires_dual_approval: ["legal", "operator"]
    time_limit: "48 hours"
    
  operational:
    routes_to: "department_head"
    if_delegated: "team_lead"
    
  default:
    routes_to: "operator"
```

### Standalone Deployment Routing

In standalone deployments all routing collapses to the operator. The same model applies — exceptions still require approval, still have expiry, still audit — but all roads lead to one person.

```yaml
# Standalone deployment: all roles held by operator
principals:
  humans:
    - id: "gb"
      roles: ["operator"]    ← operator role includes all approval authority
```

### Exception Lifecycle

```
1. Exception requested (by agent, team lead, or department head)
2. Agency identifies policy domain
3. Routes to correct approval role
4. Notifies oversight Functions (always visible even if not approver)
5. Human reviews: approve / deny / escalate / request more info
6. If dual approval required: second human notified
7. Decision logged with full audit trail
8. Exception activated or denied
9. Expiry monitoring active
   → warning at 24 hours before expiry
   → warning at 1 hour before expiry
   → immediate invalidation at expiry
```

---

## Part 8: Policy for Agent Principals

> **Not yet implemented.** Requires principal lifecycle and trust evolution (see Principal-Model.md).

Agents can hold roles in the governance model and approve certain exceptions within delegated scope. This is covered in the Principal Model but the policy implications are noted here.

A coordinator agent approving a workflow exception within tight delegated scope:

```yaml
# org/roles.yaml
roles:
  team_coordinator:
    type: "agent"
    assigned_to: "lead-assistant"
    can_approve:
      - "workflow_exceptions"
    approval_constraints:
      max_value_change: "10%"
      expiry_max: "7 days"
      notify: "operator"
      auto_review: "sec-assistant"
```

Even when an agent is the approver, the operator is notified and the security function sees it. Agent approval authority is always scoped and monitored.

---

*See also: Agency-Platform-Specification.md for the file formats this framework operates on. Principal-Model.md for the principals who approve exceptions. Agent-Lifecycle.md for how policy changes are delivered to running agents.*
