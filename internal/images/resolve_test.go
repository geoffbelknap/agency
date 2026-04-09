package images

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/docker/docker/client"
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

// TestBuildContextConsistency verifies that every Dockerfile's COPY sources
// exist relative to the build context that buildFromSource would use.
//
// This catches the bug where repoContextNames drifts from Dockerfiles:
// e.g., a Dockerfile does "COPY images/logging_config.py" (needs repo root)
// but isn't listed in repoContextNames (gets images/<name>/ as context).
func TestBuildContextConsistency(t *testing.T) {
	root := repoRoot(t)
	imagesDir := filepath.Join(root, "images")

	entries, err := os.ReadDir(imagesDir)
	if err != nil {
		t.Fatal(err)
	}

	// Determine which names are repo-context vs self-contained.
	// This must match the map in buildFromSource.
	repoContext := map[string]bool{
		"body": true, "comms": true, "knowledge": true, "intake": true, "egress": true,
	}

	copyRe := regexp.MustCompile(`^COPY\s+(\S+)`)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		dockerfile := filepath.Join(imagesDir, name, "Dockerfile")
		if _, err := os.Stat(dockerfile); err != nil {
			continue // no Dockerfile — not a buildable image
		}

		var contextDir string
		if repoContext[name] {
			contextDir = root
		} else {
			contextDir = filepath.Join(imagesDir, name)
		}

		f, err := os.Open(dockerfile)
		if err != nil {
			t.Errorf("%s: open Dockerfile: %v", name, err)
			continue
		}

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := strings.TrimSpace(scanner.Text())

			// Skip comments, multi-stage COPY --from, and ARG/ENV lines
			if strings.HasPrefix(line, "#") {
				continue
			}
			m := copyRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			src := m[1]
			if strings.HasPrefix(src, "--from=") {
				continue // multi-stage copy — source is another build stage
			}

			// Resolve the COPY source relative to the build context.
			// Handle glob patterns (e.g., "*.py", "images/body/*.py")
			fullPath := filepath.Join(contextDir, src)
			found := false
			if strings.ContainsAny(src, "*?[") {
				matches, _ := filepath.Glob(fullPath)
				found = len(matches) > 0
			} else {
				_, statErr := os.Stat(fullPath)
				found = statErr == nil
			}
			if found {
				continue
			}

			if repoContext[name] {
				t.Errorf("%s (Dockerfile:%d): COPY source %q not found relative to repo root %q",
					name, lineNum, src, contextDir)
			} else {
				// Check if it would exist with repo root context — suggests missing repoContextNames entry
				if _, err2 := os.Stat(filepath.Join(root, src)); err2 == nil {
					t.Errorf("%s (Dockerfile:%d): COPY source %q exists at repo root but image uses self-contained context %q — add %q to repoContextNames in resolve.go",
						name, lineNum, src, contextDir, name)
				} else {
					t.Errorf("%s (Dockerfile:%d): COPY source %q not found at %q",
						name, lineNum, src, fullPath)
				}
			}
		}
		f.Close()
	}
}

// TestRepoContextMatchesMakefile verifies the repoContextNames map in
// buildFromSource matches REPO_CONTEXT_IMAGES in the Makefile.
func TestRepoContextMatchesMakefile(t *testing.T) {
	root := repoRoot(t)
	makefile := filepath.Join(root, "Makefile")

	data, err := os.ReadFile(makefile)
	if err != nil {
		t.Fatal("read Makefile:", err)
	}

	// Parse REPO_CONTEXT_IMAGES from Makefile
	re := regexp.MustCompile(`REPO_CONTEXT_IMAGES\s*=\s*(.+)`)
	m := re.FindSubmatch(data)
	if m == nil {
		t.Fatal("REPO_CONTEXT_IMAGES not found in Makefile")
	}

	makefileImages := map[string]bool{}
	for _, name := range strings.Fields(string(m[1])) {
		makefileImages[name] = true
	}

	// This must match the map in buildFromSource — keep in sync.
	goImages := map[string]bool{
		"body": true, "comms": true, "knowledge": true, "intake": true, "egress": true,
	}

	for name := range makefileImages {
		if !goImages[name] {
			t.Errorf("Makefile REPO_CONTEXT_IMAGES has %q but resolve.go repoContextNames does not — add it to the Go map", name)
		}
	}
	for name := range goImages {
		if !makefileImages[name] {
			t.Errorf("resolve.go repoContextNames has %q but Makefile REPO_CONTEXT_IMAGES does not — add it to the Makefile", name)
		}
	}
}

func TestResolveUpstream_SkipsIfCurrent(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available:", err)
	}
	err = ResolveUpstream(context.Background(), cli, "nonexistent-test-xyz", "0.0.0", "test:0.0.0", "bid", nil)
	if err == nil {
		t.Error("expected error for nonexistent upstream image")
	}
}

func TestImageExists_False(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available:", err)
	}
	exists, err := imageExists(context.Background(), cli, "agency-nonexistent-test-image-xyz:latest")
	if err != nil {
		t.Skip("Docker not responding:", err)
	}
	if exists {
		t.Error("expected nonexistent image to return false")
	}
}

func TestDirtyBuildIDIncludesContentHash(t *testing.T) {
	makefilePath := filepath.Join(repoRoot(t), "Makefile")
	data, err := os.ReadFile(makefilePath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "DIRTY_HASH :=") {
		t.Fatal("Makefile is missing DIRTY_HASH computation")
	}
	if !strings.Contains(content, "-dirty.$(DIRTY_HASH)") {
		t.Fatal("Makefile dirty BUILD_ID suffix must include the diff hash")
	}
}
