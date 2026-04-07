package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/geoffbelknap/agency/internal/models"
)

// ── Missions (9 tools) ──────────────────────────────────────────────────────

func registerMissionTools(reg *MCPToolRegistry) {

	// 1. agency_mission_create
	reg.Register(
		"agency_mission_create",
		"Create a new mission with a name, description, and standing instructions. The mission is created in unassigned status. Use cost_mode to control the cost/quality tradeoff (frugal=minimal features, balanced=default, thorough=all features including reflection and LLM evaluation).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Mission name (lowercase alphanumeric with hyphens, 2–63 chars).",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Short description of the mission purpose.",
				},
				"instructions": map[string]interface{}{
					"type":        "string",
					"description": "Standing instructions the assigned agent will follow.",
				},
				"cost_mode": map[string]interface{}{
					"type":        "string",
					"description": "Cost/quality tradeoff: 'frugal' (minimal features), 'balanced' (default — memory capture, cheap evaluation), 'thorough' (reflection, LLM evaluation, full memory).",
					"enum":        []string{"frugal", "balanced", "thorough"},
				},
			},
			"required": []string{"name", "description", "instructions"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			name, _ := args["name"].(string)
			description, _ := args["description"].(string)
			instructions, _ := args["instructions"].(string)
			costMode, _ := args["cost_mode"].(string)

			if name == "" {
				return "Error: name is required", true
			}
			if description == "" {
				return "Error: description is required", true
			}
			if instructions == "" {
				return "Error: instructions is required", true
			}

			m := &models.Mission{
				Name:         name,
				Description:  description,
				Instructions: instructions,
				CostMode:     costMode,
			}
			if err := d.missions.Create(m); err != nil {
				return "Error: " + err.Error(), true
			}
			result := fmt.Sprintf("Mission %q created (id=%s).", m.Name, m.ID)
			if costMode != "" {
				result += fmt.Sprintf(" Cost mode: %s.", costMode)
			}
			return result, false
		},
	)

	// 2. agency_mission_assign
	reg.Register(
		"agency_mission_assign",
		"Assign a mission to an agent or team. The mission must be in unassigned status.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"mission": map[string]interface{}{
					"type":        "string",
					"description": "Mission name.",
				},
				"target": map[string]interface{}{
					"type":        "string",
					"description": "Agent or team name to assign the mission to.",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"description": "Target type: 'agent' (default) or 'team'.",
				},
			},
			"required": []string{"mission", "target"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			mission, _ := args["mission"].(string)
			target, _ := args["target"].(string)
			targetType, _ := args["type"].(string)

			if mission == "" {
				return "Error: mission is required", true
			}
			if target == "" {
				return "Error: target is required", true
			}
			if targetType == "" {
				targetType = "agent"
			}

			if err := d.missions.Assign(mission, target, targetType); err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Mission %q assigned to %s %q.", mission, targetType, target), false
		},
	)

	// 3. agency_mission_pause
	reg.Register(
		"agency_mission_pause",
		"Pause an active mission. The mission must be in active status.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"mission": map[string]interface{}{
					"type":        "string",
					"description": "Mission name.",
				},
			},
			"required": []string{"mission"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			mission, _ := args["mission"].(string)
			if mission == "" {
				return "Error: mission is required", true
			}
			if err := d.missions.Pause(mission, ""); err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Mission %q paused.", mission), false
		},
	)

	// 4. agency_mission_resume
	reg.Register(
		"agency_mission_resume",
		"Resume a paused mission. The mission must be in paused status.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"mission": map[string]interface{}{
					"type":        "string",
					"description": "Mission name.",
				},
			},
			"required": []string{"mission"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			mission, _ := args["mission"].(string)
			if mission == "" {
				return "Error: mission is required", true
			}
			if err := d.missions.Resume(mission); err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Mission %q resumed.", mission), false
		},
	)

	// 5. agency_mission_update
	reg.Register(
		"agency_mission_update",
		"Update the standing instructions for a mission. Increments the mission version.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"mission": map[string]interface{}{
					"type":        "string",
					"description": "Mission name.",
				},
				"instructions": map[string]interface{}{
					"type":        "string",
					"description": "New standing instructions for the mission.",
				},
			},
			"required": []string{"mission", "instructions"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			mission, _ := args["mission"].(string)
			instructions, _ := args["instructions"].(string)

			if mission == "" {
				return "Error: mission is required", true
			}
			if instructions == "" {
				return "Error: instructions is required", true
			}

			existing, err := d.missions.Get(mission)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			updated := &models.Mission{
				Description:      existing.Description,
				Instructions:     instructions,
				Status:           existing.Status,
				AssignedTo:       existing.AssignedTo,
				AssignedType:     existing.AssignedType,
				Triggers:         existing.Triggers,
				Requires:         existing.Requires,
				Health:           existing.Health,
				Budget:           existing.Budget,
				Reflection:       existing.Reflection,
				SuccessCriteria:  existing.SuccessCriteria,
				Fallback:         existing.Fallback,
				ProceduralMemory: existing.ProceduralMemory,
				EpisodicMemory:   existing.EpisodicMemory,
				CostMode:         existing.CostMode,
				MinTaskTier:      existing.MinTaskTier,
			}
			if err := d.missions.Update(mission, updated); err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Mission %q updated to version %d.", mission, existing.Version+1), false
		},
	)

	// 6. agency_mission_complete
	reg.Register(
		"agency_mission_complete",
		"Mark a mission as completed. The mission must be active or paused.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"mission": map[string]interface{}{
					"type":        "string",
					"description": "Mission name.",
				},
			},
			"required": []string{"mission"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			mission, _ := args["mission"].(string)
			if mission == "" {
				return "Error: mission is required", true
			}
			if err := d.missions.Complete(mission); err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Mission %q marked as completed.", mission), false
		},
	)

	// 7. agency_mission_show
	reg.Register(
		"agency_mission_show",
		"Show detailed information about a mission including status, assignment, and instructions.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"mission": map[string]interface{}{
					"type":        "string",
					"description": "Mission name.",
				},
			},
			"required": []string{"mission"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			mission, _ := args["mission"].(string)
			if mission == "" {
				return "Error: mission is required", true
			}
			m, err := d.missions.Get(mission)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			assignedTo := m.AssignedTo
			assignedType := m.AssignedType
			if assignedTo == "" {
				assignedTo = "—"
				assignedType = "—"
			}

			lines := []string{
				fmt.Sprintf("Mission: %s", m.Name),
				fmt.Sprintf("  ID:           %s", m.ID),
				fmt.Sprintf("  Description:  %s", m.Description),
				fmt.Sprintf("  Status:       %s", m.Status),
				fmt.Sprintf("  Version:      %d", m.Version),
				fmt.Sprintf("  Assigned to:  %s", assignedTo),
				fmt.Sprintf("  Assigned type:%s", assignedType),
				"  Instructions:",
			}
			for _, line := range strings.Split(m.Instructions, "\n") {
				lines = append(lines, "    "+line)
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// 8. agency_mission_list
	reg.Register(
		"agency_mission_list",
		"List all missions with their status and assignment.",
		nil,
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			missions, err := d.missions.List()
			if err != nil {
				return "Error: " + err.Error(), true
			}
			if len(missions) == 0 {
				return "Missions (0): none", false
			}

			lines := []string{fmt.Sprintf("Missions (%d):", len(missions))}
			for _, m := range missions {
				assignedTo := m.AssignedTo
				assignedType := m.AssignedType
				if assignedTo == "" {
					assignedTo = "—"
					assignedType = "—"
				}
				lines = append(lines, fmt.Sprintf("  %-24s  %-12s  %-20s  %s",
					m.Name, m.Status, assignedTo, assignedType))
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// 9. agency_mission_history
	reg.Register(
		"agency_mission_history",
		"Show the version history of a mission.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"mission": map[string]interface{}{
					"type":        "string",
					"description": "Mission name.",
				},
			},
			"required": []string{"mission"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			mission, _ := args["mission"].(string)
			if mission == "" {
				return "Error: mission is required", true
			}
			entries, err := d.missions.History(mission)
			if err != nil {
				return "Error: " + err.Error(), true
			}
			if len(entries) == 0 {
				return fmt.Sprintf("Mission %q: no history recorded.", mission), false
			}

			lines := []string{fmt.Sprintf("Mission %q history (%d entries):", mission, len(entries))}
			for _, entry := range entries {
				version, _ := entry["version"]
				timestamp, _ := entry["timestamp"].(string)
				lines = append(lines, fmt.Sprintf("  v%v  %s", version, timestamp))
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// 10. agency_mission_knowledge — query mission-scoped knowledge
	reg.Register(
		"agency_mission_knowledge",
		"Query the knowledge graph scoped to a specific mission. Returns nodes tagged with the given mission's ID. ASK tenet 24: knowledge access is bounded by authorization scope.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"mission": map[string]interface{}{
					"type":        "string",
					"description": "Mission name.",
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Natural-language query to search within the mission's knowledge scope.",
				},
			},
			"required": []string{"mission", "query"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			missionName, _ := args["mission"].(string)
			query, _ := args["query"].(string)
			if missionName == "" {
				return "Error: mission is required", true
			}
			if query == "" {
				return "Error: query is required", true
			}

			m, err := d.missions.Get(missionName)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			result, err := d.knowledge.QueryByMission(context.Background(), query, m.ID)
			if err != nil {
				return "Error querying mission knowledge: " + err.Error(), true
			}
			return string(result), false
		},
	)

	// 11. agency_mission_claim — claim or release a trigger event for a mission
	reg.Register(
		"agency_mission_claim",
		"Claim or release a mission trigger event for deconfliction in no-coordinator teams. First agent to claim an event key wins; others should skip processing that event.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"mission": map[string]interface{}{
					"type":        "string",
					"description": "Mission name.",
				},
				"event_key": map[string]interface{}{
					"type":        "string",
					"description": "Unique identifier for the event to claim (e.g. connector event ID or channel message ID).",
				},
				"agent": map[string]interface{}{
					"type":        "string",
					"description": "Agent name claiming the event.",
				},
				"action": map[string]interface{}{
					"type":        "string",
					"description": "Action to perform: 'claim' (default) or 'release'.",
				},
			},
			"required": []string{"mission", "event_key", "agent"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			missionName, _ := args["mission"].(string)
			eventKey, _ := args["event_key"].(string)
			agentName, _ := args["agent"].(string)
			action, _ := args["action"].(string)
			if action == "" {
				action = "claim"
			}

			if missionName == "" {
				return "Error: mission is required", true
			}
			if eventKey == "" {
				return "Error: event_key is required", true
			}
			if agentName == "" {
				return "Error: agent is required", true
			}

			m, err := d.missions.Get(missionName)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			switch action {
			case "release":
				d.claims.Release(m.ID, eventKey)
				return fmt.Sprintf("Claim released for event %q on mission %q.", eventKey, missionName), false
			case "claim":
				ok, holder := d.claims.Claim(m.ID, eventKey, agentName)
				if ok {
					return fmt.Sprintf("Claimed event %q on mission %q by %q.", eventKey, missionName, agentName), false
				}
				return fmt.Sprintf("Event %q on mission %q already claimed by %q.", eventKey, missionName, holder), false
			default:
				return fmt.Sprintf("Error: unknown action %q (must be 'claim' or 'release')", action), true
			}
		},
	)

	// 12. agency_mission_health — show health status for a mission
	reg.Register(
		"agency_mission_health",
		"Show the health status of a mission: whether the assigned agent is running and all required capabilities are still granted.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"mission": map[string]interface{}{
					"type":        "string",
					"description": "Mission name.",
				},
			},
			"required": []string{"mission"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			missionName, _ := args["mission"].(string)
			if missionName == "" {
				return "Error: mission is required", true
			}

			if d.healthMonitor == nil {
				return "Error: health monitor is not available", true
			}

			status, err := d.healthMonitor.CheckHealth(context.Background(), missionName)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			healthy := "healthy"
			if !status.Healthy {
				healthy = "UNHEALTHY"
			}

			lines := []string{
				fmt.Sprintf("Mission health: %s [%s]", status.MissionName, healthy),
				fmt.Sprintf("  Agent:         %s", status.AgentName),
				fmt.Sprintf("  Agent running: %v", status.AgentRunning),
			}
			if len(status.MissingCaps) > 0 {
				lines = append(lines, fmt.Sprintf("  Missing caps:  %s", strings.Join(status.MissingCaps, ", ")))
			} else {
				lines = append(lines, "  Missing caps:  none")
			}
			return strings.Join(lines, "\n"), false
		},
	)
}
