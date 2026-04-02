---
title: "Packs"
description: "Packs are YAML files that declare an entire deployment — agents, teams, channels, and connectors — and are the recommended way to deploy to Agency."
---


A pack is a YAML file that declares an entire deployment — agents, teams, channels, and connectors — in one file. Packs are the recommended way to deploy multi-agent teams.

## What a Pack Looks Like

```yaml
kind: pack
name: security-ops
version: "1.0"
description: "Security operations team"

requires:
  - brave-search

team:
  name: security-ops
  members:
    - name: lead
      preset: coordinator
      role: coordinator
    - name: scanner
      preset: engineer
      role: worker
    - name: researcher
      preset: researcher
      role: worker
    - name: oversight
      preset: security-reviewer
      role: function

channels:
  - name: ops
    description: "Team coordination"
  - name: findings
    description: "Vulnerability findings"
  - name: escalations
    description: "Issues needing human attention"

recommended_connectors:
  - slack-events
```

## Deploying a Pack

```bash
agency deploy security-ops/pack.yaml
```

This single command:

1. Creates all channels
2. Creates the team
3. Creates all agents with their presets and roles
4. Starts all agents through the seven-phase start sequence
5. Activates recommended connectors (if installed)

The deployment order is handled automatically — channels and teams are created before agents, and agents are started in the right sequence.

## Tearing Down

```bash
agency teardown security-ops
```

This reverses the deployment:

1. Stops all agents (graceful halt)
2. Removes agents
3. Removes the team
4. Removes channels

Audit logs are preserved — teardown doesn't delete history.

## Pack Structure

### Required Fields

| Field | Description |
|-------|------------|
| `kind` | Must be `pack` |
| `name` | Pack name (used for teardown reference) |
| `version` | Semantic version |
| `description` | Human-readable description |

### Team Definition

```yaml
team:
  name: my-team
  members:
    - name: agent-name
      preset: preset-name        # Built-in or custom preset
      role: worker|coordinator|function|reviewer|lead
      config:                     # Optional overrides
        model_tier: standard      # Override preset's model tier
```

### Channels

```yaml
channels:
  - name: channel-name
    description: "What this channel is for"
```

### Requirements

```yaml
requires:
  - service-name              # Services that must be available
```

The deploy command checks that all requirements are met before proceeding.

### Recommended Connectors

```yaml
recommended_connectors:
  - connector-name            # Activated if installed
```

These are optional — deployment succeeds even if the connectors aren't installed.

## Writing Your Own Pack

1. **Start with the team structure.** Who are the agents? What roles do they play?

2. **Choose presets.** Match each agent to the right preset for their work (see [Presets](/presets)).

3. **Define channels.** What communication flows do the agents need? Typical patterns:
   - An `ops` channel for coordination
   - A `findings` or `results` channel for output
   - An `escalations` channel for issues needing human attention

4. **List requirements.** What services do agents need access to?

5. **Save as `pack.yaml`** and test with `agency deploy`.

### Example: Code Review Team

```yaml
kind: pack
name: code-review
version: "1.0"
description: "Automated code review team"

requires:
  - github

team:
  name: code-review
  members:
    - name: reviewer-lead
      preset: coordinator
      role: coordinator
    - name: quality-checker
      preset: code-reviewer
      role: function
    - name: security-checker
      preset: security-reviewer
      role: function
    - name: implementer
      preset: engineer
      role: worker

channels:
  - name: reviews
    description: "Code review assignments and results"
  - name: issues
    description: "Quality and security issues found"
```

### Example: Research Team

```yaml
kind: pack
name: research
version: "1.0"
description: "Research and analysis team"

requires:
  - brave-search

team:
  name: research
  members:
    - name: lead
      preset: coordinator
      role: coordinator
    - name: researcher-a
      preset: researcher
      role: worker
    - name: researcher-b
      preset: researcher
      role: worker
    - name: writer
      preset: writer
      role: worker

channels:
  - name: research-ops
    description: "Task coordination"
  - name: findings
    description: "Research findings and sources"
  - name: drafts
    description: "Draft documents for review"
```

## Sharing Packs via the Hub

Packs can be shared through the [Hub](/hub):

```bash
# Others can install your pack
agency hub install my-pack
agency deploy ~/.agency/hub-cache/my-pack/pack.yaml
```

See the [Hub](/hub) page for details on publishing and sharing components.
