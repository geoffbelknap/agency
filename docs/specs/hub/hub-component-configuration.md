---
description: "Hub components (connectors, services, packs) require operator-specific configuration — API keys, channel IDs, polling..."
status: "Implemented (core), evolving with credential store"
---

# Hub Component Configuration

**Status:** Implemented (core), evolving with credential store
**Date:** 2026-03-24
**Last updated:** 2026-04-01

## Problem

Hub components (connectors, services, packs) require operator-specific configuration — API keys, channel IDs, polling intervals, routing rules. This spec defines the configuration model for hub components.

**Implementation notes:** Core hub instance management is implemented: install, activate, configure, deactivate, remove. Handler code lives in `internal/api/handlers_hub.go` and `internal/api/handlers_connector_setup.go`, with config logic in `internal/hub/config.go`. As of 2026-03-31, the credential backend has been migrated from `.capability-keys.env` to the encrypted credential store (`internal/credstore/`). Credentials are now managed via `agency creds set/list/show/delete/rotate/test` (CLI in `internal/cli/commands.go`, REST handlers in `internal/api/handlers_credentials.go`, MCP tools in `internal/api/mcp_credentials.go`). The egress proxy resolves credentials via `SocketKeyResolver` over a gateway Unix socket instead of reading flat env files. The `config.yaml` per-instance format now uses a `config:` section for non-secret values.

## Design

### Config Schema

Every hub component can declare a `config:` section listing configurable fields:

```yaml
# connector: slack-ops
config:
  - name: slack_bot_token
    description: "Slack Bot User OAuth Token (xoxb-...)"
    required: true
    secret: true
    source: credential
  - name: channel_id
    description: "Slack channel ID to monitor"
    required: true
    source: literal
  - name: poll_interval
    description: "Seconds between polls"
    required: false
    default: "60"
    source: literal
```

Values are substituted into `${...}` placeholders throughout the component YAML:

```yaml
source:
  type: poll
  url: "https://slack.com/api/conversations.history?channel=${channel_id}"
  headers:
    Authorization: "Bearer ${slack_bot_token}"
  interval: "${poll_interval}"
```

**Source types:**

| Source | Resolution | Where value ends up |
|--------|-----------|-------------------|
| `credential` | Capability key store → egress credential swap | Scoped token ref in config.yaml, real key in egress |
| `literal` | Direct pass-through | config.yaml as plain text |
| `env` | Read from host environment variable | config.yaml as plain text |
Secret values (`secret: true`) MUST use `source: credential`. They never appear in config.yaml as plaintext. The intake container only sees scoped token references — egress performs the real credential swap. This matches the existing agent service credential pattern (ASK Tenet 3: mediation is complete, Tenet 4: least privilege).

### Instance Management

Components are installed as named instances with short UUID identifiers:

```
~/.agency/connectors/
├── registry.yaml
├── a7f3e2b1/
│   ├── connector.yaml     # Hub template (placeholders intact)
│   └── config.yaml        # Resolved values (0600 permissions)
├── 3c9d1f04/
│   ├── connector.yaml
│   └── config.yaml
```

**registry.yaml:**

```yaml
instances:
  slack-incidents:
    id: a7f3e2b1
    source: default/slack-ops
    kind: connector
    created: 2026-03-24T01:00:00Z
    state: active
  slack-feedback:
    id: 3c9d1f04
    source: default/slack-ops
    kind: connector
    created: 2026-03-24T01:05:00Z
    state: inactive
```

- Instance IDs are 8-character hex, generated at install time
- Human names are operator-chosen (`--as` flag) or defaulted from the component name
- API and CLI accept either name or ID
- Multiple instances from the same hub component are supported (different configs)
- Hub updates (`agency hub update`) refresh `connector.yaml` templates without touching `config.yaml`

**config.yaml format:**

```yaml
instance: slack-incidents
id: a7f3e2b1
source_component: default/slack-ops
configured_at: 2026-03-24T01:00:00Z
values:
  channel_id: "C0123INCIDENTS"
  poll_interval: "60"
  slack_bot_token: "@scoped:slack-incidents-auth"
```

File permissions: `0600` (owner read/write only). Secret fields store scoped token references (`@scoped:` prefix), not real credentials. Real credentials live in `.capability-keys.env` and flow through egress credential swap.

### API Contract

All operations are API-first. Consumers (CLI, agency-web, MCP tools) hit the same endpoints.

**Install:**

```
POST /api/v1/hub/install
Body: {"component": "slack-ops", "kind": "connector", "as": "slack-incidents"}

201: {"name": "slack-incidents", "id": "a7f3e2b1", "status": "installed"}
409: {"error": "instance name already exists"}
```

**Activate (with config):**

```
POST /api/v1/hub/{name-or-id}/activate
Body: {"config": {"channel_id": "C0123", "slack_bot_token": "xoxb-..."}}

200: {"status": "active", "id": "a7f3e2b1", "name": "slack-incidents"}
200: {"status": "config_required", "missing": [
       {"name": "channel_id", "description": "Slack channel ID", "required": true, "secret": false},
       {"name": "slack_bot_token", "description": "Slack Bot Token", "required": true, "secret": true}
     ]}
```

When `config_required` is returned, the consumer collects the missing values and resubmits. The API never prompts — it declares what's needed.

For `secret: true` fields, the API accepts the real value, routes it to the credential key store, generates a scoped token, and stores the reference in config.yaml.

**Configure (update config):**

```
PUT /api/v1/hub/{name-or-id}/config
Body: {"config": {"channel_id": "C0456"}}

200: {"status": "updated"}
```

If the instance is active, the gateway SIGHUPs the intake service to reload.

**Deactivate:**

```
POST /api/v1/hub/{name-or-id}/deactivate

200: {"status": "inactive"}
```

Config is preserved. Reactivation does not require re-entering config.

**Show (resolve name/ID):**

```
GET /api/v1/hub/{name-or-id}

200: {"name": "slack-incidents", "id": "a7f3e2b1", "kind": "connector",
      "source": "default/slack-ops", "state": "active",
      "config": {"channel_id": "C0123", "poll_interval": "60",
                 "slack_bot_token": "**"}}
```

Secrets are masked in the response.

**Remove:**

```
DELETE /api/v1/hub/{name-or-id}

200: {"status": "removed"}
```

Deactivates if active, removes config and template, removes from registry.

### Pack Deployment

Packs declare connector instances with their config inline, referencing credentials:

```yaml
# pack: ops-team
credentials:
  - name: SLACK_BOT_TOKEN
    description: "Slack bot token for ops workspace"
    required: true
    secret: true
  - name: JIRA_TOKEN
    description: "Jira API token"
    required: true
    secret: true

connectors:
  - source: slack-ops
    name: slack-incidents
    config:
      slack_bot_token: "${SLACK_BOT_TOKEN}"
      channel_id: "C0123INCIDENTS"
      poll_interval: "30"
  - source: jira-ops
    name: jira-platform
    config:
      jira_token: "${JIRA_TOKEN}"
      project_key: "PLAT"
```

**Deploy endpoint:**

```
POST /api/v1/deploy
Body: {"pack": "ops-team", "credentials": {"SLACK_BOT_TOKEN": "xoxb-...", "JIRA_TOKEN": "..."}}

200: {"status": "deployed", "agents": [...], "connectors": [...], "team": "..."}
200: {"status": "credentials_required", "missing": [
       {"name": "SLACK_BOT_TOKEN", "description": "...", "secret": true}
     ]}
```

**Deploy sequence:**

1. **Validate** — parse pack, check all required credentials are provided
2. **Install dependencies** — for each connector, `hub install <source> --as <name>` (skip if already installed)
3. **Configure** — write config.yaml for each connector, resolve `${...}` from provided credentials. Secret values route through credential swap.
4. **Create agents/teams/channels** — existing deploy logic
5. **Activate connectors** — activate each configured connector instance
6. **Start agents** — existing start logic

**Teardown** (`agency teardown ops-team`) reverses: stop agents → deactivate connectors → remove connector instances → delete agents/teams/channels.

**Pack constraint validation:** When deploy creates agents, each agent's constraints are validated against the operator's policy hierarchy. Packs are third-party content — they cannot exceed operator policy bounds (ASK Tenet 1).

### CLI Commands

All hub management under `agency hub`:

```bash
agency hub search <query>                           # find in catalog
agency hub install <component> --as <name>          # install instance
agency hub list                                     # all installed instances
agency hub show <name-or-id>                        # detail view (secrets masked)
agency hub activate <name-or-id>                    # configure + turn on (prompts for missing config)
agency hub configure <name-or-id>                   # update config
agency hub deactivate <name-or-id>                  # turn off (config preserved)
agency hub remove <name-or-id>                      # uninstall
agency hub update                                   # sync catalog from sources
```

Pack deployment:

```bash
agency deploy ops-team.yaml --set SLACK_BOT_TOKEN=xoxb-... --set JIRA_TOKEN=...
agency deploy ops-team.yaml --credentials creds.env
agency deploy ops-team.yaml                          # prompts for missing
agency teardown ops-team
```

CLI credential resolution order: flags (`--set`) → env vars → credential file (`--credentials`) → interactive prompt.

### Audit

Every configuration operation writes to the system audit log (ASK Tenet 2):

- `hub_install` — component, instance name, ID, source
- `hub_activate` — instance, config keys set, secrets updated (masked)
- `hub_configure` — instance, config keys changed (before/after, secrets masked)
- `hub_deactivate` — instance
- `hub_remove` — instance
- `deploy` — pack name, agents created, connectors activated, credentials provided (masked)
- `teardown` — pack name, components removed

### Credential Flow (ASK Compliant)

```
Operator provides real API key via `agency creds set`
    ↓
Gateway stores in encrypted credential store (~/.agency/credentials/store.enc)
    ↓
Generates scoped key reference for the credential entry
    ↓
config.yaml stores: slack_bot_token: "@scoped:slack-incidents-auth"
    ↓
Intake reads config.yaml, sends requests with scoped token through egress
    ↓
Egress resolves credentials via SocketKeyResolver (gateway Unix socket)
    ↓
Request reaches external API with real credentials
```

No container ever sees real API keys. The credential store (`internal/credstore/`) holds encrypted entries. The egress proxy resolves key references at request time via the gateway's restricted Unix socket — no flat env files involved. The enforcer performs scope checking only and never holds credentials (ASK Tenet 3: mediation complete, Tenet 4: least privilege).

### Source: env Security

The `source: env` config type reads from the host environment. Security constraints:

- Values are read **once at configure time** and stored in config.yaml. Not re-read at runtime.
- The variable name is logged in the audit event: `{"env_var": "SLACK_BOT_TOKEN", "resolved": true}`.
- Only the operator can trigger configuration (API requires gateway auth token). Agents cannot invoke `hub configure`.
- If the env variable is not set, activation fails with `config_required` — it does not silently use an empty value.

### Placeholder Validation

At install time, all `${...}` references in the component template are validated against the `config:` declarations. Missing declarations are flagged as warnings. At activation time, any unresolved required placeholder is a hard error — activation fails with `config_required` listing the missing fields.

### Hub Update and Stale Config Detection

When `agency hub update` refreshes a component template, the gateway compares the new template's `config:` schema against the existing `config.yaml`:

- **New required fields** → instance state set to `needs_reconfiguration`. `hub list` shows the flag. Activation is blocked until reconfigured.
- **Removed fields** → warning logged, orphaned values stay in config.yaml (harmless).
- **Renamed placeholders** → treated as new required + removed old. Instance flagged.

### Teardown Credential Cleanup

When `agency teardown` or `agency hub remove` removes a connector instance:

1. Deactivate the connector (if active)
2. Remove credential entries from the credential store (`internal/credstore/`)
3. Remove per-agent service grant entries that referenced this instance
4. Delete the instance directory (config.yaml + template)
5. Remove from registry.yaml
6. Audit log: `hub_remove` event with credential cleanup confirmation

No orphaned credentials remain (ASK Tenet 4).

### Connector Hosting

Connectors are hosted by the **intake** container on the mediation network. The existing activation path routes through comms (`CommsRequest`) as a transport mechanism, but intake is the service that runs connectors. Configuration and activation:

- Gateway writes config.yaml to the host filesystem
- Intake container has the connector directory mounted read-only
- Activation: gateway calls intake's connector management endpoint via the mediation network
- SIGHUP on config change causes intake to reload active connectors

### API Route Migration

| Old Route | New Route | Change |
|-----------|-----------|--------|
| `POST /hub/install` (body: `name`, `kind`) | `POST /hub/install` (body: `component`, `kind`, `as`) | Field rename + instance naming |
| `GET /hub/{name}/info` | `GET /hub/{name-or-id}` | Now accepts ID, returns full detail |
| `POST /connectors/{name}/activate` | `POST /hub/{name-or-id}/activate` | Moved under hub, accepts config body |
| `POST /connectors/{name}/deactivate` | `POST /hub/{name-or-id}/deactivate` | Moved under hub |
| `GET /connectors/{name}/status` | `GET /hub/{name-or-id}` | Merged with show |
| `GET /connectors` | `GET /hub/instances` (with `?kind=connector` filter) | Filtered list |
| `POST /deploy` (body: `pack_path`) | `POST /deploy` (body: `pack`, `credentials`) | Extended with credential support |

Old routes are removed — no deprecation period. MCP tool names change accordingly:
- `agency_connector_list` → `agency_hub_list`
- `agency_connector_activate` → `agency_hub_activate`
- `agency_connector_deactivate` → `agency_hub_deactivate`
- `agency_connector_status` → `agency_hub_show`

### Storage Migration

The current flat-file model (`~/.agency/connectors/{name}.yaml`, `hub-installed.json`) is replaced by the instance-directory model. Migration on first access:

1. Gateway detects flat connector files in `~/.agency/connectors/`
2. For each flat file: creates instance directory with auto-generated ID, moves YAML to `connector.yaml`, creates empty `config.yaml`
3. Builds `registry.yaml` from the migrated instances + `hub-installed.json` provenance
4. Renames `hub-installed.json` to `hub-installed.json.migrated` (preserved, not deleted)
5. Logs migration to audit

After migration, the flat-file model is no longer used. New installs go directly to the instance-directory model.

### Existing Commands Removed

The `agency connector` top-level command is removed. All operations move to `agency hub`:

| Old | New |
|-----|-----|
| `agency connector list` | `agency hub list` |
| `agency connector activate <name>` | `agency hub activate <name>` |
| `agency connector deactivate <name>` | `agency hub deactivate <name>` |
| `agency connector status <name>` | `agency hub show <name>` |

The `agency intake` top-level command remains for work item inspection (`agency intake items`, `agency intake stats`).

## ASK Compliance

- **Tenet 1:** Config is operator-owned. Agents cannot install, configure, or activate components. Pack constraints validated against policy. `source: env` reads at configure time only, logged.
- **Tenet 2:** All config operations audited with before/after values (secrets masked). Env variable resolution logged.
- **Tenet 3:** Credentials flow through egress credential swap. Intake never holds real API keys. No unmediated credential path.
- **Tenet 4:** Scoped tokens per instance. Teardown cleans up credential store entries. No orphaned keys.
- **Tenet 5:** Provenance tracked — registry records source hub, install time, config changes.
- **Tenet 7:** Config history in audit log — every change is recorded and retrievable.
- **Tenet 24:** Connector routing rules validated against agent authorization scope during activation.
