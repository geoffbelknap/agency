package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireStartLockCreatesAndReleasesLock(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENCY_HOME", tmp)

	release, alreadyStarted, err := acquireStartLock(func() bool { return false }, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("acquireStartLock returned error: %v", err)
	}
	if alreadyStarted {
		t.Fatal("expected to acquire startup lock, got alreadyStarted=true")
	}
	if _, err := os.Stat(filepath.Join(tmp, "gateway.start.lock")); err != nil {
		t.Fatalf("expected startup lock file to exist: %v", err)
	}

	release()
	if _, err := os.Stat(filepath.Join(tmp, "gateway.start.lock")); !os.IsNotExist(err) {
		t.Fatalf("expected startup lock file to be removed, got err=%v", err)
	}
}

func TestAcquireStartLockReturnsAlreadyStartedWhenHealthy(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENCY_HOME", tmp)

	lockPath := filepath.Join(tmp, "gateway.start.lock")
	if err := os.WriteFile(lockPath, []byte("123\n"), 0644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	release, alreadyStarted, err := acquireStartLock(func() bool { return true }, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("acquireStartLock returned error: %v", err)
	}
	if !alreadyStarted {
		t.Fatal("expected alreadyStarted=true when healthy waiter succeeds")
	}
	release()
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected existing lock file to remain untouched, got err=%v", err)
	}
}

func TestAcquireStartLockReclaimsStaleLock(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENCY_HOME", tmp)

	lockPath := filepath.Join(tmp, "gateway.start.lock")
	if err := os.WriteFile(lockPath, []byte("123\n"), 0644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}
	old := time.Now().Add(-startLockStaleAfter - time.Second)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	release, alreadyStarted, err := acquireStartLock(func() bool { return false }, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("acquireStartLock returned error: %v", err)
	}
	if alreadyStarted {
		t.Fatal("expected stale lock to be reclaimed")
	}
	release()
}

func TestAcquireStartLockTimesOutWhenAnotherStartStalls(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENCY_HOME", tmp)

	lockPath := filepath.Join(tmp, "gateway.start.lock")
	if err := os.WriteFile(lockPath, []byte("123\n"), 0644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	_, _, err := acquireStartLock(func() bool { return false }, 200*time.Millisecond)
	if err == nil || err.Error() != "timed out waiting for daemon startup lock" {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}
