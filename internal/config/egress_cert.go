package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// ensureEgressCACert generates the mitmproxy CA certificate and key files
// if they don't already exist. This must run before infra containers start
// so that the enforcer and workspace containers can mount the CA cert for
// TLS trust on first boot. Without this, there's a race condition: mitmproxy
// generates the cert on first run, but the enforcer may start before the cert
// exists, causing TLS handshake failures (502).
//
// mitmproxy expects two files in its confdir:
//   - mitmproxy-ca.pem: PEM-encoded private key + certificate (combined)
//   - mitmproxy-ca-cert.pem: PEM-encoded certificate only (public)
//
// If the combined file (mitmproxy-ca.pem) already exists, this is a no-op.
// mitmproxy will use pre-existing certs rather than regenerating.
func EnsureEgressCACert(certsDir string) error {
	return ensureEgressCACert(certsDir)
}

func ensureEgressCACert(certsDir string) error {
	combinedPath := filepath.Join(certsDir, "mitmproxy-ca.pem")
	certOnlyPath := filepath.Join(certsDir, "mitmproxy-ca-cert.pem")

	// Idempotent: skip if both files are already generated.
	if _, err := os.Stat(combinedPath); err == nil {
		if _, err := os.Stat(certOnlyPath); err == nil {
			return nil
		}
		return writeCertOnlyFromCombined(combinedPath, certOnlyPath)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate CA key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "mitmproxy",
			Organization: []string{"mitmproxy"},
		},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create CA certificate: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// mitmproxy-ca.pem: combined key + cert (what mitmproxy reads on startup)
	combined := append(keyPEM, certPEM...)
	if err := os.WriteFile(combinedPath, combined, 0600); err != nil {
		return fmt.Errorf("write combined CA file: %w", err)
	}

	// mitmproxy-ca-cert.pem: cert only (mounted into enforcer/workspace for trust)
	if err := os.WriteFile(certOnlyPath, certPEM, 0644); err != nil {
		return fmt.Errorf("write CA cert file: %w", err)
	}

	return nil
}

func writeCertOnlyFromCombined(combinedPath, certOnlyPath string) error {
	data, err := os.ReadFile(combinedPath)
	if err != nil {
		return fmt.Errorf("read combined CA file: %w", err)
	}
	for len(data) > 0 {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		certPEM := pem.EncodeToMemory(block)
		if len(certPEM) == 0 {
			return fmt.Errorf("encode CA cert file: empty certificate block")
		}
		if err := os.WriteFile(certOnlyPath, certPEM, 0644); err != nil {
			return fmt.Errorf("write CA cert file: %w", err)
		}
		return nil
	}
	return fmt.Errorf("combined CA file does not contain a certificate block")
}
