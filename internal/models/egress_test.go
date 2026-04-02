package models

import "testing"

func TestEgressDomainEntry_Validate(t *testing.T) {
	tests := []struct {
		name    string
		entry   EgressDomainEntry
		wantErr bool
	}{
		{"valid", EgressDomainEntry{Domain: "api.example.com", ApprovedAt: "2026-01-01", ApprovedBy: "admin"}, false},
		{"empty domain", EgressDomainEntry{Domain: "", ApprovedAt: "2026-01-01", ApprovedBy: "admin"}, true},
		{"empty approved_at", EgressDomainEntry{Domain: "api.example.com", ApprovedAt: "", ApprovedBy: "admin"}, true},
		{"empty approved_by", EgressDomainEntry{Domain: "api.example.com", ApprovedAt: "2026-01-01", ApprovedBy: ""}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate.Struct(tt.entry)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAgentEgressConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     AgentEgressConfig
		wantErr bool
	}{
		{"valid minimal", AgentEgressConfig{Agent: "test", Mode: "denylist"}, false},
		{"valid allowlist", AgentEgressConfig{Agent: "test", Mode: "allowlist"}, false},
		{"empty agent", AgentEgressConfig{Agent: "", Mode: "denylist"}, true},
		{"invalid mode", AgentEgressConfig{Agent: "test", Mode: "open"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate.Struct(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
