Missions are first-class managed objects that define an agent's ongoing purpose. A mission declares what an agent should do, what events trigger work, what capabilities are required, and what healthy operation looks like. Missions layer on top of agent identity — they direct behavior without changing personality.

An agent can hold exactly one mission at a time.

## Mission Object

A mission is a YAML file at `~/.agency/missions/\{name\}.yaml`:

```yaml
id: 8a3f2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c   # UUID, immutable, assigned at creation
name: ticket-triage
description: Review and respond to incident response tickets
version: 1                                     # incremented on each update
status: active          # active, paused, unassigned, completed
assigned_to: henrybot900  # agent name or team name
assigned_type: agent      # agent or team

# What the agent should do — injected into system prompt
instructions: |
  You are responsible for triaging incoming incident response tickets.
  For each new ticket:
  1. Assess severity (P1-P4) based on impact and urgency
  2. Assign appropriate responder tags
  3. Post initial assessment to #incidents
  4. Escalate P1/P2 to operator immediately

# What events activate this mission
triggers:
  - source: connector
    connector: jira-ops
    event_type: issue_created
  - source: channel
    channel: incidents
    match: "severity:*"

# Capabilities required to execute this mission
requires:
  capabilities: [jira]
  channels: [incidents, escalations]

# Health indicators — agent monitors these, alerts if violated
health:
  indicators:
    - "If tickets are in the queue but none have been triaged in 2 hours"
    - "If a P1/P2 ticket sits unacknowledged for more than 15 minutes"
  business_hours: "09:00-17:00 America/Los_Angeles"
```

### Field Details

- **`id`** — UUID generated at creation. Immutable. All audit events, knowledge graph nodes, and signals reference `mission_id`, not name. Survives renames, reassignments, and updates.
- **`name`** — Human-friendly handle used in CLI commands. Must be unique. Used as the filename (`\{name\}.yaml`). Required.
- **`description`** — One-line summary for display in `mission list`. Required.
- **`version`** — Integer, incremented on each `mission update`. Starts at 1.
- **`status`** — Lifecycle state: `unassigned` (defined but not assigned), `active` (running), `paused` (agent ignores triggers but remains running), `completed` (done, archived — terminal state, cannot be reassigned).
- **`assigned_to`** — Agent name or team name. Empty when `status: unassigned`.
- **`assigned_type`** — `agent` or `team`. Empty when unassigned.
- **`instructions`** — Natural language instructions injected into the agent's system prompt. Required.
- **`triggers`** — Events that wake the agent for mission work. Optional — without triggers, the agent only acts on @mentions and operator DMs. Schedule, webhook, and system event triggers are deferred to the event framework spec.
- **`requires`** — Prerequisites validated before assignment. Optional.
- **`requires.capabilities`** — List of capability names the agent must have granted.
- **`requires.channels`** — List of channel names the agent must have access to.
- **`health.indicators`** — Natural language conditions the agent monitors and alerts on. Optional.
- **`health.business_hours`** — Optional context for the agent to scope health monitoring. Format: `"HH:MM-HH:MM Timezone"`.

### Trigger Types

**Connector trigger** — fires when a named connector delivers a matching event type:
```yaml
- source: connector
  connector: jira-ops        # must match a deployed connector name
  event_type: issue_created   # must match an event type the connector emits
```

**Channel trigger** — fires when a message in a specific channel matches a glob pattern:
```yaml
- source: channel
  channel: incidents          # must match an existing channel
  match: "severity:*"         # glob pattern matched against message content
```

Match patterns use glob syntax: `*` matches any characters, `?` matches one character. Case-insensitive. If `match` is omitted, any message in the channel triggers.

### Event Object

Trigger evaluation consumes event objects delivered via the WebSocket listener. Events have a normalized shape regardless of source:

```json
{
  "type": "connector_event|message",
  "connector": "jira-ops",
  "event_type": "issue_created",
  "channel": "incidents",
  "content": "New ticket INC-1234: severity:critical ...",
  "message": {"id": "...", "author": "...", "content": "..."},
  "data": {}
}
```

Connector events carry `connector`, `event_type`, and `data` (the raw payload). Channel messages carry `channel`, `content`, and `message`. The trigger evaluator checks only the fields relevant to each trigger source.

Trigger events are data and timing signals, not instructions. The event tells the agent "something happened" — the mission instructions (from the operator, a verified principal) tell the agent how to respond. This distinction satisfies ASK tenet 17.

## Mission Lifecycle

```
unassigned → active → paused → active → completed (terminal)
```

A completed mission cannot be reassigned — create a new mission instead.

### CLI Commands

| Command | Effect |
|---------|--------|
| `agency mission create <file>` | Create mission from YAML, status=unassigned, generate UUID |
| `agency mission assign <mission> <agent\|team>` | Pre-flight check, inject into prompt, status=active |
| `agency mission pause <mission>` | Agent stops acting on triggers, still responds to @mentions |
| `agency mission resume <mission>` | Re-activate triggers |
| `agency mission update <mission> <file>` | Update instructions/triggers, atomic hot-reload |
| `agency mission complete <mission>` | Mark done, remove from agent prompt (terminal) |
| `agency mission show <mission>` | Current state, assigned agent, health, version |
| `agency mission list` | All missions with status and assignee |
| `agency mission history <mission>` | Audit trail of all changes |

### REST API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/missions` | Create mission (body: YAML content) |
| `GET` | `/api/v1/missions` | List all missions |
| `GET` | `/api/v1/missions/\{name\}` | Show mission detail |
| `POST` | `/api/v1/missions/\{name\}/assign` | Assign to agent/team (body: `\{"target": "...", "type": "agent|team"\}`) |
| `POST` | `/api/v1/missions/\{name\}/pause` | Pause mission |
| `POST` | `/api/v1/missions/\{name\}/resume` | Resume mission |
| `PUT` | `/api/v1/missions/\{name\}` | Update mission (body: YAML content) |
| `POST` | `/api/v1/missions/\{name\}/complete` | Complete mission (terminal) |
| `GET` | `/api/v1/missions/\{name\}/history` | Version history |
| `DELETE` | `/api/v1/missions/\{name\}` | Delete unassigned mission |

All endpoints are REST API consumers — CLI and MCP tools call these.

### MCP Tools

| Tool | Description |
|------|-------------|
| `agency_mission_create` | Create a mission |
| `agency_mission_assign` | Assign mission to agent or team |
| `agency_mission_pause` | Pause an active mission |
| `agency_mission_resume` | Resume a paused mission |
| `agency_mission_update` | Update mission instructions/triggers |
| `agency_mission_complete` | Complete a mission |
| `agency_mission_show` | Show mission detail |
| `agency_mission_list` | List all missions |
| `agency_mission_history` | Show version history |

### Assignment Pre-Flight

Before a mission is assigned, the platform validates:

1. Required capabilities are granted to the agent (or all team members for team missions)
2. Required channels exist and agent has access
3. Agent doesn't already have an active mission (reject unless `--force`)
4. For team missions with a coordinator: coordinator has all capabilities declared in `requires` (ASK tenet 11 — delegation cannot exceed delegator scope)

If any check fails, the assignment is rejected with specifics.

### Runtime Capability Loss

If a required capability is revoked while a mission is active:

1. Mission is automatically paused
2. Alert sent to operator via `#operator` channel and external notifications
3. Agent is informed: "Mission paused — required capability \{name\} is no longer available"
4. Mission remains paused until capability is restored and operator resumes

Same pattern for required channels being archived or agent being halted.

## System Prompt Integration

Mission content layers into the system prompt. This is a change from the current prompt assembly order in `body.py:assemble_system_prompt()` — mission blocks are inserted between identity and memory:

```
identity.md              ← who you are (personality, role)
mission instructions     ← what you're currently doing (NEW)
mission behavioral frame ← push back on unrelated tasks (NEW)
mission knowledge        ← queried from knowledge graph, scoped to mission_id (NEW)
memory index             ← agent's general memory
comms context            ← channels you're on
FRAMEWORK.md             ← ASK constraints
AGENTS.md                ← who else is around
skills                   ← tools available
```

### Active Mission Injection

When a mission is active, the body runtime injects two blocks:

**Mission instructions** (from `mission.yaml`):
```
## Current Mission: ticket-triage (id: 8a3f2b1c)

You are responsible for triaging incoming incident response tickets...

### Health Monitoring
Watch for these conditions and alert the operator if violated:
- If tickets are in the queue but none have been triaged in 2 hours
- If a P1/P2 ticket sits unacknowledged for more than 15 minutes
Business hours: 09:00-17:00 America/Los_Angeles
```

**Behavioral framing**:
```
You are assigned to mission "ticket-triage". This is your sole responsibility.
If you receive requests unrelated to this mission, politely decline and suggest
the requester find a more appropriate agent. Only respond to direct operator
instructions that override your mission.
```

### Paused State

When paused, the framing changes to:
```
Your mission "ticket-triage" is currently paused. Respond to @mentions
and operator DMs normally. Do not perform mission work until resumed.
```

### No Mission

When unassigned — no mission blocks in the prompt at all. Agent behaves as today.

## Mission Knowledge

Mission work generates knowledge scoped to the mission:

- When an agent calls `contribute_knowledge` during mission work, nodes are tagged with `mission_id`
- At prompt assembly, the body runtime queries the knowledge graph for the active mission's ID and injects relevant context
- Mission knowledge persists beyond agent lifecycle — if a mission is reassigned to a different agent, the new agent gets all accumulated knowledge (ASK tenet 23)
- When a mission is completed, its knowledge remains in the graph (searchable, referenceable) but stops being auto-injected

### Knowledge Access Boundaries (ASK Tenet 24)

Mission-scoped knowledge is accessible only to agents currently assigned to that mission. An agent on mission A cannot query mission B's knowledge unless explicitly authorized by the operator. The knowledge graph enforces this by checking the querying agent's active `mission_id` against the node's `mission_id` tag.

## Event-Driven Activation

### Current Model (No Mission)

The body runtime's WebSocket listener receives all channel messages. Each message is evaluated against interest keywords — if matched, an LLM call is made. This is expensive.

### Mission Model

With an active mission, the body runtime only wakes for:

1. **Mission trigger match** — event matches a trigger in `mission.yaml`
2. **Direct @mention** — someone explicitly addressed this agent
3. **Operator DM** — message in the agent's DM channel

Everything else is silently ignored. Zero LLM calls while waiting.

### Trigger Evaluation

Trigger matching happens in the body runtime's event handler, not the LLM. It's a deterministic check — no LLM call needed to decide whether to wake:

```python
def _is_mission_trigger(self, event):
    for trigger in self._active_mission.triggers:
        if trigger.source == "connector":
            if event.connector == trigger.connector and event.type == trigger.event_type:
                return True
        elif trigger.source == "channel":
            if event.channel == trigger.channel and glob_match(event.content, trigger.match):
                return True
    return False
```

When a trigger fires, the body runtime creates a task from the event — similar to idle-reply but with full mission context in the prompt and no artificial turn limit.

### Agents Without Missions

Agents without an active mission keep the current idle-reply behavior (interest keywords, @mentions). This preserves backward compatibility.

## Container Access

The mission file is mounted read-only into the agent's workspace container at `/agency/mission.yaml`, following the same pattern as `constraints.yaml` and `identity.md`. The body runtime reads it at startup and on hot-reload signals.

On mission assignment, the gateway:
1. Writes `~/.agency/missions/\{name\}.yaml` on the host
2. Mounts it into the workspace container bind mounts (requires container recreation on first assignment, or a restart)

On mission update (hot-reload), the file is already mounted — the body runtime re-reads it in place.

## Hot-Reload (ASK Tenets 1, 6, and 7)

### Atomic Updates

Mission updates must be atomic — the agent sees either the old or new instructions, never a mix. Mission updates are routed through the enforcer sidecar (not directly via comms), consistent with the mid-session constraint push architecture. This maintains the enforcer as the sole mediation point between the gateway and the agent (ASK tenet 1).

Update flow:
1. Operator runs `agency mission update <mission> <file>`
2. Gateway validates the new YAML, increments version
3. Gateway writes the new `mission.yaml` on the host (bind mount updates in-place)
4. Gateway appends the previous version to `~/.agency/missions/.history/\{id\}.jsonl` (immutable history — tenet 7)
5. Gateway signals the enforcer via WebSocket: `\{"type": "mission_update", "mission_id": "...", "version": N\}`
6. Enforcer forwards the signal to the body runtime
7. Body runtime finishes current in-progress task, then re-reads `/agency/mission.yaml`
8. Body runtime rebuilds system prompt with new instructions
9. Body runtime acknowledges via enforcer: `\{"type": "mission_update_ack", "mission_id": "...", "version": N\}`
10. Gateway verifies ack. If no ack within 60 seconds of the body going idle, gateway alerts operator (tenet 6)

The ack timeout starts from when the body is idle (not from signal send), so a long-running task does not trigger a false alarm. The gateway tracks the body's busy state via existing signals.

On rapid successive updates, the body always reads the latest file on disk when it goes idle. Intermediate versions are skipped. The ack includes the version number so the gateway knows which version was applied.

### Version History

Every version of a mission's instructions is preserved in `~/.agency/missions/.history/\{id\}.jsonl`. Each line is a complete snapshot:

```json
{"version": 1, "timestamp": "2026-03-23T10:00:00Z", "operator": "geoff", "content": "...full YAML..."}
```

This satisfies tenet 7 — "what instructions was the agent operating under when it took that action?" is always answerable by correlating audit event timestamps with mission version history.

## Health Monitoring

### Platform-Level (Automatic)

- Agent on active mission is stopped/paused → alert operator
- Required capability revoked → pause mission, alert operator
- Required channel archived → alert operator
- Mission update unacknowledged after body goes idle → alert operator

Platform-level health alerts are written to audit by the gateway (mediation layer), not the agent. This satisfies ASK tenet 2.

### Agent-Level (From Mission Config)

Health indicators in `mission.yaml` are natural language conditions injected into the agent's prompt. The agent interprets them in the context of its mission work and alerts proactively when violated. The agent sends alerts via `send_message` to `#operator` — these messages pass through comms (mediation layer) and are logged.

### Coordinator-Level (Team Missions)

The coordinator is responsible for team-level health monitoring. If the coordinator identifies systemic issues (work backing up, members unresponsive), it alerts the operator.

### Alert Delivery

All mission health alerts go to `#operator` channel with structured metadata:
```json
{"event_type": "mission_health", "mission_id": "8a3f...", "mission_name": "ticket-triage", "severity": "warning", "detail": "..."}
```

External notification forwarding is configured at the platform level in `~/.agency/config.yaml`:
```yaml
notifications:
  - type: ntfy
    url: https://ntfy.sh/my-agency-alerts
    events: [mission_health, agent_halted, quarantine]
  - type: webhook
    url: https://hooks.slack.com/...
    events: [mission_health]
```

The gateway forwards matching alerts to configured endpoints. Simple HTTP POST, no new infrastructure.

## Team Missions

### Assignment Flow

1. Pre-flight checks run against all team members (capabilities, channel access)
2. Mission status set to `active`, `assigned_to: team-name`, `assigned_type: team`
3. If team has a `coordinator`, mission instructions are injected into the coordinator's prompt only
4. If no coordinator, mission instructions are injected into ALL members' prompts — they self-organize

### Coordinator Model

The coordinator is the only agent that gets the full mission context. It decomposes the mission and delegates via channel messages to team members:

- Coordinator posts: "@analyst review ticket INC-1234"
- Team members respond to direct mentions (which always wake them)
- Coordinator tracks progress via channel conversation
- If a team member has their own mission, they push back — coordinator reassigns

### Delegation Bounds (ASK Tenet 11)

The coordinator can only delegate sub-tasks that fall within its own capability scope. If the coordinator doesn't have Jira access, it cannot delegate Jira work to a team member — even if that member has the capability. The platform validates this: when a coordinator briefs a team member via the task delivery API, the gateway checks that the coordinator's capabilities are a superset of the task's implied requirements.

### Coordinator Failure (ASK Tenet 14)

Teams must designate a `coverage` agent at mission assignment time — a team member who assumes coordination if the coordinator goes down.

If the coordinator goes down while a team mission is active:

1. Platform detects (agent stopped + active mission with `assigned_type: team`)
2. Mission instructions are injected into the coverage agent's prompt
3. Alert sent to operator: "Coordinator \{name\} for mission \{mission\} is down — \{coverage\} has assumed coordination"
4. Team members continue in-progress work; new assignments come from coverage agent
5. Operator can restart original coordinator (which reclaims coordination) or formally reassign

If no `coverage` is designated, the platform alerts the operator and team members continue in-progress work without new assignments until the operator intervenes.

### No-Coordinator Deconfliction

When no coordinator exists and all members get mission instructions, trigger events could cause multiple agents to act simultaneously on the same event. Deconfliction uses a claim mechanism:

1. Trigger event arrives to all team members
2. First agent to respond posts a claim message to the team channel: "Claiming INC-1234"
3. Other agents see the claim and skip the event
4. If no claim within 30 seconds, any unclaimed agent may proceed

The claim mechanism is convention-based (prompted in the behavioral framing) — the platform does not enforce exclusivity. This is sufficient for most team sizes. High-volume missions that need strict deconfliction should use a coordinator.

## Observable State

### `agency status`

Agents with missions show their mission state:
```
Agents:
  Name            Status    Enforcer    Build       Mission
  ────────────────────────────────────────────────────────────────────
  henrybot900     running   running     9b18c82 ✓   ticket-triage (active)
  analyst         running   running     9b18c82 ✓   —
```

### `agency mission list`

```
  Name              Status      Assigned To       Type
  ──────────────────────────────────────────────────────
  ticket-triage     active      henrybot900       agent
  security-patrol   paused      red-team          team
  log-analysis      unassigned  —                 —
```

### `agency mission show <mission>`

Full detail: id, instructions, triggers, requires, health indicators, current version, assigned agent/team, last activity timestamp, version history count.

## Audit Trail

All mission lifecycle events are written to the gateway audit log with `mission_id`:

- `mission_created` — id, name, version, operator
- `mission_assigned` — id, assigned_to, assigned_type, pre-flight results
- `mission_paused` — id, reason (operator, capability_revoked, etc.)
- `mission_resumed` — id, operator
- `mission_updated` — id, old_version, new_version, operator
- `mission_completed` — id, operator
- `mission_update_ack` — id, version, agent
- `mission_health_alert` — id, severity, detail, source (platform or agent name)

## Scope Boundaries

This spec covers missions only. The following are separate specs:

- **Meeseeks agents** — ephemeral single-purpose agents spawned by mission-assigned agents
- **Event framework** — extensible event-driven activation system with schedule, webhook, and system event triggers (missions use a simplified trigger model defined here)
- **Brief deprecation** — replacing briefs with channel/DM delivery and replacing turn limits with budget + progress tracking
- **External notifications** — the ntfy/webhook forwarding system (simple enough to implement alongside missions, but could be its own spec)

## What This Enables

- Agents have clear purpose without burning LLM calls on irrelevant chatter
- Operators can see at a glance what every agent is doing and whether it's healthy
- Mission knowledge accumulates and transfers — organizational intelligence survives agent churn
- Team coordination emerges from mission assignment, not ad-hoc briefing
- Standing instructions are versioned, auditable, hot-reloadable — not buried in identity files
