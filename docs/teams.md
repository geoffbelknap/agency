---
title: "Teams"
description: "A team groups agents and humans together with defined roles. Teams enable coordination, delegation, and oversight at scale."
---


A team groups agents and humans together with defined roles. Teams enable coordination, delegation, and oversight at scale.

## Creating a Team

```bash
agency team create security-ops
```

### Adding Members

Members are added when creating agents or through team configuration. Agents specify their team membership in `agent.yaml`.

When using [packs](/packs), team membership is declared in the pack YAML — all agents, roles, and channels are created together.

## Team Roles

Each team member has a role that determines their authority:

| Role | What They Can Do |
|------|-----------------|
| **worker** / **standard** | Perform assigned tasks, communicate on channels |
| **reviewer** | Review work from other team members |
| **lead** | Direct work within the team |
| **director** | Strategic oversight, cross-team coordination |
| **coordinator** | Decompose tasks, delegate to team members, synthesize results |
| **function** | Cross-boundary oversight with halt authority |

## How Coordination Works

### Task Decomposition

A coordinator agent receives a complex task and breaks it into sub-tasks:

```
Operator → "Audit the authentication system for vulnerabilities"
    │
    ▼
Coordinator decomposes:
    ├── "Review auth middleware code" → engineer-a
    ├── "Check for known CVEs in dependencies" → researcher-b
    └── "Scan for OWASP Top 10 issues" → security-reviewer-c
```

The coordinator delegates through channels — posting task assignments and reading completion reports.

### Synthesis

When sub-tasks complete, the coordinator synthesizes results into a coherent output. The platform audits this synthesis to ensure all delegate outputs are accounted for.

### Delegation Validation

The platform validates delegations:
- Coordinators can only delegate to team members
- Delegation respects the policy hierarchy
- All delegations are logged in the audit trail

## Function Agents in Teams

Function agents have special authority within their team:

### Cross-Boundary Visibility

Function agents can read other team members' workspaces — but only read, never write. This is implemented as read-only volume mounts resolved during the start sequence.

```python
# Inside a security-reviewer function agent
read_file("/visibility/engineer-a/workspace/auth.py")
```

### Halt Authority

Function agents can halt team members who violate constraints:

```python
# Inside a security-reviewer function agent
halt_agent("engineer-a", reason="Detected credential in source code")
```

This triggers an immediate halt of the target agent. The halt is logged in the audit trail with the function agent's identity and reason.

### Exception Recommendations

Function agents can recommend policy exceptions for their domain (security, privacy, compliance). These recommendations go through the [exception routing](/policies-and-governance) system.

## Team Activity

Monitor team-wide activity:

```bash
agency team show security-ops        # Team details and members
agency team activity security-ops    # Activity log across all members
agency team list                     # List all teams
```

## Example: Security Review Team

```bash
# Create channels
agency comms create findings
agency comms create escalations

# Create the team
agency team create security-ops

# Create agents
agency create lead --preset coordinator
agency create scanner-1 --preset engineer
agency create scanner-2 --preset researcher
agency create oversight --preset security-reviewer

# Start all agents
agency start scanner-1
agency start scanner-2
agency start oversight
agency start lead

# Brief the coordinator
agency brief lead "Perform a security audit of the /app directory.
  scanner-1 should focus on code review, scanner-2 on dependency analysis.
  Post findings to the findings channel. Escalate critical issues to escalations."
```

Or, more practically, define this as a [pack](/packs) and deploy it in one command.

## Scaling Teams

Agency's team model scales from simple to complex:

| Scale | Setup |
|-------|-------|
| **Solo** | 1 operator + 1 agent |
| **Small team** | 1 operator + 2-5 agents + channels |
| **Coordinated team** | 1 coordinator + workers + function agents + channels |
| **Department** | Multiple teams organized under a department with shared policies |

The same security model and primitives work at every scale. Adding a coordinator doesn't change how worker agents operate — it adds a layer of task management on top.
