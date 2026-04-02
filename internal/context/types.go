package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type Severity int

const (
	SeverityLow      Severity = 1
	SeverityMedium   Severity = 2
	SeverityHigh     Severity = 3
	SeverityCritical Severity = 4
)

func (s Severity) String() string {
	switch s {
	case SeverityLow:
		return "LOW"
	case SeverityMedium:
		return "MEDIUM"
	case SeverityHigh:
		return "HIGH"
	case SeverityCritical:
		return "CRITICAL"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", s)
	}
}

func ParseSeverity(s string) (Severity, error) {
	switch s {
	case "LOW":
		return SeverityLow, nil
	case "MEDIUM":
		return SeverityMedium, nil
	case "HIGH":
		return SeverityHigh, nil
	case "CRITICAL":
		return SeverityCritical, nil
	default:
		return 0, fmt.Errorf("unknown severity: %q", s)
	}
}

func (s Severity) CanEscalateTo(target Severity) bool {
	return target > s
}

func (s Severity) AckTimeout() time.Duration {
	switch s {
	case SeverityLow:
		return 60 * time.Second
	case SeverityMedium:
		return 30 * time.Second
	case SeverityHigh:
		return 10 * time.Second
	case SeverityCritical:
		return 5 * time.Second
	default:
		return 30 * time.Second
	}
}

func (s Severity) HaltTimeout() time.Duration {
	switch s {
	case SeverityCritical:
		return 15 * time.Second
	default:
		return s.AckTimeout() * 2
	}
}

type ChangeStatus string

const (
	StatusPending      ChangeStatus = "pending"
	StatusAcked        ChangeStatus = "acked"
	StatusTimeout      ChangeStatus = "timeout"
	StatusHashMismatch ChangeStatus = "hash_mismatch"
	StatusHalted       ChangeStatus = "halted"
)

type ConstraintChange struct {
	ChangeID    string                 `json:"change_id"`
	Agent       string                 `json:"agent"`
	Version     int                    `json:"version"`
	Severity    Severity               `json:"severity"`
	Constraints map[string]interface{} `json:"constraints"`
	Hash        string                 `json:"hash"`
	Reason      string                 `json:"reason"`
	Initiator   string                 `json:"initiator"`
	Timestamp   time.Time              `json:"timestamp"`
	Status      ChangeStatus           `json:"status"`
	AckedAt     *time.Time             `json:"acked_at,omitempty"`
}

type AckReport struct {
	Type      string       `json:"type"`
	Agent     string       `json:"agent"`
	ChangeID  string       `json:"change_id"`
	Version   int          `json:"version"`
	Status    ChangeStatus `json:"status"`
	BodyHash  string       `json:"body_hash,omitempty"`
	Timestamp time.Time    `json:"timestamp"`
}

type WSPushMessage struct {
	Type        string                 `json:"type"`
	Agent       string                 `json:"agent"`
	ChangeID    string                 `json:"change_id"`
	Version     int                    `json:"version"`
	Severity    string                 `json:"severity"`
	Constraints map[string]interface{} `json:"constraints"`
	Hash        string                 `json:"hash"`
	Reason      string                 `json:"reason"`
	Timestamp   string                 `json:"timestamp"`
}

// HashConstraints computes SHA-256 of the canonical JSON-serialized constraint set.
// Canonical form: sorted keys, no trailing whitespace, no HTML escaping, compact encoding.
// IMPORTANT: Python Body runtime must use the same canonical form:
//   json.dumps(constraints, sort_keys=True, separators=(",", ":"))
// Go's json.Marshal already sorts map keys and uses compact encoding.
func HashConstraints(constraints map[string]interface{}) string {
	data, _ := json.Marshal(constraints)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
