# Principal ACL Model — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Activate permission enforcement on the platform principal registry — hierarchical scope inheritance with ceiling model, wildcard matching, route middleware, handler-level checks, suspension coverage.

**Architecture:** New `internal/registry/permissions.go` for permission resolution engine. New `internal/api/middleware_permissions.go` for route-level enforcement. Existing `middleware_auth.go` extended to map tokens to principals. Handler helpers for resource-scoped checks. Suspension/revocation flow in registry.

**Tech Stack:** Go (gateway), chi middleware, SQLite (registry)

**Spec:** `docs/specs/principal-acl-model.md`

---

## File Structure

### Files to Create

| File | Purpose |
|------|---------|
| `internal/registry/permissions.go` | EffectivePermissions(), Permits(), wildcard matching, cache |
| `internal/registry/permissions_test.go` | Unit tests for permission resolution |
| `internal/api/middleware_permissions.go` | Chi middleware for route-level permission enforcement |

### Files to Modify

| File | Changes |
|------|---------|
| `internal/registry/registry.go` | Add per-principal token generation, token→principal lookup, suspension checks |
| `internal/registry/registry_test.go` | Tests for token management, suspension |
| `internal/api/middleware_auth.go` | Extend to resolve token → principal, set principal in request context |
| `internal/api/routes.go` | Insert permission middleware, build route→permission map |
| `internal/api/handlers_registry.go` | Add suspension/revocation flow with parent validation |
| `internal/api/handlers_hub.go` | Add resource-scoped canAccessAgent() checks |
| `internal/cli/commands.go` | Update registry show to display effective permissions |

---

## Task 1: Permission Resolution Engine

**Files:**
- Create: `internal/registry/permissions.go`
- Test: `internal/registry/permissions_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/registry/permissions_test.go
package registry

import (
	"testing"
)

func TestPermitsExactMatch(t *testing.T) {
	if !Permits([]string{"agent.read"}, "agent.read") {
		t.Fatal("exact match should permit")
	}
}

func TestPermitsSuperuser(t *testing.T) {
	if !Permits([]string{"*"}, "agent.read") {
		t.Fatal("* should permit anything")
	}
}

func TestPermitsWildcard(t *testing.T) {
	if !Permits([]string{"knowledge.*"}, "knowledge.read") {
		t.Fatal("knowledge.* should permit knowledge.read")
	}
}

func TestPermitsWildcardNoPartialMatch(t *testing.T) {
	if Permits([]string{"knowledge.*"}, "agent.read") {
		t.Fatal("knowledge.* should not permit agent.read")
	}
}

func TestPermitsDeniesUnmatched(t *testing.T) {
	if Permits([]string{"agent.read"}, "agent.write") {
		t.Fatal("agent.read should not permit agent.write")
	}
}

func TestPermitsEmptyPerms(t *testing.T) {
	if Permits([]string{}, "agent.read") {
		t.Fatal("empty perms should deny everything")
	}
}

func TestEffectiveNoParent(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	uuid, _ := reg.Register("operator", "admin", WithPermissions([]string{"*"}))
	eff, _ := reg.EffectivePermissions(uuid)
	if !Permits(eff, "agent.read") {
		t.Fatal("operator with * should have all permissions")
	}
}

func TestEffectiveInheritsFromParent(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	teamUUID, _ := reg.Register("team", "infra", WithPermissions([]string{"knowledge.*", "agent.read"}))
	agentUUID, _ := reg.Register("agent", "scout", WithParent(teamUUID))
	// Agent has empty permissions → inherits team's full set
	eff, _ := reg.EffectivePermissions(agentUUID)
	if !Permits(eff, "knowledge.read") {
		t.Fatal("should inherit knowledge.read from team")
	}
	if !Permits(eff, "agent.read") {
		t.Fatal("should inherit agent.read from team")
	}
}

func TestEffectiveCeilingModel(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	teamUUID, _ := reg.Register("team", "infra", WithPermissions([]string{"knowledge.read"}))
	agentUUID, _ := reg.Register("agent", "rogue", WithParent(teamUUID),
		WithPermissions([]string{"knowledge.read", "knowledge.write"}))
	eff, _ := reg.EffectivePermissions(agentUUID)
	if !Permits(eff, "knowledge.read") {
		t.Fatal("knowledge.read is within ceiling")
	}
	if Permits(eff, "knowledge.write") {
		t.Fatal("knowledge.write exceeds ceiling — should be blocked")
	}
}

func TestEffectiveMultiLevel(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	opUUID, _ := reg.Register("operator", "admin", WithPermissions([]string{"*"}))
	teamUUID, _ := reg.Register("team", "sec", WithParent(opUUID),
		WithPermissions([]string{"knowledge.*", "agent.read"}))
	agentUUID, _ := reg.Register("agent", "scanner", WithParent(teamUUID))
	eff, _ := reg.EffectivePermissions(agentUUID)
	if !Permits(eff, "knowledge.write") {
		t.Fatal("should have knowledge.write via team ceiling under operator *")
	}
	if Permits(eff, "admin.*") {
		t.Fatal("should NOT have admin.* — team doesn't have it")
	}
}

func TestEffectiveSuspendedDeniesAll(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	uuid, _ := reg.Register("agent", "bad", WithPermissions([]string{"knowledge.read"}))
	reg.Update(uuid, map[string]interface{}{"status": "suspended"})
	eff, _ := reg.EffectivePermissions(uuid)
	if len(eff) != 0 {
		t.Fatalf("suspended principal should have no effective permissions, got %v", eff)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/geoff/agency-workspace/agency && go test ./internal/registry/ -run "TestPermits|TestEffective" -v`

- [ ] **Step 3: Implement permissions.go**

```go
// internal/registry/permissions.go
package registry

import (
	"strings"
)

// Permits checks if a permission set covers the required permission.
func Permits(perms []string, required string) bool {
	for _, p := range perms {
		if p == "*" {
			return true
		}
		if p == required {
			return true
		}
		if strings.HasSuffix(p, ".*") {
			prefix := strings.TrimSuffix(p, ".*")
			if strings.HasPrefix(required, prefix+".") {
				return true
			}
		}
	}
	return false
}

// EffectivePermissions computes the resolved permissions for a principal,
// applying the ceiling model through the hierarchy.
func (r *Registry) EffectivePermissions(uuid string) ([]string, error) {
	p, err := r.Resolve(uuid)
	if err != nil {
		return nil, err
	}

	// Suspended/revoked principals have no permissions
	if p.Status == "suspended" || p.Status == "revoked" {
		return []string{}, nil
	}

	// No parent — own permissions are effective
	if p.Parent == "" {
		return p.Permissions, nil
	}

	// Get parent's effective permissions (recursive)
	ceiling, err := r.EffectivePermissions(p.Parent)
	if err != nil {
		// Parent not found — use own permissions (orphaned)
		return p.Permissions, nil
	}

	// Empty own permissions — inherit parent's full set
	if len(p.Permissions) == 0 {
		return ceiling, nil
	}

	// Ceiling model: own ∩ ceiling
	var effective []string
	for _, perm := range p.Permissions {
		if Permits(ceiling, perm) {
			effective = append(effective, perm)
		}
	}
	return effective, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/geoff/agency-workspace/agency && go test ./internal/registry/ -v`

- [ ] **Step 5: Commit**

```bash
git add internal/registry/permissions.go internal/registry/permissions_test.go
git commit -m "feat(registry): add permission resolution engine with ceiling model"
```

---

## Task 2: Token-to-Principal Mapping

**Files:**
- Modify: `internal/registry/registry.go`
- Modify: `internal/registry/registry_test.go`

Currently the gateway has one token for all requests. We need per-principal tokens so the middleware knows WHO is requesting.

- [ ] **Step 1: Write failing tests**

Append to `internal/registry/registry_test.go`:

```go
func TestGenerateToken(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	uuid, _ := reg.Register("operator", "admin")
	token, err := reg.GenerateToken(uuid)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 32 {
		t.Fatalf("token too short: %d chars", len(token))
	}
}

func TestResolveToken(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	uuid, _ := reg.Register("operator", "admin")
	token, _ := reg.GenerateToken(uuid)

	p, err := reg.ResolveToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if p.UUID != uuid {
		t.Fatalf("expected UUID %s, got %s", uuid, p.UUID)
	}
}

func TestResolveTokenNotFound(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	_, err := reg.ResolveToken("nonexistent-token")
	if err == nil {
		t.Fatal("expected error for unknown token")
	}
}

func TestGatewayTokenResolvesToOperator(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	reg.Register("operator", "admin", WithPermissions([]string{"*"}))
	reg.SetGatewayToken("the-gateway-token")

	p, err := reg.ResolveToken("the-gateway-token")
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != "operator" {
		t.Fatalf("gateway token should resolve to operator, got %s", p.Type)
	}
}
```

- [ ] **Step 2: Implement token management**

Add a `tokens` table to the registry and methods:

```go
// In registry.go init():
_, err = r.db.Exec(`
    CREATE TABLE IF NOT EXISTS tokens (
        token TEXT PRIMARY KEY,
        principal_uuid TEXT NOT NULL REFERENCES principals(uuid),
        created_at TEXT NOT NULL
    );
`)

// Token generation
func (r *Registry) GenerateToken(principalUUID string) (string, error) {
    // Verify principal exists
    _, err := r.Resolve(principalUUID)
    if err != nil {
        return "", err
    }
    token := generateSecureToken()  // crypto/rand based
    now := time.Now().UTC().Format(time.RFC3339)
    _, err = r.db.Exec("INSERT INTO tokens (token, principal_uuid, created_at) VALUES (?, ?, ?)",
        token, principalUUID, now)
    return token, err
}

// Token resolution
func (r *Registry) ResolveToken(token string) (*Principal, error) {
    var uuid string
    err := r.db.QueryRow("SELECT principal_uuid FROM tokens WHERE token = ?", token).Scan(&uuid)
    if err != nil {
        return nil, fmt.Errorf("unknown token")
    }
    return r.Resolve(uuid)
}

// Gateway token mapping — the existing gateway token maps to the first operator
func (r *Registry) SetGatewayToken(token string) {
    r.gatewayToken = token
}

// In ResolveToken, check gateway token first:
if r.gatewayToken != "" && token == r.gatewayToken {
    // Find first active operator
    ops, _ := r.List("operator")
    for _, op := range ops {
        if op.Status == "active" {
            return &op, nil
        }
    }
}
```

- [ ] **Step 3: Run tests, commit**

---

## Task 3: Auth Middleware — Principal Context

**Files:**
- Modify: `internal/api/middleware_auth.go`

- [ ] **Step 1: Extend BearerAuth to set principal in context**

After token validation succeeds, resolve token to principal and store in request context:

```go
// Add context key
type contextKey string
const principalKey contextKey = "principal"

// In BearerAuth middleware, after token validation:
if infra != nil && infra.Registry != nil {
    p, err := infra.Registry.ResolveToken(token)
    if err == nil {
        ctx = context.WithValue(ctx, principalKey, p)
    }
}

// Helper for handlers
func getPrincipal(r *http.Request) *registry.Principal {
    p, _ := r.Context().Value(principalKey).(*registry.Principal)
    return p
}
```

This is backward-compatible — if token resolution fails (e.g., old-style gateway token before principals are set up), principal is nil and existing behavior continues.

- [ ] **Step 2: Commit**

---

## Task 4: Permission Middleware

**Files:**
- Create: `internal/api/middleware_permissions.go`
- Modify: `internal/api/routes.go`

- [ ] **Step 1: Implement permission middleware**

```go
// internal/api/middleware_permissions.go
package api

import (
    "net/http"
    "strings"

    "github.com/geoffbelknap/agency/internal/registry"
)

// routePermissions maps method+path patterns to required permissions.
var routePermissions = map[string]string{
    "POST /agents":              "agent.write",
    "GET /agents":               "agent.read",
    // ... full map from spec ...
}

// PermissionMiddleware checks the requesting principal's permissions
// against the required permission for the route.
func PermissionMiddleware(reg *registry.Registry) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            principal := getPrincipal(r)
            if principal == nil {
                // No principal resolved — legacy mode, allow
                // (backward compat until all tokens are principal-mapped)
                next.ServeHTTP(w, r)
                return
            }

            // Check suspension
            if principal.Status != "active" {
                http.Error(w, `{"error":"principal suspended"}`, http.StatusForbidden)
                return
            }

            // Find required permission
            required := matchRoutePermission(r.Method, r.URL.Path)
            if required == "" {
                // Unmatched route — require admin.*
                required = "admin.*"
            }

            // Check effective permissions
            eff, err := reg.EffectivePermissions(principal.UUID)
            if err != nil {
                http.Error(w, `{"error":"permission resolution failed"}`, http.StatusInternalServerError)
                return
            }

            if !registry.Permits(eff, required) {
                http.Error(w, `{"error":"permission denied","required":"`+required+`"}`, http.StatusForbidden)
                return
            }

            next.ServeHTTP(w, r)
        })
    }
}

func matchRoutePermission(method, path string) string {
    // Strip /api/v1 prefix
    path = strings.TrimPrefix(path, "/api/v1")

    // Try exact match first
    key := method + " " + path
    if p, ok := routePermissions[key]; ok {
        return p
    }

    // Try wildcard paths (strip last segment, try with /*)
    for {
        idx := strings.LastIndex(path, "/")
        if idx <= 0 {
            break
        }
        path = path[:idx]
        key = method + " " + path + "/*"
        if p, ok := routePermissions[key]; ok {
            return p
        }
    }

    return "" // unmatched
}
```

- [ ] **Step 2: Wire into routes.go**

In `RegisterRoutesWithOptions`, after BearerAuth:

```go
if infra != nil && infra.Registry != nil {
    r.Use(PermissionMiddleware(infra.Registry))
}
```

- [ ] **Step 3: Add startup route validation**

Log warnings for routes without permission mappings:

```go
func validateRoutePermissions(router chi.Router) {
    chi.Walk(router, func(method, route string, ...) error {
        cleaned := strings.TrimPrefix(route, "/api/v1")
        if matchRoutePermission(method, "/api/v1"+cleaned) == "" {
            log.Printf("WARNING: no permission mapping for %s %s (defaults to admin.*)", method, route)
        }
        return nil
    })
}
```

- [ ] **Step 4: Build and verify**

Run: `cd /home/geoff/agency-workspace/agency && go build ./cmd/gateway/`

- [ ] **Step 5: Commit**

---

## Task 5: Handler-Level Resource Checks

**Files:**
- Modify: `internal/api/handlers_hub.go` (or relevant handler files)

- [ ] **Step 1: Add canAccessAgent helper**

```go
func (h *handler) canAccessAgent(principal *registry.Principal, agentName string) bool {
    if principal == nil {
        return true // legacy mode
    }
    if principal.Type == "operator" {
        return true
    }
    if principal.Type == "agent" && principal.Name == agentName {
        return true // can access self
    }
    if principal.Parent != "" {
        agent, err := h.infra.Registry.ResolveByName("agent", agentName)
        if err == nil && agent.Parent == principal.Parent {
            return true // same team
        }
    }
    return false
}
```

- [ ] **Step 2: Add checks to agent-specific handlers**

In handlers that operate on specific agents (show, start, stop, halt, send), add:

```go
principal := getPrincipal(r)
if !h.canAccessAgent(principal, agentName) {
    http.Error(w, `{"error":"access denied to this agent"}`, http.StatusForbidden)
    return
}
```

- [ ] **Step 3: Build and verify**

- [ ] **Step 4: Commit**

---

## Task 6: Suspension and Revocation Flow

**Files:**
- Modify: `internal/api/handlers_registry.go`
- Modify: `internal/registry/registry.go`

- [ ] **Step 1: Add suspension validation**

In `registryUpdate` handler, when status is being changed to "suspended":

```go
if newStatus == "suspended" || newStatus == "revoked" {
    // Verify parent exists and is active
    p, _ := h.infra.Registry.Resolve(uuid)
    if p.Parent == "" {
        force := r.URL.Query().Get("force") == "true"
        if !force {
            http.Error(w, `{"error":"no coverage principal — use ?force=true to fail-closed"}`, http.StatusBadRequest)
            return
        }
    }
}
```

For revocation, add agent halt:

```go
if newStatus == "revoked" {
    // Find and halt all agents governed by this principal
    agents, _ := h.infra.Registry.List("agent")
    for _, a := range agents {
        if a.Parent == uuid {
            // Send halt signal
            h.haltAgent(a.Name)
        }
    }
}
```

- [ ] **Step 2: Add fail-closed check for orphaned governance**

In the permission middleware, after computing effective permissions, check if the agent's governance chain has any active principal:

```go
func (r *Registry) HasActiveGovernance(uuid string) bool {
    p, err := r.Resolve(uuid)
    if err != nil {
        return false
    }
    if p.Status == "active" && p.Type == "operator" {
        return true // operator is active governance
    }
    if p.Parent != "" {
        return r.HasActiveGovernance(p.Parent)
    }
    return false // no active governance found
}
```

- [ ] **Step 3: Commit**

---

## Task 7: Default Permissions on Registration

**Files:**
- Modify: `internal/registry/registry.go`
- Modify: `internal/orchestrate/agent.go`
- Modify: `internal/orchestrate/infra.go`

- [ ] **Step 1: Add default permissions**

In `Register()`, when no permissions are provided via options, apply defaults:

```go
var defaultPerms = map[string][]string{
    "operator": {"*"},
    "team":     {"agent.read", "knowledge.read", "knowledge.write", "mission.read"},
    "agent":    {"knowledge.read", "knowledge.write"},
    "channel":  {},
    "role":     {},
}

// In Register(), if no WithPermissions option:
if o.permissions == nil {
    o.permissions = defaultPerms[principalType]
}
```

- [ ] **Step 2: Update CLI show to display effective permissions**

In `registryCmd()` show subcommand, after displaying principal fields, compute and show effective:

```go
// After printing principal fields:
eff, _ := c.RegistryEffectivePermissions(uuid)
fmt.Printf("Effective: %v\n", eff)
```

This requires a new API endpoint or client method for effective permissions. Add:

```go
// In registry.go:
func (r *Registry) EffectivePermissionsJSON(uuid string) ([]byte, error) {
    eff, err := r.EffectivePermissions(uuid)
    if err != nil {
        return nil, err
    }
    return json.Marshal(map[string]interface{}{"uuid": uuid, "effective_permissions": eff})
}

// In handlers_registry.go:
func (h *handler) registryEffective(w http.ResponseWriter, r *http.Request) {
    uuid := chi.URLParam(r, "uuid")
    data, err := h.infra.Registry.EffectivePermissionsJSON(uuid)
    // ...
}

// In routes.go:
r.Get("/registry/{uuid}/effective", h.registryEffective)
```

- [ ] **Step 3: Run full test suite, commit**

---

## Task 8: Full Test Suite Validation

- [ ] **Step 1: Run registry Go tests**

```bash
go test ./internal/registry/ -v
```

- [ ] **Step 2: Build gateway**

```bash
go build ./cmd/gateway/
```

- [ ] **Step 3: Run full Go test suite**

```bash
go test ./... 2>&1 | tail -20
```

- [ ] **Step 4: Run Python tests for regressions**

```bash
python3 -m pytest images/tests/ -q --ignore=images/tests/test_realtime_comms_e2e.py --ignore=images/tests/test_comms_e2e.py 2>&1 | tail -5
```

- [ ] **Step 5: Commit any fixes, push**

---

## Summary

| Task | What it delivers |
|------|-----------------|
| **Task 1** | Permission resolution: `Permits()`, `EffectivePermissions()`, wildcard matching, ceiling model |
| **Task 2** | Token-to-principal mapping: per-principal tokens, gateway token → operator |
| **Task 3** | Auth middleware extended: resolves token to principal, sets in request context |
| **Task 4** | Permission middleware: route→permission map, default-deny for unmatched |
| **Task 5** | Handler-level checks: `canAccessAgent()`, resource-scoped authorization |
| **Task 6** | Suspension/revocation: parent validation, agent halt on revoke, fail-closed |
| **Task 7** | Default permissions: applied at registration, effective display in CLI |
| **Task 8** | Full validation |
