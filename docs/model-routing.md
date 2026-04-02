---
title: "Model Routing"
description: "How Agency routes LLM calls to the right model for each agent to balance cost, capability, and performance across your fleet."
---


Agency agents make a lot of LLM calls. A team of 10 agents running for a day can easily consume millions of tokens. Choosing the right model for each agent type is the single biggest cost lever — often 10-50x difference for identical output quality.

## The Problem

Not every agent needs a frontier model. A function agent scanning messages for PII violations produces the same results on a $0.10/MTok model as on a $5.00/MTok model. But an engineer agent debugging a novel concurrency issue genuinely needs frontier reasoning.

## Five Tiers

Agency organizes models into tiers that map across providers:

| Tier | What It's For | Anthropic | OpenAI | Google |
|------|--------------|-----------|--------|--------|
| **Frontier** | Novel reasoning, complex code, architecture | Opus 4.6 | GPT-5.4 | Gemini 3.1 Pro |
| **Standard** | General work, analysis, writing | Sonnet 4.6 | GPT-4.1 | Gemini 3 Flash |
| **Fast** | Coordination, review, structured tasks | Haiku 4.5 | GPT-4.1-mini | Gemini 2.5 Flash |
| **Mini** | High-volume classification, triage | — | GPT-4o-mini | Gemini 2.5 Flash-Lite |
| **Nano** | Simplest extraction, routing | — | GPT-4.1-nano | Gemini 2.0 Flash-Lite |

Each [preset](/presets) specifies a `model_tier`. At start time, the platform resolves the tier to the best available model based on which provider credentials you've configured.

## How Routing Works

1. **Agent has a preset** → preset declares a `model_tier` (e.g., `frontier`)
2. **Platform checks configured credentials** → which providers are available?
3. **Platform checks provider preference** → operator-configured preference order
4. **First available model wins** → agent starts with that model

This means you can run the same pack on different providers just by changing which API keys are configured.

## Provider Preferences

Configure in `~/.agency/infrastructure/routing.yaml`:

```yaml
tiers:
  frontier:
    - model: claude-opus
      preference: 0            # First choice
    - model: gpt-5.4
      preference: 1
    - model: gemini-3.1-pro
      preference: 2

  standard:
    - model: gemini-3-flash
      preference: 0            # Cheapest with good quality
    - model: gpt-4.1
      preference: 1
    - model: claude-sonnet
      preference: 2

  fast:
    - model: claude-haiku
      preference: 0
    - model: gpt-4.1-mini
      preference: 1
    - model: gemini-2.5-flash
      preference: 2
```

Lower preference number = tried first. The platform picks the first model whose provider has credentials in `~/.agency/.env`.

## Why Caching Changes Everything

Agents are multi-turn by nature — 10-50 tool-use turns per task is typical. Every turn resends the system prompt, tool definitions, and conversation history. With prompt caching, most of that input is free on subsequent turns.

### The Math

For a 20-turn agent session:

| Provider | Base Input $/MTok | Effective with Caching | Why |
|----------|------------------|----------------------|-----|
| **Anthropic Sonnet** | $3.00 | ~$0.48/turn avg | 1.25x write once, then 0.1x reads dominate |
| **OpenAI GPT-4.1** | $2.00 | ~$1.29/turn avg | No write premium, but ~50% cache hit rate |
| **Google Gemini 3 Flash** | $0.50 | ~$0.29/turn avg | Implicit caching, best-effort, lowest base price |

Key insight: **Anthropic is actually cheaper than OpenAI for multi-turn** despite the higher sticker price, because its deterministic cache hits (90% off, guaranteed) beat OpenAI's best-effort caching (~50% hit rate).

The default provider preferences account for this.

## Preset Tier Assignments

| Tier | Presets | Why This Tier |
|------|---------|--------------|
| **Frontier** | engineer, researcher, generalist | Novel problem-solving, long sessions where reasoning quality compounds |
| **Standard** | analyst, writer, ops | Good output quality needed but tasks are well-scoped |
| **Fast** | coordinator, reviewer, minimal | Structured decisions — delegation, triage, pattern matching |
| **Mini** | security-reviewer, compliance-auditor, privacy-monitor, code-reviewer, ops-monitor | Same classification thousands of times |

## Evaluating Models

The eval framework benchmarks models against agent-shaped tasks:

```bash
agency eval list-tasks              # Available eval tasks
agency eval run --tier fast         # Benchmark fast-tier models
agency eval run                     # Full run across all tiers
agency eval report                  # View results
```

Tasks mirror real agent work: code generation, debugging, log analysis, policy checking, PII detection. Results inform the default preference order.

## Per-Agent Overrides

Override a preset's tier for a specific agent in `agent.yaml`:

```yaml
name: my-agent
preset: engineer
model_tier: standard              # Override: use standard instead of frontier
```

Or in a pack definition:

```yaml
members:
  - name: budget-engineer
    preset: engineer
    config:
      model_tier: standard        # Save money on less critical work
```

## Cost Optimization Tips

1. **Use the right preset.** A coordinator on frontier is wasting money — it just delegates.
2. **Consider caching behavior.** Anthropic's caching makes it cheaper for long sessions despite higher sticker price.
3. **Use mini-tier function agents.** Security/compliance scanning doesn't need reasoning — it needs classification.
4. **Run evals for your workload.** The default preferences are good, but your tasks may favor different models.
5. **Monitor with budget tracking.** The enforcer tracks spend per agent. Set soft warnings to catch runaway costs early.

## Budget Controls

Agency's enforcer provides three budget modes:

| Mode | Behavior |
|------|----------|
| **Hard cap** | Stops the agent when budget is reached |
| **Soft warning** | Warns the operator but lets the agent continue |
| **Notify only** | Logs the threshold crossing |

Configure budgets in the agent's policy or constraints. The enforcer tracks token usage across all agents and enforces limits centrally.
