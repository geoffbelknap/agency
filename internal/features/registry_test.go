package features

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExperimentalFeatureDisabledByDefault(t *testing.T) {
	t.Setenv("AGENCY_EXPERIMENTAL_SURFACES", "")
	if Enabled(Missions) {
		t.Fatal("missions should be disabled by default")
	}
}

func TestExperimentalFeatureEnabledWithFlag(t *testing.T) {
	t.Setenv("AGENCY_EXPERIMENTAL_SURFACES", "1")
	if !Enabled(Missions) {
		t.Fatal("missions should be enabled with experimental flag")
	}
}

func TestInternalFeatureDisabledEvenWithExperimentalFlag(t *testing.T) {
	t.Setenv("AGENCY_EXPERIMENTAL_SURFACES", "1")
	if Enabled(Embeddings) {
		t.Fatal("internal features should remain disabled")
	}
}

func TestCommandVisibilityFollowsRegistry(t *testing.T) {
	t.Setenv("AGENCY_EXPERIMENTAL_SURFACES", "")
	if CommandVisible("hub") {
		t.Fatal("hub command should be hidden by default")
	}
}

func TestWebManifestJSONIsInSync(t *testing.T) {
	root := repoRoot(t)
	got, err := WebManifestJSON()
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(root, "web", "src", "app", "lib", "feature-registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(got, '\n'), want) && !bytes.Equal(got, want) {
		t.Fatal("web feature manifest is out of sync")
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
