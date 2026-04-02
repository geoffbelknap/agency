package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// ServiceCredential holds the resolved credential for a service.
type ServiceCredential struct {
	Header  string // HTTP header to set
	Value   string // Header value (the real API key)
	APIBase string // Optional: override the target API base URL
}

// ServiceDefinition represents a service definition YAML file.
// Matches the Python ServiceDefinition model in agency/models/service.py.
type ServiceDefinition struct {
	Service    string `yaml:"service"`
	APIBase    string `yaml:"api_base"`
	Credential struct {
		Header       string `yaml:"header"`
		Format       string `yaml:"format"`
		EnvVar       string `yaml:"env_var"`
		ScopedPrefix string `yaml:"scoped_prefix"`
	} `yaml:"credential"`
}

// AgentServiceGrants is the top-level structure of services.yaml.
// Matches the Python AgentServiceGrants model.
type AgentServiceGrants struct {
	Agent  string         `yaml:"agent"`
	Grants []ServiceGrant `yaml:"grants"`
}

// ServiceGrant represents a grant entry in services.yaml.
type ServiceGrant struct {
	Service   string `yaml:"service"`
	GrantedAt string `yaml:"granted_at"`
	GrantedBy string `yaml:"granted_by"`
}

// blockedHeaders are not allowed in service credential swap.
var blockedHeaders = map[string]bool{
	"host":                true,
	"transfer-encoding":  true,
	"proxy-authorization": true,
	"proxy-authenticate":  true,
	"proxy-connection":    true,
	"connection":          true,
	"content-length":      true,
	"te":                  true,
	"upgrade":             true,
}

// ServiceRegistry manages service credential lookups and swaps.
type ServiceRegistry struct {
	mu       sync.RWMutex
	services map[string]*ServiceCredential // service name -> credential
}

// NewServiceRegistry creates an empty service registry.
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		services: make(map[string]*ServiceCredential),
	}
}

// Register adds or updates a service credential.
func (sr *ServiceRegistry) Register(name string, cred *ServiceCredential) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	sr.services[name] = cred
}

// Lookup finds a service credential by name.
func (sr *ServiceRegistry) Lookup(name string) *ServiceCredential {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	return sr.services[name]
}

// LoadFromFiles loads service definitions, grants, and keys to build the registry.
func (sr *ServiceRegistry) LoadFromFiles(servicesDir, agentDir, keysFile string) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	// Load service definitions
	definitions := make(map[string]*ServiceDefinition)
	entries, err := os.ReadDir(servicesDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read services dir: %w", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(servicesDir, e.Name()))
		if err != nil {
			continue
		}
		var def ServiceDefinition
		if err := yaml.Unmarshal(data, &def); err != nil {
			continue
		}
		if def.Service != "" {
			definitions[def.Service] = &def
		}
	}

	// Load grants from services.yaml (Python AgentServiceGrants format)
	grantsFile := filepath.Join(agentDir, "services.yaml")
	var grants []ServiceGrant
	data, err := os.ReadFile(grantsFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read grants: %w", err)
	}
	if data != nil {
		var agentGrants AgentServiceGrants
		if err := yaml.Unmarshal(data, &agentGrants); err == nil {
			grants = agentGrants.Grants
		}
	}

	// Load keys from env file
	keys := loadEnvFile(keysFile)

	// Build service credentials
	sr.services = make(map[string]*ServiceCredential, len(grants))
	for _, grant := range grants {
		def, ok := definitions[grant.Service]
		if !ok {
			continue
		}

		// Look up the actual key value
		keyVal := ""
		if def.Credential.EnvVar != "" {
			keyVal = keys[def.Credential.EnvVar]
		}
		if keyVal == "" {
			continue
		}

		header := def.Credential.Header
		if header == "" {
			header = "Authorization"
		}

		// Check blocked headers
		if blockedHeaders[strings.ToLower(header)] {
			continue
		}

		// Apply format string (e.g. "Bearer {key}" -> "Bearer <actual_key>")
		value := keyVal
		if def.Credential.Format != "" {
			value = strings.Replace(def.Credential.Format, "{key}", keyVal, 1)
		}

		sr.services[grant.Service] = &ServiceCredential{
			Header:  header,
			Value:   value,
			APIBase: def.APIBase,
		}
	}

	return nil
}

// loadEnvFile reads a KEY=VALUE env file.
func loadEnvFile(path string) map[string]string {
	result := make(map[string]string)
	f, err := os.Open(path)
	if err != nil {
		return result
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			result[key] = val
		}
	}
	return result
}
