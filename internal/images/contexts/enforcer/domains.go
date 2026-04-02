package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// DomainGate controls which domains the agent can access.
type DomainGate struct {
	mu     sync.RWMutex
	mode   string            // "allowlist" or "denylist"
	list   map[string]bool   // domain -> present
	wilds  []string          // wildcard patterns like "*.example.com"
	bypass map[string]bool   // infrastructure domains that always pass
}

// DomainEntry handles both plain strings and objects with a "domain" field.
type DomainEntry struct {
	Domain string
}

func (d *DomainEntry) UnmarshalYAML(value *yaml.Node) error {
	// Try plain string first
	if value.Kind == yaml.ScalarNode {
		d.Domain = value.Value
		return nil
	}
	// Try object with "domain" field
	if value.Kind == yaml.MappingNode {
		var obj struct {
			Domain string `yaml:"domain"`
		}
		if err := value.Decode(&obj); err != nil {
			return err
		}
		d.Domain = obj.Domain
		return nil
	}
	return fmt.Errorf("expected string or mapping, got %v", value.Kind)
}

// EgressDomainsConfig represents egress-domains.yaml.
type EgressDomainsConfig struct {
	Mode    string        `yaml:"mode"`
	Domains []DomainEntry `yaml:"domains"`
}

// infrastructureDomains always bypass domain gating.
var infrastructureDomains = map[string]bool{
	"egress":    true,
	"enforcer":  true,
	"localhost": true,
	"127.0.0.1": true,
}

// NewDomainGate creates a domain gate. Default mode is denylist with empty list
// (everything allowed except infrastructure bypass).
func NewDomainGate() *DomainGate {
	return &DomainGate{
		mode:   "denylist",
		list:   make(map[string]bool),
		bypass: infrastructureDomains,
	}
}

// LoadFromFile loads domain configuration from an egress-domains.yaml file.
func (dg *DomainGate) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read egress domains: %w", err)
	}
	var config EgressDomainsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parse egress domains: %w", err)
	}

	dg.mu.Lock()
	defer dg.mu.Unlock()

	if config.Mode != "" {
		dg.mode = config.Mode
	}
	dg.list = make(map[string]bool, len(config.Domains))
	dg.wilds = nil
	for _, entry := range config.Domains {
		d := strings.ToLower(entry.Domain)
		if d == "" {
			continue
		}
		if strings.HasPrefix(d, "*.") {
			dg.wilds = append(dg.wilds, d[1:]) // store as ".example.com"
		} else {
			dg.list[d] = true
		}
	}
	return nil
}

// Allowed returns true if the domain is allowed by the gate.
func (dg *DomainGate) Allowed(domain string) bool {
	// Strip port if present
	if idx := strings.LastIndex(domain, ":"); idx != -1 {
		domain = domain[:idx]
	}
	domain = strings.ToLower(domain)

	// Infrastructure domains always bypass
	if dg.bypass[domain] {
		return true
	}

	dg.mu.RLock()
	defer dg.mu.RUnlock()

	inList := dg.list[domain] || dg.matchWild(domain)

	switch dg.mode {
	case "allowlist":
		return inList
	case "denylist":
		return !inList
	default:
		// Unknown mode, fail closed
		return false
	}
}

// matchWild checks if domain matches any wildcard pattern.
// Must be called with at least a read lock held.
func (dg *DomainGate) matchWild(domain string) bool {
	for _, suffix := range dg.wilds {
		if strings.HasSuffix(domain, suffix) {
			return true
		}
	}
	return false
}
