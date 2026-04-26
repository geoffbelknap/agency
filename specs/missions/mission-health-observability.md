---
description: "Operators create missions, assign them to agents, and have no way to know if they're actually working. The current st..."
status: "Implemented"
---

# Mission Health Observability

**Date:** 2026-03-29
**Status:** Implemented

## Problem

Operators create missions, assign them to agents, and have no way to know if they're actually working. The current state:

- Mission says "active" but its trigger connector is returning 401s with 100+ consecutive failures
- No indication that the data pipeline feeding the mission is broken
- No alert when a mission hasn't received work in an abnormally long time
- Debugging requires checking intake logs, egress proxy logs, connector poll state, and agent DMs manually
- The `health.indicators` field in mission YAML is defined but never evaluated

An operator should be able to glance at a mission and immediately know: is it healthy, is it receiving work, and if not, why not.

## Design

### Mission Health Status

Every mission gets a computed `health` status derived from its dependencies:

| Status | Meaning |
|--------|---------|
| `healthy` | Triggers are firing, agent is responding, no errors |
| `degraded` | Working but with issues (connector warnings, slow response) |
| `unhealthy` | Broken — connector failing, agent not responding, or no tasks received |
| `unknown` | Not enough data yet (mission just assigned, first cycle pending) |

### Health Checks

The gateway evaluates these checks periodically (every 5 minutes) and on-demand via `agency mission health <name>`:

**1. Trigger Source Health**

For `source: connector` triggers:
- Is the connector active? (check hub registry state)
- Is the connector's poll succeeding? (check intake poll failure count)
- When was the last successful poll? (check intake poll state)
- Is the egress proxy allowing the connector's domains? (check egress policy + credential swap)

For `source: schedule` triggers:
- Has the cron fired recently? (check schedule state)
- Did the last firing produce a task? (check event log)

**2. Agent Responsiveness**

- Is the agent running? (check container state)
- When was the agent's last LLM call? (check routing metrics)
- Has the agent completed any tasks since assignment? (check signals)

**3. Pipeline Continuity**

- Time since last task delivered to this mission's agent
- Expected task frequency vs actual (derived from connector poll interval and historical rate)
- Work items created but not assigned (routing failure)

**4. Dependency Health**

- Required capabilities: are they enabled?
- Required channels: do they exist?
- Required services: are service keys configured?

### Health Response

```json
{
  "mission": "alert-triage",
  "status": "unhealthy",
  "checks": [
    {
      "name": "connector_health",
      "status": "fail",
      "detail": "limacharlie: 101 consecutive poll failures (401 Unauthorized)",
      "connector": "limacharlie"
    },
    {
      "name": "credential_health",
      "status": "fail",
      "detail": "limacharlie-api: service key not found in credential store",
      "service": "limacharlie-api"
    },
    {
      "name": "agent_running",
      "status": "pass",
      "detail": "alert-triage: workspace and enforcer running"
    },
    {
      "name": "last_task",
      "status": "warn",
      "detail": "No tasks delivered in 6h (expected: every 2m based on connector interval)"
    },
    {
      "name": "egress_domains",
      "status": "pass",
      "detail": "api.limacharlie.io: allowed"
    }
  ],
  "summary": "Connector limacharlie failing with 401. Service key limacharlie-api missing from credential store.",
  "last_task_at": null,
  "tasks_24h": 0
}
```

### CLI

```bash
# Quick health check
agency mission health alert-triage

# Output:
# ✗ alert-triage  UNHEALTHY
#
#   ✗ connector_health    limacharlie: 101 consecutive poll failures (401)
#   ✗ credential_health   limacharlie-api: service key not found
#   ✓ agent_running       alert-triage: running
#   ! last_task           No tasks delivered in 6h
#   ✓ egress_domains      api.limacharlie.io: allowed
#
#   Fix: Add limacharlie-api key to service credentials
#        echo 'limacharlie-api=YOUR_KEY' >> ~/.agency/infrastructure/.service-keys.env

# Health for all missions
agency mission health

# Output:
# NAME                 HEALTH       ISSUES
# alert-triage         unhealthy    connector failing (401), missing credential
# security-explorer    healthy      —
```

### REST API

```
GET /api/v1/missions/{name}/health → MissionHealthResponse
GET /api/v1/missions/health → { missions: [MissionHealthResponse, ...] }
```

### Web UI Integration

**Mission list**: show health dot next to each mission (green/amber/red).

**Mission detail**: health checks panel with the full check list, like Doctor but for a single mission's dependencies.

### Data Sources

The health checks pull from existing data — no new infrastructure needed:

| Check | Data Source |
|-------|------------|
| Connector poll state | `GET /intake/stats` (intake service) |
| Connector failures | Intake `poll_state.db` via REST |
| Service key presence | Check `.service-keys.env` for key_ref |
| Agent running | `GET /agents/{name}` (container state) |
| Last LLM call | Routing metrics (`by_agent`) |
| Egress domains | Domain provenance (`GET /egress/domains`) |
| Last task time | Work items DB (latest by connector + target) |
| Capability state | `GET /capabilities` |
| Channel existence | `GET /channels` |

### Implementation

**Gateway side** (`agency-gateway/internal/orchestrate/mission_health.go`):
- `MissionHealthChecker` struct with methods for each check
- `CheckHealth(mission) → MissionHealthResponse`
- Aggregates check results into overall status (any fail = unhealthy, any warn = degraded)

**API handler** (`agency-gateway/internal/api/handlers_missions.go`):
- `GET /missions/{name}/health` → runs checks, returns response
- `GET /missions/health` → runs for all active missions

**CLI** (`agency-gateway/internal/cli/commands.go`):
- `agency mission health [name]` → formatted output with fix suggestions

### Proactive Notifications

When a mission transitions from healthy to unhealthy:
- Emit an `operator_alert` platform event via the event bus
- Route to configured notification destinations (ntfy/webhook)
- Include the summary and suggested fix

This uses the existing notification infrastructure — no new delivery mechanism needed.

### Suggested Fixes

Each failing check includes a machine-readable `fix` field with the remediation command:

```json
{
  "name": "credential_health",
  "status": "fail",
  "fix": "echo 'limacharlie-api=YOUR_KEY' >> ~/.agency/infrastructure/.service-keys.env && agency infra rebuild egress"
}
```

The CLI renders these as actionable instructions. The web UI could render them as copy-paste commands.

### Scope

**In scope:**
- Health check evaluation for all trigger types
- CLI `mission health` command
- REST endpoint
- Fix suggestions for common issues
- Proactive notification on health transition

**Not in scope:**
- Automatic remediation (fix suggestions only)
- Historical health timeline
- Custom health check definitions beyond `health.indicators`
- SLA tracking (separate feature)
