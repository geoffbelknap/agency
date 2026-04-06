// Package registry implements a UUID-based principal registry backed by SQLite.
// Every platform entity (agent, operator, team, role, channel) gets a UUID at
// creation time. Names remain the operator-facing interface; UUIDs are the
// cross-system identity for authorization, audit trails, and knowledge graph refs.
package registry

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// defaultPermissions are applied when Register() is called without WithPermissions.
var defaultPermissions = map[string][]string{
	"operator": {"*"},
	"team":     {"agent.read", "knowledge.read", "knowledge.write", "mission.read"},
	"agent":    {"knowledge.read", "knowledge.write"},
	"channel":  {},
	"role":     {},
}

const schema = `
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
CREATE TABLE IF NOT EXISTS tokens (
    token TEXT PRIMARY KEY,
    principal_uuid TEXT NOT NULL,
    created_at TEXT NOT NULL
);
`

// Principal represents a platform identity — agent, operator, team, role, or channel.
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

// RegistrySnapshot is a point-in-time export of all principals.
type RegistrySnapshot struct {
	Version     int         `json:"version"`
	GeneratedAt string      `json:"generated_at"`
	Principals  []Principal `json:"principals"`
}

// Registry wraps a SQLite database for UUID-based principal identity.
type Registry struct {
	db           *sql.DB
	gatewayToken string
}

// Option configures optional fields during principal registration.
type Option func(*registerOpts)

type registerOpts struct {
	parent      string
	metadata    map[string]interface{}
	permissions []string
}

// WithParent sets the parent UUID for the new principal.
func WithParent(uuid string) Option {
	return func(o *registerOpts) { o.parent = uuid }
}

// WithMetadata sets metadata for the new principal.
func WithMetadata(m map[string]interface{}) Option {
	return func(o *registerOpts) { o.metadata = m }
}

// WithPermissions sets permissions for the new principal.
func WithPermissions(perms []string) Option {
	return func(o *registerOpts) { o.permissions = perms }
}

// Open opens or creates the registry database at the given path and initializes the schema.
func Open(path string) (*Registry, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open registry db: %w", err)
	}
	// Enable WAL mode for concurrent reads.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Registry{db: db}, nil
}

// Close closes the underlying database connection.
func (r *Registry) Close() error {
	return r.db.Close()
}

// Register creates a new principal with a generated UUID. Returns the UUID.
// Returns an error if (type, name) already exists.
func (r *Registry) Register(principalType, name string, opts ...Option) (string, error) {
	o := &registerOpts{}
	for _, fn := range opts {
		fn(o)
	}

	// Apply default permissions if WithPermissions was not explicitly called.
	if o.permissions == nil {
		if defaults, ok := defaultPermissions[principalType]; ok {
			o.permissions = defaults
		} else {
			o.permissions = []string{}
		}
	}

	id := uuid.New().String()
	createdAt := time.Now().UTC().Format(time.RFC3339)

	permsJSON, err := json.Marshal(o.permissions)
	if err != nil {
		return "", fmt.Errorf("marshal permissions: %w", err)
	}

	metaJSON := []byte("{}")
	if o.metadata != nil {
		metaJSON, err = json.Marshal(o.metadata)
		if err != nil {
			return "", fmt.Errorf("marshal metadata: %w", err)
		}
	}

	_, err = r.db.Exec(
		`INSERT INTO principals (uuid, type, name, parent, status, permissions, created_at, metadata)
		 VALUES (?, ?, ?, ?, 'active', ?, ?, ?)`,
		id, principalType, name, o.parent, string(permsJSON), createdAt, string(metaJSON),
	)
	if err != nil {
		return "", fmt.Errorf("register principal: %w", err)
	}
	return id, nil
}

// Resolve looks up a principal by UUID.
func (r *Registry) Resolve(uuid string) (*Principal, error) {
	row := r.db.QueryRow(
		`SELECT uuid, type, name, parent, status, permissions, created_at, metadata
		 FROM principals WHERE uuid = ?`, uuid,
	)
	return scanPrincipal(row)
}

// ResolveByName looks up a principal by type and name.
func (r *Registry) ResolveByName(principalType, name string) (*Principal, error) {
	row := r.db.QueryRow(
		`SELECT uuid, type, name, parent, status, permissions, created_at, metadata
		 FROM principals WHERE type = ? AND name = ?`, principalType, name,
	)
	return scanPrincipal(row)
}

// ResolveAny looks up a principal by UUID first (if nameOrUUID is 36 chars),
// then falls back to name lookup.
func (r *Registry) ResolveAny(principalType, nameOrUUID string) (*Principal, error) {
	if len(nameOrUUID) == 36 {
		p, err := r.Resolve(nameOrUUID)
		if err == nil {
			return p, nil
		}
	}
	return r.ResolveByName(principalType, nameOrUUID)
}

// List returns all principals matching the given type. If principalType is empty,
// all principals are returned.
func (r *Registry) List(principalType string) ([]Principal, error) {
	var rows *sql.Rows
	var err error
	if principalType == "" {
		rows, err = r.db.Query(
			`SELECT uuid, type, name, parent, status, permissions, created_at, metadata
			 FROM principals ORDER BY created_at`,
		)
	} else {
		rows, err = r.db.Query(
			`SELECT uuid, type, name, parent, status, permissions, created_at, metadata
			 FROM principals WHERE type = ? ORDER BY created_at`, principalType,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("list principals: %w", err)
	}
	defer rows.Close()

	var principals []Principal
	for rows.Next() {
		p, err := scanPrincipalRow(rows)
		if err != nil {
			return nil, err
		}
		principals = append(principals, *p)
	}
	return principals, rows.Err()
}

// allowedUpdateFields defines which fields can be updated.
var allowedUpdateFields = map[string]bool{
	"parent":      true,
	"status":      true,
	"permissions": true,
	"metadata":    true,
}

// Update modifies allowed fields on an existing principal.
// Only parent, status, permissions, and metadata can be updated.
func (r *Registry) Update(uuid string, fields map[string]interface{}) error {
	for k := range fields {
		if !allowedUpdateFields[k] {
			return fmt.Errorf("field %q is not updatable", k)
		}
	}

	if len(fields) == 0 {
		return fmt.Errorf("no fields to update")
	}

	// Verify principal exists.
	var exists int
	if err := r.db.QueryRow("SELECT 1 FROM principals WHERE uuid = ?", uuid).Scan(&exists); err != nil {
		return fmt.Errorf("principal %s not found", uuid)
	}

	for k, v := range fields {
		var val string
		switch k {
		case "permissions":
			// Accept []string or []interface{}.
			switch perms := v.(type) {
			case []string:
				b, _ := json.Marshal(perms)
				val = string(b)
			case []interface{}:
				b, _ := json.Marshal(perms)
				val = string(b)
			default:
				return fmt.Errorf("permissions must be a string slice")
			}
		case "metadata":
			switch m := v.(type) {
			case map[string]interface{}:
				b, _ := json.Marshal(m)
				val = string(b)
			case json.RawMessage:
				val = string(m)
			default:
				return fmt.Errorf("metadata must be a map")
			}
		default:
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("field %q must be a string", k)
			}
			val = s
		}

		if _, err := r.db.Exec(
			fmt.Sprintf("UPDATE principals SET %s = ? WHERE uuid = ?", k),
			val, uuid,
		); err != nil {
			return fmt.Errorf("update %s: %w", k, err)
		}
	}
	return nil
}

// Delete removes a principal by UUID. Returns an error if the principal does not exist.
func (r *Registry) Delete(uuid string) error {
	result, err := r.db.Exec("DELETE FROM principals WHERE uuid = ?", uuid)
	if err != nil {
		return fmt.Errorf("delete principal: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete principal: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("principal %s not found", uuid)
	}
	return nil
}

// Snapshot returns a JSON-encoded RegistrySnapshot containing all principals.
func (r *Registry) Snapshot() ([]byte, error) {
	principals, err := r.List("")
	if err != nil {
		return nil, err
	}
	snap := RegistrySnapshot{
		Version:     1,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Principals:  principals,
	}
	return json.Marshal(snap)
}

// GenerateToken creates a secure random token mapped to the given principal UUID.
// The principal must exist. Returns a 64-character hex-encoded token.
func (r *Registry) GenerateToken(principalUUID string) (string, error) {
	// Verify principal exists.
	var exists int
	if err := r.db.QueryRow("SELECT 1 FROM principals WHERE uuid = ?", principalUUID).Scan(&exists); err != nil {
		return "", fmt.Errorf("principal %s not found", principalUUID)
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(b)
	createdAt := time.Now().UTC().Format(time.RFC3339)

	if _, err := r.db.Exec(
		"INSERT INTO tokens (token, principal_uuid, created_at) VALUES (?, ?, ?)",
		token, principalUUID, createdAt,
	); err != nil {
		return "", fmt.Errorf("store token: %w", err)
	}
	return token, nil
}

// ResolveToken looks up a token and returns the associated principal.
// If a gateway token is set and matches, returns the first active operator.
func (r *Registry) ResolveToken(token string) (*Principal, error) {
	// Check gateway token first.
	if r.gatewayToken != "" && token == r.gatewayToken {
		row := r.db.QueryRow(
			`SELECT uuid, type, name, parent, status, permissions, created_at, metadata
			 FROM principals WHERE type = 'operator' AND status = 'active'
			 ORDER BY created_at LIMIT 1`,
		)
		return scanPrincipal(row)
	}

	var principalUUID string
	err := r.db.QueryRow("SELECT principal_uuid FROM tokens WHERE token = ?", token).Scan(&principalUUID)
	if err != nil {
		return nil, fmt.Errorf("token not found")
	}
	return r.Resolve(principalUUID)
}

// SetGatewayToken stores the gateway token for backward-compatible token resolution.
func (r *Registry) SetGatewayToken(token string) {
	r.gatewayToken = token
}

// RevokeTokens deletes all tokens for the given principal UUID.
func (r *Registry) RevokeTokens(principalUUID string) error {
	_, err := r.db.Exec("DELETE FROM tokens WHERE principal_uuid = ?", principalUUID)
	if err != nil {
		return fmt.Errorf("revoke tokens: %w", err)
	}
	return nil
}

// HasActiveGovernance walks the parent hierarchy looking for an active operator.
// Returns true if the principal or any ancestor is an active operator.
func (r *Registry) HasActiveGovernance(uuid string) bool {
	p, err := r.Resolve(uuid)
	if err != nil {
		return false
	}
	// Walk up the hierarchy looking for an active operator.
	if p.Status == "active" && p.Type == "operator" {
		return true
	}
	if p.Parent != "" {
		return r.HasActiveGovernance(p.Parent)
	}
	return false
}

// scanPrincipal scans a single row into a Principal.
func scanPrincipal(row *sql.Row) (*Principal, error) {
	var p Principal
	var permsJSON, metaJSON string
	err := row.Scan(&p.UUID, &p.Type, &p.Name, &p.Parent, &p.Status, &permsJSON, &p.CreatedAt, &metaJSON)
	if err != nil {
		return nil, fmt.Errorf("principal not found: %w", err)
	}
	if err := json.Unmarshal([]byte(permsJSON), &p.Permissions); err != nil {
		p.Permissions = []string{}
	}
	if metaJSON != "" && metaJSON != "{}" {
		p.Metadata = json.RawMessage(metaJSON)
	}
	return &p, nil
}

// scanPrincipalRow scans from sql.Rows (used by List).
func scanPrincipalRow(rows *sql.Rows) (*Principal, error) {
	var p Principal
	var permsJSON, metaJSON string
	err := rows.Scan(&p.UUID, &p.Type, &p.Name, &p.Parent, &p.Status, &permsJSON, &p.CreatedAt, &metaJSON)
	if err != nil {
		return nil, fmt.Errorf("scan principal: %w", err)
	}
	if err := json.Unmarshal([]byte(permsJSON), &p.Permissions); err != nil {
		p.Permissions = []string{}
	}
	if metaJSON != "" && metaJSON != "{}" {
		p.Metadata = json.RawMessage(metaJSON)
	}
	return &p, nil
}
