# Quickstart Wizard

**Status:** Draft

## Overview

New `agency quickstart` command. Gets a user from zero to "agent just did real work in my terminal" in under 10 minutes. Opinionated first-run path — calls `agency setup` internally if needed, then guides agent creation and runs a live demo task.

Separate from `agency setup`, which remains idempotent infrastructure bootstrapping. Quickstart is the "hold my hand" experience. Setup is for re-runs and automation.

## Five Phases

Each phase auto-detects completion and skips silently if already done. A user who already has a working provider and running infra jumps straight to agent creation.

```
agency quickstart

  ✓ environment        Docker running
  ✓ provider           Anthropic — already configured
  ✓ infrastructure     all services healthy
  ● agent              Setting up your first agent...
    demo
```

### Phase 1: Environment

Check Docker is installed and running. If missing, show platform-specific guidance (reuse existing `dockerHelp()` from `setup.go`):
- macOS: "Install Docker Desktop from docker.com"
- Linux: "Start the Docker daemon: sudo systemctl start docker"
- WSL2: "Install Docker Desktop with WSL2 integration enabled"

If running, skip with "✓ environment — Docker running".

**Detection:** `docker info` succeeds.

### Phase 2: Provider

**Detection:** Check if `~/.agency/config.yaml` exists and has `llm_provider` set. If the gateway is already running (Phase 3 may not have happened yet), query the credential store (`GET /api/v1/credentials`) to verify. If the gateway isn't running yet, check for the credential store file on disk (`~/.agency/credentials/store.enc`). Validation call goes directly to the provider (not through the gateway/enforcer) since infra may not be up: Anthropic `POST /v1/messages` with `max_tokens: 1`, OpenAI `GET /v1/models`, Google `GET /v1/models`. If valid, skip with "✓ provider ({name} — already configured)".

**If no provider or validation fails:**

```
Which LLM provider do you want to use?

  1. Anthropic (recommended)
  2. OpenAI
  3. Google

Choice [1]:
API key: ••••••••••••••••
Validating... ✓
```

Key entry is masked (reuse existing `readPassword()` from `setup.go`). After entry, immediately validate with a cheap API call.

**On validation failure:** "Key didn't work: {error}. Try again? [Y/n]". Loop until valid or user quits. After 3 failures: "Having trouble? See https://geoffbelknap.github.io/agency/getting-api-keys"

**Credential handling:** After validation, the raw key is stored in the encrypted credential store and zeroed from process memory. The key must not be logged, written to config.yaml, or held in any intermediate state. The validation HTTP call must use TLS and must not disable certificate verification.

**Internally:** If `agency setup` hasn't been run (`~/.agency/config.yaml` doesn't exist), call `config.RunInit()` + credential store setup. If setup already ran, just store the new credential.

### Phase 3: Infrastructure

**Detection:** Check gateway is running (`CheckGateway()` TCP dial to port 8200) and infra containers are healthy (`GET /api/v1/infra/status`).

**If all up:** Skip with "✓ infrastructure — all services healthy".

**If not running:** Start gateway daemon + `agency infra up`, streaming progress with the existing spinner:

```
  ● infrastructure     Starting services...
    ✓ gateway
    ✓ egress
    ✓ comms
    ✓ knowledge
    ✓ intake
    ✓ web-ui
  ✓ infrastructure     all services healthy
```

First run includes Docker image pulls (~30-60 seconds). Cached runs ~10 seconds.

### Phase 4: Agent

**Detection:** Check if any agents exist and are running (`GET /api/v1/agents`, filter for `status=running`). If yes, skip with "✓ agent ({name} — already running)".

**If no running agents:**

```
What would you like your first agent to do?

  1. General assistant — research, write, analyze, code
  2. Security operations — triage alerts, investigate threats, audit posture
  3. Code review — review PRs, find bugs, suggest improvements
  4. Research & analysis — deep dives, report writing, data synthesis

Choice [1]:
```

Mapping:

| Choice | Preset | Agent Name |
|---|---|---|
| 1. General assistant | henry | henry |
| 2. Security operations | engineer | security-analyst |
| 3. Code review | code-reviewer | reviewer |
| 4. Research & analysis | researcher | researcher |

Runs `agency create {name} --preset {preset}` then `agency start {name}`, streaming the 7-phase startup:

```
  ● agent              Creating henry...
    ✓ verify
    ✓ enforcement
    ✓ constraints
    ✓ workspace
    ✓ identity
    ✓ body
    ✓ session
  ✓ agent              henry (generalist, frontier)
```

### Phase 5: Demo

Send a contextual first task and stream the agent's response in the terminal.

**Demo tasks by choice:**

| Choice | First Task |
|---|---|
| General assistant | "Look at my current directory and suggest something useful you could help me with." |
| Security operations | "Give me a brief status report on what you're ready to monitor." |
| Code review | "Look at my current directory and summarize what this project is." |
| Research & analysis | "What are you capable of? Give me three things I should try first." |

**Display:**

```
Your agent is ready. Let's try it out:

> henry is thinking...

  I've analyzed the current directory and here's what I found:
  [agent response streams in real-time]

────────────────────────────────────────
Agent is running. What's next:
  • Send tasks:  agency send henry "your task here"
  • Web UI:      https://localhost:8280
  • Status:      agency status
  • More agents: agency hub search
```

Choice-specific additions to "What's next":
- Security operations: `• Full team:  agency hub install security-ops`
- Code review: `• Review PRs:  agency send reviewer "review my latest commit"`

**Implementation:** Send task via `POST /api/v1/agents/{name}/dm` (existing DM channel endpoint). Stream response by subscribing to the agent's WebSocket signal channel and rendering `agent_signal_task_complete` or message events. Cap at 60 seconds.

**If demo times out:** "Agent started but the first task is taking a while. Check `agency status` or open https://localhost:8280."

## Error Handling

| Failure | Response |
|---|---|
| Docker not installed | Platform-specific install guidance. Exit with "Run `agency quickstart` again after installing Docker." |
| API key invalid | Retry loop, option to try different provider. After 3 failures: link to docs. |
| Image pull fails | "Check your internet connection. Run `agency quickstart` again to retry." Cached images make retries fast. |
| Agent start fails | Show failing phase + error. Run `agency admin doctor` automatically, display results. Suggest `agency start {name} --verbose`. |
| Demo task timeout | "Agent started but taking a while." Point to `agency status` and web UI. |
| Ctrl-C during any phase | Clean exit. Completed phases stay completed. Next run picks up where it left off. |

## What Quickstart Does NOT Do

- **No capability configuration.** Capabilities are day-2. The web UI setup wizard (Step 4) handles this.
- **No mission creation.** The agent works with preset defaults. Missions come after the user understands agents.
- **No notification setup.** Optional, not first-run critical.
- **No multi-provider setup.** One provider is enough. Add more via `agency creds set` or web UI.
- **No pack installs.** Single agent only. Packs are suggested in "What's next" output.

## CLI Flags

```
agency quickstart [flags]

Flags:
  --provider string    Skip provider prompt (anthropic, openai, google)
  --key string         Skip key prompt (requires --provider)
  --preset string      Skip agent choice prompt (use this preset)
  --name string        Override default agent name
  --no-demo            Skip the demo task
  --verbose            Show detailed output for all phases
```

Flags enable scripted/CI usage while the default interactive path is prompt-driven. The `--key` flag is for automation where stdin is unavailable — users should prefer the interactive masked prompt for manual use. The flag value is not logged by the CLI. For safer automation, consider `--key-file` (read from file) or `--key-env` (read from environment variable) as future additions.

## Implementation Notes

**Command location:** New file `cmd/gateway/quickstart.go` (cobra command registered in `main.go`).

**Reuse from setup.go:**
- `dockerHelp()` — platform-specific Docker guidance
- `readPassword()` — masked key input
- `config.RunInit()` — first-run initialization
- Provider credential storage flow

**Reuse from commands.go:**
- Spinner pattern (animated frames + checkmark)
- Lipgloss color palette (green/red/yellow/cyan)
- API client (`apiclient.Client`) for all gateway calls

**New code:**
- Phase detection logic (check config, credentials, infra, agents)
- Provider validation calls (cheap direct HTTP call per provider, not through enforcer)
- Demo task streaming (new: CLI WebSocket client to subscribe to agent signals + render message events in terminal)
- Agent choice → preset mapping

**No new dependencies.** Cobra + lipgloss + existing API client are sufficient. No survey/promptui needed — the existing `bufio.Scanner` + `readPassword()` patterns handle the three prompts (provider, key, agent choice).

## Sequencing

1. Scaffold `quickstart.go` with cobra command and phase structure
2. Implement phase detection (skip logic)
3. Implement phases 1-3 (environment, provider with validation, infrastructure)
4. Implement phase 4 (agent choice, create, start)
5. Implement phase 5 (demo task send + response streaming)
6. Add CLI flags for scripted usage
7. Error handling and edge cases
8. Test full flow on clean machine (no `~/.agency/`)

Steps 1-4 are the core. Step 5 is the magic. Steps 6-8 are polish.
