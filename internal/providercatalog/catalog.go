package providercatalog

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/geoffbelknap/agency/internal/hub"
	"gopkg.in/yaml.v3"
)

//go:embed providers/*.yaml setup.yaml provider_tools.yaml
var bundled embed.FS

// This package is a temporary core-path provider shim. It keeps bundled
// provider definitions local to the binary, but still delegates actual
// routing merges through the existing hub merge logic so switching back to a
// hub-backed source later is a catalog-source change, not a second install path.

type ProviderDoc struct {
	Name        string                 `yaml:"name" json:"name"`
	DisplayName string                 `yaml:"display_name" json:"display_name"`
	Description string                 `yaml:"description" json:"description"`
	Category    string                 `yaml:"category" json:"category"`
	Credential  map[string]interface{} `yaml:"credential" json:"credential"`
	Routing     map[string]interface{} `yaml:"routing" json:"routing"`
}

type ProviderToolInventory struct {
	Version      string                            `yaml:"version" json:"version"`
	Capabilities map[string]ProviderToolCapability `yaml:"capabilities" json:"capabilities"`
}

type ProviderToolCapability struct {
	Title        string                          `yaml:"title" json:"title"`
	Risk         string                          `yaml:"risk" json:"risk"`
	DefaultGrant bool                            `yaml:"default_grant" json:"default_grant"`
	Execution    string                          `yaml:"execution" json:"execution"`
	Description  string                          `yaml:"description" json:"description"`
	Providers    map[string]ProviderToolProvider `yaml:"providers" json:"providers"`
}

type ProviderToolProvider struct {
	Status          string                 `yaml:"status" json:"status"`
	RequestTools    []string               `yaml:"request_tools,omitempty" json:"request_tools,omitempty"`
	GenericTools    []string               `yaml:"generic_tools,omitempty" json:"generic_tools,omitempty"`
	RequestFields   []string               `yaml:"request_fields,omitempty" json:"request_fields,omitempty"`
	RequiredHeaders []string               `yaml:"required_headers,omitempty" json:"required_headers,omitempty"`
	Endpoints       []string               `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	CapabilityAlias string                 `yaml:"capability_alias,omitempty" json:"capability_alias,omitempty"`
	Pricing         map[string]interface{} `yaml:"pricing,omitempty" json:"pricing,omitempty"`
	Tests           []string               `yaml:"tests,omitempty" json:"tests,omitempty"`
	Notes           string                 `yaml:"notes,omitempty" json:"notes,omitempty"`
}

func List() ([]ProviderDoc, error) {
	entries, err := fs.Glob(bundled, "providers/*.yaml")
	if err != nil {
		return nil, err
	}
	sort.Strings(entries)
	out := make([]ProviderDoc, 0, len(entries))
	for _, entry := range entries {
		doc, err := loadDoc(entry)
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, nil
}

func Get(name string) (ProviderDoc, []byte, error) {
	path := filepath.Join("providers", name+".yaml")
	data, err := bundled.ReadFile(path)
	if err != nil {
		return ProviderDoc{}, nil, fmt.Errorf("provider %q not found", name)
	}
	var doc ProviderDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return ProviderDoc{}, nil, fmt.Errorf("parse provider %q: %w", name, err)
	}
	return doc, data, nil
}

func Install(home, name string) error {
	_, data, err := Get(name)
	if err != nil {
		return err
	}
	return hub.MergeProviderRouting(home, name, data)
}

func SetupConfig() (map[string]interface{}, error) {
	data, err := bundled.ReadFile("setup.yaml")
	if err != nil {
		return nil, err
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func ProviderTools() (ProviderToolInventory, error) {
	data, err := bundled.ReadFile("provider_tools.yaml")
	if err != nil {
		return ProviderToolInventory{}, err
	}
	var doc ProviderToolInventory
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return ProviderToolInventory{}, fmt.Errorf("parse provider_tools.yaml: %w", err)
	}
	return doc, nil
}

func loadDoc(path string) (ProviderDoc, error) {
	data, err := bundled.ReadFile(path)
	if err != nil {
		return ProviderDoc{}, err
	}
	var doc ProviderDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return ProviderDoc{}, fmt.Errorf("parse %s: %w", path, err)
	}
	doc.Name = strings.TrimSpace(doc.Name)
	return doc, nil
}
