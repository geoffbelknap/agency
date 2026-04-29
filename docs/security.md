---
title: "Security"
description: "Agency's structural security model enforces isolation, mediation, and audit on every agent — constraints agents cannot opt out of."
---


Agency's security model is structural — it's not something agents opt into, it's something they can't opt out of. This page explains what's enforced, how it works, and what operators need to know.

## Security Guarantees

Agency enforces these guarantees. Run `agency admin doctor` to verify them for any agent.

### 1. LLM Credentials Isolated

Agent runtimes never see API keys. The keys live in the credential store and
are resolved by the egress path at the network boundary. The agent talks to its
local enforcer, which forwards to egress, which injects the real credentials.
At no point does the agent runtime have access to the keys.

### 2. Service Credentials Isolated

When you `agency grant my-agent github`, the real GitHub token goes into the egress proxy's configuration. The agent receives a scoped token that the egress proxy swaps for the real credential at the network boundary.

### 3. Network Mediated

Agents have **no direct internet access**. All network traffic goes:

```
Agent → Enforcer → Egress Proxy → Internet
```

The enforcer handles domain allowlisting and audit logging. The egress proxy handles credential injection. There is no path from an agent runtime to the internet that bypasses this chain.

### 4. Constraints Read-Only

The agent's `constraints.yaml` is delivered read-only. The agent cannot modify,
delete, or replace its own constraints. This is verified during the start
sequence and checked by `agency admin doctor`.

### 5. Everything Logged

Audit logs are written by the infrastructure — the enforcer and egress proxy — not by the agent. The agent has **no write access** to the audit directory. Logs capture every LLM call, tool execution, network request, and state change.

### 6. Stoppable Immediately

`agency stop` halts any agent. Three tiers: supervised (graceful), immediate (SIGTERM), emergency (SIGKILL). The operator always has control. Emergency halt requires a reason for the audit trail.

### 7. MCP Tools Mediated

MCP tool usage is governed by policies:

- **Allowlist/Denylist** — Which tools an agent can call
- **Binary pinning** — SHA-256 verification that MCP server binaries haven't been modified
- **Output poisoning detection** — Scans MCP tool outputs for prompt injection
- **Cross-server attack detection** — Detects when one MCP server's output targets another

### 8. XPIA Scanning

All LLM requests pass through the enforcer for cross-prompt injection attack (XPIA) scanning. Tool-role messages are scanned for injection patterns and cross-tool references. Tool definition mutations are tracked and flagged.

### Also enforced

- **Audit logs tamper-proof** — HMAC-signed by the enforcer (ENFORCER_AUDIT_HMAC_KEY); integrity verifiable offline

## The ASK Framework

Agency implements the [ASK framework](https://github.com/geoffbelknap/ask) — 24 tenets for governing AI agents. Six core principles:

1. **Constraints are external and inviolable.** Enforcement runs outside the agent boundary.
2. **Every action leaves a trace.** Logs are written by mediation, not agents.
3. **Mediation is complete.** No unmediated path to external resources.
4. **Least privilege.** Access scoped to minimum required.
5. **Governance is operator-owned and read-only.** Agents can't modify their own rules.
6. **Each enforcement layer has its own isolation boundary.** Layers don't collapse.

When Agency blocks an operation, it reports the tenet number and explanation. These blocks are intentional and correct — the right response is to adjust your approach, not work around the block.

## Runtime Isolation

Each agent runs inside an isolated runtime boundary. The strategic Linux path
uses one Firecracker microVM per agent workload; the strategic macOS path is
`apple-vf-microvm` backed by Apple's Virtualization framework. The enforcer
stays outside the agent boundary so enforcement, mediation, and audit remain
external to the agent.

```
Per-Agent Runtime:
├── enforcer     — host-side or isolated microVM mediation process
└── workload VM  — isolated agent workspace with mediated network access
```

### Workspace Hardening

- **Read-only root filesystem** — Only `/workspace` and `/tmp` are writable
- **Least privilege** — No broad host or network capabilities
- **No direct internet** — DNS and TCP blocked; traffic routes through enforcer
- **No API keys** — Environment is clean; credentials exist only in egress
- **Skills mounted read-only** — Agent can read skills but not modify them

### Network Isolation

Runtime networking is scoped so the agent reaches only its own mediation path.
In the Firecracker path, the workload VM reaches the host through a per-agent
vsock bridge that forwards only to that agent's enforcer endpoint.

An agent can only reach its own enforcer. It cannot reach other agents, other
enforcers, shared infrastructure, or the internet directly.

## Credential Flow

```
Operator
    │
    ├── agency init --api-key KEY    → stored in ~/.agency/.env (mode 0600)
    ├── agency grant agent svc KEY   → stored in egress config
    │
    ▼
Egress Proxy (only component with real keys)
    │
    ├── LLM calls: enforcer → egress → provider API (key injected here)
    └── Service calls: enforcer → egress → service API (key swapped here)
```

The agent sends requests with scoped tokens. The egress proxy recognizes the token, swaps it for the real credential, and forwards the request. The response comes back through the same path. At no point does the agent see real credentials.

## Verifying Security

```bash
agency admin doctor
```

This checks:

- Runtime isolation (read-only constraints, least privilege)
- Network mediation (no direct internet from workspace)
- Credential isolation (no API keys in agent environment)
- Audit logging (infrastructure writing, not agent)
- Constraints read-only (`:ro` mount verified)
- Enforcer health (running, proxying correctly)

Run this after deploying any agent to verify all guarantees hold.

## Operator Responsibilities

While the platform enforces structural security, operators should:

1. **Keep API keys in environment variables**, not in command arguments or files
2. **Don't pass credentials as tool arguments** — use `agency grant` instead
3. **Monitor escalation channels** — agents flag issues they can't handle
4. **Treat agent output as untrusted** — especially channel messages and file contents
5. **Review audit logs** for unexpected patterns
6. **Run `agency admin doctor` regularly** to verify guarantees

## Threat Model

Agency defends against:

- **Agent escape** — Network mediation prevents direct internet access
- **Credential theft** — Keys never enter agent runtimes
- **Constraint tampering** — Read-only mounts prevent modification
- **Audit suppression** — Agents can't write to audit directories
- **Prompt injection (XPIA)** — Analysis service scans all LLM responses
- **MCP tool abuse** — Allowlist/denylist, binary pinning, output scanning
- **Lateral movement** — Per-agent networks prevent inter-agent communication outside channels
- **Privilege escalation** — Policy hierarchy only restricts, never expands

See `docs/threat-model.md` for the full threat model, known limitations, and accepted risks.
