# Platform Identity & UUID Adoption — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a centralized principal registry in the gateway with UUID-based identity for all platform entities, with creation-time assignment and config-delivery distribution to containers.

**Architecture:** New `internal/registry/` Go package with SQLite backend at `~/.agency/registry.db`. Entity creation paths (agent, channel, operator, team) auto-register. Registry snapshot delivered to body runtime via enforcer config path, to other containers via gateway API. Knowledge service switches from own principal_registry table to reading the snapshot.

**Tech Stack:** Go (gateway, registry, CLI), Python (knowledge service snapshot reader), SQLite, cobra (CLI)

**Spec:** `docs/specs/platform-identity-uuid-adoption.md`

---

## File Structure

### Files to Create

| File | Purpose |
|------|---------|
| `internal/registry/registry.go` | Registry struct, SQLite operations, CRUD, Snapshot |
| `internal/registry/registry_test.go` | Unit tests for all registry operations |
| `internal/api/handlers_registry.go` | REST handlers for /api/v1/registry endpoints |

### Files to Modify

| File | Changes |
|------|---------|
| `internal/orchestrate/infra.go` | Init registry in EnsureRunning(), register system channels, write snapshot |
| `internal/orchestrate/agent.go` | Register agent in registry during Create(), store UUID in agent.yaml |
| `internal/api/routes.go` | Add /api/v1/registry routes |
| `internal/apiclient/client.go` | Add registry client methods |
| `internal/cli/commands.go` | Add `agency registry` subcommand, update `agent create` output |
| `images/knowledge/principal_registry.py` | Switch from own SQLite table to reading gateway snapshot |
| `images/knowledge/server.py` | Remove POST /principals, update GET to read snapshot |

---

## Task 1: Registry Go Package — Schema and CRUD

**Files:**
- Create: `internal/registry/registry.go`
- Test: `internal/registry/registry_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/registry/registry_test.go
package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func tempDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "registry.db")
}

func TestRegister(t *testing.T) {
	reg, err := Open(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	uuid, err := reg.Register("agent", "researcher")
	if err != nil {
		t.Fatal(err)
	}
	if len(uuid) != 36 {
		t.Fatalf("expected 36-char UUID, got %d chars: %s", len(uuid), uuid)
	}
}

func TestRegisterUnique(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	uuid1, _ := reg.Register("agent", "researcher")
	_, err := reg.Register("agent", "researcher")
	if err == nil {
		t.Fatal("expected error on duplicate (type, name)")
	}
	_ = uuid1
}

func TestRegisterDifferentTypesSameName(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	u1, _ := reg.Register("agent", "alpha")
	u2, _ := reg.Register("channel", "alpha")
	if u1 == u2 {
		t.Fatal("different types with same name should get different UUIDs")
	}
}

func TestResolve(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	uuid, _ := reg.Register("operator", "geoff")
	p, err := reg.Resolve(uuid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "geoff" || p.Type != "operator" || p.UUID != uuid {
		t.Fatalf("unexpected principal: %+v", p)
	}
	if p.Status != "active" {
		t.Fatalf("expected active status, got %s", p.Status)
	}
}

func TestResolveNotFound(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	_, err := reg.Resolve("nonexistent-uuid")
	if err == nil {
		t.Fatal("expected error for nonexistent UUID")
	}
}

func TestResolveByName(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	uuid, _ := reg.Register("agent", "scout")
	p, err := reg.ResolveByName("agent", "scout")
	if err != nil {
		t.Fatal(err)
	}
	if p.UUID != uuid {
		t.Fatalf("expected UUID %s, got %s", uuid, p.UUID)
	}
}

func TestList(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	reg.Register("agent", "a1")
	reg.Register("agent", "a2")
	reg.Register("operator", "op1")

	agents, _ := reg.List("agent")
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}
	ops, _ := reg.List("operator")
	if len(ops) != 1 {
		t.Fatalf("expected 1 operator, got %d", len(ops))
	}
}

func TestUpdate(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	uuid, _ := reg.Register("agent", "test")
	err := reg.Update(uuid, map[string]interface{}{
		"status": "suspended",
		"parent": "some-team-uuid",
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := reg.Resolve(uuid)
	if p.Status != "suspended" {
		t.Fatalf("expected suspended, got %s", p.Status)
	}
	if p.Parent != "some-team-uuid" {
		t.Fatalf("expected parent some-team-uuid, got %s", p.Parent)
	}
}

func TestUpdatePermissions(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	uuid, _ := reg.Register("operator", "admin")
	perms := []string{"knowledge.read", "knowledge.write", "agent.halt"}
	permsJSON, _ := json.Marshal(perms)
	err := reg.Update(uuid, map[string]interface{}{
		"permissions": string(permsJSON),
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := reg.Resolve(uuid)
	if len(p.Permissions) != 3 {
		t.Fatalf("expected 3 permissions, got %d", len(p.Permissions))
	}
}

func TestDelete(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	uuid, _ := reg.Register("agent", "temp")
	err := reg.Delete(uuid)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Resolve(uuid)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestSnapshot(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	reg.Register("agent", "a1")
	reg.Register("operator", "op1")

	data, err := reg.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	var snap RegistrySnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Version != 1 {
		t.Fatalf("expected version 1, got %d", snap.Version)
	}
	if len(snap.Principals) != 2 {
		t.Fatalf("expected 2 principals, got %d", len(snap.Principals))
	}
}

func TestRegisterWithOptions(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	teamUUID, _ := reg.Register("team", "infra")
	agentUUID, _ := reg.Register("agent", "scout",
		WithParent(teamUUID),
		WithMetadata(map[string]interface{}{"preset": "security"}),
	)

	p, _ := reg.Resolve(agentUUID)
	if p.Parent != teamUUID {
		t.Fatalf("expected parent %s, got %s", teamUUID, p.Parent)
	}
}

func TestResolveByNameOrUUID(t *testing.T) {
	reg, _ := Open(tempDB(t))
	defer reg.Close()

	uuid, _ := reg.Register("agent", "test-agent")

	// Resolve by UUID
	p1, _ := reg.ResolveAny("agent", uuid)
	if p1.UUID != uuid {
		t.Fatal("resolve by UUID failed")
	}

	// Resolve by name
	p2, _ := reg.ResolveAny("agent", "test-agent")
	if p2.UUID != uuid {
		t.Fatal("resolve by name failed")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/geoff/agency-workspace/agency && go test ./internal/registry/ -v`

- [ ] **Step 3: Implement registry.go**

```go
// internal/registry/registry.go
package registry

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// Principal represents a registered entity in the platform.
type Principal struct {
	UUID        string   `json:"uuid" yaml:"uuid"`
	Type        string   `json:"type" yaml:"type"`
	Name        string   `json:"name" yaml:"name"`
	Parent      string   `json:"parent,omitempty" yaml:"parent,omitempty"`
	Status      string   `json:"status" yaml:"status"`
	Permissions []string `json:"permissions" yaml:"permissions"`
	CreatedAt   string   `json:"created_at" yaml:"created_at"`
	Metadata    json.RawMessage `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// RegistrySnapshot is the JSON format delivered to containers.
type RegistrySnapshot struct {
	Version     int         `json:"version"`
	GeneratedAt string      `json:"generated_at"`
	Principals  []Principal `json:"principals"`
}

// Registry manages UUID-based principal identity.
type Registry struct {
	db *sql.DB
}

// Open opens or creates the registry database.
func Open(path string) (*Registry, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open registry: %w", err)
	}
	r := &Registry{db: db}
	if err := r.init(); err != nil {
		db.Close()
		return nil, err
	}
	return r, nil
}

func (r *Registry) init() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS principals (
			uuid TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			name TEXT NOT NULL,
			parent TEXT DEFAULT '',
			status TEXT DEFAULT 'active',
			permissions TEXT DEFAULT '[]',
			created_at TEXT NOT NULL,
			metadata TEXT DEFAULT '{}',
			UNIQUE(type, name)
		);
		CREATE INDEX IF NOT EXISTS idx_principals_type ON principals(type);
		CREATE INDEX IF NOT EXISTS idx_principals_parent ON principals(parent);
		CREATE INDEX IF NOT EXISTS idx_principals_status ON principals(status);
	`)
	return err
}

// Close closes the database connection.
func (r *Registry) Close() error {
	return r.db.Close()
}

// Option configures a Register call.
type Option func(*registerOpts)

type registerOpts struct {
	parent      string
	metadata    map[string]interface{}
	permissions []string
}

func WithParent(uuid string) Option {
	return func(o *registerOpts) { o.parent = uuid }
}

func WithMetadata(m map[string]interface{}) Option {
	return func(o *registerOpts) { o.metadata = m }
}

func WithPermissions(perms []string) Option {
	return func(o *registerOpts) { o.permissions = perms }
}

// Register creates a new principal. Returns the assigned UUID.
func (r *Registry) Register(principalType, name string, opts ...Option) (string, error) {
	o := &registerOpts{}
	for _, fn := range opts {
		fn(o)
	}

	id := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)

	metaJSON := []byte("{}")
	if o.metadata != nil {
		var err error
		metaJSON, err = json.Marshal(o.metadata)
		if err != nil {
			return "", fmt.Errorf("marshal metadata: %w", err)
		}
	}

	permsJSON := []byte("[]")
	if o.permissions != nil {
		var err error
		permsJSON, err = json.Marshal(o.permissions)
		if err != nil {
			return "", fmt.Errorf("marshal permissions: %w", err)
		}
	}

	_, err := r.db.Exec(
		`INSERT INTO principals (uuid, type, name, parent, status, permissions, created_at, metadata)
		 VALUES (?, ?, ?, ?, 'active', ?, ?, ?)`,
		id, principalType, name, o.parent, string(permsJSON), now, string(metaJSON),
	)
	if err != nil {
		return "", fmt.Errorf("register %s/%s: %w", principalType, name, err)
	}
	return id, nil
}

// Resolve looks up a principal by UUID.
func (r *Registry) Resolve(uuid string) (*Principal, error) {
	return r.scanOne(
		`SELECT uuid, type, name, parent, status, permissions, created_at, metadata
		 FROM principals WHERE uuid = ?`, uuid,
	)
}

// ResolveByName looks up a principal by type and name.
func (r *Registry) ResolveByName(principalType, name string) (*Principal, error) {
	return r.scanOne(
		`SELECT uuid, type, name, parent, status, permissions, created_at, metadata
		 FROM principals WHERE type = ? AND name = ?`, principalType, name,
	)
}

// ResolveAny resolves by UUID first, falls back to name.
func (r *Registry) ResolveAny(principalType, nameOrUUID string) (*Principal, error) {
	// Try UUID first (36 chars with 4 dashes)
	if len(nameOrUUID) == 36 {
		p, err := r.Resolve(nameOrUUID)
		if err == nil {
			return p, nil
		}
	}
	return r.ResolveByName(principalType, nameOrUUID)
}

// List returns all principals of a given type. Empty type returns all.
func (r *Registry) List(principalType string) ([]Principal, error) {
	query := `SELECT uuid, type, name, parent, status, permissions, created_at, metadata FROM principals`
	var args []interface{}
	if principalType != "" {
		query += " WHERE type = ?"
		args = append(args, principalType)
	}
	query += " ORDER BY type, name"

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Principal
	for rows.Next() {
		p, err := scanPrincipal(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *p)
	}
	return result, rows.Err()
}

// Update modifies fields on an existing principal.
func (r *Registry) Update(uuid string, fields map[string]interface{}) error {
	allowed := map[string]bool{"parent": true, "status": true, "permissions": true, "metadata": true}
	for k, v := range fields {
		if !allowed[k] {
			return fmt.Errorf("cannot update field %q", k)
		}
		_, err := r.db.Exec(
			fmt.Sprintf("UPDATE principals SET %s = ? WHERE uuid = ?", k),
			v, uuid,
		)
		if err != nil {
			return fmt.Errorf("update %s: %w", k, err)
		}
	}
	return nil
}

// Delete removes a principal.
func (r *Registry) Delete(uuid string) error {
	res, err := r.db.Exec("DELETE FROM principals WHERE uuid = ?", uuid)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("principal %s not found", uuid)
	}
	return nil
}

// Snapshot returns the full registry as JSON.
func (r *Registry) Snapshot() ([]byte, error) {
	all, err := r.List("")
	if err != nil {
		return nil, err
	}
	snap := RegistrySnapshot{
		Version:     1,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Principals:  all,
	}
	return json.MarshalIndent(snap, "", "  ")
}

func (r *Registry) scanOne(query string, args ...interface{}) (*Principal, error) {
	row := r.db.QueryRow(query, args...)
	p, err := scanPrincipalRow(row)
	if err != nil {
		return nil, err
	}
	return p, nil
}

type scannable interface {
	Scan(dest ...interface{}) error
}

func scanPrincipal(s scannable) (*Principal, error) {
	return scanPrincipalRow(s)
}

func scanPrincipalRow(s scannable) (*Principal, error) {
	var p Principal
	var permsJSON, metaJSON string
	err := s.Scan(&p.UUID, &p.Type, &p.Name, &p.Parent, &p.Status,
		&permsJSON, &p.CreatedAt, &metaJSON)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(permsJSON), &p.Permissions)
	if p.Permissions == nil {
		p.Permissions = []string{}
	}
	p.Metadata = json.RawMessage(metaJSON)
	return &p, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/geoff/agency-workspace/agency && go test ./internal/registry/ -v`

- [ ] **Step 5: Commit**

```bash
git add internal/registry/
git commit -m "feat(registry): add platform principal registry with UUID-based identity"
```

---

## Task 2: Gateway Integration — Init Registry on Startup

**Files:**
- Modify: `internal/orchestrate/infra.go`
- Modify: `cmd/gateway/main.go` (or wherever Infra is initialized)

- [ ] **Step 1: Add Registry field to Infra struct**

In `internal/orchestrate/infra.go`, add to the `Infra` struct:

```go
Registry *registry.Registry
```

Import the registry package.

- [ ] **Step 2: Initialize registry in NewInfra() or EnsureRunning()**

In `NewInfra()` or early in `EnsureRunning()`:

```go
reg, err := registry.Open(filepath.Join(home, "registry.db"))
if err != nil {
    return nil, fmt.Errorf("open registry: %w", err)
}
inf.Registry = reg
```

- [ ] **Step 3: Register system channels during ensureSystemChannels()**

After creating each system channel, register it:

```go
func (inf *Infra) ensureSystemChannels(ctx context.Context) error {
    channels := []struct{ name, topic string }{
        {"_operator", "Operator commands and alerts"},
        {"_knowledge-updates", "Knowledge graph updates"},
        {"operator", "Operator channel"},
        {"general", "General channel"},
    }
    for _, ch := range channels {
        // Existing: create channel via comms
        // New: register in registry (ignore error if already exists)
        inf.Registry.Register("channel", ch.name)
        // ...
    }
}
```

- [ ] **Step 4: Write snapshot file after mutations**

Add a helper to write the snapshot:

```go
func (inf *Infra) writeRegistrySnapshot() error {
    data, err := inf.Registry.Snapshot()
    if err != nil {
        return err
    }
    snapPath := filepath.Join(inf.Home, "registry.json")
    return os.WriteFile(snapPath, data, 0644)
}
```

Call after EnsureRunning() completes and after any registration.

- [ ] **Step 5: Build and verify**

Run: `cd /home/geoff/agency-workspace/agency && go build ./cmd/gateway/`

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrate/infra.go cmd/gateway/main.go
git commit -m "feat(registry): init registry on gateway startup, register system channels"
```

---

## Task 3: Agent Create — Register and Store UUID

**Files:**
- Modify: `internal/orchestrate/agent.go`

- [ ] **Step 1: Add registry call in Create()**

In `AgentManager.Create()`, after validating the agent name and before writing agent.yaml:

```go
// Register agent in principal registry
agentUUID := ""
if am.infra != nil && am.infra.Registry != nil {
    var err error
    agentUUID, err = am.infra.Registry.Register("agent", name)
    if err != nil {
        return fmt.Errorf("register agent: %w", err)
    }
    // Write updated snapshot
    am.infra.writeRegistrySnapshot()
}
```

- [ ] **Step 2: Add UUID to agent.yaml**

In the agent.yaml map being written:

```go
agentYAML := map[string]interface{}{
    "version":      "0.1",
    "name":         name,
    "uuid":         agentUUID,  // NEW
    "type":         agentType,
    "preset":       preset,
    "lifecycle_id": uuid.New().String(),
    // ... rest unchanged
}
```

- [ ] **Step 3: Add UUID to container labels**

When starting the workspace container, add the label:

```go
labels["agency.principal.uuid"] = agentUUID
```

Find where container labels are set (in the Start or ensureWorkspace method) and add this alongside the existing `agency.managed=true` label.

- [ ] **Step 4: Build and test**

Run: `cd /home/geoff/agency-workspace/agency && go build ./cmd/gateway/`

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrate/agent.go
git commit -m "feat(registry): register agent UUID on create, store in agent.yaml and container labels"
```

---

## Task 4: REST API Endpoints

**Files:**
- Create: `internal/api/handlers_registry.go`
- Modify: `internal/api/routes.go`
- Modify: `internal/apiclient/client.go`

- [ ] **Step 1: Create handlers**

```go
// internal/api/handlers_registry.go
package api

import (
    "encoding/json"
    "net/http"

    "github.com/go-chi/chi/v5"
)

func (h *handler) registryList(w http.ResponseWriter, r *http.Request) {
    ptype := r.URL.Query().Get("type")
    principals, err := h.infra.Registry.List(ptype)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{"principals": principals})
}

func (h *handler) registryResolve(w http.ResponseWriter, r *http.Request) {
    uuid := r.URL.Query().Get("uuid")
    ptype := r.URL.Query().Get("type")
    name := r.URL.Query().Get("name")

    var p *registry.Principal
    var err error
    if uuid != "" {
        p, err = h.infra.Registry.Resolve(uuid)
    } else if ptype != "" && name != "" {
        p, err = h.infra.Registry.ResolveByName(ptype, name)
    } else {
        http.Error(w, "uuid or (type + name) required", http.StatusBadRequest)
        return
    }
    if err != nil {
        http.Error(w, err.Error(), http.StatusNotFound)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(p)
}

func (h *handler) registrySnapshot(w http.ResponseWriter, r *http.Request) {
    data, err := h.infra.Registry.Snapshot()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.Write(data)
}

func (h *handler) registryRegister(w http.ResponseWriter, r *http.Request) {
    var body struct {
        Type     string                 `json:"type"`
        Name     string                 `json:"name"`
        Parent   string                 `json:"parent,omitempty"`
        Metadata map[string]interface{} `json:"metadata,omitempty"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        http.Error(w, "invalid JSON", http.StatusBadRequest)
        return
    }
    if body.Type == "" || body.Name == "" {
        http.Error(w, "type and name required", http.StatusBadRequest)
        return
    }

    var opts []registry.Option
    if body.Parent != "" {
        opts = append(opts, registry.WithParent(body.Parent))
    }
    if body.Metadata != nil {
        opts = append(opts, registry.WithMetadata(body.Metadata))
    }

    uuid, err := h.infra.Registry.Register(body.Type, body.Name, opts...)
    if err != nil {
        http.Error(w, err.Error(), http.StatusConflict)
        return
    }
    h.infra.writeRegistrySnapshot()
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"uuid": uuid, "type": body.Type, "name": body.Name})
}

func (h *handler) registryUpdate(w http.ResponseWriter, r *http.Request) {
    uuid := chi.URLParam(r, "uuid")
    var fields map[string]interface{}
    if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
        http.Error(w, "invalid JSON", http.StatusBadRequest)
        return
    }
    if err := h.infra.Registry.Update(uuid, fields); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    h.infra.writeRegistrySnapshot()
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{"updated": uuid})
}

func (h *handler) registryDelete(w http.ResponseWriter, r *http.Request) {
    uuid := chi.URLParam(r, "uuid")
    if err := h.infra.Registry.Delete(uuid); err != nil {
        http.Error(w, err.Error(), http.StatusNotFound)
        return
    }
    h.infra.writeRegistrySnapshot()
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{"deleted": uuid})
}
```

- [ ] **Step 2: Add routes**

In `internal/api/routes.go`, add a new route group:

```go
// Registry (identity)
r.Get("/registry", h.registrySnapshot)
r.Get("/registry/resolve", h.registryResolve)
r.Post("/registry", h.registryRegister)
r.Put("/registry/{uuid}", h.registryUpdate)
r.Delete("/registry/{uuid}", h.registryDelete)
```

- [ ] **Step 3: Add API client methods**

In `internal/apiclient/client.go`:

```go
func (c *Client) RegistryList(principalType string) ([]byte, error) {
    path := "/api/v1/registry"
    if principalType != "" {
        path += "?type=" + url.QueryEscape(principalType)
    }
    return c.get(path)
}

func (c *Client) RegistryResolve(nameOrUUID, principalType string) ([]byte, error) {
    if len(nameOrUUID) == 36 {
        return c.get("/api/v1/registry/resolve?uuid=" + url.QueryEscape(nameOrUUID))
    }
    return c.get("/api/v1/registry/resolve?type=" + url.QueryEscape(principalType) + "&name=" + url.QueryEscape(nameOrUUID))
}

func (c *Client) RegistryRegister(principalType, name string) ([]byte, error) {
    body := map[string]string{"type": principalType, "name": name}
    b, _ := json.Marshal(body)
    return c.post("/api/v1/registry", b)
}

func (c *Client) RegistryUpdate(uuid string, fields map[string]interface{}) ([]byte, error) {
    b, _ := json.Marshal(fields)
    return c.put("/api/v1/registry/"+url.PathEscape(uuid), b)
}

func (c *Client) RegistryDelete(uuid string) ([]byte, error) {
    return c.delete("/api/v1/registry/" + url.PathEscape(uuid))
}
```

- [ ] **Step 4: Build and verify**

Run: `cd /home/geoff/agency-workspace/agency && go build ./cmd/gateway/`

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers_registry.go internal/api/routes.go internal/apiclient/client.go
git commit -m "feat(registry): add REST API endpoints for principal registry"
```

---

## Task 5: CLI Commands

**Files:**
- Modify: `internal/cli/commands.go`

- [ ] **Step 1: Add registry subcommand**

```go
func registryCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "registry",
        Short: "Platform principal registry",
    }

    listCmd := &cobra.Command{
        Use:   "list",
        Short: "List registered principals",
        RunE: func(cmd *cobra.Command, args []string) error {
            c := clientFromContext(cmd)
            ptype, _ := cmd.Flags().GetString("type")
            data, err := c.RegistryList(ptype)
            if err != nil {
                return err
            }
            return printOutput(cmd, data)
        },
    }
    listCmd.Flags().String("type", "", "Filter by type (agent|operator|team|role|channel)")

    showCmd := &cobra.Command{
        Use:   "show <name-or-uuid>",
        Short: "Show principal details",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            c := clientFromContext(cmd)
            ptype, _ := cmd.Flags().GetString("type")
            if ptype == "" {
                ptype = "agent" // default
            }
            data, err := c.RegistryResolve(args[0], ptype)
            if err != nil {
                return err
            }
            return printOutput(cmd, data)
        },
    }
    showCmd.Flags().String("type", "", "Principal type (for name resolution)")

    updateCmd := &cobra.Command{
        Use:   "update <name-or-uuid>",
        Short: "Update principal fields",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            c := clientFromContext(cmd)
            fields := make(map[string]interface{})
            if v, _ := cmd.Flags().GetString("parent"); v != "" {
                fields["parent"] = v
            }
            if v, _ := cmd.Flags().GetString("status"); v != "" {
                fields["status"] = v
            }
            if len(fields) == 0 {
                return fmt.Errorf("no fields to update (use --parent or --status)")
            }
            // Resolve name to UUID first if needed
            ptype, _ := cmd.Flags().GetString("type")
            if ptype == "" {
                ptype = "agent"
            }
            resolved, err := c.RegistryResolve(args[0], ptype)
            if err != nil {
                return err
            }
            var p struct{ UUID string `json:"uuid"` }
            json.Unmarshal(resolved, &p)
            data, err := c.RegistryUpdate(p.UUID, fields)
            if err != nil {
                return err
            }
            return printOutput(cmd, data)
        },
    }
    updateCmd.Flags().String("parent", "", "Parent principal UUID")
    updateCmd.Flags().String("status", "", "Status (active|suspended|revoked)")
    updateCmd.Flags().String("type", "", "Principal type (for name resolution)")

    deleteCmd := &cobra.Command{
        Use:   "delete <name-or-uuid>",
        Short: "Delete a principal",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            c := clientFromContext(cmd)
            ptype, _ := cmd.Flags().GetString("type")
            if ptype == "" {
                ptype = "agent"
            }
            resolved, err := c.RegistryResolve(args[0], ptype)
            if err != nil {
                return err
            }
            var p struct{ UUID string `json:"uuid"` }
            json.Unmarshal(resolved, &p)
            data, err := c.RegistryDelete(p.UUID)
            if err != nil {
                return err
            }
            return printOutput(cmd, data)
        },
    }
    deleteCmd.Flags().String("type", "", "Principal type (for name resolution)")

    cmd.AddCommand(listCmd, showCmd, updateCmd, deleteCmd)
    return cmd
}
```

Register in `RegisterCommands()`:

```go
rootCmd.AddCommand(registryCmd())
```

- [ ] **Step 2: Update agent create to print UUID**

In `createCmd()`, after successful creation, print the UUID:

```go
fmt.Fprintf(cmd.OutOrStdout(), "Agent %s created (UUID: %s)\n", args[0], agentUUID)
```

This requires the create API response to include the UUID (either from the agent.yaml or from the registry).

- [ ] **Step 3: Build and verify**

Run: `cd /home/geoff/agency-workspace/agency && go build ./cmd/gateway/`

- [ ] **Step 4: Commit**

```bash
git add internal/cli/commands.go
git commit -m "feat(registry): add agency registry CLI commands"
```

---

## Task 6: Knowledge Service — Switch to Snapshot Reader

**Files:**
- Modify: `images/knowledge/principal_registry.py`
- Modify: `images/knowledge/server.py`

- [ ] **Step 1: Update principal_registry.py to read from snapshot**

Replace the SQLite-backed PrincipalRegistry with a snapshot reader:

```python
# images/knowledge/principal_registry.py
"""Principal registry — reads from gateway snapshot.

The gateway is the authoritative source. This module reads the
registry.json snapshot delivered via config path or file mount.
"""
import json
import logging
import os

logger = logging.getLogger(__name__)


class PrincipalRegistry:
    """Read-only principal registry backed by a JSON snapshot."""

    VALID_TYPES = ("operator", "agent", "team", "role", "channel")

    def __init__(self, snapshot_path=None, snapshot_data=None):
        self._principals = {}  # uuid -> principal dict
        self._by_type_name = {}  # (type, name) -> uuid
        if snapshot_path:
            self.load_file(snapshot_path)
        elif snapshot_data:
            self.load_data(snapshot_data)

    def load_file(self, path):
        """Load registry from a JSON snapshot file."""
        if not os.path.exists(path):
            logger.warning("Registry snapshot not found at %s", path)
            return
        with open(path) as f:
            data = json.load(f)
        self.load_data(data)

    def load_data(self, data):
        """Load registry from a parsed snapshot dict."""
        self._principals = {}
        self._by_type_name = {}
        for p in data.get("principals", []):
            uuid = p["uuid"]
            self._principals[uuid] = p
            self._by_type_name[(p["type"], p["name"])] = uuid

    def resolve(self, uuid):
        """Resolve UUID to principal dict."""
        return self._principals.get(uuid)

    def resolve_name(self, principal_type, name):
        """Resolve type+name to UUID."""
        return self._by_type_name.get((principal_type, name))

    def list_by_type(self, principal_type):
        """List all principals of a type."""
        return [p for p in self._principals.values() if p["type"] == principal_type]

    def list_all(self):
        """List all principals."""
        return list(self._principals.values())

    @staticmethod
    def format_id(principal_type, uuid):
        return f"{principal_type}:{uuid}"

    def parse_id(self, principal_id):
        """Parse type:identifier, resolve name to UUID if possible."""
        if ":" not in principal_id:
            raise ValueError(f"Invalid principal ID: {principal_id}")
        ptype, identifier = principal_id.split(":", 1)
        if len(identifier) == 36 and identifier.count("-") == 4:
            return ptype, identifier
        resolved = self.resolve_name(ptype, identifier)
        return ptype, resolved if resolved else identifier
```

- [ ] **Step 2: Update server.py — remove POST /principals, update init**

In `images/knowledge/server.py`:

1. Update PrincipalRegistry initialization in `create_app()` to use snapshot path:

```python
snapshot_path = os.environ.get("REGISTRY_SNAPSHOT_PATH", "/app/registry.json")
# Fallback: try ~/.agency/registry.json via mount
if not os.path.exists(snapshot_path):
    snapshot_path = os.environ.get("AGENCY_HOME", "/data") + "/registry.json"
principal_registry = PrincipalRegistry(snapshot_path=snapshot_path)
app["principal_registry"] = principal_registry
```

2. Remove the `handle_principals_register` handler (POST /principals).
3. Keep `handle_principals_list` and `handle_principals_resolve` — they now read from the snapshot.
4. Remove the `app.router.add_post("/principals", ...)` route.

- [ ] **Step 3: Verify server imports**

Run: `cd /home/geoff/agency-workspace/agency && python3 -c "import sys; sys.path.insert(0, 'images/knowledge'); from principal_registry import PrincipalRegistry; print('OK')"`

- [ ] **Step 4: Update tests**

Update `images/tests/test_principal_registry.py` to test the snapshot-based API instead of SQLite-based.

- [ ] **Step 5: Commit**

```bash
git add images/knowledge/principal_registry.py images/knowledge/server.py images/tests/test_principal_registry.py
git commit -m "feat(knowledge): switch principal registry to gateway snapshot reader"
```

---

## Task 7: Config Delivery — Registry Snapshot to Enforcer Path

**Files:**
- Modify: `internal/orchestrate/infra.go` (or agent startup path)

- [ ] **Step 1: Write registry.json to agent config directory**

When starting an agent, write the registry snapshot to the agent's config directory so the enforcer can serve it:

```go
// In the agent start path, after writing other config files:
snapData, _ := am.infra.Registry.Snapshot()
snapPath := filepath.Join(agentDir, "state", "registry.json")
os.WriteFile(snapPath, snapData, 0644)
```

- [ ] **Step 2: Mount into enforcer**

Add the snapshot file to the enforcer's bind mounts (read-only) if not already served via the existing config delivery mechanism.

If the enforcer already serves files from the agent's state directory, no mount change is needed — the body runtime can fetch it via `GET /config/registry.json`.

- [ ] **Step 3: Build and verify**

Run: `cd /home/geoff/agency-workspace/agency && go build ./cmd/gateway/`

- [ ] **Step 4: Commit**

```bash
git add internal/orchestrate/
git commit -m "feat(registry): deliver registry snapshot via enforcer config path"
```

---

## Task 8: Full Test Suite Validation

- [ ] **Step 1: Run Go registry tests**

```bash
go test ./internal/registry/ -v
```

- [ ] **Step 2: Run Go gateway build**

```bash
go build ./cmd/gateway/
```

- [ ] **Step 3: Run full Go test suite**

```bash
go test ./... 2>&1 | tail -20
```

- [ ] **Step 4: Run Python tests (knowledge service)**

```bash
python3 -m pytest images/tests/test_principal_registry.py -v
```

- [ ] **Step 5: Run all Phase 1-4 tests for regressions**

```bash
python3 -m pytest images/tests/test_edge_provenance.py images/tests/test_scope_model.py images/tests/test_save_insight.py images/tests/test_graph_intelligence.py -q
```

- [ ] **Step 6: Commit any fixes**

---

## Summary

| Phase | What it delivers |
|-------|-----------------|
| **Task 1** | `internal/registry/` Go package — Registry struct, SQLite, full CRUD, Snapshot |
| **Task 2** | Gateway startup creates registry.db, registers system channels, writes snapshot |
| **Task 3** | `agency agent create` auto-registers, UUID in agent.yaml and container labels |
| **Task 4** | REST API: GET/POST/PUT/DELETE `/api/v1/registry` |
| **Task 5** | CLI: `agency registry list/show/update/delete` |
| **Task 6** | Knowledge service reads snapshot, POST /principals removed |
| **Task 7** | Registry snapshot delivered via enforcer config path |
| **Task 8** | Full validation across Go and Python |
