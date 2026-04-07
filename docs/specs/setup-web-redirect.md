# Setup: Web-Assisted Default

**Status:** Approved  
**Date:** 2026-04-03  
**Scope:** `cmd/gateway/main.go` (setup command only)

## Overview

Change `agency setup` to perform mechanical initialization (config, daemon, infrastructure) then redirect the operator to the agency-web setup wizard at `/setup` for provider configuration and onboarding. The current fully-interactive CLI flow is preserved behind `--cli`.

## Behavior

### `agency setup` (default)

1. Check Docker (unchanged)
2. `config.RunInit()` with no provider or API key — creates `~/.agency/` directory structure and config files only
3. Start daemon
4. Start infrastructure (all containers including agency-web)
5. Print `https://<host>:8280/setup` to stdout (no automatic browser open — avoids side effects in headless/CI environments)

No interactive prompts. No provider menu. No API key input.

### `agency setup --cli`

Exactly the current behavior: interactive provider selection, masked API key input, credential storage, legacy credential migration, "next steps" output.

## Browser Launch

Best-effort, per-platform:

| Platform | Command |
|----------|---------|
| macOS | `open <url>` |
| Linux | `xdg-open <url>` |
| WSL | `wslview <url>`, fallback `cmd.exe /c start <url>` |

If the command fails or the binary isn't found, silently fall back to printing the URL. No error message — the operator may be SSH'd in or headless.

WSL is detected the same way the existing `isWSL()` function works (checking `/proc/version` for "microsoft" or "WSL").

## Code Changes

All changes in `cmd/gateway/main.go`.

### `setupCmd()`

- Add `--cli` bool flag (`"Run full interactive setup in the terminal"`)
- When `--cli` is false (default): skip the interactive provider/key block, call `runSetup` with empty provider/apiKey, then handle web-redirect output in `RunE` after `runSetup` returns
- When `--cli` is true: execute the existing interactive prompt logic before calling `runSetup`

### `runSetup()`

- Add `webSetup bool` parameter
- When `webSetup` is true: replace the "You're ready to go" / next-steps block with browser launch + URL print
- When `webSetup` is false: existing output unchanged

### New helper: `openBrowser(url string)`

Attempts to open the URL in the system default browser. Returns an error (caller ignores it). Uses `runtime.GOOS` and `isWSL()` to select the command.

## Output

### Default (`agency setup`)

```
Agency platform initialized at /home/user/.agency

Starting daemon...
Daemon started successfully.

Starting infrastructure...
  ✓ egress
  ✓ comms
  ✓ knowledge
  ✓ intake
  ✓ agency-web
Infrastructure running.

Finish setup at: https://localhost:8280/setup
```

### CLI mode (`agency setup --cli`)

Unchanged from current output.
