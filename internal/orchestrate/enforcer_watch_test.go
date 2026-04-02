package orchestrate

import "testing"

func TestExtractAgentName(t *testing.T) {
	tests := []struct {
		containerName string
		suffix        string
		want          string
	}{
		{"agency-henrybot900-enforcer", "-enforcer", "henrybot900"},
		{"agency-alice-enforcer", "-enforcer", "alice"},
		{"/agency-bob-enforcer", "-enforcer", "bob"},
		{"agency-henrybot900-workspace", "-enforcer", ""},        // wrong suffix
		{"other-henrybot900-enforcer", "-enforcer", ""},          // wrong prefix
		{"agency-my-agent-name-enforcer", "-enforcer", "my-agent-name"},
	}
	for _, tt := range tests {
		got := extractAgentName(tt.containerName, tt.suffix)
		if got != tt.want {
			t.Errorf("extractAgentName(%q, %q) = %q, want %q", tt.containerName, tt.suffix, got, tt.want)
		}
	}
}
