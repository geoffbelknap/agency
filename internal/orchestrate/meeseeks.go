package orchestrate

import (
	"fmt"
	"sync"
	"time"

	"github.com/geoffbelknap/agency/internal/models"
)

const (
	DefaultMeeseeksLimit  = 5    // max concurrent per parent
	DefaultMeeseeksRate   = 10   // max spawns per minute per parent
	DefaultMeeseeksBudget = 0.05 // USD
	DefaultMeeseeksModel  = "haiku"
)

// MeeseeksConfig holds mission-level Meeseeks settings.
type MeeseeksConfig struct {
	Enabled bool
	Limit   int
	Model   string
	Budget  float64
}

// MeeseeksManager tracks active Meeseeks in memory (ephemeral — no persistence).
type MeeseeksManager struct {
	mu       sync.RWMutex
	active   map[string]*models.Meeseeks   // id -> Meeseeks
	byParent map[string]map[string]struct{} // parent -> set of ids
	spawnLog map[string][]time.Time         // parent -> recent spawn timestamps (rate limiting)
}

// NewMeeseeksManager creates a new empty MeeseeksManager.
func NewMeeseeksManager() *MeeseeksManager {
	return &MeeseeksManager{
		active:   make(map[string]*models.Meeseeks),
		byParent: make(map[string]map[string]struct{}),
		spawnLog: make(map[string][]time.Time),
	}
}

// Spawn validates limits and creates a new Meeseeks record.
func (mm *MeeseeksManager) Spawn(req *models.MeeseeksSpawnRequest, parent string, parentMissionID string, parentTools []string, cfg MeeseeksConfig) (*models.Meeseeks, error) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// Concurrent limit
	limit := cfg.Limit
	if limit <= 0 {
		limit = DefaultMeeseeksLimit
	}
	if ids, ok := mm.byParent[parent]; ok && len(ids) >= limit {
		return nil, fmt.Errorf("concurrent Meeseeks limit reached (%d/%d) for parent %s", len(ids), limit, parent)
	}

	// Rate limit
	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)
	recent := mm.spawnLog[parent]
	var filtered []time.Time
	for _, t := range recent {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) >= DefaultMeeseeksRate {
		return nil, fmt.Errorf("spawn rate limit exceeded (%d/min) for parent %s", DefaultMeeseeksRate, parent)
	}

	// Validate tools subset of parent's
	if len(req.Tools) > 0 && len(parentTools) > 0 {
		parentSet := make(map[string]bool, len(parentTools))
		for _, t := range parentTools {
			parentSet[t] = true
		}
		for _, t := range req.Tools {
			if !parentSet[t] {
				return nil, fmt.Errorf("requested tool %q not in parent's tool set (ASK tenet 11: delegation cannot exceed delegator scope)", t)
			}
		}
	}

	// Defaults
	model := req.Model
	if model == "" {
		model = cfg.Model
		if model == "" {
			model = DefaultMeeseeksModel
		}
	}
	budget := req.Budget
	if budget <= 0 {
		budget = cfg.Budget
		if budget <= 0 {
			budget = DefaultMeeseeksBudget
		}
	}

	id := models.NewMeeseeksID()
	shortID := id[4:] // strip "mks-" prefix for container naming

	mks := &models.Meeseeks{
		ID:              id,
		ParentAgent:     parent,
		ParentMissionID: parentMissionID,
		Task:            req.Task,
		Tools:           req.Tools,
		Model:           model,
		Budget:          budget,
		BudgetUsed:      0,
		Channel:         req.Channel,
		Status:          models.MeeseeksStatusSpawned,
		SpawnedAt:       now,
		ContainerName:   fmt.Sprintf("%s-mks-%s-workspace", prefix, shortID),
		EnforcerName:    fmt.Sprintf("%s-mks-%s-enforcer", prefix, shortID),
		NetworkName:     fmt.Sprintf("%s-mks-%s-internal", prefix, shortID),
	}

	mm.active[id] = mks
	if mm.byParent[parent] == nil {
		mm.byParent[parent] = make(map[string]struct{})
	}
	mm.byParent[parent][id] = struct{}{}
	mm.spawnLog[parent] = append(filtered, now)

	return mks, nil
}

// Get returns a Meeseeks by ID.
func (mm *MeeseeksManager) Get(id string) (*models.Meeseeks, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	mks, ok := mm.active[id]
	if !ok {
		return nil, fmt.Errorf("meeseeks %s not found", id)
	}
	return mks, nil
}

// List returns all active Meeseeks, optionally filtered by parent.
func (mm *MeeseeksManager) List(parent string) []*models.Meeseeks {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	var result []*models.Meeseeks
	if parent != "" {
		ids, ok := mm.byParent[parent]
		if !ok {
			return result
		}
		for id := range ids {
			if mks, exists := mm.active[id]; exists {
				result = append(result, mks)
			}
		}
	} else {
		for _, mks := range mm.active {
			result = append(result, mks)
		}
	}
	return result
}

// UpdateStatus transitions a Meeseeks to a new status.
func (mm *MeeseeksManager) UpdateStatus(id string, status models.MeeseeksStatus) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mks, ok := mm.active[id]
	if !ok {
		return fmt.Errorf("meeseeks %s not found", id)
	}
	mks.Status = status
	if status == models.MeeseeksStatusCompleted || status == models.MeeseeksStatusTerminated {
		now := time.Now()
		mks.CompletedAt = &now
	}
	return nil
}

// UpdateBudgetUsed records spend against a Meeseeks budget.
func (mm *MeeseeksManager) UpdateBudgetUsed(id string, cost float64) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mks, ok := mm.active[id]
	if !ok {
		return fmt.Errorf("meeseeks %s not found", id)
	}
	mks.BudgetUsed += cost
	return nil
}

// Remove deletes a Meeseeks from the active set (after container cleanup).
func (mm *MeeseeksManager) Remove(id string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mks, ok := mm.active[id]
	if !ok {
		return
	}
	parent := mks.ParentAgent
	delete(mm.active, id)
	if ids, ok := mm.byParent[parent]; ok {
		delete(ids, id)
		if len(ids) == 0 {
			delete(mm.byParent, parent)
		}
	}
}

// CountByParent returns the number of active Meeseeks for a parent.
func (mm *MeeseeksManager) CountByParent(parent string) int {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	return len(mm.byParent[parent])
}

// MarkOrphaned marks all Meeseeks for a parent as orphaned.
// Returns the IDs of orphaned Meeseeks.
func (mm *MeeseeksManager) MarkOrphaned(parent string) []string {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	var ids []string
	if set, ok := mm.byParent[parent]; ok {
		for id := range set {
			if mks, exists := mm.active[id]; exists {
				mks.Orphaned = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}
