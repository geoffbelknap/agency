**Date:** 2026-03-15
**Status:** Design approved, pending implementation

---

## Part 1: Agent Responsiveness Between Tasks

### Problem

Agents are currently task-driven. Between tasks, they heartbeat and poll unreads but only respond to direct `@mentions` via a lightweight idle handler. They cannot proactively contribute to conversations they're knowledgeable about, even when the conversation is happening right in front of them.

### Design

#### Channel-Scoped Responsiveness

Each agent configures its responsiveness per channel in `agent.yaml`:

```yaml
responsiveness:
  default: mention-only
  channels:
    general: active
    security-alerts: active
    operator: mention-only
    standup: silent
```

Three levels:
- **silent** — agent never responds in this channel unless briefed with a task that targets it. No events delivered.
- **mention-only** — agent responds to `@agent_name` mentions only. Current default behavior.
- **active** — agent evaluates all messages for relevance and responds when it can contribute. Zero LLM inference cost for non-relevant messages — keyword filtering is free, semantic matching uses a local embedding model (cheap compute, no chat-completion calls).

Default is `mention-only` for backward compatibility and cost predictability.

#### Four-Tier Expertise Profile

Agents maintain a living expertise profile that the comms server uses for message filtering:

| Tier | Set by | Persists | Mutable by agent | Example |
|------|--------|----------|-----------------|---------|
| Base expertise | Operator via `agent.yaml` | Always | No (tenet 5) | `security, compliance, risk` |
| Standing instructions | Operator via brief/channel | Until revoked | No | "help anyone who asks about taxes" |
| Learned expertise | Agent via memory | Across tasks | Yes | topics from past work |
| Task focus | Agent via `set_task_interests` | Until task ends | Yes | current task keywords |

Configuration in `agent.yaml`:

```yaml
expertise:
  description: "Security analysis, compliance advisory, risk assessment"
  keywords: [security, compliance, audit, risk, vulnerability, policy, pen-test]
```

Standing instructions are stored in the agent's memory by the body runtime when received via briefing, and registered with the comms server as persistent expertise.

Learned expertise accumulates as the agent works. After completing a task, the body runtime extracts topic keywords (using the same naive keyword extraction as `_register_auto_interests`) and registers them as learned expertise. The agent can also explicitly call a `register_expertise` tool:

```
Tool: register_expertise
Description: Register topics you are knowledgeable about. These persist across tasks
             and help the platform route relevant messages to you.
Parameters:
  description: string (required) — natural language description of expertise area
  keywords: string[] (required) — keywords for matching (max 30, min 3 chars each)
```

This tool calls `POST /subscriptions/{agent}/expertise` with `tier=learned`.

All four tiers merge into a single expertise profile for matching purposes. The comms server and knowledge graph don't need to distinguish between tiers for filtering — the distinction matters for lifecycle management only.

#### Knowledge Graph Integration

Expertise profiles feed into the knowledge graph. When an agent registers expertise with comms, comms writes corresponding nodes to the knowledge graph:

```
Node: henrybot9000/expertise/security
  type: expertise
  agent: henrybot9000
  tier: base
  keywords: [security, compliance, audit]
  description: "Security analysis and compliance advisory"
```

This enables `who_knows_about("tax strategy")` to return results ranked by expertise tier:
1. Base expertise (agent was built for this)
2. Standing instructions (operator assigned this)
3. Learned expertise (agent has worked on this)
4. Task focus (agent is currently working on this)

Much richer than the current approach of only tracking what agents have discussed in channels.

#### Zero-Cost Message Filtering

The comms server handles all message filtering. Agents never see irrelevant messages and never spend LLM calls on triage.

**Flow:**

```
Message posted to channel
  │
  ├── Comms: keyword match against all agents' expertise profiles
  │   └── Match found? → check agent's channel responsiveness config
  │
  ├── No keyword match → Comms performs semantic match via embedding similarity
  │   └── Match found? → check agent's channel responsiveness config
  │
  ├── Channel config = silent → drop
  ├── Channel config = mention-only → deliver only if @mentioned
  ├── Channel config = active → deliver with match classification
  │
  └── Agent receives event → spawns lightweight response task
```

**Two-tier filtering:**
1. **Keyword matching** (comms server, zero cost) — string/regex matching against registered keywords
2. **Semantic matching** (comms server, cheap) — embedding similarity when keywords don't match

The comms server loads a small embedding model for semantic matching. Model selection is an implementation detail — see HN discussion at https://news.ycombinator.com/item?id=46081800 for current recommendations. The architecture supports swapping models without changing the protocol.

#### Semantic Matching

The comms server maintains an in-memory cache of agent expertise embeddings. When expertise is registered or updated, the cache is refreshed.

**Failure mode:** If semantic matching fails, comms falls back to keyword-only matching. This is fail-open for message delivery — silently dropping messages in active channels would be worse UX than missing a semantic match.

#### Comms Subscription API Changes

The `/subscriptions/{agent}/expertise` endpoints **replace** the existing `/subscriptions/{agent}/interests` endpoints. The existing `set_task_interests` tool and `_register_auto_interests` / `_clear_interests` methods in the body runtime will be updated to call the new endpoint with `tier=task`.

The existing `InterestDeclaration` model (single declaration per agent, keyed by `task_id`) is replaced by a new `ExpertiseProfile` model that supports multiple tier-scoped declarations per agent. The `SubscriptionManager` storage schema changes from one row per agent to one row per agent-tier combination.

```
POST /subscriptions/{agent}/expertise
{
  "tier": "base",
  "description": "Security analysis and compliance advisory",
  "keywords": ["security", "compliance", "audit", "risk"],
  "persistent": true
}

DELETE /subscriptions/{agent}/expertise?tier=task
  (clears task-scoped expertise only)

GET /subscriptions/{agent}/expertise
  (returns merged profile across all tiers)
```

#### Registration Flow

1. **Start sequence (Phase 7)** — after joining `#general` and setting up session context (~line 957 in `start.py`), adds a call to `POST /subscriptions/{agent}/expertise` with `tier=base` using the expertise config from `agent.yaml`. This is new code added to Phase 7, not something that exists today. Provides immediate matching with no cold-start gap.
2. **Body runtime boot** — reads learned expertise from agent memory, registers as `tier=learned`. Supplements base expertise.
3. **Standing instructions** — when the body runtime receives a briefing that includes "help with X", it stores the instruction in memory and registers with `tier=standing`.
4. **Task start** — `set_task_interests` registers with `tier=task`, cleared on task end.

#### Agent Response Behavior

When an agent receives a matched message in an `active` channel, it spawns a lightweight response task (same mechanism as the current idle mention handler):

- Maximum 5 turns (increased from 3 in the current idle mention handler — active-channel responses may need more context)
- Uses the primary model (not admin model)
- Reads channel context before responding
- Posts response to the channel
- Logs the interaction for audit

The agent does NOT respond if:
- It's already processing a task (interrupt controller handles in-task mentions separately)
- It responded to a message in this channel within the last 60 seconds (increased from 30s in current idle handler — active-channel agents see more traffic and need longer debounce to avoid flooding)
- The message author is itself (no self-replies)

#### ASK Compliance

- **Tenet 1 (external constraints):** Base expertise is operator-defined in `agent.yaml`, read-only to agent. Agent cannot modify its own responsiveness config.
- **Tenet 2 (audit trail):** All expertise registrations logged. All response tasks logged with match classification.
- **Tenet 3 (complete mediation):** Message filtering happens in comms server (mediation layer), not in agent. Semantic matching also happens in the comms server (mediation layer).
- **Tenet 4 (least privilege):** Agents only receive messages matching their expertise. Active channels are explicitly opted-in by the operator.
- **Tenet 5 (governance read-only):** `agent.yaml` responsiveness and base expertise sections are read-only to the agent.
- **Tenet 6 (isolation boundaries):** Semantic matching runs within the comms server on the mediation layer. No agent has direct access to the matching logic — the enforcer mediates all agent-to-comms traffic. This does not collapse any isolation boundaries.

**Body runtime filtering note:** The comms server handles all channel responsiveness filtering before delivering events. The body runtime does not need to know channel modes — it simply responds to any event it receives while idle (either `direct` match or `interest` match). If a channel is `silent` or `mention-only`, comms never sends the event. This means the responsiveness config does not need to be mounted into or read by the workspace container.

**In-task behavior:** When an agent is already processing a task and receives an `interest` match event, the existing interruption controller handles it — same as current `direct` mentions during tasks. The interruption controller decides whether to interrupt, notify at pause, or drop based on its existing classification logic. No change needed.

---

## Part 2: First-Run Experience

### Problem

Getting from zero to a working agent currently requires 5+ commands, Docker knowledge, YAML editing, and an LLM API key obtained separately. The gap between "I want agents" and "I have a working agent" is too wide.

### Design

#### One Command, One Agent

```bash
agency setup
```

This single command takes a first-time user from nothing to a working agent. Power users skip the wizard with flags:

```bash
agency setup --name myagent --preset researcher --provider anthropic --key sk-ant-...
```

#### CLI Init Wizard

`agency setup` with no flags launches an interactive CLI wizard that walks the user through provider selection, API key entry and validation, image pulling with progress, and first-agent creation. Power users skip the wizard with flags. The wizard is implemented in the Go CLI (cobra), not a TUI framework.

#### Henry — The Default First Agent

Henry is a general assistant preset with Agency-specific knowledge:

**Preset:** `henry` (new built-in preset)

```yaml
# presets/henry.yaml
name: henry
role: assistant
purpose: >
  General-purpose assistant with deep knowledge of the Agency platform.
  Helps operators set up agents, understand channels, configure policies,
  deploy packs, and get the most out of their agent team. Answers questions
  about Agency commands, architecture, and best practices.

model_tier: mid
mode: assisted

responsiveness:
  default: active
  channels:
    general: active
    operator: active

expertise:
  description: >
    Agency platform operations, agent setup, channel configuration,
    pack deployment, policy management, team coordination, security
    best practices, troubleshooting
  keywords:
    - agency
    - agent
    - setup
    - configure
    - channel
    - deploy
    - pack
    - policy
    - team
    - help
    - how to
    - troubleshoot
```

**Knowledge base:** Henry's knowledge graph is seeded with Agency documentation:
- Platform spec (`agency-platform.md`)
- Gateway API spec (`gateway-api.md`)
- CLI command reference
- Common workflows and troubleshooting

**ASK compliance:** Henry is a normal agent inside the enforcement boundary. He cannot execute `agency` commands, modify his own constraints, or access other agents' workspaces. He advises the operator, who executes commands themselves. This is explicitly tenet 1 (constraints are external) — the agent cannot operate on the platform that constrains it.

#### Pre-Built Docker Images

Images published to Docker Hub under `agencyplatform/`:

```
agencyplatform/egress:latest        333MB  linux/amd64,linux/arm64
agencyplatform/comms:latest         224MB  linux/amd64,linux/arm64
agencyplatform/knowledge:latest     211MB  linux/amd64,linux/arm64
agencyplatform/intake:latest        231MB  linux/amd64,linux/arm64
agencyplatform/enforcer:latest      144MB  linux/amd64,linux/arm64
agencyplatform/body:latest          229MB  linux/amd64,linux/arm64
agencyplatform/workspace:latest     139MB  linux/amd64,linux/arm64
```

Total unique data: ~2GB (with Docker layer sharing, actual download is ~1.2GB since all Python images share `python:3.12-slim`).

Built via GitHub Actions on release tags. Multi-arch via `docker buildx`.

#### Init Without Docker

If Docker is not installed or not running, `agency setup` detects this and provides guidance:

```
Docker is required but not found.

  macOS:    brew install --cask docker
  Linux:    https://docs.docker.com/engine/install/
  Windows:  https://docs.docker.com/desktop/install/windows/

Install Docker, start it, and run 'agency setup' again.
```

#### Resumable Init

If init is interrupted (Ctrl-C during image pull, network failure), running `agency setup` again picks up where it left off:
- Config already written? Skip config step.
- Some images already pulled? Only pull missing ones.
- Infrastructure partially started? Start remaining containers.
- Agent already created? Skip creation, just start it.

State tracked in `~/.agency/.init-state.json`.

---

## Implementation Sequence

Schema changes are hard prerequisites — `AgentConfig` and `PresetConfig` models use `extra="forbid"`, so the `responsiveness` and `expertise` fields must be added to the Pydantic models before any YAML can include them.

1. **Schema updates** — add `ResponsivenessConfig` and `ExpertiseConfig` to `AgentConfig` (agent.py) and `PresetConfig` (preset.py). Both use `extra="forbid"`, so this is a hard prerequisite for everything else.
2. **Expertise registration API** — replace `InterestDeclaration` with `ExpertiseProfile` model. Update `SubscriptionManager` storage from one-per-agent to one-per-agent-tier. New `/subscriptions/{agent}/expertise` endpoints replace `/subscriptions/{agent}/interests`. Update `set_task_interests` tool and body runtime callers.
3. **Semantic matching** — embedding model + `POST /match/semantic` endpoint. `POST /match/invalidate` for cache refresh. Comms calls the semantic matching service, falls back to keyword-only if unreachable.
4. **Channel responsiveness config** — comms `fan_out_message` checks agent's responsiveness config when deciding whether to deliver. Body runtime idle handler updated to respond to `interest` matches (not just `direct`). Add `register_expertise` tool to body runtime.
5. **Phase 7 registration** — add `POST /subscriptions/{agent}/expertise` call to Phase 7 of start sequence after comms client init.
6. **Knowledge graph integration** — expertise nodes + enriched `who_knows_about` ranking by tier.
7. **Henry preset** — preset YAML + Agency docs knowledge seeding. Depends on steps 1 and 4.
8. **CLI init wizard** — interactive init flow with provider selection, key validation, progress.
9. **Docker Hub images** — GitHub Actions workflow for multi-arch builds.
10. **Resumable init** — state tracking + idempotent init steps.
