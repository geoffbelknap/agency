package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/geoffbelknap/agency/internal/models"
)

// ── Meeseeks (3 tools) ───────────────────────────────────────────────────────

func registerMeeseeksTools(reg *MCPToolRegistry) {

	// 1. agency_meeseeks_list
	reg.Register(
		"agency_meeseeks_list",
		"List active Meeseeks ephemeral agents. Optionally filter by parent agent.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"parent": map[string]interface{}{
					"type":        "string",
					"description": "Filter by parent agent name (optional).",
				},
			},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			parent, _ := args["parent"].(string)
			items := d.meeseeks.List(parent)
			if len(items) == 0 {
				if parent != "" {
					return fmt.Sprintf("Meeseeks for %q: none active.", parent), false
				}
				return "Meeseeks (0): none active.", false
			}

			lines := []string{fmt.Sprintf("Meeseeks (%d):", len(items))}
			for _, m := range items {
				age := ""
				if !m.SpawnedAt.IsZero() {
					age = time.Since(m.SpawnedAt).Round(time.Second).String()
				}
				task := m.Task
				if len(task) > 40 {
					task = task[:37] + "..."
				}
				orphanedFlag := ""
				if m.Orphaned {
					orphanedFlag = " [ORPHANED]"
				}
				lines = append(lines, fmt.Sprintf("  %-14s  %-20s  %-12s  $%.3f/$%.3f  %s  %q%s",
					m.ID, m.ParentAgent, string(m.Status),
					m.BudgetUsed, m.Budget, age, task, orphanedFlag))
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// 2. agency_meeseeks_show
	reg.Register(
		"agency_meeseeks_show",
		"Show detailed information about a specific Meeseeks ephemeral agent.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "Meeseeks ID (e.g. mks-a1b2c3d4).",
				},
			},
			"required": []string{"id"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			id, _ := args["id"].(string)
			if id == "" {
				return "Error: id is required", true
			}
			m, err := d.meeseeks.Get(id)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			completedAt := "—"
			if m.CompletedAt != nil {
				completedAt = m.CompletedAt.Format(time.RFC3339)
			}
			age := time.Since(m.SpawnedAt).Round(time.Second).String()

			lines := []string{
				fmt.Sprintf("Meeseeks: %s", m.ID),
				fmt.Sprintf("  Status:        %s", string(m.Status)),
				fmt.Sprintf("  Orphaned:      %v", m.Orphaned),
				fmt.Sprintf("  Parent:        %s", m.ParentAgent),
				fmt.Sprintf("  Mission:       %s", m.ParentMissionID),
				fmt.Sprintf("  Model:         %s", m.Model),
				fmt.Sprintf("  Budget:        $%.4f used / $%.4f total", m.BudgetUsed, m.Budget),
				fmt.Sprintf("  Age:           %s", age),
				fmt.Sprintf("  Spawned:       %s", m.SpawnedAt.Format(time.RFC3339)),
				fmt.Sprintf("  Completed:     %s", completedAt),
				fmt.Sprintf("  Container:     %s", m.ContainerName),
				fmt.Sprintf("  Enforcer:      %s", m.EnforcerName),
				fmt.Sprintf("  Network:       %s", m.NetworkName),
				fmt.Sprintf("  Task:          %s", m.Task),
			}
			if len(m.Tools) > 0 {
				lines = append(lines, fmt.Sprintf("  Tools:         %s", strings.Join(m.Tools, ", ")))
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// 3. agency_meeseeks_kill
	reg.Register(
		"agency_meeseeks_kill",
		"Terminate a Meeseeks ephemeral agent by ID.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "Meeseeks ID to terminate.",
				},
			},
			"required": []string{"id"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			id, _ := args["id"].(string)
			if id == "" {
				return "Error: id is required", true
			}
			mks, err := d.meeseeks.Get(id)
			if err != nil {
				return "Error: " + err.Error(), true
			}
			if err := d.meeseeks.UpdateStatus(id, models.MeeseeksStatusTerminated); err != nil {
				return "Error: " + err.Error(), true
			}
			d.meeseeks.Remove(id)
			d.audit.Write(mks.ParentAgent, "meeseeks_terminated", map[string]interface{}{
				"meeseeks_id": id,
				"task":        mks.Task,
				"budget_used": mks.BudgetUsed,
				"source":      "mcp_tool",
				"build_id":    d.cfg.BuildID,
			})
			return fmt.Sprintf("Meeseeks %q terminated (task: %q, budget used: $%.4f).", id, mks.Task, mks.BudgetUsed), false
		},
	)
}
