package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func tempDB(t *testing.T) *Registry {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test-registry.db")
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestRegisterReturns36CharUUID(t *testing.T) {
	r := tempDB(t)
	uuid, err := r.Register("agent", "researcher")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(uuid) != 36 {
		t.Errorf("expected 36-char UUID, got %d chars: %s", len(uuid), uuid)
	}
}

func TestDuplicateTypeNameReturnsError(t *testing.T) {
	r := tempDB(t)
	_, err := r.Register("agent", "researcher")
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}
	_, err = r.Register("agent", "researcher")
	if err == nil {
		t.Fatal("expected error for duplicate (type, name), got nil")
	}
}

func TestDifferentTypesSameNameGetDifferentUUIDs(t *testing.T) {
	r := tempDB(t)
	u1, err := r.Register("agent", "alpha")
	if err != nil {
		t.Fatalf("Register agent: %v", err)
	}
	u2, err := r.Register("channel", "alpha")
	if err != nil {
		t.Fatalf("Register channel: %v", err)
	}
	if u1 == u2 {
		t.Errorf("expected different UUIDs, both got %s", u1)
	}
}

func TestResolveByUUID(t *testing.T) {
	r := tempDB(t)
	uuid, _ := r.Register("operator", "geoff")
	p, err := r.Resolve(uuid)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.UUID != uuid {
		t.Errorf("UUID mismatch: got %s, want %s", p.UUID, uuid)
	}
	if p.Type != "operator" || p.Name != "geoff" {
		t.Errorf("unexpected type/name: %s/%s", p.Type, p.Name)
	}
	if p.Status != "active" {
		t.Errorf("expected status active, got %s", p.Status)
	}
}

func TestResolveNotFoundReturnsError(t *testing.T) {
	r := tempDB(t)
	_, err := r.Resolve("00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected error for not found, got nil")
	}
}

func TestResolveByName(t *testing.T) {
	r := tempDB(t)
	uuid, _ := r.Register("agent", "scanner")
	p, err := r.ResolveByName("agent", "scanner")
	if err != nil {
		t.Fatalf("ResolveByName: %v", err)
	}
	if p.UUID != uuid {
		t.Errorf("UUID mismatch: got %s, want %s", p.UUID, uuid)
	}
}

func TestResolveAnyByUUID(t *testing.T) {
	r := tempDB(t)
	uuid, _ := r.Register("agent", "triage")
	p, err := r.ResolveAny("agent", uuid)
	if err != nil {
		t.Fatalf("ResolveAny by UUID: %v", err)
	}
	if p.Name != "triage" {
		t.Errorf("expected name triage, got %s", p.Name)
	}
}

func TestResolveAnyByName(t *testing.T) {
	r := tempDB(t)
	uuid, _ := r.Register("agent", "triage")
	p, err := r.ResolveAny("agent", "triage")
	if err != nil {
		t.Fatalf("ResolveAny by name: %v", err)
	}
	if p.UUID != uuid {
		t.Errorf("UUID mismatch: got %s, want %s", p.UUID, uuid)
	}
}

func TestListByType(t *testing.T) {
	r := tempDB(t)
	r.Register("agent", "a1")
	r.Register("agent", "a2")
	r.Register("operator", "op1")

	agents, err := r.List("agent")
	if err != nil {
		t.Fatalf("List agent: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(agents))
	}

	ops, err := r.List("operator")
	if err != nil {
		t.Fatalf("List operator: %v", err)
	}
	if len(ops) != 1 {
		t.Errorf("expected 1 operator, got %d", len(ops))
	}
}

func TestListAll(t *testing.T) {
	r := tempDB(t)
	r.Register("agent", "a1")
	r.Register("operator", "op1")
	r.Register("channel", "ch1")

	all, err := r.List("")
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 principals, got %d", len(all))
	}
}

func TestUpdateParent(t *testing.T) {
	r := tempDB(t)
	teamUUID, _ := r.Register("team", "security")
	agentUUID, _ := r.Register("agent", "scanner")

	err := r.Update(agentUUID, map[string]interface{}{"parent": teamUUID})
	if err != nil {
		t.Fatalf("Update parent: %v", err)
	}
	p, _ := r.Resolve(agentUUID)
	if p.Parent != teamUUID {
		t.Errorf("expected parent %s, got %s", teamUUID, p.Parent)
	}
}

func TestUpdateStatus(t *testing.T) {
	r := tempDB(t)
	uuid, _ := r.Register("agent", "scanner")

	err := r.Update(uuid, map[string]interface{}{"status": "suspended"})
	if err != nil {
		t.Fatalf("Update status: %v", err)
	}
	p, _ := r.Resolve(uuid)
	if p.Status != "suspended" {
		t.Errorf("expected status suspended, got %s", p.Status)
	}
}

func TestUpdatePermissions(t *testing.T) {
	r := tempDB(t)
	uuid, _ := r.Register("agent", "scanner")

	perms := []string{"read:knowledge", "write:comms"}
	err := r.Update(uuid, map[string]interface{}{"permissions": perms})
	if err != nil {
		t.Fatalf("Update permissions: %v", err)
	}
	p, _ := r.Resolve(uuid)
	if len(p.Permissions) != 2 || p.Permissions[0] != "read:knowledge" {
		t.Errorf("unexpected permissions: %v", p.Permissions)
	}
}

func TestUpdateRejectsUnknownFields(t *testing.T) {
	r := tempDB(t)
	uuid, _ := r.Register("agent", "scanner")

	err := r.Update(uuid, map[string]interface{}{"bogus": "value"})
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

func TestDeleteRemovesPrincipal(t *testing.T) {
	r := tempDB(t)
	uuid, _ := r.Register("agent", "scanner")

	err := r.Delete(uuid)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = r.Resolve(uuid)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestDeleteNonexistentReturnsError(t *testing.T) {
	r := tempDB(t)
	err := r.Delete("00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected error for nonexistent delete, got nil")
	}
}

func TestSnapshotContainsAllPrincipals(t *testing.T) {
	r := tempDB(t)
	r.Register("agent", "a1")
	r.Register("operator", "op1")

	data, err := r.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	var snap RegistrySnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if snap.Version != 1 {
		t.Errorf("expected version 1, got %d", snap.Version)
	}
	if len(snap.Principals) != 2 {
		t.Errorf("expected 2 principals in snapshot, got %d", len(snap.Principals))
	}
}

func TestRegisterWithOptions(t *testing.T) {
	r := tempDB(t)
	teamUUID, _ := r.Register("team", "security")

	meta := map[string]interface{}{"source": "hub"}
	uuid, err := r.Register("agent", "scanner",
		WithParent(teamUUID),
		WithMetadata(meta),
		WithPermissions([]string{"read:knowledge"}),
	)
	if err != nil {
		t.Fatalf("Register with options: %v", err)
	}

	p, _ := r.Resolve(uuid)
	if p.Parent != teamUUID {
		t.Errorf("expected parent %s, got %s", teamUUID, p.Parent)
	}
	if len(p.Permissions) != 1 || p.Permissions[0] != "read:knowledge" {
		t.Errorf("unexpected permissions: %v", p.Permissions)
	}

	var m map[string]interface{}
	json.Unmarshal(p.Metadata, &m)
	if m["source"] != "hub" {
		t.Errorf("expected metadata source=hub, got %v", m["source"])
	}
}

func TestGenerateToken(t *testing.T) {
	r := tempDB(t)
	uuid, _ := r.Register("operator", "geoff")
	token, err := r.GenerateToken(uuid)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("expected 64-char hex token, got %d chars: %s", len(token), token)
	}
}

func TestResolveToken(t *testing.T) {
	r := tempDB(t)
	uuid, _ := r.Register("operator", "geoff")
	token, _ := r.GenerateToken(uuid)

	p, err := r.ResolveToken(token)
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if p.UUID != uuid {
		t.Errorf("expected UUID %s, got %s", uuid, p.UUID)
	}
	if p.Name != "geoff" {
		t.Errorf("expected name geoff, got %s", p.Name)
	}
}

func TestResolveTokenNotFound(t *testing.T) {
	r := tempDB(t)
	_, err := r.ResolveToken("deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if err == nil {
		t.Fatal("expected error for unknown token, got nil")
	}
}

func TestGatewayTokenResolvesToOperator(t *testing.T) {
	r := tempDB(t)
	// Create an inactive operator and an active one.
	inactiveUUID, _ := r.Register("operator", "inactive-op")
	r.Update(inactiveUUID, map[string]interface{}{"status": "suspended"})
	activeUUID, _ := r.Register("operator", "active-op")

	r.SetGatewayToken("my-gateway-token")

	p, err := r.ResolveToken("my-gateway-token")
	if err != nil {
		t.Fatalf("ResolveToken gateway: %v", err)
	}
	if p.UUID != activeUUID {
		t.Errorf("expected active operator UUID %s, got %s", activeUUID, p.UUID)
	}
}

func TestRevokeTokens(t *testing.T) {
	r := tempDB(t)
	uuid, _ := r.Register("operator", "geoff")
	token, _ := r.GenerateToken(uuid)

	err := r.RevokeTokens(uuid)
	if err != nil {
		t.Fatalf("RevokeTokens: %v", err)
	}

	_, err = r.ResolveToken(token)
	if err == nil {
		t.Fatal("expected error after revocation, got nil")
	}
}

func TestHasActiveGovernance(t *testing.T) {
	r := tempDB(t)

	opUUID, _ := r.Register("operator", "admin", WithPermissions([]string{"*"}))
	teamUUID, _ := r.Register("team", "sec", WithParent(opUUID))
	agentUUID, _ := r.Register("agent", "scout", WithParent(teamUUID))

	if !r.HasActiveGovernance(agentUUID) {
		t.Fatal("agent with active operator in chain should have governance")
	}

	// Suspend operator — agent should lose governance.
	r.Update(opUUID, map[string]interface{}{"status": "suspended"})
	if r.HasActiveGovernance(agentUUID) {
		t.Fatal("agent should lose governance when operator is suspended")
	}
}

func TestHasActiveGovernanceNoParent(t *testing.T) {
	r := tempDB(t)

	agentUUID, _ := r.Register("agent", "orphan")
	if r.HasActiveGovernance(agentUUID) {
		t.Fatal("orphan agent with no parent should not have active governance")
	}
}

func TestOpenCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new-registry.db")
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	r.Close()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected DB file to be created")
	}
}

func TestDefaultPermissionsOperator(t *testing.T) {
	r := tempDB(t)
	uuid, err := r.Register("operator", "admin")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	p, err := r.Resolve(uuid)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !Permits(p.Permissions, "agent.write") {
		t.Fatal("operator should default to * (covers everything)")
	}
}

func TestDefaultPermissionsAgent(t *testing.T) {
	r := tempDB(t)
	uuid, err := r.Register("agent", "scout")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	p, err := r.Resolve(uuid)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !Permits(p.Permissions, "knowledge.read") {
		t.Fatal("agent should default to knowledge.read")
	}
	if Permits(p.Permissions, "agent.write") {
		t.Fatal("agent should NOT have agent.write by default")
	}
}

func TestExplicitEmptyPermissions(t *testing.T) {
	r := tempDB(t)
	uuid, err := r.Register("agent", "minimal", WithPermissions([]string{}))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	p, err := r.Resolve(uuid)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(p.Permissions) != 0 {
		t.Fatalf("explicit empty should stay empty, got %v", p.Permissions)
	}
}
