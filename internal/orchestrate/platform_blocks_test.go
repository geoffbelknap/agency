package orchestrate

import (
	"strings"
	"testing"
)

func TestGeneratePlatformMD_Meeseeks(t *testing.T) {
	result := GeneratePlatformMD("meeseeks", nil)
	if !strings.Contains(result, "Agency") {
		t.Error("meeseeks platform context should mention Agency")
	}
	if !strings.Contains(result, "github.com/geoffbelknap/agency") {
		t.Error("meeseeks platform context should include canonical source pointer")
	}
	if strings.Contains(result, "enforcer") {
		t.Error("meeseeks should not get operational block")
	}
	if strings.Contains(result, "Knowledge Graph") {
		t.Error("meeseeks should not get knowledge block")
	}
}

func TestGeneratePlatformMD_Function(t *testing.T) {
	result := GeneratePlatformMD("function", nil)
	if !strings.Contains(result, "enforcer") {
		t.Error("function agent should get operational block")
	}
	if strings.Contains(result, "Knowledge Graph") {
		t.Error("function agent should not get knowledge block")
	}
}

func TestGeneratePlatformMD_Standard(t *testing.T) {
	result := GeneratePlatformMD("standard", nil)
	if !strings.Contains(result, "enforcer") {
		t.Error("standard agent should get operational block")
	}
	if !strings.Contains(result, "Knowledge Graph") {
		t.Error("standard agent should get knowledge block")
	}
	if strings.Contains(result, "Delegation") {
		t.Error("standard agent should not get delegation block")
	}
}

func TestGeneratePlatformMD_Coordinator(t *testing.T) {
	result := GeneratePlatformMD("coordinator", nil)
	if !strings.Contains(result, "enforcer") {
		t.Error("coordinator should get operational block")
	}
	if !strings.Contains(result, "Knowledge Graph") {
		t.Error("coordinator should get knowledge block")
	}
	if !strings.Contains(result, "Delegation") {
		t.Error("coordinator should get delegation block")
	}
}

func TestGeneratePlatformMD_UnknownDefaultsToStandard(t *testing.T) {
	result := GeneratePlatformMD("unknown-type", nil)
	standard := GeneratePlatformMD("standard", nil)
	if result != standard {
		t.Error("unknown agent type should default to standard blocks")
	}
}

func TestGeneratePlatformMD_NotGrantedCaps(t *testing.T) {
	// No caps granted — should list all as not available
	result := GeneratePlatformMD("standard", nil)
	if !strings.Contains(result, "Capabilities Not Available") {
		t.Error("should include not-available section when no caps granted")
	}
	if !strings.Contains(result, "web-fetch") {
		t.Error("should mention web-fetch as not granted")
	}
}

func TestGeneratePlatformMD_GrantedCapsOmitted(t *testing.T) {
	// All known caps granted — should NOT list anything as unavailable
	granted := map[string]bool{}
	for cap := range allCapabilities {
		granted[cap] = true
	}
	result := GeneratePlatformMD("standard", granted)
	if strings.Contains(result, "Capabilities Not Available") {
		t.Error("should not include not-available section when all caps granted")
	}
}
