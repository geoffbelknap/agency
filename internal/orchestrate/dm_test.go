package orchestrate

import (
	"testing"

	"github.com/geoffbelknap/agency/internal/models"
)

func TestDMChannelName(t *testing.T) {
	tests := []struct {
		agent string
		want  string
	}{
		{"scout", "dm-scout"},
		{"my-agent", "dm-my-agent"},
		{"a", "dm-a"},
	}
	for _, tt := range tests {
		got := models.DMChannelName(tt.agent)
		if got != tt.want {
			t.Errorf("DMChannelName(%q) = %q, want %q", tt.agent, got, tt.want)
		}
	}
}

func TestIsDMChannel(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"dm-scout", true},
		{"dm-", true},
		{"general", false},
		{"team-alpha", false},
		{"", false},
	}
	for _, tt := range tests {
		got := models.IsDMChannel(tt.name)
		if got != tt.want {
			t.Errorf("IsDMChannel(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
