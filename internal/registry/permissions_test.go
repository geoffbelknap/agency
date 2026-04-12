package registry

import (
	"testing"
)

// --- Permits tests ---

func TestPermitsExactMatch(t *testing.T) {
	if !Permits([]string{"agent.read", "agent.write"}, "agent.read") {
		t.Error("expected exact match to permit")
	}
}

func TestPermitsSuperuserWildcard(t *testing.T) {
	if !Permits([]string{"*"}, "anything.at.all") {
		t.Error("expected * to permit everything")
	}
}

func TestPermitsNamespaceWildcard(t *testing.T) {
	perms := []string{"knowledge.*"}
	if !Permits(perms, "knowledge.read") {
		t.Error("expected knowledge.* to permit knowledge.read")
	}
	if !Permits(perms, "knowledge.write") {
		t.Error("expected knowledge.* to permit knowledge.write")
	}
}

func TestPermitsWildcardDoesNotMatchDifferentPrefix(t *testing.T) {
	if Permits([]string{"knowledge.*"}, "agent.read") {
		t.Error("knowledge.* should not match agent.read")
	}
}

func TestPermitsNoMatch(t *testing.T) {
	if Permits([]string{"agent.read"}, "agent.write") {
		t.Error("expected no match for different permission")
	}
}

func TestPermitsPartialStringNoMatch(t *testing.T) {
	// "agent.rea" should not match "agent.read"
	if Permits([]string{"agent.rea"}, "agent.read") {
		t.Error("partial string should not match")
	}
}

func TestPermitsEmptyPerms(t *testing.T) {
	if Permits([]string{}, "agent.read") {
		t.Error("empty perms should not permit anything")
	}
	if Permits(nil, "agent.read") {
		t.Error("nil perms should not permit anything")
	}
}

func TestPermitsWildcardRequiresPrefix(t *testing.T) {
	// "knowledge.*" should not match plain "knowledge"
	if Permits([]string{"knowledge.*"}, "knowledge") {
		t.Error("knowledge.* should not match bare 'knowledge'")
	}
}

func TestApplyPermissionCeiling(t *testing.T) {
	got := ApplyPermissionCeiling([]string{"knowledge.*", "agent.read"}, []string{"knowledge.read", "agent.write", "agent.read"})
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (%v)", len(got), got)
	}
	if got[0] != "knowledge.read" || got[1] != "agent.read" {
		t.Fatalf("got = %v, want [knowledge.read agent.read]", got)
	}
}

// --- EffectivePermissions tests ---

func TestEffectivePermissions_NoParent_OwnPerms(t *testing.T) {
	r := tempDB(t)
	uuid, _ := r.Register("operator", "geoff",
		WithPermissions([]string{"agent.read", "agent.write"}),
	)

	perms, err := r.EffectivePermissions(uuid)
	if err != nil {
		t.Fatalf("EffectivePermissions: %v", err)
	}
	if len(perms) != 2 {
		t.Fatalf("expected 2 perms, got %d: %v", len(perms), perms)
	}
}

func TestEffectivePermissions_EmptyOwnInheritsParent(t *testing.T) {
	r := tempDB(t)
	parentUUID, _ := r.Register("team", "security",
		WithPermissions([]string{"knowledge.read", "agent.read"}),
	)
	childUUID, _ := r.Register("agent", "scanner",
		WithParent(parentUUID),
		WithPermissions([]string{}), // explicit empty — should inherit parent's full set
	)

	perms, err := r.EffectivePermissions(childUUID)
	if err != nil {
		t.Fatalf("EffectivePermissions: %v", err)
	}
	if len(perms) != 2 {
		t.Fatalf("expected 2 inherited perms, got %d: %v", len(perms), perms)
	}
}

func TestEffectivePermissions_CeilingBlocksExcess(t *testing.T) {
	r := tempDB(t)
	parentUUID, _ := r.Register("team", "security",
		WithPermissions([]string{"knowledge.read"}),
	)
	// Child claims more than parent has — ceiling should block
	childUUID, _ := r.Register("agent", "scanner",
		WithParent(parentUUID),
		WithPermissions([]string{"knowledge.read", "agent.write"}),
	)

	perms, err := r.EffectivePermissions(childUUID)
	if err != nil {
		t.Fatalf("EffectivePermissions: %v", err)
	}
	if len(perms) != 1 || perms[0] != "knowledge.read" {
		t.Fatalf("expected [knowledge.read], got %v", perms)
	}
}

func TestEffectivePermissions_MultiLevelHierarchy(t *testing.T) {
	r := tempDB(t)
	// operator (top) → team → agent
	opUUID, _ := r.Register("operator", "geoff",
		WithPermissions([]string{"knowledge.*", "agent.read", "comms.send"}),
	)
	teamUUID, _ := r.Register("team", "security",
		WithParent(opUUID),
		WithPermissions([]string{"knowledge.read", "agent.read", "comms.send"}),
	)
	agentUUID, _ := r.Register("agent", "scanner",
		WithParent(teamUUID),
		WithPermissions([]string{"knowledge.read", "comms.send", "admin.delete"}),
	)

	perms, err := r.EffectivePermissions(agentUUID)
	if err != nil {
		t.Fatalf("EffectivePermissions: %v", err)
	}
	// agent claims knowledge.read, comms.send, admin.delete
	// team ceiling has knowledge.read, agent.read, comms.send
	// so admin.delete is blocked, agent.read is not claimed → result: knowledge.read, comms.send
	expected := map[string]bool{"knowledge.read": true, "comms.send": true}
	if len(perms) != len(expected) {
		t.Fatalf("expected %d perms, got %d: %v", len(expected), len(perms), perms)
	}
	for _, p := range perms {
		if !expected[p] {
			t.Errorf("unexpected perm: %s", p)
		}
	}
}

func TestEffectivePermissions_SuspendedReturnsEmpty(t *testing.T) {
	r := tempDB(t)
	uuid, _ := r.Register("agent", "scanner",
		WithPermissions([]string{"knowledge.read"}),
	)
	r.Update(uuid, map[string]interface{}{"status": "suspended"})

	perms, err := r.EffectivePermissions(uuid)
	if err != nil {
		t.Fatalf("EffectivePermissions: %v", err)
	}
	if len(perms) != 0 {
		t.Errorf("suspended principal should have no perms, got %v", perms)
	}
}

func TestEffectivePermissions_RevokedReturnsEmpty(t *testing.T) {
	r := tempDB(t)
	uuid, _ := r.Register("agent", "scanner",
		WithPermissions([]string{"knowledge.read"}),
	)
	r.Update(uuid, map[string]interface{}{"status": "revoked"})

	perms, err := r.EffectivePermissions(uuid)
	if err != nil {
		t.Fatalf("EffectivePermissions: %v", err)
	}
	if len(perms) != 0 {
		t.Errorf("revoked principal should have no perms, got %v", perms)
	}
}

func TestEffectivePermissions_NotFoundReturnsError(t *testing.T) {
	r := tempDB(t)
	_, err := r.EffectivePermissions("00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected error for nonexistent principal")
	}
}

func TestEffectivePermissions_ParentWildcardPermitsChild(t *testing.T) {
	r := tempDB(t)
	parentUUID, _ := r.Register("operator", "admin",
		WithPermissions([]string{"*"}),
	)
	childUUID, _ := r.Register("agent", "scanner",
		WithParent(parentUUID),
		WithPermissions([]string{"knowledge.read", "agent.write"}),
	)

	perms, err := r.EffectivePermissions(childUUID)
	if err != nil {
		t.Fatalf("EffectivePermissions: %v", err)
	}
	if len(perms) != 2 {
		t.Fatalf("parent * should permit all child perms, got %v", perms)
	}
}

func TestEffectivePermissions_ParentNamespaceWildcard(t *testing.T) {
	r := tempDB(t)
	parentUUID, _ := r.Register("team", "security",
		WithPermissions([]string{"knowledge.*"}),
	)
	childUUID, _ := r.Register("agent", "scanner",
		WithParent(parentUUID),
		WithPermissions([]string{"knowledge.read", "knowledge.write", "agent.read"}),
	)

	perms, err := r.EffectivePermissions(childUUID)
	if err != nil {
		t.Fatalf("EffectivePermissions: %v", err)
	}
	// knowledge.* permits knowledge.read and knowledge.write but not agent.read
	expected := map[string]bool{"knowledge.read": true, "knowledge.write": true}
	if len(perms) != 2 {
		t.Fatalf("expected 2 perms, got %d: %v", len(perms), perms)
	}
	for _, p := range perms {
		if !expected[p] {
			t.Errorf("unexpected perm: %s", p)
		}
	}
}
