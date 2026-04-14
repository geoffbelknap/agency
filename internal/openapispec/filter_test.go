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
