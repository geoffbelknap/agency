package context

import (
	"testing"
	"time"

	agencysecurity "github.com/geoffbelknap/agency/internal/security"
)

func TestSeverityString(t *testing.T) {
	tests := []struct {
		s    Severity
		want string
	}{
		{SeverityLow, "LOW"},
		{SeverityMedium, "MEDIUM"},
		{SeverityHigh, "HIGH"},
		{SeverityCritical, "CRITICAL"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestSeverityRiskLevel(t *testing.T) {
	tests := []struct {
		s    Severity
		want agencysecurity.RiskLevel
	}{
		{SeverityLow, agencysecurity.RiskLow},
		{SeverityMedium, agencysecurity.RiskMedium},
		{SeverityHigh, agencysecurity.RiskHigh},
		{SeverityCritical, agencysecurity.RiskCritical},
	}
	for _, tt := range tests {
		if got := tt.s.RiskLevel(); got != tt.want {
			t.Errorf("%s.RiskLevel() = %q, want %q", tt.s, got, tt.want)
		}
		if got := SeverityFromRiskLevel(tt.want); got != tt.s {
			t.Errorf("SeverityFromRiskLevel(%q) = %s, want %s", tt.want, got, tt.s)
		}
	}
}

func TestSeverityCanEscalateTo(t *testing.T) {
	tests := []struct {
		from, to Severity
		want     bool
	}{
		{SeverityLow, SeverityHigh, true},
		{SeverityHigh, SeverityLow, false},
		{SeverityMedium, SeverityMedium, false},
	}
	for _, tt := range tests {
		if got := tt.from.CanEscalateTo(tt.to); got != tt.want {
			t.Errorf("%s.CanEscalateTo(%s) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestChangeStatusValues(t *testing.T) {
	statuses := []ChangeStatus{StatusPending, StatusAcked, StatusTimeout, StatusHashMismatch, StatusHalted}
	seen := map[ChangeStatus]bool{}
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("duplicate status: %s", s)
		}
		seen[s] = true
	}
}

func TestSeverityAckTimeout(t *testing.T) {
	tests := []struct {
		s    Severity
		want time.Duration
	}{
		{SeverityLow, 60 * time.Second},
		{SeverityMedium, 30 * time.Second},
		{SeverityHigh, 10 * time.Second},
		{SeverityCritical, 5 * time.Second},
	}
	for _, tt := range tests {
		if got := tt.s.AckTimeout(); got != tt.want {
			t.Errorf("%s.AckTimeout() = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestSeverityHaltTimeout(t *testing.T) {
	tests := []struct {
		s    Severity
		want time.Duration
	}{
		{SeverityLow, 120 * time.Second},
		{SeverityMedium, 60 * time.Second},
		{SeverityHigh, 20 * time.Second},
		{SeverityCritical, 15 * time.Second},
	}
	for _, tt := range tests {
		if got := tt.s.HaltTimeout(); got != tt.want {
			t.Errorf("%s.HaltTimeout() = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestHashConstraintsDeterministic(t *testing.T) {
	// Known input → known output. This is the cross-language contract with Python:
	// json.dumps({"foo":"bar"}, sort_keys=True, separators=(",", ":")) → {"foo":"bar"}
	// SHA-256 of that string.
	constraints := map[string]interface{}{"foo": "bar"}
	hash := HashConstraints(constraints)

	// Verify determinism: same input always produces same hash
	if hash2 := HashConstraints(constraints); hash != hash2 {
		t.Errorf("non-deterministic: %s != %s", hash, hash2)
	}

	// Verify key ordering doesn't matter
	constraints2 := map[string]interface{}{"b": 2, "a": 1}
	constraints3 := map[string]interface{}{"a": 1, "b": 2}
	if HashConstraints(constraints2) != HashConstraints(constraints3) {
		t.Error("key ordering affected hash")
	}

	// Verify hash is 64-char hex (SHA-256)
	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash))
	}
}
