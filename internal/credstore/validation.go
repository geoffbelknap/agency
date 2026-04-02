package credstore

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// placeholderRe matches ${...} placeholders in protocol config values.
var placeholderRe = regexp.MustCompile(`\$\{([^}]+)\}`)

// allowedPlaceholders is the fixed allowlist for ${...} expansion.
var allowedPlaceholders = map[string]bool{
	"credential": true,
}

// ValidateScopes checks the credential's ExternalScopes against the agent
// preset's declared scopes. Returns warnings for excess scopes (key has
// scopes agent doesn't need) and missing scopes (agent needs scopes key
// doesn't have). Only applies when scope is "agent:{name}".
// home is the ~/.agency root directory.
func ValidateScopes(entry Entry, home string) []Warning {
	if len(entry.Metadata.ExternalScopes) == 0 {
		return nil
	}

	// Extract agent name from scope.
	if !strings.HasPrefix(entry.Metadata.Scope, "agent:") {
		return nil
	}
	agentName := strings.TrimPrefix(entry.Metadata.Scope, "agent:")

	declaredScopes := loadAgentDeclaredScopes(agentName, home)
	if len(declaredScopes) == 0 {
		return nil // No preset scopes declared — nothing to compare.
	}

	declared := make(map[string]bool, len(declaredScopes))
	for _, s := range declaredScopes {
		declared[s] = true
	}

	credScopes := make(map[string]bool, len(entry.Metadata.ExternalScopes))
	for _, s := range entry.Metadata.ExternalScopes {
		credScopes[s] = true
	}

	var warnings []Warning

	// Excess: credential has scopes the agent doesn't need.
	for _, s := range entry.Metadata.ExternalScopes {
		if !declared[s] {
			warnings = append(warnings, Warning{
				Field:   "external_scopes",
				Message: fmt.Sprintf("excess scope %q: credential has it but agent %q does not declare it", s, agentName),
			})
		}
	}

	// Missing: agent needs scopes the credential doesn't have.
	for _, s := range declaredScopes {
		if !credScopes[s] {
			warnings = append(warnings, Warning{
				Field:   "external_scopes",
				Message: fmt.Sprintf("missing scope %q: agent %q declares it but credential does not have it", s, agentName),
			})
		}
	}

	return warnings
}

// loadAgentDeclaredScopes reads the agent's preset YAML and returns the
// union of scopes.required and scopes.optional. Uses direct YAML parsing
// to avoid circular imports with the models package.
// home is the ~/.agency root directory.
func loadAgentDeclaredScopes(agentName, home string) []string {
	// Try agent.yaml to find preset reference.
	agentYAML := filepath.Join(home, "agents", agentName, "agent.yaml")
	presetName := ""
	if data, err := os.ReadFile(agentYAML); err == nil {
		var agent struct {
			Preset string `yaml:"preset"`
		}
		if yaml.Unmarshal(data, &agent) == nil && agent.Preset != "" {
			presetName = agent.Preset
		}
	}

	if presetName == "" {
		return nil
	}

	// Read preset YAML.
	presetPath := filepath.Join(home, "hub-cache", "default", "presets", presetName, "preset.yaml")
	data, err := os.ReadFile(presetPath)
	if err != nil {
		return nil
	}

	var preset struct {
		Scopes struct {
			Required []string `yaml:"required"`
			Optional []string `yaml:"optional"`
		} `yaml:"scopes"`
	}
	if err := yaml.Unmarshal(data, &preset); err != nil {
		return nil
	}

	all := make([]string, 0, len(preset.Scopes.Required)+len(preset.Scopes.Optional))
	all = append(all, preset.Scopes.Required...)
	all = append(all, preset.Scopes.Optional...)
	return all
}

// ValidateDependencies checks that each item in entry.Requires exists in
// the platform config files (~/.agency/.env and ~/.agency/config.yaml).
func ValidateDependencies(entry Entry, configDir string) []Warning {
	if len(entry.Metadata.Requires) == 0 {
		return nil
	}

	known := loadConfigKeys(configDir)

	var warnings []Warning
	for _, dep := range entry.Metadata.Requires {
		if !known[dep] {
			warnings = append(warnings, Warning{
				Field:   "requires",
				Message: fmt.Sprintf("dependency %q is not set in platform config", dep),
			})
		}
	}
	return warnings
}

// loadConfigKeys reads keys from .env and config.yaml in configDir.
func loadConfigKeys(configDir string) map[string]bool {
	keys := make(map[string]bool)

	// Read .env (KEY=VALUE lines).
	envPath := filepath.Join(configDir, ".env")
	if data, err := os.ReadFile(envPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if idx := strings.IndexByte(line, '='); idx > 0 {
				keys[strings.TrimSpace(line[:idx])] = true
			}
		}
	}

	// Read config.yaml (top-level keys).
	cfgPath := filepath.Join(configDir, "config.yaml")
	if data, err := os.ReadFile(cfgPath); err == nil {
		var cfg map[string]any
		if yaml.Unmarshal(data, &cfg) == nil {
			for k := range cfg {
				keys[k] = true
			}
		}
	}

	return keys
}

// ValidateProtocolConfig validates the entry's protocol configuration.
// For jwt-exchange, it checks that token_url domain is in allowedDomains.
// It also validates that ${...} placeholders only use the fixed allowlist:
// ${credential} and ${config:VARNAME}.
func ValidateProtocolConfig(entry Entry, allowedDomains []string) error {
	if len(entry.Metadata.ProtocolConfig) == 0 {
		return nil
	}

	// Check ${...} placeholders in all string values.
	for key, val := range entry.Metadata.ProtocolConfig {
		strVal, ok := val.(string)
		if !ok {
			// Check nested maps (e.g., token_params).
			if m, ok := val.(map[string]any); ok {
				for nk, nv := range m {
					if s, ok := nv.(string); ok {
						if err := validatePlaceholders(fmt.Sprintf("protocol_config.%s.%s", key, nk), s); err != nil {
							return err
						}
					}
				}
			}
			continue
		}
		if err := validatePlaceholders(fmt.Sprintf("protocol_config.%s", key), strVal); err != nil {
			return err
		}
	}

	// For jwt-exchange, validate token_url domain.
	if entry.Metadata.Protocol == ProtocolJWTExchange {
		tokenURL, _ := entry.Metadata.ProtocolConfig["token_url"].(string)
		if tokenURL == "" {
			return fmt.Errorf("jwt-exchange protocol requires token_url in protocol_config")
		}
		parsed, err := url.Parse(tokenURL)
		if err != nil {
			return fmt.Errorf("invalid token_url %q: %w", tokenURL, err)
		}
		domain := strings.ToLower(parsed.Hostname())
		if len(allowedDomains) > 0 && !domainAllowed(domain, allowedDomains) {
			return fmt.Errorf("token_url domain %q is not in allowed domains", domain)
		}
	}

	return nil
}

// validatePlaceholders checks that all ${...} references in s use the allowlist.
func validatePlaceholders(field, s string) error {
	matches := placeholderRe.FindAllStringSubmatch(s, -1)
	for _, match := range matches {
		placeholder := match[1]
		if allowedPlaceholders[placeholder] {
			continue
		}
		if strings.HasPrefix(placeholder, "config:") {
			continue // ${config:VARNAME} is allowed.
		}
		// Allow ${VARNAME} (env-style) — resolved from process environment at
		// runtime. This is the existing format in jwt-swap.yaml configs.
		if placeholder == strings.ToUpper(placeholder) && !strings.Contains(placeholder, " ") {
			continue
		}
		return fmt.Errorf("field %s: disallowed placeholder ${%s}; only ${credential}, ${config:VARNAME}, and ${ENV_VAR} are allowed", field, placeholder)
	}
	return nil
}

// domainAllowed checks if domain matches any entry in allowedDomains.
func domainAllowed(domain string, allowedDomains []string) bool {
	for _, d := range allowedDomains {
		if strings.ToLower(d) == domain {
			return true
		}
	}
	return false
}
