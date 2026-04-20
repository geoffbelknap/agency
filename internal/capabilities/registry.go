package capabilities

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/geoffbelknap/agency/internal/providercatalog"
	"gopkg.in/yaml.v3"
)

// Entry represents a registered capability (MCP server, service, skill, or provider tool).
type Entry struct {
	Name        string                 `json:"name" yaml:"name"`
	Kind        string                 `json:"kind" yaml:"kind"` // mcp-server, service, skill
	Description string                 `json:"description,omitempty" yaml:"description"`
	State       string                 `json:"state"` // available, restricted, disabled
	Agents      []string               `json:"agents,omitempty"`
	URL         string                 `json:"url,omitempty" yaml:"url,omitempty"`
	KeyEnv      string                 `json:"key_env,omitempty" yaml:"key_env,omitempty"`
	Spec        map[string]interface{} `json:"spec,omitempty" yaml:",inline"`
}

type capState struct {
	State  string   `yaml:"state"`
	Agents []string `yaml:"agents,omitempty"`
	Key    string   `yaml:"key,omitempty"`
}

type capConfig struct {
	Capabilities map[string]capState `yaml:"capabilities"`
}

// Registry manages capability registration, state, and keys.
type Registry struct {
	Home string
}

// NewRegistry creates a new capability registry rooted at the agency home directory.
func NewRegistry(home string) *Registry {
	return &Registry{Home: resolveHome(home)}
}

// List returns all capabilities, merging registry entries with state from capabilities.yaml.
func (r *Registry) List() []Entry {
	states := r.loadStates()
	var entries []Entry

	// Walk registry directories
	kinds := map[string]string{
		"mcp-servers": "mcp-server",
		"services":    "service",
		"skills":      "skill",
	}

	registryDir := filepath.Join(r.Home, "registry")
	for dirName, kind := range kinds {
		dir := filepath.Join(registryDir, dirName)
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".yaml") {
				continue
			}
			name := strings.TrimSuffix(f.Name(), ".yaml")
			data, err := os.ReadFile(filepath.Join(dir, f.Name()))
			if err != nil {
				continue
			}
			var spec map[string]interface{}
			if yaml.Unmarshal(data, &spec) != nil {
				continue
			}
			desc, _ := spec["description"].(string)
			if desc == "" {
				desc, _ = spec["display_name"].(string)
			}
			url, _ := spec["url"].(string)
			keyEnv, _ := spec["key_env"].(string)

			state := "disabled"
			var agents []string
			if s, ok := states[name]; ok {
				state = s.State
				agents = s.Agents
			}

			entries = append(entries, Entry{
				Name:        name,
				Kind:        kind,
				Description: desc,
				State:       state,
				Agents:      agents,
				URL:         url,
				KeyEnv:      keyEnv,
			})
		}
	}

	// Also check services directory for service-based capabilities
	servicesDir := filepath.Join(r.Home, "services")
	if files, err := os.ReadDir(servicesDir); err == nil {
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".yaml") {
				continue
			}
			name := strings.TrimSuffix(f.Name(), ".yaml")
			// Skip if already found in registry
			found := false
			for _, e := range entries {
				if e.Name == name {
					found = true
					break
				}
			}
			if found {
				continue
			}
			data, err := os.ReadFile(filepath.Join(servicesDir, f.Name()))
			if err != nil {
				continue
			}
			var spec map[string]interface{}
			if yaml.Unmarshal(data, &spec) != nil {
				continue
			}
			desc, _ := spec["description"].(string)
			if desc == "" {
				desc, _ = spec["display_name"].(string)
			}
			url, _ := spec["url"].(string)
			keyEnv, _ := spec["key_env"].(string)

			state := "disabled"
			var agents []string
			if s, ok := states[name]; ok {
				state = s.State
				agents = s.Agents
			}

			entries = append(entries, Entry{
				Name:        name,
				Kind:        "service",
				Description: desc,
				State:       state,
				Agents:      agents,
				URL:         url,
				KeyEnv:      keyEnv,
			})
		}
	}

	entries = append(entries, r.providerToolEntries(states, entries)...)
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}

// Show returns details for a single capability.
func (r *Registry) Show(name string) *Entry {
	for _, e := range r.List() {
		if e.Name == name {
			return &e
		}
	}
	return nil
}

// Enable sets a capability to available or restricted state.
// Key storage is handled by the API handler via the credential store.
func (r *Registry) Enable(name, key string, agents []string) error {
	states := r.loadStates()

	state := "available"
	if len(agents) > 0 {
		state = "restricted"
	}
	cs := capState{State: state, Agents: agents}
	states[name] = cs

	return r.saveStates(states)
}

// Disable sets a capability to disabled state.
func (r *Registry) Disable(name string) error {
	states := r.loadStates()
	if s, ok := states[name]; ok {
		s.State = "disabled"
		states[name] = s
	} else {
		states[name] = capState{State: "disabled"}
	}
	return r.saveStates(states)
}

// Add writes a new registry entry YAML file.
func (r *Registry) Add(kind, name string, spec map[string]interface{}) error {
	dirName := ""
	switch kind {
	case "mcp-server":
		dirName = "mcp-servers"
	case "service":
		dirName = "services"
	case "skill":
		dirName = "skills"
	default:
		return fmt.Errorf("unknown capability kind: %s", kind)
	}

	dir := filepath.Join(r.Home, "registry", dirName)
	os.MkdirAll(dir, 0755)

	if spec == nil {
		spec = make(map[string]interface{})
	}
	spec["name"] = name

	data, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}

	return os.WriteFile(filepath.Join(dir, name+".yaml"), data, 0644)
}

// Delete removes a capability from the registry and capabilities.yaml.
func (r *Registry) Delete(name string) error {
	// Remove from registry directories
	kinds := []string{"mcp-servers", "services", "skills"}
	registryDir := filepath.Join(r.Home, "registry")
	for _, kind := range kinds {
		path := filepath.Join(registryDir, kind, name+".yaml")
		os.Remove(path) // ignore errors — may not exist in this dir
	}

	// Remove from capabilities.yaml
	states := r.loadStates()
	delete(states, name)
	return r.saveStates(states)
}

// --- internal helpers ---

func (r *Registry) providerToolEntries(states map[string]capState, existing []Entry) []Entry {
	seen := make(map[string]bool, len(existing))
	for _, entry := range existing {
		seen[entry.Name] = true
	}
	inventory, err := providercatalog.ProviderTools()
	if err != nil {
		return nil
	}
	out := make([]Entry, 0, len(inventory.Capabilities))
	for name, tool := range inventory.Capabilities {
		if seen[name] {
			continue
		}
		state := "disabled"
		var agents []string
		if s, ok := states[name]; ok {
			state = s.State
			agents = s.Agents
		}
		out = append(out, Entry{
			Name:        name,
			Kind:        "provider-tool",
			Description: tool.Description,
			State:       state,
			Agents:      agents,
			Spec: map[string]interface{}{
				"title":         tool.Title,
				"risk":          tool.Risk,
				"default_grant": tool.DefaultGrant,
				"execution":     tool.Execution,
				"providers":     tool.Providers,
			},
		})
	}
	return out
}

func (r *Registry) configPath() string {
	return filepath.Join(r.Home, "capabilities.yaml")
}

func (r *Registry) loadStates() map[string]capState {
	result := make(map[string]capState)
	data, err := os.ReadFile(r.configPath())
	if err != nil {
		return result
	}
	var cfg capConfig
	yaml.Unmarshal(data, &cfg)
	if cfg.Capabilities != nil {
		return cfg.Capabilities
	}
	return result
}

func (r *Registry) saveStates(states map[string]capState) error {
	cfg := capConfig{Capabilities: states}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(r.configPath(), data, 0644)
}

func resolveHome(home string) string {
	if home != "" {
		return home
	}
	if envHome := os.Getenv("AGENCY_HOME"); envHome != "" {
		return envHome
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ".agency"
	}
	return filepath.Join(userHome, ".agency")
}
