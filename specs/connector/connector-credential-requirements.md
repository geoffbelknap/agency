---
description: "---"
status: "Approved"
---

# Connector Credential Requirements and Guided Setup

**Date:** 2026-03-28
**Status:** Approved
**Last updated:** 2026-04-01

---

## Problem

Connectors declare their credential requirements in `requires.credentials`, but the runtime ignores this block. Operators must manually figure out what grants, env vars, and JWT swap configs to create. There's no validation at activation time, no guided setup, and no API for web UIs to render setup forms.

The LimaCharlie connector needs: a service grant (`limacharlie-api`), an env var (`LC_ORG_ID`), a JWT swap config, and two egress domain allowlist entries. None of this is discoverable from the activation flow — the operator has to read the README.

## Solution

Extend the `requires` block with richer metadata, wire it into the activation flow for pre-flight validation, and expose it via the REST API so any client (CLI, web, MCP) can guide the operator through setup.

---

## Schema: Extended requires Block

```yaml
requires:
  credentials:
    - name: LC_API_KEY
      description: LimaCharlie API key with detections and sensors read scope
      type: secret            # secret | env | config
      scope: service-grant    # service-grant | env-var | file
      grant_name: limacharlie-api   # service grant name in egress
      setup_url: "https://app.limacharlie.io/orgs/{LC_ORG_ID}/api-keys"

    - name: LC_ORG_ID
      description: LimaCharlie organization ID
      type: config            # not a secret — safe to display
      scope: env-var          # goes in ~/.agency/.env
      example: "<your-org-id>"

  auth:
    type: jwt-exchange        # none | bearer | jwt-exchange | oauth2
    token_url: "https://jwt.limacharlie.io"
    token_params:
      oid: "${LC_ORG_ID}"
      secret: "${credential}"
    token_response_field: "jwt"
    token_ttl_seconds: 3000

  egress_domains:
    - api.limacharlie.io
    - jwt.limacharlie.io

  services:
    - limacharlie-api
```

### Field Definitions

**credentials[].type:**
| Value | Meaning | Display |
|-------|---------|---------|
| `secret` | API key, token, password — must not be displayed after entry | Masked input, stored in service grant |
| `config` | Non-sensitive configuration value (org ID, region, project name) | Plain text input, stored in env var |

**credentials[].scope:**
| Value | Storage | How set |
|-------|---------|---------|
| `service-grant` | Encrypted credential store via `agency creds set` | `agency creds set --name {grant_name} --value {value} --kind service` |
| `env-var` | `~/.agency/config.yaml` config section (or legacy `~/.agency/.env`) | `agency config set {name} {value}` |
| `file` | Written to `~/.agency/secrets/` | For certificates, private keys |

**auth.type:**
| Value | Behavior |
|-------|----------|
| `none` | No auth header needed (public API or auth handled elsewhere) |
| `bearer` | Direct Bearer token injection from service grant |
| `jwt-exchange` | Exchange credential for JWT via token endpoint, cache and inject |
| `oauth2` | OAuth2 client_credentials flow |

When `auth.type` is `jwt-exchange`, the activation flow automatically stores the JWT exchange configuration as `protocol_config` in the credential store. The operator doesn't need to configure it manually.

**egress_domains:** Domains that must be in the egress allowlist for the connector to function. Activation flow checks and offers to add missing ones.

---

## Activation Flow

### Current (broken)

```
agency connector activate limacharlie
→ Activated.
→ (silently fails on first poll because no credentials configured)
```

### Proposed

```
agency connector activate limacharlie

Checking requirements for limacharlie v0.2.0...

Credentials:
  ✗ LC_API_KEY (secret) — not configured
    LimaCharlie API key with detections and sensors read scope
    Setup: https://app.limacharlie.io/orgs/{LC_ORG_ID}/api-keys

  ✗ LC_ORG_ID (config) — not configured
    LimaCharlie organization ID
    Example: <your-org-id>

Auth: jwt-exchange via jwt.limacharlie.io
  ✗ JWT swap config not found

Egress domains:
  ✗ api.limacharlie.io — not in allowlist
  ✗ jwt.limacharlie.io — not in allowlist

Configure now? [Y/n]
```

If the operator says yes, the CLI walks through each missing item:

```
LC_ORG_ID (config): <your-org-id>
  → Stored in config (agency config set LC_ORG_ID ...)

LC_API_KEY (secret): ****
  → Stored in credential store (agency creds set --name limacharlie-api ...)

Auth: jwt-exchange
  → JWT exchange config stored as protocol_config in credential store

Egress domains:
  → Added api.limacharlie.io to allowlist
  → Added jwt.limacharlie.io to allowlist
  → Restarting egress proxy...

All requirements met. Activating limacharlie...
✓ Activated. First poll in 2 minutes.
```

### Non-Interactive Mode

```
agency connector activate limacharlie \
  --set LC_API_KEY=<your-api-key> \
  --set LC_ORG_ID=<your-org-id>
```

All values provided via `--set` — no prompts. Fails with a clear error if anything is still missing.

### Dry Run

```
agency connector activate limacharlie --dry-run

Requirements check:
  ✗ LC_API_KEY — missing
  ✗ LC_ORG_ID — missing
  ✗ JWT swap config — not generated
  ✗ 2 egress domains not in allowlist

Would activate: limacharlie v0.2.0
Status: NOT READY (4 requirements unmet)
```

---

## REST API

### GET /api/v1/hub/connectors/{name}/requirements

Returns the structured requirements for a connector, with current status.

```json
{
  "connector": "limacharlie",
  "version": "0.2.0",
  "ready": false,
  "credentials": [
    {
      "name": "LC_API_KEY",
      "description": "LimaCharlie API key with detections and sensors read scope",
      "type": "secret",
      "scope": "service-grant",
      "grant_name": "limacharlie-api",
      "setup_url": "https://app.limacharlie.io/orgs/{LC_ORG_ID}/api-keys",
      "configured": false
    },
    {
      "name": "LC_ORG_ID",
      "description": "LimaCharlie organization ID",
      "type": "config",
      "scope": "env-var",
      "example": "<your-org-id>",
      "configured": false
    }
  ],
  "auth": {
    "type": "jwt-exchange",
    "configured": false
  },
  "egress_domains": [
    {"domain": "api.limacharlie.io", "allowed": false},
    {"domain": "jwt.limacharlie.io", "allowed": false}
  ]
}
```

### POST /api/v1/hub/connectors/{name}/configure

Accepts credential values and configures them:

```json
{
  "credentials": {
    "LC_API_KEY": "<your-api-key>",
    "LC_ORG_ID": "<your-org-id>"
  }
}
```

Response:
```json
{
  "configured": ["LC_API_KEY", "LC_ORG_ID"],
  "auth_configured": true,
  "egress_domains_added": ["api.limacharlie.io", "jwt.limacharlie.io"],
  "ready": true
}
```

### POST /api/v1/hub/connectors/{name}/activate

Activates the connector. Returns error if requirements are not met:

```json
{
  "error": "requirements_not_met",
  "missing": ["LC_API_KEY"]
}
```

---

## agency-web Integration

The REST API gives agency-web everything it needs to render a setup form:

1. Fetch `/api/v1/hub/connectors/{name}/requirements`
2. Render form fields: password inputs for `type: secret`, text inputs for `type: config`, with descriptions, examples, and setup_url links
3. Show auth type badge (JWT exchange, Bearer, OAuth2)
4. Show egress domain status (green check / red x)
5. Submit credentials via `/api/v1/hub/connectors/{name}/configure`
6. Activate via `/api/v1/hub/connectors/{name}/activate`

No connector-specific UI code needed — the form is generated from the requirements schema.

---

## MCP Tool

The existing `agency_admin_knowledge` tool pattern extends to connectors:

```
action: connector_requirements
connector: limacharlie
→ returns requirements JSON

action: connector_configure
connector: limacharlie
credentials: {LC_API_KEY: "...", LC_ORG_ID: "..."}
→ configures and returns status

action: connector_activate
connector: limacharlie
→ activates (fails if not ready)
```

---

## Backward Compatibility

Connectors without the extended `requires` block continue to work as today — `agency connector activate` activates immediately with no validation. The guided setup only engages when the connector declares requirements.

The existing `requires.services` and `requires.credentials` fields are preserved. The new fields (`type`, `scope`, `grant_name`, `setup_url`, `example`, `auth`, `egress_domains`) are all optional additions.

---

## Egress Domain Provenance

### Problem

When a connector is activated, its required egress domains are added to the allowlist. But without provenance tracking, the operator can't answer:
- "Why is `api.limacharlie.io` in my allowlist?" — was it added by a connector, a pack, or manually?
- "Can I remove this domain?" — is anything still using it?
- "What happens if I deactivate this connector?" — which domains should be cleaned up?

### Design

Every egress allowlist entry tracks its provenance — who added it and why.

**Storage:** `~/.agency/egress/domain-provenance.yaml`

```yaml
api.limacharlie.io:
  sources:
    - type: connector
      name: limacharlie
      added_at: "2026-03-28T19:30:00Z"
  auto_managed: true

jwt.limacharlie.io:
  sources:
    - type: connector
      name: limacharlie
      added_at: "2026-03-28T19:30:00Z"
  auto_managed: true

api.slack.com:
  sources:
    - type: connector
      name: slack-events
      added_at: "2026-03-15T10:00:00Z"
    - type: operator
      added_at: "2026-03-01T08:00:00Z"
      reason: "manual addition for testing"
  auto_managed: false   # operator also added it, don't auto-remove
```

### Lifecycle

**On connector activate:**
1. Read `requires.egress_domains` from the connector YAML
2. For each domain: add to egress allowlist if not present
3. Record provenance entry: `{type: connector, name: <connector>, added_at: <now>}`
4. Reload egress proxy

**On connector deactivate:**
1. For each domain the connector declared in `requires.egress_domains`:
   - Check provenance: is this domain used by any other active connector or pack?
   - If no other sources and `auto_managed: true`: remove from allowlist, delete provenance entry
   - If other sources exist: remove this connector's provenance entry, keep the domain
   - If `auto_managed: false` (operator also added it manually): remove connector's provenance entry, keep the domain
2. Reload egress proxy

**On manual `agency admin egress allow <domain>`:**
- Add provenance entry: `{type: operator, reason: <optional>}`
- Set `auto_managed: false` (operator-added domains are never auto-removed)

**On manual `agency admin egress deny <domain>`:**
- Remove the domain regardless of provenance (operator override)
- Clear all provenance entries for the domain
- Warn if active connectors require it: "Warning: limacharlie connector requires api.limacharlie.io — it will fail on next poll"

### CLI

```
agency admin egress domains
  DOMAIN                  SOURCE          ADDED         AUTO
  api.limacharlie.io     connector:lc    2026-03-28    yes
  jwt.limacharlie.io     connector:lc    2026-03-28    yes
  api.slack.com          connector:slack  2026-03-15    no
                         operator         2026-03-01
  hooks.slack.com        operator         2026-03-01    no

agency admin egress why api.limacharlie.io
  Required by:
    connector: limacharlie (active)
  Auto-managed: yes (will be removed on deactivate)
```

### REST API

```
GET /api/v1/hub/egress/domains
→ [{domain, sources: [{type, name, added_at}], auto_managed}]

GET /api/v1/hub/egress/domains/{domain}/provenance
→ {domain, sources: [...], auto_managed, active_dependents: ["limacharlie"]}
```

### Pack Inheritance

Packs that include connectors inherit their egress domain requirements. When a pack is activated, its connectors' `requires.egress_domains` are collected and provisioned. Provenance tracks the pack as the source:

```yaml
api.limacharlie.io:
  sources:
    - type: pack
      name: soc-triage
      connector: limacharlie
      added_at: "2026-03-28T19:30:00Z"
```

On pack deactivation, all connector-required domains from that pack are cleaned up following the same rules.

### Audit

Every domain addition and removal is logged to the platform audit log:
```json
{"event": "egress_domain_added", "domain": "api.limacharlie.io", "source": "connector:limacharlie", "auto_managed": true}
{"event": "egress_domain_removed", "domain": "api.limacharlie.io", "reason": "connector_deactivated", "connector": "limacharlie"}
```

---

## Security Properties

- **Secret values** (`type: secret`) are never returned by the requirements API after configuration. The API returns `configured: true/false`, not the value.
- **Credentials flow through the credential store** — they are resolved by the egress proxy via SocketKeyResolver (gateway Unix socket), never in agent containers (ASK Tenet 4).
- **JWT swap configs are auto-generated** from the `auth` block — operators don't need to write YAML manually, reducing misconfiguration risk. JWT exchange parameters are stored as `protocol_config` in the credential store.
- **Egress domains are validated** — the connector can't reach endpoints that aren't in the allowlist, even if credentials are configured (ASK Tenet 3).
- **Configure endpoint is localhost-only** — the `/connectors/{name}/configure` endpoint accepts secret values in the POST body. The gateway rejects configure requests from non-loopback source addresses unless TLS is active. This prevents secret transmission in cleartext over the network (ASK Tenet 4). The CLI accesses it locally; agency-web requires TLS if running on a different host.
- **Configure is idempotent** — re-running configure with the same values is safe. Each target (service grant, env var, jwt-swap entry, egress domain) is checked before writing; already-configured items are skipped. This prevents partial-state issues when configure is interrupted or retried.
- **Credential store is operator-owned** — `~/.agency/credentials/store.enc` is host-side only, never bind-mounted into agent containers. AES-256-GCM encrypted at rest. File permissions: 0600 owner-only.
- **Provenance file is operator-owned** — `~/.agency/egress/domain-provenance.yaml` tracks egress allowlist state on the host filesystem. File permissions: 0600 owner-only. Never accessible from agent containers.
