package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUserLinePresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subuid")
	content := "root:0:65536\nalice:100000:65536\ndaemon:165536:65536\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		user string
		want bool
	}{
		{"alice", true},
		{"root", true},
		{"daemon", true},
		{"bob", false},
		{"alic", false}, // prefix of alice, must not match
	}
	for _, tc := range cases {
		if got := userLinePresent(path, tc.user); got != tc.want {
			t.Errorf("userLinePresent(%q) = %v, want %v", tc.user, got, tc.want)
		}
	}
}

func TestUserLinePresentMissingFile(t *testing.T) {
	if got := userLinePresent("/nonexistent/definitely-not-real", "alice"); got {
		t.Error("want false when file does not exist")
	}
}

func TestPluralize(t *testing.T) {
	if got := pluralize(1, "is", "are"); got != "is" {
		t.Errorf("pluralize(1) = %q, want is", got)
	}
	if got := pluralize(2, "is", "are"); got != "are" {
		t.Errorf("pluralize(2) = %q, want are", got)
	}
	if got := pluralize(0, "is", "are"); got != "are" {
		t.Errorf("pluralize(0) = %q, want are", got)
	}
}

func TestInteractiveInstallDisabledByEnv(t *testing.T) {
	t.Setenv("AGENCY_NO_INTERACTIVE", "1")
	if interactiveInstallEnabled() {
		t.Fatal("want interactive disabled when AGENCY_NO_INTERACTIVE=1")
	}
}

func TestInteractiveInstallRequiresTTY(t *testing.T) {
	// In the test runner stdin is not a TTY, so even without the env
	// override this should return false. Belt + suspenders: clear the
	// env var first.
	t.Setenv("AGENCY_NO_INTERACTIVE", "")
	if interactiveInstallEnabled() {
		t.Fatal("want interactive disabled when stdin is not a TTY")
	}
}
