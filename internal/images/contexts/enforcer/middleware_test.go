package main

import (
	"testing"
)

func TestIsValidToken(t *testing.T) {
	// Set up AuthMiddleware with one registered key.
	keys := []APIKey{
		{Key: "real-secret-key-abc123", Name: "test-agent"},
	}
	am := NewAuthMiddleware(keys)

	t.Run("registered key passes", func(t *testing.T) {
		if !am.isValidToken("real-secret-key-abc123") {
			t.Error("registered key should be valid")
		}
	})

	t.Run("fabricated agency-scoped prefix is rejected", func(t *testing.T) {
		if am.isValidToken("agency-scoped-fake") {
			t.Error("fabricated 'agency-scoped-fake' token must be rejected — prefix bypass is a security vulnerability (ASK Tenet 3: mediation is complete)")
		}
	})

	t.Run("empty string is rejected", func(t *testing.T) {
		if am.isValidToken("") {
			t.Error("empty token should be rejected")
		}
	})

	t.Run("random string is rejected", func(t *testing.T) {
		if am.isValidToken("not-a-valid-token-xyz987") {
			t.Error("random unregistered token should be rejected")
		}
	})
}
