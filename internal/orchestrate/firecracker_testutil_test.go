package orchestrate

import (
	"os"
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
