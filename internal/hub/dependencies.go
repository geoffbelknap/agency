package hub

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DependencyRef is a hub component dependency declared by a component YAML file.
type DependencyRef struct {
	Name string
	Kind string
}

// DependencyRefsFromYAML extracts installable dependency references from component YAML.
func DependencyRefsFromYAML(data []byte) []DependencyRef {
	var doc map[string]interface{}
	if yaml.Unmarshal(data, &doc) != nil {
		return nil
	}

	seen := make(map[string]bool)
	var deps []DependencyRef
	add := func(name, kind string) {
		if name == "" || kind == "" {
			return
		}
		key := kind + ":" + name
		if seen[key] {
			return
		}
		seen[key] = true
		deps = append(deps, DependencyRef{Name: name, Kind: kind})
	}

	if requires, ok := doc["requires"].(map[string]interface{}); ok {
		addList := func(key, kind string) {
			items, ok := requires[key].([]interface{})
			if !ok {
				return
			}
			for _, item := range items {
				name, _ := item.(string)
				add(name, kind)
			}
		}
		addList("services", "service")
		addList("presets", "preset")
		addList("missions", "mission")
		addList("connectors", "connector")
	}

	if assignments, ok := doc["mission_assignments"].([]interface{}); ok {
		for _, raw := range assignments {
			assignment, _ := raw.(map[string]interface{})
			name, _ := assignment["mission"].(string)
			add(name, "mission")
		}
	}

	return deps
}

func (m *Manager) instanceDependencyRefs(inst *Instance) []DependencyRef {
	instDir := m.Registry.InstanceDir(inst.Name)
	if instDir == "" {
		return nil
	}
	templatePath := filepath.Join(instDir, inst.Kind+".yaml")
	data, err := os.ReadFile(templatePath)
	if err != nil {
		return nil
	}
	return DependencyRefsFromYAML(data)
}

// RemoveWithDependencies removes an installed instance and auto-installed orphan dependencies.
func (m *Manager) RemoveWithDependencies(nameOrID string) ([]Instance, error) {
	return m.removeWithDependencies(nameOrID, map[string]bool{})
}

func (m *Manager) removeWithDependencies(nameOrID string, seen map[string]bool) ([]Instance, error) {
	inst := m.Registry.Resolve(nameOrID)
	if inst == nil {
		return nil, fmt.Errorf("instance %q not found", nameOrID)
	}
	if seen[inst.Name] {
		return nil, nil
	}
	seen[inst.Name] = true

	deps := m.instanceDependencyRefs(inst)
	if err := m.Registry.Remove(inst.Name); err != nil {
		return nil, err
	}

	removed := []Instance{*inst}
	for _, dep := range deps {
		if child := m.Registry.Resolve(dep.Name); child != nil {
			_ = m.Registry.RemoveRequiredBy(child.Name, inst.Name)
			child = m.Registry.Resolve(dep.Name)
			if child != nil && child.AutoInstalled && len(child.RequiredBy) == 0 {
				childRemoved, err := m.removeWithDependencies(child.Name, seen)
				if err != nil {
					return removed, err
				}
				removed = append(removed, childRemoved...)
			}
		}
	}

	return removed, nil
}
