package images

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
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

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		dockerfile := filepath.Join(imagesDir, name, "Dockerfile")
		if _, err := os.Stat(dockerfile); err != nil {
			continue // no Dockerfile — not a buildable image
		}

		spec, err := sourceBuildSpec(name, root)
		if err != nil {
			t.Errorf("%s: sourceBuildSpec: %v", name, err)
			continue
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
			if !strings.HasPrefix(strings.ToUpper(line), "COPY ") && !strings.HasPrefix(strings.ToUpper(line), "ADD ") {
				continue
			}
			sources, namedSources, err := dockerfileSources(dockerfile)
			if err != nil {
				t.Errorf("%s: parse Dockerfile: %v", name, err)
				break
			}
			checkSourcePaths(t, name, lineNum, spec.contextDir, sources)
			for ctxName, ctxSources := range namedSources {
				checkSourcePaths(t, name, lineNum, spec.namedContexts[ctxName], ctxSources)
			}
			break
		}
		f.Close()
	}
}

func checkSourcePaths(t *testing.T, name string, lineNum int, contextDir string, sources []string) {
	t.Helper()
	for _, src := range sources {
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
		t.Errorf("%s (Dockerfile:%d): COPY source %q not found relative to %q", name, lineNum, src, contextDir)
	}
}

func TestRewriteDockerfileForNamedContextsPreservesDirectorySlash(t *testing.T) {
	input := "COPY --from=shared models/ /app/images/models/\n"
	got := rewriteDockerfileForNamedContexts(input, map[string]string{"shared": "/repo/images"})
	want := "COPY _ctx_shared/models/ /app/images/models/\n"
	if got != want {
		t.Fatalf("rewriteDockerfileForNamedContexts() = %q, want %q", got, want)
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

	goImages := map[string]bool{
		"intake": true,
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

func TestPythonBaseDependenciesMatchMakefile(t *testing.T) {
	root := repoRoot(t)
	makefile := filepath.Join(root, "Makefile")

	data, err := os.ReadFile(makefile)
	if err != nil {
		t.Fatal("read Makefile:", err)
	}

	re := regexp.MustCompile(`(?m)^([^\n:]+):\s+python-base\s*$`)
	matches := re.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatal("python-base dependencies not found in Makefile")
	}

	makeDeps := map[string]bool{}
	for _, match := range matches {
		for _, name := range strings.Fields(match[1]) {
			makeDeps[name] = true
		}
	}

	goDeps := map[string]bool{}
	for _, name := range sourceImageDependencies["body"] {
		_ = name
	}
	for imageName, deps := range sourceImageDependencies {
		for _, dep := range deps {
			if dep == "python-base" {
				goDeps[imageName] = true
			}
		}
	}

	for name := range makeDeps {
		if !goDeps[name] {
			t.Errorf("Makefile declares %q depends on python-base, but resolve.go does not", name)
		}
	}
	for name := range goDeps {
		if !makeDeps[name] {
			t.Errorf("resolve.go declares %q depends on python-base, but Makefile does not", name)
		}
	}
}

func TestResolveUpstream_SkipsIfCurrent(t *testing.T) {
	cli, err := runtimehost.NewRawClient()
	if err != nil {
		t.Skip("Docker not available:", err)
	}
	err = ResolveUpstream(context.Background(), cli, "nonexistent-test-xyz", "0.0.0", "test:0.0.0", "bid", nil)
	if err == nil {
		t.Error("expected error for nonexistent upstream image")
	}
}

func TestImageExists_False(t *testing.T) {
	cli, err := runtimehost.NewRawClient()
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

func TestMakefileDoesNotForceCacheBust(t *testing.T) {
	makefilePath := filepath.Join(repoRoot(t), "Makefile")
	data, err := os.ReadFile(makefilePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "CACHE_BUST=$$$$(date +%s)") || strings.Contains(string(data), "CACHE_BUST=$(date +%s)") {
		t.Fatal("Makefile image builds must not force timestamp cache busts")
	}
}

func TestSourceFingerprintIgnoresUncopiedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\nCOPY app.py /app.py\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.py"), []byte("print('one')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := sourceFingerprint(dir, "Dockerfile", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := sourceFingerprint(dir, "Dockerfile", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("source fingerprint changed for a file not copied by the Dockerfile")
	}
	if err := os.WriteFile(filepath.Join(dir, "app.py"), []byte("print('two')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := sourceFingerprint(dir, "Dockerfile", nil)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("source fingerprint did not change for a copied file")
	}
}
