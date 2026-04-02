package orchestrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWorkspaceEnv(t *testing.T) {
	// Write a temp .env file
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("MY_CONFIG=hello\nOTHER=world\n"), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	declared := map[string]string{
		"CRED_VAR":   "${credential:my-cred}",
		"CONFIG_VAR": "${config:MY_CONFIG}",
		"STATIC_VAR": "staticvalue",
	}

	got := resolveWorkspaceEnv(declared, dir, "scoped-key-abc123")

	if got["CRED_VAR"] != "scoped-key-abc123" {
		t.Errorf("CRED_VAR: want scoped-key-abc123, got %q", got["CRED_VAR"])
	}
	if got["CONFIG_VAR"] != "hello" {
		t.Errorf("CONFIG_VAR: want hello, got %q", got["CONFIG_VAR"])
	}
	if got["STATIC_VAR"] != "staticvalue" {
		t.Errorf("STATIC_VAR: want staticvalue, got %q", got["STATIC_VAR"])
	}
}

func TestResolveWorkspaceEnvMissingConfig(t *testing.T) {
	dir := t.TempDir()
	// .env file exists but does not contain the requested key
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OTHER=x\n"), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	declared := map[string]string{
		"MISSING": "${config:NOT_PRESENT}",
	}

	got := resolveWorkspaceEnv(declared, dir, "key")
	if got["MISSING"] != "" {
		t.Errorf("missing config key: want empty string, got %q", got["MISSING"])
	}
}

func TestWorkspaceDeps_Merge(t *testing.T) {
	a := WorkspaceDeps{
		Pip: []string{"requests", "boto3"},
		Apt: []string{"curl", "jq"},
		Env: map[string]string{"FOO": "from-a", "BAR": "bar-a"},
	}
	b := WorkspaceDeps{
		Pip: []string{"boto3", "numpy"},   // boto3 is a duplicate
		Apt: []string{"jq", "git"},        // jq is a duplicate
		Env: map[string]string{"FOO": "from-b", "BAZ": "baz-b"},
	}

	a.Merge(b)

	// Check pip deduplication
	pipSet := make(map[string]int)
	for _, p := range a.Pip {
		pipSet[p]++
	}
	for _, pkg := range []string{"requests", "boto3", "numpy"} {
		if pipSet[pkg] != 1 {
			t.Errorf("pip package %q: want count 1, got %d", pkg, pipSet[pkg])
		}
	}

	// Check apt deduplication
	aptSet := make(map[string]int)
	for _, p := range a.Apt {
		aptSet[p]++
	}
	for _, pkg := range []string{"curl", "jq", "git"} {
		if aptSet[pkg] != 1 {
			t.Errorf("apt package %q: want count 1, got %d", pkg, aptSet[pkg])
		}
	}

	// First wins on env conflict
	if a.Env["FOO"] != "from-a" {
		t.Errorf("env FOO: want from-a (first wins), got %q", a.Env["FOO"])
	}
	// Non-conflicting key from b is added
	if a.Env["BAZ"] != "baz-b" {
		t.Errorf("env BAZ: want baz-b, got %q", a.Env["BAZ"])
	}
	// Original non-conflicting key preserved
	if a.Env["BAR"] != "bar-a" {
		t.Errorf("env BAR: want bar-a, got %q", a.Env["BAR"])
	}
}

func TestWorkspaceDeps_IsEmpty(t *testing.T) {
	empty := WorkspaceDeps{}
	if !empty.IsEmpty() {
		t.Error("empty WorkspaceDeps should return IsEmpty() == true")
	}

	withPip := WorkspaceDeps{Pip: []string{"requests"}}
	if withPip.IsEmpty() {
		t.Error("WorkspaceDeps with pip packages should return IsEmpty() == false")
	}

	withApt := WorkspaceDeps{Apt: []string{"curl"}}
	if withApt.IsEmpty() {
		t.Error("WorkspaceDeps with apt packages should return IsEmpty() == false")
	}

	withEnv := WorkspaceDeps{Env: map[string]string{"K": "V"}}
	if withEnv.IsEmpty() {
		t.Error("WorkspaceDeps with env vars should return IsEmpty() == false")
	}
}
