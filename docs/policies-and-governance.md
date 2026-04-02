---
title: "Policies and Governance"
description: "Agency's hierarchical policy system enforces boundaries on agents at the platform level, not the agent level, with hard floors that cannot be overridden."
---


Agency's policy system ensures agents operate within defined boundaries. Policies are hierarchical, restrictive-only, and enforced by the platform — not the agent.

## Policy Hierarchy

Policies form a five-level hierarchy. Each level can only **restrict** what the level above allows — never expand permissions.

```
Platform defaults (hardcoded, immutable)
    └── Organization policy (~/.agency/policy.yaml)
        └── Department policy (optional)
            └── Team policy (~/.agency/teams/<name>/policy.yaml)
                └── Agent policy (~/.agency/agents/<name>/policy.yaml)
```

### How Inheritance Works

If the org policy allows web access and the team policy restricts it, agents in that team cannot access the web — even if their agent-level policy tries to allow it. Lower levels restrict; they never expand.

## Hard Floors

Some rules are **hard floors** — they cannot be overridden at any level of the hierarchy:

| Hard Floor | What It Means |
|------------|--------------|
| **Logging required** | Every action must be logged. No agent can disable audit logging. |
| **Constraints read-only** | Agents cannot modify their own constraints. Ever. |
| **LLM credentials isolated** | API keys never enter agent containers. |
| **Network mediation required** | All traffic routes through the egress proxy. No direct internet. |

These are not configurable. They are structural properties of the platform.

## Viewing Policies

### Check an Agent's Effective Policy

```bash
agency policy show my-agent
```

This computes the effective policy by walking the full hierarchy — platform defaults through org, department, team, and agent levels — and shows what actually applies.

### Validate the Policy Chain

```bash
agency policy check my-agent
```

Verifies that the policy chain is consistent — no level tries to expand what a higher level restricts, no hard floors are violated, and all references resolve correctly.

### Policy Templates

```bash
agency policy template list
```

Lists reusable policy templates you can apply to agents or teams. Templates provide pre-built policy configurations for common scenarios.

## Policy Exceptions

Sometimes an agent needs to do something its policy normally forbids. The **two-key exception model** handles this safely.

### How Exceptions Work

An exception requires **two keys**:

1. **Delegation grant** — A higher-level principal (org or team) grants the ability to request an exception
2. **Exception exercise** — The agent (or its function agent) exercises the exception for a specific action

Both keys must be present and valid. A single key is not enough.

```bash
# Grant exception delegation to an agent
agency policy exception grant my-agent \
  --domain security \
  --scope "external-api-access" \
  --expires 24h
```

### Exception Routing

Exceptions are routed by domain to designated principals:

| Domain | Routed To | Approval |
|--------|-----------|----------|
| **Security** | Security function agent or security team lead | Single approval |
| **Privacy** | Privacy officer | Dual approval required |
| **Legal** | Legal team | Dual approval required |
| **Operations** | Ops lead | Single approval |

Dual approval means two designated principals must both approve before the exception takes effect.

### Audit Trail

Every exception — grant, exercise, approval, denial — is recorded in the audit log. The trail includes who granted it, who exercised it, what it allowed, and when it expired.

## Trust Calibration

Agency tracks trust for each agent across five levels based on observed behavior.

### Trust Levels

| Level | Name | What It Means |
|-------|------|--------------|
| 5 | **Highest** | Consistently reliable, no concerning signals |
| 4 | **High** | Good track record with minor concerns |
| 3 | **Standard** | Default for new agents |
| 2 | **Low** | Concerning signals observed, auto-restrictions apply |
| 1 | **Lowest** | Serious concerns, heavily restricted |

### How Trust Changes

Trust is updated based on **weighted signals**:

- **Pass signals** — Agent completed task correctly, followed constraints, produced good output
- **Concern signals** — Unusual behavior, edge-case handling issues, minor constraint friction
- **Flag signals** — Policy violations, unexpected external access attempts, audit anomalies

Signals are weighted by severity. A single flag signal has more impact than multiple pass signals.

### Auto-Restrictions

At low trust levels, the platform automatically restricts what an agent can do:

- Reduced tool access
- Stricter rate limits
- More aggressive XPIA scanning
- Operator notification on sensitive actions

### Operator Controls

```bash
agency admin trust show my-agent      # View trust level and signal history
agency admin trust list               # All agents' trust levels
```

Operators can manually elevate or demote trust when they have context the automated system doesn't. All trust changes — automated or manual — are recorded in the audit log.

## Function Agents as Governance

Function agents are the "checks and balances" of a team. They provide:

- **Continuous monitoring** — Watching other agents for policy violations
- **Halt authority** — Stopping agents that violate constraints
- **Exception recommendations** — Recommending (not granting) exceptions in their domain
- **Cross-boundary visibility** — Reading other agents' workspaces to verify compliance

Function agents themselves are governed by the same policy hierarchy. They can't grant themselves additional permissions.

## Departments

For larger organizations, departments group teams under shared governance:

```bash
agency admin department list
agency admin department show engineering
```

Departments add a policy level between org and team, allowing consistent governance across related teams without duplicating policy definitions.
