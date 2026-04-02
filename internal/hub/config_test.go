package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfigSchema(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		data := []byte(`
name: my-connector
kind: connector
config:
  - name: api_key
    description: API key for the service
    required: true
    secret: true
    source: credential
  - name: base_url
    description: Base URL
    required: false
    default: https://api.example.com
`)
		schema, err := ParseConfigSchema(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(schema.Fields) != 2 {
			t.Fatalf("expected 2 fields, got %d", len(schema.Fields))
		}
		if schema.Fields[0].Name != "api_key" {
			t.Errorf("expected first field name 'api_key', got %q", schema.Fields[0].Name)
		}
		if !schema.Fields[0].Required {
			t.Error("expected api_key to be required")
		}
		if !schema.Fields[0].Secret {
			t.Error("expected api_key to be secret")
		}
		if schema.Fields[0].Source != "credential" {
			t.Errorf("expected source 'credential', got %q", schema.Fields[0].Source)
		}
		if schema.Fields[1].Default != "https://api.example.com" {
			t.Errorf("expected default 'https://api.example.com', got %q", schema.Fields[1].Default)
		}
	})

	t.Run("empty config section", func(t *testing.T) {
		data := []byte(`
name: my-connector
kind: connector
config: []
`)
		schema, err := ParseConfigSchema(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(schema.Fields) != 0 {
			t.Errorf("expected 0 fields, got %d", len(schema.Fields))
		}
	})

	t.Run("missing config section", func(t *testing.T) {
		data := []byte(`
name: my-connector
kind: connector
description: no config here
`)
		schema, err := ParseConfigSchema(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(schema.Fields) != 0 {
			t.Errorf("expected 0 fields, got %d", len(schema.Fields))
		}
	})
}

func TestValidate(t *testing.T) {
	schema := &ConfigSchema{
		Fields: []ConfigField{
			{Name: "api_key", Required: true, Secret: true},
			{Name: "region", Required: true, Default: "us-east-1"},
			{Name: "timeout", Required: false},
		},
	}

	t.Run("missing required", func(t *testing.T) {
		missing := schema.Validate(map[string]string{})
		if len(missing) != 1 {
			t.Fatalf("expected 1 missing field, got %d", len(missing))
		}
		if missing[0].Name != "api_key" {
			t.Errorf("expected missing field 'api_key', got %q", missing[0].Name)
		}
	})

	t.Run("defaults fill in", func(t *testing.T) {
		// region has a default so it's not missing even without a value
		missing := schema.Validate(map[string]string{"api_key": "secret123"})
		if len(missing) != 0 {
			t.Errorf("expected 0 missing fields, got %d: %v", len(missing), missing)
		}
	})

	t.Run("all provided", func(t *testing.T) {
		missing := schema.Validate(map[string]string{
			"api_key": "secret123",
			"region":  "eu-west-1",
			"timeout": "30",
		})
		if len(missing) != 0 {
			t.Errorf("expected 0 missing fields, got %d", len(missing))
		}
	})

	t.Run("empty schema", func(t *testing.T) {
		empty := &ConfigSchema{}
		missing := empty.Validate(map[string]string{})
		if len(missing) != 0 {
			t.Errorf("expected 0 missing fields, got %d", len(missing))
		}
	})
}

func TestResolvePlaceholders(t *testing.T) {
	t.Run("basic substitution", func(t *testing.T) {
		result := ResolvePlaceholders("Hello ${name}!", map[string]string{"name": "World"})
		if result != "Hello World!" {
			t.Errorf("expected 'Hello World!', got %q", result)
		}
	})

	t.Run("multiple placeholders", func(t *testing.T) {
		result := ResolvePlaceholders("${greeting} ${name}, you are ${age}!", map[string]string{
			"greeting": "Hello",
			"name":     "Alice",
			"age":      "30",
		})
		if result != "Hello Alice, you are 30!" {
			t.Errorf("unexpected result: %q", result)
		}
	})

	t.Run("no placeholders", func(t *testing.T) {
		result := ResolvePlaceholders("no placeholders here", map[string]string{"key": "value"})
		if result != "no placeholders here" {
			t.Errorf("expected unchanged string, got %q", result)
		}
	})

	t.Run("unresolved stay", func(t *testing.T) {
		result := ResolvePlaceholders("${known} and ${unknown}", map[string]string{"known": "found"})
		if result != "found and ${unknown}" {
			t.Errorf("expected 'found and ${unknown}', got %q", result)
		}
	})
}

func TestWriteReadConfig(t *testing.T) {
	t.Run("round-trip", func(t *testing.T) {
		dir := t.TempDir()

		cv := &ConfigValues{
			Instance:        "my-instance",
			ID:              "abc123",
			SourceComponent: "my-connector",
			ConfiguredAt:    "2024-01-15T10:00:00Z",
			Values: map[string]string{
				"base_url": "https://api.example.com",
				"region":   "us-east-1",
			},
		}

		if err := WriteConfig(dir, cv); err != nil {
			t.Fatalf("WriteConfig error: %v", err)
		}

		// Verify file permissions
		info, err := os.Stat(filepath.Join(dir, "config.yaml"))
		if err != nil {
			t.Fatalf("stat error: %v", err)
		}
		if info.Mode().Perm() != 0600 {
			t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
		}

		got, err := ReadConfig(dir)
		if err != nil {
			t.Fatalf("ReadConfig error: %v", err)
		}

		if got.Instance != cv.Instance {
			t.Errorf("instance: expected %q, got %q", cv.Instance, got.Instance)
		}
		if got.ID != cv.ID {
			t.Errorf("id: expected %q, got %q", cv.ID, got.ID)
		}
		if got.SourceComponent != cv.SourceComponent {
			t.Errorf("source_component: expected %q, got %q", cv.SourceComponent, got.SourceComponent)
		}
		if got.ConfiguredAt != cv.ConfiguredAt {
			t.Errorf("configured_at: expected %q, got %q", cv.ConfiguredAt, got.ConfiguredAt)
		}
		if len(got.Values) != len(cv.Values) {
			t.Errorf("values length: expected %d, got %d", len(cv.Values), len(got.Values))
		}
		for k, v := range cv.Values {
			if got.Values[k] != v {
				t.Errorf("values[%q]: expected %q, got %q", k, v, got.Values[k])
			}
		}
	})
}

func TestSplitSecrets(t *testing.T) {
	schema := &ConfigSchema{
		Fields: []ConfigField{
			{Name: "api_key", Secret: true},
			{Name: "token", Secret: true},
			{Name: "base_url", Secret: false},
			{Name: "region", Secret: false},
		},
	}

	values := map[string]string{
		"api_key":  "super-secret-key",
		"token":    "bearer-token-xyz",
		"base_url": "https://api.example.com",
		"region":   "us-east-1",
	}

	t.Run("secrets get scoped reference", func(t *testing.T) {
		config, secrets := schema.SplitSecrets(values, "my-instance")

		// Secrets map should have real values
		if secrets["api_key"] != "super-secret-key" {
			t.Errorf("expected secrets['api_key'] = 'super-secret-key', got %q", secrets["api_key"])
		}
		if secrets["token"] != "bearer-token-xyz" {
			t.Errorf("expected secrets['token'] = 'bearer-token-xyz', got %q", secrets["token"])
		}

		// Config map should have scoped references for secrets
		if config["api_key"] != "@scoped:my-instance-api_key" {
			t.Errorf("expected config['api_key'] = '@scoped:my-instance-api_key', got %q", config["api_key"])
		}
		if config["token"] != "@scoped:my-instance-token" {
			t.Errorf("expected config['token'] = '@scoped:my-instance-token', got %q", config["token"])
		}

		// Non-secret fields pass through directly
		if config["base_url"] != "https://api.example.com" {
			t.Errorf("expected config['base_url'] = 'https://api.example.com', got %q", config["base_url"])
		}
		if config["region"] != "us-east-1" {
			t.Errorf("expected config['region'] = 'us-east-1', got %q", config["region"])
		}

		// Secrets map should not contain non-secret fields
		if _, ok := secrets["base_url"]; ok {
			t.Error("secrets map should not contain non-secret field 'base_url'")
		}
		if _, ok := secrets["region"]; ok {
			t.Error("secrets map should not contain non-secret field 'region'")
		}
	})

	t.Run("non-secrets pass through", func(t *testing.T) {
		schemaNoSecrets := &ConfigSchema{
			Fields: []ConfigField{
				{Name: "base_url", Secret: false},
			},
		}
		config, secrets := schemaNoSecrets.SplitSecrets(map[string]string{"base_url": "http://example.com"}, "inst")
		if config["base_url"] != "http://example.com" {
			t.Errorf("unexpected config value: %q", config["base_url"])
		}
		if len(secrets) != 0 {
			t.Errorf("expected empty secrets, got %v", secrets)
		}
	})
}

func TestWriteSecrets(t *testing.T) {
	store := map[string]string{}
	putter := SecretPutter(func(name, value string) error {
		store[name] = value
		return nil
	})

	err := WriteSecrets("", "slack-incidents", map[string]string{
		"slack_bot_token": "xoxb-real-key-123",
	}, putter)
	if err != nil {
		t.Fatal(err)
	}

	if store["slack_bot_token"] != "xoxb-real-key-123" {
		t.Errorf("expected slack_bot_token in store, got %v", store)
	}
}

func TestWriteSecrets_NilPutter(t *testing.T) {
	err := WriteSecrets("", "test", map[string]string{"k": "v"}, nil)
	if err == nil {
		t.Fatal("expected error for nil putter")
	}
}

func TestRemoveSecrets(t *testing.T) {
	store := map[string]string{"token": "abc", "other": "keep"}
	deleter := SecretDeleter(func(name string) error {
		delete(store, name)
		return nil
	})

	err := RemoveSecrets("", "slack-incidents", []string{"token"}, deleter)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := store["token"]; ok {
		t.Error("token should have been removed")
	}
	if _, ok := store["other"]; !ok {
		t.Error("other should still exist")
	}
}

func TestRemoveSecrets_NilDeleter(t *testing.T) {
	err := RemoveSecrets("", "test", []string{"k"}, nil)
	if err == nil {
		t.Fatal("expected error for nil deleter")
	}
}

func TestResolvedYAML(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)
	inst, _ := reg.Create("test-conn", "connector", "default/test")

	// Write template with placeholders
	instDir := reg.InstanceDir("test-conn")
	template := `name: test
source:
  url: "https://api.example.com?key=${api_key}&channel=${channel}"
config:
  - name: api_key
    required: true
    secret: true
  - name: channel
    required: true
`
	os.WriteFile(filepath.Join(instDir, "connector.yaml"), []byte(template), 0644)

	// Write config
	WriteConfig(instDir, &ConfigValues{
		Instance: inst.Name,
		Values: map[string]string{
			"api_key": "@scoped:test-conn-api_key",
			"channel": "C0123",
		},
	})

	resolved, err := reg.ResolvedYAML("test-conn")
	if err != nil {
		t.Fatal(err)
	}
	content := string(resolved)
	if strings.Contains(content, "${api_key}") {
		t.Error("api_key placeholder not resolved")
	}
	if !strings.Contains(content, "@scoped:test-conn-api_key") {
		t.Error("scoped reference not substituted")
	}
	if !strings.Contains(content, "C0123") {
		t.Error("channel not resolved")
	}
}

func TestResolvedYAML_NoConfig(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)
	reg.Create("no-config", "connector", "default/test")

	instDir := reg.InstanceDir("no-config")
	os.WriteFile(filepath.Join(instDir, "connector.yaml"), []byte("name: test\n"), 0644)

	resolved, err := reg.ResolvedYAML("no-config")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resolved), "name: test") {
		t.Error("template not returned when no config exists")
	}
}

func TestFindUnresolvedPlaceholders(t *testing.T) {
	t.Run("finds them", func(t *testing.T) {
		placeholders := FindUnresolvedPlaceholders("Hello ${name}, your ${item} is ready at ${location}.")
		if len(placeholders) != 3 {
			t.Fatalf("expected 3 placeholders, got %d: %v", len(placeholders), placeholders)
		}
		found := map[string]bool{}
		for _, p := range placeholders {
			found[p] = true
		}
		for _, expected := range []string{"name", "item", "location"} {
			if !found[expected] {
				t.Errorf("expected placeholder %q not found in %v", expected, placeholders)
			}
		}
	})

	t.Run("returns empty when none", func(t *testing.T) {
		placeholders := FindUnresolvedPlaceholders("no placeholders here")
		if len(placeholders) != 0 {
			t.Errorf("expected empty slice, got %v", placeholders)
		}
	})

	t.Run("empty string", func(t *testing.T) {
		placeholders := FindUnresolvedPlaceholders("")
		if len(placeholders) != 0 {
			t.Errorf("expected empty slice, got %v", placeholders)
		}
	})

	t.Run("single placeholder", func(t *testing.T) {
		placeholders := FindUnresolvedPlaceholders("${only_one}")
		if len(placeholders) != 1 || placeholders[0] != "only_one" {
			t.Errorf("expected ['only_one'], got %v", placeholders)
		}
	})
}
