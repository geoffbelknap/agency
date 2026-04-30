package config

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureEgressCACert_GeneratesValidCert(t *testing.T) {
	dir := t.TempDir()

	if err := ensureEgressCACert(dir); err != nil {
		t.Fatalf("ensureEgressCACert failed: %v", err)
	}

	// Both files should exist
	combinedPath := filepath.Join(dir, "mitmproxy-ca.pem")
	certOnlyPath := filepath.Join(dir, "mitmproxy-ca-cert.pem")

	if _, err := os.Stat(combinedPath); err != nil {
		t.Fatalf("mitmproxy-ca.pem not created: %v", err)
	}
	if _, err := os.Stat(certOnlyPath); err != nil {
		t.Fatalf("mitmproxy-ca-cert.pem not created: %v", err)
	}

	// Parse the cert-only file and validate it's a CA cert
	certPEM, err := os.ReadFile(certOnlyPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if !cert.IsCA {
		t.Error("expected CA cert")
	}
	if cert.Subject.CommonName != "mitmproxy" {
		t.Errorf("expected CN=mitmproxy, got %s", cert.Subject.CommonName)
	}

	// Combined file should contain both key and cert
	combined, err := os.ReadFile(combinedPath)
	if err != nil {
		t.Fatalf("read combined: %v", err)
	}
	keyBlock, rest := pem.Decode(combined)
	if keyBlock == nil || keyBlock.Type != "RSA PRIVATE KEY" {
		t.Fatal("expected RSA PRIVATE KEY as first block in combined file")
	}
	certBlock, _ := pem.Decode(rest)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		t.Fatal("expected CERTIFICATE as second block in combined file")
	}
}

func TestEnsureEgressCACert_Idempotent(t *testing.T) {
	dir := t.TempDir()

	if err := ensureEgressCACert(dir); err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	// Read the cert
	first, err := os.ReadFile(filepath.Join(dir, "mitmproxy-ca-cert.pem"))
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}

	// Second call should be a no-op
	if err := ensureEgressCACert(dir); err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	second, err := os.ReadFile(filepath.Join(dir, "mitmproxy-ca-cert.pem"))
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}

	if string(first) != string(second) {
		t.Error("second call regenerated cert — should be idempotent")
	}
}

func TestEnsureEgressCACert_RepairsMissingCertOnlyFile(t *testing.T) {
	dir := t.TempDir()

	if err := ensureEgressCACert(dir); err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	certOnlyPath := filepath.Join(dir, "mitmproxy-ca-cert.pem")
	first, err := os.ReadFile(certOnlyPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	if err := os.Remove(certOnlyPath); err != nil {
		t.Fatalf("remove cert-only file: %v", err)
	}

	if err := ensureEgressCACert(dir); err != nil {
		t.Fatalf("repair call failed: %v", err)
	}

	repaired, err := os.ReadFile(certOnlyPath)
	if err != nil {
		t.Fatalf("read repaired cert: %v", err)
	}
	if string(repaired) != string(first) {
		t.Fatal("repaired cert-only file does not match existing combined CA certificate")
	}
}

func TestEnsureEgressCACert_FilePermissions(t *testing.T) {
	dir := t.TempDir()

	if err := ensureEgressCACert(dir); err != nil {
		t.Fatalf("ensureEgressCACert failed: %v", err)
	}

	// Combined file (has private key) should be 0600
	info, _ := os.Stat(filepath.Join(dir, "mitmproxy-ca.pem"))
	if info.Mode().Perm() != 0600 {
		t.Errorf("mitmproxy-ca.pem perms = %o, want 0600", info.Mode().Perm())
	}

	// Cert-only file should be 0644 (public)
	info, _ = os.Stat(filepath.Join(dir, "mitmproxy-ca-cert.pem"))
	if info.Mode().Perm() != 0644 {
		t.Errorf("mitmproxy-ca-cert.pem perms = %o, want 0644", info.Mode().Perm())
	}
}

func TestRunInit_GeneratesEgressCACert(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	_, err := RunInit(InitOptions{})
	if err != nil {
		t.Fatalf("RunInit failed: %v", err)
	}

	certPath := filepath.Join(tmpDir, ".agency", "infrastructure", "egress", "certs", "mitmproxy-ca-cert.pem")
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("RunInit did not generate egress CA cert: %v", err)
	}
}
