# API Path Consolidation

API paths should mirror Go package names. The package defines the domain boundary, the API path reflects it, and the CLI is an ergonomic alias on top.

## Canonical Naming Chain

Go package → API path → CLI command

## Current State: 35 flat top-level paths

No hierarchy, no grouping. A consumer can't tell which paths are related.

## Target State: 11 top-level groups

| Go Package | API Path Prefix | CLI Command | What moves under it |
|---|---|---|---|
| `admin` | `/api/v1/admin/` | `agency admin` | `/capabilities`, `/policy`, `/profiles`, `/registry`, `/teams`, `/audit` |
| `agents` | `/api/v1/agents/` | `agency agent` | `/economics`, `/meeseeks` |
| `comms` | `/api/v1/comms/` | `agency comms` | `/channels`, `/unreads` |
| `creds` | `/api/v1/creds/` | `agency creds` | `/credentials` (renamed) |
| `events` | `/api/v1/events/` | `agency event` | `/webhooks`, `/notifications`, `/subscriptions`, `/intake` |
| `graph` | `/api/v1/graph/` | `agency graph` | `/knowledge`, `/ontology` (renamed) |
| `hub` | `/api/v1/hub/` | `agency hub` | `/connectors`, `/presets`, `/deploy`, `/teardown`, `/egress` |
| `infra` | `/api/v1/infra/` | `agency infra` | `/providers`, `/routing`, `/setup` |
| `missions` | `/api/v1/missions/` | `agency mission` | *(no change)* |
| *(root)* | `/api/v1/mcp/` | `agency mcp-server` | *(no change)* |
| `platform` | `/api/v1/health`, `/api/v1/init` | `agency setup/status` | Cross-cutting, stays at root |

## Detailed Path Mapping

### admin

| Old | New |
|---|---|
| `/api/v1/capabilities` | `/api/v1/admin/capabilities` |
| `/api/v1/capabilities/{name}` | `/api/v1/admin/capabilities/{name}` |
| `/api/v1/capabilities/{name}/enable` | `/api/v1/admin/capabilities/{name}/enable` |
| `/api/v1/capabilities/{name}/disable` | `/api/v1/admin/capabilities/{name}/disable` |
| `/api/v1/policy/{agent}` | `/api/v1/admin/policy/{agent}` |
| `/api/v1/policy/{agent}/validate` | `/api/v1/admin/policy/{agent}/validate` |
| `/api/v1/profiles` | `/api/v1/admin/profiles` |
| `/api/v1/profiles/{id}` | `/api/v1/admin/profiles/{id}` |
| `/api/v1/registry` | `/api/v1/admin/registry` |
| `/api/v1/registry/resolve` | `/api/v1/admin/registry/resolve` |
| `/api/v1/registry/list` | `/api/v1/admin/registry/list` |
| `/api/v1/registry/{uuid}` | `/api/v1/admin/registry/{uuid}` |
| `/api/v1/registry/{uuid}/effective` | `/api/v1/admin/registry/{uuid}/effective` |
| `/api/v1/teams` | `/api/v1/admin/teams` |
| `/api/v1/teams/{name}` | `/api/v1/admin/teams/{name}` |
| `/api/v1/teams/{name}/activity` | `/api/v1/admin/teams/{name}/activity` |
| `/api/v1/audit/summarize` | `/api/v1/admin/audit/summarize` |
| `/api/v1/admin/doctor` | *(no change)* |
| `/api/v1/admin/destroy` | *(no change)* |
| `/api/v1/admin/trust` | *(no change)* |
| `/api/v1/admin/audit` | *(no change)* |
| `/api/v1/admin/egress` | *(no change)* |
| `/api/v1/admin/knowledge` | `/api/v1/admin/graph` (matches graph package rename) |
| `/api/v1/admin/department` | *(no change)* |
| `/api/v1/agents/{name}/rebuild` | `/api/v1/admin/agents/{name}/rebuild` |

### agents

| Old | New |
|---|---|
| `/api/v1/economics/summary` | `/api/v1/agents/economics/summary` |
| `/api/v1/meeseeks` | `/api/v1/agents/meeseeks` |
| `/api/v1/meeseeks/{id}` | `/api/v1/agents/meeseeks/{id}` |
| `/api/v1/meeseeks/{id}/complete` | `/api/v1/agents/meeseeks/{id}/complete` |
| All other `/api/v1/agents/*` | *(no change)* |

### comms

| Old | New |
|---|---|
| `/api/v1/channels` | `/api/v1/comms/channels` |
| `/api/v1/channels/{name}/messages` | `/api/v1/comms/channels/{name}/messages` |
| `/api/v1/channels/{name}/messages/{id}` | `/api/v1/comms/channels/{name}/messages/{id}` |
| `/api/v1/channels/{name}/messages/{id}/reactions` | `/api/v1/comms/channels/{name}/messages/{id}/reactions` |
| `/api/v1/channels/{name}/messages/{id}/reactions/{emoji}` | `/api/v1/comms/channels/{name}/messages/{id}/reactions/{emoji}` |
| `/api/v1/channels/search` | `/api/v1/comms/channels/search` |
| `/api/v1/channels/{name}/archive` | `/api/v1/comms/channels/{name}/archive` |
| `/api/v1/channels/{name}/mark-read` | `/api/v1/comms/channels/{name}/mark-read` |
| `/api/v1/unreads` | `/api/v1/comms/unreads` |

### creds

| Old | New |
|---|---|
| `/api/v1/credentials` | `/api/v1/creds` |
| `/api/v1/credentials/{name}` | `/api/v1/creds/{name}` |
| `/api/v1/credentials/{name}/rotate` | `/api/v1/creds/{name}/rotate` |
| `/api/v1/credentials/{name}/test` | `/api/v1/creds/{name}/test` |
| `/api/v1/credentials/groups` | `/api/v1/creds/groups` |
| `/api/v1/internal/credentials/resolve` | `/api/v1/internal/creds/resolve` |

### events

| Old | New |
|---|---|
| `/api/v1/webhooks` | `/api/v1/events/webhooks` |
| `/api/v1/webhooks/{name}` | `/api/v1/events/webhooks/{name}` |
| `/api/v1/webhooks/{name}/rotate-secret` | `/api/v1/events/webhooks/{name}/rotate-secret` |
| `/api/v1/events/webhook/{name}` | *(no change — already nested)* |
| `/api/v1/notifications` | `/api/v1/events/notifications` |
| `/api/v1/notifications/{name}` | `/api/v1/events/notifications/{name}` |
| `/api/v1/notifications/{name}/test` | `/api/v1/events/notifications/{name}/test` |
| `/api/v1/subscriptions` | `/api/v1/events/subscriptions` |
| `/api/v1/intake/items` | `/api/v1/events/intake/items` |
| `/api/v1/intake/stats` | `/api/v1/events/intake/stats` |
| `/api/v1/intake/webhook` | `/api/v1/events/intake/webhook` |
| `/api/v1/events` | *(no change)* |
| `/api/v1/events/{id}` | *(no change)* |

Note: `/api/v1/intake/poll-health` and `/api/v1/intake/poll/{connector}` are in the hub package — they move to `/api/v1/hub/intake/poll-health` and `/api/v1/hub/intake/poll/{connector}`.

### graph

| Old | New |
|---|---|
| `/api/v1/knowledge/query` | `/api/v1/graph/query` |
| `/api/v1/knowledge/who-knows` | `/api/v1/graph/who-knows` |
| `/api/v1/knowledge/stats` | `/api/v1/graph/stats` |
| `/api/v1/knowledge/export` | `/api/v1/graph/export` |
| `/api/v1/knowledge/import` | `/api/v1/graph/import` |
| `/api/v1/knowledge/changes` | `/api/v1/graph/changes` |
| `/api/v1/knowledge/context` | `/api/v1/graph/context` |
| `/api/v1/knowledge/neighbors` | `/api/v1/graph/neighbors` |
| `/api/v1/knowledge/path` | `/api/v1/graph/path` |
| `/api/v1/knowledge/flags` | `/api/v1/graph/flags` |
| `/api/v1/knowledge/restore` | `/api/v1/graph/restore` |
| `/api/v1/knowledge/curation-log` | `/api/v1/graph/curation-log` |
| `/api/v1/knowledge/pending` | `/api/v1/graph/pending` |
| `/api/v1/knowledge/review/{id}` | `/api/v1/graph/review/{id}` |
| `/api/v1/knowledge/ontology` | `/api/v1/graph/ontology` |
| `/api/v1/knowledge/ontology/types` | `/api/v1/graph/ontology/types` |
| `/api/v1/knowledge/ontology/relationships` | `/api/v1/graph/ontology/relationships` |
| `/api/v1/knowledge/ontology/validate` | `/api/v1/graph/ontology/validate` |
| `/api/v1/knowledge/ontology/migrate` | `/api/v1/graph/ontology/migrate` |
| `/api/v1/ontology/candidates` | `/api/v1/graph/ontology/candidates` |
| `/api/v1/ontology/promote` | `/api/v1/graph/ontology/promote` |
| `/api/v1/ontology/reject` | `/api/v1/graph/ontology/reject` |
| `/api/v1/knowledge/principals` | `/api/v1/graph/principals` |
| `/api/v1/knowledge/principals/{uuid}` | `/api/v1/graph/principals/{uuid}` |
| `/api/v1/knowledge/quarantine` | `/api/v1/graph/quarantine` |
| `/api/v1/knowledge/quarantine/release` | `/api/v1/graph/quarantine/release` |
| `/api/v1/knowledge/classification` | `/api/v1/graph/classification` |
| `/api/v1/knowledge/communities` | `/api/v1/graph/communities` |
| `/api/v1/knowledge/communities/{id}` | `/api/v1/graph/communities/{id}` |
| `/api/v1/knowledge/hubs` | `/api/v1/graph/hubs` |
| `/api/v1/knowledge/ingest` | `/api/v1/graph/ingest` |
| `/api/v1/knowledge/insight` | `/api/v1/graph/insight` |

### hub

| Old | New |
|---|---|
| `/api/v1/connectors/{name}/requirements` | `/api/v1/hub/connectors/{name}/requirements` |
| `/api/v1/connectors/{name}/configure` | `/api/v1/hub/connectors/{name}/configure` |
| `/api/v1/presets` | `/api/v1/hub/presets` |
| `/api/v1/presets/{name}` | `/api/v1/hub/presets/{name}` |
| `/api/v1/deploy` | `/api/v1/hub/deploy` |
| `/api/v1/teardown/{pack}` | `/api/v1/hub/teardown/{pack}` |
| `/api/v1/egress/domains` | `/api/v1/hub/egress/domains` |
| `/api/v1/egress/domains/{domain}/provenance` | `/api/v1/hub/egress/domains/{domain}/provenance` |
| `/api/v1/intake/poll-health` | `/api/v1/hub/intake/poll-health` |
| `/api/v1/intake/poll/{connector}` | `/api/v1/hub/intake/poll/{connector}` |
| All other `/api/v1/hub/*` | *(no change)* |

### infra

| Old | New |
|---|---|
| `/api/v1/providers` | `/api/v1/infra/providers` |
| `/api/v1/routing/metrics` | `/api/v1/infra/routing/metrics` |
| `/api/v1/routing/config` | `/api/v1/infra/routing/config` |
| `/api/v1/routing/suggestions` | `/api/v1/infra/routing/suggestions` |
| `/api/v1/routing/suggestions/{id}/approve` | `/api/v1/infra/routing/suggestions/{id}/approve` |
| `/api/v1/routing/suggestions/{id}/reject` | `/api/v1/infra/routing/suggestions/{id}/reject` |
| `/api/v1/routing/stats` | `/api/v1/infra/routing/stats` |
| `/api/v1/setup/config` | `/api/v1/infra/setup/config` |
| `/api/v1/internal/llm` | `/api/v1/infra/internal/llm` |
| All other `/api/v1/infra/*` | *(no change)* |

## CLI Command Mapping

| Old | New |
|---|---|
| `agency knowledge` | `agency graph` |
| `agency channel` | `agency comms` |
| `agency credential` / `agency credentials` | *(removed — `agency creds` is canonical)* |
| `agency notify` / `agency notifications` | `agency event notify` or keep as top-level alias |
| `agency registry` | `agency admin registry` |
| `agency routing` | `agency infra routing` |
| `agency webhook` | `agency event webhook` |
| `agency audit` | `agency admin audit` |

## Consumers to Update

1. **Go routes** — `internal/api/*/routes.go` (path strings)
2. **CLI** — `internal/cli/commands.go` (API paths in REST calls)
3. **MCP tools** — `internal/api/mcp_*.go` (any hardcoded paths)
4. **OpenAPI spec** — `internal/api/openapi.yaml`
5. **Web UI** — `web/` (API fetch calls)
6. **Body runtime** — `images/body/` (enforcer mediation paths)
7. **Enforcer** — `images/enforcer/` (mediation proxy routes)
8. **CLAUDE.md** — endpoint references
9. **Docs/specs** — any spec referencing API paths

## No Backward Compatibility

Clean break. No aliases, no redirects, no deprecation period.
