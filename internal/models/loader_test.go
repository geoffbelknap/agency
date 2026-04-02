// agency-gateway/internal/models/loader_test.go
package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndValidate_UnknownField(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "org.yaml")
	os.WriteFile(f, []byte("name: test\noperator: op\ncreated: now\nbogus: field\n"), 0644)

	err := LoadAndValidate(f)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), "unknown") && !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected unknown field error, got: %s", err.Error())
	}
}

func TestLoadAndValidate_ValidOrg(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "org.yaml")
	os.WriteFile(f, []byte("name: test\noperator: op\ncreated: now\n"), 0644)

	err := LoadAndValidate(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
