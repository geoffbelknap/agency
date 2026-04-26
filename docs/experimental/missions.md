---
title: "Missions"
description: "Experimental standing instructions for agents. Missions declare what an agent should do, what events trigger work, and what healthy operation looks like."
---


A mission is a standing instruction that gives an agent ongoing purpose. Unlike ad-hoc tasks sent via `agency send`, a mission declares what an agent should do, what events trigger work, what capabilities are required, and what healthy operation looks like.

> Experimental surface: missions remain available for continued development, but
> they are not part of the default `0.2.x` core Agency path. The supported
> first-user workflow is still the direct-message path.

An agent can hold exactly one mission at a time.

## Why Missions

Without a mission, an agent responds to every channel message that matches its interest keywords — burning LLM calls on irrelevant chatter. With a mission, the agent only wakes for matching triggers, direct @mentions, and operator DMs. Zero LLM calls while idle.

Missions also make agent purpose visible. `agency status` shows what every agent is doing. `agency mission list` shows all missions and their state.

## Creating a Mission

Missions are YAML files. Create one at any path:

```yaml
name: ticket-triage
description: Review and respond to incident response tickets
instructions: |
  You are responsible for triaging incoming incident response tickets.
  For each new ticket:
  1. Assess severity (P1-P4) based on impact and urgency
  2. Assign appropriate responder tags
  3. Post initial assessment to #incidents
  4. Escalate P1/P2 to operator immediately

triggers:
  - source: connector
    connector: jira-ops
    event_type: issue_created
  - source: channel
    channel: incidents
    match: "severity:*"

requires:
  capabilities: [jira]
  channels: [incidents, escalations]

health:
  indicators:
    - "If tickets are in the queue but none have been triaged in 2 hours"
    - "If a P1/P2 ticket sits unacknowledged for more than 15 minutes"
  business_hours: "09:00-17:00 America/Los_Angeles"

budget:
  daily: 5.00
  monthly: 100.00
  per_task: 1.00

meeseeks: true
meeseeks_limit: 5
meeseeks_model: haiku
meeseeks_budget: 0.05
```

Register it with the platform:

```bash
agency mission create ticket-triage.yaml
```

The platform assigns a UUID, sets `version: 1`, and stores the mission at `~/.agency/missions/ticket-triage.yaml`.

### Field Reference

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique human-friendly handle. Used in CLI commands and as the filename. |
| `description` | Yes | One-line summary shown in `mission list`. |
| `instructions` | Yes | Natural language instructions injected into the agent's system prompt. |
| `triggers` | No | Events that wake the agent. Without triggers, the agent only acts on @mentions and operator DMs. |
| `requires.capabilities` | No | Capabilities the agent must have before assignment. |
| `requires.channels` | No | Channels the agent must have access to. |
| `health.indicators` | No | Conditions the agent monitors and alerts on. |
| `health.business_hours` | No | Scope for health monitoring. Format: `"HH:MM-HH:MM Timezone"`. |
| `budget` | No | Per-mission budget overrides (daily, monthly, per_task in USD). Cannot exceed platform defaults. |
| `meeseeks` | No | Allow this mission's agent to spawn [Meeseeks](/meeseeks). |
| `meeseeks_limit` | No | Max concurrent Meeseeks per parent (default: 5). |
| `meeseeks_model` | No | Default model for spawned Meeseeks (default: haiku). |
| `meeseeks_budget` | No | Default USD budget per Meeseeks (default: platform per_task). |

## Assigning to an Agent

```bash
agency mission assign ticket-triage henrybot900
```

### Pre-Flight Checks

Before assignment, the platform validates:

1. Required capabilities are granted to the agent
2. Required channels exist and the agent has access
3. The agent doesn't already have an active mission (use `--force` to override)
4. For team assignments: the coordinator has all required capabilities (delegation cannot exceed delegator scope)

If any check fails, the assignment is rejected with specifics.

### What Happens on Assignment

The platform injects mission instructions into the agent's system prompt — between identity and memory. The agent's behavior changes immediately:

- Mission instructions tell the agent what to do
- A behavioral frame tells the agent to focus on this mission and decline unrelated requests
- Mission triggers create [event subscriptions](/events) that wake the agent for matching events
- Health indicators are monitored by the agent

## Mission Lifecycle

```
unassigned → active → paused → active → completed (terminal)
```

### Pausing

```bash
agency mission pause ticket-triage
```

When paused, the agent stops acting on triggers but still responds to @mentions and operator DMs. Event subscriptions are deactivated, not deleted.

### Resuming

```bash
agency mission resume ticket-triage
```

Re-activates triggers and event subscriptions.

### Completing

```bash
agency mission complete ticket-triage
```

Marks the mission as done and removes it from the agent's prompt. This is a terminal state — completed missions cannot be reassigned. Create a new mission instead.

## Triggers

Triggers define what events wake the agent. Each trigger maps to a source type.

### Connector Trigger

Fires when a named connector delivers a matching event:

```yaml
triggers:
  - source: connector
    connector: jira-ops
    event_type: issue_created
```

### Channel Trigger

Fires when a message in a channel matches a glob pattern:

```yaml
triggers:
  - source: channel
    channel: incidents
    match: "severity:*"
```

Glob syntax: `*` matches any characters, `?` matches one character. Case-insensitive. Omit `match` to trigger on any message.

### Schedule Trigger

Fires on a cron schedule:

```yaml
triggers:
  - source: schedule
    name: daily-compliance-scan
    cron: "0 9 * * MON-FRI"
```

Timezone is derived from `health.business_hours`, or UTC if unspecified. The gateway manages the cron timer internally — no external cron daemon.

### Webhook Trigger

Fires when a registered inbound webhook receives a POST:

```yaml
triggers:
  - source: webhook
    name: deploy-notifications
    event_type: deployment_complete
```

See [Events and Webhooks](/events) for webhook setup.

### Platform Trigger

Fires on internal platform events:

```yaml
triggers:
  - source: platform
    event_type: agent_halted
```

Use this for missions that react to system state — "when any agent is halted, investigate and report."

## Health Indicators

Health indicators are natural language conditions injected into the agent's prompt. The agent monitors these during mission work and alerts the operator when violated.

```yaml
health:
  indicators:
    - "If tickets are in the queue but none have been triaged in 2 hours"
    - "If a P1/P2 ticket sits unacknowledged for more than 15 minutes"
  business_hours: "09:00-17:00 America/Los_Angeles"
```

The platform also monitors automatically:
- Agent on active mission stopped or paused
- Required capability revoked (mission auto-pauses)
- Required channel archived
- Mission update unacknowledged

All alerts go to the `#operator` channel and any configured [external notifications](/events#outbound-notifications).

## Hot-Reload

Update mission instructions without restarting the agent:

```bash
agency mission update ticket-triage updated-triage.yaml
```

The update is atomic — the agent sees either the old or new instructions, never a mix. The flow:

1. Gateway validates the new YAML and increments the version
2. New file is written to disk (bind mount updates in-place)
3. Previous version is appended to immutable history
4. Signal sent through the enforcer to the body runtime
5. Body runtime finishes its current task, then re-reads the mission file
6. Body runtime rebuilds the system prompt with new instructions
7. Body runtime acknowledges the update

If the agent doesn't acknowledge within 60 seconds of going idle, the gateway alerts the operator. Every version is preserved in `~/.agency/missions/.history/\{id\}.jsonl` for audit.

## Mission Knowledge

Knowledge generated during mission work is scoped to the mission:

- Agents tag knowledge contributions with the mission ID
- When the system prompt is assembled, relevant mission knowledge is injected
- Mission knowledge persists beyond agent lifecycle — if a mission is reassigned to a different agent, the new agent gets all accumulated knowledge
- Completed mission knowledge remains searchable but stops being auto-injected

Knowledge access is bounded by authorization. An agent on mission A cannot query mission B's knowledge unless explicitly authorized.

## Team Missions

Missions can be assigned to teams:

```bash
agency mission assign security-patrol red-team
```

### With a Coordinator

If the team has a coordinator agent, only the coordinator gets the full mission context. The coordinator decomposes the mission and delegates to team members via channel @mentions.

Team members respond to direct mentions (which always wake them). If a member has their own mission, they push back — the coordinator reassigns.

### Without a Coordinator

If no coordinator exists, all team members get the mission instructions and self-organize. A claim mechanism prevents duplicate work: the first agent to respond posts "Claiming INC-1234" to the team channel, and others skip the event.

For high-volume missions that need strict deconfliction, use a coordinator.

### Coverage Agent

Teams should designate a coverage agent. If the coordinator goes down, the coverage agent assumes coordination automatically. The platform alerts the operator and team members continue in-progress work.

```
Coordinator {name} for mission {mission} is down — {coverage} has assumed coordination
```

## Budget Overrides

Missions can override platform budget defaults (but cannot exceed them):

```yaml
budget:
  daily: 5.00       # USD per calendar day
  monthly: 100.00   # USD per calendar month
  per_task: 1.00     # USD per individual task
```

Budget enforcement happens at the enforcer level. When a limit is hit:
- **Per-task**: current task ends, next event gets fresh budget
- **Daily**: agent stops accepting tasks until midnight
- **Monthly**: mission auto-pauses, operator must resume

See `agency show <agent>` for current budget usage.

## CLI Commands

| Command | Description |
|---------|-------------|
| `agency mission create <file>` | Create mission from YAML file |
| `agency mission assign <mission> <agent\|team>` | Assign mission to agent or team |
| `agency mission pause <mission>` | Pause — stop acting on triggers |
| `agency mission resume <mission>` | Resume triggers |
| `agency mission update <mission> <file>` | Update instructions (hot-reload) |
| `agency mission complete <mission>` | Mark done (terminal) |
| `agency mission show <mission>` | Show detail: status, assignee, health, version |
| `agency mission list` | List all missions |
| `agency mission history <mission>` | Version history and audit trail |

## Example: Ticket Triage Mission

Create the mission file:

```yaml
name: ticket-triage
description: Triage incoming incident response tickets
instructions: |
  You are responsible for triaging incoming incident response tickets.
  For each new ticket:
  1. Read the ticket summary and description
  2. Assess severity (P1-P4) based on impact and urgency
  3. Post your assessment to #incidents with recommended responder tags
  4. For P1/P2, immediately alert the operator

triggers:
  - source: connector
    connector: jira-ops
    event_type: issue_created

requires:
  capabilities: [jira]
  channels: [incidents]

health:
  indicators:
    - "If a P1/P2 ticket sits unacknowledged for more than 15 minutes"
  business_hours: "09:00-17:00 America/Los_Angeles"

budget:
  per_task: 0.50
  daily: 5.00
```

Deploy it:

```bash
agency mission create ticket-triage.yaml
agency mission assign ticket-triage henrybot900
```

Now `henrybot900` wakes every time `jira-ops` delivers an `issue_created` event. Between events, zero LLM calls.

## Next Steps

- **[Meeseeks Agents](/meeseeks)** — Spawn ephemeral sub-task agents from mission-assigned agents
- **[Events and Webhooks](/events)** — How triggers connect to the event bus
- **[Teams](/teams)** — Multi-agent coordination with team missions
- **[Policies and Governance](/policies-and-governance)** — How budgets fit into the policy hierarchy
