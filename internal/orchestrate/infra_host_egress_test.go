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
