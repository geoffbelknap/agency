package pathsafety

import (
	"path/filepath"
	"testing"
)

func TestSegmentAcceptsRuntimeNames(t *testing.T) {
	for _, name := range []string{"alice", "alice-enforcer", "agent_1", "agent.1"} {
		if got, err := Segment("runtime id", name); err != nil || got != name {
			t.Fatalf("Segment(%q) = %q, %v", name, got, err)
		}
	}
}

func TestSegmentRejectsPathTraversal(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../alice", "alice/bob", `alice\bob`, "alice:bob"} {
		if _, err := Segment("runtime id", name); err == nil {
			t.Fatalf("Segment(%q) returned nil error", name)
		}
	}
}

func TestJoinKeepsPathInsideBase(t *testing.T) {
	base := t.TempDir()
	got, err := Join(base, "alice", "rootfs.ext4")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "alice", "rootfs.ext4")
	if got != want {
		t.Fatalf("Join = %q, want %q", got, want)
	}
}
