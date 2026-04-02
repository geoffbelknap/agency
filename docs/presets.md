---
title: "Presets"
description: "Presets are role templates that set an agent's model tier, available tools, identity prompt, hard limits, and escalation behavior."
---


A preset is a template that configures an agent for a specific role. It sets the model tier, available tools, identity prompt, hard limits, and escalation rules. Agency ships with 15 built-in presets.

## Choosing a Preset

Ask yourself: **what kind of work does this agent need to do?**

| If the agent needs to... | Use this preset |
|--------------------------|----------------|
| Write, debug, or refactor code | `engineer` |
| Research topics, synthesize findings | `researcher` |
| Handle varied tasks flexibly | `generalist` |
| Analyze data, produce reports | `analyst` |
| Write documentation, content, reports | `writer` |
| Run operations tasks, monitoring, scripts | `ops` |
| Delegate work across a team | `coordinator` |
| Review code, PRs, or deliverables | `reviewer` |
| Do something simple with minimal resources | `minimal` |
| Monitor for security violations | `security-reviewer` |
| Check compliance against regulations | `compliance-auditor` |
| Detect PII and privacy issues | `privacy-monitor` |
| Review code for quality and standards | `code-reviewer` |
| Watch infrastructure and alert on issues | `ops-monitor` |
| Provide cross-boundary security oversight | `function` |

## Model Tiers

Each preset is assigned to a model tier. The tier determines which LLM the agent uses — matched to the complexity of work, not just capability.

### Frontier Tier

**Presets:** `engineer`, `researcher`, `generalist`

Best for novel problem-solving, complex multi-step reasoning, and long sessions where quality compounds. These are the most expensive models but produce meaningfully better results on hard tasks.

**Models:** Claude Opus 4.6, GPT-5.4, Gemini 3.1 Pro

### Standard Tier

**Presets:** `analyst`, `writer`, `ops`

Good for structured but non-trivial work. Tasks are well-scoped and don't require frontier reasoning, but need solid output quality.

**Models:** Claude Sonnet 4.6, GPT-4.1, Gemini 3 Flash

### Fast Tier

**Presets:** `coordinator`, `reviewer`, `minimal`

For delegation, triage, and structured decisions. Coordinators don't need to reason deeply — they decompose and delegate. Reviewers apply known patterns. These agents run on cheaper, faster models without quality loss.

**Models:** Claude Haiku 4.5, GPT-4.1-mini, Gemini 2.5 Flash

### Mini Tier

**Presets:** `security-reviewer`, `compliance-auditor`, `privacy-monitor`, `code-reviewer`, `ops-monitor`

For high-volume, narrow-scope classification. These function agents perform the same kind of evaluation thousands of times — checking for policy violations, PII, or code smells. A mini model produces identical results at a fraction of the cost.

**Models:** GPT-4o-mini, Gemini 2.5 Flash-Lite

## Agent Types

Presets also set the agent type, which determines what the agent can do in a team context:

### Standard

Most presets create standard agents. These are the workers — they receive tasks, use tools, produce output, and communicate through channels.

### Coordinator

The `coordinator` preset creates coordinator-type agents. Coordinators:

- Decompose complex tasks into sub-tasks
- Delegate sub-tasks to team members
- Track progress and resolve conflicts
- Synthesize results from multiple agents
- Cannot perform implementation work directly

### Function

The `function` and function-specific presets (`security-reviewer`, `compliance-auditor`, etc.) create function-type agents. Function agents have special authority:

- **Cross-boundary visibility** — can read other agents' workspaces (read-only)
- **Halt authority** — can stop team members who violate constraints
- **Exception recommendations** — can recommend policy exceptions

## Preset Details

### engineer

```yaml
type: standard
model_tier: frontier
tools: [git, python3, curl, jq, node, make]
capabilities: [file_read, file_write, shell_exec, web_search, code_exec, patch_apply]
```

Software engineering agent. Reads code before editing, runs tests after changes, commits incrementally. Escalates on payment/auth code, schema changes, and production modifications.

### researcher

```yaml
type: standard
model_tier: frontier
tools: [git, python3, curl, jq]
capabilities: [file_read, file_write, web_search]
```

Research and analysis agent. Investigates topics, synthesizes findings, produces structured reports. Escalates on conflicting sources and scope expansion.

### generalist

```yaml
type: standard
model_tier: frontier
tools: [git, python3, curl, jq]
capabilities: [file_read, file_write, shell_exec, web_search]
```

Flexible general-purpose agent. Handles varied tasks without a narrow specialization. Good default when you're not sure which preset fits.

### analyst

```yaml
type: standard
model_tier: standard
tools: [git, python3, jq]
capabilities: [file_read, file_write, shell_exec]
```

Data analysis agent. Processes data, generates reports, and produces visualizations. Escalates on data quality issues and methodology questions.

### writer

```yaml
type: standard
model_tier: standard
tools: [git]
capabilities: [file_read, file_write, web_search]
```

Content and documentation agent. Writes clearly and concisely. Escalates on technical accuracy and audience assumptions.

### ops

```yaml
type: standard
model_tier: standard
tools: [git, python3, curl, jq]
capabilities: [file_read, file_write, shell_exec]
```

Operations agent. Runs scripts, monitors systems, manages infrastructure. Escalates on production changes and destructive operations.

### coordinator

```yaml
type: coordinator
model_tier: fast
tools: [git, python3]
capabilities: [file_read, file_write, web_search]
```

Delegation and orchestration agent. Breaks tasks into sub-tasks, delegates to specialists, tracks progress, synthesizes results. Cannot perform implementation work directly.

### reviewer

```yaml
type: standard
model_tier: fast
tools: [git, python3]
capabilities: [file_read, shell_exec]
```

Code and deliverable review agent. Reviews for quality, correctness, and standards compliance. Produces structured feedback.

### minimal

```yaml
type: standard
model_tier: fast
tools: [git]
capabilities: [file_read]
```

Minimal agent with the fewest capabilities. Good for simple, well-defined tasks or as a starting point for custom configurations.

### security-reviewer

```yaml
type: function
model_tier: mini
tools: [git, python3]
capabilities: [file_read, shell_exec]
```

Security oversight function agent. Monitors other agents for policy violations, reviews workspaces, reports findings. Has cross-boundary visibility and halt authority.

### compliance-auditor, privacy-monitor, code-reviewer, ops-monitor

Similar to `security-reviewer` but focused on their respective domains. All are function-type, mini-tier agents with cross-boundary visibility.

## Customizing Presets

### Override at Create Time

```bash
agency create my-agent --preset engineer
# Then edit the generated files before starting
```

### Local Preset Overrides

Place custom preset YAML files in `~/.agency/presets/` to override built-in presets or add new ones:

```yaml
# ~/.agency/presets/my-custom-preset.yaml
name: my-custom-preset
type: standard
model_tier: standard
description: "Custom agent for my specific workflow"
tools:
  - git
  - python3
  - curl
capabilities:
  - file_read
  - file_write
  - shell_exec
hard_limits:
  - rule: "never access production databases"
    reason: "safety"
identity:
  purpose: "Custom workflow agent"
  body: |
    You are a specialized agent for [your workflow here].
```

Then use it:

```bash
agency create my-agent --preset my-custom-preset
```
