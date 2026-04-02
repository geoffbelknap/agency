package credstore

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func newTestBackend(t *testing.T) *FileBackend {
	t.Helper()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.enc")
	keyPath := filepath.Join(dir, ".key")
	fb, err := NewFileBackend(storePath, keyPath)
	if err != nil {
		t.Fatalf("NewFileBackend: %v", err)
	}
	return fb
}

func TestPutGetRoundTrip(t *testing.T) {
	fb := newTestBackend(t)

	meta := map[string]string{"kind": "service", "scope": "platform"}
	if err := fb.Put("MY_KEY", "secret-value-123", meta); err != nil {
		t.Fatalf("Put: %v", err)
	}

	val, gotMeta, err := fb.Get("MY_KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "secret-value-123" {
		t.Errorf("value = %q, want %q", val, "secret-value-123")
	}
	if gotMeta["kind"] != "service" || gotMeta["scope"] != "platform" {
		t.Errorf("metadata = %v, want kind=service scope=platform", gotMeta)
	}
}

func TestPutIdempotent(t *testing.T) {
	fb := newTestBackend(t)

	meta := map[string]string{"kind": "provider"}
	if err := fb.Put("KEY", "val", meta); err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	if err := fb.Put("KEY", "val", meta); err != nil {
		t.Fatalf("Put 2: %v", err)
	}

	refs, err := fb.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 1 {
		t.Errorf("got %d entries, want 1", len(refs))
	}
}

func TestPutUpdatesValue(t *testing.T) {
	fb := newTestBackend(t)

	meta := map[string]string{"kind": "provider"}
	if err := fb.Put("KEY", "old-value", meta); err != nil {
		t.Fatalf("Put old: %v", err)
	}
	if err := fb.Put("KEY", "new-value", meta); err != nil {
		t.Fatalf("Put new: %v", err)
	}

	val, _, err := fb.Get("KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "new-value" {
		t.Errorf("value = %q, want %q", val, "new-value")
	}
}

func TestPutMergesMetadata(t *testing.T) {
	fb := newTestBackend(t)

	if err := fb.Put("KEY", "val", map[string]string{"a": "1", "b": "2"}); err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	if err := fb.Put("KEY", "val", map[string]string{"b": "updated", "c": "3"}); err != nil {
		t.Fatalf("Put 2: %v", err)
	}

	_, meta, err := fb.Get("KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if meta["a"] != "1" || meta["b"] != "updated" || meta["c"] != "3" {
		t.Errorf("metadata = %v, want a=1 b=updated c=3", meta)
	}
}

func TestDeleteRemovesEntry(t *testing.T) {
	fb := newTestBackend(t)

	if err := fb.Put("KEY", "val", map[string]string{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := fb.Delete("KEY"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, _, err := fb.Get("KEY")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestDeleteNotFound(t *testing.T) {
	fb := newTestBackend(t)

	err := fb.Delete("NONEXISTENT")
	if err == nil {
		t.Fatal("expected error for nonexistent key, got nil")
	}
}

func TestListReturnsRefsWithoutValues(t *testing.T) {
	fb := newTestBackend(t)

	if err := fb.Put("A", "secret-a", map[string]string{"kind": "provider"}); err != nil {
		t.Fatalf("Put A: %v", err)
	}
	if err := fb.Put("B", "secret-b", map[string]string{"kind": "service"}); err != nil {
		t.Fatalf("Put B: %v", err)
	}

	refs, err := fb.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2", len(refs))
	}

	for _, ref := range refs {
		if ref.Name != "A" && ref.Name != "B" {
			t.Errorf("unexpected name: %q", ref.Name)
		}
		if ref.Metadata == nil {
			t.Errorf("metadata is nil for %q", ref.Name)
		}
	}
}

func TestKeyGenerationOnFirstUse(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "keys", ".key")

	_, err := NewFileBackend(filepath.Join(dir, "store.enc"), keyPath)
	if err != nil {
		t.Fatalf("NewFileBackend: %v", err)
	}

	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	if len(keyData) != 32 {
		t.Errorf("key length = %d, want 32", len(keyData))
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0400 {
		t.Errorf("key perm = %04o, want 0400", perm)
	}
}

func TestEmptyStoreReturnsList(t *testing.T) {
	fb := newTestBackend(t)

	refs, err := fb.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("got %d refs, want 0", len(refs))
	}
}

func TestGetNotFound(t *testing.T) {
	fb := newTestBackend(t)

	_, _, err := fb.Get("NONEXISTENT")
	if err == nil {
		t.Fatal("expected error for nonexistent key, got nil")
	}
}

func TestConcurrentPuts(t *testing.T) {
	fb := newTestBackend(t)

	var wg sync.WaitGroup
	errs := make([]error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := "KEY"
			val := "value"
			errs[idx] = fb.Put(name, val, map[string]string{"i": "x"})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}

	// Should still have exactly one entry.
	refs, err := fb.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 1 {
		t.Errorf("got %d entries after concurrent puts, want 1", len(refs))
	}
}

func TestExistingKeyIsPreserved(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, ".key")
	storePath := filepath.Join(dir, "store.enc")

	// Create first backend (generates key).
	fb1, err := NewFileBackend(storePath, keyPath)
	if err != nil {
		t.Fatalf("NewFileBackend 1: %v", err)
	}
	if err := fb1.Put("K", "V", map[string]string{}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	key1, _ := os.ReadFile(keyPath)

	// Create second backend (should reuse key).
	fb2, err := NewFileBackend(storePath, keyPath)
	if err != nil {
		t.Fatalf("NewFileBackend 2: %v", err)
	}

	key2, _ := os.ReadFile(keyPath)
	if string(key1) != string(key2) {
		t.Fatal("key was regenerated on second init")
	}

	// Second backend can read what first wrote.
	val, _, err := fb2.Get("K")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "V" {
		t.Errorf("value = %q, want %q", val, "V")
	}
}
