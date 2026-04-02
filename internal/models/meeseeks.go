// agency-gateway/internal/models/meeseeks.go
package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MeeseeksStatus represents the lifecycle state of a Meeseeks.
type MeeseeksStatus string

const (
	MeeseeksStatusSpawned    MeeseeksStatus = "spawned"
	MeeseeksStatusWorking    MeeseeksStatus = "working"
	MeeseeksStatusCompleted  MeeseeksStatus = "completed"
	MeeseeksStatusDistressed MeeseeksStatus = "distressed"
	MeeseeksStatusTerminated MeeseeksStatus = "terminated"
)

// MeeseeksSpawnRequest is the validated input from spawn_meeseeks tool calls.
type MeeseeksSpawnRequest struct {
	Task    string   `json:"task" validate:"required"`
	Tools   []string `json:"tools,omitempty"`
	Model   string   `json:"model,omitempty"`
	Budget  float64  `json:"budget,omitempty"`
	Channel string   `json:"channel,omitempty"`
}

// Meeseeks represents an ephemeral single-purpose agent.
type Meeseeks struct {
	ID              string         `json:"id"`
	ParentAgent     string         `json:"parent_agent"`
	ParentMissionID string         `json:"parent_mission_id,omitempty"`
	Task            string         `json:"task"`
	Tools           []string       `json:"tools"`
	Model           string         `json:"model"`
	Budget          float64        `json:"budget"`
	BudgetUsed      float64        `json:"budget_used"`
	Channel         string         `json:"channel,omitempty"`
	Status          MeeseeksStatus `json:"status"`
	Orphaned        bool           `json:"orphaned"`
	SpawnedAt       time.Time      `json:"spawned_at"`
	CompletedAt     *time.Time     `json:"completed_at,omitempty"`
	ContainerName   string         `json:"container_name"`
	EnforcerName    string         `json:"enforcer_name"`
	NetworkName     string         `json:"network_name"`
}

var meeseeksIDPrefix = "mks-"
var meeseeksTaskMaxLen = 2000

// NewMeeseeksID generates a new Meeseeks identifier with the mks- prefix.
func NewMeeseeksID() string {
	short := uuid.New().String()[:8]
	return meeseeksIDPrefix + short
}

var validMeeseeksModels = map[string]bool{
	"haiku": true, "sonnet": true,
}

// Validate checks the spawn request fields.
func (r *MeeseeksSpawnRequest) Validate() error {
	if r.Task == "" {
		return fmt.Errorf("task is required")
	}
	if len(r.Task) > meeseeksTaskMaxLen {
		return fmt.Errorf("task exceeds max length (%d chars)", meeseeksTaskMaxLen)
	}
	if r.Model != "" && !validMeeseeksModels[r.Model] {
		return fmt.Errorf("invalid model: %s (allowed: haiku, sonnet)", r.Model)
	}
	if r.Budget < 0 {
		return fmt.Errorf("budget must be non-negative")
	}
	return nil
}
