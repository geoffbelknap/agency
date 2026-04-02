## What This Document Covers

How Agency discovers, manages, and distributes capabilities (MCP servers, skills, API services) to agents. A centralized registry with operator-controlled enablement replaces per-agent manual configuration.

> **Status:** Implemented. The enable/disable workflow, auto-import of bundled services, custom MCP server and API service registration, OpenAPI auto-discovery, and Phase 3 wiring are operational. Marketplace, trust verification, and policy hierarchy assignment are designed but not yet implemented.

---

## Part 1: Capability Types

Every external capability that an agent can use is a **capability entry** in the registry. Three capability types:

| Type | What It Is | Example |
|---|---|---|
| `mcp-server` | MCP stdio server package | filesystem-server, browser-tools, code-search |
| `skill` | Instruction package (SKILL.md) | code-review, incident-response |
| `service` | External API with credential management | brave-search, github, jira |

### Registry Structure

```
~/.agency/
├── registry/
│   ├── mcp-servers/
│   │   ├── code-search.yaml
│   │   └── browser-tools.yaml
│   ├── skills/
│   │   └── code-review/
│   │       └── SKILL.md
│   └── services/
│       ├── brave-search.yaml
│       └── github.yaml
├── services/                # Service definitions (tool endpoints)
│   ├── brave-search.yaml
│   └── github.yaml
└── capabilities.yaml        # Operator state: enabled/disabled, auth, agent scope
```

### Capability Definition Schema

Every capability has a common envelope:

```yaml
# registry/mcp-servers/browser-tools.yaml
kind: mcp-server
name: browser-tools
version: "1.2.0"
description: Web browsing, screenshot, and page interaction via Playwright.
source: local              # or marketplace URL (future)

requires:
  network: ["playwright.dev"]

spec:
  command: "npx"
  args: ["@anthropic/browser-tools-mcp"]
  env:
    BROWSER_HEADLESS: "true"

permissions:
  filesystem: read-only
  network: true
  execution: false
```

---

## Part 2: Operator Workflow

### Enable bundled capabilities

Agency ships with bundled service definitions (brave-search, github). Enable them with a single command:

```bash
agency cap enable brave-search --key $BRAVE_API_KEY
```

This auto-imports the bundled service definition into the registry, stores the key securely, and makes it available to all agents. On next `agency start`, Phase 3 wires the service into the agent's runtime.

### Restrict to specific agents

```bash
agency cap enable github --key $GITHUB_TOKEN --agents atlas,bolt
```

Only `atlas` and `bolt` will receive the GitHub capability.

### Agent-specific keys

```bash
agency cap enable github --agent atlas --key $ATLAS_GITHUB_TOKEN
```

### Disable a capability

```bash
agency cap disable brave-search
```

### Discovery

```bash
# See all capabilities: bundled, installed, and status
agency cap list

# See what a specific agent has access to
agency cap show dev-assistant
```

### Add custom MCP servers

```bash
# Register and enable
agency cap add mcp code-search --command npx --args @modelcontextprotocol/server-code-search
agency cap enable code-search
```

### Add custom API services

```bash
# Register (auto-discovers OpenAPI spec when available)
agency cap add api my-api --url https://api.example.com --key-env MY_API_KEY

# Review generated service definition, then enable
agency cap enable my-api --key $MY_API_KEY
```

When you run `add api`, Agency probes common endpoints (`/openapi.json`, `/swagger.json`, `/api-docs`, etc.) for an OpenAPI/Swagger spec. If found, tool definitions are auto-generated from the API's endpoints. Otherwise, a scaffold with an example tool is created for you to edit.

---

## Part 3: Service Definition Format

API services are defined as YAML files in `~/.agency/services/`. Each tool maps to an API endpoint that becomes available to agents as an MCP tool via the agency-services bridge (`agency-services-mcp.js`).

```yaml
service: my-api
display_name: My API
api_base: https://api.example.com
credential:
  env_var: MY_API_KEY
  header: Authorization
  format: "Bearer {key}"           # Optional (omit for raw key in header)
  scoped_prefix: agency-scoped-my-api

tools:
  - name: search
    description: Search for items.
    method: GET
    path: /v1/search
    parameters:
      - name: query
        description: Search query
        required: true
      - name: limit
        description: Max results
        required: false
        default: "10"
    query_params:
      query: q
      limit: limit
    response_path: results          # Extract nested response field

  - name: create_item
    description: Create a new item.
    method: POST
    path: /v1/items
    parameters:
      - name: name
        description: Item name
        required: true
    body_template:
      name: "{name}"
```

See `agency/services/brave-search.yaml` and `agency/services/github.yaml` for real examples.

### Credential flow

Agents never see real API keys:

1. Real key stored in `.service-keys.env` (0600 permissions, outside agent container)
2. Agent receives a scoped token (`agency-scoped-my-api-xxxx`)
3. Enforcer sidecar intercepts requests with `X-Agency-Service` header
4. Enforcer swaps scoped token for real key at the network layer
5. Audit log records the request (agent cannot suppress)

---

## Part 4: Phase 3 Integration

The start sequence (Phase 3) resolves capabilities and prepares the runtime's configuration files:

```
Phase 3: Constraints
  1. Resolve effective policy chain
  2. Compute effective capabilities from policy + capabilities.yaml
  3. For each capability by type:
     mcp-server → merge into mcp-servers.json
     service    → generate services-manifest.json, wire agency-services-mcp.js
     skill      → merge into skills-manifest.json
  4. Mount skill directories read-only
  5. Configure enforcer with service credentials and egress allowlist
```

The runtime sees the same interface it always has — `mcp-servers.json`, `skills-manifest.json`, service tool calls through MCP. The registry is a platform concern, not a runtime concern.

### Egress Policy Integration

Each capability declares its network requirements. The start sequence aggregates these into the egress proxy allowlist:

```
Agent dev-assistant effective capabilities:
  - brave-search: requires api.search.brave.com
  - github: requires api.github.com

Egress allowlist for dev-assistant:
  - api.search.brave.com
  - api.github.com
  + agent.yaml requires.network entries
```

---

## Part 5: Relationship to ASK Tenets

### Tenet 4: Access Matches Purpose, Nothing More

- Agents only get capabilities the operator enables for them.
- Capabilities declare what permissions they need (network, filesystem, execution).
- Credential scopes are declared by the operator, not left to defaults.

### Tenet 5: Superego Is Operator-Owned and Read-Only

- Agents cannot install, remove, or modify capabilities.
- Capability state is outside the agent's isolation boundary.
- Service definitions and registry entries are read-only to agents.

### Tenet 2: Every Action Leaves a Trace

- Capability resolution at start time is logged in the audit trail.
- Credential grants and revocations are logged.
- All service API calls through the enforcer are logged.

---

## Future: Marketplace and Policy Assignment

The following are designed but not yet implemented:

- **External marketplace** for discovering and installing community capabilities
- **Trust levels** (official, verified, community, local) with integrity verification
- **Policy hierarchy assignment** — capabilities assigned via department/team/agent policy inheritance instead of flat capabilities.yaml
- **Credential rotation** and external secret store integration
- **MCP server hot-reload** without full runtime restart

*See also: Runtime-Specification.md for how the runtime consumes capabilities. Policy-Framework.md for the inheritance model. Agency-Platform-Specification.md for the file family and CLI reference.*
