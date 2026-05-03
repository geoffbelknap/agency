package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
)

func TestVerifyAppleVFRuntimeArtifactsFailsClosedWithGuidance(t *testing.T) {
	err := verifyMicroVMRuntimeArtifacts(hostruntimebackend.BackendAppleVFMicroVM, map[string]string{
		"kernel_path": "/missing/Image",
	})
	if err == nil {
		t.Fatal("verifyMicroVMRuntimeArtifacts() error = nil, want fail-closed error")
	}
	for _, want := range []string{
		"apple-vf-microvm runtime artifacts are not ready",
		"Apple VF helper",
		"Apple VF kernel",
		"Apple VF host enforcer",
		"Apple VF guest vsock bridge",
		"Apple VF rootfs OCI artifact",
		"scripts/readiness/apple-vf-artifacts.sh",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
}

func TestVerifyAppleVFRuntimeArtifactsRejectsLatestRootFSRef(t *testing.T) {
	dir := t.TempDir()
	err := verifyMicroVMRuntimeArtifacts(hostruntimebackend.BackendAppleVFMicroVM, map[string]string{
		"helper_binary":            executableFixture(t, dir, "helper"),
		"kernel_path":              readableFixture(t, dir, "Image"),
		"mke2fs_path":              executableFixture(t, dir, "mke2fs"),
		"enforcer_binary_path":     executableFixture(t, dir, "enforcer"),
		"vsock_bridge_binary_path": executableFixture(t, dir, "bridge"),
		"rootfs_oci_ref":           "ghcr.io/example/agency-runtime-body:latest",
	})
	if err == nil || !strings.Contains(err.Error(), "uses mutable :latest") {
		t.Fatalf("verifyMicroVMRuntimeArtifacts() error = %v, want latest rejection", err)
	}
}

func TestVerifyFirecrackerRuntimeArtifactsPassesWithConfiguredArtifacts(t *testing.T) {
	dir := t.TempDir()
	cfg := map[string]string{
		"binary_path":              executableFixture(t, dir, "firecracker"),
		"kernel_path":              kernelFixture(t, dir, "vmlinux"),
		"mke2fs_path":              executableFixture(t, dir, "mke2fs"),
		"enforcer_binary_path":     executableFixture(t, dir, "enforcer"),
		"vsock_bridge_binary_path": executableFixture(t, dir, "bridge"),
	}
	if err := verifyMicroVMRuntimeArtifacts(hostruntimebackend.BackendFirecracker, cfg); err != nil {
		t.Fatalf("verifyMicroVMRuntimeArtifacts() error = %v", err)
	}
}

func TestVerifyMicroagentRuntimeArtifactsFailsClosedWithGuidance(t *testing.T) {
	err := verifyMicroVMRuntimeArtifacts(hostruntimebackend.BackendMicroagent, map[string]string{})
	if err == nil {
		t.Fatal("verifyMicroVMRuntimeArtifacts() error = nil, want fail-closed error")
	}
	for _, want := range []string{
		"microagent runtime artifacts are not ready",
		"microagent binary",
		"microagent host enforcer",
		"microagent rootfs OCI artifact",
		"AGENCY_MICROAGENT_BIN",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
}

func TestVerifyMicroagentRuntimeArtifactsRejectsLatestRootFSRef(t *testing.T) {
	dir := t.TempDir()
	err := verifyMicroVMRuntimeArtifacts(hostruntimebackend.BackendMicroagent, map[string]string{
		"binary_path":          executableFixture(t, dir, "microagent"),
		"mke2fs_path":          executableFixture(t, dir, "mke2fs"),
		"enforcer_binary_path": executableFixture(t, dir, "enforcer"),
		"rootfs_oci_ref":       "ghcr.io/example/agency-runtime-body:latest",
	})
	if err == nil || !strings.Contains(err.Error(), "uses mutable :latest") {
		t.Fatalf("verifyMicroVMRuntimeArtifacts() error = %v, want latest rejection", err)
	}
}

func TestVerifyMicroagentRuntimeArtifactsPassesWithConfiguredArtifacts(t *testing.T) {
	dir := t.TempDir()
	cfg := map[string]string{
		"binary_path":          executableFixture(t, dir, "microagent"),
		"mke2fs_path":          executableFixture(t, dir, "mke2fs"),
		"enforcer_binary_path": executableFixture(t, dir, "enforcer"),
		"rootfs_oci_ref":       "ghcr.io/example/agency-runtime-body:v1",
	}
	if err := verifyMicroVMRuntimeArtifacts(hostruntimebackend.BackendMicroagent, cfg); err != nil {
		t.Fatalf("verifyMicroVMRuntimeArtifacts() error = %v", err)
	}
}

func executableFixture(t *testing.T, dir, name string) string {
	t.Helper()
	path := readableFixture(t, dir, name)
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
	return path
}

func readableFixture(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("fixture"), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func kernelFixture(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, append([]byte("\x7fELF"), []byte("fixture")...), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}
