package consent

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func signToken(t *testing.T, token Token, priv ed25519.PrivateKey) string {
	t.Helper()
	raw, err := token.MarshalCanonical()
	if err != nil {
		t.Fatalf("marshal token: %v", err)
	}
	encoded, err := EncodeSignedToken(SignedToken{
		Token:     token,
		Signature: ed25519.Sign(priv, raw),
	})
	if err != nil {
		t.Fatalf("encode token: %v", err)
	}
	return encoded
}

func testToken(now time.Time) Token {
	return Token{
		Version:         1,
		DeploymentID:    "dep-123",
		OperationKind:   "add_managed_doc",
		OperationTarget: []byte("drive-abc"),
		Issuer:          "slack-interactivity",
		Witnesses:       []string{"U1", "U2"},
		IssuedAt:        now.UnixMilli(),
		ExpiresAt:       now.Add(5 * time.Minute).UnixMilli(),
		Nonce:           []byte("0123456789abcdef"),
		SigningKeyID:    "dep-123:v1",
	}
}

func TestValidatorAcceptsValidToken(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Now().UTC()
	token := testToken(now)
	validator := NewValidator("dep-123", map[string]ed25519.PublicKey{
		token.SigningKeyID: pub,
	}, 15*time.Minute, 30*time.Second)
	result, err := validator.Validate(Requirement{
		OperationKind:    "add_managed_doc",
		TokenInputField:  "consent_token",
		TargetInputField: "drive_id",
		MinWitnesses:     2,
	}, signToken(t, token, priv), "drive-abc", now)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.NonceEncoded == "" {
		t.Fatal("expected nonce to be recorded")
	}
}

func TestValidatorRejectsReplay(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Now().UTC()
	token := testToken(now)
	validator := NewValidator("dep-123", map[string]ed25519.PublicKey{
		token.SigningKeyID: pub,
	}, 15*time.Minute, 30*time.Second)
	encoded := signToken(t, token, priv)
	req := Requirement{
		OperationKind:    "add_managed_doc",
		TokenInputField:  "consent_token",
		TargetInputField: "drive_id",
		MinWitnesses:     2,
	}
	if _, err := validator.Validate(req, encoded, "drive-abc", now); err != nil {
		t.Fatalf("initial validation failed: %v", err)
	}
	if _, err := validator.Validate(req, encoded, "drive-abc", now.Add(1*time.Second)); err != ErrReplayed {
		t.Fatalf("expected ErrReplayed, got %v", err)
	}
}

func TestValidatorRejectsWrongTarget(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Now().UTC()
	token := testToken(now)
	validator := NewValidator("dep-123", map[string]ed25519.PublicKey{
		token.SigningKeyID: pub,
	}, 15*time.Minute, 30*time.Second)
	_, err = validator.Validate(Requirement{
		OperationKind:    "add_managed_doc",
		TokenInputField:  "consent_token",
		TargetInputField: "drive_id",
		MinWitnesses:     2,
	}, signToken(t, token, priv), "drive-other", now)
	if err != ErrWrongTarget {
		t.Fatalf("expected ErrWrongTarget, got %v", err)
	}
}

func TestVerificationConfigWriteAndLoad(t *testing.T) {
	dir := t.TempDir()
	cfg := &VerificationConfig{
		DeploymentID:    "dep-123",
		MaxTTLSeconds:   900,
		ClockSkewMillis: 30000,
		VerificationKeys: map[string]string{
			"dep-123:v1": "pubkey",
		},
	}
	if err := cfg.Write(dir); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	loaded, err := LoadVerificationConfig(dir)
	if err != nil {
		t.Fatalf("LoadVerificationConfig returned error: %v", err)
	}
	if loaded.DeploymentID != cfg.DeploymentID {
		t.Fatalf("deployment_id = %q, want %q", loaded.DeploymentID, cfg.DeploymentID)
	}
	if loaded.VerificationKeys["dep-123:v1"] != "pubkey" {
		t.Fatalf("verification key not preserved")
	}
}

func TestNextKeyIDIncrementsVersion(t *testing.T) {
	keys := map[string]string{
		"dep-123:v1": "a",
		"dep-123:v3": "b",
		"other:v2":   "c",
	}
	if got := NextKeyID(keys, "dep-123"); got != "dep-123:v4" {
		t.Fatalf("NextKeyID = %q, want dep-123:v4", got)
	}
}

func TestConfigFileNameStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), ConfigFileName)
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file missing: %v", err)
	}
}
