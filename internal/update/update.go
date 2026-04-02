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
	releaseURL  = "https://api.github.com/repos/geoffbelknap/agency/releases/latest"
	cacheTTL    = 12 * time.Hour
	httpTimeout = 3 * time.Second
	waitTimeout = 4 * time.Second // longer than httpTimeout to avoid racing it
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
		case <-time.After(waitTimeout):
		}
		once.Do(func() {})
		return result
	}
}

func check(currentVersion, agencyHome string) *Result {
	if currentVersion == "dev" {
		return nil
	}

	cachePath := filepath.Join(agencyHome, "update-check.json")

	if cached, ok := readCache(cachePath, cacheTTL); ok {
		return &Result{
			Latest:  cached.Latest,
			URL:     cached.URL,
			Current: currentVersion,
		}
	}

	rel, err := fetchLatest(releaseURL)
	if err != nil {
		return nil
	}

	latest := strings.TrimPrefix(rel.TagName, "v")

	entry := cacheEntry{
		CheckedAt: time.Now(),
		Latest:    latest,
		URL:       rel.HTMLURL,
	}
	data, _ := json.Marshal(entry)
	os.MkdirAll(filepath.Dir(cachePath), 0755)
	os.WriteFile(cachePath, data, 0644)

	return &Result{
		Latest:  latest,
		URL:     rel.HTMLURL,
		Current: currentVersion,
	}
}

func fetchLatest(url string) (*releaseResponse, error) {
	client := &http.Client{Timeout: httpTimeout}
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
