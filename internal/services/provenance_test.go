package services

import "testing"

func TestGenerateHMAC(t *testing.T) {
	key := []byte("test-secret-key")
	hmac1 := GenerateHMAC("agency-infra-comms", key)
	hmac2 := GenerateHMAC("agency-infra-comms", key)
	if hmac1 != hmac2 {
		t.Errorf("HMAC not deterministic: %s != %s", hmac1, hmac2)
	}
	if hmac1 == "" {
		t.Error("HMAC is empty")
	}
}

func TestGenerateHMACDifferentNames(t *testing.T) {
	key := []byte("test-secret-key")
	hmac1 := GenerateHMAC("agency-infra-comms", key)
	hmac2 := GenerateHMAC("agency-infra-analysis", key)
	if hmac1 == hmac2 {
		t.Error("different names should produce different HMACs")
	}
}

func TestVerifyHMAC(t *testing.T) {
	key := []byte("test-secret-key")
	hmac := GenerateHMAC("agency-infra-comms", key)
	if !VerifyHMAC("agency-infra-comms", hmac, key) {
		t.Error("valid HMAC should verify")
	}
}

func TestVerifyHMACWrongKey(t *testing.T) {
	key := []byte("test-secret-key")
	hmac := GenerateHMAC("agency-infra-comms", key)
	if VerifyHMAC("agency-infra-comms", hmac, []byte("wrong-key")) {
		t.Error("wrong key should not verify")
	}
}

func TestVerifyHMACWrongName(t *testing.T) {
	key := []byte("test-secret-key")
	hmac := GenerateHMAC("agency-infra-comms", key)
	if VerifyHMAC("agency-infra-analysis", hmac, key) {
		t.Error("wrong name should not verify")
	}
}

func TestVerifyProvenance_InMemory(t *testing.T) {
	known := map[string]bool{"abc123": true}
	ok, source, err := VerifyProvenance("abc123", "agency-infra-comms", known, "", nil)
	if err != nil || !ok {
		t.Errorf("in-memory container should verify: ok=%v err=%v", ok, err)
	}
	if source != "memory" {
		t.Errorf("source should be memory, got %s", source)
	}
}

func TestVerifyProvenance_HMAC(t *testing.T) {
	key := []byte("test-key")
	hmac := GenerateHMAC("agency-infra-comms", key)
	known := map[string]bool{}
	ok, source, err := VerifyProvenance("xyz789", "agency-infra-comms", known, hmac, key)
	if err != nil || !ok {
		t.Errorf("HMAC should verify: ok=%v err=%v", ok, err)
	}
	if source != "hmac" {
		t.Errorf("source should be hmac, got %s", source)
	}
}

func TestVerifyProvenance_Rejected(t *testing.T) {
	known := map[string]bool{}
	ok, _, err := VerifyProvenance("xyz789", "agency-infra-comms", known, "bad-hmac", []byte("key"))
	if ok {
		t.Error("bad HMAC should not verify")
	}
	if err != nil {
		t.Errorf("rejection is not an error: %v", err)
	}
}
