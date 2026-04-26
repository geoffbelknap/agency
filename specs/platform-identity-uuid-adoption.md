---
description: "UUID-based identity for all platform principals, centralized registry in the gateway, creation-time assignment, config-delivery distribution."
status: "Draft"
---

# Platform Identity & UUID Adoption

*UUID-based identity for every entity in the platform — agents, operators, teams, roles, channels.*

**Date:** 2026-04-06
**Status:** Draft

---

## Overview

Today every subsystem identifies entities by string names. Agent "researcher", channel "#security-ops", operator "geoff" — these are the primary keys everywhere: comms, intake, credentials, enforcer, knowledge graph. Names work fine within a single subsystem, but they create collisions and ambiguity at boundaries — two operators named "geoff" across deployments, an agent and a channel with similar names, credentials scoped by string matching.

This spec introduces a centralized principal registry with UUID-based identity. Every entity gets a UUID at creation time. Names remain the operator-facing interface — humans never type UUIDs. Internally, UUIDs are the cross-system identity used for authorization scoping, audit trails, and knowledge graph references.

**This is Spec A of a two-spec initiative.** Spec B (Principal ACL Model) builds on this registry to add permission enforcement, scope inheritance, delegation, and coverage principals. This spec delivers the identity infrastructure; Spec B activates authorization logic.

---

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| UUIDs vs names | UUIDs internal, names operator-facing | Names are great for humans, UUIDs for machines. Zero UX disruption. |
| Registry location | Gateway (Go, SQLite) | Gateway is the coordination point. Identity resolution works even when knowledge container is down. |
| Migration strategy | No migration — fresh deployments only | Simplifies everything. `agency setup` creates the registry from scratch. |
| Delivery to containers | Enforcer config delivery (body), gateway API (comms/knowledge/intake), file mount (fallback) | Reuses existing config delivery infrastructure. No new network hops for body runtime. |
| Principal types | operator, agent, team, role, channel | Covers all current entity types. Extensible via schema (no code change to add types). |
| Hierarchy | Parent field on principals | Teams contain agents, roles assigned to operators. Scope inheritance deferred to Spec B. |
| Forward-looking fields | parent, status, permissions | Stored now, enforced in Spec B. Avoids schema migration later. |

---

## The Registry

### Schema

The gateway manages a `PrincipalRegistry` backed by SQLite at `~/.agency/registry.db`.

```sql
CREATE TABLE principals (
    uuid TEXT PRIMARY KEY,
    type TEXT NOT NULL,            -- 'operator', 'agent', 'team', 'role', 'channel'
    name TEXT NOT NULL,            -- human-readable, unique within type
    parent TEXT,                   -- UUID of parent principal (team for agents, role for operators)
    status TEXT DEFAULT 'active',  -- 'active', 'suspended', 'revoked'
    permissions TEXT DEFAULT '[]', -- JSON array of permission strings (enforced by Spec B)
    created_at TEXT NOT NULL,
    metadata TEXT DEFAULT '{}',    -- JSON: additional properties
    UNIQUE(type, name)
);

CREATE INDEX idx_principals_type ON principals(type);
CREATE INDEX idx_principals_parent ON principals(parent);
CREATE INDEX idx_principals_status ON principals(status);
```

### Go Implementation

New package at `internal/registry/`:

```go
type Principal struct {
    UUID        string          `json:"uuid" yaml:"uuid"`
    Type        string          `json:"type" yaml:"type"`
    Name        string          `json:"name" yaml:"name"`
    Parent      string          `json:"parent,omitempty" yaml:"parent,omitempty"`
    Status      string          `json:"status" yaml:"status"`
    Permissions []string        `json:"permissions" yaml:"permissions"`
    CreatedAt   string          `json:"created_at" yaml:"created_at"`
    Metadata    json.RawMessage `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

type Registry struct {
    db *sql.DB
}

func Open(path string) (*Registry, error)
func (r *Registry) Register(principalType, name string, opts ...Option) (string, error)
func (r *Registry) Resolve(uuid string) (*Principal, error)
func (r *Registry) ResolveByName(principalType, name string) (*Principal, error)
func (r *Registry) List(principalType string) ([]Principal, error)
func (r *Registry) Update(uuid string, fields map[string]interface{}) error
func (r *Registry) Delete(uuid string) error
func (r *Registry) Snapshot() ([]byte, error)

// Options for Register
type Option func(*registerOpts)
func WithParent(uuid string) Option
func WithMetadata(m map[string]interface{}) Option
func WithPermissions(perms []string) Option
```

`Register()` generates a UUID via `github.com/google/uuid`, enforces `UNIQUE(type, name)`, and returns the UUID.

### Initialization

`agency setup` creates `~/.agency/registry.db` and registers the initial operator:

```go
reg, _ := registry.Open(filepath.Join(home, "registry.db"))
reg.Register("operator", operatorName)
```

---

## Creation-Time UUID Assignment

Every entity gets a UUID the moment it's created. No lazy assignment, no retroactive migration.

### Agent Creation

`agency agent create <name>`:
1. Gateway calls `registry.Register("agent", name)` → UUID
2. UUID stored in `~/.agency/agents/{name}/agent.yaml` as `uuid` field
3. Container labels include `agency.principal.uuid={uuid}`
4. Agent principal automatically linked to team if `--team` flag provided (sets `parent`)

### Channel Creation

Channels are created via the comms service (`POST /channels`). The gateway registers the channel before or after comms creates it:
1. Gateway calls `registry.Register("channel", channelName)` → UUID
2. UUID passed to comms as metadata, stored in channel's `meta.json`
3. For pack-deployed channels: registered during pack activation

### Operator Registration

`agency setup` (first operator) or `agency admin operator add <name>`:
1. `registry.Register("operator", name)` → UUID
2. UUID stored in `principals.yaml` on the operator entry

### Team Creation

`agency team create <name>`:
1. `registry.Register("team", name)` → UUID
2. Members added via `agency team add <team> <agent>` → sets `parent` on agent's registry entry to team's UUID

### Role Creation

Deferred — roles are a Spec B concern. The registry supports the type; creation commands come later.

---

## Registry Delivery to Containers

### Path 1: Enforcer Config Delivery (Body Runtime)

The enforcer already serves `/config/{filename}` to the body runtime. The gateway writes `registry.json` to the agent's config directory alongside `PLATFORM.md`, `mission.yaml`, and `services-manifest.json`. The enforcer serves it. The body runtime fetches on startup and on SIGHUP (existing hot-reload pattern).

### Path 2: Gateway API (Comms, Knowledge, Intake)

These containers reach the gateway via `http://gateway:8200`. They call:
- `GET /api/v1/admin/registry` — full snapshot (cached, refreshed on mutation)
- `GET /api/v1/admin/registry/resolve?type=agent&name=researcher` — single lookup

Cache locally with 60s TTL. Resolution only happens at entity boundaries, not per-message.

### Path 3: File Mount (Fallback)

`~/.agency/registry.json` written by the gateway on every registry mutation. Mounted read-only into containers that need offline resolution. This is a fallback for containers that start before the gateway socket proxy is ready.

### Snapshot Format

```json
{
  "version": 1,
  "generated_at": "2026-04-06T...",
  "principals": [
    {
      "uuid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
      "type": "agent",
      "name": "researcher",
      "parent": "f1e2d3c4-b5a6-7890-1234-567890abcdef",
      "status": "active",
      "permissions": [],
      "metadata": {}
    }
  ]
}
```

### Knowledge Service Integration

The knowledge service's `principal_registry.py` (from Phase 1) is updated to read from the snapshot file instead of maintaining its own SQLite table. `resolve()` and `resolve_name()` read from an in-memory dict loaded from the snapshot. The knowledge service's `POST /principals` endpoint is removed — registration is gateway-only. `GET /principals` and `GET /principals/{uuid}` remain but read from the snapshot.

---

## API Surface

### New Gateway REST Endpoints

| Method | Path | Description | Auth |
|---|---|---|---|
| `GET` | `/api/v1/admin/registry` | Full registry snapshot | Any authenticated |
| `GET` | `/api/v1/admin/registry/resolve` | Resolve by `?type=&name=` or `?uuid=` | Any authenticated |
| `POST` | `/api/v1/admin/registry` | Register a principal | **Operator only** |
| `PUT` | `/api/v1/admin/registry/{uuid}` | Update principal fields | **Operator only** |
| `DELETE` | `/api/v1/admin/registry/{uuid}` | Delete principal | **Operator only** |

**ASK Tenet 18 (hierarchy inviolable from below):** All write endpoints (POST, PUT, DELETE) require operator-level authentication. The enforcer MUST NOT proxy these endpoints to agent containers. Agents can read the registry (GET) but cannot modify it.

**ASK Tenet 13 (authority monitored):** All registry mutations are logged as authority-level events in the gateway audit trail — same rigor as agent action logging.

### New CLI Commands

```
agency registry list [--type agent|operator|team|role|channel]
agency registry show <name-or-uuid>
agency registry update <name-or-uuid> [--parent <uuid>] [--status active|suspended|revoked]
agency registry delete <name-or-uuid>
```

Registration happens implicitly through existing creation commands — no separate `agency registry register` needed.

### Changes to Existing Commands

- `agency agent create <name>` — auto-registers in registry, prints UUID in output
- `agency agent list` — gains UUID column
- `agency status` — includes registry stats (principal count by type)

### MCP Tool Updates

The existing `agency_admin_knowledge` MCP tool's `principal_*` actions are redirected to the gateway registry endpoints instead of the knowledge service.

---

## Registry Snapshot Scoping

The full `registry.json` snapshot delivered to containers contains all principals. An agent receiving this snapshot can see the existence of all operators, teams, and channels — including ones outside its authorization scope.

**Decision:** Principal existence is not sensitive information. Knowing that "operator:geoff" or "channel:#security-ops" exists does not grant access to anything — scope enforcement (Phase 1/1b) gates actual data access. This is analogous to a corporate directory: employees can see that other departments exist without having access to their files.

If a future deployment requires principal existence to be confidential (e.g., compartmented operations), the snapshot delivery can be scoped per-agent. The API endpoint `GET /api/v1/admin/registry` can accept a `scope` parameter. This is not implemented in this spec.

---

## Status Field: Informational Until Spec B

The `status` field supports `active`, `suspended`, and `revoked`. However, until Spec B implements:
- Coverage principal transfer (ASK Tenet 16 — authority never orphaned)
- Permission enforcement gating on status

**Status changes are informational only.** Setting `status: suspended` on an operator logs the change and updates the registry, but does NOT:
- Halt the operator's agents
- Revoke credentials
- Block API access

This prevents a false sense of security from status changes that aren't yet enforced. Spec B activates enforcement.

---

## ASK Compliance

| Tenet | How this spec complies |
|---|---|
| Tenet 1 (Constraints external) | Registry is gateway infrastructure, not in agent containers. Agents cannot modify the registry. |
| Tenet 2 (Every action traced) | Registry mutations logged by the gateway's audit trail. |
| Tenet 6 (Trust explicit/auditable) | **Strengthened.** Every trust relationship has a UUID, declared in the registry, inspectable via API. |
| Tenet 7 (Least privilege) | UUIDs enable per-principal scoping. Snapshot delivery via enforcer follows existing config path. |
| Tenet 9 (Constraints atomic) | Registry snapshot is a single file delivered atomically. |
| Tenet 13 (Authority monitored) | Registry mutations logged as authority-level events. |
| Tenet 15 (Independent lifecycles) | Registry separates principal identity (UUID, persistent) from agent lifecycle (lifecycle_id, per-startup). |
| Tenet 16 (Authority never orphaned) | Status field exists. Coverage transfer deferred to Spec B. Status changes are informational until then. |
| Tenet 17 (Trust earned/monitored) | Status and permissions fields enable trust tracking. Enforcement deferred to Spec B. |
| Tenet 18 (Hierarchy inviolable from below) | All registry write endpoints are operator-only. Enforcer does not proxy writes to agents. |
| Tenet 25 (Identity mutations auditable) | UUID provides stable reference for forensic reconstruction. All mutations logged. |
| Tenet 27 (Knowledge access bounded) | Knowledge service reads scope from snapshot. Scope enforcement from Phase 1/1b unchanged. |

---

## Implementation Phases

### Phase 1: Registry Core

- `internal/registry/` Go package with `Registry` struct, SQLite backend
- `Principal` model with all fields (uuid, type, name, parent, status, permissions, metadata)
- CRUD operations: Register, Resolve, ResolveByName, List, Update, Delete, Snapshot
- `~/.agency/registry.db` created by `agency setup`
- Initial operator registered during setup
- Unit tests for all operations

### Phase 2: Creation-Time Assignment

- `agency agent create` → auto-register agent in registry, store UUID in agent.yaml
- Channel creation → register in registry, UUID in meta.json
- `agency admin operator add` → register operator
- `agency team create` / `agency team add` → register team, set parent links
- Container labels include `agency.principal.uuid`

### Phase 3: API & CLI

- Gateway REST endpoints: GET/POST/PUT/DELETE `/api/v1/admin/registry`
- CLI: `agency registry list/show/update/delete`
- Existing commands updated: `agent create` prints UUID, `agent list` shows UUID column
- Operator-only auth gating on write endpoints

### Phase 4: Config Delivery & Knowledge Integration

- Gateway writes `registry.json` snapshot on every mutation
- Snapshot delivered to agents via enforcer config path
- Knowledge service `principal_registry.py` updated to read from snapshot
- Knowledge service `POST /principals` endpoint removed
- MCP tool actions redirected to gateway

---

## Testing

### Phase 1 Tests

- Register principal returns UUID
- Unique constraint on (type, name) enforced
- Resolve by UUID and by name
- List by type
- Update parent, status, permissions
- Delete removes principal
- Snapshot serializes all principals

### Phase 2 Tests

- Agent create registers in registry
- Agent UUID stored in agent.yaml
- Channel create registers in registry
- Operator add registers in registry
- Team create registers, member add sets parent
- Container labels include UUID

### Phase 3 Tests

- GET /registry returns snapshot
- GET /registry/resolve by name and UUID
- POST /registry creates principal
- PUT /registry/{uuid} updates fields
- DELETE /registry/{uuid} removes principal
- Write endpoints reject non-operator auth

### Phase 4 Tests

- Snapshot file written on mutation
- Enforcer serves registry.json
- Knowledge service reads from snapshot
- Knowledge service POST /principals returns 410 (removed)
- Scope resolution uses snapshot data

---

## Follow-On: Spec B (Principal ACL Model)

This spec delivers the identity registry. Spec B activates authorization:

- **Permission enforcement** — Gate API actions and agent capabilities by principal permissions
- **Scope inheritance** — Team scope flows to member agents. Role permissions flow to assigned operators.
- **Delegation** — Principals can grant subsets of their permissions to others (Tenet 19)
- **Coverage principals** — Suspended principals transfer authority (Tenet 16)
- **Classification enforcement** — Knowledge graph `classification` field gates access (public/internal/restricted/confidential)
- **Cross-deployment federation** — Multiple Agency instances sharing identity
