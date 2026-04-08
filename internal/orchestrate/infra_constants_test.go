package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test file to find the repo root (contains go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod found)")
		}
		dir = parent
	}
}

// TestNetworkConstantsInDocs verifies that the actual Docker network names
// defined in Go constants appear in CLAUDE.md. Catches stale doc references
// when network names are renamed in code.
func TestNetworkConstantsInDocs(t *testing.T) {
	root := repoRoot(t)
	claudeMD := filepath.Join(root, "CLAUDE.md")

	data, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal("read CLAUDE.md:", err)
	}
	content := string(data)

	// These are the network constants from infra.go that should appear in docs.
	networks := map[string]string{
		"gatewayNet":   gatewayNet,
		"egressIntNet": egressIntNet,
		"egressExtNet": egressExtNet,
	}

	for constName, netName := range networks {
		if !strings.Contains(content, netName) {
			t.Errorf("CLAUDE.md does not mention network %q (Go const %s = %q) — update the Container topology section",
				netName, constName, netName)
		}
	}
}

// TestDefaultImagesHaveHealthChecks verifies that every infra image in
// defaultImages also has a health check defined. Missing health checks
// cause containers to never become "healthy" and block startup.
func TestDefaultImagesHaveHealthChecks(t *testing.T) {
	// These images intentionally have no health check (non-service or external)
	noHealthCheck := map[string]bool{
		"relay":      true,
		"web":        true,
		"embeddings": true,
	}

	for name := range defaultImages {
		if noHealthCheck[name] {
			continue
		}
		if _, ok := defaultHealthChecks[name]; !ok {
			t.Errorf("defaultImages has %q but defaultHealthChecks does not — infra up will never consider it healthy", name)
		}
	}
}
