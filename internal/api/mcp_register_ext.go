package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	"gopkg.in/yaml.v3"
)

// mcpValidateResourceName rejects names containing path separators or ".."
// components to prevent path traversal when names are used in file paths.
func mcpValidateResourceName(name, kind string) error {
	if name == "" {
		return fmt.Errorf("%s name is required", kind)
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("invalid %s name '%s': must not contain path separators", kind, name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid %s name '%s': must not be a relative path component", kind, name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("invalid %s name '%s': must not contain '..'", kind, name)
	}
	return nil
}

// ── Team (4 tools) ──────────────────────────────────────────────────────────

func registerTeamTools(reg *MCPToolRegistry) {
	// agency_team_create
	reg.Register(
		"agency_team_create",
		"Create a new team with optional coordinator and members.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":        map[string]interface{}{"type": "string", "description": "Team name"},
				"coordinator": map[string]interface{}{"type": "string", "description": "Coordinator agent name"},
				"members":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Member agent names"},
			},
			"required": []string{"name"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "name")
			if err := mcpValidateResourceName(name, "team"); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}

			teamDir := filepath.Join(h.cfg.Home, "teams", name)
			if err := os.MkdirAll(teamDir, 0755); err != nil {
				return "Error: " + err.Error(), true
			}

			team := map[string]interface{}{"name": name}
			if coord := mapStr(args, "coordinator"); coord != "" {
				team["coordinator"] = coord
			}
			members := mapSlice(args, "members")
			if len(members) > 0 {
				team["members"] = members
			}

			data, _ := yaml.Marshal(team)
			if err := os.WriteFile(filepath.Join(teamDir, "team.yaml"), data, 0644); err != nil {
				return "Error: " + err.Error(), true
			}

			h.log.Info("team created", "name", name)
			h.audit.WriteSystem("team_created", map[string]interface{}{"team": name})
			return fmt.Sprintf("Team '%s' created.", name), false
		},
	)

	// agency_team_list
	reg.Register(
		"agency_team_list",
		"List all configured teams with member counts.",
		nil,
		func(h *handler, args map[string]interface{}) (string, bool) {
			teamsDir := filepath.Join(h.cfg.Home, "teams")
			entries, err := os.ReadDir(teamsDir)
			if err != nil {
				return "No teams configured.", false
			}

			var teams []string
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				teamName := e.Name()
				memberCount := 0
				teamPath := filepath.Join(teamsDir, teamName, "team.yaml")
				if data, err := os.ReadFile(teamPath); err == nil {
					var t map[string]interface{}
					if yaml.Unmarshal(data, &t) == nil {
						if members, ok := t["members"].([]interface{}); ok {
							memberCount = len(members)
						}
					}
				}
				teams = append(teams, fmt.Sprintf("  %s: %d members", teamName, memberCount))
			}

			if len(teams) == 0 {
				return "No teams configured.", false
			}
			return "Teams:\n" + strings.Join(teams, "\n"), false
		},
	)

	// agency_team_show
	reg.Register(
		"agency_team_show",
		"Show team details: members, roles, coordinator, and configuration.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Team name"},
			},
			"required": []string{"name"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "name")
			if err := mcpValidateResourceName(name, "team"); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			teamPath := filepath.Join(h.cfg.Home, "teams", name, "team.yaml")
			data, err := os.ReadFile(teamPath)
			if err != nil {
				return fmt.Sprintf("Error: team not found: %s", name), true
			}
			var team map[string]interface{}
			if err := yaml.Unmarshal(data, &team); err != nil {
				return "Error: invalid team config", true
			}

			lines := []string{fmt.Sprintf("Team: %s", mapStr(team, "name"))}
			if coord := mapStr(team, "coordinator"); coord != "" {
				lines = append(lines, fmt.Sprintf("  Coordinator: %s", coord))
			}
			membersRaw := mapSlice(team, "members")
			if len(membersRaw) > 0 {
				lines = append(lines, fmt.Sprintf("  Members (%d):", len(membersRaw)))
				for _, m := range membersRaw {
					switch v := m.(type) {
					case string:
						lines = append(lines, fmt.Sprintf("    %s", v))
					case map[string]interface{}:
						mName := mapStr(v, "name")
						role := mapStr(v, "role")
						if role != "" {
							lines = append(lines, fmt.Sprintf("    %s (%s)", mName, role))
						} else {
							lines = append(lines, fmt.Sprintf("    %s", mName))
						}
					}
				}
			} else {
				lines = append(lines, "  No members.")
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// agency_team_activity
	reg.Register(
		"agency_team_activity",
		"Show the workspace activity register for a team. Tracks what agents are currently doing.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Team name"},
			},
			"required": []string{"name"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "name")
			if err := mcpValidateResourceName(name, "team"); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			teamPath := filepath.Join(h.cfg.Home, "teams", name, "team.yaml")
			data, err := os.ReadFile(teamPath)
			if err != nil {
				return fmt.Sprintf("Error: team not found: %s", name), true
			}
			var team map[string]interface{}
			yaml.Unmarshal(data, &team)

			members, _ := team["members"].([]interface{})
			reader := logs.NewReader(h.cfg.Home)
			var activity []string
			for _, m := range members {
				memberName, ok := m.(string)
				if !ok {
					continue
				}
				events, err := reader.ReadAgentLog(memberName, "", "")
				if err != nil || len(events) == 0 {
					activity = append(activity, fmt.Sprintf("  %s: no activity", memberName))
					continue
				}
				last := events[len(events)-1]
				eventType := mapStr(map[string]interface{}(last), "type")
				if eventType == "" {
					eventType = "unknown"
				}
				activity = append(activity, fmt.Sprintf("  %s: %s", memberName, eventType))
			}

			if len(activity) == 0 {
				return fmt.Sprintf("No activity registered for team '%s'.", name), false
			}
			return fmt.Sprintf("Activity for team '%s':\n%s", name, strings.Join(activity, "\n")), false
		},
	)
}

// ── Deploy (2 tools) ────────────────────────────────────────────────────────

func registerDeployTools(reg *MCPToolRegistry) {
	// agency_deploy
	reg.Register(
		"agency_deploy",
		"Deploy a pack file — creates team, agents, channels, and starts all agents. Use --dry-run to validate first. Packs are declarative YAML files that compose presets, skills, and teams into a deployable unit.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pack_file":   map[string]interface{}{"type": "string", "description": "Path to the pack YAML file"},
				"dry_run":     map[string]interface{}{"type": "boolean", "description": "Validate only, do not deploy", "default": false},
				"credentials": map[string]interface{}{"type": "object", "description": "Credential key-value pairs required by the pack", "additionalProperties": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"pack_file"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			packFile := mapStr(args, "pack_file")
			if packFile == "" {
				return "Error: pack_file is required", true
			}

			pack, err := orchestrate.LoadPack(packFile)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			// Extract credentials from args.
			creds := map[string]string{}
			if raw, ok := args["credentials"]; ok {
				if credMap, ok := raw.(map[string]interface{}); ok {
					for k, v := range credMap {
						if s, ok := v.(string); ok {
							creds[k] = s
						}
					}
				}
			}

			// Validate required credentials.
			if len(pack.Credentials) > 0 {
				var missing []string
				for _, cred := range pack.Credentials {
					if cred.Required {
						if _, ok := creds[cred.Name]; !ok {
							missing = append(missing, cred.Name)
						}
					}
				}
				if len(missing) > 0 {
					data, _ := json.Marshal(map[string]interface{}{
						"status":  "credentials_required",
						"missing": missing,
					})
					return string(data), true
				}
			}

			deployer := orchestrate.NewDeployer(h.cfg.Home, h.cfg.Version, h.dc, h.log)
			deployer.Credentials = creds

			if mapBool(args, "dry_run") {
				result, err := deployer.DryRunDeploy(context.Background(), pack, func(s string) {
					h.log.Info("deploy dry-run", "status", s)
				})
				if err != nil {
					return "Error: " + err.Error(), true
				}
				data, _ := json.Marshal(result)
				return string(data), false
			}

			result, err := deployer.Deploy(context.Background(), pack, func(s string) {
				h.log.Info("deploy", "status", s)
			})
			if err != nil {
				h.audit.WriteSystem("deploy_failed", map[string]interface{}{"pack": packFile, "error": err.Error()})
				return "Error: " + err.Error(), true
			}
			h.audit.WriteSystem("pack_deployed", map[string]interface{}{"pack": packFile})
			data, _ := json.Marshal(result)
			return string(data), false
		},
	)

	// agency_teardown
	reg.Register(
		"agency_teardown",
		"Teardown a deployed pack — stops all agents. Use --delete to also remove agents, team, and channels.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pack_name": map[string]interface{}{"type": "string", "description": "Name of the deployed pack"},
				"delete":    map[string]interface{}{"type": "boolean", "description": "Remove agents, team, and channels", "default": false},
			},
			"required": []string{"pack_name"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			packName := mapStr(args, "pack_name")
			if packName == "" {
				return "Error: pack_name is required", true
			}
			del := mapBool(args, "delete")

			deployer := orchestrate.NewDeployer(h.cfg.Home, h.cfg.Version, h.dc, h.log)
			if err := deployer.Teardown(context.Background(), packName, del); err != nil {
				return "Error: " + err.Error(), true
			}
			h.audit.WriteSystem("pack_teardown", map[string]interface{}{"pack": packName, "delete": del})
			action := "stopped"
			if del {
				action = "torn down and deleted"
			}
			return fmt.Sprintf("Pack '%s' %s.", packName, action), false
		},
	)
}

// ── Hub instance management (4 tools) ───────────────────────────────────────

func registerConnectorTools(reg *MCPToolRegistry) {
	// agency_hub_instances
	reg.Register(
		"agency_hub_instances",
		"List hub component instances and their activation status. Optionally filter by kind.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"kind": map[string]interface{}{"type": "string", "description": "Component kind filter (optional, e.g. connector, preset, pack)"},
			},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			kind := mapStr(args, "kind")
			mgr := hub.NewManager(h.cfg.Home)
			instances := mgr.Registry.List(kind)

			if len(instances) == 0 {
				return "No hub component instances installed.", false
			}
			var lines []string
			for _, inst := range instances {
				lines = append(lines, fmt.Sprintf("  %s (id=%s, kind=%s): %s", inst.Name, inst.ID, inst.Kind, inst.State))
			}
			return "Hub component instances:\n" + strings.Join(lines, "\n"), false
		},
	)

	// agency_hub_activate
	reg.Register(
		"agency_hub_activate",
		"Activate a hub component instance so the intake service starts receiving events from it.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name_or_id": map[string]interface{}{"type": "string", "description": "Component instance name or ID"},
			},
			"required": []string{"name_or_id"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "name_or_id")
			if name == "" {
				return "Error: name_or_id is required", true
			}
			mgr := hub.NewManager(h.cfg.Home)
			inst := mgr.Registry.Resolve(name)
			if inst == nil {
				return fmt.Sprintf("Error: component instance not found: %s", name), true
			}
			if err := mgr.Registry.SetState(name, "active"); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			h.dc.CommsRequest(context.Background(), "POST", "/hub/"+inst.Name+"/activate", nil)
			h.log.Info("hub component activated", "name", inst.Name, "id", inst.ID)
			return fmt.Sprintf("Component instance '%s' activated.", inst.Name), false
		},
	)

	// agency_hub_deactivate
	reg.Register(
		"agency_hub_deactivate",
		"Deactivate a hub component instance — stop receiving events.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name_or_id": map[string]interface{}{"type": "string", "description": "Component instance name or ID"},
			},
			"required": []string{"name_or_id"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "name_or_id")
			if name == "" {
				return "Error: name_or_id is required", true
			}
			mgr := hub.NewManager(h.cfg.Home)
			inst := mgr.Registry.Resolve(name)
			if inst == nil {
				return fmt.Sprintf("Error: component instance not found: %s", name), true
			}
			if err := mgr.Registry.SetState(name, "inactive"); err != nil {
				return fmt.Sprintf("Error: %s", err), true
			}
			h.dc.CommsRequest(context.Background(), "POST", "/hub/"+inst.Name+"/deactivate", nil)
			h.log.Info("hub component deactivated", "name", inst.Name, "id", inst.ID)
			return fmt.Sprintf("Component instance '%s' deactivated.", inst.Name), false
		},
	)

	// agency_hub_show
	reg.Register(
		"agency_hub_show",
		"Show hub component instance detail: config, state, event counts, and rate limit state.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name_or_id": map[string]interface{}{"type": "string", "description": "Component instance name or ID"},
			},
			"required": []string{"name_or_id"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "name_or_id")
			if name == "" {
				return "Error: name_or_id is required", true
			}
			mgr := hub.NewManager(h.cfg.Home)
			inst := mgr.Registry.Resolve(name)
			if inst == nil {
				return fmt.Sprintf("Error: component instance not found: %s", name), true
			}

			status := map[string]interface{}{
				"name":  inst.Name,
				"id":    inst.ID,
				"state": inst.State,
			}

			// Try to get live status from intake
			liveData, err := h.dc.CommsRequest(context.Background(), "GET", "/hub/"+inst.Name+"/status", nil)
			if err == nil {
				var liveStatus map[string]interface{}
				if json.Unmarshal(liveData, &liveStatus) == nil {
					for k, v := range liveStatus {
						status[k] = v
					}
				}
			}

			lines := []string{fmt.Sprintf("Component: %s (id=%s)", inst.Name, inst.ID)}
			lines = append(lines, fmt.Sprintf("State: %s", inst.State))
			if te := mapStr(status, "total_events"); te != "" {
				lines = append(lines, fmt.Sprintf("Total events: %s", te))
			}
			if byStatus, ok := status["by_status"].(map[string]interface{}); ok {
				for s, count := range byStatus {
					lines = append(lines, fmt.Sprintf("  %s: %v", s, count))
				}
			}
			return strings.Join(lines, "\n"), false
		},
	)
}

// ── Intake (2 tools) ────────────────────────────────────────────────────────

func registerIntakeTools(reg *MCPToolRegistry) {
	// agency_intake_items
	reg.Register(
		"agency_intake_items",
		"List work items from the intake service.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"connector":    map[string]interface{}{"type": "string", "description": "Filter by connector name"},
				"status":       map[string]interface{}{"type": "string", "description": "Filter by status"},
				"sla_breached": map[string]interface{}{"type": "boolean", "description": "Show only SLA-breached items", "default": false},
				"limit":        map[string]interface{}{"type": "integer", "description": "Max items", "default": 50},
			},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			path := "/items"
			connector := mapStr(args, "connector")
			if connector != "" {
				path += "?connector=" + connector
			}

			out, err := serviceGet(context.Background(), "8205", path)
			if err != nil {
				return "No work items found.", false
			}

			var items []map[string]interface{}
			if json.Unmarshal(out, &items) != nil {
				return "No work items found.", false
			}

			if len(items) == 0 {
				return "No work items found.", false
			}

			var lines []string
			for _, item := range items {
				target := mapStr(item, "target_name")
				if target == "" {
					target = mapStr(item, "target")
				}
				if target == "" {
					target = "-"
				}
				lines = append(lines, fmt.Sprintf("  %s | %s | %s | %s | %s",
					mapStr(item, "id"),
					mapStr(item, "connector"),
					mapStr(item, "status"),
					target,
					mapStr(item, "priority"),
				))
			}
			return fmt.Sprintf("Work items (%d):\n%s", len(items), strings.Join(lines, "\n")), false
		},
	)

	// agency_intake_stats
	reg.Register(
		"agency_intake_stats",
		"Show intake service statistics — counts by status and connector.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"connector": map[string]interface{}{"type": "string", "description": "Filter by connector name"},
			},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			out, err := serviceGet(context.Background(), "8205", "/stats")
			if err != nil {
				return "Intake service unavailable.", true
			}

			var result map[string]interface{}
			if json.Unmarshal(out, &result) != nil {
				return "Intake service unavailable.", true
			}

			lines := []string{fmt.Sprintf("Total: %v", result["total"])}
			if byStatus, ok := result["by_status"].(map[string]interface{}); ok {
				lines = append(lines, "By status:")
				for status, count := range byStatus {
					lines = append(lines, fmt.Sprintf("  %s: %v", status, count))
				}
			}
			if byConnector, ok := result["by_connector"].(map[string]interface{}); ok {
				lines = append(lines, "By connector:")
				for name, count := range byConnector {
					lines = append(lines, fmt.Sprintf("  %s: %v", name, count))
				}
			}
			return strings.Join(lines, "\n"), false
		},
	)
}

// ── Hub (6 tools) ───────────────────────────────────────────────────────────

func registerHubTools(reg *MCPToolRegistry) {
	// agency_hub_search
	reg.Register(
		"agency_hub_search",
		"Search for components across hub sources. Returns matching components with name, kind, source, and description.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string", "description": "Search query"},
				"kind":  map[string]interface{}{"type": "string", "description": "Component kind filter (optional)"},
			},
			"required": []string{"query"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			query := mapStr(args, "query")
			kind := mapStr(args, "kind")
			mgr := hub.NewManager(h.cfg.Home)
			results := mgr.Search(query, kind)

			if len(results) == 0 {
				return fmt.Sprintf("No components found matching '%s'.", query), false
			}

			lines := []string{fmt.Sprintf("Found %d component(s):", len(results))}
			for _, r := range results {
				lines = append(lines, fmt.Sprintf("  %-12s %-24s (%s) -- %s", r.Kind, r.Name, r.Source, r.Description))
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// agency_hub_install
	reg.Register(
		"agency_hub_install",
		"Install a component from a hub source. Resolves transitive dependencies for packs.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"component": map[string]interface{}{"type": "string", "description": "Component name"},
				"kind":      map[string]interface{}{"type": "string", "description": "Component kind"},
				"source":    map[string]interface{}{"type": "string", "description": "Hub source name (optional)"},
			},
			"required": []string{"component", "kind"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "component")
			kind := mapStr(args, "kind")
			source := mapStr(args, "source")

			mgr := hub.NewManager(h.cfg.Home)
			inst, err := mgr.Install(name, kind, source, "")
			if err != nil {
				return "Error: " + err.Error(), true
			}
			// Show license if present on the installed component
			comp := mgr.FindInCache(name, kind, source)
			if comp != nil && comp.License != "" {
				return fmt.Sprintf("Installed %s (%s) as %s. License: %s", name, kind, inst.ID, comp.License), false
			}
			return fmt.Sprintf("Installed %s (%s) as %s.", name, kind, inst.ID), false
		},
	)

	// agency_hub_remove
	reg.Register(
		"agency_hub_remove",
		"Remove an installed hub component and its provenance entry.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"component": map[string]interface{}{"type": "string", "description": "Component name"},
				"kind":      map[string]interface{}{"type": "string", "description": "Component kind"},
			},
			"required": []string{"component", "kind"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "component")
			kind := mapStr(args, "kind")

			mgr := hub.NewManager(h.cfg.Home)
			if err := mgr.Remove(name, kind); err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Removed %s (%s).", name, kind), false
		},
	)

	// agency_hub_list
	reg.Register(
		"agency_hub_list",
		"List all hub-installed components with provenance details.",
		nil,
		func(h *handler, args map[string]interface{}) (string, bool) {
			mgr := hub.NewManager(h.cfg.Home)
			installed := mgr.List()

			if len(installed) == 0 {
				return "No hub-installed components.", false
			}

			lines := []string{"Hub-installed components:"}
			for _, p := range installed {
				lines = append(lines, fmt.Sprintf("  %-12s %-24s (source: %s)", p.Kind, p.DisplayName(), p.Source))
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// agency_hub_update
	reg.Register(
		"agency_hub_update",
		"Sync all hub source caches (git pull). Does not upgrade installed components.",
		nil,
		func(h *handler, args map[string]interface{}) (string, bool) {
			mgr := hub.NewManager(h.cfg.Home)
			report, err := mgr.Update()
			if err != nil {
				return "Error: " + err.Error(), true
			}

			lines := []string{}
			for _, su := range report.Sources {
				if su.OldCommit == su.NewCommit || su.NewCommit == "" {
					lines = append(lines, fmt.Sprintf("  %s  up to date", su.Name))
				} else {
					lines = append(lines, fmt.Sprintf("  %s  %d new commits (%s → %s)", su.Name, su.CommitCount, su.OldCommit, su.NewCommit))
				}
			}

			if len(report.Available) > 0 {
				lines = append(lines, "\nUpgrades available:")
				for _, u := range report.Available {
					if u.Kind == "managed" {
						lines = append(lines, fmt.Sprintf("  %-20s managed   %s", u.Name, u.Summary))
					} else {
						lines = append(lines, fmt.Sprintf("  %-20s %-10s %s → %s", u.Name, u.Kind, u.InstalledVersion, u.AvailableVersion))
					}
				}
				lines = append(lines, "\nRun 'agency hub upgrade' to apply.")
			}

			if len(lines) == 0 {
				return "Hub sources up to date.", false
			}
			return "Hub sources updated.\n" + strings.Join(lines, "\n"), false
		},
	)

	// agency_hub_outdated
	reg.Register(
		"agency_hub_outdated",
		"Show what would be upgraded from current hub cache. No network access.",
		nil,
		func(h *handler, args map[string]interface{}) (string, bool) {
			mgr := hub.NewManager(h.cfg.Home)
			upgrades := mgr.Outdated()

			if len(upgrades) == 0 {
				return "All components up to date.", false
			}

			lines := []string{"Upgrades available:"}
			for _, u := range upgrades {
				if u.Kind == "managed" {
					lines = append(lines, fmt.Sprintf("  %-20s managed   %s", u.Name, u.Summary))
				} else {
					lines = append(lines, fmt.Sprintf("  %-20s %-10s %s → %s", u.Name, u.Kind, u.InstalledVersion, u.AvailableVersion))
				}
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// agency_hub_upgrade
	reg.Register(
		"agency_hub_upgrade",
		"Apply available upgrades: sync managed files and upgrade installed components. Optionally specify component names.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"components": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Specific components to upgrade (omit to upgrade all)",
				},
			},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			var components []string
			if raw, ok := args["components"]; ok {
				if arr, ok := raw.([]interface{}); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							components = append(components, s)
						}
					}
				}
			}

			mgr := hub.NewManager(h.cfg.Home)
			report, err := mgr.Upgrade(components)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			lines := []string{}
			for _, f := range report.Files {
				switch f.Status {
				case "upgraded":
					detail := f.Summary
					if detail == "" {
						detail = "updated"
					}
					lines = append(lines, fmt.Sprintf("  %-12s %s", f.Category, detail))
				case "unchanged":
					lines = append(lines, fmt.Sprintf("  %-12s unchanged", f.Category))
				case "error":
					lines = append(lines, fmt.Sprintf("  %-12s ERROR: %s", f.Category, f.Summary))
				}
			}

			for _, cu := range report.Components {
				switch cu.Status {
				case "upgraded":
					lines = append(lines, fmt.Sprintf("  %-20s %-10s %s → %s", cu.Name, cu.Kind, cu.OldVersion, cu.NewVersion))
				case "error":
					lines = append(lines, fmt.Sprintf("  %-20s %-10s ERROR: %s", cu.Name, cu.Kind, cu.Error))
				}
			}

			if len(lines) == 0 {
				return "Nothing to upgrade.", false
			}
			return "Hub upgraded:\n" + strings.Join(lines, "\n"), false
		},
	)

	// agency_hub_info
	reg.Register(
		"agency_hub_info",
		"Show detailed info about a component: metadata, which sources have it, and install status.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"component": map[string]interface{}{"type": "string", "description": "Component name"},
				"kind":      map[string]interface{}{"type": "string", "description": "Component kind (optional)"},
			},
			"required": []string{"component"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			name := mapStr(args, "component")
			kind := mapStr(args, "kind")

			mgr := hub.NewManager(h.cfg.Home)
			info, err := mgr.Info(name, kind)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			lines := []string{
				fmt.Sprintf("Name: %s", mapStr(info, "name")),
				fmt.Sprintf("Kind: %s", mapStr(info, "kind")),
				fmt.Sprintf("Description: %s", mapStr(info, "description")),
				fmt.Sprintf("Source: %s", mapStr(info, "source")),
			}
			if author := mapStr(info, "author"); author != "" {
				lines = append(lines, fmt.Sprintf("Author: %s", author))
			}
			if license := mapStr(info, "license"); license != "" {
				lines = append(lines, fmt.Sprintf("License: %s", license))
			}
			if installed, ok := info["installed"].(bool); ok && installed {
				lines = append(lines, "Installed: yes")
			} else {
				lines = append(lines, "Installed: no")
			}
			return strings.Join(lines, "\n"), false
		},
	)
}
