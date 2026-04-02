package orchestrate

import (
	"testing"
	"time"
)

func TestMissionClaimRegistry_Claim(t *testing.T) {
	r := NewMissionClaimRegistry()

	// First claim succeeds.
	ok, holder := r.Claim("m1", "INC-001", "agent-a")
	if !ok || holder != "agent-a" {
		t.Fatalf("expected claim success by agent-a, got ok=%v holder=%s", ok, holder)
	}

	// Second claim by different agent fails.
	ok, holder = r.Claim("m1", "INC-001", "agent-b")
	if ok {
		t.Fatal("expected claim to fail for agent-b")
	}
	if holder != "agent-a" {
		t.Fatalf("expected holder agent-a, got %s", holder)
	}

	// Same agent re-claiming succeeds (idempotent).
	ok, holder = r.Claim("m1", "INC-001", "agent-a")
	if !ok || holder != "agent-a" {
		t.Fatalf("expected idempotent claim success, got ok=%v holder=%s", ok, holder)
	}
}

func TestMissionClaimRegistry_Release(t *testing.T) {
	r := NewMissionClaimRegistry()
	r.Claim("m1", "INC-001", "agent-a")
	r.Release("m1", "INC-001")

	// After release, a different agent can claim.
	ok, holder := r.Claim("m1", "INC-001", "agent-b")
	if !ok || holder != "agent-b" {
		t.Fatalf("expected claim success after release, got ok=%v holder=%s", ok, holder)
	}
}

func TestMissionClaimRegistry_Expiry(t *testing.T) {
	r := NewMissionClaimRegistry()
	r.Claim("m1", "INC-001", "agent-a")

	// Manually expire the claim.
	r.mu.Lock()
	r.claims["m1:INC-001"].ExpiresAt = time.Now().Add(-1 * time.Second)
	r.mu.Unlock()

	// Expired claim allows reclaim.
	ok, holder := r.Claim("m1", "INC-001", "agent-b")
	if !ok || holder != "agent-b" {
		t.Fatalf("expected reclaim after expiry, got ok=%v holder=%s", ok, holder)
	}
}

func TestMissionClaimRegistry_CleanExpired(t *testing.T) {
	r := NewMissionClaimRegistry()
	r.Claim("m1", "INC-001", "agent-a")
	r.Claim("m1", "INC-002", "agent-b")

	// Expire one claim.
	r.mu.Lock()
	r.claims["m1:INC-001"].ExpiresAt = time.Now().Add(-1 * time.Second)
	r.mu.Unlock()

	r.CleanExpired()

	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.claims["m1:INC-001"]; ok {
		t.Fatal("expected expired claim to be cleaned")
	}
	if _, ok := r.claims["m1:INC-002"]; !ok {
		t.Fatal("expected active claim to remain")
	}
}
