package knowledge

import (
	"time"
)

// ConsolidationConfig holds retention settings for procedures and episodes.
type ConsolidationConfig struct {
	MaxSuccessfulProcedures int // max successful procedures to keep per mission (default: 20)
	MaxFailedProcedures     int // max failed procedures to keep per mission (default: 10)
	ConsolidateAfter        int // trigger consolidation after this many procedures (default: 50)
	EpisodeRetentionDays    int // days before episodes are eligible for narrative consolidation (default: 90)
}

// DefaultConsolidationConfig returns sensible defaults.
func DefaultConsolidationConfig() ConsolidationConfig {
	return ConsolidationConfig{
		MaxSuccessfulProcedures: 20,
		MaxFailedProcedures:     10,
		ConsolidateAfter:        50,
		EpisodeRetentionDays:    90,
	}
}

// ProcedurePruneResult records what was pruned.
type ProcedurePruneResult struct {
	MissionID        string
	TotalBefore      int
	SuccessKept      int
	FailedKept       int
	Deleted          int
	NeedsConsolidate bool // true if total was >= ConsolidateAfter
}

// EpisodeRetentionResult records what needs consolidation.
type EpisodeRetentionResult struct {
	AgentName       string
	MissionID       string
	EpisodesExpired int // episodes older than retention_days
	OldestExpired   time.Time
}

// ProcedureMetadata is the minimal info needed for pruning decisions.
type ProcedureMetadata struct {
	TaskID    string
	MissionID string
	Outcome   string // success, partial, failed
	Timestamp time.Time
}

// EpisodeMetadata is the minimal info needed for retention checks.
type EpisodeMetadata struct {
	TaskID    string
	AgentName string
	MissionID string
	Timestamp time.Time
}

// PruneProcedures checks procedure counts for a mission and returns what should be pruned.
// This is a pure function — it takes procedure metadata and returns pruning decisions.
// The caller is responsible for executing the deletes against the knowledge graph.
func PruneProcedures(procedures []ProcedureMetadata, cfg ConsolidationConfig) ProcedurePruneResult {
	result := ProcedurePruneResult{
		TotalBefore: len(procedures),
	}

	if len(procedures) == 0 {
		return result
	}
	result.MissionID = procedures[0].MissionID

	if len(procedures) >= cfg.ConsolidateAfter {
		result.NeedsConsolidate = true
	}

	// Separate by outcome
	var successful, failed, partial []ProcedureMetadata
	for _, p := range procedures {
		switch p.Outcome {
		case "failed":
			failed = append(failed, p)
		case "success":
			successful = append(successful, p)
		default: // partial
			partial = append(partial, p)
		}
	}

	// Sort each group by timestamp descending (most recent first)
	// Assume input is already sorted by timestamp desc from the knowledge graph query

	// Apply retention limits
	result.SuccessKept = minInt(len(successful)+len(partial), cfg.MaxSuccessfulProcedures)
	result.FailedKept = minInt(len(failed), cfg.MaxFailedProcedures)

	// Count deletions as: total - kept
	result.Deleted = len(procedures) - result.SuccessKept - result.FailedKept

	return result
}

// CheckEpisodeRetention checks if any episodes are older than the retention period.
func CheckEpisodeRetention(episodes []EpisodeMetadata, retentionDays int) EpisodeRetentionResult {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result := EpisodeRetentionResult{}

	for _, ep := range episodes {
		if ep.Timestamp.Before(cutoff) {
			result.EpisodesExpired++
			if result.OldestExpired.IsZero() || ep.Timestamp.Before(result.OldestExpired) {
				result.OldestExpired = ep.Timestamp
			}
			if result.AgentName == "" {
				result.AgentName = ep.AgentName
				result.MissionID = ep.MissionID
			}
		}
	}

	return result
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
