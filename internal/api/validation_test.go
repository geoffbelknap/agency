package api

import (
	"net/http/httptest"
	"testing"
)

func TestRequireName(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		// Valid names
		{"a", true},
		{"ab", true},
		{"my-agent", true},
		{"agent-01", true},
		{"a1b2c3", true},
		{"x", true},

		// Path traversal attacks
		{"..", false},
		{"../etc/passwd", false},
		{"..%2Fevil", false},
		{"../../credentials/store.enc", false},

		// Invalid characters
		{"", false},
		{".", false},
		{"My-Agent", false},
		{"agent_01", false},
		{"agent 01", false},
		{"agent/evil", false},
		{"agent\\evil", false},
		{"-leading-hyphen", false},
		{"trailing-hyphen-", false},

		// Length limit
		{"a234567890123456789012345678901234567890123456789012345678901234", true},  // 64 chars
		{"a2345678901234567890123456789012345678901234567890123456789012345", false}, // 65 chars
	}

	for _, tt := range tests {
		w := httptest.NewRecorder()
		name, ok := requireName(w, tt.input)
		if ok != tt.valid {
			t.Errorf("requireName(%q) = %v, want %v", tt.input, ok, tt.valid)
		}
		if ok && name != tt.input {
			t.Errorf("requireName(%q) returned %q, want same", tt.input, name)
		}
		if !ok && w.Code != 400 {
			t.Errorf("requireName(%q) status = %d, want 400", tt.input, w.Code)
		}
	}
}
