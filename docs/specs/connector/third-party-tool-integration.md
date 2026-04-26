---
description: "Enable Agency agents to use vendor CLI tools (like limacharlie) inside workspace containers with transparent credenti..."
---

# Third-Party Tool Integration

## Goal

Enable Agency agents to use vendor CLI tools (like `limacharlie`) inside workspace containers with transparent credential management via the egress proxy. Provide a three-tier integration model from zero-effort drop-in to fully native vendor manifests.

## Problem

Vendors build Claude Code plugins with CLI-based tools (run via Bash). Agency agents run in isolated containers where those CLIs aren't available. Today, translating a vendor plugin into Agency service tools is fully manual — reading vendor docs, figuring out API endpoints, and hand-writing service YAML. This doesn't scale.

## Architecture

Three tiers that compose — an agent can have both CLI access (Tier 1) and structured service tools (Tier 2/3) simultaneously.

### Tier 1 — Drop-in CLI (build now)

Install vendor CLIs in the workspace container. Agent uses them via Bash. Egress proxy handles credential swap transparently.

**Manifest extension** — new `requires.workspace` block:

```yaml
requires:
  workspace:
    pip:
      - limacharlie>=5.0
    apt:
      - jq
      - nmap
    env:
      LIMACHARLIE_API_KEY: "${credential:limacharlie-api}"
      LC_ORG_ID: "${config:LC_ORG_ID}"
```

- `pip`: Python packages installed via `pip install` at workspace provisioning
- `apt`: System packages installed via `apt-get install` at workspace provisioning
- `env`: Environment variables injected into the workspace container
  - `${credential:name}` resolves to the scoped placeholder key for that service grant
  - `${config:name}` resolves from `~/.agency/.env`

**Credential flow** — unchanged from existing pattern:

```
Agent workspace              Enforcer              Egress proxy           Vendor API
(has scoped key)             (audit)               (has real key)

CLI runs, sends              logs Bash             intercepts by domain
scoped key in header  ─────> command  ────────────> swaps credential  ──> authenticated
                                                   logs HTTP request      API call
<──────────────────────────────────────────────────────────────────────── response
(never sees real key)
```

1. Component declares `requires.credentials` with grant name
2. Operator provides real key via `agency grant` → stored in `.service-keys.env`
3. At agent creation, scoped placeholder injected as env var in workspace
4. CLI reads env var, makes API calls
5. Egress proxy intercepts by domain, swaps placeholder for real credential
6. All HTTP traffic logged in audit

**Workspace provisioning** — during the "workspace" phase of seven-phase agent startup:

1. Read `requires.workspace` from the component manifest (preset or activated connector)
2. Merge workspace requirements from all active components (preset + granted services + assigned mission connectors)
3. If `pip` packages declared, run `pip install <packages>` in the workspace container after creation. Route through egress proxy.
4. If `apt` packages declared, run `apt-get install -y <packages>`. Route through egress proxy.
5. Inject `env` vars into the container environment (alongside existing HTTPS_PROXY, SSL_CERT_FILE, etc.)
6. Record resolved package versions in the agent's audit log.
7. Remove pip/apt-get from PATH after provisioning (agent cannot install packages at runtime).

**Egress domain requirements for provisioning**:

Package installation requires temporary egress access to package registries. These are allowed only during the provisioning phase (infrastructure-controlled), not at the agent's request:
- `pypi.org`, `files.pythonhosted.org` (pip)
- `deb.debian.org`, `security.debian.org` (apt)

### Tier 2 — Translated Tool Definitions (future)

Auto-generate Agency service tool definitions from vendor-provided API descriptions.

**Input sources** (priority order):
1. **OpenAPI/Swagger spec** — parse directly into service tools
2. **CLI `--ai-help` output** — AI-friendly help describing commands, flags, endpoints
3. **Claude Code plugin skill files** — extract tool definitions from skill content

**CLI command**:
```bash
agency hub translate <source> --output service.yaml
```

- Reads the source (URL to OpenAPI spec, CLI binary name, or plugin path)
- Generates a service YAML with tool definitions
- Operator reviews and edits before activating
- Never auto-deploys — always a review step

**Generated output**: Tool name, description, HTTP method, path, parameters, auth type, egress domains.

**Not generated** (operator fills in): Credential grant names, swap type details (JWT exchange params, token TTL), rate limits.

### Tier 3 — Native Vendor Manifest (future)

Vendor ships an `agency.yaml` alongside their Claude Code plugin. Agency consumes it directly.

**Manifest format**:

```yaml
agency:
  version: "1"
  name: limacharlie

  service:
    display_name: LimaCharlie API
    api_base: "https://api.limacharlie.io"
    auth:
      type: jwt-exchange
      token_url: "https://jwt.limacharlie.io"
      token_params:
        oid: "${LC_ORG_ID}"
        secret: "${credential}"
      token_response_field: jwt
      token_ttl_seconds: 3000
    egress_domains:
      - api.limacharlie.io
      - jwt.limacharlie.io
    tools:
      - name: list_detections
        description: "List detections for a time range"
        method: GET
        path: /v1/insight/${LC_ORG_ID}/detections
        parameters:
          - name: start
            type: string
            required: true
          - name: end
            type: string
            required: true

  workspace:
    pip:
      - limacharlie>=5.0
    env:
      LIMACHARLIE_API_KEY: "${credential:limacharlie-api}"
      LC_ORG_ID: "${config:LC_ORG_ID}"

  credentials:
    - name: limacharlie-api
      description: "LimaCharlie API key"
      setup_url: "https://app.limacharlie.io/api-keys"
```

**How Agency consumes it**:
- `agency hub install` detects `agency.yaml` in a plugin repo
- Generates service definition, credential swap config, and workspace requirements
- Operator goes through consent/activation flow (credentials, egress domains)
- Manifest stored in operator-controlled constraint space
- Changes via hub update go through operator acknowledgment (Tenet 6)

## How the Tiers Compose

An agent with a LimaCharlie integration gets both Tier 1 and service tools simultaneously:

- **Tier 1** (CLI via Bash): Ad-hoc operations, complex multi-step workflows
- **Service tools** (from Tier 2/3): Structured API access with better observability

The agent chooses based on the task. Both paths go through the egress proxy.

## What Changes in Agency (Tier 1)

| Component | Change |
|---|---|
| Hub manifest schema | New `requires.workspace` block (pip, apt, env) |
| Workspace provisioner | Install declared packages at agent creation |
| Agent startup (phase 4) | Read workspace requirements, run installs, inject env vars |
| Credential swap config gen | Map `${credential:name}` to scoped placeholders |
| Audit log | Record resolved package versions at provisioning |

## What Doesn't Change

- Egress proxy credential swap mechanism
- Enforcer audit logging (Bash commands + HTTP)
- Container networking (already routes through proxy)
- Service tool system (already works for structured API access)
- Seven-phase startup structure (workspace phase already exists)

## ASK Compliance

**Overall verdict: ASK-COMPLIANT with documented trade-offs.**

### Scoped Key Trade-off (Tenet 4)

Scoped placeholder credentials are visible to the agent as environment variables. This is an acceptable trade-off because:
- The scoped key is **per-agent** — revoked on halt/quarantine
- The scoped key is **domain-locked** — only works through the egress proxy for configured domains
- The scoped key is **audit-logged** — every use recorded by egress proxy
- The alternative (no credential) means the CLI won't function

This is the minimum viable credential pattern. The real credential never enters the container.

### Package Installation Security (Tenet 3)

Package installation during provisioning must route through the egress proxy:
- Workspace networking is fully proxied before pip/apt runs
- Package registries (pypi.org, deb.debian.org) allowed during provisioning only
- Agent cannot invoke pip/apt at runtime (removed from PATH after provisioning)

### Constraint History (Tenet 7)

Resolved package versions are recorded in the audit log at provisioning time. "What tools did this agent have at time T?" is always answerable.

### XPIA Considerations

- CLI tool output could contain adversarial content — standard XPIA surface, same as any tool result
- Write operations (create rules, isolate sensors) must be in the agent's hard_limits as requiring operator approval
- Agent could theoretically use API write operations to exfiltrate data — mitigated by audit logging and rate limits

## LimaCharlie Integration (First Implementation)

The first use of this system is the `limacharlie` CLI for the `limacharlie-ops` pack.

**Preset additions**:
```yaml
requires:
  workspace:
    pip:
      - limacharlie>=5.0
    env:
      LIMACHARLIE_API_KEY: "${credential:limacharlie-api}"
      LC_ORG_ID: "${config:LC_ORG_ID}"
  credentials:
    - name: limacharlie-api
      grant_name: limacharlie-api
  egress_domains:
    - api.limacharlie.io
    - jwt.limacharlie.io
```

**What the agent can do**:
- `limacharlie sensor list --oid $LC_ORG_ID --output yaml` — list sensors
- `limacharlie detection list --start ... --end ... --oid $LC_ORG_ID --output yaml` — query detections
- `limacharlie dr list --oid $LC_ORG_ID --output yaml` — list D&R rules
- `limacharlie fp set --name ... --oid $LC_ORG_ID --input fp.yaml` — create FP rules (with operator approval)
- Any `limacharlie` CLI command — the full CLI surface is available

**What the agent cannot do**:
- See the real API key (only the scoped placeholder)
- Install additional packages at runtime
- Access APIs outside the egress allowlist
- Bypass the egress proxy
