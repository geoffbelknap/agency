package orchestrate

import (
	"strings"
	"testing"
)

func TestGeneratePlatformMD_Meeseeks(t *testing.T) {
	result := GeneratePlatformMD("meeseeks")
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
	result := GeneratePlatformMD("function")
	if !strings.Contains(result, "enforcer") {
		t.Error("function agent should get operational block")
	}
	if strings.Contains(result, "Knowledge Graph") {
		t.Error("function agent should not get knowledge block")
	}
}

func TestGeneratePlatformMD_Standard(t *testing.T) {
	result := GeneratePlatformMD("standard")
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
	result := GeneratePlatformMD("coordinator")
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
	result := GeneratePlatformMD("unknown-type")
	standard := GeneratePlatformMD("standard")
	if result != standard {
		t.Error("unknown agent type should default to standard blocks")
	}
}
