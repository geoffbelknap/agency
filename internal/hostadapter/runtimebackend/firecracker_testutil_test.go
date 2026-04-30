package runtimebackend

import (
	"os"
	"path/filepath"
	"testing"
)

func shortSocketTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "agency-fc-*")
	if err != nil {
		t.Fatalf("create short socket temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove short socket temp dir %s: %v", dir, err)
		}
	})
	return dir
}

func shortSocketPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(shortSocketTempDir(t), name)
}
