package orchestrate

import "github.com/geoffbelknap/agency/internal/models"

// missionHealthTargets resolves the agent names that count as healthy
// execution targets for a mission.
//
// For team missions with a coordinator, the coverage agent is also considered a
// valid health target so coordinator failover does not get preempted by a
// false auto-pause the moment the coordinator stops.
func missionHealthTargets(home string, m *models.Mission) ([]string, error) {
	if m.AssignedTo == "" {
		return nil, nil
	}
	if m.AssignedType != "team" {
		return []string{m.AssignedTo}, nil
	}

	mm := NewMissionManager(home)
	teamCfg, err := mm.LoadTeamConfig(m.AssignedTo)
	if err != nil {
		return nil, err
	}
	if teamCfg.Coordinator != "" {
		targets := []string{teamCfg.Coordinator}
		if teamCfg.Coverage != "" && teamCfg.Coverage != teamCfg.Coordinator {
			targets = append(targets, teamCfg.Coverage)
		}
		return targets, nil
	}

	var agents []string
	for _, member := range teamCfg.Members {
		if member.Type == "" || member.Type == "agent" {
			agents = append(agents, member.Name)
		}
	}
	return agents, nil
}
