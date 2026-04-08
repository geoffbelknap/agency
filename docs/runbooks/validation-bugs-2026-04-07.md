# Runbook Validation — Bugs Found

Discovered 2026-04-07 while validating `agency/docs/runbooks/` against the actual CLI (`internal/cli/commands.go`).

## CLI Bugs / Missing Features

### 1. No `agency list` command for agents

**Severity:** Medium
**Location:** `internal/cli/commands.go`

There is no top-level `agency list` command to list agents. `agency status` (no args) shows a platform overview that includes agents, but it's not scriptable — you can't pipe it to `grep running | awk '{print $1}'` to get agent names for batch operations.

**Impact:** The backup runbook needed a way to stop all running agents before backup. Without a scriptable list command, operators must manually inspect `agency status` output.

**Suggested fix:** Add a top-level `agency list` command (or `agency agent list`) that outputs a machine-parseable list of agents with their states. Support `--format` or at minimum output one agent per line with name and state.

---

### 2. No `knowledge export` / `knowledge restore` commands

**Severity:** Medium
**Location:** `internal/cli/commands.go` — knowledge subcommands

The knowledge graph has `query`, `stats`, `ingest`, `review`, `ontology`, etc. but no `export` or `restore` commands for backup/restore workflows.

**Impact:** Operators must fall back to filesystem-level `cp -r ~/.agency/knowledge/` for backups, which requires stopping the daemon first to ensure consistency. A proper export/restore would allow hot backups and cross-machine migration.

**Suggested fix:** Add `agency knowledge export` (dump graph to JSON/YAML) and `agency knowledge import` (load from dump). The export should include nodes, edges, ontology, and classification config.

---

### 3. `stop` command has no `--immediate` or `--force` flag

**Severity:** Low
**Location:** `internal/cli/commands.go`, `stopCmd()` (~line 277)

The `stop` command takes only an agent name — no flags at all. The three-tier halt system (`--tier supervised/immediate/emergency`) only exists on `halt`. If an agent is in a restart loop, operators must run `halt --tier immediate` followed by `stop` as two separate commands.

**Impact:** Minor UX friction. The two-step process is correct (halt then stop), but a `--force` flag on `stop` that internally does `halt immediate + stop` would simplify the restart-loop recovery procedure.

**Suggested fix:** Either add `--force` to `stop` that chains halt+stop, or document the two-step pattern more prominently. The runbooks have been updated to use the two-step approach.

---

### 4. `status` and `show` have overlapping/confused responsibilities

**Severity:** Low
**Location:** `internal/cli/commands.go`, `statusCmd()` (~line 367), `showCmd()` (~line 770)

`agency status [agent]` does double duty — platform overview with no args, agent detail with an arg. `agency show <agent>` is hidden but actually does more than `status <agent>` (includes budget info). These should be cleanly separated:

- `agency status` — platform/infra overview only (no agent arg)
- `agency show <agent>` — agent detail including budget (unhide it)

Currently `show` is hidden and marked as an alias for `status <agent>`, but it's not — it has extra budget display logic that `status <agent>` lacks.

**Suggested fix:** Remove the agent-detail branch from `statusCmd()`, unhide `showCmd()`, and make `status <agent>` a deprecated alias that points to `show`.

---

### 5. No `--quiet` / `--no-spinner` flag for machine-readable output

**Severity:** Medium
**Location:** `internal/cli/commands.go` — all commands that use spinners

CLI commands use animated spinners (e.g., `⠋ Starting enforcement containers`) that produce massive output when run by AI agents or in non-interactive contexts. A single `agency start` generated hundreds of spinner characters in context.

**Suggested fix:** Add a global `--quiet` or `--machine` flag that suppresses spinners and progress animations. Could also auto-detect non-TTY and suppress automatically. This would make CLI output AI-friendly and scriptable.

---

### 6. `creds set` requires flags not documented in runbooks

**Severity:** Medium
**Location:** `internal/cli/commands.go`, `credsSetCmd()`

Runbooks (initial-setup.md, validation-checklist.md) show `agency creds set <name> --value <value>` but the actual command requires `--name`, `--kind`, `--protocol`, and `--scope` flags. Provider credentials need: `--name ANTHROPIC_API_KEY --value sk-ant-... --kind provider --protocol api-key --scope platform`.

**Impact:** Operators following the runbooks will get errors on credential setup.

---

### 7. `knowledge ontology show` fails — missing base-ontology.yaml

**Severity:** Medium
**Location:** Knowledge graph ontology loader

`agency knowledge ontology show` errors with: `open ~/.agency/knowledge/base-ontology.yaml: no such file or directory`. The knowledge directory only has `data/` and `ontology.d/` — no base ontology file. Either `setup` should create it, or the ontology loader should handle its absence gracefully.

---

### 8. `hub search` requires an argument (runbook says it doesn't)

**Severity:** Low
**Location:** `internal/cli/commands.go`, hub search command

Validation checklist says `agency hub search` (bare) should return results or empty. Actual: `Error: accepts 1 arg(s), received 0`. Needs a query argument.

---

### 9. Gateway PID file not written

**Severity:** Low
**Location:** Daemon startup code

`~/.agency/gateway.pid` doesn't exist even though the daemon is running (PID 51214 on :8200). Multiple runbooks reference this file for health checks and shutdown. Either the PID file location changed or it's not being written.

---

## Runbook Corrections Made

All runbooks were updated to match the actual CLI:

| File | Issue | Fix |
|------|-------|-----|
| agent-recovery.md | `--type`, `stop --immediate` | `--tier`, two-step halt+stop |
| backup-restore.md | `agency list`, `knowledge export/restore` | `agency status`, filesystem-level backup |
| credential-rotation.md | `creds list --groups` | `creds list --group <name>` |
| security-incident-response.md | `--type`, `stop --immediate` | `--tier`, two-step halt+stop |
| upgrade.md | `agency list` | `agency status` |
| validation-checklist.md | `--type`, `knowledge ontology` | `--tier`, `knowledge ontology show` |
| infrastructure-recovery.md | (no issues found) | No changes needed |
| initial-setup.md | (no issues found) | No changes needed |
