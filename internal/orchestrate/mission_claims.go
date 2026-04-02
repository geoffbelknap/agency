package orchestrate

import (
	"sync"
	"time"
)

// MissionClaim tracks which agent has claimed a specific event for a mission.
type MissionClaim struct {
	MissionID string    `json:"mission_id"`
	EventKey  string    `json:"event_key"`
	AgentName string    `json:"agent_name"`
	ClaimedAt time.Time `json:"claimed_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// MissionClaimRegistry manages event claims for no-coordinator team missions.
// First agent to claim wins. Claims are convention-based (prompt-enforced),
// but the registry provides a deterministic answer to "who claimed this?"
type MissionClaimRegistry struct {
	mu     sync.RWMutex
	claims map[string]*MissionClaim // keyed by missionID + ":" + eventKey
}

// NewMissionClaimRegistry creates a new claim registry.
func NewMissionClaimRegistry() *MissionClaimRegistry {
	return &MissionClaimRegistry{
		claims: make(map[string]*MissionClaim),
	}
}

// Claim attempts to claim an event for a mission. Returns true if claimed,
// false if already claimed by another agent. The holder name is always returned.
func (mcr *MissionClaimRegistry) Claim(missionID, eventKey, agentName string) (bool, string) {
	mcr.mu.Lock()
	defer mcr.mu.Unlock()
	key := missionID + ":" + eventKey
	if existing, ok := mcr.claims[key]; ok {
		if time.Now().Before(existing.ExpiresAt) {
			return existing.AgentName == agentName, existing.AgentName
		}
		// Expired — allow reclaim.
	}
	mcr.claims[key] = &MissionClaim{
		MissionID: missionID,
		EventKey:  eventKey,
		AgentName: agentName,
		ClaimedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	return true, agentName
}

// Release releases a claim.
func (mcr *MissionClaimRegistry) Release(missionID, eventKey string) {
	mcr.mu.Lock()
	defer mcr.mu.Unlock()
	delete(mcr.claims, missionID+":"+eventKey)
}

// CleanExpired removes expired claims.
func (mcr *MissionClaimRegistry) CleanExpired() {
	mcr.mu.Lock()
	defer mcr.mu.Unlock()
	now := time.Now()
	for k, c := range mcr.claims {
		if now.After(c.ExpiresAt) {
			delete(mcr.claims, k)
		}
	}
}
