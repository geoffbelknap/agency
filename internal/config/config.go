package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds gateway configuration derived from the Agency home directory.
type Config struct {
	Home          string               // ~/.agency
	HMACKey       []byte               // 32-byte HMAC signing key
	GatewayAddr   string               // listen address, default 127.0.0.1:8200
	Token         string               // auth token for gateway API (full access)
	EgressToken   string               // scoped token for egress proxy (credential resolve only)
	Version       string               // build version (set by ldflags, e.g. "0.1.0")
	BuildID       string               // content-aware build ID: {short_commit} or {short_commit}-dirty
	SourceDir     string               // repo source tree root (agency_core/), empty for release installs
	Notifications []NotificationConfig // outbound notification destinations
	ConfigVars    map[string]string    // platform config values from config.yaml (LC_ORG_ID, etc.)
}

// NotificationConfig defines an outbound notification destination.
type NotificationConfig struct {
	Name    string            `yaml:"name"`
	Type    string            `yaml:"type"`    // ntfy, webhook
	URL     string            `yaml:"url"`
	Events  []string          `yaml:"events"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

// configFile mirrors the fields we care about in config.yaml.
type configFile struct {
	Token         string               `yaml:"token"`
	EgressToken   string               `yaml:"egress_token,omitempty"`
	HMACKey       string               `yaml:"hmac_key,omitempty"`
	GatewayAddr   string               `yaml:"gateway_addr,omitempty"`
	SourceDir     string               `yaml:"source_dir,omitempty"`
	Notifications []NotificationConfig `yaml:"notifications,omitempty"`
	ConfigVars    map[string]string    `yaml:"config,omitempty"` // platform config values (LC_ORG_ID, etc.)
}

// Load returns the gateway config, resolving the agency home directory.
func Load() *Config {
	home := os.Getenv("AGENCY_HOME")
	if home == "" {
		userHome, _ := os.UserHomeDir()
		home = filepath.Join(userHome, ".agency")
	}

	cfg := &Config{
		Home:        home,
		GatewayAddr: "127.0.0.1:8200",
	}

	configPath := filepath.Join(home, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		// config.yaml doesn't exist yet (first run before agency init); use defaults.
		return cfg
	}

	var cf configFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		// Unparseable config — use defaults.
		return cfg
	}

	if cf.GatewayAddr != "" {
		cfg.GatewayAddr = cf.GatewayAddr
	}
	if cf.SourceDir != "" {
		cfg.SourceDir = cf.SourceDir
	}
	cfg.Notifications = cf.Notifications
	cfg.Token = cf.Token
	cfg.EgressToken = cf.EgressToken
	cfg.ConfigVars = cf.ConfigVars

	if cf.HMACKey != "" {
		decoded, err := hex.DecodeString(cf.HMACKey)
		if err == nil {
			cfg.HMACKey = decoded
		}
	}

	// Auto-detect source_dir for dev mode: if not configured, look for
	// images/ relative to the running binary or known checkout paths.
	if cfg.SourceDir == "" {
		cfg.SourceDir = detectSourceDir()
	}

	if len(cfg.HMACKey) == 0 {
		// Generate a fresh 32-byte key and persist it.
		key := make([]byte, 32)
		if _, err := rand.Read(key); err == nil {
			cfg.HMACKey = key
			cf.HMACKey = hex.EncodeToString(key)
			if out, err := yaml.Marshal(&cf); err == nil {
				_ = os.WriteFile(configPath, out, 0600)
			}
		}
	}

	return cfg
}

// detectSourceDir attempts to find the repo root for dev-mode image builds.
// Checks the binary's parent directories for an images/ subdirectory.
// Returns empty string for release installs (no source tree).
func detectSourceDir() string {
	// Find the binary location
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}

	// Walk up from the binary looking for images/
	dir := filepath.Dir(exe)
	for i := 0; i < 6; i++ {
		if info, err := os.Stat(filepath.Join(dir, "images")); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// ConfigPath returns the path to config.yaml.
func (c *Config) ConfigPath() string {
	return filepath.Join(c.Home, "config.yaml")
}

// AgentsDir returns the path to the agents directory.
func (c *Config) AgentsDir() string {
	return filepath.Join(c.Home, "agents")
}

// AuditDir returns the path to the audit log directory.
func (c *Config) AuditDir() string {
	return filepath.Join(c.Home, "audit")
}

// PresetsDir returns the path to the presets directory.
func (c *Config) PresetsDir() string {
	return filepath.Join(c.Home, "presets")
}

// BudgetDir returns the path to the budget state directory.
func (c *Config) BudgetDir() string {
	return filepath.Join(c.Home, "budget")
}
