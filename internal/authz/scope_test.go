package authz_test

import (
	"path/filepath"
	"testing"

	"github.com/geoffbelknap/agency/internal/authz"
	"github.com/geoffbelknap/agency/internal/registry"
)

func openTestRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	reg, err := registry.Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	t.Cleanup(func() { reg.Close() })
	return reg
}

// mustRegister registers a principal and returns its resolved form.
func mustRegister(t *testing.T, reg *registry.Registry, kind, name string, opts ...registry.Option) *registry.Principal {
	t.Helper()
	if _, err := reg.Register(kind, name, opts...); err != nil {
		t.Fatalf("register %s/%s: %v", kind, name, err)
	}
	p, err := reg.ResolveByName(kind, name)
	if err != nil {
		t.Fatalf("resolve %s/%s: %v", kind, name, err)
	}
	return p
}

func TestResolve_NilPrincipalAllowsAll(t *testing.T) {
	reg := openTestRegistry(t)
	s := authz.Resolve(nil, reg)
	if !s.All {
		t.Fatal("nil principal should resolve to All=true (transitional)")
	}
}

func TestResolve_NilRegistryAllowsAll(t *testing.T) {
	p := &registry.Principal{Type: "agent", Name: "bob"}
	s := authz.Resolve(p, nil)
	if !s.All {
		t.Fatal("nil registry should resolve to All=true")
	}
}

func TestResolve_OperatorAllowsAll(t *testing.T) {
	reg := openTestRegistry(t)
	p := mustRegister(t, reg, "operator", "alice")
	s := authz.Resolve(p, reg)
	if !s.All {
		t.Fatal("operator scope should be All=true")
	}
}

func TestResolve_AgentSelfAndDM(t *testing.T) {
	reg := openTestRegistry(t)
	p := mustRegister(t, reg, "agent", "bob")

	s := authz.Resolve(p, reg)
	if s.All {
		t.Fatal("agent scope should not be All")
	}
	if !s.AllowsAgent("bob") {
		t.Error("agent should see its own events")
	}
	if s.AllowsAgent("eve") {
		t.Error("agent should not see unrelated agent events")
	}
	if !s.AllowsChannel("dm-bob") {
		t.Error("agent should see its own DM channel")
	}
	if s.AllowsChannel("dm-eve") {
		t.Error("agent should not see another agent's DM")
	}
	if s.AllowsInfra() {
		t.Error("agent should not see infra events")
	}
}

func TestResolve_AgentSeesTeamSiblings(t *testing.T) {
	reg := openTestRegistry(t)
	team := mustRegister(t, reg, "team", "security")
	bob := mustRegister(t, reg, "agent", "bob", registry.WithParent(team.UUID))
	_ = mustRegister(t, reg, "agent", "carol", registry.WithParent(team.UUID))
	_ = mustRegister(t, reg, "agent", "eve") // no parent — not a sibling

	s := authz.Resolve(bob, reg)
	if !s.AllowsAgent("bob") {
		t.Error("bob should see bob")
	}
	if !s.AllowsAgent("carol") {
		t.Error("bob should see team sibling carol")
	}
	if s.AllowsAgent("eve") {
		t.Error("bob should NOT see unrelated agent eve")
	}
}

func TestResolve_TeamSeesMembers(t *testing.T) {
	reg := openTestRegistry(t)
	team := mustRegister(t, reg, "team", "security")
	_ = mustRegister(t, reg, "agent", "bob", registry.WithParent(team.UUID))
	_ = mustRegister(t, reg, "agent", "carol", registry.WithParent(team.UUID))
	_ = mustRegister(t, reg, "agent", "eve")

	s := authz.Resolve(team, reg)
	if s.All {
		t.Fatal("team scope should not be All")
	}
	if !s.AllowsAgent("bob") || !s.AllowsAgent("carol") {
		t.Error("team should see its members")
	}
	if s.AllowsAgent("eve") {
		t.Error("team should not see non-member agents")
	}
	if s.AllowsInfra() {
		t.Error("team should not see infra events")
	}
}

func TestResolve_UnknownTypeDenies(t *testing.T) {
	reg := openTestRegistry(t)
	p := &registry.Principal{Type: "role", Name: "auditor"}
	s := authz.Resolve(p, reg)
	if s.All {
		t.Fatal("unknown type should not be All")
	}
	if s.AllowsAgent("anyone") || s.AllowsChannel("anything") || s.AllowsInfra() {
		t.Fatal("unknown type should deny everything")
	}
}

func TestAllowsAgent_EmptyNameAllowed(t *testing.T) {
	s := &authz.Scope{Agents: map[string]bool{"bob": true}}
	if !s.AllowsAgent("") {
		t.Fatal("events without an agent name must not be scope-blocked by AllowsAgent")
	}
}

func TestAllowsChannel_EmptyNameAllowed(t *testing.T) {
	s := &authz.Scope{Channels: map[string]bool{"dm-bob": true}}
	if !s.AllowsChannel("") {
		t.Fatal("events without a channel name must not be scope-blocked by AllowsChannel")
	}
}
