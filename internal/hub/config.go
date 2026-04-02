package hub

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/pkg/envfile"
)

// ConfigField describes a single configuration field in a hub component's config schema.
type ConfigField struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Required    bool   `yaml:"required" json:"required"`
	Secret      bool   `yaml:"secret" json:"secret"`
	Source      string `yaml:"source,omitempty" json:"source,omitempty"` // credential, literal, env, vault
	Default     string `yaml:"default,omitempty" json:"default,omitempty"`
}

// ConfigSchema holds the parsed config section from a component YAML.
type ConfigSchema struct {
	Fields []ConfigField `yaml:"config"`
}

// ConfigValues holds the resolved configuration for an installed component instance.
type ConfigValues struct {
	Instance        string            `yaml:"instance"`
	ID              string            `yaml:"id"`
	SourceComponent string            `yaml:"source_component"`
	ConfiguredAt    string            `yaml:"configured_at"`
	Values          map[string]string `yaml:"values"`
}

// ParseConfigSchema parses the config: section from a component YAML document.
// Other fields (name, kind, source, routes, etc.) are ignored.
func ParseConfigSchema(data []byte) (*ConfigSchema, error) {
	var schema ConfigSchema
	if err := yaml.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("parsing config schema: %w", err)
	}
	if schema.Fields == nil {
		schema.Fields = []ConfigField{}
	}
	return &schema, nil
}

// Validate returns all required fields that are missing from values.
// A field with a non-empty Default is not considered missing even if absent from values.
func (s *ConfigSchema) Validate(values map[string]string) []ConfigField {
	var missing []ConfigField
	for _, field := range s.Fields {
		if !field.Required {
			continue
		}
		if _, ok := values[field.Name]; ok {
			continue
		}
		if field.Default != "" {
			continue
		}
		missing = append(missing, field)
	}
	if missing == nil {
		missing = []ConfigField{}
	}
	return missing
}

// ResolvePlaceholders replaces all ${key} occurrences in template with the
// corresponding value from values. Unrecognized placeholders are left as-is.
func ResolvePlaceholders(template string, values map[string]string) string {
	result := template
	for k, v := range values {
		result = strings.ReplaceAll(result, "${"+k+"}", v)
	}
	return result
}

// placeholderRe matches ${...} placeholders.
var placeholderRe = regexp.MustCompile(`\$\{([^}]+)\}`)

// FindUnresolvedPlaceholders returns the names of all ${...} placeholders
// found in template (the inner key, not the full ${key} syntax).
func FindUnresolvedPlaceholders(template string) []string {
	matches := placeholderRe.FindAllStringSubmatch(template, -1)
	if len(matches) == 0 {
		return []string{}
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m[1])
	}
	return names
}

// WriteConfig writes a ConfigValues to config.yaml in instDir with 0600 permissions.
func WriteConfig(instDir string, cv *ConfigValues) error {
	data, err := yaml.Marshal(cv)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	path := filepath.Join(instDir, "config.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config.yaml: %w", err)
	}
	return nil
}

// ReadConfig reads config.yaml from instDir and returns the parsed ConfigValues.
func ReadConfig(instDir string) (*ConfigValues, error) {
	path := filepath.Join(instDir, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config.yaml: %w", err)
	}
	var cv ConfigValues
	if err := yaml.Unmarshal(data, &cv); err != nil {
		return nil, fmt.Errorf("parsing config.yaml: %w", err)
	}
	return &cv, nil
}

// WriteSecrets writes secret values to the single key store for egress swap.
// Each secret is stored as key=realvalue in infrastructure/.service-keys.env.
func WriteSecrets(home, instanceName string, secrets map[string]string) error {
	svcFile := filepath.Join(home, "infrastructure", ".service-keys.env")
	os.MkdirAll(filepath.Dir(svcFile), 0755)

	entries := make(map[string]string, len(secrets))
	for k, v := range secrets {
		entries[k] = v
	}

	if err := envfile.Upsert(svcFile, entries); err != nil {
		return fmt.Errorf("writing service keys: %w", err)
	}
	return nil
}

// RemoveSecrets cleans up credential store entries for an instance.
// Removes entries by key name from infrastructure/.service-keys.env.
// Called by teardown/remove.
func RemoveSecrets(home, instanceName string, fieldNames []string) error {
	svcFile := filepath.Join(home, "infrastructure", ".service-keys.env")

	if err := removeEnvFileKeys(svcFile, fieldNames); err != nil {
		return fmt.Errorf("removing service keys: %w", err)
	}
	return nil
}


// removeEnvFilePrefix reads path (if it exists) and removes all lines whose KEY
// starts with prefix. If the file does not exist the call is a no-op. Writes 0600.
func removeEnvFilePrefix(path, prefix string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			lines = append(lines, trimmed)
			continue
		}
		eqIdx := strings.IndexByte(trimmed, '=')
		if eqIdx >= 0 && strings.HasPrefix(trimmed[:eqIdx], prefix) {
			continue
		}
		lines = append(lines, trimmed)
	}

	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0600)
}

// removeEnvFileKeys reads path (if it exists) and removes all lines whose KEY
// matches one of the provided keys. If the file does not exist the call is a no-op. Writes 0600.
func removeEnvFileKeys(path string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	keySet := make(map[string]bool, len(keys))
	for _, k := range keys {
		keySet[k] = true
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			lines = append(lines, trimmed)
			continue
		}
		eqIdx := strings.IndexByte(trimmed, '=')
		if eqIdx >= 0 && keySet[trimmed[:eqIdx]] {
			continue
		}
		lines = append(lines, trimmed)
	}

	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0600)
}

// SplitSecrets separates values into non-secret config and secrets.
// For secret fields, the config map gets a scoped reference (@scoped:{instanceName}-{fieldName})
// and the real value goes into the secrets map. Non-secret fields pass directly to config.
func (s *ConfigSchema) SplitSecrets(values map[string]string, instanceName string) (config map[string]string, secrets map[string]string) {
	config = make(map[string]string)
	secrets = make(map[string]string)

	// Build a lookup of which field names are secret.
	secretFields := make(map[string]bool, len(s.Fields))
	for _, f := range s.Fields {
		if f.Secret {
			secretFields[f.Name] = true
		}
	}

	for k, v := range values {
		if secretFields[k] {
			config[k] = "@scoped:" + instanceName + "-" + k
			secrets[k] = v
		} else {
			config[k] = v
		}
	}
	return config, secrets
}
