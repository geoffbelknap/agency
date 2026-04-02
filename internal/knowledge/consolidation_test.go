package knowledge

import (
	"fmt"
	"testing"
	"time"
)

func TestPruneProcedures_UnderLimit(t *testing.T) {
	procs := makeProcedures(5, "success")
	result := PruneProcedures(procs, DefaultConsolidationConfig())
	if result.Deleted != 0 {
		t.Errorf("expected 0 deletions for 5 procedures, got %d", result.Deleted)
	}
	if result.NeedsConsolidate {
		t.Error("should not need consolidation for 5 procedures")
	}
}

func TestPruneProcedures_OverSuccessLimit(t *testing.T) {
	procs := makeProcedures(25, "success")
	cfg := DefaultConsolidationConfig()
	result := PruneProcedures(procs, cfg)
	if result.Deleted != 5 {
		t.Errorf("expected 5 deletions (25 - 20 max), got %d", result.Deleted)
	}
	if result.SuccessKept != 20 {
		t.Errorf("expected 20 kept, got %d", result.SuccessKept)
	}
}

func TestPruneProcedures_OverFailedLimit(t *testing.T) {
	procs := makeProcedures(15, "failed")
	cfg := DefaultConsolidationConfig()
	result := PruneProcedures(procs, cfg)
	if result.Deleted != 5 {
		t.Errorf("expected 5 deletions (15 - 10 max), got %d", result.Deleted)
	}
	if result.FailedKept != 10 {
		t.Errorf("expected 10 kept, got %d", result.FailedKept)
	}
}

func TestPruneProcedures_MixedOutcomes(t *testing.T) {
	var procs []ProcedureMetadata
	procs = append(procs, makeProcedures(22, "success")...)
	procs = append(procs, makeProcedures(12, "failed")...)
	cfg := DefaultConsolidationConfig()
	result := PruneProcedures(procs, cfg)
	// 22 success - keep 20 = 2 deleted
	// 12 failed - keep 10 = 2 deleted
	if result.Deleted != 4 {
		t.Errorf("expected 4 deletions, got %d", result.Deleted)
	}
}

func TestPruneProcedures_ConsolidationFlag(t *testing.T) {
	procs := makeProcedures(50, "success")
	cfg := DefaultConsolidationConfig()
	result := PruneProcedures(procs, cfg)
	if !result.NeedsConsolidate {
		t.Error("should flag consolidation at 50 procedures")
	}
}

func TestPruneProcedures_Empty(t *testing.T) {
	result := PruneProcedures(nil, DefaultConsolidationConfig())
	if result.Deleted != 0 || result.TotalBefore != 0 {
		t.Error("empty input should produce zero result")
	}
}

func TestCheckEpisodeRetention_NoExpired(t *testing.T) {
	episodes := makeEpisodes(5, time.Now().AddDate(0, 0, -30)) // 30 days old
	result := CheckEpisodeRetention(episodes, 90)
	if result.EpisodesExpired != 0 {
		t.Errorf("expected 0 expired, got %d", result.EpisodesExpired)
	}
}

func TestCheckEpisodeRetention_SomeExpired(t *testing.T) {
	var episodes []EpisodeMetadata
	episodes = append(episodes, makeEpisodes(3, time.Now().AddDate(0, 0, -100))...) // 100 days old
	episodes = append(episodes, makeEpisodes(2, time.Now().AddDate(0, 0, -30))...)  // 30 days old
	result := CheckEpisodeRetention(episodes, 90)
	if result.EpisodesExpired != 3 {
		t.Errorf("expected 3 expired, got %d", result.EpisodesExpired)
	}
}

func TestCheckEpisodeRetention_AllExpired(t *testing.T) {
	episodes := makeEpisodes(5, time.Now().AddDate(0, -6, 0)) // 6 months old
	result := CheckEpisodeRetention(episodes, 90)
	if result.EpisodesExpired != 5 {
		t.Errorf("expected 5 expired, got %d", result.EpisodesExpired)
	}
}

// --- helpers ---

func makeProcedures(n int, outcome string) []ProcedureMetadata {
	procs := make([]ProcedureMetadata, n)
	base := time.Now()
	for i := 0; i < n; i++ {
		procs[i] = ProcedureMetadata{
			TaskID:    fmt.Sprintf("task-%d", i),
			MissionID: "mission-1",
			Outcome:   outcome,
			Timestamp: base.Add(-time.Duration(i) * time.Hour), // most recent first
		}
	}
	return procs
}

func makeEpisodes(n int, baseTime time.Time) []EpisodeMetadata {
	eps := make([]EpisodeMetadata, n)
	for i := 0; i < n; i++ {
		eps[i] = EpisodeMetadata{
			TaskID:    fmt.Sprintf("task-%d", i),
			AgentName: "agent-1",
			MissionID: "mission-1",
			Timestamp: baseTime.Add(-time.Duration(i) * time.Hour),
		}
	}
	return eps
}
