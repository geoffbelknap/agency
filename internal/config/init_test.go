package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRunInit_NotificationConfig(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	err := RunInit(InitOptions{
		NotifyURL: "https://ntfy.sh/my-agency-alerts",
	})
	if err != nil {
		t.Fatalf("RunInit failed: %v", err)
	}

	// notifications.yaml should exist with the configured entry
	notifData, err := os.ReadFile(filepath.Join(tmpDir, ".agency", "notifications.yaml"))
	if err != nil {
		t.Fatalf("failed to read notifications.yaml: %v", err)
	}

	var notifConfigs []NotificationConfig
	if err := yaml.Unmarshal(notifData, &notifConfigs); err != nil {
		t.Fatalf("failed to parse notifications.yaml: %v", err)
	}

	if len(notifConfigs) == 0 {
		t.Fatal("expected at least one notification config")
	}

	nc := notifConfigs[0]
	if nc.Name != "operator-alerts" {
		t.Errorf("expected name=operator-alerts, got %v", nc.Name)
	}
	if nc.Type != "ntfy" {
		t.Errorf("expected type=ntfy, got %v", nc.Type)
	}
	if nc.URL != "https://ntfy.sh/my-agency-alerts" {
		t.Errorf("expected url to match, got %v", nc.URL)
	}

	// config.yaml should NOT contain a notifications key
	cfgData, err := os.ReadFile(filepath.Join(tmpDir, ".agency", "config.yaml"))
	if err != nil {
		t.Fatalf("failed to read config.yaml: %v", err)
	}
	var cfg map[string]interface{}
	yaml.Unmarshal(cfgData, &cfg)
	if _, ok := cfg["notifications"]; ok {
		t.Error("config.yaml should not contain notifications key — notifications belong in notifications.yaml")
	}
}

func TestRunInit_NoNotificationURL(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	err := RunInit(InitOptions{})
	if err != nil {
		t.Fatalf("RunInit failed: %v", err)
	}

	// notifications.yaml should not exist
	notifPath := filepath.Join(tmpDir, ".agency", "notifications.yaml")
	if _, err := os.Stat(notifPath); err == nil {
		t.Error("expected notifications.yaml to not exist when no URL provided")
	}

	// config.yaml should not have notifications key either
	data, err := os.ReadFile(filepath.Join(tmpDir, ".agency", "config.yaml"))
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var cfg map[string]interface{}
	yaml.Unmarshal(data, &cfg)

	if _, ok := cfg["notifications"]; ok {
		t.Error("expected no notifications key when URL not provided")
	}
}

func TestValidateOperatorName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		// Valid names
		{"alice", false},
		{"Alice Smith", false},
		{"ops.team", false},
		{"security_ops", false},
		{"team-alpha", false},
		{"A", false},
		{"Ab", false},
		{"a1b2c3", false},

		// Invalid: empty
		{"", true},

		// Invalid: starts with dash
		{"-badstart", true},

		// Invalid: ends with dash
		{"badend-", true},

		// Invalid: starts with space
		{" leadingspace", true},

		// Invalid: YAML-dangerous characters
		{"name:value", true},
		{"name{obj}", true},
		{"name[arr]", true},
		{"name#comment", true},
		{"name&anchor", true},
		{"name*alias", true},
		{"name!tag", true},
		{"name|literal", true},
		{"name>folded", true},
		{"name'quoted", true},
		{`name"double`, true},
		{"name%pct", true},
		{"name@at", true},
		{"name`tick", true},

		// Invalid: path separators
		{"name/traversal", true},
		{"name\\traversal", true},

		// Invalid: exceeds 64 characters
		{"a" + string(make([]byte, 64)), true},
	}
	for _, tt := range tests {
		err := ValidateOperatorName(tt.name)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateOperatorName(%q) error=%v, wantErr=%v", tt.name, err, tt.wantErr)
		}
	}
}

func TestRunInit_InvalidOperatorName(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	err := RunInit(InitOptions{
		Operator: "bad:name",
	})
	if err == nil {
		t.Fatal("expected error for invalid operator name, got nil")
	}
}

func TestRunInit_OperatorWrittenToConfig(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	err := RunInit(InitOptions{
		Operator: "alice",
	})
	if err != nil {
		t.Fatalf("RunInit failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, ".agency", "config.yaml"))
	if err != nil {
		t.Fatalf("failed to read config.yaml: %v", err)
	}

	var cfg map[string]interface{}
	yaml.Unmarshal(data, &cfg)

	op, ok := cfg["operator"]
	if !ok {
		t.Fatal("expected operator key in config.yaml")
	}
	if op != "alice" {
		t.Errorf("expected operator=alice, got %v", op)
	}
}

func TestDetectNotificationType(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://ntfy.sh/my-topic", "ntfy"},
		{"https://ntfy.example.com/alerts", "ntfy"},
		{"https://hooks.slack.com/services/T00/B00/xxx", "webhook"},
		{"https://example.com/webhook", "webhook"},
	}
	for _, tt := range tests {
		got := detectNotificationType(tt.url)
		if got != tt.want {
			t.Errorf("detectNotificationType(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}
