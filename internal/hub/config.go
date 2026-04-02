package hub

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// SecretPutter writes a single secret to the credential store.
// Callers provide this as a closure wrapping credstore.Store.Put().
type SecretPutter func(name, value string) error

// SecretDeleter removes a single secret from the credential store.
// Callers provide this as a closure wrapping credstore.Store.Delete().
type SecretDeleter func(name string) error

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

// WriteSecrets writes secret values to the encrypted credential store.
// The put function is typically a closure wrapping credstore.Store.Put().
func WriteSecrets(home, instanceName string, secrets map[string]string, put SecretPutter) error {
	if put == nil {
		return fmt.Errorf("credential store required")
	}
	for name, value := range secrets {
		if err := put(name, value); err != nil {
			return fmt.Errorf("store secret %q: %w", name, err)
		}
	}
	return nil
}

// RemoveSecrets cleans up credential store entries for an instance.
// The del function is typically a closure wrapping credstore.Store.Delete().
// Called by teardown/remove.
func RemoveSecrets(home, instanceName string, fieldNames []string, del SecretDeleter) error {
	if del == nil {
		return fmt.Errorf("credential store required")
	}
	for _, name := range fieldNames {
		del(name) // ignore errors — may not exist
	}
	return nil
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
