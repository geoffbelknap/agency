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
- A supported container backend running for the lane you are validating
  (Docker, Podman, or containerd)
- At least one LLM API key (Anthropic or OpenAI) for model-backed exercises
- Agency installed (`agency --help` works)
- For MCP mode: Agency MCP server connected in Claude Code

**Automated validation before manual runbooks:**

Run the smallest sufficient automated lane first. Useful defaults:

```bash
go test ./...
./scripts/python-image-tests.sh
make web-test-all
./scripts/runtime-contract-smoke.sh --agent <agent>
```

For backend-specific readiness, use the scoped adapter lane:

```bash
./scripts/docker-readiness-check.sh
./scripts/podman-readiness-check.sh
./scripts/containerd-rootless-readiness-check.sh
./scripts/containerd-rootful-readiness-check.sh
```

Expected: the selected lane passes. Treat Docker, Podman, and containerd
warnings as backend-adapter hygiene unless `agency admin doctor` or runtime
manifest/status/validate reports a generic runtime failure.

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
| **Model Validation** | Go tests + 10 | Validates Go validation layer end-to-end |

## Final Cleanup

After all exercises:

```
agency_infra_down()
```

Verify no orphaned Agency runtimes through the dev cleanup helper first:

```bash
./scripts/cleanup-live-test-runtimes.sh
```

For backend-specific inspection, use the active backend's CLI. Examples:

```bash
docker ps -a --filter name=agency
podman ps -a --filter name=agency
```

Expected: no unexpected Agency containers or runtime processes remain.

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
