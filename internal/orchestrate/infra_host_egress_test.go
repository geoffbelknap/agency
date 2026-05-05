package orchestrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareHostEgressPathsRepairsCertFileDirectories(t *testing.T) {
	home := t.TempDir()
	certPath := filepath.Join(home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	combinedPath := filepath.Join(home, "infrastructure", "egress", "certs", "mitmproxy-ca.pem")
	if err := os.MkdirAll(certPath, 0o755); err != nil {
		t.Fatal(err)
	}

	inf := &Infra{Home: home}
	paths, err := inf.prepareHostEgressPaths()
	if err != nil {
		t.Fatalf("prepareHostEgressPaths returned error: %v", err)
	}

	if paths.certsDir != filepath.Dir(certPath) {
		t.Fatalf("certs dir = %q, want %q", paths.certsDir, filepath.Dir(certPath))
	}
	if info, err := os.Stat(certPath); err != nil || info.IsDir() {
		t.Fatalf("cert file was not repaired: info=%v err=%v", info, err)
	}
	if info, err := os.Stat(combinedPath); err != nil || info.IsDir() {
		t.Fatalf("combined cert file was not generated: info=%v err=%v", info, err)
	}
}

func TestPrepareHostEgressPathsGeneratesMitmproxyCA(t *testing.T) {
	home := t.TempDir()
	certsDir := filepath.Join(home, "infrastructure", "egress", "certs")

	inf := &Infra{Home: home}
	paths, err := inf.prepareHostEgressPaths()
	if err != nil {
		t.Fatalf("prepareHostEgressPaths returned error: %v", err)
	}

	if paths.certsDir != certsDir {
		t.Fatalf("certs dir = %q, want %q", paths.certsDir, certsDir)
	}
	for _, name := range []string{"mitmproxy-ca.pem", "mitmproxy-ca-cert.pem"} {
		path := filepath.Join(certsDir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s not generated: %v", name, err)
		}
		if info.IsDir() || info.Size() == 0 {
			t.Fatalf("%s invalid: info=%v", name, info)
		}
	}
}

func TestHostInfraVenvBinFindsHomebrewLibexecVenv(t *testing.T) {
	prefix := t.TempDir()
	sourceDir := filepath.Join(prefix, "share", "agency-rc")
	venvPython := filepath.Join(prefix, "libexec", "venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(venvPython), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(venvPython, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := hostInfraVenvBin(sourceDir, "python"); got != venvPython {
		t.Fatalf("hostInfraVenvBin = %q, want %q", got, venvPython)
	}
}

func TestHostInfraVenvBinPrefersSourceVenv(t *testing.T) {
	prefix := t.TempDir()
	sourceDir := filepath.Join(prefix, "share", "agency")
	sourcePython := filepath.Join(sourceDir, ".venv", "bin", "python")
	libexecPython := filepath.Join(prefix, "libexec", "venv", "bin", "python")
	for _, path := range []string{sourcePython, libexecPython} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if got := hostInfraVenvBin(sourceDir, "python"); got != sourcePython {
		t.Fatalf("hostInfraVenvBin = %q, want %q", got, sourcePython)
	}
}
