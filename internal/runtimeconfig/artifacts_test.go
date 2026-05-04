package runtimeconfig

import (
	"path/filepath"
	"testing"

	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
)

func TestMicroagentConfigRefreshesManagedReleaseArtifacts(t *testing.T) {
	home := t.TempDir()
	sourceDir := filepath.Join(home, "Cellar", "agency", "0.3.16", "share", "agency")
	cfg := map[string]string{
		"enforcer_binary_path": filepath.Join(home, "Cellar", "agency", "0.3.15", "share", "agency", "bin", "agency-enforcer-host"),
		"rootfs_oci_ref":       "ghcr.io/geoffbelknap/agency-runtime-body:v0.3.15",
	}

	got := WithMicroagentArtifactConfig(hostruntimebackend.BackendMicroagent, cfg, ArtifactOptions{
		Home:      home,
		SourceDir: sourceDir,
		Version:   "0.3.16",
	})

	if want := filepath.Join(sourceDir, "bin", "agency-enforcer-host"); got["enforcer_binary_path"] != want {
		t.Fatalf("enforcer_binary_path = %q, want %q", got["enforcer_binary_path"], want)
	}
	if want := "ghcr.io/geoffbelknap/agency-runtime-body:v0.3.16"; got["rootfs_oci_ref"] != want {
		t.Fatalf("rootfs_oci_ref = %q, want %q", got["rootfs_oci_ref"], want)
	}
}

func TestMicroagentConfigPreservesCustomArtifacts(t *testing.T) {
	home := t.TempDir()
	cfg := map[string]string{
		"enforcer_binary_path": "/custom/bin/agency-enforcer-host",
		"rootfs_oci_ref":       "ghcr.io/example/body:stable",
	}

	got := WithMicroagentArtifactConfig(hostruntimebackend.BackendMicroagent, cfg, ArtifactOptions{
		Home:      home,
		SourceDir: filepath.Join(home, "share", "agency"),
		Version:   "0.3.16",
	})

	if got["enforcer_binary_path"] != "/custom/bin/agency-enforcer-host" {
		t.Fatalf("custom enforcer path was not preserved: %#v", got)
	}
	if got["rootfs_oci_ref"] != "ghcr.io/example/body:stable" {
		t.Fatalf("custom rootfs ref was not preserved: %#v", got)
	}
}
