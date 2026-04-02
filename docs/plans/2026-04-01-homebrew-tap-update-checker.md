# Homebrew Tap & CLI Update Checker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the Agency CLI via Homebrew tap (`brew install geoffbelknap/agency/agency`) and add a non-blocking update checker that notifies users when a new version is available.

**Architecture:** goreleaser config and release workflow already exist — they need minor fixes (missing `buildID` ldflag, missing `prepare-embed` target). A new `internal/update` package checks GitHub releases in a background goroutine on every CLI invocation, caches the result for 24h, and prints a one-liner to stderr if a newer version exists. The tap repo (`geoffbelknap/homebrew-agency`) is auto-managed by goreleaser on tag push.

**Tech Stack:** Go, goreleaser v2, GitHub Actions, GitHub Releases API, Homebrew

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `agency-gateway/.goreleaser.yaml` | Add `buildID` ldflag, fix `prepare-embed` hook |
| Modify | `agency-gateway/Makefile` | Add `prepare-embed` target (no-op for now) |
| Create | `agency-gateway/internal/update/update.go` | Background version check: fetch latest release, compare semver, cache result |
| Create | `agency-gateway/internal/update/update_test.go` | Tests for version comparison and cache logic |
| Modify | `agency-gateway/cmd/gateway/main.go` | Wire update checker into CLI startup, print hint after command |
| Modify | `.github/workflows/release.yaml` | Add `HOMEBREW_TAP_TOKEN` secret for cross-repo push |

The tap repo (`geoffbelknap/homebrew-agency`) is created manually on GitHub — goreleaser populates it automatically.

---

### Task 1: Fix goreleaser config

The existing `.goreleaser.yaml` is missing the `buildID` ldflag (so release binaries get `buildID=unknown`) and references a `prepare-embed` Makefile target that doesn't exist.

**Files:**
- Modify: `agency-gateway/.goreleaser.yaml`
- Modify: `agency-gateway/Makefile`

- [ ] **Step 1: Add `buildID` ldflag and fix before hook in goreleaser config**

In `agency-gateway/.goreleaser.yaml`, update the `ldflags` section to include `buildID` (goreleaser's `{{.ShortCommit}}` maps to the same short hash the Makefile uses), and remove the `prepare-embed` before hook since there's nothing to embed:

```yaml
version: 2

project_name: agency

builds:
  - id: agency
    main: ./cmd/gateway/
    binary: agency
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.Commit}}
      - -X main.date={{.Date}}
      - -X main.buildID={{.ShortCommit}}

archives:
  - id: default
    format: tar.gz
    format_overrides:
      - goos: windows
        format: zip
    name_template: "agency_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: "checksums.txt"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^ci:"

brews:
  - repository:
      owner: geoffbelknap
      name: homebrew-agency
      token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"
    name: agency
    homepage: "https://github.com/geoffbelknap/agency"
    description: "Agency platform - manage AI agent teams"
    install: |
      bin.install "agency"
    test: |
      system "#{bin}/agency", "--version"

release:
  github:
    owner: geoffbelknap
    name: agency
  prerelease: auto
  name_template: "Agency v{{.Version}}"
```

Key changes from existing file:
- Removed `before.hooks` (`prepare-embed` doesn't exist)
- Added `-X main.buildID={{.ShortCommit}}` to ldflags
- Added `token` to brews repository (needed for cross-repo push)

- [ ] **Step 2: Verify goreleaser config locally**

```bash
cd agency-gateway && goreleaser check
```

Expected: `config is valid`

If goreleaser is not installed locally, skip — CI will validate.

- [ ] **Step 3: Commit**

```bash
git add agency-gateway/.goreleaser.yaml
git commit -m "fix: add buildID ldflag and clean up goreleaser config"
```

---

### Task 2: Update release workflow for tap token

The release workflow needs the `HOMEBREW_TAP_TOKEN` secret so goreleaser can push the formula to `geoffbelknap/homebrew-agency`.

**Files:**
- Modify: `.github/workflows/release.yaml`

- [ ] **Step 1: Add HOMEBREW_TAP_TOKEN to the workflow**

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: "1.26"
          cache-dependency-path: agency-gateway/go.sum

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
          workdir: agency-gateway
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}
```

The `HOMEBREW_TAP_TOKEN` must be a personal access token (classic) with `repo` scope, stored as a repository secret in `geoffbelknap/agency`. `GITHUB_TOKEN` can't push to a different repo.

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/release.yaml
git commit -m "ci: pass HOMEBREW_TAP_TOKEN to goreleaser for tap updates"
```

---

### Task 3: Create the update checker package

A small, self-contained package that checks GitHub releases for newer versions. Non-blocking, cached, fail-silent.

**Files:**
- Create: `agency-gateway/internal/update/update.go`
- Create: `agency-gateway/internal/update/update_test.go`

- [ ] **Step 1: Write tests for version comparison and cache logic**

Create `agency-gateway/internal/update/update_test.go`:

```go
package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"0.1.0", "0.2.0", true},
		{"0.1.0", "0.1.1", true},
		{"0.2.0", "0.1.0", false},
		{"0.1.0", "0.1.0", false},
		{"1.0.0", "0.9.9", false},
		{"0.9.9", "1.0.0", true},
		{"dev", "0.1.0", false},   // dev builds never nag
		{"0.1.0", "invalid", false},
	}
	for _, tt := range tests {
		got := isNewer(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestCheckResult_FromGitHubAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/geoffbelknap/agency/releases/latest" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"tag_name": "v0.3.0",
			"html_url": "https://github.com/geoffbelknap/agency/releases/tag/v0.3.0",
		})
	}))
	defer srv.Close()

	r, err := fetchLatest(srv.URL + "/repos/geoffbelknap/agency/releases/latest")
	if err != nil {
		t.Fatal(err)
	}
	if r.TagName != "v0.3.0" {
		t.Errorf("got tag %q, want v0.3.0", r.TagName)
	}
}

func TestCache_WriteThenRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update-check.json")

	entry := cacheEntry{
		CheckedAt: time.Now(),
		Latest:    "0.3.0",
	}
	writeCache(path, entry)

	got, ok := readCache(path, 24*time.Hour)
	if !ok {
		t.Fatal("cache should be valid")
	}
	if got.Latest != "0.3.0" {
		t.Errorf("got %q, want 0.3.0", got.Latest)
	}
}

func TestCache_Expired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update-check.json")

	entry := cacheEntry{
		CheckedAt: time.Now().Add(-25 * time.Hour),
		Latest:    "0.3.0",
	}
	writeCache(path, entry)

	_, ok := readCache(path, 24*time.Hour)
	if ok {
		t.Error("cache should be expired")
	}
}

func TestCache_Missing(t *testing.T) {
	_, ok := readCache("/nonexistent/path", 24*time.Hour)
	if ok {
		t.Error("missing cache should return not-ok")
	}
}

func writeCache(path string, entry cacheEntry) {
	data, _ := json.Marshal(entry)
	os.WriteFile(path, data, 0644)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd agency-gateway && go test ./internal/update/ -v
```

Expected: compilation errors — package doesn't exist yet.

- [ ] **Step 3: Implement the update checker**

Create `agency-gateway/internal/update/update.go`:

```go
// Package update checks GitHub releases for newer versions of the agency CLI.
// Checks run in a background goroutine with a short timeout and are cached
// for 24 hours. Failures are silent — never block or degrade the CLI.
package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	releaseURL = "https://api.github.com/repos/geoffbelknap/agency/releases/latest"
	cacheTTL   = 24 * time.Hour
	timeout    = 3 * time.Second
)

// Result is the outcome of a background update check.
type Result struct {
	Latest  string // e.g. "0.3.0"
	URL     string // release page URL
	Current string // the running version
}

// Newer returns true if the check found a newer version.
func (r *Result) Newer() bool {
	return r != nil && isNewer(r.Current, r.Latest)
}

type releaseResponse struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

type cacheEntry struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
	URL       string    `json:"url"`
}

// Check starts a background version check and returns a function that
// retrieves the result. The returned function blocks until the check
// completes or the timeout expires. Call it after the CLI command finishes.
//
//	wait := update.Check("0.1.0", "~/.agency")
//	// ... run CLI command ...
//	if r := wait(); r.Newer() { print hint }
func Check(currentVersion, agencyHome string) func() *Result {
	var (
		once   sync.Once
		result *Result
		done   = make(chan struct{})
	)

	go func() {
		defer close(done)
		r := check(currentVersion, agencyHome)
		once.Do(func() { result = r })
	}()

	return func() *Result {
		select {
		case <-done:
		case <-time.After(timeout):
			// Don't block the CLI; return nil if check is still running
		}
		once.Do(func() {}) // ensure result is read safely
		return result
	}
}

func check(currentVersion, agencyHome string) *Result {
	// "dev" builds never nag
	if currentVersion == "dev" {
		return nil
	}

	cachePath := filepath.Join(agencyHome, "update-check.json")

	// Try cache first
	if cached, ok := readCache(cachePath, cacheTTL); ok {
		return &Result{
			Latest:  cached.Latest,
			URL:     cached.URL,
			Current: currentVersion,
		}
	}

	// Fetch from GitHub
	rel, err := fetchLatest(releaseURL)
	if err != nil {
		return nil
	}

	latest := strings.TrimPrefix(rel.TagName, "v")

	// Update cache (best-effort)
	entry := cacheEntry{
		CheckedAt: time.Now(),
		Latest:    latest,
		URL:       rel.HTMLURL,
	}
	data, _ := json.Marshal(entry)
	os.WriteFile(cachePath, data, 0644)

	return &Result{
		Latest:  latest,
		URL:     rel.HTMLURL,
		Current: currentVersion,
	}
}

func fetchLatest(url string) (*releaseResponse, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api: %s", resp.Status)
	}

	var rel releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func readCache(path string, ttl time.Duration) (cacheEntry, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cacheEntry{}, false
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return cacheEntry{}, false
	}
	if time.Since(entry.CheckedAt) > ttl {
		return cacheEntry{}, false
	}
	return entry, true
}

// isNewer returns true if latest is a higher semver than current.
func isNewer(current, latest string) bool {
	cur := parseSemver(current)
	lat := parseSemver(latest)
	if cur == nil || lat == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if lat[i] > cur[i] {
			return true
		}
		if lat[i] < cur[i] {
			return false
		}
	}
	return false
}

func parseSemver(s string) []int {
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
		// Strip pre-release suffix (e.g. "1-rc1" → "1")
		if idx := strings.IndexByte(p, '-'); idx >= 0 {
			p = p[:idx]
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	return nums
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd agency-gateway && go test ./internal/update/ -v
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add agency-gateway/internal/update/
git commit -m "feat: add background update checker for CLI version notifications"
```

---

### Task 4: Wire update checker into CLI startup

The update check fires in the background when any CLI command runs. After the command completes, if a newer version was found, print a one-liner to stderr.

**Files:**
- Modify: `agency-gateway/cmd/gateway/main.go`

- [ ] **Step 1: Add update check to main()**

In `agency-gateway/cmd/gateway/main.go`, add the import and wire the check around `root.Execute()`:

Add to imports:
```go
"github.com/geoffbelknap/agency/agency-gateway/internal/update"
```

Replace the existing `main()` execute block (lines 216-218):

```go
	// FROM:
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}

	// TO:
	// Start background update check (non-blocking, cached 24h, fail-silent)
	agencyHome := filepath.Join(os.Getenv("HOME"), ".agency")
	waitForUpdate := update.Check(version, agencyHome)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}

	// Print update hint if a newer version was found (stderr so it doesn't
	// pollute piped output)
	if r := waitForUpdate(); r.Newer() {
		fmt.Fprintf(os.Stderr, "\nA new version of agency is available: %s → %s\n", r.Current, r.Latest)
		fmt.Fprintf(os.Stderr, "Update with: brew upgrade agency\n")
	}
```

- [ ] **Step 2: Build and verify**

```bash
cd agency-gateway && go build -ldflags "-X main.version=0.0.1" -o agency ./cmd/gateway/
./agency --version
```

Expected: `agency version 0.0.1 (unknown, unknown)` — and if there's a release on GitHub, an update hint after any command.

- [ ] **Step 3: Verify dev builds don't nag**

```bash
cd agency-gateway && go build -o agency ./cmd/gateway/
./agency --version
```

Expected: version shows `dev`, no update hint (dev builds are excluded).

- [ ] **Step 4: Commit**

```bash
git add agency-gateway/cmd/gateway/main.go
git commit -m "feat: show update notification on CLI startup when newer version available"
```

---

### Task 5: Create the Homebrew tap repo

This is a manual GitHub step — goreleaser will auto-populate the formula on the first release.

**Files:**
- Create: `geoffbelknap/homebrew-agency` repo on GitHub (public)

- [ ] **Step 1: Create the repo**

Go to https://github.com/new and create `homebrew-agency` (public, no template, no README — goreleaser will populate it).

- [ ] **Step 2: Create a GitHub App for tap pushes**

1. Go to https://github.com/settings/apps/new
2. Name: `agency-homebrew-tap` (or similar)
3. Permissions → Repository permissions → Contents: **Read and write**
4. No other permissions needed. Uncheck everything else.
5. Where can this app be installed? **Only on this account**
6. Create the app. Note the **App ID** from the app's settings page.
7. Generate a **private key** (downloads a `.pem` file).
8. Install the app on `geoffbelknap/homebrew-agency` only (not all repos).
9. Go to `geoffbelknap/agency` → Settings → Secrets and variables → Actions
10. Add secret: `HOMEBREW_APP_ID` = the App ID from step 6
11. Add secret: `HOMEBREW_APP_PRIVATE_KEY` = contents of the `.pem` file from step 7

- [ ] **Step 3: Test the full flow**

Tag a release to trigger the pipeline:

```bash
git tag v0.1.0
git push origin v0.1.0
```

Watch the Actions tab. On success:
- GitHub Release page shows `agency_0.1.0_darwin_arm64.tar.gz`, `agency_0.1.0_darwin_amd64.tar.gz`, `agency_0.1.0_linux_arm64.tar.gz`, `agency_0.1.0_linux_amd64.tar.gz`, `checksums.txt`
- `geoffbelknap/homebrew-agency` has a `Formula/agency.rb` file

- [ ] **Step 4: Verify brew install works**

```bash
brew tap geoffbelknap/agency
brew install agency
agency --version
```

Expected: `agency version 0.1.0 (short-commit, build-date)`
