package orchestrate

import (
	"strings"
	"testing"

	"github.com/geoffbelknap/agency/internal/models"
)

func TestMeeseeksManagerSpawn(t *testing.T) {
	mm := NewMeeseeksManager()
	cfg := MeeseeksConfig{Enabled: true, Limit: 5, Model: "haiku", Budget: 0.05}
	parentTools := []string{"read_file", "write_file", "execute_command"}

	req := &models.MeeseeksSpawnRequest{Task: "do thing"}
	mks, err := mm.Spawn(req, "test-agent", "mission-1", parentTools, cfg)
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	if mks.ID == "" {
		t.Error("expected non-empty ID")
	}
	if mks.ParentAgent != "test-agent" {
		t.Errorf("parent = %s, want test-agent", mks.ParentAgent)
	}
	if mks.Status != models.MeeseeksStatusSpawned {
		t.Errorf("status = %s, want spawned", mks.Status)
	}
	if mks.Model != "haiku" {
		t.Errorf("model = %s, want haiku", mks.Model)
	}
	if mks.Budget != 0.05 {
		t.Errorf("budget = %f, want 0.05", mks.Budget)
	}
}

func TestMeeseeksManagerConcurrentLimit(t *testing.T) {
	mm := NewMeeseeksManager()
	cfg := MeeseeksConfig{Enabled: true, Limit: 2, Model: "haiku", Budget: 0.05}

	for i := 0; i < 2; i++ {
		req := &models.MeeseeksSpawnRequest{Task: "task"}
		_, err := mm.Spawn(req, "agent", "m1", nil, cfg)
		if err != nil {
			t.Fatalf("Spawn %d error = %v", i, err)
		}
	}

	req := &models.MeeseeksSpawnRequest{Task: "too many"}
	_, err := mm.Spawn(req, "agent", "m1", nil, cfg)
	if err == nil {
		t.Error("expected concurrent limit error")
	}
}

func TestMeeseeksManagerToolSubset(t *testing.T) {
	mm := NewMeeseeksManager()
	cfg := MeeseeksConfig{Enabled: true, Limit: 5}
	parentTools := []string{"read_file", "write_file"}

	req := &models.MeeseeksSpawnRequest{Task: "do thing", Tools: []string{"execute_command"}}
	_, err := mm.Spawn(req, "agent", "m1", parentTools, cfg)
	if err == nil {
		t.Error("expected tool subset error")
	}
}

func TestMeeseeksManagerGet(t *testing.T) {
	mm := NewMeeseeksManager()
	cfg := MeeseeksConfig{Enabled: true, Limit: 5}

	req := &models.MeeseeksSpawnRequest{Task: "task"}
	mks, _ := mm.Spawn(req, "agent", "m1", nil, cfg)

	got, err := mm.Get(mks.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != mks.ID {
		t.Errorf("Get() ID = %s, want %s", got.ID, mks.ID)
	}

	_, err = mm.Get("mks-nonexistent")
	if err == nil {
		t.Error("expected error for unknown ID")
	}
}

func TestMeeseeksManagerList(t *testing.T) {
	mm := NewMeeseeksManager()
	cfg := MeeseeksConfig{Enabled: true, Limit: 5}

	mm.Spawn(&models.MeeseeksSpawnRequest{Task: "t1"}, "agent1", "m1", nil, cfg)
	mm.Spawn(&models.MeeseeksSpawnRequest{Task: "t2"}, "agent2", "m2", nil, cfg)

	all := mm.List("")
	if len(all) != 2 {
		t.Errorf("List() = %d items, want 2", len(all))
	}

	byAgent1 := mm.List("agent1")
	if len(byAgent1) != 1 {
		t.Errorf("List(agent1) = %d items, want 1", len(byAgent1))
	}
}

func TestMeeseeksManagerMarkOrphaned(t *testing.T) {
	mm := NewMeeseeksManager()
	cfg := MeeseeksConfig{Enabled: true, Limit: 5}

	mks, _ := mm.Spawn(&models.MeeseeksSpawnRequest{Task: "t1"}, "agent1", "m1", nil, cfg)

	ids := mm.MarkOrphaned("agent1")
	if len(ids) != 1 || ids[0] != mks.ID {
		t.Errorf("MarkOrphaned() = %v, want [%s]", ids, mks.ID)
	}

	got, _ := mm.Get(mks.ID)
	if !got.Orphaned {
		t.Error("expected Meeseeks to be orphaned")
	}
}

func TestMeeseeksManagerRemove(t *testing.T) {
	mm := NewMeeseeksManager()
	cfg := MeeseeksConfig{Enabled: true, Limit: 5}

	mks, _ := mm.Spawn(&models.MeeseeksSpawnRequest{Task: "t1"}, "agent1", "m1", nil, cfg)
	mm.Remove(mks.ID)

	_, err := mm.Get(mks.ID)
	if err == nil {
		t.Error("expected error after Remove")
	}
	if mm.CountByParent("agent1") != 0 {
		t.Error("expected 0 count after Remove")
	}
}

func TestMeeseeksManagerUpdateStatus(t *testing.T) {
	mm := NewMeeseeksManager()
	cfg := MeeseeksConfig{Enabled: true, Limit: 5}

	mks, _ := mm.Spawn(&models.MeeseeksSpawnRequest{Task: "t1"}, "agent1", "m1", nil, cfg)

	if err := mm.UpdateStatus(mks.ID, models.MeeseeksStatusWorking); err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}
	got, _ := mm.Get(mks.ID)
	if got.Status != models.MeeseeksStatusWorking {
		t.Errorf("status = %s, want working", got.Status)
	}

	if err := mm.UpdateStatus(mks.ID, models.MeeseeksStatusCompleted); err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}
	got, _ = mm.Get(mks.ID)
	if got.CompletedAt == nil {
		t.Error("expected CompletedAt to be set on completion")
	}
}

func TestMeeseeksManagerUpdateBudgetUsed(t *testing.T) {
	mm := NewMeeseeksManager()
	cfg := MeeseeksConfig{Enabled: true, Limit: 5, Budget: 0.10}

	mks, _ := mm.Spawn(&models.MeeseeksSpawnRequest{Task: "t1"}, "agent1", "m1", nil, cfg)

	if err := mm.UpdateBudgetUsed(mks.ID, 0.03); err != nil {
		t.Fatalf("UpdateBudgetUsed() error = %v", err)
	}
	got, _ := mm.Get(mks.ID)
	if got.BudgetUsed != 0.03 {
		t.Errorf("BudgetUsed = %f, want 0.03", got.BudgetUsed)
	}
}

// TestSpawn_DelegationBounds validates ASK Tenet 11: delegation cannot exceed delegator scope.
func TestSpawn_DelegationBounds(t *testing.T) {
	cfg := MeeseeksConfig{Enabled: true, Limit: 10, Model: "haiku", Budget: 0.10}

	t.Run("parent restricted child requests subset — OK", func(t *testing.T) {
		mm := NewMeeseeksManager()
		parentTools := []string{"read_file", "write_file", "execute_command"}
		req := &models.MeeseeksSpawnRequest{
			Task:  "subset task",
			Tools: []string{"read_file", "write_file"},
		}
		_, err := mm.Spawn(req, "parent-agent", "mission-1", parentTools, cfg)
		if err != nil {
			t.Errorf("expected success for tool subset, got error: %v", err)
		}
	})

	t.Run("parent restricted child requests tool not in set — FAIL with ASK tenet 11", func(t *testing.T) {
		mm := NewMeeseeksManager()
		parentTools := []string{"read_file", "write_file"}
		req := &models.MeeseeksSpawnRequest{
			Task:  "exceeds scope task",
			Tools: []string{"read_file", "execute_command"}, // execute_command not in parent set
		}
		_, err := mm.Spawn(req, "parent-agent", "mission-1", parentTools, cfg)
		if err == nil {
			t.Error("expected error when child requests tool not in parent's set")
		}
		if err != nil {
			if !strings.Contains(err.Error(), "ASK tenet 11") {
				t.Errorf("expected error to contain 'ASK tenet 11', got: %v", err)
			}
		}
	})

	t.Run("parent restricted child requests empty tools — OK (inherits parent scope)", func(t *testing.T) {
		mm := NewMeeseeksManager()
		parentTools := []string{"read_file", "write_file"}
		req := &models.MeeseeksSpawnRequest{
			Task:  "empty tools task",
			Tools: []string{}, // empty — inherits parent scope
		}
		_, err := mm.Spawn(req, "parent-agent", "mission-1", parentTools, cfg)
		if err != nil {
			t.Errorf("expected success when child requests empty tools, got error: %v", err)
		}
	})

	t.Run("parent unrestricted child requests anything — OK", func(t *testing.T) {
		mm := NewMeeseeksManager()
		parentTools := []string{} // parent has no restrictions
		req := &models.MeeseeksSpawnRequest{
			Task:  "unrestricted task",
			Tools: []string{"read_file", "write_file", "execute_command", "network_call"},
		}
		_, err := mm.Spawn(req, "parent-agent", "mission-1", parentTools, cfg)
		if err != nil {
			t.Errorf("expected success when parent has no restrictions, got error: %v", err)
		}
	})
}

