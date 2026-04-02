package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// ServiceCredential holds the resolved credential for a service.
type ServiceCredential struct {
	Header        string            // HTTP header to set
	Value         string            // Header value (the real API key)
	APIBase       string            // Optional: override the target API base URL
	ToolScopes    map[string]string // tool name → required scope
	AllowedScopes map[string]bool   // scopes this agent has for this service
}

// ServiceToolDef represents a tool entry in a service definition (for scope parsing).
type ServiceToolDef struct {
	Name  string `yaml:"name"`
	Scope string `yaml:"scope"`
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
	Tools []ServiceToolDef `yaml:"tools"`
}

// AgentServiceGrants is the top-level structure of services.yaml.
// Matches the Python AgentServiceGrants model.
type AgentServiceGrants struct {
	Agent  string         `yaml:"agent"`
	Grants []ServiceGrant `yaml:"grants"`
}

// ServiceGrant represents a grant entry in services.yaml.
type ServiceGrant struct {
	Service       string   `yaml:"service"`
	GrantedAt     string   `yaml:"granted_at"`
	GrantedBy     string   `yaml:"granted_by"`
	AllowedScopes []string `yaml:"allowed_scopes"`
}

// blockedHeaders are not allowed in service credential swap.
var blockedHeaders = map[string]bool{
	"host":                true,
	"transfer-encoding":  true,
	"connection":          true,
	"content-length":      true,
	"te":                  true,
	"upgrade":             true,
	"proxy-authorization": true,
	"proxy-authenticate":  true,
	"proxy-connection":    true,
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

// CheckScope validates that the agent has the required scope for the given tool.
// Returns ("", true) if no scope is required or scope is allowed.
// Returns (requiredScope, false) if scope check fails.
func (sr *ServiceRegistry) CheckScope(service, toolName string) (string, bool) {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	cred, ok := sr.services[service]
	if !ok {
		return "", false // service not found
	}
	scope, hasScope := cred.ToolScopes[toolName]
	if !hasScope || scope == "" {
		return "", true // no scope annotation — allow (backward compat)
	}
	if len(cred.AllowedScopes) == 0 {
		return "", true // no scope restrictions on grant — allow all (backward compat)
	}
	if cred.AllowedScopes[scope] {
		return "", true // scope allowed
	}
	return scope, false // scope denied
}

// LoadFromFiles loads service definitions and grants to build the registry.
// The enforcer only needs service metadata for scope checking — it does not
// need real credential values (those are injected by the egress proxy).
func (sr *ServiceRegistry) LoadFromFiles(servicesDir, agentDir string) error {
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

	// Build service credentials — the enforcer uses these for scope checks
	// and routing metadata only. Real key values are never needed here;
	// credential injection is handled by the egress proxy.
	sr.services = make(map[string]*ServiceCredential, len(grants))
	for _, grant := range grants {
		def, ok := definitions[grant.Service]
		if !ok {
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

		// Build tool→scope mapping from service definition
		toolScopes := make(map[string]string)
		for _, t := range def.Tools {
			if t.Scope != "" {
				toolScopes[t.Name] = t.Scope
			}
		}

		// Build allowed scopes set from grant
		allowedScopes := make(map[string]bool)
		for _, s := range grant.AllowedScopes {
			allowedScopes[s] = true
		}

		sr.services[grant.Service] = &ServiceCredential{
			Header:        header,
			Value:         "enforcer-scope-only", // placeholder — enforcer never injects credentials
			APIBase:       def.APIBase,
			ToolScopes:    toolScopes,
			AllowedScopes: allowedScopes,
		}
	}

	return nil
}

