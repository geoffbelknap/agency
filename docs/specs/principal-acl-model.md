---
description: "Permission enforcement, hierarchical scope inheritance, coverage principals, and default permission set for the platform principal model."
status: "Draft"
---

# Principal ACL Model

*Activate permission enforcement on the platform principal registry — hierarchical scope inheritance, wildcard matching, suspension coverage, and default-deny routing.*

**Date:** 2026-04-06
**Status:** Draft
**Depends on:** [Platform Identity & UUID Adoption](platform-identity-uuid-adoption.md) (implemented)

---

## Overview

The principal registry (Spec A) stores UUIDs, parent hierarchy, status, and permissions for every entity — but nothing enforces them yet. All authenticated requests have equal authority. This spec activates enforcement.

Two layers: middleware gates every route by required permission, handlers do resource-specific checks. Permissions inherit down the hierarchy with a ceiling model — a child can never exceed its parent's scope. Suspension transfers authority to the parent. No parent means fail-closed.

---

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Enforcement layers | Middleware (route-level) + handler (resource-level) | Middleware catches broad categories, handlers handle nuance (e.g., "can this principal see *this* agent"). |
| Scope inheritance | Ceiling model — parent defines max, children ≤ parent | Directly implements ASK Tenet 19 (delegation cannot exceed delegator). Structural guarantee, not policy. |
| Permission format | Hierarchical with wildcards (`knowledge.*`, `*`) | 80/20 — wildcards cover expansion. `*` for superuser. Resource-scoped suffixes can be added later. |
| Coverage on suspension | Hierarchy fallback to parent | Reuses existing `parent` field. No extra `coverage` field needed. Fail-closed if no parent. |
| Default permissions | Read/write pairs per API group (~15), plus `admin.*` | Maps cleanly to existing handler files. Operators start with `*`. |
| Empty permissions | Inherit parent's full set | New agents in a team get team defaults without explicit assignment. Documented as intentional. |

---

## Permission Enforcement

### Layer 1: Route Middleware

A chi middleware applied to all `/api/v1/` routes. For each request:

1. Extract requesting principal from auth token (gateway token → operator, scoped token → agent)
2. Match request method + path against the permission map
3. Load principal from registry, compute effective permissions
4. Check if effective permissions cover the required permission
5. If denied → 403 Forbidden with `{"error": "permission denied", "required": "agent.write"}`
6. If approved → pass to handler

**Permission map** — built at route registration time:

```go
var routePermissions = map[string]string{
    // Agent operations
    "POST /api/v1/agents":              "agent.write",
    "GET /api/v1/agents":               "agent.read",
    "GET /api/v1/agents/*":             "agent.read",
    "POST /api/v1/agents/*/start":      "agent.write",
    "POST /api/v1/agents/*/stop":       "agent.write",
    "POST /api/v1/agents/*/halt":       "agent.write",
    "DELETE /api/v1/agents/*":          "agent.write",
    "POST /api/v1/agents/*/send":       "agent.write",

    // Knowledge
    "GET /api/v1/knowledge/*":          "knowledge.read",
    "POST /api/v1/knowledge/query":     "knowledge.read",
    "POST /api/v1/knowledge/ingest":    "knowledge.write",
    "POST /api/v1/knowledge/insight":   "knowledge.write",

    // Registry
    "GET /api/v1/registry":             "registry.read",
    "GET /api/v1/registry/*":           "registry.read",
    "POST /api/v1/registry":            "registry.write",
    "PUT /api/v1/registry/*":           "registry.write",
    "DELETE /api/v1/registry/*":        "registry.write",

    // Missions
    "GET /api/v1/missions":             "mission.read",
    "GET /api/v1/missions/*":           "mission.read",
    "POST /api/v1/missions":            "mission.write",
    "PUT /api/v1/missions/*":           "mission.write",
    "DELETE /api/v1/missions/*":        "mission.write",

    // Hub
    "GET /api/v1/hub/*":                "hub.read",
    "POST /api/v1/hub/*":              "hub.write",

    // Credentials
    "GET /api/v1/credentials":          "creds.read",
    "GET /api/v1/credentials/*":        "creds.read",
    "POST /api/v1/credentials":         "creds.write",
    "PUT /api/v1/credentials/*":        "creds.write",
    "DELETE /api/v1/credentials/*":     "creds.write",

    // Infrastructure
    "GET /api/v1/infra/*":              "infra.read",
    "POST /api/v1/infra/*":            "infra.write",

    // Notifications
    "GET /api/v1/notifications":        "notification.read",
    "GET /api/v1/notifications/*":      "notification.read",
    "POST /api/v1/notifications":       "notification.write",
    "DELETE /api/v1/notifications/*":   "notification.write",

    // Admin — catch-all for operator-only operations
    "POST /api/v1/admin/*":             "admin.*",
}
```

**Unmatched routes default to `admin.*`** — fail-closed. New routes added without a permission mapping are denied to non-admin principals.

**Startup validation:** On gateway startup, log a warning for any registered route that doesn't have a permission mapping. This catches routes added in future PRs that forget to add a mapping.

### Layer 2: Handler-Level Checks

For resource-specific authorization that middleware can't express:

```go
// Example: handler checks if principal can access this specific agent
func (h *handler) agentShow(w http.ResponseWriter, r *http.Request) {
    principal := getPrincipal(r)  // set by middleware
    agentName := chi.URLParam(r, "name")

    // Middleware already confirmed agent.read — now check resource scope
    if !h.canAccessAgent(principal, agentName) {
        http.Error(w, "access denied to this agent", http.StatusForbidden)
        return
    }
    // ... handle request
}

func (h *handler) canAccessAgent(principal *registry.Principal, agentName string) bool {
    // Operators can access all agents
    if principal.Type == "operator" {
        return true
    }
    // Agents can access themselves
    if principal.Type == "agent" && principal.Name == agentName {
        return true
    }
    // Team members can access agents in their team
    if principal.Parent != "" {
        agent, _ := h.infra.Registry.ResolveByName("agent", agentName)
        if agent != nil && agent.Parent == principal.Parent {
            return true  // same team
        }
    }
    return false
}
```

Handler checks are additive — they narrow access beyond what middleware grants. Middleware is never bypassed.

---

## Scope Inheritance — Ceiling Model

### Algorithm

```
effective_permissions(principal):
    own = principal.Permissions
    if principal.Parent == "":
        return own  // top-level, no ceiling

    parent = registry.Resolve(principal.Parent)
    ceiling = effective_permissions(parent)  // recursive

    if own is empty:
        return ceiling  // inherit parent's full set

    return [p for p in own if permits(ceiling, p)]  // intersection with ceiling
```

### Wildcard Matching

`permits(permission_set, required)` returns true if any permission in the set covers the required permission:

```go
func permits(perms []string, required string) bool {
    for _, p := range perms {
        if p == "*" {
            return true
        }
        if p == required {
            return true
        }
        // Wildcard: "knowledge.*" covers "knowledge.read"
        if strings.HasSuffix(p, ".*") {
            prefix := strings.TrimSuffix(p, ".*")
            if strings.HasPrefix(required, prefix+".") {
                return true
            }
        }
    }
    return false
}
```

### Examples

| Parent permissions | Own permissions | Effective |
|---|---|---|
| `["*"]` | `[]` (empty) | `["*"]` — inherit full ceiling |
| `["*"]` | `["knowledge.read"]` | `["knowledge.read"]` — own within ceiling |
| `["knowledge.*", "agent.read"]` | `["knowledge.read"]` | `["knowledge.read"]` — within ceiling |
| `["knowledge.read"]` | `["knowledge.write"]` | `[]` — write not in ceiling, blocked |
| (no parent) | `["agent.read"]` | `["agent.read"]` — no ceiling |

### Caching

Effective permissions are computed on each request (they depend on current registry state — parent could be updated between requests). For performance, the middleware caches `{principal_uuid: effective_perms}` with a 60-second TTL. Registry mutations (Update, Delete) invalidate the cache.

---

## Coverage and Suspension

### Suspension Flow

`registry update <principal> --status suspended`:

1. Gateway validates the principal exists
2. Gateway checks that a parent exists and is active
   - If no parent and no `--force`: reject with error "no coverage principal — use --force to fail-closed"
   - If no parent and `--force`: proceed, governed agents will fail-closed
3. Set `status: "suspended"` in registry
4. Log authority-level event: `{action: "suspend", principal: uuid, coverage: parent_uuid}`
5. Write updated snapshot

### What Suspension Does

- **Suspended principal cannot authenticate** — middleware rejects with 403 if `status != "active"`
- **Governed agents continue running** — ASK tenet 15 (independent lifecycles)
- **Parent gains governance** — parent can halt, resume, reassign the suspended principal's agents
- **Effective permissions unchanged** — agents were already within parent's ceiling

### Revocation Flow

`registry update <principal> --status revoked`:

1. Same validation as suspension
2. Set `status: "revoked"` in registry
3. **Halt all governed agents immediately** — send halt signal to each agent whose parent is this principal
4. Log authority-level event
5. Requires `--force` flag (destructive operation)

### Fail-Closed Behavior

When an agent's governing chain has no active principal (all suspended/revoked, no reachable parent):
- Agent cannot start new tasks
- Agent completes current task (if any) then halts
- Agent's API endpoints return 503 "governance unavailable"
- Operator must restore an active principal or explicitly decommission

---

## Default Permission Set

### Permission Definitions

| Permission | Covers |
|---|---|
| `agent.read` | List, show, status, economics, trajectory, episodes, procedures |
| `agent.write` | Create, start, stop, halt, delete, rebuild, send, signal |
| `knowledge.read` | Query, who-knows, stats, export, neighbors, path, communities, hubs, context |
| `knowledge.write` | Ingest, insight, contribute, ontology promote/reject, curate |
| `registry.read` | List, resolve, snapshot |
| `registry.write` | Register, update, delete |
| `mission.read` | List, show, evaluations, procedures, episodes |
| `mission.write` | Create, assign, pause, resume, complete, delete |
| `hub.read` | Search, list, instances, show |
| `hub.write` | Install, activate, config, update, remove |
| `creds.read` | List, show (redacted values) |
| `creds.write` | Set, delete, rotate, test, groups |
| `infra.read` | Status, logs |
| `infra.write` | Up, down, restart |
| `notification.read` | List notifications |
| `notification.write` | Add, remove, test notifications |
| `admin.*` | Doctor, rebuild, usage, destroy — operator-only |

### Default Assignments

| Principal | Default Permissions | Rationale |
|---|---|---|
| New operator | `["*"]` | Superuser. Single-operator deployments just work. |
| New team | `["agent.read", "knowledge.read", "knowledge.write", "mission.read"]` | Teams can read agents and work with knowledge. |
| New agent (no team) | `["knowledge.read", "knowledge.write"]` | Minimal: query and contribute to knowledge graph. |
| New agent (in team) | `[]` (empty — inherits team ceiling) | Gets team's full permission set via inheritance. |

### Behavioral Note: Empty Permissions

When a principal has `permissions: []` and a `parent`, it inherits the parent's full permission set. This is intentional — it means "use whatever the team/role allows" without explicit assignment. This behavior is prominent in the CLI output:

```
$ agency registry show researcher
UUID:        a1b2c3d4-...
Type:        agent
Name:        researcher
Parent:      f1e2d3c4-... (team: infra)
Status:      active
Permissions: [] (effective: [agent.read, knowledge.read, knowledge.write, mission.read])
```

The `effective:` display shows what the principal can actually do after inheritance resolution.

---

## ASK Compliance

| Tenet | How this spec complies |
|---|---|
| Tenet 1 (Constraints external) | Permission enforcement in gateway middleware, outside agent boundary. |
| Tenet 5 (Least privilege) | **Strengthened.** Agents get explicit, minimal permission sets. Ceiling model prevents over-granting. |
| Tenet 7 (Default-deny) | Unmatched routes require `admin.*`. Middleware denies if permission missing. |
| Tenet 11 (No self-elevation) | Ceiling model structural guarantee. Agents can't modify registry. |
| Tenet 12 (Human outranks agent) | Operators get `*`, agents get scoped subsets. Hierarchy enforces ordering. |
| Tenet 15 (Independent lifecycles) | Suspension doesn't auto-halt agents. Revocation does (explicit destructive action). |
| Tenet 16 (Authority never orphaned) | Parent fallback on suspension. No parent = fail-closed with `--force`. |
| Tenet 17 (Trust monitored) | Permission changes and suspension logged as authority-level events. |
| Tenet 18 (Hierarchy inviolable from below) | Ceiling model — agent can never exceed parent's permission set. Structural, not policy. |
| Tenet 19 (Delegation ≤ delegator) | **Directly implemented.** `effective = own ∩ ceiling`. |

### Documentation Requirements (from ASK Review)

1. **Permissions are never auto-derived from graph content.** The knowledge graph cannot influence permission decisions. Permissions are operator-explicit only.
2. **Startup validation for route mapping completeness.** Every registered route must have a permission mapping. Unmapped routes default to `admin.*` (deny). Warnings logged for unmapped routes.
3. **Empty-permissions inheritance is intentional.** `permissions: []` with a parent means "inherit parent's full set." Documented prominently in CLI output and this spec.

---

## Implementation Phases

### Phase 1: Permission Resolution Engine

- `internal/registry/permissions.go` — `EffectivePermissions()`, `Permits()`, wildcard matching
- Unit tests for all inheritance/wildcard/ceiling scenarios
- Cache with TTL and invalidation

### Phase 2: Route Middleware

- `internal/api/middleware_permissions.go` — chi middleware
- Route-to-permission map
- Principal extraction from auth token
- Startup route mapping validation
- 403 response with required permission in error body

### Phase 3: Handler-Level Checks

- `canAccessAgent()`, `canAccessMission()` helper functions
- Resource-scoped checks in handlers that need them
- Operator bypass (operators access everything)

### Phase 4: Suspension and Coverage

- Suspension flow with parent validation
- Revocation flow with agent halt
- Fail-closed behavior for orphaned governance
- CLI: `agency registry update <name> --status suspended`

### Phase 5: Defaults and Integration

- Default permission sets applied at registration time
- `effective:` display in CLI output
- Snapshot includes effective permissions for container consumption
- Full test suite validation

---

## Testing

### Phase 1 Tests

- Wildcard matching: `*` covers everything, `knowledge.*` covers `knowledge.read`
- Ceiling inheritance: own ∩ parent ceiling
- Empty permissions inherit parent's full set
- Multi-level hierarchy (operator → team → agent)
- Permits rejects permissions outside ceiling
- Cache invalidation on registry update

### Phase 2 Tests

- Middleware allows request with correct permission
- Middleware denies request without permission (403)
- Middleware denies suspended principal (403)
- Unmatched route defaults to admin.*
- Startup validation warns for unmapped routes

### Phase 3 Tests

- Operator can access any agent
- Agent can access itself
- Agent can access team members
- Agent cannot access agents outside its team

### Phase 4 Tests

- Suspend with parent → authority transfers
- Suspend without parent → rejected (unless --force)
- Suspend without parent + --force → agents fail-closed
- Revoke → agents halted immediately
- Revoked principal cannot authenticate

### Phase 5 Tests

- New operator gets `*`
- New team gets default set
- New agent gets `knowledge.read+write`
- New agent in team gets empty (inherits)
- CLI shows effective permissions
- Snapshot includes effective permissions
