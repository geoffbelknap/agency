package orchestrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareHostEgressPathsRepairsCertFileDirectories(t *testing.T) {
	home := t.TempDir()
	certPath := filepath.Join(home, "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
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
	if _, err := os.Stat(certPath); !os.IsNotExist(err) {
		t.Fatalf("invalid cert directory still exists: %v", err)
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
