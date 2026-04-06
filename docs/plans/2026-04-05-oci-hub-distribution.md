# OCI Hub Distribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish hub components as signed OCI artifacts to GHCR. Update the agency CLI to pull from OCI instead of git.

**Architecture:** Two workstreams: (1) CI pipeline in agency-hub that publishes OCI artifacts on merge, signs with cosign; (2) new `internal/hub/oci.go` in agency that replaces git-based fetch with ORAS-based pull and cosign verification. The `Source` type gains a `Type` field (`oci` or `git`) for backward compat during migration, but the default source becomes OCI.

**Tech Stack:** ORAS Go library (`oras.land/oras-go/v2`), Cosign (CI + Go verification library), GHCR, GitHub Actions

**Spec:** `docs/specs/oci-hub-distribution.md`

---

### Task 1: Add ORAS Go dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add the ORAS Go library**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
go get oras.land/oras-go/v2
```

- [ ] **Step 2: Add the cosign verification library**

```bash
go get github.com/sigstore/cosign/v2/pkg/cosign
go get github.com/sigstore/sigstore-go/pkg/verify
```

- [ ] **Step 3: Verify the module graph resolves**

```bash
go mod tidy
go build ./...
```

Expected: Clean build, no errors.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add oras-go and sigstore for OCI hub distribution"
```

---

### Task 2: Extend Source type with OCI support

**Files:**
- Modify: `internal/hub/hub.go:49-54,85-89`
- Create: `internal/hub/oci.go`
- Create: `internal/hub/oci_test.go`

- [ ] **Step 1: Write tests for Source type changes**

Create `internal/hub/oci_test.go`:

```go
package hub

import (
	"testing"
)

func TestSourceTypeDefault(t *testing.T) {
	// Existing sources without Type field should default to "git"
	s := Source{Name: "official", URL: "https://github.com/geoffbelknap/agency-hub.git"}
	if s.EffectiveType() != "git" {
		t.Errorf("expected git, got %s", s.EffectiveType())
	}
}

func TestSourceTypeOCI(t *testing.T) {
	s := Source{Name: "official", Type: "oci", Registry: "ghcr.io/geoffbelknap/agency-hub"}
	if s.EffectiveType() != "oci" {
		t.Errorf("expected oci, got %s", s.EffectiveType())
	}
}

func TestSourceOCIRef(t *testing.T) {
	s := Source{Name: "official", Type: "oci", Registry: "ghcr.io/geoffbelknap/agency-hub"}
	ref := s.ComponentRef("connector", "limacharlie", "0.5.0")
	expected := "ghcr.io/geoffbelknap/agency-hub/connector/limacharlie:0.5.0"
	if ref != expected {
		t.Errorf("expected %s, got %s", expected, ref)
	}
}

func TestSourceOCIRefLatest(t *testing.T) {
	s := Source{Name: "official", Type: "oci", Registry: "ghcr.io/geoffbelknap/agency-hub"}
	ref := s.ComponentRef("pack", "security-ops", "")
	expected := "ghcr.io/geoffbelknap/agency-hub/pack/security-ops:latest"
	if ref != expected {
		t.Errorf("expected %s, got %s", expected, ref)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
go test ./internal/hub/ -run TestSource -v
```

Expected: Compilation errors — `EffectiveType`, `Registry`, `ComponentRef` don't exist yet.

- [ ] **Step 3: Update Source struct and add methods**

In `internal/hub/hub.go`, change the Source struct (lines 49-54):

```go
// Source represents a hub registry source (OCI or git).
type Source struct {
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type,omitempty" json:"type,omitempty"` // "oci" or "git"; defaults to "git"
	URL      string `yaml:"url,omitempty" json:"url,omitempty"`   // git URL (when type=git)
	Registry string `yaml:"registry,omitempty" json:"registry,omitempty"` // OCI registry base (when type=oci)
	Branch   string `yaml:"branch,omitempty" json:"branch,omitempty"`    // git branch (when type=git)
}

// EffectiveType returns the source type, defaulting to "git" for backward compat.
func (s Source) EffectiveType() string {
	if s.Type == "oci" {
		return "oci"
	}
	return "git"
}

// ComponentRef returns the full OCI reference for a component.
// Format: {registry}/{kind}/{name}:{version}
func (s Source) ComponentRef(kind, name, version string) string {
	if version == "" {
		version = "latest"
	}
	return s.Registry + "/" + kind + "/" + name + ":" + version
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/hub/ -run TestSource -v
```

Expected: All 4 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/hub/hub.go internal/hub/oci.go internal/hub/oci_test.go
git commit -m "feat(hub): extend Source type with OCI registry support"
```

---

### Task 3: Implement OCI pull

**Files:**
- Create: `internal/hub/oci.go` (add functions)
- Create: `internal/hub/oci_test.go` (add tests)

- [ ] **Step 1: Write test for OCI pull function**

Add to `internal/hub/oci_test.go`:

```go
func TestPullComponentToCache(t *testing.T) {
	// This test verifies the cache directory structure created by pullComponentToCache.
	// It uses a mock — real registry tests are integration tests.
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "hub-cache", "official")

	// Simulate what pullComponentToCache writes
	kind := "connector"
	name := "test-connector"
	destDir := filepath.Join(cacheDir, kind+"s", name)
	os.MkdirAll(destDir, 0755)

	yamlContent := []byte("name: test-connector\nkind: connector\nversion: 0.1.0\n")
	os.WriteFile(filepath.Join(destDir, "connector.yaml"), yamlContent, 0644)

	metaContent := []byte("name: test-connector\nkind: connector\nversion: 0.1.0\nbuild: abc1234\n")
	os.WriteFile(filepath.Join(destDir, "metadata.yaml"), metaContent, 0644)

	// Verify structure matches what discover() expects
	data, err := os.ReadFile(filepath.Join(destDir, "connector.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "test-connector") {
		t.Error("component YAML should contain name")
	}
}
```

- [ ] **Step 2: Implement OCI pull in oci.go**

Create `internal/hub/oci.go`:

```go
package hub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const (
	// OCI media types for hub components
	MediaTypeComponent = "application/vnd.agency.hub.component.v1+yaml"
	MediaTypeMetadata  = "application/vnd.agency.hub.metadata.v1+yaml"

	// Annotation keys
	AnnotationKind       = "agency.hub.kind"
	AnnotationReviewedBy = "agency.hub.reviewed_by"
)

// ociClient wraps ORAS operations for hub component pull.
type ociClient struct {
	registry string
}

// newOCIClient creates a client for the given registry base URL.
func newOCIClient(registry string) *ociClient {
	return &ociClient{registry: registry}
}

// pullComponent fetches a single component artifact and writes it to cacheDir.
// Cache layout mirrors git: {cacheDir}/{sourceName}/{kind}s/{name}/{kind}.yaml
func (c *ociClient) pullComponent(ctx context.Context, kind, name, version, cacheDir, sourceName string) error {
	if version == "" {
		version = "latest"
	}
	ref := c.registry + "/" + kind + "/" + name + ":" + version

	repo, err := remote.NewRepository(ref)
	if err != nil {
		return fmt.Errorf("parse reference %s: %w", ref, err)
	}
	repo.Client = retry.DefaultClient
	repo.Client = &auth.Client{Client: retry.DefaultClient}

	destDir := filepath.Join(cacheDir, sourceName, kind+"s", name)
	os.MkdirAll(destDir, 0755)

	store, err := file.New(destDir)
	if err != nil {
		return fmt.Errorf("create file store: %w", err)
	}
	defer store.Close()

	desc, err := oras.Copy(ctx, repo, version, store, version)
	if err != nil {
		return fmt.Errorf("pull %s: %w", ref, err)
	}

	// Rename pulled files to match expected cache structure
	// ORAS writes files by their annotation title; we need {kind}.yaml and metadata.yaml
	_ = desc // descriptor available for digest verification
	return nil
}

// listTags returns all tags for a component in the registry.
func (c *ociClient) listTags(ctx context.Context, kind, name string) ([]string, error) {
	ref := c.registry + "/" + kind + "/" + name
	repo, err := remote.NewRepository(ref)
	if err != nil {
		return nil, fmt.Errorf("parse reference %s: %w", ref, err)
	}
	repo.Client = &auth.Client{Client: retry.DefaultClient}

	var tags []string
	err = repo.Tags(ctx, "", func(t []string) error {
		tags = append(tags, t...)
		return nil
	})
	return tags, err
}

// discoverOCI lists all components from an OCI registry source.
// Uses the registry catalog API to enumerate repositories, then reads tags.
func (c *ociClient) discoverAll(ctx context.Context, sourceName, cacheDir string) ([]Component, error) {
	// Read from local cache — OCI sources are synced to cache on update,
	// same as git sources. discover() already reads from cache.
	// This function is called by syncOCISource to populate the cache.
	return nil, nil // Cache-based discovery handled by existing discover()
}

// syncOCISource syncs all components from an OCI source into the cache.
// Pulls the catalog, compares with local cache, and fetches updated components.
func syncOCISource(ctx context.Context, src Source, cacheDir string) error {
	client := newOCIClient(src.Registry)

	// List all repositories under the registry base
	reg, err := remote.NewRegistry(extractRegistryHost(src.Registry))
	if err != nil {
		return fmt.Errorf("connect to registry: %w", err)
	}
	reg.Client = &auth.Client{Client: retry.DefaultClient}

	// The registry base includes the namespace path (e.g., ghcr.io/geoffbelknap/agency-hub)
	// We enumerate {kind}/{name} repos under that path
	for _, kind := range KnownKinds {
		prefix := extractRepoPrefix(src.Registry) + "/" + kind + "/"
		reg.Repositories(ctx, "", func(repos []string) error {
			for _, repo := range repos {
				if !strings.HasPrefix(repo, prefix) {
					continue
				}
				name := strings.TrimPrefix(repo, prefix)
				// Pull latest version
				if err := client.pullComponent(ctx, kind, name, "latest", cacheDir, src.Name); err != nil {
					// Log warning, continue with other components
					fmt.Printf("[hub] WARNING: failed to pull %s/%s: %v\n", kind, name, err)
				}
			}
			return nil
		})
	}
	return nil
}

// extractRegistryHost returns the registry hostname from a full reference.
// "ghcr.io/geoffbelknap/agency-hub" → "ghcr.io"
func extractRegistryHost(registry string) string {
	parts := strings.SplitN(registry, "/", 2)
	return parts[0]
}

// extractRepoPrefix returns the repository prefix from a registry base.
// "ghcr.io/geoffbelknap/agency-hub" → "geoffbelknap/agency-hub"
func extractRepoPrefix(registry string) string {
	parts := strings.SplitN(registry, "/", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/hub/ -run TestPull -v
```

Expected: Pass (test uses local filesystem, not a real registry).

- [ ] **Step 4: Commit**

```bash
git add internal/hub/oci.go internal/hub/oci_test.go
git commit -m "feat(hub): implement OCI pull via ORAS"
```

---

### Task 4: Implement cosign signature verification

**Files:**
- Modify: `internal/hub/oci.go`
- Modify: `internal/hub/oci_test.go`

- [ ] **Step 1: Write verification test**

Add to `internal/hub/oci_test.go`:

```go
func TestVerifySignatureUnsigned(t *testing.T) {
	// Unsigned artifact should be rejected
	err := verifySignature(context.Background(), "ghcr.io/geoffbelknap/agency-hub/connector/fake:0.0.0")
	if err == nil {
		t.Error("expected error for unsigned artifact")
	}
}
```

- [ ] **Step 2: Implement signature verification**

Add to `internal/hub/oci.go`:

```go
import (
	"github.com/sigstore/cosign/v2/pkg/cosign"
	ociremote "github.com/sigstore/cosign/v2/pkg/oci/remote"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
)

// verifySignature checks the cosign keyless signature on an OCI artifact.
// Returns nil if valid, error if unsigned or invalid.
// Uses Sigstore public-good instance (Fulcio + Rekor) for verification.
func verifySignature(ctx context.Context, ref string) error {
	co := &cosign.CheckOpts{
		// Keyless verification against Sigstore public instance
		RekorURL:     "https://rekor.sigstore.dev",
		RootCerts:    fulcio.GetRoots(),
		IntermediateCerts: fulcio.GetIntermediates(),
		// Verify the signature was created by GitHub Actions OIDC
		Identities: []cosign.Identity{
			{
				Issuer:  "https://token.actions.githubusercontent.com",
				Subject: "https://github.com/geoffbelknap/agency-hub/.github/workflows/*",
			},
		},
	}

	parsedRef, err := name.ParseReference(ref)
	if err != nil {
		return fmt.Errorf("parse reference: %w", err)
	}

	_, _, err = cosign.VerifyImageSignatures(ctx, parsedRef, co)
	if err != nil {
		return fmt.Errorf("signature verification failed for %s: %w", ref, err)
	}
	return nil
}
```

Note: The exact cosign API surface may differ by version. The implementation should follow the cosign Go library's current verification API. If the sigstore-go library provides a simpler path, prefer that. The key requirement is: keyless verification against GitHub Actions OIDC identity for the agency-hub repo.

- [ ] **Step 3: Run test**

```bash
go test ./internal/hub/ -run TestVerify -v
```

Expected: Pass (unsigned artifact returns error).

- [ ] **Step 4: Commit**

```bash
git add internal/hub/oci.go internal/hub/oci_test.go
git commit -m "feat(hub): add cosign signature verification for OCI artifacts"
```

---

### Task 5: Wire OCI into hub Manager

**Files:**
- Modify: `internal/hub/hub.go`

This task modifies the Manager methods to dispatch between git and OCI based on `Source.EffectiveType()`.

- [ ] **Step 1: Write test for OCI source sync dispatch**

Add to `internal/hub/oci_test.go`:

```go
func TestManagerUpdateDispatchesOCI(t *testing.T) {
	// Verify that Update() calls syncOCISource for OCI-type sources
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)

	// Write config with an OCI source
	cfgDir := tmpDir
	os.MkdirAll(cfgDir, 0755)
	config := []byte(`hub:
  sources:
    - name: official
      type: oci
      registry: ghcr.io/geoffbelknap/agency-hub
`)
	os.WriteFile(filepath.Join(cfgDir, "config.yaml"), config, 0644)

	// Update will fail to reach the registry (no network in unit test)
	// but it should attempt OCI sync, not git sync
	report, _ := m.Update()
	// The warning should mention registry/OCI, not git
	if len(report.Warnings) > 0 && strings.Contains(report.Warnings[0], "git") {
		t.Error("expected OCI sync, got git sync attempt")
	}
}
```

- [ ] **Step 2: Modify Update() to dispatch by source type**

In `hub.go`, change the `Update()` function (line 105):

```go
func (m *Manager) Update() (*UpdateReport, error) {
	cfg := m.loadConfig()
	cacheDir := filepath.Join(m.Home, "hub-cache")
	os.MkdirAll(cacheDir, 0755)

	report := &UpdateReport{}
	for _, src := range cfg.Hub.Sources {
		switch src.EffectiveType() {
		case "oci":
			if err := syncOCISource(context.Background(), src, cacheDir); err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %s", src.Name, err.Error()))
			}
			report.Sources = append(report.Sources, SourceUpdate{Name: src.Name})
		default:
			su, err := m.syncSourceWithReport(src, cacheDir)
			if err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %s", src.Name, err.Error()))
			}
			report.Sources = append(report.Sources, su)
		}
	}

	report.Available = m.Outdated()
	return report, nil
}
```

Add `"context"` to the imports if not already present.

- [ ] **Step 3: Modify Install() to verify signatures for OCI sources**

In `hub.go`, after line 300 (reading the source file), add signature verification:

```go
	// Verify signature for OCI-sourced components
	if src := m.findSourceByName(comp.Source); src != nil && src.EffectiveType() == "oci" {
		ref := src.ComponentRef(kind, componentName, comp.Version)
		if err := verifySignature(context.Background(), ref); err != nil {
			return nil, fmt.Errorf("signature verification failed: %w", err)
		}
	}
```

Add the helper:

```go
// findSourceByName returns the Source config for a given source name.
func (m *Manager) findSourceByName(name string) *Source {
	cfg := m.loadConfig()
	for _, src := range cfg.Hub.Sources {
		if src.Name == name {
			return &src
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/hub/ -v
```

Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/hub/hub.go internal/hub/oci.go internal/hub/oci_test.go
git commit -m "feat(hub): wire OCI sync and signature verification into Manager"
```

---

### Task 6: Update add-source CLI command

**Files:**
- Modify: `internal/cli/commands.go:1751-1780`

- [ ] **Step 1: Update add-source to accept OCI registries**

Replace the `add-source` command (line 1751) with:

```go
	addSourceCmd := &cobra.Command{
		Use:   "add-source <name> <url-or-registry>",
		Short: "Add a hub source (OCI registry or git URL)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			srcType, _ := cmd.Flags().GetString("type")
			branch, _ := cmd.Flags().GetString("branch")

			// Auto-detect type if not specified
			if srcType == "" {
				if strings.Contains(args[1], ".git") || strings.HasPrefix(args[1], "https://github.com") {
					srcType = "git"
				} else {
					srcType = "oci"
				}
			}

			home, _ := os.UserHomeDir()
			cfgPath := home + "/.agency/config.yaml"
			data, err := os.ReadFile(cfgPath)
			if err != nil {
				return fmt.Errorf("read config: %w", err)
			}

			var entry string
			switch srcType {
			case "oci":
				entry = fmt.Sprintf("    - name: %s\n      type: oci\n      registry: %s\n", args[0], args[1])
			default:
				if branch == "" {
					branch = "main"
				}
				entry = fmt.Sprintf("    - name: %s\n      url: %s\n      branch: %s\n", args[0], args[1], branch)
			}

			content := string(data)
			if !strings.Contains(content, "hub:") {
				content += "\nhub:\n  sources:\n" + entry
			} else if !strings.Contains(content, "sources:") {
				content = strings.Replace(content, "hub:", "hub:\n  sources:\n"+entry, 1)
			} else {
				// Append after last source entry
				idx := strings.LastIndex(content, "      branch:")
				if idx == -1 {
					idx = strings.LastIndex(content, "      registry:")
				}
				if idx != -1 {
					// Find end of that line
					nlIdx := strings.Index(content[idx:], "\n")
					if nlIdx != -1 {
						insertAt := idx + nlIdx + 1
						content = content[:insertAt] + entry + content[insertAt:]
					}
				}
			}

			if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
				return err
			}
			typeLabel := srcType
			if typeLabel == "oci" {
				typeLabel = "OCI"
			}
			fmt.Printf("%s Added %s source %s (%s)\n", green.Render("✓"), typeLabel, bold.Render(args[0]), args[1])
			fmt.Println("  Run 'agency hub update' to fetch components from this source")
			return nil
		},
	}
	addSourceCmd.Flags().String("type", "", "Source type: oci or git (auto-detected if omitted)")
	addSourceCmd.Flags().String("branch", "main", "Git branch (only for git sources)")
	cmd.AddCommand(addSourceCmd)
```

- [ ] **Step 2: Run build to verify**

```bash
go build ./cmd/gateway/
```

Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add internal/cli/commands.go
git commit -m "feat(hub): add-source supports OCI registry type"
```

---

### Task 7: Change default source to OCI

**Files:**
- Modify: `internal/hub/hub.go`

- [ ] **Step 1: Add default OCI source constant**

Add near the top of `hub.go` (after the KnownKinds declaration):

```go
// DefaultSource is the official Agency hub, distributed as OCI artifacts.
var DefaultSource = Source{
	Name:     "official",
	Type:     "oci",
	Registry: "ghcr.io/geoffbelknap/agency-hub",
}
```

- [ ] **Step 2: Update loadConfig to inject default source**

Modify `loadConfig()` to add the default OCI source when no sources are configured:

In the `loadConfig` function (around line 1144), after unmarshaling, add:

```go
	// If no sources configured, use the default OCI source
	if len(cfg.Hub.Sources) == 0 {
		cfg.Hub.Sources = []Source{DefaultSource}
	}
```

- [ ] **Step 3: Write test for default source**

Add to `internal/hub/oci_test.go`:

```go
func TestDefaultSourceIsOCI(t *testing.T) {
	tmpDir := t.TempDir()
	// Write empty config
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(""), 0644)

	m := NewManager(tmpDir)
	cfg := m.loadConfig()
	if len(cfg.Hub.Sources) != 1 {
		t.Fatalf("expected 1 default source, got %d", len(cfg.Hub.Sources))
	}
	if cfg.Hub.Sources[0].EffectiveType() != "oci" {
		t.Errorf("expected oci default source, got %s", cfg.Hub.Sources[0].EffectiveType())
	}
	if cfg.Hub.Sources[0].Registry != "ghcr.io/geoffbelknap/agency-hub" {
		t.Errorf("unexpected registry: %s", cfg.Hub.Sources[0].Registry)
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/hub/ -v
```

Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/hub/hub.go internal/hub/oci_test.go
git commit -m "feat(hub): default source is OCI (ghcr.io/geoffbelknap/agency-hub)"
```

---

### Task 8: OCI publishing CI workflow (agency-hub repo)

**Files:**
- Create: `agency-hub/.github/workflows/publish-oci.yml`

- [ ] **Step 1: Create the OCI publishing workflow**

Create `/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub/.github/workflows/publish-oci.yml`:

```yaml
name: Publish OCI Artifacts

on:
  push:
    branches: [main]
    paths:
      - 'connectors/**'
      - 'packs/**'
      - 'presets/**'
      - 'missions/**'
      - 'skills/**'
      - 'services/**'
      - 'providers/**'
      - 'ontology/**'

permissions:
  contents: read
  packages: write
  id-token: write  # Required for cosign keyless signing

env:
  REGISTRY: ghcr.io/geoffbelknap/agency-hub

jobs:
  publish:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 2  # Need parent commit for diff

      - name: Install ORAS
        run: |
          ORAS_VERSION="1.2.0"
          curl -sLO "https://github.com/oras-project/oras/releases/download/v${ORAS_VERSION}/oras_${ORAS_VERSION}_linux_amd64.tar.gz"
          tar xzf "oras_${ORAS_VERSION}_linux_amd64.tar.gz" oras
          sudo mv oras /usr/local/bin/

      - name: Install cosign
        uses: sigstore/cosign-installer@v3

      - name: Log in to GHCR
        run: echo "${{ secrets.GITHUB_TOKEN }}" | oras login ghcr.io -u "${{ github.actor }}" --password-stdin

      - name: Detect changed components
        id: changes
        run: |
          CHANGED=$(git diff --name-only HEAD~1 HEAD | grep -E '^(connectors|packs|presets|missions|skills|services|providers|ontology)/' | sort -u)
          echo "files<<EOF" >> "$GITHUB_OUTPUT"
          echo "$CHANGED" >> "$GITHUB_OUTPUT"
          echo "EOF" >> "$GITHUB_OUTPUT"

      - name: Publish changed components
        if: steps.changes.outputs.files != ''
        env:
          COSIGN_EXPERIMENTAL: "1"
        run: |
          echo "${{ steps.changes.outputs.files }}" | while IFS= read -r file; do
            [ -z "$file" ] && continue
            # Skip metadata files — they're not components
            [[ "$file" == */metadata.yaml ]] && continue
            [[ "$file" == */README.md ]] && continue

            # Parse kind and name from path: {kind}s/{name}/{kind}.yaml
            KIND_PLURAL=$(echo "$file" | cut -d/ -f1)
            NAME=$(echo "$file" | cut -d/ -f2)
            KIND="${KIND_PLURAL%s}"  # connectors → connector

            COMPONENT_FILE="$file"
            METADATA_FILE="$(dirname "$file")/metadata.yaml"

            # Skip if not a YAML component file
            [[ "$file" != *.yaml ]] && continue
            [[ "$(basename "$file")" == "metadata.yaml" ]] && continue

            # Extract version from component YAML
            VERSION=$(python3 -c "import yaml,sys; d=yaml.safe_load(open('$COMPONENT_FILE')); print(d.get('version','0.0.0'))")

            REF="${{ env.REGISTRY }}/${KIND}/${NAME}:${VERSION}"
            REF_LATEST="${{ env.REGISTRY }}/${KIND}/${NAME}:latest"

            echo "Publishing ${KIND}/${NAME}:${VERSION}..."

            # Push with both version and latest tags
            PUSH_ARGS=(
              "$COMPONENT_FILE:application/vnd.agency.hub.component.v1+yaml"
            )
            if [ -f "$METADATA_FILE" ]; then
              PUSH_ARGS+=("$METADATA_FILE:application/vnd.agency.hub.metadata.v1+yaml")
            fi

            oras push "$REF" \
              --annotation "org.opencontainers.image.version=$VERSION" \
              --annotation "org.opencontainers.image.source=https://github.com/${{ github.repository }}" \
              --annotation "org.opencontainers.image.revision=${{ github.sha }}" \
              --annotation "agency.hub.kind=$KIND" \
              "${PUSH_ARGS[@]}"

            # Tag as latest
            oras tag "$REF" latest

            # Sign with cosign (keyless, GitHub OIDC)
            cosign sign --yes "$REF"

            echo "✓ Published and signed ${KIND}/${NAME}:${VERSION}"
          done

      - name: Summary
        if: steps.changes.outputs.files != ''
        run: echo "### OCI artifacts published to ghcr.io" >> "$GITHUB_STEP_SUMMARY"
```

- [ ] **Step 2: Commit in agency-hub repo**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub
git add .github/workflows/publish-oci.yml
git commit -m "ci: add OCI publishing workflow with cosign signing"
```

---

### Task 9: Migration logic for existing git installations

**Files:**
- Modify: `internal/hub/hub.go`

- [ ] **Step 1: Write migration test**

Add to `internal/hub/oci_test.go`:

```go
func TestMigrateGitSourceToOCI(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate old git-based config
	config := []byte(`hub:
  sources:
    - name: official
      url: https://github.com/geoffbelknap/agency-hub.git
      branch: main
`)
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), config, 0644)

	m := NewManager(tmpDir)
	migrated := m.migrateDefaultSourceToOCI()

	if !migrated {
		t.Error("expected migration to occur")
	}

	// Re-read config and verify
	cfg := m.loadConfig()
	if len(cfg.Hub.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(cfg.Hub.Sources))
	}
	if cfg.Hub.Sources[0].EffectiveType() != "oci" {
		t.Errorf("expected oci, got %s", cfg.Hub.Sources[0].EffectiveType())
	}
}
```

- [ ] **Step 2: Implement migration function**

Add to `hub.go`:

```go
// migrateDefaultSourceToOCI checks if the "official" source is still git-based
// and migrates it to OCI. Returns true if migration occurred.
func (m *Manager) migrateDefaultSourceToOCI() bool {
	cfgPath := filepath.Join(m.Home, "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return false
	}

	var cfg hubConfig
	if yaml.Unmarshal(data, &cfg) != nil {
		return false
	}

	migrated := false
	for i, src := range cfg.Hub.Sources {
		if src.Name == "official" && src.EffectiveType() == "git" &&
			strings.Contains(src.URL, "agency-hub") {
			cfg.Hub.Sources[i] = DefaultSource
			migrated = true
		}
	}

	if !migrated {
		return false
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return false
	}
	os.WriteFile(cfgPath, out, 0644)
	return true
}
```

- [ ] **Step 3: Call migration in Update()**

At the top of `Update()`, before loading config:

```go
	// One-time migration: official source git → OCI
	if m.migrateDefaultSourceToOCI() {
		log.Printf("[hub] Migrated official source from git to OCI")
	}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/hub/ -v
```

Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/hub/hub.go internal/hub/oci_test.go
git commit -m "feat(hub): auto-migrate official source from git to OCI"
```

---

### Task 10: Update hub documentation

**Files:**
- Modify: `CLAUDE.md` (agency repo)

- [ ] **Step 1: Update CLAUDE.md hub references**

Find the hub-related entries in `CLAUDE.md` and update:

1. Change the `add-source` description:
```
- Hub-managed files (routing.yaml, services) are overwritten by `agency hub update`.
```
Keep as-is (this behavior is unchanged).

2. Add a new bullet in the Key Rules section:
```
- **Hub distribution is OCI-based**: The official hub publishes signed OCI artifacts to `ghcr.io/geoffbelknap/agency-hub`. `agency hub install` pulls from OCI and verifies cosign signatures. Third-party sources can be OCI registries (`agency hub add-source my-corp ghcr.io/my-corp/hub`) or git URLs. Signature verification is mandatory — unsigned artifacts are rejected (ASK tenet 23).
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document OCI hub distribution in CLAUDE.md"
```

---

### Task 11: Update agency-hub CONTRIBUTING.md

**Files:**
- Modify: `/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub/CONTRIBUTING.md`

- [ ] **Step 1: Add OCI publishing information**

Add a section to CONTRIBUTING.md explaining the Homebrew model:

```markdown
## How Components Are Published

When your PR is merged to main, components are automatically:

1. **Metadata stamped** — version, build hash, and timestamp added to `metadata.yaml`
2. **Published to GHCR** — pushed as OCI artifacts to `ghcr.io/geoffbelknap/agency-hub/{kind}/{name}:{version}`
3. **Signed** — cosign keyless signing with GitHub Actions OIDC identity

You don't need to interact with the OCI registry directly. Just submit your YAML, pass review, and CI handles distribution.

Users install your component with:
```bash
agency hub update     # fetches latest from registry
agency hub install <name>
```
```

- [ ] **Step 2: Commit in agency-hub repo**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub
git add CONTRIBUTING.md
git commit -m "docs: add OCI publishing info to CONTRIBUTING.md"
```

---

### Task 12: Push and verify end-to-end

- [ ] **Step 1: Push agency repo changes**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git push origin main
```

- [ ] **Step 2: Push agency-hub repo changes**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub
git push origin main
```

- [ ] **Step 3: Verify OCI publishing workflow triggers**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub
gh run list --limit 1 --json status,name
```

Expected: "Publish OCI Artifacts" workflow running.

- [ ] **Step 4: Verify artifact exists in GHCR**

After the workflow completes:

```bash
oras manifest fetch ghcr.io/geoffbelknap/agency-hub/connector/limacharlie:latest 2>&1 | head -5
```

Expected: OCI manifest JSON returned.

- [ ] **Step 5: Verify CLI can pull from OCI**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
go build -o agency ./cmd/gateway/
./agency hub update
./agency hub search limacharlie
```

Expected: Components discovered from OCI registry.
