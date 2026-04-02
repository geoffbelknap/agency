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
		{"dev", "0.1.0", false},
		{"0.1.0", "invalid", false},
		{"0.1.0", "0.2.0-rc1", true},    // pre-release suffix stripped
		{"0.2.0-beta.1", "0.2.0", false}, // same base version after strip
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
