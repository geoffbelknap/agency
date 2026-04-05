# Agency

[![CI](https://github.com/geoffbelknap/agency/actions/workflows/ci.yml/badge.svg)](https://github.com/geoffbelknap/agency/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go 1.26](https://img.shields.io/badge/Go-1.26-00ADD8.svg)](https://go.dev)

An operating system for AI agents.

Most organizations want to use AI agents. What stops them is everything around the agent: credential isolation, audit trails, cost control, network mediation, governance. The problems that don't show up in the demo but kill you in production.

Agency handles all of it. You focus on the work — one personal assistant or a hundred agents running at scale. Agency handles the security, the infrastructure, and the operational overhead so the agents can do real work with real data in real organizations.

The reference implementation of [ASK](https://askframework.org), the open framework for agent security.

## How it works

![Agency architecture](docs/images/architecture.svg)

Operators manage the platform through the CLI, web UI, or MCP tools. The gateway orchestrates everything below it. Shared infrastructure handles messaging, a persistent knowledge graph, work intake from external systems, and an egress proxy that swaps scoped tokens for real API keys at the network boundary.

Each agent gets an enforcer — a Go proxy that mediates every request, scans for prompt injection, tracks budget, and writes HMAC-signed audit logs. Below that, the workspace is a hardened container where the agent actually runs. It can only reach the enforcer. It never sees real credentials, the internet, or any other agent's workspace.

Inside each workspace, Agency implements the [ASK cognitive model](https://askframework.org/#cognitive): the agent's Mind is split into Constraints (operator-owned, read-only), Identity (agent-owned, audited), and Session (ephemeral). The critical security boundary — between what the operator controls and what the agent can modify — is structural, not a matter of trust.

Agents communicate through channels, remember what they learn across sessions, and wake only when triggered. The same security model works whether you're running one agent or a hundred.

## Quick start

**You'll need:**
- [Docker](https://docs.docker.com/get-docker/) installed and running
- An API key from [Anthropic](https://console.anthropic.com) (recommended), [OpenAI](https://platform.openai.com), or [Google](https://aistudio.google.com)

> **Windows:** Install [Docker Desktop](https://docs.docker.com/desktop/install/windows-install/) with WSL integration enabled, then run everything below inside your WSL2 terminal.

### Install

**macOS / Linux (Homebrew):**

```bash
brew install geoffbelknap/tap/agency
```

**Linux (no Homebrew):**

```bash
curl -fsSL https://geoffbelknap.github.io/agency/install.sh | bash
```

The install script downloads the binary, checks Docker, and runs setup. Alternatively, [build from source](#building).

### Set up and run

```bash
agency setup
```

This walks you through choosing a provider, setting your API key, starts the daemon, and brings up all infrastructure (including the web UI on `localhost:8280`).

Then create an agent and give it work:

```bash
agency create my-agent
agency start my-agent
agency send my-agent "summarize the open issues in this repo"
agency status
agency admin doctor    # verify all security guarantees are holding
```

### Operating via AI assistant

Agency ships with an MCP server (85+ tools) and a Claude Code plugin with guided skills (`/status`, `/deploy`, `/create-agent`, `/create-mission`, and more).

#### Claude Code (recommended)

Install the plugin for auto-discovery of MCP tools and skills:

```bash
claude plugin add /path/to/agency
```

Or add the MCP server directly:

```bash
claude mcp add agency -- agency mcp-server
```

The plugin is in `.claude-plugin/` at the repo root. If you're working inside the agency repo, Claude Code picks it up automatically.

#### GitHub Copilot CLI

```bash
gh copilot mcp add agency -- agency mcp-server
```

#### VS Code

Add to your workspace `.vscode/mcp.json`:

```json
{
  "servers": {
    "agency": {
      "command": "agency",
      "args": ["mcp-server"]
    }
  }
}
```

#### OpenAI Codex CLI

```bash
codex mcp add agency -- agency mcp-server
```

## Key commands

```
agency setup                         Initialize the platform
agency create <name> [--preset X]    Create an agent
agency start <name>                  Start (7-phase verified sequence)
agency stop <name>                   Halt an agent
agency send <target> <message>       Send work via DM or channel
agency status                        Platform overview
agency log <name>                    Audit log

agency mission create <file>         Standing instructions from YAML
agency mission assign <name> <agent> Assign mission to agent
agency deploy <pack.yaml>            Deploy a full team from a pack
agency teardown <pack>               Reverse a deployment

agency hub install <name>            Install a connector or pack
agency hub <name> activate           Activate it
agency creds set <name> <value>      Store a credential
agency admin doctor                  Verify security guarantees
```

Run `agency <command> --help` for details on any command.

## Connectors

Connectors bring external work into Agency. Published to [agency-hub](https://github.com/geoffbelknap/agency-hub), installed by name.

| Connector | What it does |
|-----------|-------------|
| `limacharlie` | LimaCharlie detections and sensor inventory |
| `nextdns-blocked` | NextDNS blocked DNS queries |
| `unifi` | UniFi Site Manager infrastructure devices |
| `slack-ops` | Slack channel polling, bidirectional |
| `slack-events` | Slack Events API webhooks, real-time |
| `jira-ops` | Jira Cloud issues, routed by type and priority |

Plus companion connectors for additional data sources (`limacharlie-sensors`, `nextdns-analytics`, `unifi-hosts`, `unifi-sites`) and Slack bridges (`comms-to-slack`, `red-team-escalations-to-slack`).

```bash
agency hub install limacharlie --kind connector
agency hub limacharlie activate
agency hub limacharlie config
```

## Model providers

Agency works with any LLM provider that speaks the OpenAI API format. The setup wizard configures Anthropic, OpenAI, and Google out of the box. Any other OpenAI-compatible provider (Groq, Together, Mistral, Ollama, etc.) can be added in `routing.yaml`.

Five model tiers (frontier, standard, fast, mini, nano). Each agent preset declares a tier. The platform resolves to the best available model based on configured credentials.

## Related projects

| Project | What it is |
|---------|-----------|
| [ASK](https://askframework.org) | The security framework Agency implements. 27 tenets. Vendor-neutral. |
| [web/](web/) | Web UI. Vite/React. Connects to the gateway REST API. |
| [agency-hub](https://github.com/geoffbelknap/agency-hub) | Component registry. Packs, presets, connectors, missions, services. |

## Building

```bash
make all          # Go binary + all container images
make install      # Go binary only (auto-restarts daemon)
make images       # Container images only
make deploy       # install + infra up
make test         # Run tests

# Go tests from repo root
go test ./...

# Python tests (container image code)
pytest images/tests/
```

## Repository structure

```
agency/
├── cmd/gateway/        # Go binary entry point
├── internal/           # Go packages (api, cli, models, orchestrate, hub, etc.)
├── images/             # Container image sources
│   ├── body/           # Agent runtime (Python)
│   ├── comms/          # Messaging server (Python)
│   ├── knowledge/      # Knowledge graph (Python)
│   ├── intake/         # Work intake (Python)
│   ├── egress/         # Credential swap proxy (Python)
│   ├── enforcer/       # Enforcement proxy (Go)
│   ├── workspace/      # Agent workspace container
│   ├── web-fetch/      # Web page fetcher (Go)
│   ├── models/         # Shared Pydantic models
│   └── tests/          # Python tests for image code
├── presets/            # Agent preset YAML files
├── docs/              # Specs and documentation
├── go.mod             # Go module (github.com/geoffbelknap/agency)
└── Makefile           # Unified build (Go binary + container images)
```

## Platform support

Linux (x86_64, arm64) and macOS (Apple Silicon, Intel) natively. Windows via WSL2.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). All changes must satisfy the [ASK tenets](https://askframework.org).

## License

Apache 2.0. See [LICENSE](LICENSE).
