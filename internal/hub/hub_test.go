package hub

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSourceUpdateEmptyWhenNoChange(t *testing.T) {
	su := SourceUpdate{Name: "test", OldCommit: "abc1234", NewCommit: "abc1234"}
	if su.OldCommit != su.NewCommit {
		t.Fatal("expected same commit")
	}
	if su.CommitCount != 0 {
		t.Fatal("expected 0 commits")
	}
}

func TestOutdatedDetectsVersionDiff(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Create a fake installed instance with version 0.1.0
	inst, err := mgr.Registry.Create("test-connector", "connector", "default/test-connector")
	if err != nil {
		t.Fatal(err)
	}
	mgr.Registry.SetVersion(inst.Name, "0.1.0")

	// Write a fake component YAML in hub-cache with version 0.2.0
	cacheDir := filepath.Join(home, "hub-cache", "default", "connectors", "test-connector")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "connector.yaml"), []byte("name: test-connector\nversion: \"0.2.0\"\n"), 0644)

	// Write hub config
	os.MkdirAll(home, 0755)
	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	upgrades := mgr.Outdated()
	found := false
	for _, u := range upgrades {
		if u.Name == "test-connector" && u.InstalledVersion == "0.1.0" && u.AvailableVersion == "0.2.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected test-connector upgrade, got: %+v", upgrades)
	}
}

func TestUpdateReturnsReport(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// No sources configured — should return empty report, no error
	os.MkdirAll(home, 0755)
	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources: []\n"), 0644)

	report, err := mgr.Update()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Sources) != 0 {
		t.Fatalf("expected 0 sources, got %d", len(report.Sources))
	}
}

func TestUpgradeAllSyncsManagedFiles(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Set up hub cache with an ontology file
	cacheDir := filepath.Join(home, "hub-cache", "default")
	os.MkdirAll(filepath.Join(cacheDir, "ontology"), 0755)
	os.WriteFile(filepath.Join(cacheDir, "ontology", "base-ontology.yaml"),
		[]byte("version: v2\nentity_types:\n  Host: {}\n  Software: {}\n"), 0644)

	// Set up local ontology (older version)
	os.MkdirAll(filepath.Join(home, "knowledge"), 0755)
	os.WriteFile(filepath.Join(home, "knowledge", "base-ontology.yaml"),
		[]byte("version: v1\nentity_types:\n  Host: {}\n"), 0644)

	// Config
	os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	report, err := mgr.Upgrade(nil)
	if err != nil {
		t.Fatal(err)
	}

	foundOntology := false
	for _, f := range report.Files {
		if f.Category == "ontology" && f.Status == "upgraded" {
			foundOntology = true
		}
	}
	if !foundOntology {
		t.Fatalf("expected ontology upgrade, got: %+v", report.Files)
	}
}

func TestUpgradeSpecificComponent(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Create installed instance at version 0.1.0
	inst, _ := mgr.Registry.Create("test-conn", "connector", "default/test-conn")
	mgr.Registry.SetVersion(inst.Name, "0.1.0")

	// Write old YAML in instance dir
	instDir := mgr.Registry.InstanceDir(inst.Name)
	os.WriteFile(filepath.Join(instDir, "connector.yaml"),
		[]byte("kind: connector\nname: test-conn\nversion: \"0.1.0\"\nsource:\n  type: webhook\nroutes:\n  - match: {}\n    target:\n      agent: tester\n"), 0644)

	// Write new version in hub cache
	cacheDir := filepath.Join(home, "hub-cache", "default", "connectors", "test-conn")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "connector.yaml"),
		[]byte("kind: connector\nname: test-conn\nversion: \"0.2.0\"\nsource:\n  type: webhook\nroutes:\n  - match: {}\n    target:\n      agent: tester\n"), 0644)

	// Config
	os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	report, err := mgr.Upgrade([]string{"test-conn"})
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(report.Components))
	}
	cu := report.Components[0]
	if cu.Status != "upgraded" || cu.OldVersion != "0.1.0" || cu.NewVersion != "0.2.0" {
		t.Fatalf("unexpected component upgrade: %+v", cu)
	}

	// Verify version was updated in registry
	updated := mgr.Registry.Resolve("test-conn")
	if updated.Version != "0.2.0" {
		t.Fatalf("expected version 0.2.0, got %s", updated.Version)
	}
	pkg, ok := mgr.Registry.GetPackage("connector", "test-conn")
	if !ok {
		t.Fatal("expected installed package entry after upgrade")
	}
	if pkg.Version != "0.2.0" {
		t.Fatalf("expected installed package version 0.2.0, got %s", pkg.Version)
	}
}

func TestInstallRejectsManagedKinds(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	setupDir := filepath.Join(home, "hub-cache", "default", "setups", "default-wizard")
	os.MkdirAll(setupDir, 0755)
	os.WriteFile(filepath.Join(setupDir, "setup.yaml"), []byte("name: default-wizard\nkind: setup\nversion: \"1.0\"\n"), 0644)

	if _, err := mgr.Install("default-wizard", "setup", "", ""); err == nil {
		t.Fatal("expected setup install to be rejected")
	} else if !strings.Contains(err.Error(), "hub-managed") {
		t.Fatalf("expected hub-managed error, got %v", err)
	}

	if _, err := mgr.Install("base-ontology", "ontology", "", ""); err == nil {
		t.Fatal("expected ontology install to be rejected")
	} else if !strings.Contains(err.Error(), "hub-managed") {
		t.Fatalf("expected hub-managed error, got %v", err)
	}
}

func TestInstallRejectsPackageEnvelopeComponents(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	pkgDir := filepath.Join(home, "hub-cache", "default", "connectors", "slack-interactivity")
	os.MkdirAll(pkgDir, 0755)
	os.WriteFile(filepath.Join(pkgDir, "package.yaml"), []byte(`api_version: hub.agency/v2
kind: connector
metadata:
  name: slack-interactivity
  version: 1.0.0
trust:
  tier: verified
  signature_required: true
  executable: true
`), 0644)

	if _, err := mgr.Install("slack-interactivity", "connector", "", ""); err == nil {
		t.Fatal("expected package-envelope install to be rejected")
	} else if !strings.Contains(err.Error(), "package envelope") {
		t.Fatalf("expected package envelope error, got %v", err)
	}
}

func TestInstallConnectorPublishesPackageSpec(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	cacheDir := filepath.Join(home, "hub-cache", "default", "connectors", "google-drive-admin")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "connector.yaml"), []byte(`kind: connector
name: google-drive-admin
version: "1.0.0"
requires:
  credentials:
    - name: google-drive-admin
      scope: service-grant
  auth:
    type: google_service_account
    scopes:
      - https://www.googleapis.com/auth/drive
source:
  type: none
mcp:
  name: google-drive-admin
  credential: google-drive-admin
  api_base: https://www.googleapis.com
  tools:
    - name: drive_share_file
      method: POST
      path: /drive/v3/files/{file_id}/permissions
      query_params: [sendNotificationEmail]
      parameters:
        file_id: {type: string}
        emailAddress: {type: string}
        role: {type: string}
        sendNotificationEmail: {type: boolean}
rate_limits:
  max_per_hour: 100
  max_concurrent: 10
`), 0644)

	if _, err := mgr.Install("google-drive-admin", "connector", "", ""); err != nil {
		t.Fatalf("Install(): %v", err)
	}
	pkg, ok := mgr.Registry.GetPackage("connector", "google-drive-admin")
	if !ok {
		t.Fatal("expected installed package entry")
	}
	if len(pkg.Assurance) == 0 {
		t.Fatalf("expected assurance metadata, got %#v", pkg)
	}
	runtimeSpec, ok := pkg.Spec["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("missing runtime spec: %#v", pkg.Spec)
	}
	executor, ok := runtimeSpec["executor"].(map[string]any)
	if !ok {
		t.Fatalf("missing executor: %#v", runtimeSpec)
	}
	auth, ok := executor["auth"].(map[string]any)
	if !ok || auth["type"] != "google_service_account" {
		t.Fatalf("unexpected auth: %#v", executor["auth"])
	}
	actions, ok := executor["actions"].(map[string]any)
	if !ok {
		t.Fatalf("missing actions: %#v", executor)
	}
	action, ok := actions["drive_share_file"].(map[string]any)
	if !ok {
		t.Fatalf("missing drive_share_file action: %#v", actions)
	}
	if _, ok := action["query"].(map[string]any); !ok {
		t.Fatalf("expected query mapping: %#v", action)
	}
	if _, ok := action["body"].(map[string]any); !ok {
		t.Fatalf("expected body mapping: %#v", action)
	}
}

func TestInstallFetchesStructuredAssuranceFromHubAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/v1/hubs/default/artifacts/connector/google-drive-admin/1.0.0/assurance"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "schema_version": 1,
		  "hub_id": "hub:default:test",
		  "statements": [
		    {
		      "artifact": {
		        "kind": "connector",
		        "name": "google-drive-admin",
		        "version": "1.0.0"
		      },
		      "issuer": {
		        "hub_id": "hub:default:test",
		        "statement_id": "stmt-1"
		      },
		      "statement_type": "ask_reviewed",
		      "result": "ASK-Partial",
		      "review_scope": "package-change",
		      "reviewer_type": "automated",
		      "policy_version": "2026-04-12"
		    }
		  ]
		}`))
	}))
	defer server.Close()

	home := t.TempDir()
	mgr := NewManager(home)

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n      api: "+server.URL+"\n"), 0644)

	cacheDir := filepath.Join(home, "hub-cache", "default", "connectors", "google-drive-admin")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "connector.yaml"), []byte(`kind: connector
name: google-drive-admin
version: "1.0.0"
source:
  type: none
mcp:
  name: google-drive-admin
  credential: google-drive-admin
  api_base: https://www.googleapis.com
  tools:
    - name: drive_list_file_permissions
      method: GET
      path: /drive/v3/files/{file_id}/permissions
      parameters:
        file_id: {type: string}
`), 0644)

	if _, err := mgr.Install("google-drive-admin", "connector", "", ""); err != nil {
		t.Fatalf("Install(): %v", err)
	}
	pkg, ok := mgr.Registry.GetPackage("connector", "google-drive-admin")
	if !ok {
		t.Fatal("expected installed package entry")
	}
	if len(pkg.AssuranceStatements) != 1 {
		t.Fatalf("expected 1 structured assurance statement, got %#v", pkg.AssuranceStatements)
	}
	if pkg.AssuranceStatements[0].Result != "ASK-Partial" {
		t.Fatalf("unexpected structured assurance: %#v", pkg.AssuranceStatements[0])
	}
	if pkg.AssuranceIssuer != "hub:default:test" {
		t.Fatalf("unexpected assurance issuer: %q", pkg.AssuranceIssuer)
	}
	if !containsString(pkg.Assurance, "ask_partial") {
		t.Fatalf("expected legacy assurance projection, got %#v", pkg.Assurance)
	}
}

func TestInstallIgnoresUnapprovedHubAssuranceAPI(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer server.Close()

	home := t.TempDir()
	mgr := NewManager(home)

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: partner\n      url: https://example.com\n      api: "+server.URL+"\n"), 0644)

	cacheDir := filepath.Join(home, "hub-cache", "partner", "connectors", "google-drive-admin")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "connector.yaml"), []byte(`kind: connector
name: google-drive-admin
version: "1.0.0"
source:
  type: none
mcp:
  name: google-drive-admin
  credential: google-drive-admin
  api_base: https://www.googleapis.com
  tools:
    - name: drive_list_file_permissions
      method: GET
      path: /drive/v3/files/{file_id}/permissions
      parameters:
        file_id: {type: string}
`), 0644)

	if _, err := mgr.Install("google-drive-admin", "connector", "partner", ""); err != nil {
		t.Fatalf("Install(): %v", err)
	}
	if called {
		t.Fatal("expected unapproved hub API to be ignored")
	}
	pkg, ok := mgr.Registry.GetPackage("connector", "google-drive-admin")
	if !ok {
		t.Fatal("expected installed package entry")
	}
	if len(pkg.AssuranceStatements) != 0 {
		t.Fatalf("expected no structured assurance statements, got %#v", pkg.AssuranceStatements)
	}
	if pkg.AssuranceIssuer != "" {
		t.Fatalf("expected no assurance issuer, got %q", pkg.AssuranceIssuer)
	}
}

func TestBuildInstalledPackageTreatsDefaultSourceAsOfficial(t *testing.T) {
	home := t.TempDir()
	componentDir := filepath.Join(home, "connectors")
	if err := os.MkdirAll(componentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	connectorPath := filepath.Join(componentDir, "slack-interactivity.yaml")
	if err := os.WriteFile(connectorPath, []byte(`kind: connector
name: slack-interactivity
version: "1.1.0"
source:
  type: webhook
  path: /webhooks/slack-interactivity
routes:
  - match:
      payload_type: shortcut
    target:
      agent: slack-operator
`), 0o644); err != nil {
		t.Fatalf("write connector: %v", err)
	}

	pkg, err := buildInstalledPackage("slack-interactivity", "connector", "1.1.0", "default/slack-interactivity", connectorPath, nil)
	if err != nil {
		t.Fatalf("buildInstalledPackage(): %v", err)
	}
	if got, want := pkg.Trust, "verified"; got != want {
		t.Fatalf("Trust = %q, want %q", got, want)
	}
	if !containsString(pkg.Assurance, "official_source") {
		t.Fatalf("expected official_source assurance, got %#v", pkg.Assurance)
	}
	if !containsString(pkg.Assurance, "ask_partial") {
		t.Fatalf("expected ask_partial assurance, got %#v", pkg.Assurance)
	}
}

func TestUpgradeRefreshesInstalledPackageMetadataWhenVersionIsUnchanged(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      registry: ghcr.io/geoffbelknap/agency-hub\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cacheDir := filepath.Join(home, "hub-cache", "default", "connectors", "slack-interactivity")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	componentYAML := []byte(`kind: connector
name: slack-interactivity
version: "1.1.0"
source:
  type: webhook
  path: /webhooks/slack-interactivity
routes:
  - match:
      payload_type: shortcut
    target:
      agent: slack-operator
`)
	if err := os.WriteFile(filepath.Join(cacheDir, "connector.yaml"), componentYAML, 0o644); err != nil {
		t.Fatalf("write cache component: %v", err)
	}

	inst, err := mgr.Registry.Create("slack-interactivity", "connector", "default/slack-interactivity")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	if err := mgr.Registry.SetVersion(inst.Name, "1.1.0"); err != nil {
		t.Fatalf("set version: %v", err)
	}
	instDir := mgr.Registry.InstanceDir(inst.Name)
	if err := os.WriteFile(filepath.Join(instDir, "connector.yaml"), componentYAML, 0o644); err != nil {
		t.Fatalf("write installed component: %v", err)
	}
	if err := mgr.Registry.PutPackage(InstalledPackage{
		Kind:      "connector",
		Name:      "slack-interactivity",
		Version:   "1.1.0",
		Trust:     "local",
		Path:      filepath.Join(instDir, "connector.yaml"),
		Assurance: []string{"ask_partial"},
	}); err != nil {
		t.Fatalf("seed package: %v", err)
	}

	report, err := mgr.Upgrade([]string{"slack-interactivity"})
	if err != nil {
		t.Fatalf("Upgrade(): %v", err)
	}
	if len(report.Components) != 1 || report.Components[0].Status != "unchanged" {
		t.Fatalf("unexpected upgrade report: %#v", report.Components)
	}

	pkg, ok := mgr.Registry.GetPackage("connector", "slack-interactivity")
	if !ok {
		t.Fatal("expected installed package entry")
	}
	if got, want := pkg.Trust, "verified"; got != want {
		t.Fatalf("Trust = %q, want %q", got, want)
	}
	if !containsString(pkg.Assurance, "official_source") {
		t.Fatalf("expected official_source assurance, got %#v", pkg.Assurance)
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func TestInstallableKindSemantics(t *testing.T) {
	for _, kind := range []string{"pack", "preset", "connector", "service", "mission", "skill", "workspace", "policy", "provider"} {
		if !IsInstallableKind(kind) {
			t.Fatalf("expected %s to be installable", kind)
		}
	}
	for _, kind := range []string{"ontology", "setup", "unknown"} {
		if IsInstallableKind(kind) {
			t.Fatalf("expected %s to be non-installable", kind)
		}
	}
}

func TestOutdatedNoChangeWhenVersionsMatch(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	inst, err := mgr.Registry.Create("test-connector", "connector", "default/test-connector")
	if err != nil {
		t.Fatal(err)
	}
	mgr.Registry.SetVersion(inst.Name, "0.1.0")

	cacheDir := filepath.Join(home, "hub-cache", "default", "connectors", "test-connector")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "connector.yaml"), []byte("name: test-connector\nversion: \"0.1.0\"\n"), 0644)

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	upgrades := mgr.Outdated()
	if len(upgrades) != 0 {
		t.Fatalf("expected no upgrades, got: %+v", upgrades)
	}
}

func TestUpgradePreservesInstalledProviders(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Hub cache with Anthropic + OpenAI defaults
	hubRoutingYAML := `version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com
    auth_env: ANTHROPIC_API_KEY
  openai:
    api_base: https://api.openai.com
    auth_env: OPENAI_API_KEY
models:
  claude-sonnet:
    provider: anthropic
    capabilities: [tools, vision, streaming]
  gpt-4o:
    provider: openai
    capabilities: [tools, vision, streaming]
tiers:
  standard:
    - model: claude-sonnet
      preference: 0
    - model: gpt-4o
      preference: 1
settings:
  default_tier: standard
  tier_strategy: best_effort
`
	cacheDir := filepath.Join(home, "hub-cache", "default")
	os.MkdirAll(filepath.Join(cacheDir, "pricing"), 0755)
	os.WriteFile(filepath.Join(cacheDir, "pricing", "routing.yaml"), []byte(hubRoutingYAML), 0644)

	// Ontology in cache (upgrade syncs ontology too)
	os.MkdirAll(filepath.Join(cacheDir, "ontology"), 0755)
	os.WriteFile(filepath.Join(cacheDir, "ontology", "base-ontology.yaml"),
		[]byte("version: v2\nentity_types:\n  Host: {}\n"), 0644)

	// Config with hub sources
	os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	// Write the hub base routing.yaml to infrastructure/
	os.MkdirAll(filepath.Join(home, "infrastructure"), 0755)
	os.WriteFile(filepath.Join(home, "infrastructure", "routing.yaml"), []byte(hubRoutingYAML), 0644)

	// Register a non-default provider "together-ai" in the registry
	_, err := mgr.Registry.Create("together-ai", "provider", "default/together-ai")
	if err != nil {
		t.Fatal(err)
	}

	// Write provider YAML to the instance dir
	instDir := mgr.Registry.InstanceDir("together-ai")
	if instDir == "" {
		t.Fatal("expected instance dir for together-ai")
	}
	providerYAML := `provider: together-ai
version: "1.0.0"
routing:
  api_base: https://api.together.xyz
  auth_env: OPENAI_API_KEY
  models:
    together-llama:
      capabilities: [tools, streaming]
  tiers:
    standard: together-llama
`
	os.WriteFile(filepath.Join(instDir, "provider.yaml"), []byte(providerYAML), 0644)

	// Merge provider routing (simulates hub install)
	if err := MergeProviderRouting(home, "together-ai", []byte(providerYAML)); err != nil {
		t.Fatalf("MergeProviderRouting failed: %v", err)
	}

	// Verify together-ai is in routing.yaml before upgrade
	preData, _ := os.ReadFile(filepath.Join(home, "infrastructure", "routing.yaml"))
	if !strings.Contains(string(preData), "together-ai") {
		t.Fatal("expected together-ai in routing.yaml before upgrade")
	}

	// Run full upgrade
	_, err = mgr.Upgrade(nil)
	if err != nil {
		t.Fatalf("Upgrade failed: %v", err)
	}

	// Read the routing.yaml after upgrade
	postData, err := os.ReadFile(filepath.Join(home, "infrastructure", "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(postData, &cfg); err != nil {
		t.Fatal(err)
	}

	providers, _ := cfg["providers"].(map[string]interface{})
	models, _ := cfg["models"].(map[string]interface{})

	// Assert: together-ai provider still present with correct api_base
	togetherProv, ok := providers["together-ai"].(map[string]interface{})
	if !ok {
		t.Fatalf("together-ai provider missing after upgrade; providers: %v", providers)
	}
	if togetherProv["api_base"] != "https://api.together.xyz" {
		t.Errorf("expected together-ai api_base https://api.together.xyz, got %v", togetherProv["api_base"])
	}

	// Assert: together-ai model still present
	if _, ok := models["together-llama"]; !ok {
		t.Errorf("together-llama model missing after upgrade; models: %v", models)
	}

	// Assert: default providers still present and match hub cache
	if _, ok := providers["anthropic"]; !ok {
		t.Errorf("anthropic provider missing after upgrade")
	}
	if _, ok := providers["openai"]; !ok {
		t.Errorf("openai provider missing after upgrade")
	}

	// Assert: default models still present
	if _, ok := models["claude-sonnet"]; !ok {
		t.Errorf("claude-sonnet model missing after upgrade")
	}
	if _, ok := models["gpt-4o"]; !ok {
		t.Errorf("gpt-4o model missing after upgrade")
	}
}

func TestUpgradeDoesNotPreserveRemovedProviders(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Hub cache with just anthropic
	hubRoutingYAML := `version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com
    auth_env: ANTHROPIC_API_KEY
models:
  claude-sonnet:
    provider: anthropic
    capabilities: [tools, vision, streaming]
tiers:
  standard:
    - model: claude-sonnet
      preference: 0
settings:
  default_tier: standard
  tier_strategy: best_effort
`
	cacheDir := filepath.Join(home, "hub-cache", "default")
	os.MkdirAll(filepath.Join(cacheDir, "pricing"), 0755)
	os.WriteFile(filepath.Join(cacheDir, "pricing", "routing.yaml"), []byte(hubRoutingYAML), 0644)

	// Config
	os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	// Write routing.yaml that has a stale provider "stale-provider" (NOT in registry)
	staleRoutingYAML := `version: "0.1"
providers:
  anthropic:
    api_base: https://api.anthropic.com
    auth_env: ANTHROPIC_API_KEY
  stale-provider:
    api_base: https://api.stale.com
    auth_env: OPENAI_API_KEY
models:
  claude-sonnet:
    provider: anthropic
    capabilities: [tools, vision, streaming]
  stale-model:
    provider: stale-provider
    capabilities: [tools]
tiers:
  standard:
    - model: claude-sonnet
      preference: 0
    - model: stale-model
      preference: 1
settings:
  default_tier: standard
  tier_strategy: best_effort
`
	os.MkdirAll(filepath.Join(home, "infrastructure"), 0755)
	os.WriteFile(filepath.Join(home, "infrastructure", "routing.yaml"), []byte(staleRoutingYAML), 0644)

	// Run full upgrade
	_, err := mgr.Upgrade(nil)
	if err != nil {
		t.Fatalf("Upgrade failed: %v", err)
	}

	// Read routing.yaml after upgrade
	postData, err := os.ReadFile(filepath.Join(home, "infrastructure", "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// Assert: stale-provider is gone (hub base overwrites, and it's not in the registry)
	if strings.Contains(string(postData), "stale-provider") {
		t.Errorf("stale-provider should have been removed after upgrade, but found in routing.yaml")
	}
	if strings.Contains(string(postData), "stale-model") {
		t.Errorf("stale-model should have been removed after upgrade, but found in routing.yaml")
	}

	// Verify anthropic is still there
	var cfg map[string]interface{}
	yaml.Unmarshal(postData, &cfg)
	providers, _ := cfg["providers"].(map[string]interface{})
	if _, ok := providers["anthropic"]; !ok {
		t.Errorf("anthropic provider should still be present after upgrade")
	}
}

func TestDiscoverFindsProviderComponent(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	// Write hub config defining a source
	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	// Create provider cache dir and YAML using provider: as the identifier key
	providerDir := filepath.Join(home, "hub-cache", "default", "providers", "anthropic")
	os.MkdirAll(providerDir, 0755)
	os.WriteFile(filepath.Join(providerDir, "provider.yaml"),
		[]byte("provider: anthropic\nversion: \"1.0.0\"\ndescription: Anthropic Claude provider\n"), 0644)

	components := mgr.discover()

	var found *Component
	for i := range components {
		if components[i].Kind == "provider" && components[i].Name == "anthropic" {
			found = &components[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected to find provider component 'anthropic', got: %+v", components)
	}
	if found.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", found.Version)
	}
}

func TestDiscoverFindsPackageEnvelope(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	pkgDir := filepath.Join(home, "hub-cache", "default", "connectors", "slack-interactivity")
	os.MkdirAll(pkgDir, 0755)
	os.WriteFile(filepath.Join(pkgDir, "package.yaml"), []byte(`api_version: hub.agency/v2
kind: connector
metadata:
  name: slack-interactivity
  version: 1.0.0
trust:
  tier: verified
  signature_required: true
  executable: true
`), 0644)

	results := mgr.Search("slack-interactivity", "connector")
	if len(results) != 1 {
		t.Fatalf("expected one connector search result, got %+v", results)
	}
	if results[0].Name != "slack-interactivity" || results[0].Kind != "connector" {
		t.Fatalf("unexpected package result: %+v", results[0])
	}
	if results[0].Version != "1.0.0" {
		t.Fatalf("version = %q, want 1.0.0", results[0].Version)
	}
	if !strings.HasSuffix(results[0].Path, filepath.Join("connectors", "slack-interactivity", "package.yaml")) {
		t.Fatalf("path = %q, want package.yaml path", results[0].Path)
	}
}

func TestDiscoverFindsSetupComponent(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	setupDir := filepath.Join(home, "hub-cache", "default", "setup", "default-wizard")
	os.MkdirAll(setupDir, 0755)
	os.WriteFile(filepath.Join(setupDir, "setup.yaml"),
		[]byte("name: default-wizard\nkind: setup\nversion: \"1.0\"\ndescription: Setup wizard\n"), 0644)

	results := mgr.Search("default-wizard", "setup")
	if len(results) != 1 {
		t.Fatalf("expected one setup search result, got %+v", results)
	}
	if results[0].Name != "default-wizard" || results[0].Kind != "setup" {
		t.Fatalf("unexpected setup result: %+v", results[0])
	}
	if !strings.HasSuffix(results[0].Path, filepath.Join("setup", "default-wizard", "setup.yaml")) {
		t.Fatalf("path = %q, want setup.yaml path", results[0].Path)
	}
}

func TestInfoIncludesInstalledProvenance(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	setupDir := filepath.Join(home, "hub-cache", "default", "setup", "default-wizard")
	os.MkdirAll(setupDir, 0755)
	os.WriteFile(filepath.Join(setupDir, "setup.yaml"),
		[]byte("name: default-wizard\nkind: setup\nversion: \"1.0\"\ndescription: Setup wizard\n"), 0644)
	os.WriteFile(filepath.Join(home, "hub-installed.json"),
		[]byte(`[{"name":"default-wizard","kind":"setup","source":"default","installed_at":"2026-04-10T17:00:00Z"}]`), 0644)

	info, err := mgr.Info("default-wizard", "setup")
	if err != nil {
		t.Fatalf("Info returned error: %v", err)
	}
	if info["_installed"] != true {
		t.Fatalf("expected installed provenance, got %+v", info)
	}
	if info["_installed_at"] != "2026-04-10T17:00:00Z" {
		t.Fatalf("unexpected installed_at: %+v", info["_installed_at"])
	}
	if info["_installed_source"] != "default" {
		t.Fatalf("unexpected installed source: %+v", info["_installed_source"])
	}
}

func TestDiscoverFindsMarkdownSkillComponent(t *testing.T) {
	home := t.TempDir()
	mgr := NewManager(home)

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte("hub:\n  sources:\n    - name: default\n      url: https://example.com\n"), 0644)

	skillDir := filepath.Join(home, "hub-cache", "default", "skills", "code-review")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: code-review
version: "1.0"
description: Review code changes
---

# Code Review
`), 0644)

	results := mgr.Search("code-review", "skill")
	if len(results) != 1 {
		t.Fatalf("expected one skill search result, got %+v", results)
	}
	if results[0].Name != "code-review" || results[0].Kind != "skill" {
		t.Fatalf("unexpected skill result: %+v", results[0])
	}
	if results[0].Version != "1.0" {
		t.Fatalf("version = %q, want 1.0", results[0].Version)
	}
	if !strings.HasSuffix(results[0].Path, filepath.Join("skills", "code-review", "SKILL.md")) {
		t.Fatalf("path = %q, want SKILL.md path", results[0].Path)
	}
}
