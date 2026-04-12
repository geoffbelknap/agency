package deployments

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Schema struct {
	SchemaVersion  int                               `yaml:"schema_version" json:"schema_version"`
	Deployment     SchemaDeployment                  `yaml:"deployment" json:"deployment"`
	Config         map[string]ConfigField            `yaml:"config" json:"config"`
	Credentials    map[string]CredentialField        `yaml:"credentials" json:"credentials"`
	Instances      SchemaInstances                   `yaml:"instances" json:"instances"`
	ConnectorConfig map[string]map[string]interface{} `yaml:"connector_config,omitempty" json:"connector_config,omitempty"`
}

type SchemaDeployment struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

type ConfigField struct {
	Type        string        `yaml:"type" json:"type"`
	ItemType    string        `yaml:"item_type,omitempty" json:"item_type,omitempty"`
	Required    bool          `yaml:"required,omitempty" json:"required,omitempty"`
	RequiredIf  string        `yaml:"required_if,omitempty" json:"required_if,omitempty"`
	Default     interface{}   `yaml:"default,omitempty" json:"default,omitempty"`
	Pattern     string        `yaml:"pattern,omitempty" json:"pattern,omitempty"`
	Enum        []interface{} `yaml:"enum,omitempty" json:"enum,omitempty"`
	Minimum     *int          `yaml:"minimum,omitempty" json:"minimum,omitempty"`
	Maximum     *int          `yaml:"maximum,omitempty" json:"maximum,omitempty"`
	Description string        `yaml:"description,omitempty" json:"description,omitempty"`
}

type CredentialField struct {
	Description    string `yaml:"description,omitempty" json:"description,omitempty"`
	CredstoreScope string `yaml:"credstore_scope,omitempty" json:"credstore_scope,omitempty"`
}

type SchemaInstances struct {
	Pack       SchemaInstance   `yaml:"pack" json:"pack"`
	Connectors []SchemaInstance `yaml:"connectors,omitempty" json:"connectors,omitempty"`
	Presets    []SchemaInstance `yaml:"presets,omitempty" json:"presets,omitempty"`
}

type SchemaInstance struct {
	Component string `yaml:"component" json:"component"`
	Required  bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

func LoadSchema(path string) (*Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSchema(data)
}

func ParseSchema(data []byte) (*Schema, error) {
	var schema Schema
	if err := yaml.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("parse deployment schema: %w", err)
	}
	if err := schema.Validate(); err != nil {
		return nil, err
	}
	return &schema, nil
}

func (s *Schema) Validate() error {
	if s.SchemaVersion <= 0 {
		return fmt.Errorf("schema_version is required")
	}
	if strings.TrimSpace(s.Deployment.Name) == "" {
		return fmt.Errorf("deployment.name is required")
	}
	if strings.TrimSpace(s.Instances.Pack.Component) == "" {
		return fmt.Errorf("instances.pack.component is required")
	}
	for name, field := range s.Config {
		if err := validateConfigField(name, field); err != nil {
			return err
		}
	}
	if s.Credentials == nil {
		s.Credentials = map[string]CredentialField{}
	}
	if s.Config == nil {
		s.Config = map[string]ConfigField{}
	}
	if s.ConnectorConfig == nil {
		s.ConnectorConfig = map[string]map[string]interface{}{}
	}
	return nil
}

func validateConfigField(name string, field ConfigField) error {
	switch field.Type {
	case "string", "int", "bool", "list", "object":
	default:
		return fmt.Errorf("config.%s type %q is invalid", name, field.Type)
	}
	if field.Type == "list" && field.ItemType == "" {
		return fmt.Errorf("config.%s item_type is required for list", name)
	}
	return nil
}

func (s *Schema) ApplyDefaults(config map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(config)+len(s.Config))
	for k, v := range config {
		out[k] = v
	}
	for name, field := range s.Config {
		if _, ok := out[name]; !ok && field.Default != nil {
			out[name] = field.Default
		}
	}
	return out
}

func (s *Schema) ValidateConfig(config map[string]interface{}) error {
	config = s.ApplyDefaults(config)
	for name, field := range s.Config {
		value, present := config[name]
		required := field.Required || evalRequiredIf(field.RequiredIf, config)
		if !present {
			if required {
				return fmt.Errorf("config.%s is required", name)
			}
			continue
		}
		if err := validateConfigValue(name, field, value); err != nil {
			return err
		}
	}
	return nil
}

func validateConfigValue(name string, field ConfigField, value interface{}) error {
	switch field.Type {
	case "string":
		str, ok := value.(string)
		if !ok {
			return fmt.Errorf("config.%s must be a string", name)
		}
		if field.Pattern != "" {
			re, err := regexp.Compile(field.Pattern)
			if err != nil {
				return fmt.Errorf("config.%s pattern is invalid: %w", name, err)
			}
			if !re.MatchString(str) {
				return fmt.Errorf("config.%s does not match pattern", name)
			}
		}
	case "int":
		iv, ok := toInt(value)
		if !ok {
			return fmt.Errorf("config.%s must be an int", name)
		}
		if field.Minimum != nil && iv < *field.Minimum {
			return fmt.Errorf("config.%s must be >= %d", name, *field.Minimum)
		}
		if field.Maximum != nil && iv > *field.Maximum {
			return fmt.Errorf("config.%s must be <= %d", name, *field.Maximum)
		}
	case "bool":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("config.%s must be a bool", name)
		}
	case "list":
		items, ok := value.([]interface{})
		if !ok {
			switch typed := value.(type) {
			case []string:
				items = make([]interface{}, 0, len(typed))
				for _, v := range typed {
					items = append(items, v)
				}
			default:
				return fmt.Errorf("config.%s must be a list", name)
			}
		}
		for _, item := range items {
			if err := validateConfigValue(name+"[]", ConfigField{Type: field.ItemType}, item); err != nil {
				return err
			}
		}
	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Errorf("config.%s must be an object", name)
		}
	}
	if len(field.Enum) > 0 {
		match := false
		for _, allowed := range field.Enum {
			if fmt.Sprintf("%v", allowed) == fmt.Sprintf("%v", value) {
				match = true
				break
			}
		}
		if !match {
			return fmt.Errorf("config.%s must be one of %v", name, field.Enum)
		}
	}
	return nil
}

func evalRequiredIf(expr string, config map[string]interface{}) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	op := "=="
	if strings.Contains(expr, "!=") {
		op = "!="
	}
	parts := strings.SplitN(expr, op, 2)
	if len(parts) != 2 {
		return false
	}
	key := strings.TrimSpace(parts[0])
	want := strings.Trim(strings.TrimSpace(parts[1]), `"`)
	got, ok := config[key]
	if !ok {
		return false
	}
	switch op {
	case "!=":
		return fmt.Sprintf("%v", got) != want
	default:
		return fmt.Sprintf("%v", got) == want
	}
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	default:
		return 0, false
	}
}
