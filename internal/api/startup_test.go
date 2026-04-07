package api

import (
	"testing"

	"github.com/geoffbelknap/agency/internal/config"
)

// TestStartup_NilDocker_ReturnsError verifies that passing a nil Docker client
// causes Startup to return an error rather than a partially-initialized result.
// This exercises the hard-fail semantics for core component initialization.
func TestStartup_NilDocker_ReturnsError(t *testing.T) {
	cfg := &config.Config{Home: t.TempDir(), Version: "test"}
	_, err := Startup(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error when docker client is nil")
	}
}
