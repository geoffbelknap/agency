package main

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type FetchConfig struct {
	TimeoutSeconds   int    `yaml:"timeout_seconds"`
	MaxResponseBytes int64  `yaml:"max_response_bytes"`
	MaxContentBytes  int64  `yaml:"max_content_bytes"`
	UserAgent        string `yaml:"user_agent"`
	FollowRedirects  bool   `yaml:"follow_redirects"`
	MaxRedirects     int    `yaml:"max_redirects"`
}

type ContentTypeConfig struct {
	Allowed []string `yaml:"allowed"`
}

type RateLimitConfig struct {
	PerDomainRPM int `yaml:"per_domain_rpm"`
	GlobalRPM    int `yaml:"global_rpm"`
}

type CacheConfig struct {
	MaxEntries    int   `yaml:"max_entries"`
	TTLMinutes    int   `yaml:"ttl_minutes"`
	MaxEntryBytes int64 `yaml:"max_entry_bytes"`
}

type XPIAConfig struct {
	Enabled     bool `yaml:"enabled"`
	BlockOnFlag bool `yaml:"block_on_flag"`
}

type BlocklistRefreshConfig struct {
	AutoRefresh     bool   `yaml:"auto_refresh"`
	RefreshInterval string `yaml:"refresh_interval"`
}

type Config struct {
	Fetch        FetchConfig            `yaml:"fetch"`
	ContentTypes ContentTypeConfig      `yaml:"content_types"`
	RateLimits   RateLimitConfig        `yaml:"rate_limits"`
	Cache        CacheConfig            `yaml:"cache"`
	XPIA         XPIAConfig             `yaml:"xpia"`
	Blocklists   BlocklistRefreshConfig `yaml:"blocklists"`
}

func DefaultConfig() Config {
	return Config{
		Fetch: FetchConfig{
			TimeoutSeconds:   15,
			MaxResponseBytes: 2 * 1024 * 1024,
			MaxContentBytes:  100 * 1024,
			UserAgent:        "Agency/1.0 (web-fetch)",
			FollowRedirects:  true,
			MaxRedirects:     5,
		},
		ContentTypes: ContentTypeConfig{
			Allowed: []string{
				"text/html", "text/plain", "text/xml",
				"text/markdown", "text/csv",
				"application/json", "application/xml",
			},
		},
		RateLimits: RateLimitConfig{
			PerDomainRPM: 10,
			GlobalRPM:    200,
		},
		Cache: CacheConfig{
			MaxEntries:    1000,
			TTLMinutes:    15,
			MaxEntryBytes: 100 * 1024,
		},
		XPIA: XPIAConfig{
			Enabled:     true,
			BlockOnFlag: false,
		},
		Blocklists: BlocklistRefreshConfig{
			AutoRefresh:     true,
			RefreshInterval: "6h",
		},
	}
}

func (c *Config) RefreshDuration() time.Duration {
	d, err := time.ParseDuration(c.Blocklists.RefreshInterval)
	if err != nil {
		return 6 * time.Hour
	}
	return d
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
