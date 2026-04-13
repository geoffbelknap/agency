package orchestrate

import (
	"testing"

	"github.com/geoffbelknap/agency/internal/models"
)

func TestMissionHealthTargetsForCoordinatorTeamIncludesCoverage(t *testing.T) {
	home := t.TempDir()
	makeTestTeam(t, home, models.TeamConfig{
		Name:        "alpha-team",
		Coordinator: "coord-agent",
		Coverage:    "cover-agent",
		Members: []models.TeamMember{
			{Name: "coord-agent", Type: "agent", AgentType: "coordinator"},
			{Name: "cover-agent", Type: "agent"},
		},
	})

	targets, err := missionHealthTargets(home, &models.Mission{
		Name:         "team-health",
		Description:  "health",
		Instructions: "health",
		AssignedTo:   "alpha-team",
		AssignedType: "team",
	})
	if err != nil {
		t.Fatalf("missionHealthTargets: %v", err)
	}
	if len(targets) != 2 || targets[0] != "coord-agent" || targets[1] != "cover-agent" {
		t.Fatalf("unexpected targets: %#v", targets)
	}
}

func TestMissionHealthCheckerResolvesTeamTargets(t *testing.T) {
	home := t.TempDir()
	makeTestAgent(t, home, "coord-agent")
	makeTestAgent(t, home, "cover-agent")
	makeTestTeam(t, home, models.TeamConfig{
		Name:        "alpha-team",
		Coordinator: "coord-agent",
		Coverage:    "cover-agent",
		Members: []models.TeamMember{
			{Name: "coord-agent", Type: "agent", AgentType: "coordinator"},
			{Name: "cover-agent", Type: "agent"},
		},
	})

	resp := (&MissionHealthChecker{Home: home}).CheckHealth(&models.Mission{
		Name:         "team-health",
		Status:       "active",
		AssignedTo:   "alpha-team",
		AssignedType: "team",
	})

	var passCount int
	for _, check := range resp.Checks {
		if check.Name == "agent_running" && check.Status == "pass" {
			passCount++
		}
	}
	if passCount != 2 {
		t.Fatalf("expected two passing agent_running checks, got %#v", resp.Checks)
	}
}
