---
title: "What is Agency?"
description: "Agency is a platform for running AI agents and teams of agents on real tasks like research, code review, and security audits, with enforced isolation."
---


Agency is a platform that lets you run AI agents — and teams of AI agents — that do real work for you. Research, code review, security audits, data analysis, writing, operations — you describe what you want done, and agents go do it.

## Why Not Just Use ChatGPT / Claude / Copilot?

You can — and for many tasks, that's the right call. But some work doesn't fit in a single conversation:

- **It takes hours, not minutes.** A security audit of a codebase. A research project across dozens of sources. Monitoring a system around the clock.
- **It takes a team, not one person.** One agent does the research, another writes the report, a third reviews it for accuracy. A coordinator keeps everyone on track.
- **It needs guardrails.** You want the agent to work autonomously, but you don't want it making API calls with your production credentials, accessing systems it shouldn't, or going off-script with no way to stop it.

Agency handles all three. You define agents, give them roles, set boundaries, and let them work — while the platform keeps everything safe and auditable.

## How It Works (The Simple Version)

Think of Agency like managing a small team:

1. **You hire people** → You create agents and give them roles (engineer, researcher, analyst, coordinator)
2. **You set expectations** → Each agent has constraints — what it can and can't do, what to escalate
3. **You assign work** → You brief agents on tasks, just like you'd assign work to a team member
4. **They collaborate** → Agents communicate through channels (like group chats) and share findings
5. **You stay in control** → You can check on progress, read their messages, and stop any agent at any time

The key difference from a real team: Agency enforces the boundaries automatically. Agents can't access things they shouldn't, can't see credentials, and can't modify their own rules. Every action is logged. This isn't trust — it's structure.

## What Does It Feel Like to Use?

There are two main ways to use Agency:

### Talk to an AI assistant (recommended for most users)

Agency plugs into Claude Code (or other AI assistants). You just have a conversation:

> **You:** "Create a research team — two researchers and a writer. Have them investigate the latest developments in battery technology and produce a summary report."

> **Assistant:** *Creates the agents, sets up channels, starts everything, briefs the coordinator*

> **You:** "How's the research going? Anything interesting so far?"

> **Assistant:** *Checks the channels, reads the findings* "They've found three promising papers on solid-state batteries. Researcher-1 is analyzing the MIT study, Researcher-2 is looking at the Toyota patent filing. No escalations."

> **You:** "Great. When the report is done, stop the team."

You don't need to know CLI commands, YAML, or Docker. The AI assistant handles all of that.

### Use the command line (for developers and power users)

```bash
agency create researcher --preset researcher
agency start researcher
agency brief researcher "Investigate recent developments in solid-state batteries"
agency log researcher
agency comms read findings
agency stop researcher
```

Both paths use the same platform underneath. The difference is just how you talk to it.

## What Keeps It Safe?

Every agent runs inside an isolated container — think of it as a locked room with controlled doors:

- **No internet access** — Agents can't browse the web directly. All requests go through a supervised proxy.
- **No credentials** — Agents never see your API keys. The platform injects them at the network boundary.
- **No rule changes** — Agents can't modify their own constraints. Rules are read-only.
- **Full audit trail** — Every action is logged by the platform, not the agent. The agent can't hide what it did.
- **Kill switch** — You can stop any agent immediately. Always.

This isn't about trusting the AI to behave — it's about making misbehavior structurally ~impossible~ more expensive.

## How Is This Different from OpenClaw?

[OpenClaw](https://github.com/openclaw/openclaw) is the most popular open-source AI agent platform (~400K users). It's a great project — but it solves a different problem than Agency.

**OpenClaw is a personal AI assistant.** It connects to 50+ messaging channels, runs sub-agents for coding, browsing, and deploying, and focuses on making AI accessible to everyone. Think of it as "AI that does things for you."

**Agency is a governed agent operating platform.** It focuses on running agents — especially teams of agents — with enforced security boundaries, credential isolation, and continuous audit. Think of it as "AI that does things for your organization, safely."

Here's where they differ in practice:

| | OpenClaw | Agency |
|---|---|---|
| **Agent isolation** | Agents share the host environment | Each agent runs in its own isolated container with a dedicated enforcer |
| **Credentials** | Agents can access configured API keys and services directly | Agents never see credentials — the egress proxy injects them at the network boundary |
| **Network access** | Agents make outbound requests directly | All traffic goes through a mediation layer; no direct internet access |
| **Audit** | Logging is application-level; agents can influence what gets logged | Logs are written by the mediation layer, not agents — agents cannot modify or suppress audit records |
| **Governance** | Community-built add-on ([Mission Control](https://github.com/abhi1693/openclaw-mission-control)) | Built into the platform from day one — hierarchical policy engine with immutable hard floors |
| **Halt / kill switch** | Process-level stop | Three-tier halt system (supervised → immediate → emergency quarantine) with full audit trail and asymmetric resume authority |
| **Prompt injection defense** | Known vulnerability; no structural mitigation | External inputs are data, never instructions (ASK Tenet 17) — enforced at the architecture level |
| **Multi-agent governance** | Orchestrator pattern with no native authorization model | Delegation cannot exceed delegator scope; combined outputs cannot exceed individual authorization |

None of this makes OpenClaw bad — it's excellent for personal use, hobbyist projects, and teams that are comfortable managing their own security. But if you need agents operating with real credentials, accessing sensitive systems, or working autonomously in an environment where "the agent went rogue" has actual consequences, you need the enforcement guarantees that Agency provides.

The short version: **OpenClaw trusts agents to behave. Agency makes misbehavior structurally impossible.**

## Who Is Agency For?

**Researchers and analysts** who need to investigate topics, process information, and produce reports — and want AI agents doing the legwork while they focus on the thinking.

**Development teams** who want AI agents handling code review, security scanning, testing, and documentation — with proper isolation so agents can't access production systems or leak credentials.

**Operations teams** who need always-on monitoring, incident response triage, and automated workflows — with audit trails for compliance.

**Anyone with complex, multi-step work** who wants to delegate to AI agents but needs confidence that the agents are operating within defined boundaries.

## Key Concepts (Plain Language)

| Concept | What It Is | Real-World Analogy |
|---------|-----------|-------------------|
| **Agent** | An AI worker that does tasks | A team member |
| **Preset** | A template that defines an agent's role | A job description |
| **Channel** | A message stream where agents communicate | A group chat |
| **Team** | A group of agents working together | A project team |
| **Coordinator** | An agent that assigns work to others | A team lead |
| **Pack** | A file that defines a whole team setup | An org chart + job descriptions |
| **Connector** | A link to an external system (Slack, Jira, etc.) | An integration |
| **Policy** | Rules that define what agents can do | Company policy |
| **Brief** | A task you give to an agent | A work assignment |

For the full list, see the [Glossary](/glossary).

## What's Next?

- **[Quick Start](/quickstart)** — Get your first agent running
- **[Core Concepts](/concepts)** — Deeper dive into how everything fits together
- **[Presets](/presets)** — Choose the right agent type for your work
