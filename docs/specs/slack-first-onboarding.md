# Slack-First Onboarding

## Context

Agency has two personas: developers (CLI, Docker, YAML power users) and information workers (want agent value without becoming platform experts). Today's setup requires Python, Docker, CLI commands, manual Slack app creation, and env var configuration. The information worker persona has no viable path to get started.

**The goal:** a non-technical user on Mac or Windows runs one command, answers a few questions, and within minutes has agents running in their Slack workspace — visible, interactive, and producing consumable work. They manage their agency partnered with an AI assistant operator while engaging with agents like co-workers.

---

## Before You Start: API Key Prerequisite

Agency agents need at least one LLM provider API key to function. Both the install script and setup wizard must surface this clearly **before** the user commits time to installation.

**Pre-install notice** (shown by `install.sh` / `install.ps1` before doing anything):
```
Before we begin, you'll need an API key from at least one AI provider.
This takes ~2 minutes and you can start for free with Google Gemini.

  Provider         Free Tier?    Recommended For
  ─────────────────────────────────────────────
  Google (Gemini)  Yes, generous  Getting started at zero cost
  Anthropic        No, pay-as-go  Best reasoning (recommended)
  OpenAI           Limited        Broadest model range

  Full guide: docs/getting-api-keys.md

Have your key ready? Press Enter to continue...
```

The setup wizard (`agency setup`) auto-detects keys from environment variables (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`) and offers to use them. If none are found, it links to the guide and prompts for manual entry with provider selection.

**Key design choice:** Lead with Google Gemini as the free option for cost-sensitive users. Recommend Anthropic for best agent quality. The getting-api-keys guide already covers all three providers — see [getting-api-keys.md](/getting-api-keys).

---

## What Already Exists (leverage, don't rebuild)

| Component | Status | Notes |
|-----------|--------|-------|
| `install.sh` | Working | Handles Python, venv, Docker, `agency setup`, `agency infra up` on Linux/macOS |
| `agency setup` | Working | Creates `~/.agency/`, scaffolds configs, builds container images |
| `agency deploy` / `teardown` | Working | Deploys packs (multi-agent teams) declaratively |
| Slack connectors | Working | `slack-ops` (poll), `slack-events` (webhook), `comms-to-slack` (channel-watch relay) |
| WebSocket push (agent comms) | Working | Real-time push from comms to agents via WS. No agent-side polling. |
| Channel-watch source type | Working | Intake watches comms channels, forwards to external systems (e.g., Slack relay) |
| `slack.yaml` service | Working | 11 Slack API tools for agents |
| Knowledge service | Working | Full REST API (query, who-knows, context, export, stats, curation) |
| MCP server | Working | 64 tools, full CLI parity for AI assistant operation |
| `getting-api-keys.md` | Written | Covers all 3 providers including Google free tier |
| 15 built-in presets | Working | generalist, coordinator, analyst, ops, engineer, etc. |
| 3 existing packs | Working | slack-ops, jira-ops, red-team |

### Architecture Note: Push, Not Poll

Agent-to-agent communication is already WebSocket push-based (commit 69bf4c3). The comms service fans out messages to connected agents in real-time via `fan_out_message()` with server-side classification (direct/interest_match/ambient). Agents reconnect with catch-up for missed messages. There is no heartbeat polling.

The `slack-ops` connector still polls Slack's *external* API (this is inbound from Slack, not internal comms). The Socket Mode bridge (Phase 1E) replaces this with real-time WebSocket from Slack.

---

## Plan — Three Phases

### Phase 1: Minimum Viable Simple Onboarding

Ship these together. Gets a non-technical user from zero to agents-in-Slack.

#### 1A. Slack App Manifest

**Why first:** Eliminates the hardest manual step. Creating a Slack app with correct OAuth scopes, event subscriptions, and permissions is error-prone and intimidating for non-technical users.

Create a Slack App manifest YAML that users paste into `api.slack.com/apps > Create from manifest`. Includes:
- All required bot scopes (channels:history, channels:read, chat:write, reactions:read/write, users:read, files:read/write, search:read, channels:join)
- Event subscriptions (message.channels, message.groups)
- Socket Mode enabled (avoids needing a public URL — critical for simple onboarding)
- App-level token scope (`connections:write` for Socket Mode)

**Files:**
- Create `agency-hub/connectors/slack-ops/slack-manifest.yaml` — static manifest, ready to paste
- Create `agency/agency/templates/slack-app-manifest.yaml.j2` — Jinja2 version for `agency setup` to generate with user's app name

#### 1B. Windows Installer Script

**Why:** Mac works via `install.sh` + Homebrew. Windows has nothing today.

**Strategy:** Windows runs Agency inside WSL2 (same as the existing development path). The PowerShell script bootstraps WSL2 and delegates to `install.sh`.

Create `install.ps1`:
- Detect/enable WSL2 (`wsl --install --no-distribution`)
- Detect/install Ubuntu distro
- Detect Docker Desktop + WSL2 backend integration
- **Pre-install API key notice** (see above)
- Delegate to `install.sh` inside WSL2
- Post-install: add `~/.agency/bin` to PATH (native Windows binary) or fall back to WSL

**Files:**
- Create `agency/install.ps1`

#### 1C. Improve macOS Docker Desktop Handling

The current `install.sh` fails hard on macOS when Docker Desktop isn't found (`print_fail` + exit). Instead:
- Detect Docker Desktop `.app` exists but daemon not running → `open -a Docker` and poll until `docker info` succeeds
- Not installed → `open https://docs.docker.com/desktop/install/mac-install/`, print friendly guidance, wait for user confirmation
- **Pre-install API key notice** (see above) — add to `install.sh` before `ensure_python()`

**Files:**
- Modify `agency/install.sh` — improve `brew` case in `ensure_docker()`, add preflight API key notice

#### 1D. Setup Wizard (`agency setup`)

**The core of Phase 1.** A new CLI command that wraps `init` + Slack config + pack deploy into a guided flow using Rich (already a dependency).

**Wizard steps:**

```
Step 1: Welcome
  "Let's get your Agency running. This takes about 5 minutes."

Step 2: API Key Configuration
  → Auto-detect from env vars (ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY)
  → "Found ANTHROPIC_API_KEY in environment. Use it? [Y/n]"
  → Or: provider picker → "Paste your API key:" → validate with test call
  → Link to getting-api-keys.md if no key found

Step 3: Slack Integration (optional but encouraged)
  → "Do you want your agents in Slack? (Y/n)"
  → If yes:
    1. "Open this URL to create your Slack app:" → opens browser to manifest install URL
    2. "Paste the Bot Token (starts with xoxb-):" → validates with auth.test
    3. "Paste the App-Level Token (starts with xapp-):" → validates
    4. "Paste the Signing Secret:" → stores
    5. "Which Slack channel should agents use? (default: #agency):" → validates exists

Step 4: Team Selection
  → "What should your agents do?"
    1. "General assistant team" (recommended — coordinator + generalist)
    2. "Slack ops team" (4 agents — monitor and respond in Slack)
    3. "Custom" (advanced — skip pack, create agents manually)

Step 5: Deploy
  → Runs: agency setup → hub install <pack> → deploy <pack>
  → Configures connectors (comms-to-slack relay, slack socket bridge)
  → Grants Slack service credentials to agents
  → Rich progress bar for each step

Step 6: Done
  → "Your agents are live! Go to #agency in Slack and say hello."
  → "To manage your agency, ask your AI assistant or run: agency help"
```

**Resumable:** Stores state in `~/.agency/.setup-state.json`. If the wizard fails partway (Docker not ready, network issue), re-running `agency setup` picks up from the last successful step.

**Files:**
- Create `agency/agency/commands/setup_cmd.py` — Click command + Rich wizard UI
- Create `agency/agency/core/setup.py` — orchestration logic (calls init, hub install, deploy, connector activate, grant)
- Modify `agency/agency/cli.py` — register `setup` command
- Modify `agency/install.sh` — call `agency setup` at end
- Modify `agency/install.ps1` — same

#### 1E. Slack Socket Mode Bridge

**Why in Phase 1 (not Phase 2):** Socket Mode eliminates the need for a public URL, which is the biggest barrier for non-technical users. The WebSocket pattern is already proven internally (comms push). And polling Slack's API every 10s is wasteful and still laggy.

A new container on the mediation network that:
1. Connects outbound to Slack via Socket Mode WebSocket (using `xapp-...` app-level token)
2. Receives real-time Slack events (messages, reactions, etc.)
3. Forwards them to the intake service's webhook endpoint locally (`POST /webhooks/slack-socket`)
4. Handles reconnection with exponential backoff (same pattern as `ws_listener.py`)

Replaces `slack-ops` polling for the onboarding path. The poll-based connector remains available for advanced configurations.

**Files:**
- Create `agency/agency/images/slack-bridge/bridge.py` — Socket Mode client → intake forwarder
- Create `agency/agency/images/slack-bridge/Dockerfile`
- Create `agency/agency/images/slack-bridge/requirements.txt` — `slack-sdk[socket-mode]`, `aiohttp`
- Create `agency-hub/connectors/slack-socket/connector.yaml` — Socket Mode connector definition (source type: webhook, paired with bridge container)
- Modify `agency/agency/core/infrastructure.py` — add slack-bridge to shared infra lifecycle (only started when Slack is configured)

#### 1F. Getting Started Pack

A simpler pack than `slack-ops`, optimized for the onboarding experience:
- **2 agents:** coordinator + generalist (not 4 — lower resource usage, faster startup)
- Pre-wired `comms-to-slack` relay for the user's chosen Slack channel
- Pre-wired `slack-socket` connector for inbound Slack messages
- Welcome brief that has the coordinator introduce itself in Slack on first deploy
- Generalist uses `generalist` preset — can research, write, analyze, take on varied tasks

**Files:**
- Create `agency-hub/packs/getting-started/pack.yaml`

---

### Phase 2: Polish and Deeper Integration

Independent pieces, build in any order.

#### 2A. Agent Identity in Slack

Currently `comms-to-slack` posts everything as the bot with `*[channel]* *sender*: content`. Improve:
- Use Slack's `username` override to show agent name as the poster
- Use `icon_emoji` per agent role (coordinator: :brain:, researcher: :mag:, writer: :pencil:, generalist: :sparkles:, ops: :wrench:)
- Format messages with Slack Blocks for rich formatting (headers, code blocks, dividers)
- Strip internal channel prefix — the Slack channel IS the context

**Files:**
- Modify `agency-hub/connectors/comms-to-slack/connector.yaml` — enhanced relay body template with username/icon_emoji
- May require relay body to support JSON blocks (check if relay body template supports structured Slack payloads)

#### 2B. Knowledge Web Dashboard

A single-page static web app served from the knowledge container at `http://localhost:18092/ui/`.
- Graph visualization (vis.js or d3-force, loaded from CDN)
- Search bar → `/query` endpoint
- Recent changes feed → `/changes` endpoint
- Agent expertise view → `/who-knows` endpoint
- Stats overview → `/stats` endpoint
- Clickable nodes → show details, related knowledge, source agent

No build toolchain — just HTML/JS/CSS bundled into the knowledge container image.

**Files:**
- Create `agency/agency/images/knowledge/static/index.html`
- Create `agency/agency/images/knowledge/static/app.js`
- Create `agency/agency/images/knowledge/static/style.css`
- Modify `agency/agency/images/knowledge/server.py` — add static file serving at `/ui/` path

#### 2C. Knowledge Digests to Slack

Scheduled connector that posts periodic knowledge summaries to Slack:
- Source type: `schedule` with cron (e.g., daily at 9am)
- Queries `/changes?since=24h` and `/stats` from knowledge service
- Formats a digest: new findings, top contributors, emerging topics
- Relay target: Slack `chat.postMessage` with rich blocks

**Files:**
- Create `agency-hub/connectors/knowledge-digest/connector.yaml`

#### 2D. Operator Bot in Slack (DM-based management)

The killer feature for the non-technical persona. User DMs the Agency bot in Slack to manage the platform — no CLI or Claude Code needed.

> **User (in Slack DM):** "Create a research team to investigate battery technology"
> **Bot:** "I'll set that up. Creating 3 agents: coordinator, researcher-1, researcher-2..."
> **Bot:** "Done! The team is working in #battery-research. I'll post updates there."

**Architecture:** A mediation-layer service (NOT an agent — needs operator authority):
1. Receives DMs via Slack Socket Mode bridge
2. Sends to Claude API with MCP tool definitions as function calling tools
3. Executes tool calls against Agency's MCP server functions (direct Python imports, not subprocess)
4. Returns results to Slack DM thread

**ASK compliance:** Runs in mediation layer with operator authority. Audit trail maintained because it uses the same MCP functions that log all actions. Governance remains read-only to agents — the operator bot is part of the enforcement/operator layer, not the agent layer.

**Files:**
- Create `agency/agency/images/operator-bot/bot.py` — Slack DM handler + Claude API + MCP tool execution
- Create `agency/agency/images/operator-bot/Dockerfile`
- Create `agency/agency/images/operator-bot/requirements.txt`
- Modify `agency/agency/core/infrastructure.py` — add operator-bot to shared infra lifecycle
- Create `agency-hub/connectors/operator-dm/connector.yaml` — DM routing

#### 2E. Slack Thread Continuity

When an agent responds to a Slack message, the reply should be in the same thread. When the human replies in that thread, the agent should see it as a continuation. This requires:
- Thread-aware routing in the Socket Mode bridge (include `thread_ts` in forwarded events)
- Brief templates that include thread context so agents reply in-thread
- Conversation state tracking so multi-turn Slack interactions feel natural

**Files:**
- Modify `agency-hub/connectors/slack-socket/connector.yaml` — thread-aware routing rules
- May need intake router enhancement for thread context propagation

---

### Phase 3: Full Vision (Future)

- **Desktop App** — Electron/Tauri wrapper bundling Docker detection, setup wizard, knowledge dashboard, system tray icon. Eliminates the terminal entirely for non-technical users.
- **Slack App Directory** — Submit for one-click install. Eliminates the manifest step.
- **Cloud-Hosted Option** — Agency runs on a server; user just installs the Slack app. Eliminates Docker Desktop entirely. This is the ultimate low-friction path.
- **Microsoft Teams Connector** — Same pattern as Slack for enterprise environments where Teams is the workspace.
- **Mobile Experience** — Slack/Teams mobile apps already work, but push notifications for escalations, knowledge digests, and agent status need to be tuned for mobile consumption.

---

## Dependency Graph

```
Phase 1 (ship together):
  1A Slack Manifest ──────────┐
  1B Windows Installer ──────┐│
  1C macOS Docker handling ──┤├──→ 1D Setup Wizard ──→ ship
  1E Slack Socket Bridge ────┤│
  1F Getting Started Pack ───┘│
                               │
Phase 2 (independent):         │
  2A Agent Identity ───────────┤
  2B Knowledge Dashboard ─────┤
  2C Knowledge Digests ────────┤
  2D Operator Bot ─────────────┤
  2E Thread Continuity ────────┘
```

## The User Journey (Phase 1 Complete)

```
1. User hears about Agency. Gets an API key (2 min, free with Google).

2. On Mac:  curl -fsSL <url>/install | bash
   On Win:  irm <url>/install.ps1 | iex

3. Installer handles Python, Docker, venv. Launches setup wizard.

4. Setup wizard:
   - "Paste API key" → validated
   - "Want Slack?" → Yes → opens manifest URL → paste tokens
   - "General assistant team" → selected
   - Deploys: 2 agents (coordinator + generalist), Socket Mode bridge, Slack relay

5. Coordinator posts in #agency: "Hi! I'm your agency coordinator.
   I work with a generalist agent. Ask me anything — research,
   writing, analysis. Just @ me or post here."

6. User types in #agency: "Research the latest developments in
   solid-state batteries and summarize the top 3 findings."

7. Coordinator delegates to generalist. Both agents' messages
   are visible in #agency as they work. Generalist posts findings.
   Coordinator synthesizes and posts summary.

8. Knowledge graph accumulates findings. User can query it later:
   "What do we know about battery technology?"
```

## Key Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Slack inbound | Socket Mode (Phase 1) | No public URL needed. Real-time. WebSocket pattern already proven internally. |
| Setup UI | Terminal wizard (Rich) | User is already in terminal from installer. No web UI complexity. |
| API key messaging | Lead with Google free tier | Lowest barrier. Recommend Anthropic for quality. |
| Operator bot location | Mediation layer, not agent | Needs operator authority. ASK compliant — outside agent boundary. |
| Getting started pack | 2 agents (coordinator + generalist) | Lower resource footprint, faster first experience. Upgrade later. |
| Windows strategy | WSL2 delegation | Agency is Linux-native. WSL2 provides full compat without porting. |

## Verification

After Phase 1 implementation, test the full flow on each platform:

1. **Clean Mac** — `curl` install → setup wizard → agents in Slack
2. **Clean Windows 11** — PowerShell install → WSL2 → setup wizard → agents in Slack
3. **Slack interaction** — human posts in channel → agent responds in thread
4. **Reconnection** — kill Socket Mode bridge → restart → messages caught up
5. **`agency admin doctor`** passes for all agents
6. **`agency status`** shows agents healthy
7. **Knowledge** — after agents have worked, `agency admin knowledge` returns accumulated findings

## Key Files Referenced

| Purpose | Path |
|---------|------|
| Existing install script | `agency/install.sh` |
| Init logic | `agency/agency/core/init.py` |
| CLI entry point | `agency/agency/cli.py` |
| Connector model | `agency_core/models/connector.py` |
| Intake service | `agency/agency/images/intake/server.py` |
| Comms WebSocket (push) | `agency/agency/images/comms/websocket.py` |
| Agent WS listener | `agency/agency/images/body/ws_listener.py` |
| Comms-to-Slack relay | `agency-hub/connectors/comms-to-slack/connector.yaml` |
| Slack-ops connector | `agency-hub/connectors/slack-ops/connector.yaml` |
| Slack service definition | `agency-hub/services/slack.yaml` |
| Getting API keys guide | `agency/docs/getting-api-keys.md` |
| Infrastructure lifecycle | `agency/agency/core/infrastructure.py` |
| Deploy logic | `agency/agency/core/deploy.py` |
| MCP server (operator tools) | `agency/agency/mcp_server.py` |
