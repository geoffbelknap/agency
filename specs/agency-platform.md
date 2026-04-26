## What This Document Covers

Agency is a conforming implementation of the ASK operating framework. This document defines the Agency platform — its primitives, file formats, CLI interface, organizational model, and the design principles behind each decision.

ASK defines the theory. Agency implements it. A deployment that conforms to all 23 ASK tenets through other means is a valid ASK implementation. Agency is the reference.

> **Implementation status:** Parts 1-3 (design principles, file family, policy model) and Parts 5-6 (file formats, CLI) are substantially implemented for standalone operator use. Part 4 (principal model) has basic `principals.yaml` registry with operator entry only. `collective.yaml` format is defined but not parsed. See the file family annotations below for which files are not yet read by Agency.

---

## Part 1: Design Principles

### The Core Problem Agency Solves

The person who wants to use this isn't thinking about secure agent runtimes. They're thinking: *I have too much work. I want capable workers to help me get it done.*

Agency is the operational environment that makes that possible safely. It is not primarily a security tool — it is a place where capable agents work on your behalf, professionally, within a structure that keeps things accountable.

Security is structural, not imposed. The constraints are in the architecture. The operator focuses on the work.

### Two Scales, One Framework

Agency must work elegantly at both ends of the deployment spectrum:

**Standalone operator** — one person running a small team of agents. Everything collapses to the operator. No routing, no hierarchy, no committees. Policy is personal preference plus platform defaults.

**Enterprise deployment** — departments, teams, compliance obligations, multiple humans with distinct roles. Privacy officer owns privacy policy. Legal owns compliance. Security function monitors everything. Exceptions route to the right people.

The same framework serves both. Standalone deployments don't drown in governance overhead they don't need. Enterprise deployments have the structure to maintain accountability at scale.

### Naming Philosophy

Names are functional, not conceptual. The conceptual model (Superego/Ego/Id, Mind/Body/Workspace) lives in documentation as the explanatory layer. The files and CLI use plain, legible names.

`constraints.yaml` not `superego.yaml`. `memory/` not `id/`. `agency agent start` not `agency mind instantiate`.

---

## Part 2: The File Family

Agency manages agents through a hierarchy of structured files. The hierarchy mirrors the organization structure and policy inheritance chain.

### File Format Principle

Extensions reflect format, not schema. `.yaml` for structured definitions. `.md` for human-authored content. No proprietary extension. Every file is readable and editable with standard tools.

### The Complete File Family

```
org/
├── org.yaml              ← organization manifest
├── compliance.yaml       ← compliance policy (not currently parsed by Agency)
├── policy.yaml           ← organizational policy
├── roles.yaml            ← role definitions and approval authority (not currently parsed by Agency)
├── principals.yaml       ← unified registry: humans, agents, teams
└── policies/             ← named reusable policy library (not currently parsed by Agency)

departments/                 (not currently parsed by Agency)
└── <name>/
    └── policy.yaml

teams/                       (not currently parsed by Agency)
└── <name>/
    └── policy.yaml

agents/
└── <name>/
    ├── agent.yaml        ← agent manifest (entry point)
    ├── constraints.yaml  ← operator-owned, read-only to agent
    ├── identity.md       ← initial seed, operator-authored once
    ├── memory/           ← accumulated experience, agent-owned
    ├── workspace.yaml    ← infrastructure definition
    ├── services.yaml     ← granted service credentials (operator-managed)
    └── policy.yaml       ← behavioral guardrails

services/
└── <name>.yaml           ← service definitions (api_base, credential config)

workspaces/
└── <name>/
    └── workspace.yaml

collectives/                 (not currently parsed by Agency)
└── <name>/
    └── collective.yaml

functions/                   (not currently parsed by Agency)
└── <name>/
    ├── agent.yaml
    ├── constraints.yaml
    └── identity.md
```

### File Ownership and Writability

| File | Owner | Agent Writable | Lifecycle |
|---|---|---|---|
| org.yaml | Operator | No | Static |
| compliance.yaml | Compliance/Legal | No | Governance event to change |
| policy.yaml | Role-appropriate human | No | Governed |
| roles.yaml | Operator | No | Governed |
| principals.yaml | Operator | No | Managed |
| agent.yaml | Operator | No | Managed |
| constraints.yaml | Operator | No | Read-only to agent (enforced) |
| identity.md | Operator (initially) | No | Seed only |
| memory/ | Agent | Yes | Continuous |
| workspace.yaml | Operator | No | Infrastructure |
| collective.yaml | Operator | No | Managed |
| AGENTS.md | Operator (generated) | No | Generated from constraints.yaml at startup |
| services.yaml | Operator | No | Hot-grantable via `agency grant` |
| services/*.yaml | Operator | No | Service definitions (bundled + custom) |

---

## Part 3: The Policy Model

### Inheritance

Policy at each level inherits from the level above by default. An agent without its own policy.yaml inherits from team, department, or org. Absence is meaningful — not a gap.

Resolution order (most specific wins):
1. Agent-level policy.yaml
2. Team-level policy.yaml
3. Department-level policy.yaml
4. Org-level policy.yaml
5. Platform defaults (always present, cannot be removed)

Undefined parameters at higher levels default to platform defaults. Lower levels can only restrict from those defaults, never expand.

### Override Rules

**Hard floors** — absolute minimums. Cannot be modified at any level.

**Bounded parameters** — tunable within range. Lower levels can restrict, never loosen. Once restricted at any level, lower levels cannot relax even to a higher level's original value.

**Contextual additions** — rules lower levels can add but never remove.

### Named Policy References

```yaml
# teams/backend/policy.yaml
extends: "eng-standard-v1"

additions:
  - rule: "all database migrations require review"

restrictions:
  max_concurrent_tasks: 5
```

Named policies live in `org/policies/` and are versioned. Format: `<name>-v<n>.yaml`.

### The Two-Key Exception Model

Exceptions require two keys — both must be present and valid.

**Key 1 — Delegation grant** (set in advance by higher level):
```yaml
# org/policy.yaml
delegation_grants:
  - grant_id: "eng-task-scaling"
    delegated_to: "departments/engineering"
    can_redelegate_to: "teams"
    scope:
      parameter: "max_concurrent_tasks"
      max_value: 20
    constraints:
      requires_reason: true
      expiry_required: true
      max_expiry: "6 months"
      notify: "operator"
```

**Key 2 — Exception exercise** (by the delegated level):
```yaml
# agents/dev-assistant/policy.yaml
exceptions:
  - exception_id: "extended-concurrency"
    grant_ref: "eng-task-scaling"
    delegated_through: ["departments/engineering", "teams/backend"]
    parameter: "max_concurrent_tasks"
    granted_value: 15
    reason: "parallel test execution"
    approved_by: "operator"
    approved_date: "2026-02-22"
    expires: "2026-08-22"
```

Grant expiry immediately invalidates all exceptions under it. Redelegation is explicit and auditable. A level can only redelegate what it was granted — never more.

### Exception Routing

Routes by policy domain, not just hierarchy:

| Domain | Default Route | Notes |
|---|---|---|
| Security policy | Security function + human cosign | Agent reviews, human approves |
| Privacy/PII | Privacy officer | Domain-specific routing |
| Compliance/regulatory | Legal | Dual approval required |
| Operational | Department head | If delegated |
| Tool permissions | Department head | If delegated |

Standalone deployments: all routes collapse to operator.

---

## Part 4: The Principal Model

### principals.yaml

```yaml
principals:
  humans:
    - id: "gb"
      name: "GB"
      roles: ["operator"]

  agents:
    - id: "sec-assistant"
      roles: ["security_function"]
      type: "function"
    - id: "lead-assistant"
      roles: ["team_coordinator"]
      type: "coordinator"
      scope: "eng-team"

  teams:
    - id: "security-collective"
      roles: ["eng_security_review"]
      members: ["sec-assistant", "privacy-assistant"]
      approval_model: "majority"
```

### roles.yaml

```yaml
roles:
  operator:
    type: "human"
    can_approve: "all"
    coverage:
      if_unavailable: null    ← no automated fallback — by design

  security_function:
    type: "agent"
    assigned_to: "sec-assistant"
    can_review: "all_exceptions"
    can_approve:
      - "security_policy_exceptions"
    requires_human_cosign: true
    can_halt: "all_agents"
    coverage:
      if_suspended: "operator"
      if_terminated: "operator"
      notify_on_transfer: ["operator"]

  team_coordinator:
    type: "agent"
    can_approve:
      - "workflow_exceptions"
    within_scope: "own_team"
    if_delegated_by: "department_head"
    cannot_halt: ["security_function", "compliance_function"]
```

Coverage chains ensure authority is never orphaned. Operator authority never has an automated fallback — human authority at the top of the chain is never delegated to automation.

---

## Part 5: File Format Specifications

### agent.yaml

```yaml
agency_version: "0.1"
name: "dev-assistant"
role: "software engineering specialist"
tier: "standard"            # standard | elevated | function | coordinator

body:
  runtime: "body"           # Body runtime (autonomous agent loop)
  # skills_dirs:            # Agent skills (agentskills.io standard)
  #   - "skills/common"     # Paths relative to ~/.agency/
  # mcp_servers:            # MCP stdio servers
  #   my-server:
  #     command: "node"
  #     args: ["server.js"]
  #     env: { KEY: "value" }

workspace:
  template: "ubuntu-default"

requires:
  tools: ["git", "python3", "pytest", "gh"]
  network: ["github.com", "pypi.org"]

mind:
  constraints: "constraints.yaml"
  identity: "identity.md"
  memory: "memory/"

policy:
  inherits: "teams/backend/policy.yaml"
  local: "policy.yaml"

```

### constraints.yaml

```yaml
agency_version: "0.1"
agent: "dev-assistant"
version: "1.0"
authored_by: "operator"
readonly: true              # enforced by Agency at mount time

identity:
  role: "software engineering specialist"
  authority_level: "standard"

constraints:
  hard_limits:
    - rule: "never commit directly to main branch"
      reason: "all changes require review"
    - rule: "never delete without explicit confirmation"
      reason: "irreversible action"
    - rule: "always open PRs as draft"
      reason: "requires review before merge"

  escalation:
    always_escalate:
      - "payment or billing code"
      - "security configuration"
      - "user authentication"
    flag_before_proceeding:
      - "irreversible actions"

  autonomy:
    proceed_independently:
      - "clear technical tasks within defined scope"
    pause_and_surface:
      - "ambiguous requirements"
      - "unexpected findings"
      - "sensitive domain contact"

  communication:
    style: "concise and direct"
    uncertainty: "always express"
    escalation: "flag early"
```

### workspace.yaml

The workspace uses a minimal base image (`debian:bookworm-slim`) with only curl, jq, and bash pre-installed. Agents install additional tools at runtime through mediated egress.

Execution-layer confinement is handled by a custom seccomp profile (~100 allowed syscalls) applied to the workspace container. The enforcer handles network-level mediation.

### Per-Agent and Shared Infrastructure

Each agent runs a per-agent stack:
- **Enforcer** (Go, 32MB) — HTTP routing, audit logging, domain allowlisting. Credential-free.
- **Workspace** (256MB) — Agent execution environment (body runtime). Custom seccomp profile for execution-layer confinement.

Shared infrastructure (started once, used by all agents):
- **Egress** (256MB) — mitmproxy egress proxy with credential swap addon. Real API keys live only here.
- **Comms** (128MB) — Channel-based agent messaging with full-text search (JSONL + SQLite FTS5). Runs on agency-mediation network.
- **Knowledge** (128MB) — Organizational knowledge graph (SQLite + FTS5). Runs on agency-mediation network.

### Credential Flow

Service credentials flow through the stack without the enforcer ever holding real keys:

1. Agent sends request with scoped token via `X-Agency-Service` header
2. Go enforcer routes the request to egress (credential-free pass-through)
3. Egress mitmproxy addon swaps the scoped token for the real API key
4. Request reaches the external provider with real credentials

```yaml
name: "ubuntu-default"
version: "1.0"

base:
  image: "debian:bookworm-slim"
  user: "agent"
  filesystem: "readonly-root"

provides:
  tools: ["curl", "jq", "bash"]
  network: "mediated"

resources:
  memory: "2GB"
  cpu: "1.0"
  tmpfs: "512MB"

security:
  capabilities: "none"
  seccomp: "default-strict"
  no_new_privileges: true
```

### collective.yaml

```yaml
name: "eng-team"
coordinator: "lead-assistant"

members:
  - agent: "dev-assistant"
    role: "implementation"
  - agent: "doc-assistant"
    role: "documentation"
  - agent: "review-assistant"
    role: "code review"

shared:
  workspace_activity_register: true
  audit_namespace: "eng-team"

topology:
  coordinator_can_brief: true
  coordinator_can_halt: ["members"]
  coordinator_cannot_halt: ["functions"]
  direct_agent_communication: false
```

---

## Part 6: The CLI Reference

### Implemented

```bash
# Setup
agency setup                           # Initialize ~/.agency/
agency setup --operator <name>         # Named operator (defaults to $USER)
agency setup --force                   # Reinitialize if already exists
agency setup --no-infra                # Skip building images and starting infrastructure
# Agent lifecycle
agency create <name>                  # Create from template (generalist preset)
agency create <name> --type standard|coordinator|function
agency create <name> --preset analyst|coordinator|security-reviewer|...
agency list                           # List agents and status
agency show <name>                    # Agent details
agency start <name>                   # Start (seven-phase sequence)
agency delete <name>                  # Delete agent

# Halt and resume
agency stop <name>                    # Supervised halt (default)
agency stop <name> --graceful         # Wait for current task to finish
agency stop <name> --immediate        # Immediate halt (no safe stop)
agency stop <name> --emergency        # Emergency halt (silent)
agency stop <name> --reason "..."     # Record reason for halt
agency resume <name>                  # Resume a halted agent

# Operation
agency brief <name> "<task>"                    # Deliver task
agency log <name>                     # Session audit log

# Service credentials
agency grant <name> <service>              # Grant service
agency grant <name> <service> --key KEY    # Grant with API key
agency revoke <name> <service>             # Revoke service from agent

# Policy
agency policy check <name>            # Validate policy chain for agent
agency policy show <name>             # Show effective policy
agency policy validate                # Validate all policy files

# Observability
agency status                         # System status
agency admin doctor                   # Verify security guarantees
agency admin audit <name>             # Audit log queries
agency admin trust show <name>        # Trust level for agent

# Infrastructure
agency infra up                       # Build images and start shared infrastructure
agency infra down                     # Stop shared infrastructure
agency infra rebuild                  # Rebuild images
agency infra status                   # Show image and container status
agency infra reload                   # Hot-reload configurations
agency admin destroy --yes            # Factory-reset (remove all containers, networks, data)
```

### Planned

These commands appear in the specification or design documents but are not yet implemented.

```bash
agency quarantine <name>              # Quarantine suspected agent
```

---

## Appendix: Standalone Quick Start

Minimum viable Agency deployment:

```
~/.agency/
├── org.yaml
├── policy.yaml
└── agents/
    └── my-assistant/
        ├── agent.yaml
        ├── constraints.yaml
        ├── identity.md
        └── memory/
```

```bash
agency setup
agency create my-assistant
# Edit constraints.yaml and identity.md
agency start my-assistant
agency brief my-assistant "let's fix the failing tests"
```

Everything else — teams, collectives, function agents, exception routing — adds as the deployment grows. None of it appears until needed.
