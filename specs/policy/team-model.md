# Team Model

> **Tier:** Experimental. The `Teams` feature is `TierExperimental` in
> `internal/features/registry.go`. Not part of the default 0.2.x core
> surface; see [Core Pruning Rationale](../core-pruning-rationale.md).

## What This Document Covers

How Agency supports teams of humans and agents working together to operate an organization. The team model replaces the "collective" concept with a unified model where humans and agents are both members, roles flex based on need and scale, and the same primitives work from a standalone operator to a full department.

> **Status:** Design document. Replaces Multi-Agent-Coordination.md. The rename from "collective" to "team" and the core model changes are the first implementation step.

---

## Part 1: The Core Model

### Members, Not Operators and Agents

Everyone in a team is a **member**. Members have roles. Some members are human, some are agents. The distinction matters for trust (humans start trusted, agents earn it) and for what the platform manages (agents need containers, humans don't). But structurally, humans and agents are peers.

```yaml
# ~/.agency/teams/dev-team/team.yaml
name: dev-team
description: Product development team

members:
  - name: alice
    type: human
    role: lead
    responsibilities:
      - Architecture decisions
      - Code review approval
      - Sprint planning

  - name: dev-assistant
    type: agent
    role: developer
    responsibilities:
      - Feature implementation
      - Test writing
      - Bug fixes

  - name: review-assistant
    type: agent
    role: reviewer
    responsibilities:
      - Code review
      - Security scanning
      - Documentation checks

  - name: bob
    type: human
    role: developer
    responsibilities:
      - Frontend development
      - Design implementation
```

Alice and dev-assistant are both members. Alice can do work directly. Dev-assistant does work through its agent runtime. The team structure treats them the same.

### Roles Are Contextual

A member's role defines what they do in a given context, not what they are permanently. The same human can be:

- A **worker** on implementation tasks
- A **reviewer** for other members' output
- A **lead** setting direction for the team
- A **director** across multiple teams

The same agent can be:

- A **worker** executing assigned tasks
- A **coordinator** decomposing and delegating (if its constraints allow)
- A **function** agent providing cross-boundary oversight

Roles are defined per-team, not globally. Alice is a lead on dev-team and a worker on the docs-team.

### The Spectrum

The team model handles every point on the scale:

```
┌─────────────────────────────────────────────────────────────────┐
│  Standalone    Small team       Department       Organization  │
│                                                                │
│  1 human       1 human          5 humans         hierarchy     │
│  0-1 agents    3-5 agents       10+ agents       departments   │
│  no structure  flat roles       team leads       teams within  │
│  operator IS   operator         coordinator      teams         │
│  the team      directs          agents manage    policy layers │
│                                 day-to-day                     │
└─────────────────────────────────────────────────────────────────┘
```

At every point, the same primitives apply:

- **Members** with roles
- **Tasks** that get assigned and tracked
- **Communication** between members
- **Policy** governing what members can do
- **Audit** recording what happened

The system doesn't change shape at any threshold. A standalone operator with one agent is a team of two. Adding members is additive, not a migration.

---

## Part 2: Human Members

### What the Platform Manages for Humans

Humans don't run in containers. The platform manages their *relationship to the team*, not their execution:

- **Identity** — who they are, how they authenticate
- **Role** — what they do in this team
- **Authority** — what they can approve, delegate, halt
- **Communication** — how agents and other systems reach them
- **Presence** — whether they're available (for routing decisions)

### Human Authority

Humans have inherent authority that agents must earn. In the ASK framework:

- Humans can grant, modify, or revoke agent permissions
- Humans can halt any agent (at the appropriate tier)
- Human judgment is required for policy exceptions
- Humans approve high-stakes synthesis

But human authority is still bounded by their role in the team structure. A team member can't modify org-level policy. A department lead can't override platform hard floors. Authority flows from the structure, not from being human.

### Operator Backward Compatibility

The current "operator" is the simplest case: a standalone human who is the entire team. In the team model, the operator is a human member with the `lead` role and full authority over their scope.

```yaml
# Implicit team for a standalone operator (created by agency setup)
name: default
members:
  - name: operator
    type: human
    role: lead
```

`agency start dev-assistant` still works exactly as before — it starts an agent in the default team. The operator doesn't need to think about teams until they want structure.

---

## Part 3: Communication

### The Communication Problem

In the current model, communication is one-directional:
- Operator → agent (via `agency brief`)
- Agent → operator (via session output, audit logs)

A real team needs:
- Human → agent (task assignment, feedback, steering)
- Agent → human (questions, review requests, status updates, results)
- Human → human (out of band — Agency doesn't manage this)
- Agent → agent (through the platform, never directly)

### Communication Channels

Members communicate through **channels** managed by the platform. Channels are typed and audited.

**Task channel** — structured task delivery and results.
```
alice → dev-assistant: "implement the notification preferences feature"
dev-assistant → alice: {status: complete, result: "PR #42 ready for review"}
```

**Review channel** — requests for human judgment.
```
dev-assistant → alice: {type: review_request, context: "PR #42", question: "should notification frequency be user-configurable?"}
alice → dev-assistant: {type: review_response, decision: "yes, add a settings page"}
```

**Status channel** — ambient awareness (read-only for agents).
```
The workspace activity register, but expanded to include human presence and availability.
```

**Alert channel** — urgent notifications that need human attention.
```
review-assistant → alice: {type: alert, severity: high, message: "SQL injection vulnerability found in PR #42"}
```

### Agent-to-Agent Communication

Agents never communicate directly. All inter-agent coordination goes through the platform:

1. **Activity register** — ambient awareness of who's working on what
2. **Task delegation** — coordinator assigns tasks through the brief system
3. **Result collection** — completed work flows back through the platform

This is an ASK requirement (tenet 3: mediation is complete). Agent-to-agent messages would create an unmediated channel. The platform mediates all coordination.

### Human Communication Integrations

Humans communicate through their existing tools. Agency integrates with these rather than replacing them:

- **CLI** — `agency brief`, `agency log`, `agency session inspect`
- **Slack** — agents can post to channels, humans respond in Slack
- **Email** — for async notifications and summaries
- **agency-web** — web UI for real-time collaboration (merges separately)

The "Monday summary" pattern from the coordination doc is a communication output: the coordinator synthesizes the week's work and delivers it through the human's preferred channel.

---

## Part 4: Team Lifecycle

### Creating a Team

```bash
# Create a team with human lead
agency team create dev-team --lead alice

# Add agent members
agency team add dev-team dev-assistant --role developer
agency team add dev-team review-assistant --role reviewer

# Add human members
agency team add dev-team bob --type human --role developer
```

### Starting a Team

```bash
# Start all agent members in the team
agency team start dev-team

# This:
# 1. Validates team.yaml
# 2. Checks all agent members have valid configs
# 3. Starts each agent through the standard seven-phase sequence
# 4. Initializes the activity register
# 5. Wires communication channels
# 6. Reports team status
```

Individual agent start still works: `agency start dev-assistant` starts one member. The team-level start is a convenience that starts all agent members and wires them as a unit.

### Team Status

```bash
# Unified view of the team
agency team status dev-team

dev-team (4 members)
  alice          human    lead        available
  dev-assistant  agent    developer   autonomous  working in: src/api/
  review-assistant agent  reviewer    idle
  bob            human    developer   away

  Active tasks: 3
  Pending reviews: 1
  Last activity: 2 minutes ago
```

### Scaling the Team

Adding members to a running team:

```bash
# Add a new agent (starts immediately if team is running)
agency team add dev-team doc-assistant --role writer --start

# Add a human
agency team add dev-team carol --type human --role reviewer
```

Removing members:

```bash
# Remove an agent (halts gracefully)
agency team remove dev-team doc-assistant

# Remove a human
agency team remove dev-team carol
```

### Halting a Team

```bash
# Graceful halt of all agent members
agency team halt dev-team

# Emergency halt
agency team halt dev-team --emergency
```

---

## Part 5: Task Flow

### Human Assigns Work

The simplest flow: a human gives work to the team.

```bash
# Brief a specific agent
agency brief dev-assistant "fix the failing tests"

# Brief the team (coordinator or lead decides who does it)
agency team brief dev-team "implement notification preferences"
```

When briefing the team, the task goes to the coordinator agent (if one exists) or is presented to the human lead for assignment.

### Coordinator-Driven Workflow

When a coordinator agent exists, it can decompose and delegate:

```
alice → team: "implement notification preferences"
  ↓
coordinator receives task, decomposes:
  1. dev-assistant: implement backend API
  2. dev-assistant: implement frontend (depends on 1)
  3. review-assistant: review implementation (depends on 2)
  4. doc-assistant: update documentation (depends on 2)
  ↓
coordinator delegates sub-tasks as dependencies resolve
  ↓
coordinator collects results, synthesizes, reports to alice
```

The coordinator uses platform tools to delegate (not direct agent-to-agent communication). Each delegation is validated against the coordinator's delegation scope (tenet 12).

### Coordinator Agent Tools

A coordinator agent's body runtime includes additional tools for team management:

- `delegate_task(assignee, description, depends_on)` — assign work to a team member
- `check_task_status(task_id)` — check progress on a delegated task
- `collect_result(task_id)` — get the result of a completed task
- `request_review(reviewer, context, question)` — ask a human for a decision
- `team_status()` — see the activity register
- `report_to_lead(summary)` — deliver results to the team lead

These tools are implemented as MCP tools that call back into the Agency platform. They are only available to agents with the coordinator role.

### Human-in-the-Loop

At any point, a human can:

- **Steer** — redirect an agent's work mid-task
- **Review** — approve or reject output before it proceeds
- **Intervene** — halt an agent and take over
- **Delegate** — reassign work to a different member

```bash
# Steer a running agent
agency brief dev-assistant "actually, use WebSockets instead of polling"

# Review a pending synthesis
agency team reviews dev-team
agency team approve dev-team synth-20260307-abc123

# Take over from an agent
agency stop dev-assistant --reason "I'll finish this myself"
```

---

## Part 6: Shared Workspace

### The Problem

Multiple agents (and humans) working on the same codebase or dataset need a shared filesystem. Today, each agent has its own isolated workspace. This prevents collaboration on the same files.

### Shared Workspace Model

A team can declare a shared workspace — a filesystem that multiple members can access.

```yaml
# team.yaml
shared_workspace: /path/to/project

workspace_policy:
  conflict_resolution: yield    # yield | coordinator | operator
  file_locking: advisory        # advisory | mandatory | none
  human_paths_excluded: []      # paths humans are working in (agents avoid)
```

### Implementation

The shared workspace is mounted into each agent's container. Conflict detection uses the activity register:

1. Before an agent writes to a file, it checks the activity register for other agents working in the same area.
2. If a conflict is detected, the agent follows the `conflict_resolution` policy:
   - **yield** — back off, work on something else, flag to operator
   - **coordinator** — ask the coordinator to arbitrate
   - **operator** — flag to the human lead for a decision
3. All file modifications are tracked in the activity register.

Humans working in the shared workspace are outside Agency's control. The `human_paths_excluded` list lets the team declare areas where humans are working so agents avoid them.

### File Locking

Advisory locks prevent accidental conflicts:

```
dev-assistant: acquires advisory lock on src/api/notifications.py
bob (human): opens same file in editor → no lock (humans aren't managed)
dev-assistant: sees activity register update, yields
```

Advisory locks are best-effort. They prevent agent-agent conflicts. Human-agent conflicts are handled by convention (the activity register) and policy (yield to humans by default).

---

## Part 7: Policy Integration

### Team-Level Policy

Teams map to the existing policy hierarchy:

```
Platform defaults → org policy → department policy → team policy → member policy
```

A team can have its own `policy.yaml` that restricts (never expands) what members can do:

```yaml
# teams/dev-team/policy.yaml
parameters:
  risk_tolerance: medium
  max_concurrent_tasks: 3

capabilities:
  required:
    - github
    - brave-search
  denied:
    - production-deploy

rules:
  - description: All PRs require human review
    effect: require
    conditions:
      action: create_pull_request
    approval: human_member
```

### Role-Based Permissions

Roles carry default permission sets:

| Role | Default Permissions |
|---|---|
| lead | full authority within team scope, can delegate, can halt |
| coordinator | can delegate to team members, cannot exceed own permissions |
| developer | can work in workspace, cannot delegate |
| reviewer | read access to all team workspaces, can flag issues |
| function | cross-boundary read access, can halt, cannot modify workspaces |

These are defaults. Team policy can restrict them further.

---

## Part 8: Relationship to ASK Tenets

| Tenet | How It's Met |
|---|---|
| 1. Constraints are external | Team structure, roles, and policy are defined by humans, not agents. Agents cannot modify team.yaml. |
| 3. Mediation is complete | Agent-to-agent coordination goes through the platform. No direct communication channel. |
| 4. Access matches purpose | Roles define what each member can do. Coordinator delegation is bounded by the coordinator's own permissions. |
| 5. Superego is read-only | Team config, policy, and role definitions are read-only to agents. |
| 12. Delegation cannot exceed scope | Enforced at delegation time by DelegationValidator. |
| 13. Synthesis cannot exceed authorization | Enforced by SynthesisAuditor with human review for high-stakes outputs. |
| 21. Unknown conflict → yield | Default conflict resolution is yield. Agents do not force resolution. |

---

## Part 9: Migration from Collectives

The "collective" concept is renamed to "team" throughout:

| Old | New |
|---|---|
| `collective.yaml` | `team.yaml` |
| `~/.agency/collectives/` | `~/.agency/teams/` |
| `CollectiveConfig` | `TeamConfig` |
| `CollectiveMember` | `TeamMember` |
| `agency collective <cmd>` | `agency team <cmd>` |

The data model expands:

- `CollectiveMember` gains `type: Literal["human", "agent"]` (default: "agent" for backward compat)
- `TeamConfig` gains communication and workspace policy fields
- Activity register expands to track human presence
- New CLI commands: `team start`, `team halt`, `team status`, `team brief`

Existing `collective.yaml` files are read with the old field names and automatically mapped. No manual migration required.

---

## Part 10: Implementation Plan

### Phase 1: Rename and Model (collective → team)

- Rename `CollectiveConfig` → `TeamConfig`, `CollectiveMember` → `TeamMember`
- Rename `~/.agency/collectives/` → `~/.agency/teams/`
- Rename CLI group `collective` → `team`
- Add `type: Literal["human", "agent"]` to `TeamMember`
- Update all tests, docs, CLI help text
- No behavioral changes — just naming and the human member type field

### Phase 2: Team Lifecycle

- `agency team start <name>` — start all agent members
- `agency team halt <name>` — halt all agent members
- `agency team status <name>` — unified view of all members
- `agency team add/remove` — manage membership
- Activity register tracks human presence (manual or integration-driven)

### Phase 3: Communication Channels

- Review request/response flow between agents and humans
- Alert channel for urgent notifications
- Status channel (expanded activity register)
- Integration points for Slack, CLI, agency-web

### Phase 4: Coordinator Runtime Tools

- `delegate_task`, `check_task_status`, `collect_result` as MCP tools
- `request_review`, `team_status`, `report_to_lead`
- Available only to coordinator-role agents
- Wired into body runtime during Phase 3 of start sequence

### Phase 5: Shared Workspace

- Shared filesystem mount for team members
- Conflict detection via activity register
- Advisory file locking
- Human path exclusion

### Phase 6: Team Briefing

- `agency team brief <name> "<task>"` — brief the team as a unit
- Routing to coordinator or human lead
- Task decomposition and delegation flow
- Result collection and synthesis

---

*See also: Principal-Model.md for the authority model. Policy-Framework.md for the hierarchy. Agent-Lifecycle.md for the start sequence. MCP-OAuth.md for remote MCP server support.*
