package hub

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Instance represents an installed hub component with a unique identity.
type Instance struct {
	ID      string `yaml:"id" json:"id"`
	Name    string `yaml:"name" json:"name"`
	Kind    string `yaml:"kind" json:"kind"`
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
	Source  string `yaml:"source" json:"source"`
	State   string `yaml:"state" json:"state"`
	Created string `yaml:"created" json:"created"`
}

type registryFile struct {
	Instances map[string]Instance `yaml:"instances"`
}

// Registry is an instance-based registry for hub components. Each installed
// component gets a unique 8-char hex ID and a human-readable name.
// All methods are safe for concurrent use.
type Registry struct {
	home string
	mu   sync.Mutex
}

// NewRegistry creates a Registry rooted at home. The registry file is lazily
// loaded on first access; home need not exist yet.
func NewRegistry(home string) *Registry {
	return &Registry{home: home}
}

// registryPath returns the path of the registry YAML file.
func (r *Registry) registryPath() string {
	return filepath.Join(r.home, "registry.yaml")
}

// load reads the registry from disk. Caller must hold r.mu.
func (r *Registry) load() (registryFile, error) {
	var rf registryFile
	data, err := os.ReadFile(r.registryPath())
	if os.IsNotExist(err) {
		rf.Instances = make(map[string]Instance)
		return rf, nil
	}
	if err != nil {
		return rf, fmt.Errorf("read registry: %w", err)
	}
	if err := yaml.Unmarshal(data, &rf); err != nil {
		return rf, fmt.Errorf("parse registry: %w", err)
	}
	if rf.Instances == nil {
		rf.Instances = make(map[string]Instance)
	}
	return rf, nil
}

// save writes the registry to disk. Caller must hold r.mu.
func (r *Registry) save(rf registryFile) error {
	if err := os.MkdirAll(r.home, 0755); err != nil {
		return fmt.Errorf("create registry dir: %w", err)
	}
	data, err := yaml.Marshal(rf)
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	if err := os.WriteFile(r.registryPath(), data, 0644); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	return nil
}

// generateID returns a random 8-character lowercase hex string.
func generateID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Create installs a new instance with the given human-readable name, kind, and
// source. It generates a unique 8-char hex ID, creates the instance directory
// at {home}/{kind}s/{id}/, and persists the entry to registry.yaml.
// Returns an error if name is already taken.
func (r *Registry) Create(name, kind, source string) (*Instance, error) {
	name = filepath.Base(name)
	kind = filepath.Base(kind)
	r.mu.Lock()
	defer r.mu.Unlock()

	rf, err := r.load()
	if err != nil {
		return nil, err
	}

	// Check for duplicate name
	for _, inst := range rf.Instances {
		if inst.Name == name {
			return nil, fmt.Errorf("instance with name %q already exists (id=%s)", name, inst.ID)
		}
	}

	id, err := generateID()
	if err != nil {
		return nil, err
	}

	// Create instance directory
	instDir := filepath.Join(r.home, kind+"s", id)
	if err := os.MkdirAll(instDir, 0755); err != nil {
		return nil, fmt.Errorf("create instance dir: %w", err)
	}

	inst := Instance{
		ID:      id,
		Name:    name,
		Kind:    kind,
		Source:  source,
		State:   "installed",
		Created: time.Now().UTC().Format(time.RFC3339),
	}

	rf.Instances[name] = inst

	if err := r.save(rf); err != nil {
		// Best-effort cleanup of directory we just created
		os.RemoveAll(instDir)
		return nil, err
	}

	return &inst, nil
}

// resolve finds an instance by name or ID. Caller must hold r.mu.
func (r *Registry) resolve(rf registryFile, nameOrID string) *Instance {
	// Try by name first (O(1) map lookup)
	if inst, ok := rf.Instances[nameOrID]; ok {
		return &inst
	}
	// Fall back to ID scan
	for _, inst := range rf.Instances {
		if inst.ID == nameOrID {
			return &inst
		}
	}
	return nil
}

// Resolve looks up an instance by human name or 8-char hex ID.
// Returns nil if not found.
func (r *Registry) Resolve(nameOrID string) *Instance {
	r.mu.Lock()
	defer r.mu.Unlock()

	rf, err := r.load()
	if err != nil {
		return nil
	}
	return r.resolve(rf, nameOrID)
}

// Remove deletes the instance identified by nameOrID, removes its directory,
// and purges the registry entry. Returns an error if not found.
func (r *Registry) Remove(nameOrID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	rf, err := r.load()
	if err != nil {
		return err
	}

	inst := r.resolve(rf, nameOrID)
	if inst == nil {
		return fmt.Errorf("instance %q not found", nameOrID)
	}

	instDir := filepath.Join(r.home, inst.Kind+"s", inst.ID)
	if err := os.RemoveAll(instDir); err != nil {
		return fmt.Errorf("remove instance dir: %w", err)
	}

	delete(rf.Instances, inst.Name)
	return r.save(rf)
}

// List returns all instances, optionally filtered by kind. Pass an empty
// string to return all kinds.
func (r *Registry) List(kind string) []Instance {
	r.mu.Lock()
	defer r.mu.Unlock()

	rf, err := r.load()
	if err != nil {
		return nil
	}

	var out []Instance
	for _, inst := range rf.Instances {
		if kind == "" || inst.Kind == kind {
			out = append(out, inst)
		}
	}
	return out
}

// SetState updates the state field of the instance identified by nameOrID.
// Valid state values are: installed, active, inactive, needs_reconfiguration.
// Returns an error if the instance is not found.
func (r *Registry) SetState(nameOrID, state string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	rf, err := r.load()
	if err != nil {
		return err
	}

	inst := r.resolve(rf, nameOrID)
	if inst == nil {
		return fmt.Errorf("instance %q not found", nameOrID)
	}

	inst.State = state
	rf.Instances[inst.Name] = *inst
	return r.save(rf)
}

// SetVersion updates the version field of the instance identified by nameOrID.
// Returns an error if the instance is not found.
func (r *Registry) SetVersion(nameOrID, version string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	rf, err := r.load()
	if err != nil {
		return err
	}

	inst := r.resolve(rf, nameOrID)
	if inst == nil {
		return fmt.Errorf("instance %q not found", nameOrID)
	}

	inst.Version = version
	rf.Instances[inst.Name] = *inst
	return r.save(rf)
}

// InstanceDir returns the filesystem path for the instance identified by
// nameOrID. Returns an empty string if the instance is not found.
func (r *Registry) InstanceDir(nameOrID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	rf, err := r.load()
	if err != nil {
		return ""
	}

	inst := r.resolve(rf, nameOrID)
	if inst == nil {
		return ""
	}
	return filepath.Join(r.home, inst.Kind+"s", inst.ID)
}

// ResolvedYAML reads the component template and config, substitutes
// all ${...} placeholders, and returns the resolved YAML bytes.
// If no config.yaml exists, the template is returned as-is.
func (r *Registry) ResolvedYAML(nameOrID string) ([]byte, error) {
	inst := r.Resolve(nameOrID)
	if inst == nil {
		return nil, fmt.Errorf("instance %q not found", nameOrID)
	}

	instDir := filepath.Join(r.home, inst.Kind+"s", inst.ID)

	templatePath := filepath.Join(instDir, inst.Kind+".yaml")
	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("read template: %w", err)
	}

	cv, err := ReadConfig(instDir)
	if err != nil {
		// No config.yaml — return template as-is
		return templateData, nil
	}

	resolved := ResolvePlaceholders(string(templateData), cv.Values)
	return []byte(resolved), nil
}

// oldProvenance is the legacy hub-installed.json entry format.
type oldProvenance struct {
	Name        string `json:"name"`
	Component   string `json:"component"` // legacy alias for name
	Kind        string `json:"kind"`
	Source      string `json:"source"`
	InstalledAt string `json:"installed_at"`
}

// MigrateIfNeeded migrates flat-file hub installations to the instance-directory
// model. It is a no-op if registry.yaml already exists (already migrated).
// Returns the count of migrated instances.
func (r *Registry) MigrateIfNeeded() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Skip if already migrated
	if _, err := os.Stat(r.registryPath()); err == nil {
		return 0, nil
	}

	// Load old provenance file (best-effort; may not exist)
	provMap := make(map[string]oldProvenance) // keyed by name
	provPath := filepath.Join(r.home, "hub-installed.json")
	if data, err := os.ReadFile(provPath); err == nil {
		var entries []oldProvenance
		if err := json.Unmarshal(data, &entries); err != nil {
			return 0, fmt.Errorf("parse hub-installed.json: %w", err)
		}
		for _, e := range entries {
			name := e.Name
			if name == "" {
				name = e.Component
			}
			if name != "" {
				provMap[name] = e
			}
		}
	}

	kinds := []string{"connector", "service", "preset", "skill", "pack"}
	rf := registryFile{Instances: make(map[string]Instance)}
	migrated := 0

	for _, kind := range kinds {
		kindDir := filepath.Join(r.home, kind+"s")
		entries, err := os.ReadDir(kindDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return migrated, fmt.Errorf("read %s dir: %w", kindDir, err)
		}

		for _, entry := range entries {
			// Only process non-directory yaml/yml files
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
				continue
			}

			// Derive instance name from filename (strip extension)
			instanceName := strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")

			id, err := generateID()
			if err != nil {
				return migrated, err
			}

			// Create instance directory
			instDir := filepath.Join(r.home, kind+"s", id)
			if err := os.MkdirAll(instDir, 0755); err != nil {
				return migrated, fmt.Errorf("create instance dir: %w", err)
			}

			// Move flat file into instance directory as {kind}.yaml
			srcPath := filepath.Join(kindDir, name)
			dstPath := filepath.Join(instDir, kind+".yaml")
			if err := os.Rename(srcPath, dstPath); err != nil {
				return migrated, fmt.Errorf("move %s to instance dir: %w", srcPath, err)
			}

			// Determine source from old provenance, falling back to "migrated"
			source := "migrated"
			created := time.Now().UTC().Format(time.RFC3339)
			if prov, ok := provMap[instanceName]; ok {
				if prov.Source != "" {
					source = prov.Source
				}
				if prov.InstalledAt != "" {
					created = prov.InstalledAt
				}
			}

			inst := Instance{
				ID:      id,
				Name:    instanceName,
				Kind:    kind,
				Source:  source,
				State:   "installed",
				Created: created,
			}
			rf.Instances[instanceName] = inst
			migrated++
		}
	}

	// Persist registry
	if err := r.save(rf); err != nil {
		return migrated, err
	}

	// Rename old provenance file
	if _, err := os.Stat(provPath); err == nil {
		if err := os.Rename(provPath, provPath+".migrated"); err != nil {
			return migrated, fmt.Errorf("rename hub-installed.json: %w", err)
		}
	}

	return migrated, nil
}
