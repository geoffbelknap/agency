package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// GenerateHMAC creates an HMAC-SHA256 signature for a container name.
func GenerateHMAC(containerName string, key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(containerName))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyHMAC checks if the label value matches the expected HMAC.
func VerifyHMAC(containerName, labelValue string, key []byte) bool {
	expected := GenerateHMAC(containerName, key)
	return hmac.Equal([]byte(expected), []byte(labelValue))
}

// VerifyProvenance checks dual-layer verification: in-memory first, then HMAC.
// Returns (verified, source, error). Source is "memory", "hmac", or "" if rejected.
func VerifyProvenance(containerID, containerName string, knownIDs map[string]bool, hmacLabel string, hmacKey []byte) (bool, string, error) {
	if knownIDs[containerID] {
		return true, "memory", nil
	}
	if len(hmacKey) > 0 && hmacLabel != "" && VerifyHMAC(containerName, hmacLabel, hmacKey) {
		return true, "hmac", nil
	}
	return false, "", nil
}
