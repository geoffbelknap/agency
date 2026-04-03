package profiles

import (
	"testing"

	"github.com/geoffbelknap/agency/internal/models"
)

func TestPutAndGet(t *testing.T) {
	s := NewStore(t.TempDir())
	p := models.Profile{
		ID:          "alice",
		Type:        "operator",
		DisplayName: "Alice Smith",
		Email:       "alice@example.com",
		Department:  "Engineering",
	}
	if err := s.Put(p); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get("alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DisplayName != "Alice Smith" {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, "Alice Smith")
	}
	if got.Email != "alice@example.com" {
		t.Errorf("Email = %q, want %q", got.Email, "alice@example.com")
	}
	if got.Created == "" {
		t.Error("Created should be set")
	}
	if got.Updated == "" {
		t.Error("Updated should be set")
	}
}

func TestGetNotFound(t *testing.T) {
	s := NewStore(t.TempDir())
	_, err := s.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestDelete(t *testing.T) {
	s := NewStore(t.TempDir())
	p := models.Profile{ID: "bob", Type: "agent", DisplayName: "Bob Agent"}
	if err := s.Put(p); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete("bob"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := s.Get("bob")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestDeleteNotFound(t *testing.T) {
	s := NewStore(t.TempDir())
	err := s.Delete("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestListAll(t *testing.T) {
	s := NewStore(t.TempDir())
	s.Put(models.Profile{ID: "op1", Type: "operator", DisplayName: "Op 1"})
	s.Put(models.Profile{ID: "agent1", Type: "agent", DisplayName: "Agent 1"})
	s.Put(models.Profile{ID: "agent2", Type: "agent", DisplayName: "Agent 2"})

	all, err := s.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(all))
	}
}

func TestListFiltered(t *testing.T) {
	s := NewStore(t.TempDir())
	s.Put(models.Profile{ID: "op1", Type: "operator", DisplayName: "Op 1"})
	s.Put(models.Profile{ID: "agent1", Type: "agent", DisplayName: "Agent 1"})

	agents, err := s.List("agent")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent profile, got %d", len(agents))
	}
	if agents[0].ID != "agent1" {
		t.Errorf("expected agent1, got %s", agents[0].ID)
	}
}

func TestPutInvalidID(t *testing.T) {
	s := NewStore(t.TempDir())
	err := s.Put(models.Profile{ID: "../bad", Type: "operator", DisplayName: "Bad"})
	if err == nil {
		t.Fatal("expected error for invalid ID")
	}
}

func TestPutInvalidType(t *testing.T) {
	s := NewStore(t.TempDir())
	err := s.Put(models.Profile{ID: "test", Type: "invalid", DisplayName: "Test"})
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestPutPreservesCreated(t *testing.T) {
	s := NewStore(t.TempDir())
	s.Put(models.Profile{ID: "alice", Type: "operator", DisplayName: "Alice"})
	first, _ := s.Get("alice")

	s.Put(models.Profile{ID: "alice", Type: "operator", DisplayName: "Alice Updated"})
	second, _ := s.Get("alice")

	if second.Created != first.Created {
		t.Errorf("Created changed: %q -> %q", first.Created, second.Created)
	}
	if second.DisplayName != "Alice Updated" {
		t.Errorf("DisplayName not updated: %q", second.DisplayName)
	}
}
