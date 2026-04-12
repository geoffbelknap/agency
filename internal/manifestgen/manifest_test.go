package manifestgen

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	agencyconsent "github.com/geoffbelknap/agency/internal/consent"
	runpkg "github.com/geoffbelknap/agency/internal/runtime"
)

func TestGenerateAgentManifest_ProjectsRuntimeToolsWithoutConsentActions(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "coordinator")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "constraints.yaml"), []byte("agent: coordinator\ngranted_capabilities: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentYAML := `
version: "0.1"
name: coordinator
role: test
body:
  runtime: body
  version: "1.0"
workspace:
  ref: default
instances:
  attach:
    - instance_id: inst_drive
      node_id: drive_admin
`
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	instanceDir := filepath.Join(home, "instances", "inst_drive")
	rtStore := runpkg.NewStore(instanceDir)
	manifest := &runpkg.Manifest{
		APIVersion: runpkg.ManifestAPIVersion,
		Kind:       runpkg.ManifestKind,
		Metadata: runpkg.ManifestMeta{
			ManifestID:   "mf_test",
			InstanceID:   "inst_drive",
			InstanceName: "community-admin",
			CompiledAt:   time.Now().UTC(),
			Planner:      runpkg.PlannerVersion,
		},
		Runtime: runpkg.RuntimeSpec{
			Nodes: []runpkg.RuntimeNode{{
				NodeID:         "drive_admin",
				Kind:           "connector.authority",
				Tools:          []string{"add_viewer", "list_permissions"},
				ConsentActions: []string{"add_viewer"},
				Executor: &runpkg.RuntimeExecutor{
					Kind:    "http_json",
					BaseURL: "https://example.test",
					Actions: map[string]runpkg.RuntimeHTTPAction{
						"add_viewer":       {Path: "/permissions/add", Method: "POST"},
						"list_permissions": {Path: "/permissions/list", Method: "POST"},
					},
				},
			}},
		},
	}
	if err := rtStore.SaveManifest(manifest); err != nil {
		t.Fatalf("SaveManifest(): %v", err)
	}

	gen := Generator{
		Home:   home,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if err := gen.GenerateAgentManifest("coordinator"); err != nil {
		t.Fatalf("GenerateAgentManifest(): %v", err)
	}

	data, err := os.ReadFile(filepath.Join(agentDir, "services-manifest.json"))
	if err != nil {
		t.Fatalf("read services-manifest.json: %v", err)
	}
	var got struct {
		Services []map[string]any `json:"services"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if len(got.Services) != 1 {
		t.Fatalf("len(services) = %d, want 1", len(got.Services))
	}
	tools, ok := got.Services[0]["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("projected tools = %#v", got.Services[0]["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tool = %#v", tools[0])
	}
	if tool["name"] != "instance_community_admin_drive_admin_list_permissions" {
		t.Fatalf("tool name = %#v", tool["name"])
	}
	if tool["path"] != "/api/v1/instances/inst_drive/runtime/nodes/drive_admin/actions/list_permissions" {
		t.Fatalf("tool path = %#v", tool["path"])
	}
	if tool["passthrough"] != true {
		t.Fatalf("passthrough = %#v, want true", tool["passthrough"])
	}
}

func TestGenerateAgentManifest_ProjectsConsentRuntimeToolsWhenConfigured(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "coordinator")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "constraints.yaml"), []byte("agent: coordinator\ngranted_capabilities: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentYAML := `
version: "0.1"
name: coordinator
role: test
body:
  runtime: body
  version: "1.0"
workspace:
  ref: default
instances:
  attach:
    - instance_id: inst_drive
      node_id: drive_admin
`
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	instanceDir := filepath.Join(home, "instances", "inst_drive")
	rtStore := runpkg.NewStore(instanceDir)
	if err := rtStore.SaveManifest(&runpkg.Manifest{
		APIVersion: runpkg.ManifestAPIVersion,
		Kind:       runpkg.ManifestKind,
		Metadata: runpkg.ManifestMeta{
			ManifestID:   "mf_test",
			InstanceID:   "inst_drive",
			InstanceName: "community-admin",
			CompiledAt:   time.Now().UTC(),
			Planner:      runpkg.PlannerVersion,
		},
		Source: runpkg.ManifestSource{
			ConsentDeploymentID: "dep-123",
		},
		Runtime: runpkg.RuntimeSpec{
			Nodes: []runpkg.RuntimeNode{{
				NodeID:         "drive_admin",
				Kind:           "connector.authority",
				Tools:          []string{"add_viewer"},
				ConsentActions: []string{"add_viewer"},
				ConsentRequirements: map[string]agencyconsent.Requirement{
					"add_viewer": {
						OperationKind:    "grant_drive_viewer",
						TokenInputField:  "consent_token",
						TargetInputField: "drive_id",
					},
				},
				Executor: &runpkg.RuntimeExecutor{
					Kind:    "http_json",
					BaseURL: "https://example.test",
					Actions: map[string]runpkg.RuntimeHTTPAction{
						"add_viewer": {Path: "/permissions/add", Method: "POST"},
					},
				},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	gen := Generator{Home: home, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := gen.GenerateAgentManifest("coordinator"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(agentDir, "services-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Services []struct {
			Tools []struct {
				Name       string `json:"name"`
				Parameters []struct {
					Name string `json:"name"`
				} `json:"parameters"`
			} `json:"tools"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Services) != 1 || len(got.Services[0].Tools) != 1 {
		t.Fatalf("unexpected projected tools: %#v", got.Services)
	}
	if got.Services[0].Tools[0].Name != "instance_community_admin_drive_admin_add_viewer" {
		t.Fatalf("tool name = %q", got.Services[0].Tools[0].Name)
	}
	if len(got.Services[0].Tools[0].Parameters) != 2 {
		t.Fatalf("parameters = %#v", got.Services[0].Tools[0].Parameters)
	}
}

func TestGenerateAgentManifest_RespectsAttachmentActionFilter(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "coordinator")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "constraints.yaml"), []byte("agent: coordinator\ngranted_capabilities: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentYAML := `
version: "0.1"
name: coordinator
role: test
body:
  runtime: body
  version: "1.0"
workspace:
  ref: default
instances:
  attach:
    - instance_id: inst_drive
      node_id: drive_admin
      actions: ["list_permissions"]
`
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	instanceDir := filepath.Join(home, "instances", "inst_drive")
	rtStore := runpkg.NewStore(instanceDir)
	if err := rtStore.SaveManifest(&runpkg.Manifest{
		APIVersion: runpkg.ManifestAPIVersion,
		Kind:       runpkg.ManifestKind,
		Metadata: runpkg.ManifestMeta{
			ManifestID:   "mf_test",
			InstanceID:   "inst_drive",
			InstanceName: "community-admin",
			CompiledAt:   time.Now().UTC(),
			Planner:      runpkg.PlannerVersion,
		},
		Runtime: runpkg.RuntimeSpec{
			Nodes: []runpkg.RuntimeNode{{
				NodeID: "drive_admin",
				Kind:   "connector.authority",
				Tools:  []string{"list_permissions", "remove_viewer"},
				Executor: &runpkg.RuntimeExecutor{
					Kind:    "http_json",
					BaseURL: "https://example.test",
					Actions: map[string]runpkg.RuntimeHTTPAction{
						"list_permissions": {Path: "/permissions/list", Method: "POST"},
						"remove_viewer":    {Path: "/permissions/remove", Method: "POST"},
					},
				},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	gen := Generator{Home: home, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := gen.GenerateAgentManifest("coordinator"); err != nil {
		t.Fatalf("GenerateAgentManifest(): %v", err)
	}
	data, err := os.ReadFile(filepath.Join(agentDir, "services-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Services []struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Services) != 1 || len(got.Services[0].Tools) != 1 {
		t.Fatalf("unexpected projected tools: %#v", got.Services)
	}
	if got.Services[0].Tools[0].Name != "instance_community_admin_drive_admin_list_permissions" {
		t.Fatalf("tool name = %q", got.Services[0].Tools[0].Name)
	}
}

func TestGenerateAgentManifest_SkipsMissingRuntimeManifest(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "coordinator")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "constraints.yaml"), []byte("agent: coordinator\ngranted_capabilities: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentYAML := `
version: "0.1"
name: coordinator
role: test
body:
  runtime: body
  version: "1.0"
workspace:
  ref: default
instances:
  attach:
    - instance_id: inst_missing
      node_id: drive_admin
`
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	gen := Generator{Home: home, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := gen.GenerateAgentManifest("coordinator"); err != nil {
		t.Fatalf("GenerateAgentManifest(): %v", err)
	}
	data, err := os.ReadFile(filepath.Join(agentDir, "services-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Services []map[string]any `json:"services"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Services) != 0 {
		t.Fatalf("len(services) = %d, want 0", len(got.Services))
	}
}

func TestProjectedActions_RequiresExecutableNonConsentAction(t *testing.T) {
	node := &runpkg.RuntimeNode{
		Tools:          []string{"list_permissions", "add_viewer"},
		ConsentActions: []string{"add_viewer"},
		Executor: &runpkg.RuntimeExecutor{
			Actions: map[string]runpkg.RuntimeHTTPAction{
				"list_permissions": {Path: "/ok"},
			},
		},
	}
	got := projectedActions(&runpkg.Manifest{}, node, nil)
	if len(got) != 1 || got[0] != "list_permissions" {
		t.Fatalf("projectedActions() = %#v", got)
	}
}

func TestProjectedActions_RequiresConsentMetadataForConsentAction(t *testing.T) {
	node := &runpkg.RuntimeNode{
		Tools:          []string{"add_viewer"},
		ConsentActions: []string{"add_viewer"},
		Executor: &runpkg.RuntimeExecutor{
			Actions: map[string]runpkg.RuntimeHTTPAction{
				"add_viewer": {Path: "/ok"},
			},
		},
	}
	if got := projectedActions(&runpkg.Manifest{}, node, nil); len(got) != 0 {
		t.Fatalf("projectedActions() without deployment id = %#v", got)
	}
	node.ConsentRequirements = map[string]agencyconsent.Requirement{
		"add_viewer": {
			OperationKind:    "grant_drive_viewer",
			TokenInputField:  "consent_token",
			TargetInputField: "drive_id",
		},
	}
	got := projectedActions(&runpkg.Manifest{
		Source: runpkg.ManifestSource{ConsentDeploymentID: "dep-123"},
	}, node, nil)
	if len(got) != 1 || got[0] != "add_viewer" {
		t.Fatalf("projectedActions() with requirement = %#v", got)
	}
}

func TestGeneratorWritesGrantsFile(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "coordinator")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "constraints.yaml"), []byte("agent: coordinator\ngranted_capabilities: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentYAML := `
version: "0.1"
name: coordinator
role: test
body:
  runtime: body
  version: "1.0"
workspace:
  ref: default
`
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	gen := Generator{Home: home, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := gen.GenerateAgentManifest("coordinator"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(agentDir, "services.yaml")); err != nil {
		t.Fatalf("services.yaml not written: %v", err)
	}
}

func TestGeneratorAcceptsPresetScopeLoader(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "coordinator")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "services"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "constraints.yaml"), []byte("agent: coordinator\ngranted_capabilities: [demo]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("version: \"0.1\"\nname: coordinator\nrole: test\nbody:\n  runtime: body\n  version: \"1.0\"\nworkspace:\n  ref: default\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	serviceYAML := `
service: demo
display_name: Demo
api_base: https://demo.example.test
credential:
  env_var: DEMO_TOKEN
  header: Authorization
  scoped_prefix: agency-scoped-demo
tools:
  - name: allowed_action
    description: allowed
    scope: allowed
    path: /allowed
  - name: blocked_action
    description: blocked
    scope: blocked
    path: /blocked
`
	if err := os.WriteFile(filepath.Join(home, "services", "demo.yaml"), []byte(serviceYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	gen := Generator{
		Home:   home,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		LoadPresetScopes: func(string) map[string]map[string]bool {
			return map[string]map[string]bool{"demo": {"allowed": true}}
		},
	}
	if err := gen.GenerateAgentManifest("coordinator"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(agentDir, "services-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Services []struct {
			Service string `json:"service"`
			Tools   []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Services) != 1 || len(got.Services[0].Tools) != 1 || got.Services[0].Tools[0].Name != "allowed_action" {
		t.Fatalf("unexpected scoped tools: %#v", got.Services)
	}
}

func TestGeneratorUsesAgentValidatorForAttachments(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "coordinator")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "constraints.yaml"), []byte("agent: coordinator\ngranted_capabilities: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentYAML := `
version: "0.1"
name: coordinator
role: test
body:
  runtime: body
  version: "1.0"
workspace:
  ref: default
instances:
  attach:
    - instance_id: inst_drive
      node_id: drive_admin
    - instance_id: inst_drive
      node_id: drive_admin
`
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	gen := Generator{Home: home, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := gen.GenerateAgentManifest("coordinator"); err == nil {
		t.Fatal("expected duplicate attachment validation error")
	}
}

func TestGeneratorCanBeUsedRepeatedly(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "coordinator")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "constraints.yaml"), []byte("agent: coordinator\ngranted_capabilities: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("version: \"0.1\"\nname: coordinator\nrole: test\nbody:\n  runtime: body\n  version: \"1.0\"\nworkspace:\n  ref: default\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gen := Generator{Home: home, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for i := 0; i < 2; i++ {
		if err := gen.GenerateAgentManifest("coordinator"); err != nil {
			t.Fatalf("GenerateAgentManifest() run %d: %v", i, err)
		}
	}
}

func TestGeneratorAttachedAgents(t *testing.T) {
	home := t.TempDir()
	for _, agentName := range []string{"coordinator", "observer"} {
		agentDir := filepath.Join(home, "agents", agentName)
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			t.Fatal(err)
		}
		agentYAML := "version: \"0.1\"\nname: " + agentName + "\nrole: test\nbody:\n  runtime: body\n  version: \"1.0\"\nworkspace:\n  ref: default\n"
		if agentName == "coordinator" {
			agentYAML += "instances:\n  attach:\n    - instance_id: inst_drive\n      node_id: drive_admin\n"
		}
		if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gen := Generator{Home: home, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	agents, err := gen.AttachedAgents("inst_drive")
	if err != nil {
		t.Fatalf("AttachedAgents(): %v", err)
	}
	if len(agents) != 1 || agents[0] != "coordinator" {
		t.Fatalf("AttachedAgents() = %#v", agents)
	}
}
