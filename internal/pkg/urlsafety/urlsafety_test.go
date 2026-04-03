package urlsafety

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		url   string
		valid bool
	}{
		{"https://ntfy.sh/test", true},
		{"https://hooks.slack.com/services/T0/B0/abc", true},
		{"http://external.com/hook", false},
		{"ftp://evil.com", false},
		{"file:///etc/passwd", false},
		{"javascript:alert(1)", false},
		{"http://localhost:8080/test", true},
		{"http://127.0.0.1:8080/test", true},
		{"https://169.254.169.254/latest/meta-data/", false},
		{"https://10.0.0.1/internal", false},
		{"https://172.16.0.1/internal", false},
		{"https://192.168.1.1/internal", false},
		{"https://metadata.google.internal/computeMetadata/v1/", false},
		{"https://localhost/hook", false},
		{"https://127.0.0.1/hook", false},
		{"", false},
		{"not-a-url", false},
	}

	for _, tt := range tests {
		err := Validate(tt.url)
		if (err == nil) != tt.valid {
			t.Errorf("Validate(%q) error=%v, wantValid=%v", tt.url, err, tt.valid)
		}
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		host    string
		private bool
	}{
		{"169.254.169.254", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"203.0.113.1", false},
	}

	for _, tt := range tests {
		result := IsPrivateIP(tt.host)
		if result != tt.private {
			t.Errorf("IsPrivateIP(%q) = %v, want %v", tt.host, result, tt.private)
		}
	}
}
