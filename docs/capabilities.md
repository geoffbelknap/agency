---
title: "Capabilities"
description: "Capabilities are things agents can use — tools, skills, and external services. The operator controls what's available through a centralized registry."
---


Capabilities are things agents can use — tools, skills, and external services. The operator controls what's available through a centralized registry.

## Three Categories

### MCP Servers

MCP (Model Context Protocol) servers provide tools to agents. They run **operator-side** (outside the agent runtime) and are wired into the agent at boot time.

Examples: browser automation, code search, database access.

```bash
# Register an MCP server
agency cap add mcp code-search --command "npx @anthropic/code-search"

# List registered capabilities
agency cap list

# Show what a specific agent can access
agency cap show my-agent
```

MCP servers declared in an agent's config are started during the seven-phase start sequence. The platform enforces tool policies — allowlists, denylists, and binary hash pinning.

### Skills

Skills are instruction packages that follow the [agentskills.io](https://agentskills.io) open standard. Each skill has a `SKILL.md` file containing procedural knowledge — workflows, best practices, and domain-specific guidance.

Skills are:

- Delivered **read-only** into the agent runtime
- Described in the system prompt (so the agent knows what skills are available)
- Loaded on demand (full content pulled in when the agent invokes a skill)

Agency ships with built-in skills for platform operation. You can add custom skills by placing them in the skills directory.

### Services

Services are external APIs with managed credentials. Agency handles the credential lifecycle — you grant access, the platform manages the keys.

```bash
# Grant a service to an agent
agency grant my-agent github --key-env GITHUB_TOKEN
agency grant my-agent brave-search --key-env BRAVE_API_KEY

# Grant using a key file
agency grant my-agent slack --key-file ~/.secrets/slack-token

# Grant by typing the key
agency grant my-agent jira --key-stdin
```

**How credentials work:** The real API key is stored in the egress proxy's configuration. The agent receives a scoped token that the egress proxy swaps for the real credential at the network boundary. The agent never sees the actual key.

Revoke access at any time:

```bash
agency revoke my-agent github
```

Grants and revocations take effect immediately on running agents via hot-reload (SIGHUP) — no restart required.

## Managing Capabilities

### List Available Capabilities

```bash
agency cap list
```

Shows all registered MCP servers, skills, and services, along with their status (enabled/disabled).

### Enable and Disable

```bash
agency cap enable brave-search --key $BRAVE_API_KEY
agency cap disable brave-search
```

Disabling a capability removes it from all agents. Enabling it makes it available for agents to use.

### Show Agent Access

```bash
agency cap show my-agent
```

Displays everything a specific agent can access — MCP servers, skills, and services.

### Register New Capabilities

**MCP Server:**

```bash
agency cap add mcp my-tool --command "path/to/mcp-server"
```

**API Service:**

```bash
agency cap add api my-service --url "https://api.example.com"
```

### Remove Capabilities

```bash
agency cap delete my-tool
```

## MCP Tool Policies

The platform enforces policies on MCP tools:

- **Allowlist/Denylist** — Control which tools an agent can call
- **Binary pinning** — Hash verification ensures MCP server binaries haven't been tampered with
- **Output poisoning detection** — Scans MCP tool outputs for prompt injection attempts
- **Cross-server attack detection** — Detects when tool outputs from one MCP server try to manipulate another

These policies are defined in `constraints.yaml` and enforced by the platform — not the agent.

## Built-in Agent Tools

Every agent has these tools available in its runtime, regardless of capabilities:

**File operations:**
- `read_file`, `write_file`, `list_directory`, `search_files`, `execute_command`
- All enforce the `/workspace` boundary — agents can't access files outside their workspace

**Memory:**
- `save_memory`, `search_memory`, `list_memories`, `delete_memory`

**Communication:**
- `send_message`, `read_channel`, `list_channels`, `search_messages`, `create_channel`

**Knowledge graph:**
- `query_knowledge`, `who_knows_about`, `what_changed_since`, `get_context`

**Authority tools (function agents only):**
- `halt_agent` — Stop a team member who violates constraints
- Exception recommendation tools
