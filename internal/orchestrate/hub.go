package orchestrate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// HubComponent represents a discovered hub component.
type HubComponent struct {
	Name        string `json:"name" yaml:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description" yaml:"description"`
	Source      string `json:"source"`
	Path        string `json:"path"`
}

// HubSource represents a configured hub git source.
type HubSource struct {
	Name   string `yaml:"name"`
	URL    string `yaml:"url"`
	Branch string `yaml:"branch"`
}

type hubConfig struct {
	Hub struct {
		Sources []HubSource `yaml:"sources"`
	} `yaml:"hub"`
}

var knownKinds = []string{"connector", "preset", "service", "skill", "workspace", "policy", "pack"}

// HubManager handles hub operations.
type HubManager struct {
	Home string
}

func NewHubManager(home string) *HubManager {
	return &HubManager{Home: home}
}

// Sync pulls all configured hub sources.
func (h *HubManager) Sync() ([]string, error) {
	config := h.loadConfig()
	cacheDir := filepath.Join(h.Home, "hub-cache")
	os.MkdirAll(cacheDir, 0755)

	var warnings []string
	for _, src := range config.Hub.Sources {
		if err := h.syncSource(src, cacheDir); err != nil {
			warnings = append(warnings, err.Error())
		}
	}
	return warnings, nil
}

// Search finds components matching a query.
func (h *HubManager) Search(query, kind string) []HubComponent {
	all := h.Discover()
	q := strings.ToLower(query)

	if kind != "" {
		var filtered []HubComponent
		for _, c := range all {
			if c.Kind == kind {
				filtered = append(filtered, c)
			}
		}
		all = filtered
	}

	var exact, substring, desc []HubComponent
	for _, c := range all {
		nameLower := strings.ToLower(c.Name)
		descLower := strings.ToLower(c.Description)
		if nameLower == q {
			exact = append(exact, c)
		} else if strings.Contains(nameLower, q) {
			substring = append(substring, c)
		} else if strings.Contains(descLower, q) {
			desc = append(desc, c)
		}
	}
	return append(append(exact, substring...), desc...)
}

// Discover returns all components across hub sources.
func (h *HubManager) Discover() []HubComponent {
	config := h.loadConfig()
	cacheDir := filepath.Join(h.Home, "hub-cache")
	var results []HubComponent

	for _, src := range config.Hub.Sources {
		srcDir := filepath.Join(cacheDir, src.Name)
		if _, err := os.Stat(srcDir); err != nil {
			continue
		}
		for _, kind := range knownKinds {
			kindDir := filepath.Join(srcDir, kind+"s")
			if _, err := os.Stat(kindDir); err != nil {
				continue
			}
			filepath.Walk(kindDir, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() || !strings.HasSuffix(path, ".yaml") {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return nil
				}
				var m map[string]interface{}
				if yaml.Unmarshal(data, &m) != nil {
					return nil
				}
				name, _ := m["name"].(string)
				if name == "" {
					name = strings.TrimSuffix(info.Name(), ".yaml")
				}
				desc, _ := m["description"].(string)
				results = append(results, HubComponent{
					Name:        name,
					Kind:        kind,
					Description: desc,
					Source:      src.Name,
					Path:        path,
				})
				return nil
			})
		}
	}
	return results
}

// List returns installed components from provenance.
func (h *HubManager) List() []map[string]string {
	provPath := filepath.Join(h.Home, "hub-installed.json")
	data, err := os.ReadFile(provPath)
	if err != nil {
		return nil
	}
	var entries []map[string]string
	// Simple JSON parse
	if len(data) > 2 {
		// Use yaml for simplicity (it handles JSON)
		yaml.Unmarshal(data, &entries)
	}
	return entries
}

func (h *HubManager) syncSource(src HubSource, cacheDir string) error {
	dest := filepath.Join(cacheDir, src.Name)
	gitDir := filepath.Join(dest, ".git")

	if _, err := os.Stat(gitDir); err == nil {
		// Pull
		cmd := exec.Command("git", "-C", dest,
			"-c", "core.hooksPath=/dev/null",
			"-c", "protocol.ext.allow=never",
			"pull", "--ff-only")
		out, err := cmd.CombinedOutput()
		if err != nil {
			// Re-clone
			os.RemoveAll(dest)
		} else {
			_ = out
			return nil
		}
	}

	// Clone
	branch := src.Branch
	if branch == "" {
		branch = "main"
	}
	cmd := exec.Command("git", "clone", "--depth", "1",
		"--branch", branch,
		"-c", "core.hooksPath=/dev/null",
		"-c", "protocol.ext.allow=never",
		src.URL, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("clone %s: %s", src.Name, string(out))
	}
	return nil
}

func (h *HubManager) loadConfig() hubConfig {
	var config hubConfig
	data, err := os.ReadFile(filepath.Join(h.Home, "config.yaml"))
	if err != nil {
		return config
	}
	yaml.Unmarshal(data, &config)
	return config
}
