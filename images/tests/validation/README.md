# Validation Runbooks

Step-by-step operator runbooks for validating every feature of the Agency
platform. Walk through each exercise via MCP tools (primary) or CLI,
observe the output, and mark PASS/FAIL.

## How to Use

**Pick a mode** and stick with it for the whole session:

| Mode | Tool | When |
|------|------|------|
| **MCP** | `agency_*` tools from Claude Code | Primary — this is how Agency is operated |
| **CLI** | `agency` shell commands | Alternate — validates CLI parity |

**Process:**
1. Run exercises in order within each group. Later groups may depend on earlier ones.
2. Mark each step PASS or FAIL. If a step fails, log it and continue.
3. Each exercise has a **Cleanup** section — always run it.
4. Record results in the tracker below.

**Prerequisites:**
- Docker running
- At least one LLM API key (Anthropic or OpenAI)
- Agency installed (`agency --help` works)
- For MCP mode: Agency MCP server connected in Claude Code

**Go Test Validation (after model/policy port):**

Run before any manual validation to ensure the model port is sound:

```bash
cd agency-gateway && go test ./internal/models/ ./internal/policy/ -v
```

Expected: 500+ tests pass. If any fail, fix before proceeding with manual validation.

**Time estimate:** ~45 minutes for a full run (groups 1–8, excluding swarm).

## Runbook Index

| File | Group | Exercises | Depends On |
|------|-------|-----------|------------|
| [01-platform-bootstrap.md](01-platform-bootstrap.md) | Platform Bootstrap | 2 | — |
| [02-agent-lifecycle.md](02-agent-lifecycle.md) | Agent Lifecycle | 4 | Group 1 |
| [03-capabilities.md](03-capabilities.md) | Capabilities | 5 | Group 2 |
| [04-communication.md](04-communication.md) | Communication & Knowledge | 6 | Group 1 |
| [05-security.md](05-security.md) | Security & Enforcement | 8 | Group 2 |
| [06-governance.md](06-governance.md) | Governance | 5 | Group 2 |
| [07-deploy-and-integration.md](07-deploy-and-integration.md) | Deploy & Integration | 4 | Group 1 |
| [08-admin-and-maintenance.md](08-admin-and-maintenance.md) | Admin & Maintenance | 4 | Group 4 |
| [09-swarm.md](09-swarm.md) | Swarm (Multi-Host) | 11 | Two-host setup |
| [10-model-validation.md](10-model-validation.md) | Model & Schema Validation | 3 | Group 1 |

## Focus Sets

Run subsets when working on specific areas:

| Set | Groups | When |
|-----|--------|------|
| **Smoke** | 1 | Quick sanity check |
| **Core** | 1, 2, 5 | Agent lifecycle + security — minimum before merge |
| **Comms** | 1, 4, 8 | Messaging, knowledge, and admin |
| **Governance** | 1, 2, 6 | Trust, policy, teams |
| **Full** | 1–8 | Before release |
| **Model Validation** | Go tests | After model/policy port — validates Go structs match Python schemas |
| **Post-Port** | Go tests + 10 | After model/policy port — validates Go validation layer end-to-end |

## Final Cleanup

After all exercises:

```
agency_infra_down()
```

Verify no orphaned containers:

```bash
docker ps -a --filter name=agency
```

Expected: No agency containers running.

---

## Results Tracker

Record results for each run. Copy this table and fill in.

| Exercise | Result | Notes |
|----------|--------|-------|
| **Group 1 — Platform Bootstrap** | | |
| Init & Infrastructure | | |
| Doctor & Status | | |
| **Group 2 — Agent Lifecycle** | | |
| Create & Configure | | |
| Seven-Phase Start | | |
| Brief & Task Delivery | | |
| Stop, Restart, Delete | | |
| **Group 3 — Capabilities** | | |
| Capability Registry | | |
| Service Credentials | | |
| Persistent Memory | | |
| Skills & Presets | | |
| Extra Mounts | | |
| **Group 4 — Communication** | | |
| Channels & Messaging | | |
| Full-Text Search | | |
| Real-Time Push | | |
| Interest Matching | | |
| Knowledge Graph | | |
| Knowledge Push | | |
| **Group 5 — Security** | | |
| Network Isolation | | |
| XPIA Scanning | | |
| Egress Domain Control | | |
| Credential Scoping | | |
| Budget Enforcement | | |
| Policy Hard Floors | | |
| Audit Integrity | | |
| Container Hardening | | |
| **Group 6 — Governance** | | |
| Trust Calibration | | |
| Policy Exceptions | | |
| Teams | | |
| Function Agent Authority | | |
| Capability Scoping | | |
| **Group 7 — Deploy & Integration** | | |
| Pack Deploy | | |
| Connectors | | |
| Intake | | |
| Hub Operations | | |
| **Group 8 — Admin & Maintenance** | | |
| Admin Model | | |
| Knowledge Curation | | |
| Departments | | |
| Audit Export & Retention | | |
| **Group 10 — Model Validation** | | |
| YAML Strict Validation | | |
| Connector Schema Validation | | |
| Pack Schema Validation | | |

---

## Issue Tracker

Log issues discovered during the run. Fix blockers immediately, defer the rest.

### Blockers

| ID | Exercise | Description | Fix |
|----|----------|-------------|-----|
| | | | |

### Bugs

| ID | Exercise | Description | Resolution |
|----|----------|-------------|------------|
| | | | |

### ASK Violations

| ID | Exercise | Tenet | Description | Resolution |
|----|----------|-------|-------------|------------|
| | | | | |
