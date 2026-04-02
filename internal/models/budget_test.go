package models

import "testing"

func TestDefaultPlatformBudgetConfig(t *testing.T) {
	cfg := DefaultPlatformBudgetConfig()
	if cfg.AgentDaily != 10.00 {
		t.Errorf("AgentDaily = %f, want 10.00", cfg.AgentDaily)
	}
	if cfg.AgentMonthly != 200.00 {
		t.Errorf("AgentMonthly = %f, want 200.00", cfg.AgentMonthly)
	}
	if cfg.PerTask != 2.00 {
		t.Errorf("PerTask = %f, want 2.00", cfg.PerTask)
	}
	if cfg.InfraDaily != 5.00 {
		t.Errorf("InfraDaily = %f, want 5.00", cfg.InfraDaily)
	}
}

func TestPlatformBudgetConfigZeroValid(t *testing.T) {
	cfg := PlatformBudgetConfig{}
	if cfg.AgentDaily != 0 || cfg.AgentMonthly != 0 || cfg.PerTask != 0 {
		t.Error("zero-value PlatformBudgetConfig should have zero limits")
	}
}
