// agency-gateway/internal/models/validate_test.go
package models

import "testing"

func TestValidateAPIBase(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid https", "https://api.anthropic.com", false},
		{"valid http local", "http://localhost:11434", false},
		{"empty", "", true},
		{"blocked metadata", "http://169.254.169.254/latest", true},
		{"raw IP https", "https://192.168.1.1/api", true},
		{"raw IP http ok", "http://192.168.1.1/api", false},
		{"bad scheme", "ftp://example.com", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAPIBase(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAPIBase(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateCredentialEnv(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"", false},
		{"ANTHROPIC_API_KEY", false},
		{"OPENAI_TOKEN", false},
		{"MY_SECRET", false},
		{"lowercase_key", true},
		{"NOPE", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := ValidateCredentialEnv(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCredentialEnv(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateHierarchyName(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"engineering", true},
		{"my-team", true},
		{"a", false},
		{"-bad", false},
		{"UPPER", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ValidateHierarchyName(tt.input)
			if got != tt.want {
				t.Errorf("ValidateHierarchyName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
