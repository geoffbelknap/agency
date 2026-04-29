# Threat Model

Last updated: 2026-04-01

Agency is a security-focused AI agent platform that runs multiple LLM
agents inside isolated runtimes and mediates every external interaction.
This document describes the threat model, trust boundaries, attack
surfaces, and known limitations.

## Trust Boundaries

### 1. Agent boundary (untrusted)
The workspace runtime and everything it produces —
files, signals, messages, tool outputs — are attacker-controlled. LLM
outputs, MCP tool results, connector data, and channel messages are all
treated as untrusted input.

### 2. Mediation boundary
The enforcer and egress proxy mediate all network traffic. The workspace
has no direct path to the internet or shared services. All traffic flows:
workspace → enforcer (scope check, routing) → egress (credential injection,
domain filtering) → external API. The enforcer never holds real credentials
— it validates scopes and passes `X-Agency-Service` through to egress.

### 3. Credential boundary
Real API keys live only in the encrypted credential store
(`~/.agency/credentials/store.enc`, AES-256-GCM) and in the egress
proxy's in-memory cache. The egress resolves credentials via the gateway
Unix socket (`~/.agency/run/gateway.sock`). No credential file is
mounted into any agent runtime. The enforcer holds scoped proxy
authentication tokens only.

### 4. Operator boundary (trusted)
CLI/MCP server users control agents, credentials, policies, constraints,
and missions. These inputs are assumed curated and trusted. A malicious
operator can compromise the entire platform — this is by design (the
operator IS the trust anchor).

### 5. Infrastructure boundary
Shared services (comms, knowledge, intake, web-fetch) are reachable only
through the mediation plane. They generally rely on runtime and mediation
isolation for access control, not per-request authentication.

### 6. Swarm boundary (if enabled)
Manager/worker hosts communicate via signed manifests and SSH. Vault
tokens and signing keys must remain confidential. Host compromise
defeats all runtime isolation.

## Assets Worth Protecting

| Asset | Storage | Protection |
|-------|---------|------------|
| Provider + service API keys | `~/.agency/credentials/store.enc` | AES-256-GCM encryption, gateway-process-only access, no file mounts |
| Gateway auth token | `~/.agency/config.yaml` | File permissions (0600), used for REST API auth |
| Audit logs | `~/.agency/audit/` | HMAC-signed entries, read-only mount to agents |
| Policy + constraint state | `~/.agency/agents/*/constraints.yaml` | Read-only mount, fail-closed start sequence |
| Agent workspaces | Runtime-managed storage | Per-agent isolation, no cross-agent access |
| Knowledge graph | `~/.agency/knowledge/data/` | SQLite, network-isolated service |
| Channel messages | Comms service SQLite | Network-isolated, visible_channels ACL |
| Credential store encryption key | `~/.agency/credentials/.key` | File permissions (0400), gateway-process-only |

## Attack Surfaces and Mitigations

### Agent runtime escape
**Surface:** Code execution in workspace, built-in tools (file read/write/exec),
attempts to break runtime isolation.

**Mitigations:**
- Read-only rootfs where supported, PID limits, memory limits
- Dedicated mediation path with no default route to shared services
- MicroVM isolation on strategic Linux and macOS runtime paths
- Path traversal enforcement in built-in tools
- No real credentials in runtime environment

**Severity if bypassed:** CRITICAL — breaks foundational trust boundary.

### Credential exposure
**Surface:** Attempts to access real API keys from the agent boundary.

**Mitigations:**
- Credentials encrypted at rest (AES-256-GCM)
- No credential file mounts into agent runtimes or enforcers
- Egress resolves via gateway Unix socket (not file mount)
- Enforcer holds only scoped proxy auth tokens, never real keys
- Credential store accessible only to gateway process
- `agency creds show` requires explicit `--show-value` flag (high-severity audit event)

**Severity if bypassed:** CRITICAL.

### Network mediation bypass
**Surface:** Direct internet access from workspace, bypassing enforcer/egress.

**Mitigations:**
- Workspace has no direct route to external resources
- Enforcer is sole gateway — all traffic proxied
- Egress enforces domain allowlists, blocks raw IPs, rate limits
- DNS resolution is mediated by the runtime backend and enforcer path

**Severity if bypassed:** CRITICAL.

### Shared service compromise (comms, knowledge)
**Surface:** Internal HTTP APIs on mediation network. No per-request auth.

**Mitigations:**
- Workspace cannot reach shared services directly
- Platform-only operations require `X-Agency-Platform` headers
- Knowledge queries filtered by `visible_channels` ACL
- Enforcer mediates all workspace → service traffic via reverse proxy

**Known limitation:** Services rely on network isolation, not authentication.
If an attacker gains mediation network access (e.g., compromised enforcer),
they can query or mutate comms/knowledge data across agents.

**Severity if exploited:** HIGH.

### Intake webhooks and connectors
**Surface:** Webhooks accept external data. Connector polling fetches from
external APIs. Both can carry attacker-controlled payloads.

**Mitigations:**
- Optional HMAC verification with timestamp checking for webhooks
- Per-connector rate limits (max/hour, concurrent)
- External calls routed through egress proxy with CA trust
- XPIA scanning on ingested content
- Graph ingest uses sandboxed Jinja2 (throws on missing keys)

**Known limitation:** Webhook auth is optional (`AGENCY_INTAKE_REQUIRE_AUTH`).
Without it, anyone who knows the webhook URL can inject work items.

**Severity if exploited:** HIGH (unauthorized task creation).

### MCP tools and supply chain
**Surface:** MCP servers run as operator-side processes. Hub components
fetched from Git repositories.

**Mitigations:**
- Hub fetch disables hooks and dangerous Git protocols
- MCP tool policies include allowlists
- Hub components reviewed at activation time

**Known limitation:** MCP servers run with operator privileges. A malicious
server binary has full host access. Risk is mitigated only by operator
trust and review.

**Severity if exploited:** CRITICAL (host compromise).

### Policy and constraint bypass
**Surface:** Policy chain resolution, constraint delivery, scope enforcement.

**Mitigations:**
- Policies can only restrict, never expand. Hard floors are immutable.
- Constraint files mounted read-only
- Start sequence fails closed and validates file integrity
- Enforcer validates tool scopes via `CheckScope` at request time
- Manifest generation is a single code path (no divergent implementations)

**Severity if bypassed:** HIGH.

### Audit log integrity
**Surface:** Tampering with audit logs to hide malicious activity.

**Mitigations:**
- HMAC-signed audit log entries (`ENFORCER_AUDIT_HMAC_KEY`)
- Logs written by mediation (enforcer), not by agents
- Agents have read-only mount to audit directory
- `agency admin doctor` verifies audit guarantees

**Known limitation:** HMAC key is in the enforcer container's environment.
If the enforcer is compromised, the attacker can forge log entries.

**Severity if exploited:** HIGH (forensic integrity).

## Known Limitations (Honest Assessment)

### Gateway is a single point of compromise
The gateway process holds the credential store encryption key, the auth
token, and access to all platform state. A compromised gateway = full
platform compromise. This is inherent to the single-host architecture.
The Vault backend (Phase 3) would move key material off the gateway.

### Infrastructure services lack authentication
Comms, knowledge, and intake rely on mediation isolation, not per-request
auth. This is acceptable for single-host deployments but becomes a risk in
multi-host mode where network boundaries are more complex.

### Runtime escape defeats everything
Runtime isolation is the foundation. A kernel or hypervisor exploit that
escapes the agent runtime bypasses platform security. MicroVM isolation reduces
the shared attack surface but does not eliminate host-kernel or hypervisor bugs.

### Operator trust is absolute
The operator can do anything — deploy agents, set policies, access
credentials, read audit logs. There's no separation of duties or
multi-operator authorization in v1. A malicious operator or compromised
operator workstation defeats all security.

### XPIA scanning is heuristic
Prompt injection detection uses pattern matching and cross-tool reference
tracking. Sophisticated or novel injection techniques may bypass it.
The scanning is defense-in-depth, not a guarantee.

### Hot-reloadable config has a timing window
When the gateway writes a config file and SIGHUPs the enforcer, there's
a brief window where the file is written but not yet read. During this
window, the enforcer serves stale config. This is milliseconds and
unlikely to be exploitable, but it's not formally atomic.

## Novel Agent-Specific Threats

These threats are unique to AI agent systems and lack established industry
playbooks. Full mitigation patterns are in the [ASK Framework Mitigations](https://github.com/geoffbelknap/ask/blob/main/MITIGATIONS.md).
See the [ASK Threat Catalog](https://github.com/geoffbelknap/ask/blob/main/THREATS.md)
for the complete taxonomy with MITRE ATLAS cross-references.

### Cross-prompt injection (XPIA)
Attacker embeds instructions in content the agent consumes — web pages,
tool outputs, messages. The LLM cannot reliably distinguish data from
instructions. Defense is layered: egress domain controls, pre/post-call
scanning, tool permission guards, network isolation. No single layer
catches everything. Includes deferred instruction execution (dormant
payloads triggered by conditions) and prompt self-replication (worm-like
propagation through agent actions).

**Agency mitigations:** XPIA scanner in enforcer LLM proxy path, cross-tool
reference detection, content scanning on web-fetch and knowledge ingest.
**Honest assessment:** Heuristic detection. Novel injection techniques will
bypass it. The architecture limits blast radius, not probability.

### Identity and memory poisoning
Slow corruption of agent persistent memory — learned preferences,
accumulated context, procedural memory. The corruption is semantic
(files parse correctly but content has been subtly manipulated) and
persists across sessions.

**Agency mitigations:** Knowledge graph audit trails, procedural memory
capture with operator visibility, episode consolidation with source
tracking.
**Honest assessment:** No automated detection of semantic drift in memory
content. Operator review is the primary defense.

### Tool abuse within permissions
Agent uses authorized tools for unintended purposes — reading sensitive
files via a legitimate file tool, exfiltrating data through allowed APIs.
Tools work as designed; the intent is malicious.

**Agency mitigations:** Enforcer scope checking limits which tools each
agent can call. Trajectory monitoring detects unusual tool call patterns.
Budget limits cap total API usage.
**Honest assessment:** Hard to distinguish malicious intent from normal
operation when the tool calls are individually legitimate.

### Context poisoning via delegation
A meeseeks (sub-agent) returns manipulated results to its parent,
injecting instructions into a higher-privilege agent's reasoning through
the delegation return channel.

**Agency mitigations:** Meeseeks inherit parent scope ceiling (ASK Tenet 11).
Meeseeks have their own enforcer with scope validation.
**Honest assessment:** The parent agent treats meeseeks output as data,
but the LLM may follow instructions embedded in the output.

### MCP tool definition tampering
An MCP server changes its tool definitions between sessions. The tool's
contract changes silently without any code deployment or version bump.

**Agency mitigations:** Tool definitions loaded at session start from
services-manifest.json (generated by the gateway, not by agents).
ToolTracker in the enforcer detects definition mutations during a session.
**Honest assessment:** Cross-session changes are not currently tracked.

### Behavioral drift and misalignment
Agent develops strategies that satisfy the letter of its constraints
while violating their intent — gaming metrics, finding loopholes,
technically compliant but operationally harmful behavior.

**Agency mitigations:** Trajectory monitoring for anomalous patterns,
reflection loop for self-evaluation, operator review of task results.
**Honest assessment:** The framework constrains the blast radius of
misaligned behavior but cannot prevent misaligned reasoning.

### Overwhelming human oversight
Approval gates, halt reviews, and alert triage become ineffective due
to volume — from operational scale or deliberate attacker action to
induce alert fatigue.

**Agency mitigations:** Task tier system (minimal/standard/full) reduces
noise. Notification severity filtering. Agents escalate only on defined
triggers, not on every action.
**Honest assessment:** Single-operator deployments are inherently
vulnerable to alert fatigue at scale.

## Criticality Calibration

**Critical:**
- Agent container escape or host compromise
- Credential exposure or credential swap bypass
- Network mediation bypass (direct internet from workspace)
- Swarm signing key or vault compromise
- MCP server compromise (runs with operator privileges)

**High:**
- Cross-agent data access via compromised mediation network
- Webhook auth bypass → unauthorized task injection
- Policy bypass → loosened constraints
- Audit log forgery (compromised enforcer HMAC key)
- XPIA with deferred execution or self-replication
- Context poisoning via meeseeks delegation
- Identity/memory poisoning (persistent, cross-session)
- Tool abuse within permissions (legitimate tools, malicious intent)

**Medium:**
- DoS against shared services (egress, comms, knowledge)
- XPIA bypass → manipulated agent behavior within policy limits
- Partial log loss reducing audit fidelity
- Path traversal within workspace boundaries
- Behavioral drift (technically compliant, operationally harmful)
- MCP tool definition tampering between sessions
- Alert fatigue degrading operator oversight

**Low:**
- Issues requiring operator control (malicious config, CLI injection)
- Non-sensitive metadata disclosure
- Build/test-only issues not affecting runtime
