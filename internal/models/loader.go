// agency-gateway/internal/models/loader.go
package models

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

// validate is the shared validator instance.
var validate *validator.Validate

func init() {
	validate = validator.New()
}

// Validatable is implemented by models that need cross-field validation.
type Validatable interface {
	Validate() error
}

// LoadAndValidate loads a YAML file and validates it against its schema.
func LoadAndValidate(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("%s: empty file", path)
	}

	target, err := detectSchema(path)
	if err != nil {
		return err
	}
	if target == nil {
		return nil
	}

	if err := decodeStrict(data, target); err != nil {
		return fmt.Errorf("%s: %s", path, err)
	}

	applyDefaults(target)

	if err := validate.Struct(target); err != nil {
		return formatValidationErrors(path, err)
	}

	if v, ok := target.(Validatable); ok {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("%s: %s", path, err)
		}
	}

	return nil
}

// Load loads and validates a YAML file into the given struct pointer.
func Load(path string, target interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	if err := decodeStrict(data, target); err != nil {
		return fmt.Errorf("%s: %s", path, err)
	}

	applyDefaults(target)

	if err := validate.Struct(target); err != nil {
		return formatValidationErrors(path, err)
	}

	if v, ok := target.(Validatable); ok {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("%s: %s", path, err)
		}
	}

	return nil
}

// decodeStrict decodes YAML with KnownFields(true) to reject unknown keys.
func decodeStrict(data []byte, target interface{}) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	return decoder.Decode(target)
}

// detectSchema returns a new zero-value struct pointer for the given file.
func detectSchema(path string) (interface{}, error) {
	name := filepath.Base(path)

	switch name {
	case "org.yaml":
		return &OrgConfig{}, nil
	case "egress-domains.yaml":
		return &AgentEgressConfig{}, nil
	case "routing.yaml":
		return &RoutingConfig{}, nil
	case "principals.yaml":
		return &PrincipalsConfig{}, nil
	case "policy.yaml":
		return detectPolicySchema(path)
	case "workspace.yaml":
		return detectWorkspaceSchema(path)
	case "constraints.yaml":
		return &ConstraintsConfig{}, nil
	case "package.yaml":
		return &PackageConfig{}, nil
	case "preset.yaml":
		return &PresetConfig{}, nil
	case "mission.yaml":
		return &Mission{}, nil
	case "pack.yaml":
		return &PackConfig{}, nil
	case "connector.yaml":
		return &ConnectorConfig{}, nil
	case "agent.yaml":
		return &AgentConfig{}, nil
	default:
		return nil, nil
	}
}

// detectPolicySchema returns the appropriate policy schema based on path context.
// Files under agents/ use AgentPolicyConfig; files with a "bundle" key use PolicyConfig.
func detectPolicySchema(path string) (interface{}, error) {
	if strings.Contains(path, "/agents/") {
		return &AgentPolicyConfig{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return &AgentPolicyConfig{}, nil
	}
	var raw map[string]interface{}
	yaml.Unmarshal(data, &raw)
	if _, ok := raw["bundle"]; ok {
		return &PolicyConfig{}, nil
	}
	return &AgentPolicyConfig{}, nil
}

// detectWorkspaceSchema returns the appropriate workspace schema based on path context.
// Files under agents/ use AgentWorkspaceConfig; files with a "base" key use WorkspaceConfig.
func detectWorkspaceSchema(path string) (interface{}, error) {
	if strings.Contains(path, "/agents/") {
		return &AgentWorkspaceConfig{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return &AgentWorkspaceConfig{}, nil
	}
	var raw map[string]interface{}
	yaml.Unmarshal(data, &raw)
	if _, ok := raw["base"]; ok {
		return &WorkspaceConfig{}, nil
	}
	return &AgentWorkspaceConfig{}, nil
}

// applyDefaults sets zero-value fields to their default tag values.
func applyDefaults(target interface{}) {
	v := reflect.ValueOf(target)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fv := v.Field(i)

		def := field.Tag.Get("default")
		if def == "" {
			continue
		}

		if !fv.IsZero() {
			continue
		}

		switch fv.Kind() {
		case reflect.String:
			fv.SetString(def)
		case reflect.Int, reflect.Int64:
			if n, err := strconv.ParseInt(def, 10, 64); err == nil {
				fv.SetInt(n)
			}
		case reflect.Float64:
			if f, err := strconv.ParseFloat(def, 64); err == nil {
				fv.SetFloat(f)
			}
		case reflect.Bool:
			if b, err := strconv.ParseBool(def); err == nil {
				fv.SetBool(b)
			}
		}
	}

	// Recurse into nested struct fields.
	for i := 0; i < t.NumField(); i++ {
		fv := v.Field(i)
		if fv.Kind() == reflect.Struct {
			applyDefaults(fv.Addr().Interface())
		}
	}
}

// formatValidationErrors formats go-playground/validator errors with file context.
func formatValidationErrors(path string, err error) error {
	validationErrors, ok := err.(validator.ValidationErrors)
	if !ok {
		return fmt.Errorf("%s: %s", path, err)
	}

	var msgs []string
	for _, fe := range validationErrors {
		field := yamlFieldName(fe.StructField(), fe.Namespace())
		msg := formatFieldError(fe)
		msgs = append(msgs, fmt.Sprintf("  %s: %s", field, msg))
	}

	return fmt.Errorf("validation failed for %s:\n%s", path, strings.Join(msgs, "\n"))
}

// yamlFieldName converts a Go struct field name to its yaml tag name.
func yamlFieldName(structField, namespace string) string {
	return strings.ToLower(structField)
}

// formatFieldError produces a human-readable error for a single field.
func formatFieldError(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "required field missing"
	case "oneof":
		return fmt.Sprintf("must be one of [%s], got '%s'", fe.Param(), fe.Value())
	case "min":
		return fmt.Sprintf("must be at least %s", fe.Param())
	case "max":
		return fmt.Sprintf("must be at most %s", fe.Param())
	case "gte":
		return fmt.Sprintf("must be >= %s", fe.Param())
	case "lte":
		return fmt.Sprintf("must be <= %s", fe.Param())
	default:
		return fmt.Sprintf("failed validation '%s'", fe.Tag())
	}
}
