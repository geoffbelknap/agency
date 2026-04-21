package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareCommsDataDirSeedsWritableFiles(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "infrastructure", "comms", "data")

	if err := prepareCommsDataDir(dataDir); err != nil {
		t.Fatalf("prepareCommsDataDir() error = %v", err)
	}

	for _, path := range []string{
		dataDir,
		filepath.Join(dataDir, "channels"),
		filepath.Join(dataDir, "cursors"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", path)
		}
		if info.Mode().Perm() != 0o777 {
			t.Fatalf("%s mode = %o, want 777", path, info.Mode().Perm())
		}
	}

	for _, path := range []string{
		filepath.Join(dataDir, "index.db"),
		filepath.Join(dataDir, "subscriptions.db"),
		filepath.Join(dataDir, "cursors", "_operator.json"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode().Perm() != 0o666 {
			t.Fatalf("%s mode = %o, want 666", path, info.Mode().Perm())
		}
		f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0)
		if err != nil {
			t.Fatalf("%s is not writable: %v", path, err)
		}
		_ = f.Close()
	}

	data, err := os.ReadFile(filepath.Join(dataDir, "cursors", "_operator.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "{}" {
		t.Fatalf("_operator cursor = %q, want {}", string(data))
	}
}

func TestCommsDataRootForRepair(t *testing.T) {
	path := "/home/me/.agency/infrastructure/comms/data/cursors/_operator.json"
	want := "/home/me/.agency/infrastructure/comms/data"
	if got := commsDataRootForRepair(path); got != want {
		t.Fatalf("commsDataRootForRepair() = %q, want %q", got, want)
	}
}

func TestPrepareKnowledgeDataDirKeepsSQLiteFilesWritable(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "knowledge", "data")
	if err := os.MkdirAll(dataDir, 0o777); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"knowledge.db", "knowledge.db-shm", "knowledge.db-wal", ".ontology-migrated"} {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := prepareKnowledgeDataDir(dataDir); err != nil {
		t.Fatalf("prepareKnowledgeDataDir() error = %v", err)
	}

	for _, name := range []string{"knowledge.db", "knowledge.db-shm", "knowledge.db-wal", ".ontology-migrated"} {
		path := filepath.Join(dataDir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode().Perm() != 0o666 {
			t.Fatalf("%s mode = %o, want 666", path, info.Mode().Perm())
		}
		f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0)
		if err != nil {
			t.Fatalf("%s is not writable: %v", path, err)
		}
		_ = f.Close()
	}
}

func TestKnowledgeDataRootForRepair(t *testing.T) {
	path := "/home/me/.agency/knowledge/data/knowledge.db"
	want := "/home/me/.agency/knowledge/data"
	if got := knowledgeDataRootForRepair(path); got != want {
		t.Fatalf("knowledgeDataRootForRepair() = %q, want %q", got, want)
	}
}

func TestPrepareKnowledgeRegistrySnapshotCopiesReadableFallback(t *testing.T) {
	home := t.TempDir()
	knowledgeDir := filepath.Join(home, "knowledge", "data")
	if err := os.MkdirAll(knowledgeDir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "registry.json"), []byte(`{"principals":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := prepareKnowledgeRegistrySnapshot(home, knowledgeDir); err != nil {
		t.Fatalf("prepareKnowledgeRegistrySnapshot() error = %v", err)
	}

	dst := filepath.Join(knowledgeDir, "registry.json")
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != `{"principals":[]}` {
		t.Fatalf("registry snapshot = %q", string(data))
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("%s mode = %o, want 644", dst, info.Mode().Perm())
	}
}
