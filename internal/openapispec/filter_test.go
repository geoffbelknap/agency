package openapispec

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestFilterByTierCore(t *testing.T) {
	specPath := filepath.Join(repoRoot(t), "internal", "api", "openapi.yaml")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}

	filtered, err := FilterByTier(data, "core")
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(filtered, &doc); err != nil {
		t.Fatal(err)
	}

	if got := doc["x-agency-generated-view"]; got != "core" {
		t.Fatalf("expected generated view metadata, got %v", got)
	}

	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatal("filtered spec missing paths")
	}

	assertPathPresent(t, paths, "/graph/query")
	assertPathPresent(t, paths, "/admin/doctor")
	assertPathPresent(t, paths, "/infra/status")
	assertPathMissing(t, paths, "/graph/ontology")
	assertPathMissing(t, paths, "/admin/teams")
	assertPathMissing(t, paths, "/missions")
	assertPathMissing(t, paths, "/infra/internal/llm")

	tags, ok := doc["tags"].([]any)
	if !ok {
		t.Fatal("filtered spec missing tags")
	}

	tagNames := map[string]struct{}{}
	for _, item := range tags {
		tag, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := tag["name"].(string)
		tagNames[name] = struct{}{}
	}

	for _, expected := range []string{"Health", "Agents", "Admin", "Graph", "Infra", "Comms", "Creds", "MCP"} {
		if _, ok := tagNames[expected]; !ok {
			t.Fatalf("expected tag %q in core view", expected)
		}
	}
	for _, unexpected := range []string{"Missions", "Hub", "Events", "Internal", "AuthZ"} {
		if _, ok := tagNames[unexpected]; ok {
			t.Fatalf("did not expect tag %q in core view", unexpected)
		}
	}
}

func TestGeneratedCoreSpecIsInSync(t *testing.T) {
	root := repoRoot(t)
	canonicalPath := filepath.Join(root, "internal", "api", "openapi.yaml")
	corePath := filepath.Join(root, "internal", "api", "openapi-core.yaml")

	data, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := FilterByTier(data, "core")
	if err != nil {
		t.Fatal(err)
	}
	checkedIn, err := os.ReadFile(corePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, checkedIn) {
		t.Fatalf("generated core spec is out of sync with %s", corePath)
	}
}

func TestCoreInfraStatusSchemaIncludesOperatorBackendFields(t *testing.T) {
	specPath := filepath.Join(repoRoot(t), "internal", "api", "openapi.yaml")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := FilterByTier(data, "core")
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(filtered, &doc); err != nil {
		t.Fatal(err)
	}

	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatal("core spec missing paths")
	}
	infraStatus, ok := paths["/infra/status"].(map[string]any)
	if !ok {
		t.Fatal("core spec missing /infra/status")
	}
	get, ok := infraStatus["get"].(map[string]any)
	if !ok {
		t.Fatal("core spec missing /infra/status GET")
	}
	responses, ok := get["responses"].(map[string]any)
	if !ok {
		t.Fatal("core spec missing infra responses")
	}
	okResponse, ok := responses["200"].(map[string]any)
	if !ok {
		t.Fatal("core spec missing infra 200 response")
	}
	content, ok := okResponse["content"].(map[string]any)
	if !ok {
		t.Fatal("core spec missing infra content")
	}
	jsonContent, ok := content["application/json"].(map[string]any)
	if !ok {
		t.Fatal("core spec missing infra JSON content")
	}
	schema, ok := jsonContent["schema"].(map[string]any)
	if !ok {
		t.Fatal("core spec missing infra schema")
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("core spec missing infra properties")
	}

	for _, key := range []string{"gateway_url", "web_url", "container_backend", "backend", "backend_endpoint", "backend_mode", "infra_control_available", "host_runtime", "components"} {
		if _, ok := properties[key]; !ok {
			t.Fatalf("core infra status schema missing %q", key)
		}
	}
}

func assertPathPresent(t *testing.T, paths map[string]any, path string) {
	t.Helper()
	if _, ok := paths[path]; !ok {
		t.Fatalf("expected path %q to be present", path)
	}
}

func assertPathMissing(t *testing.T, paths map[string]any, path string) {
	t.Helper()
	if _, ok := paths[path]; ok {
		t.Fatalf("expected path %q to be absent", path)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(dir, "..", ".."))
}
