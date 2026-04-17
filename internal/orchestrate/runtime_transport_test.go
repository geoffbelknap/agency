package orchestrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadScopedAPIKeyReturnsFirstKeyValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api_keys.yaml")
	if err := os.WriteFile(path, []byte("- key: \"agency-scoped--abc123\"\n  name: \"agency-workspace\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readScopedAPIKey(path); got != "agency-scoped--abc123" {
		t.Fatalf("readScopedAPIKey() = %q, want %q", got, "agency-scoped--abc123")
	}
}

func TestReadScopedAPIKeyReturnsEmptyForMissingOrInvalidFile(t *testing.T) {
	if got := readScopedAPIKey(""); got != "" {
		t.Fatalf("readScopedAPIKey(empty) = %q, want empty", got)
	}
	if got := readScopedAPIKey("/does/not/exist"); got != "" {
		t.Fatalf("readScopedAPIKey(missing) = %q, want empty", got)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "api_keys.yaml")
	if err := os.WriteFile(path, []byte("not: [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readScopedAPIKey(path); got != "" {
		t.Fatalf("readScopedAPIKey(invalid) = %q, want empty", got)
	}
}
