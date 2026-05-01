# Agency

[![CI](https://github.com/geoffbelknap/agency/actions/workflows/ci.yml/badge.svg)](https://github.com/geoffbelknap/agency/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go 1.26](https://img.shields.io/badge/Go-1.26-00ADD8.svg)](https://go.dev)

Governed AI agents with real isolation, mediated execution, durable memory, and
complete auditability.

Agency is the reference implementation of [ASK](https://askframework.org), the
open framework for agent security.

## What Agency Is

Agency is a platform for running one or a few AI agents that can do real work
without being trusted with your machine, your network, or your credentials.

The core product is intentionally simple:

- create an agent
- start it in an isolated workspace
- talk to it through a direct-message workflow
- let it use governed tools through a mediation layer
- keep a durable audit trail and visible budget/usage records
- let it build graph-backed context that improves future work

This is not "just another chat UI." The point is the governed runtime around the
agent.

## How It Works

![Agency architecture](docs/images/architecture.svg)

Operators use the CLI, web UI, REST API, or MCP server. The Go gateway is the
control plane and source of truth.

Each agent runs inside its own isolated microVM workspace. An external
per-agent enforcer boundary mediates every LLM call, tool call, and service
request. The agent never sees real API keys and never gets direct outbound
internet access.

Inside the workspace, Agency implements the
[ASK cognitive model](https://askframework.org/#cognitive):

- `Constraints` are operator-owned and read-only
- `Identity` is agent-owned and durable
- `Session` is ephemeral per run

The system is event-driven. Agents are woken by direct messages, platform
events, and other routed events rather than broad polling loops being the main
product model.

Agency also keeps a durable knowledge graph. The important part of that story is
not "graph features for their own sake," but that agents can retrieve useful
context from previous work and get smarter and faster over time.

## Why It Exists

Most AI agent demos skip the hard parts:

- isolation
- mediation
- credential boundaries
- auditability
- fail-closed behavior
- operator recovery

Those are exactly the parts that matter once an agent is doing real work.
Agency is built around them first.

## Quick Start

**You'll need:**

- a supported microVM runtime path for your platform
- host tools for the supported runtime path
- an API key from at least one supported model provider

On Linux and WSL2, Agency defaults to Firecracker and requires KVM plus vsock
access for the operator account. On macOS Apple silicon, Agency defaults to
`apple-vf-microvm` backed by Apple's Virtualization framework.

The supported microVM path also needs host tools and a local Python
environment for host-managed egress:

- `.venv/bin/mitmdump` plus Agency's egress addon Python dependencies for host-managed egress mediation
- `e2fsprogs` / `mke2fs` for microVM root filesystem creation
- Node/npm dependencies for the host-managed web UI

Host-managed infra code is packaged as Agency services under `services/`.
The `images/` tree remains in the source repo for OCI/rootfs build inputs; it
is not shipped in packaged installs and is not the host service runtime
contract.

`agency setup` and `agency quickstart` own runtime artifact readiness. They
persist the selected microVM backend and fail closed before daemon startup if
the helper, kernel, enforcer, guest transport, or rootfs tooling needed by that
backend is missing. Source checkouts can prepare those artifacts with:

```bash
make apple-vf-helpers
./scripts/readiness/apple-vf-artifacts.sh

make firecracker-helpers
./scripts/readiness/firecracker-artifacts.sh
./scripts/readiness/firecracker-kernel-artifacts.sh
```

Packaged installs run the host dependency helper automatically. To install or
verify them yourself from a source checkout:

```bash
./scripts/install/host-dependencies.sh --check
./scripts/install/host-dependencies.sh
```

The script uses Homebrew on macOS/Linuxbrew when available, or common Linux
package managers such as `apt-get`, `dnf`, `yum`, `pacman`, or `zypper`. It
installs system packages such as Python and e2fsprogs, then installs the
pinned Python dependencies used by the bundled host-managed infrastructure
services into the installed Agency asset tree. Packaged installs ship prebuilt
web UI assets; Node/npm are only needed when building the web UI from source.

Dockerfiles remain part of Agency as OCI filesystem recipes. Docker, Podman,
containerd, and Apple Container execution backends are legacy paths and are no
longer selectable through setup or quickstart. MicroVM rootfs inputs must be
explicit, versioned OCI artifact references; Agency does not use mutable
`latest` runtime images as a release gate. Release publishing emits the
microVM runtime artifacts as `agency-runtime-body:vX.Y.Z` and
`agency-runtime-enforcer:vX.Y.Z` in GHCR.

### Install

**Recommended: macOS / Linux Homebrew**

```bash
brew tap geoffbelknap/tap
brew install agency
```

**One-shot installer**

```bash
curl -fsSL https://geoffbelknap.github.io/agency/install.sh | bash
```

The one-shot installer downloads the release archive directly, installs the
`agency` binary to `~/.local/bin`, installs runtime assets to
`~/.local/share/agency`, and uses the host package manager only for runtime
dependencies. Before installing, it reminds you that Homebrew is easier to
audit and uninstall, then asks you to confirm that you want to continue with the
script path.

**Last resort: source install**

```bash
git clone https://github.com/geoffbelknap/agency.git
cd agency
make install
```

Source installs are useful for contributors or environments that cannot use
Homebrew. `make install` installs required host dependencies using
`scripts/install/host-dependencies.sh`; set `SKIP_HOST_DEPS=1` if your package
manager or release packaging already provides those dependencies.

### First Run

```bash
agency quickstart
```

Quickstart guides you through:

1. choosing a provider
2. storing an API key
3. starting infrastructure
4. creating a first agent
5. opening the web UI and direct-message chat

After setup, the main path is:

```bash
agency send henry "summarize the open issues in this repo"
agency log henry
agency admin doctor
```

See [docs/quickstart.md](docs/quickstart.md) for the guided flow.

## Programmatic Surface

Agency is not only a CLI and web app. It also exposes a build surface for
operators and other tools:

- REST API
- canonical OpenAPI spec at
  [internal/api/openapi.yaml](internal/api/openapi.yaml)
- supported core API view at `/api/v1/openapi-core.yaml`
- MCP server via `agency mcp-server`

That means Agency itself, its web UI, AI assistants, and third-party clients
can all build on the same contract.

Operator/runtime surfaces now include:

- `POST /api/v1/agents/{name}/dm` to establish or reuse the direct-message channel for an agent
- `GET /api/v1/agents/{name}/runtime/manifest` for persisted runtime contract state
- `GET /api/v1/agents/{name}/runtime/status` for projected runtime health and backend state
- `POST /api/v1/agents/{name}/runtime/validate` for fail-closed runtime validation
- `GET /api/v1/admin/doctor` for deployment safety, with runtime checks separated from backend-specific hygiene

### AI Assistant Integration

Add Agency as an MCP server:

```bash
claude mcp add agency -- agency mcp-server
codex mcp add agency -- agency mcp-server
gh copilot mcp add agency -- agency mcp-server
```

## Core Commands

```text
agency quickstart
agency setup
agency infra up
agency status

agency create <name> [--preset X]
agency start <name>
agency stop <name>
agency show <name>
agency send <agent> <message>
agency log <name>

agency admin doctor
agency admin usage --agent <name>
agency graph query <text>
agency graph stats
```

Run `agency <command> --help` for details.

## What Is In Scope Today

Agency's credible near-term core is:

- governed single-agent or small-agent workflows
- direct messages and simple channels
- event-driven execution
- provider routing and governed tool use
- graph-backed retrieval/context
- audit, budget, and usage visibility
- web UI for setup, agents, DM, and activity
- API and MCP surfaces for builders

There are broader platform areas in the repo, but they are not the center of
the product story right now.

## Building

```bash
make all
make install
make images
make test

go test ./...
pytest images/tests/
```

For local source installs, `make install` runs:

```bash
./scripts/install/host-dependencies.sh
```

Use `./scripts/install/host-dependencies.sh --check` to verify host dependency
presence without installing packages. Use `--dry-run` to see which package
manager, Python dependencies, and source-web build dependencies would be used.

For runtime/lifecycle changes, the highest-signal validation path is:

```bash
bash ./scripts/readiness/runtime-contract-smoke.sh --agent <agent>
./scripts/e2e/e2e-live-disposable.sh --skip-build
```

Firecracker is the Linux production runtime target. `apple-vf-microvm` is the
supported macOS local-development target. Legacy container backend smokes are
archived for historical adapter validation only; they are not release gates.

That path is not part of required CI or branch protection.

See [tests/checklists/runtime-smoke.md](tests/checklists/runtime-smoke.md) and
[tests/checklists/validation-checklist.md](tests/checklists/validation-checklist.md)
for the current operator validation flow.

## Repository Structure

```text
agency/
├── cmd/gateway/        # Go binary entry point
├── internal/           # Go packages: API, CLI, orchestrate, policy, runtime
├── images/             # OCI image filesystem recipes
├── presets/            # Agent preset YAML files
├── web/                # Web UI (REST client)
├── docs/               # User-facing docs (Mintlify) + operator runbooks
├── specs/              # Architecture specs (contributor reference)
├── tests/              # Engineering test artifacts (release/validation checklists)
├── scripts/            # Categorized: readiness/, e2e/, hub-oci/, dev/, ci/, release/
├── go.mod
└── Makefile
```

## Related Projects

| Project | What it is |
|---------|-----------|
| [ASK](https://askframework.org) | The security framework Agency implements. |
| [web/](web/) | The Agency web UI. |
| [agency-hub](https://github.com/geoffbelknap/agency-hub) | Registry and ecosystem work outside the core runtime story. |

## Platform Support

Linux (`x86_64`, `arm64`) and macOS (Apple Silicon, Intel) natively. Windows
via WSL2.

Linux and WSL2 default to Firecracker. macOS defaults to `apple-vf-microvm`.
Docker, Podman, containerd, and Apple Container execution backends are legacy
adapter history, not supported runtime selections.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). All changes must satisfy the
[ASK tenets](https://askframework.org).

## License

Apache 2.0. See [LICENSE](LICENSE).
