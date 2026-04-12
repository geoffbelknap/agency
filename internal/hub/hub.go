package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geoffbelknap/agency/internal/models"
	"gopkg.in/yaml.v3"
)

// allowedAPIDomains is the set of known-good API provider domains.
// Hub-synced routing and service definitions are validated against this list.
// Defense-in-depth: the egress proxy allowlist is the primary control.
var allowedAPIDomains = map[string]bool{
	"api.anthropic.com":                 true,
	"api.openai.com":                    true,
	"generativelanguage.googleapis.com": true,
	"api.x.ai":                          true,
	"api.deepseek.com":                  true,
	"api.github.com":                    true,
	"api.search.brave.com":              true,
	"slack.com":                         true,
	"localhost":                         true, // local models (ollama, etc.)
}

// allowedAuthEnvVars is the set of known-good credential env var names.
// Hub-synced routing definitions must only reference these variables to
// prevent secret exfiltration via attacker-controlled auth_env fields.
var allowedAuthEnvVars = map[string]bool{
	"ANTHROPIC_API_KEY": true,
	"OPENAI_API_KEY":    true,
	"GOOGLE_API_KEY":    true,
	"GEMINI_API_KEY":    true,
	"XAI_API_KEY":       true,
	"DEEPSEEK_API_KEY":  true,
	"GITHUB_TOKEN":      true,
	"BRAVE_API_KEY":     true,
}

// Component kinds supported by the hub.
var KnownKinds = []string{"pack", "preset", "connector", "service", "mission", "skill", "workspace", "policy", "ontology", "provider", "setup"}

var nonInstallableKinds = map[string]bool{
	"ontology": true,
	"setup":    true,
}

// Source represents a hub registry source (OCI or git).
type Source struct {
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type,omitempty" json:"type,omitempty"`         // "oci" or "git"; defaults to "git"
	URL      string `yaml:"url,omitempty" json:"url,omitempty"`           // git URL (when type=git)
	Registry string `yaml:"registry,omitempty" json:"registry,omitempty"` // OCI registry base (when type=oci)
	Branch   string `yaml:"branch,omitempty" json:"branch,omitempty"`     // git branch (when type=git)
}

// EffectiveType returns the source type, defaulting to "git" for backward compat.
func (s Source) EffectiveType() string {
	if s.Type == "oci" {
		return "oci"
	}
	return "git"
}

// ComponentRef returns the full OCI reference for a component.
// Format: {registry}/{kind}/{name}:{version}
func (s Source) ComponentRef(kind, name, version string) string {
	if version == "" {
		version = "latest"
	}
	return s.Registry + "/" + kind + "/" + name + ":" + version
}

// DefaultSource is the official Agency hub.
var DefaultSource = Source{
	Name:     "official",
	Type:     "oci",
	Registry: "ghcr.io/geoffbelknap/agency-hub",
}

// Component represents a discovered hub component.
type Component struct {
	Name        string `json:"name" yaml:"name"`
	Kind        string `json:"kind"`
	Version     string `json:"version,omitempty" yaml:"version"`
	Description string `json:"description,omitempty" yaml:"description"`
	License     string `yaml:"license,omitempty" json:"license,omitempty"`
	Author      string `yaml:"author,omitempty" json:"author,omitempty"`
	Source      string `json:"source"`
	Path        string `json:"path"`
}

// Provenance records the installation of a hub component.
type Provenance struct {
	Name        string `json:"name,omitempty"`
	Component   string `json:"component,omitempty"`
	Kind        string `json:"kind"`
	Source      string `json:"source"`
	InstalledAt string `json:"installed_at"`
}

// DisplayName returns the component name, checking both fields for compatibility.
func (p Provenance) DisplayName() string {
	if p.Name != "" {
		return p.Name
	}
	return p.Component
}

type hubConfig struct {
	Hub struct {
		Sources []Source `yaml:"sources"`
	} `yaml:"hub"`
}

// Manager handles hub operations: sync, search, install, remove, list, info.
type Manager struct {
	Home     string
	Registry *Registry
}

// NewManager creates a new hub manager rooted at the agency home directory.
func NewManager(home string) *Manager {
	return &Manager{Home: home, Registry: NewRegistry(filepath.Join(home, "hub-registry"))}
}

// Update clones or pulls all configured hub sources into hub-cache/.
// Returns an UpdateReport with source diffs and available upgrades.
// Does NOT sync managed files or upgrade components — use Upgrade() for that.
func (m *Manager) Update() (*UpdateReport, error) {
	// One-time migration: official source git → OCI
	if m.migrateDefaultSourceToOCI() {
		fmt.Println("[hub] Migrated official source from git to OCI")
	}

	cfg := m.loadConfig()
	cacheDir := filepath.Join(m.Home, "hub-cache")
	os.MkdirAll(cacheDir, 0755)

	report := &UpdateReport{}
	if err := pruneLegacyOfficialCache(cacheDir, cfg.Hub.Sources); err != nil {
		report.Warnings = append(report.Warnings, err.Error())
	}
	for _, src := range cfg.Hub.Sources {
		switch src.EffectiveType() {
		case "oci":
			client := newOCIClient(src.Registry)
			if err := client.syncOCISource(context.Background(), cacheDir, src.Name); err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %s", src.Name, err.Error()))
			}
			report.Sources = append(report.Sources, SourceUpdate{Name: src.Name})
		default:
			su, err := m.syncSourceWithReport(src, cacheDir)
			if err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %s", src.Name, err.Error()))
			}
			report.Sources = append(report.Sources, su)
		}
	}

	// Check what upgrades are available after pull
	report.Available = m.Outdated()

	return report, nil
}

// syncRouting copies the hub's routing.yaml into infrastructure/ after
// validating that provider entries only reference known-good domains and
// credential env vars. Entries with unknown auth_env values are stripped
// to prevent secret exfiltration. Unknown api_base domains produce a
// warning but are kept (operator may have legitimate custom providers).
// Defense-in-depth: the egress proxy allowlist is the primary control.
func (m *Manager) syncRouting(cacheDir string) error {
	// Step 1-2: Read and validate hub cache base
	var hubBase []byte
	entries, _ := os.ReadDir(cacheDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cacheDir, e.Name(), "pricing/routing.yaml"))
		if err != nil {
			continue
		}
		validated, err := validateRoutingConfig(data)
		if err != nil {
			return fmt.Errorf("routing validation: %w", err)
		}
		hubBase = validated
		break
	}
	if hubBase == nil {
		return nil // no routing.yaml in hub cache
	}

	// Step 3: Unmarshal hub base
	var cfg map[string]interface{}
	if err := yaml.Unmarshal(hubBase, &cfg); err != nil {
		return fmt.Errorf("parse hub routing: %w", err)
	}

	// Step 4: Identify default provider names from hub base
	defaultProviders := map[string]bool{}
	if providers, ok := cfg["providers"].(map[string]interface{}); ok {
		for name := range providers {
			defaultProviders[name] = true
		}
	}

	// Steps 5-7: Query installed providers, filter to non-defaults, merge
	for _, inst := range m.Registry.List("provider") {
		if defaultProviders[inst.Name] {
			continue // hub base is authoritative for defaults
		}
		instDir := m.Registry.InstanceDir(inst.Name)
		if instDir == "" {
			continue
		}
		providerData, err := os.ReadFile(filepath.Join(instDir, "provider.yaml"))
		if err != nil {
			log.Printf("[hub] WARNING: cannot read provider %q for routing merge: %v", inst.Name, err)
			continue
		}
		if err := mergeProviderInto(cfg, inst.Name, providerData); err != nil {
			log.Printf("[hub] WARNING: failed to merge provider %q routing: %v", inst.Name, err)
		}
	}

	// Step 8: Validate merged output (catches auth_env allowlist changes since install)
	merged, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal merged routing: %w", err)
	}
	validated, err := validateRoutingConfig(merged)
	if err != nil {
		return fmt.Errorf("merged routing validation: %w", err)
	}

	// Step 9: Write
	destPath := filepath.Join(m.Home, "infrastructure", "routing.yaml")
	os.MkdirAll(filepath.Dir(destPath), 0755)
	return os.WriteFile(destPath, validated, 0644)
}

// syncServices copies service definitions from the hub into the registry.
// Each service YAML is validated: api_base must use HTTPS (or localhost HTTP)
// and should belong to a known-good domain. Services with unknown domains are
// written with a warning — the operator may have legitimate custom services.
// Defense-in-depth: the egress proxy allowlist is the primary control.
func (m *Manager) syncServices(cacheDir string) error {
	destDir := filepath.Join(m.Home, "registry", "services")
	os.MkdirAll(destDir, 0755)

	entries, _ := os.ReadDir(cacheDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		svcDir := filepath.Join(cacheDir, e.Name(), "services")
		files, err := os.ReadDir(svcDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			var data []byte
			var svcName string
			if f.IsDir() {
				// New structure: services/{name}/service.yaml
				svcYAML := filepath.Join(svcDir, f.Name(), "service.yaml")
				d, err := os.ReadFile(svcYAML)
				if err != nil {
					continue
				}
				data = d
				svcName = f.Name() + ".yaml"
			} else if strings.HasSuffix(f.Name(), ".yaml") {
				// Legacy structure: services/{name}.yaml
				d, err := os.ReadFile(filepath.Join(svcDir, f.Name()))
				if err != nil {
					continue
				}
				data = d
				svcName = f.Name()
			} else {
				continue
			}
			if err := validateServiceDefinition(svcName, data); err != nil {
				log.Printf("[hub] WARNING: skipping service %s: %s", svcName, err)
				continue
			}
			os.WriteFile(filepath.Join(destDir, svcName), data, 0644)
		}
	}
	return nil
}

// syncOntology copies the base ontology from the hub.
func (m *Manager) syncOntology(cacheDir string) error {
	destDir := filepath.Join(m.Home, "knowledge")
	os.MkdirAll(destDir, 0755)

	return m.syncHubFile(cacheDir, "ontology/base-ontology.yaml",
		filepath.Join(destDir, "base-ontology.yaml"))
}

// syncHubFile copies a single file from the first hub source that has it.
func (m *Manager) syncHubFile(cacheDir, hubPath, destPath string) error {
	entries, _ := os.ReadDir(cacheDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cacheDir, e.Name(), hubPath))
		if err != nil {
			continue
		}
		os.MkdirAll(filepath.Dir(destPath), 0755)
		return os.WriteFile(destPath, data, 0644)
	}
	return nil
}

// Search finds components matching a query string and optional kind filter.
func (m *Manager) Search(query, kind string) []Component {
	all := m.discover()
	q := strings.ToLower(query)

	if kind != "" {
		var filtered []Component
		for _, c := range all {
			if c.Kind == kind {
				filtered = append(filtered, c)
			}
		}
		all = filtered
	}

	if q == "" || q == "*" {
		return all
	}

	var exact, substring, desc []Component
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

// Install copies a component from hub-cache into the instance registry and returns
// the created Instance. instanceName defaults to componentName when empty.
func (m *Manager) Install(componentName, kind, source, instanceName string) (*Instance, error) {
	// Auto-detect kind if not specified
	if kind == "" {
		matches := m.findAllInCache(componentName, source)
		if len(matches) == 0 {
			return nil, fmt.Errorf("component %q not found in hub cache", componentName)
		}
		if len(matches) > 1 {
			kinds := make([]string, len(matches))
			for i, c := range matches {
				kinds[i] = c.Kind
			}
			return nil, fmt.Errorf("component %q is ambiguous — found as %s. Use --kind to specify", componentName, strings.Join(kinds, ", "))
		}
		kind = matches[0].Kind
	}

	if !isValidKind(kind) {
		return nil, fmt.Errorf("unknown kind: %s", kind)
	}
	if !IsInstallableKind(kind) {
		return nil, fmt.Errorf("kind %q is hub-managed and cannot be installed directly", kind)
	}

	if instanceName == "" {
		instanceName = componentName
	}

	// Find component in cache
	comp := m.findInCache(componentName, kind, source)
	if comp == nil {
		return nil, fmt.Errorf("component %q (kind=%s) not found in hub cache", componentName, kind)
	}

	// Read source file
	data, err := os.ReadFile(comp.Path)
	if err != nil {
		return nil, fmt.Errorf("read component: %w", err)
	}

	// Verify signature for OCI-sourced components
	if src := m.findSourceByName(comp.Source); src != nil && src.EffectiveType() == "oci" {
		ref := src.ComponentRef(kind, componentName, comp.Version)
		if err := verifySignature(context.Background(), ref); err != nil {
			return nil, fmt.Errorf("signature verification failed: %w", err)
		}
	}

	// Create instance via registry
	instSource := comp.Source + "/" + componentName
	inst, err := m.Registry.Create(instanceName, kind, instSource)
	if err != nil {
		return nil, fmt.Errorf("create instance: %w", err)
	}

	// Extract version from component YAML
	var doc map[string]interface{}
	if yaml.Unmarshal(data, &doc) == nil {
		if v, ok := doc["version"].(string); ok {
			m.Registry.SetVersion(inst.Name, v)
			inst.Version = v
		}
	}

	// Write template YAML into the instance directory
	instDir := m.Registry.InstanceDir(inst.ID)
	destPath := filepath.Join(instDir, kind+".yaml")
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		// Best-effort cleanup
		m.Registry.Remove(inst.Name)
		return nil, fmt.Errorf("write component: %w", err)
	}

	// Provider-specific: merge routing config
	if kind == "provider" {
		if err := MergeProviderRouting(m.Home, componentName, data); err != nil {
			log.Printf("[hub] WARNING: failed to merge provider routing: %v", err)
		}
	}

	return inst, nil
}

// Remove deletes a component from ~/.agency/{kind}s/ and removes its provenance entry.
func (m *Manager) Remove(name, kind string) error {
	if !isValidKind(kind) {
		return fmt.Errorf("unknown kind: %s", kind)
	}

	var destPath string
	if kind == "ontology" {
		destPath = filepath.Join(m.Home, "knowledge", "ontology.d", name+".yaml")
	} else {
		destPath = filepath.Join(m.Home, kind+"s", name+".yaml")
	}
	if _, err := os.Stat(destPath); err != nil {
		return fmt.Errorf("component %q (kind=%s) not installed", name, kind)
	}

	// Provider-specific: remove routing entries
	if kind == "provider" {
		if err := RemoveProviderRouting(m.Home, name); err != nil {
			log.Printf("[hub] WARNING: failed to remove provider routing: %v", err)
		}
	}

	if err := os.Remove(destPath); err != nil {
		return fmt.Errorf("remove component: %w", err)
	}

	m.removeProvenance(name, kind)
	return nil
}

// List returns all installed components from hub-installed.json.
func (m *Manager) List() []Provenance {
	return m.loadProvenance()
}

// Info returns full YAML content for a component found in the hub cache.
func (m *Manager) Info(name, kind string) (map[string]interface{}, error) {
	comp := m.findInCache(name, kind, "")
	if comp == nil {
		return nil, fmt.Errorf("component %q not found in hub cache", name)
	}

	data, err := os.ReadFile(comp.Path)
	if err != nil {
		return nil, fmt.Errorf("read component: %w", err)
	}

	var content map[string]interface{}
	if err := yaml.Unmarshal(data, &content); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}

	// Add metadata
	content["_source"] = comp.Source
	content["_kind"] = comp.Kind
	content["_path"] = comp.Path
	for _, installed := range m.List() {
		if installed.DisplayName() == comp.Name && installed.Kind == comp.Kind {
			content["_installed"] = true
			content["_installed_at"] = installed.InstalledAt
			content["_installed_source"] = installed.Source
			break
		}
	}

	return content, nil
}

// Outdated compares installed component versions and managed file hashes
// against the current hub-cache state. No network access, no writes.
func (m *Manager) Outdated() []AvailableUpgrade {
	var upgrades []AvailableUpgrade

	// Compare installed component versions
	instances := m.Registry.List("")
	for _, inst := range instances {
		cached := m.findInCache(inst.Name, inst.Kind, "")
		if cached == nil || cached.Version == "" || inst.Version == "" {
			continue
		}
		if cached.Version != inst.Version {
			upgrades = append(upgrades, AvailableUpgrade{
				Name:             inst.Name,
				Kind:             inst.Kind,
				InstalledVersion: inst.Version,
				AvailableVersion: cached.Version,
			})
		}
	}

	// Compare managed files
	upgrades = append(upgrades, m.outdatedManagedFiles()...)

	return upgrades
}

// outdatedManagedFiles compares managed file hashes between hub-cache and ~/.agency/.
func (m *Manager) outdatedManagedFiles() []AvailableUpgrade {
	cacheDir := filepath.Join(m.Home, "hub-cache")
	var upgrades []AvailableUpgrade

	type managedFile struct {
		hubRelPath string
		localPath  string
		category   string
	}

	files := []managedFile{
		{"ontology/base-ontology.yaml", filepath.Join(m.Home, "knowledge", "base-ontology.yaml"), "ontology"},
		{"pricing/routing.yaml", filepath.Join(m.Home, "infrastructure", "routing.yaml"), "routing"},
	}

	entries, _ := os.ReadDir(cacheDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		srcDir := filepath.Join(cacheDir, e.Name())

		// Check fixed managed files
		for _, mf := range files {
			hubPath := filepath.Join(srcDir, mf.hubRelPath)
			hubH := fileHash(hubPath)
			localH := fileHash(mf.localPath)
			if hubH != "" && hubH != localH {
				upgrades = append(upgrades, AvailableUpgrade{
					Name:     mf.category,
					Kind:     "managed",
					Category: mf.category,
					Summary:  m.managedFileSummary(mf.category, mf.localPath, hubPath),
				})
			}
		}

		// Check services directory
		svcCacheDir := filepath.Join(srcDir, "services")
		svcFiles, err := os.ReadDir(svcCacheDir)
		if err != nil {
			continue
		}
		svcLocalDir := filepath.Join(m.Home, "registry", "services")
		var svcChanges []string
		for _, sf := range svcFiles {
			if sf.IsDir() || !strings.HasSuffix(sf.Name(), ".yaml") {
				continue
			}
			hubH := fileHash(filepath.Join(svcCacheDir, sf.Name()))
			localH := fileHash(filepath.Join(svcLocalDir, sf.Name()))
			if hubH != localH {
				if localH == "" {
					svcChanges = append(svcChanges, "+"+sf.Name())
				} else {
					svcChanges = append(svcChanges, "~"+sf.Name())
				}
			}
		}
		if len(svcChanges) > 0 {
			upgrades = append(upgrades, AvailableUpgrade{
				Name:     "services",
				Kind:     "managed",
				Category: "services",
				Summary:  strings.Join(svcChanges, ", "),
			})
		}

		break // Only check first source
	}

	return upgrades
}

// managedFileSummary generates a human-readable summary for a managed file change.
func (m *Manager) managedFileSummary(category, localPath, hubPath string) string {
	switch category {
	case "ontology":
		return ontologySummary(localPath, hubPath)
	case "routing":
		return routingSummary(localPath, hubPath)
	default:
		return ""
	}
}

// ontologySummary compares two ontology YAML files and reports type count changes.
func ontologySummary(localPath, hubPath string) string {
	countTypes := func(path string) (int, int, string) {
		data, err := os.ReadFile(path)
		if err != nil {
			return 0, 0, ""
		}
		var doc map[string]interface{}
		if yaml.Unmarshal(data, &doc) != nil {
			return 0, 0, ""
		}
		entities, _ := doc["entity_types"].(map[string]interface{})
		rels, _ := doc["relationship_types"].(map[string]interface{})
		ver, _ := doc["version"].(string)
		return len(entities), len(rels), ver
	}

	localE, localR, localV := countTypes(localPath)
	hubE, hubR, hubV := countTypes(hubPath)

	parts := []string{}
	if localV != "" && hubV != "" && localV != hubV {
		parts = append(parts, fmt.Sprintf("%s → %s", localV, hubV))
	}
	eDiff := hubE - localE
	rDiff := hubR - localR
	if eDiff != 0 {
		parts = append(parts, fmt.Sprintf("%+d entity types", eDiff))
	}
	if rDiff != 0 {
		parts = append(parts, fmt.Sprintf("%+d relationships", rDiff))
	}
	if len(parts) == 0 {
		return "content changed"
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// routingSummary compares two routing YAML files and reports provider count changes.
func routingSummary(localPath, hubPath string) string {
	countProviders := func(path string) int {
		data, err := os.ReadFile(path)
		if err != nil {
			return 0
		}
		var doc map[string]interface{}
		if yaml.Unmarshal(data, &doc) != nil {
			return 0
		}
		providers, _ := doc["providers"].(map[string]interface{})
		return len(providers)
	}

	localP := countProviders(localPath)
	hubP := countProviders(hubPath)
	diff := hubP - localP
	if diff != 0 {
		return fmt.Sprintf("%+d provider(s)", diff)
	}
	return "content changed"
}

// Upgrade syncs managed files and upgrades installed components.
// If components is nil, syncs managed files AND upgrades all components.
// If components is non-nil, upgrades only named components (no managed file sync).
func (m *Manager) Upgrade(components []string) (*UpgradeReport, error) {
	report := &UpgradeReport{}

	// Sync managed files (only when upgrading everything)
	if components == nil {
		report.Files = m.syncManagedFilesWithReport()
	}

	// Upgrade components
	instances := m.Registry.List("")
	for _, inst := range instances {
		// If specific components requested, filter
		if components != nil && !containsStr(components, inst.Name) {
			continue
		}

		cached := m.findInCache(inst.Name, inst.Kind, "")
		if cached == nil {
			continue
		}

		// Read cached version
		if cached.Version == "" || cached.Version == inst.Version {
			report.Components = append(report.Components, ComponentUpgrade{
				Name:       inst.Name,
				Kind:       inst.Kind,
				OldVersion: inst.Version,
				NewVersion: cached.Version,
				Status:     "unchanged",
			})
			continue
		}

		// Copy new component YAML into instance dir
		instDir := m.Registry.InstanceDir(inst.Name)
		destPath := filepath.Join(instDir, inst.Kind+".yaml")
		data, err := os.ReadFile(cached.Path)
		if err != nil {
			report.Components = append(report.Components, ComponentUpgrade{
				Name:       inst.Name,
				Kind:       inst.Kind,
				OldVersion: inst.Version,
				NewVersion: cached.Version,
				Status:     "error",
				Error:      err.Error(),
			})
			continue
		}

		if err := os.WriteFile(destPath, data, 0644); err != nil {
			report.Components = append(report.Components, ComponentUpgrade{
				Name:       inst.Name,
				Kind:       inst.Kind,
				OldVersion: inst.Version,
				NewVersion: cached.Version,
				Status:     "error",
				Error:      err.Error(),
			})
			continue
		}

		// Re-validate if active (credential requirements may have changed)
		if inst.State == "active" {
			schema, err := ParseConfigSchema(data)
			if err == nil && schema != nil {
				cv, _ := ReadConfig(instDir)
				vals := map[string]string{}
				if cv != nil {
					vals = cv.Values
				}
				missing := schema.Validate(vals)
				if len(missing) > 0 {
					m.Registry.SetState(inst.Name, "needs_reconfiguration")
					var names []string
					for _, f := range missing {
						names = append(names, f.Name)
					}
					report.Components = append(report.Components, ComponentUpgrade{
						Name:       inst.Name,
						Kind:       inst.Kind,
						OldVersion: inst.Version,
						NewVersion: cached.Version,
						Status:     "error",
						Error:      "new required credential: " + strings.Join(names, ", "),
					})
					continue
				}
			}
		}

		// Update version in registry
		m.Registry.SetVersion(inst.Name, cached.Version)

		// Republish resolved YAML for active connectors so the intake
		// container picks up the new version on restart/rebuild.
		if inst.State == "active" && inst.Kind == "connector" {
			if resolved, err := m.Registry.ResolvedYAML(inst.Name); err == nil && resolved != nil {
				os.WriteFile(filepath.Join(instDir, "resolved.yaml"), resolved, 0644)
				connectorsDir := filepath.Join(m.Home, "connectors")
				os.MkdirAll(connectorsDir, 0755)
				os.WriteFile(filepath.Join(connectorsDir, inst.Name+".yaml"), resolved, 0644)
			}
		}

		report.Components = append(report.Components, ComponentUpgrade{
			Name:       inst.Name,
			Kind:       inst.Kind,
			OldVersion: inst.Version,
			NewVersion: cached.Version,
			Status:     "upgraded",
		})
	}

	// Regenerate swap config after any upgrade — component upgrades may change
	// egress domains or credential requirements (ASK Tenet 6: atomic constraint changes)
	WriteSwapConfig(m.Home) //nolint:errcheck

	return report, nil
}

// syncManagedFilesWithReport syncs hub-managed files and returns per-file status.
func (m *Manager) syncManagedFilesWithReport() []FileUpgrade {
	cacheDir := filepath.Join(m.Home, "hub-cache")
	var files []FileUpgrade

	// Snapshot hashes before sync
	ontologyPath := filepath.Join(m.Home, "knowledge", "base-ontology.yaml")
	routingPath := filepath.Join(m.Home, "infrastructure", "routing.yaml")

	ontologyBefore := fileHash(ontologyPath)
	routingBefore := fileHash(routingPath)

	// Sync services — snapshot hashes before
	svcDir := filepath.Join(m.Home, "registry", "services")
	svcBefore := map[string]string{}
	if entries, err := os.ReadDir(svcDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				svcBefore[e.Name()] = fileHash(filepath.Join(svcDir, e.Name()))
			}
		}
	}

	// Run the syncs
	if err := m.syncRouting(cacheDir); err != nil {
		files = append(files, FileUpgrade{Category: "routing", Path: routingPath, Status: "error", Summary: err.Error()})
	} else {
		routingAfter := fileHash(routingPath)
		status := "unchanged"
		if routingAfter != routingBefore {
			status = "upgraded"
		}
		summary := ""
		if status == "upgraded" {
			entries, _ := os.ReadDir(cacheDir)
			for _, e := range entries {
				if e.IsDir() {
					hubPath := filepath.Join(cacheDir, e.Name(), "pricing/routing.yaml")
					summary = routingSummary(routingPath, hubPath)
					break
				}
			}
		}
		files = append(files, FileUpgrade{Category: "routing", Path: routingPath, Status: status, Summary: summary})
	}

	if err := m.syncServices(cacheDir); err != nil {
		files = append(files, FileUpgrade{Category: "services", Path: svcDir, Status: "error", Summary: err.Error()})
	} else {
		// Diff services
		var changes []string
		svcAfter, _ := os.ReadDir(svcDir)
		for _, e := range svcAfter {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			after := fileHash(filepath.Join(svcDir, e.Name()))
			before := svcBefore[e.Name()]
			if before == "" {
				changes = append(changes, "+"+e.Name())
			} else if after != before {
				changes = append(changes, "~"+e.Name())
			}
		}
		status := "unchanged"
		if len(changes) > 0 {
			status = "upgraded"
		}
		files = append(files, FileUpgrade{Category: "services", Path: svcDir, Status: status, Summary: strings.Join(changes, ", ")})
	}

	if err := m.syncOntology(cacheDir); err != nil {
		files = append(files, FileUpgrade{Category: "ontology", Path: ontologyPath, Status: "error", Summary: err.Error()})
	} else {
		ontologyAfter := fileHash(ontologyPath)
		status := "unchanged"
		if ontologyAfter != ontologyBefore {
			status = "upgraded"
		}
		summary := ""
		if status == "upgraded" {
			entries, _ := os.ReadDir(cacheDir)
			for _, e := range entries {
				if e.IsDir() {
					hubPath := filepath.Join(cacheDir, e.Name(), "ontology/base-ontology.yaml")
					summary = ontologySummary(ontologyPath, hubPath)
					break
				}
			}
		}
		files = append(files, FileUpgrade{Category: "ontology", Path: ontologyPath, Status: status, Summary: summary})
	}

	return files
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// --- hub content validation ---

// validateServiceDefinition checks that a hub-synced service YAML has a safe
// api_base value. Rejects services where api_base is not HTTPS (except
// localhost HTTP for local services) or targets a blocked host. Logs a warning
// for unknown domains but does not reject — the egress proxy allowlist is the
// primary protection against credential exfiltration.
func validateServiceDefinition(filename string, data []byte) error {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}

	apiBase, _ := doc["api_base"].(string)
	if apiBase == "" {
		return nil // no api_base means no credentialed traffic risk
	}

	// Expand ${...} placeholders to dummy values so url.Parse can validate
	// the URL structure. Services like Jira use ${JIRA_DOMAIN} in api_base
	// which is resolved at runtime from operator env vars.
	expandedBase := os.Expand(apiBase, func(key string) string { return "placeholder" })
	parsed, err := url.Parse(expandedBase)
	if err != nil {
		return fmt.Errorf("api_base is not a valid URL: %s", apiBase)
	}

	host := parsed.Hostname()

	// Block cloud metadata endpoints
	if isBlockedHost(host) {
		return fmt.Errorf("api_base targets blocked host: %s", host)
	}

	// Require HTTPS for non-localhost services
	isLocal := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if parsed.Scheme != "https" && !isLocal {
		return fmt.Errorf("api_base must use HTTPS for non-localhost services, got %s://%s", parsed.Scheme, host)
	}

	// Warn about unknown domains (but allow — operator may have custom services)
	if !isLocal && !isAllowedAPIDomain(host) {
		log.Printf("[hub] WARNING: service %s uses unrecognized api_base domain: %s (egress proxy allowlist is the primary control)", filename, host)
	}

	return nil
}

// validateRoutingConfig parses a routing.yaml, strips providers with unknown
// auth_env values, and warns about unknown api_base domains. Returns the
// (possibly modified) YAML bytes.
func validateRoutingConfig(data []byte) ([]byte, error) {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	providers, _ := doc["providers"].(map[string]interface{})
	if providers == nil {
		return data, nil
	}

	modified := false
	for name, v := range providers {
		pm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}

		// Validate auth_env — strip entries with unknown env var names
		authEnv, _ := pm["auth_env"].(string)
		if authEnv != "" && !allowedAuthEnvVars[authEnv] {
			log.Printf("[hub] SECURITY: stripping routing provider %q — unknown auth_env %q could leak secrets", name, authEnv)
			delete(providers, name)
			modified = true
			continue
		}

		// Validate api_base (expand ${...} placeholders for URL parsing)
		apiBase, _ := pm["api_base"].(string)
		if apiBase != "" {
			expandedAPIBase := os.Expand(apiBase, func(key string) string { return "placeholder" })
			parsed, err := url.Parse(expandedAPIBase)
			if err != nil {
				log.Printf("[hub] SECURITY: stripping routing provider %q — invalid api_base: %s", name, apiBase)
				delete(providers, name)
				modified = true
				continue
			}

			host := parsed.Hostname()
			if isBlockedHost(host) {
				log.Printf("[hub] SECURITY: stripping routing provider %q — api_base targets blocked host: %s", name, host)
				delete(providers, name)
				modified = true
				continue
			}

			isLocal := host == "localhost" || host == "127.0.0.1" || host == "::1"

			// Credentialed providers (non-empty auth_env) must use HTTPS
			if authEnv != "" && parsed.Scheme != "https" && !isLocal {
				log.Printf("[hub] SECURITY: stripping routing provider %q — credentialed provider must use HTTPS, got %s://%s", name, parsed.Scheme, host)
				delete(providers, name)
				modified = true
				continue
			}

			if !isLocal && !isAllowedAPIDomain(host) {
				log.Printf("[hub] WARNING: routing provider %q uses unrecognized api_base domain: %s (egress proxy allowlist is the primary control)", name, host)
			}
		}
	}

	if modified {
		return yaml.Marshal(doc)
	}
	return data, nil
}

// isAllowedAPIDomain checks if a hostname matches one of the known-good
// API provider domains (exact match or subdomain match).
func isAllowedAPIDomain(host string) bool {
	if allowedAPIDomains[host] {
		return true
	}
	// Check subdomain match (e.g., "us.api.anthropic.com" matches "api.anthropic.com")
	for domain := range allowedAPIDomains {
		if strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// blockedHosts are cloud metadata and other dangerous endpoints that must
// never appear as API base URLs in hub-synced content.
var blockedHosts = map[string]bool{
	"169.254.169.254":          true, // AWS/GCP metadata
	"metadata.google.internal": true,
	"100.100.100.200":          true, // Alibaba metadata
	"0.0.0.0":                  true,
}

func isBlockedHost(host string) bool {
	return blockedHosts[host]
}

// --- internal helpers ---

func (m *Manager) discover() []Component {
	cfg := m.loadConfig()
	cacheDir := filepath.Join(m.Home, "hub-cache")
	var results []Component

	for _, src := range cfg.Hub.Sources {
		srcDir := filepath.Join(cacheDir, src.Name)
		if _, err := os.Stat(srcDir); err != nil {
			continue
		}
		for _, kind := range KnownKinds {
			kindDir := filepath.Join(srcDir, cacheDirNameForKind(kind))
			if _, err := os.Stat(kindDir); err != nil {
				continue
			}
			filepath.Walk(kindDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if info.IsDir() {
					switch {
					case path != kindDir && fileExists(filepath.Join(path, "package.yaml")):
						if component, ok := discoverPackage(path, src.Name, kind); ok {
							results = append(results, component)
						}
						return filepath.SkipDir
					case path != kindDir && fileExists(filepath.Join(path, "connector.yaml")):
						if component, ok := discoverLegacyConnector(path, src.Name, kind); ok {
							results = append(results, component)
						}
						return filepath.SkipDir
					default:
						return nil
					}
				}
				if !isDiscoverableComponentFile(kind, info.Name()) {
					return nil
				}
				// Skip metadata files — they're CI-stamped, not components
				if info.Name() == "metadata.yaml" {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return nil
				}
				var doc map[string]interface{}
				if yaml.Unmarshal(data, &doc) != nil {
					return nil
				}
				name, _ := doc["name"].(string)
				if name == "" {
					name, _ = doc["service"].(string)
				}
				if name == "" {
					name, _ = doc["provider"].(string)
				}
				if name == "" {
					return nil
				}
				desc, _ := doc["description"].(string)
				version, _ := doc["version"].(string)
				license, _ := doc["license"].(string)
				author, _ := doc["author"].(string)
				results = append(results, Component{
					Name:        name,
					Kind:        kind,
					Version:     version,
					Description: desc,
					License:     license,
					Author:      author,
					Source:      src.Name,
					Path:        path,
				})
				return nil
			})
		}
	}
	return results
}

func discoverPackage(dir, source, fallbackKind string) (Component, bool) {
	var cfg models.PackageConfig
	if err := models.Load(filepath.Join(dir, "package.yaml"), &cfg); err != nil {
		return Component{}, false
	}

	kind := cfg.Kind
	if kind == "" {
		kind = fallbackKind
	}

	return Component{
		Name:        cfg.Metadata.Name,
		Kind:        kind,
		Version:     cfg.Metadata.Version,
		Description: cfg.Metadata.Title,
		Source:      source,
		Path:        filepath.Join(dir, "package.yaml"),
	}, true
}

func discoverLegacyConnector(dir, source, fallbackKind string) (Component, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "connector.yaml"))
	if err != nil {
		return Component{}, false
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Component{}, false
	}

	name, _ := doc["name"].(string)
	if name == "" {
		name, _ = doc["connector"].(string)
	}
	if name == "" {
		name = filepath.Base(dir)
	}

	kind, _ := doc["kind"].(string)
	if kind == "" {
		kind = fallbackKind
	}

	return Component{
		Name:        name,
		Kind:        kind,
		Version:     stringValue(doc["version"]),
		Description: stringValue(doc["description"]),
		Author:      stringValue(doc["author"]),
		License:     stringValue(doc["license"]),
		Source:      source,
		Path:        filepath.Join(dir, "connector.yaml"),
	}, true
}

func stringValue(v interface{}) string {
	s, _ := v.(string)
	return s
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func cacheDirNameForKind(kind string) string {
	if kind == "setup" {
		return "setup"
	}
	return kind + "s"
}

func isDiscoverableComponentFile(kind, name string) bool {
	if name == "metadata.yaml" {
		return false
	}
	if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
		return true
	}
	return kind == "skill" && strings.EqualFold(name, "SKILL.md")
}

// FindInCache returns the first cached component matching name, kind, and source.
func (m *Manager) FindInCache(name, kind, source string) *Component {
	return m.findInCache(name, kind, source)
}

func (m *Manager) findAllInCache(name, source string) []Component {
	all := m.discover()
	var matches []Component
	for _, c := range all {
		if c.Name == name {
			if source != "" && c.Source != source {
				continue
			}
			matches = append(matches, c)
		}
	}
	return matches
}

func (m *Manager) findInCache(name, kind, source string) *Component {
	all := m.discover()
	for _, c := range all {
		if c.Name == name {
			if kind != "" && c.Kind != kind {
				continue
			}
			if source != "" && c.Source != source {
				continue
			}
			return &c
		}
	}
	return nil
}

func (m *Manager) syncSource(src Source, cacheDir string) error {
	dest := filepath.Join(cacheDir, src.Name)
	gitDir := filepath.Join(dest, ".git")

	if _, err := os.Stat(gitDir); err == nil {
		cmd := exec.Command("git", "-C", dest,
			"-c", "core.hooksPath=/dev/null",
			"-c", "protocol.ext.allow=never",
			"pull", "--ff-only")
		out, err := cmd.CombinedOutput()
		if err != nil {
			os.RemoveAll(dest)
		} else {
			_ = out
			return nil
		}
	}

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
		return fmt.Errorf("clone failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// fileHash returns the hex-encoded SHA-256 hash of a file's contents.
// Returns an empty string if the file cannot be read.
func fileHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// gitHeadCommit returns the short HEAD commit hash for a git repo.
// Uses core.hooksPath=/dev/null for consistency with syncSource security posture.
func gitHeadCommit(repoDir string) string {
	cmd := exec.Command("git", "-C", repoDir, "-c", "core.hooksPath=/dev/null", "rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitCommitCount returns the number of commits between two refs.
// Uses core.hooksPath=/dev/null for consistency with syncSource security posture.
func gitCommitCount(repoDir, oldRef, newRef string) int {
	if oldRef == "" || newRef == "" || oldRef == newRef {
		return 0
	}
	cmd := exec.Command("git", "-C", repoDir, "-c", "core.hooksPath=/dev/null", "rev-list", "--count", oldRef+".."+newRef)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	var count int
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &count)
	return count
}

// syncSourceWithReport syncs a hub source and returns a SourceUpdate with commit diff info.
func (m *Manager) syncSourceWithReport(src Source, cacheDir string) (SourceUpdate, error) {
	dest := filepath.Join(cacheDir, src.Name)
	oldCommit := gitHeadCommit(dest)

	err := m.syncSource(src, cacheDir)

	newCommit := gitHeadCommit(dest)
	su := SourceUpdate{
		Name:      src.Name,
		OldCommit: oldCommit,
		NewCommit: newCommit,
	}
	if oldCommit != "" && newCommit != "" && oldCommit != newCommit {
		su.CommitCount = gitCommitCount(dest, oldCommit, newCommit)
	}
	return su, err
}

// migrateDefaultSourceToOCI checks if the official agency-hub source is still
// git-based and migrates it to OCI. Returns true if migration occurred.
func (m *Manager) migrateDefaultSourceToOCI() bool {
	cfgPath := filepath.Join(m.Home, "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return false
	}

	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil {
		return false
	}

	sourcesNode := hubSourcesNode(&doc)
	if sourcesNode == nil || sourcesNode.Kind != yaml.SequenceNode {
		return false
	}

	migrated := false
	for _, sourceNode := range sourcesNode.Content {
		var src Source
		if sourceNode.Decode(&src) != nil {
			continue
		}
		if isLegacyOfficialHubSource(src) {
			migratedSource := DefaultSource
			migratedSource.Name = src.Name
			if replaceSourceNode(sourceNode, migratedSource) {
				migrated = true
			}
		}
	}

	if !migrated {
		return false
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return false
	}
	os.WriteFile(cfgPath, out, 0644)
	return true
}

func hubSourcesNode(doc *yaml.Node) *yaml.Node {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}

	hubNode := mappingValueNode(root, "hub")
	if hubNode == nil || hubNode.Kind != yaml.MappingNode {
		return nil
	}
	return mappingValueNode(hubNode, "sources")
}

func mappingValueNode(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func replaceSourceNode(node *yaml.Node, src Source) bool {
	data, err := yaml.Marshal(src)
	if err != nil {
		return false
	}

	var replacement yaml.Node
	if err := yaml.Unmarshal(data, &replacement); err != nil {
		return false
	}
	if replacement.Kind == yaml.DocumentNode && len(replacement.Content) > 0 {
		*node = *replacement.Content[0]
		return true
	}
	return false
}

func isLegacyOfficialHubSource(src Source) bool {
	if src.EffectiveType() != "git" {
		return false
	}
	if src.Name != "official" && src.Name != "default" {
		return false
	}
	return strings.Contains(src.URL, "agency-hub")
}

func pruneLegacyOfficialCache(cacheDir string, sources []Source) error {
	for _, src := range sources {
		if src.Name == "official" {
			return nil
		}
	}

	officialDir := filepath.Join(cacheDir, "official")
	gitConfig := filepath.Join(officialDir, ".git", "config")
	data, err := os.ReadFile(gitConfig)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("official: inspect legacy hub cache: %w", err)
	}
	if !strings.Contains(string(data), "github.com/geoffbelknap/agency-hub") {
		return nil
	}
	if err := os.RemoveAll(officialDir); err != nil {
		return fmt.Errorf("official: remove legacy hub cache: %w", err)
	}
	return nil
}

func (m *Manager) loadConfig() hubConfig {
	var cfg hubConfig
	data, err := os.ReadFile(filepath.Join(m.Home, "config.yaml"))
	if err != nil {
		cfg.Hub.Sources = []Source{DefaultSource}
		return cfg
	}
	yaml.Unmarshal(data, &cfg)

	// If no sources configured and config doesn't explicitly set an empty list,
	// use the default OCI source. An explicit "sources: []" means "no sources."
	if len(cfg.Hub.Sources) == 0 && !strings.Contains(string(data), "sources:") {
		cfg.Hub.Sources = []Source{DefaultSource}
	}

	return cfg
}

// findSourceByName returns the Source config for a given source name.
func (m *Manager) findSourceByName(name string) *Source {
	cfg := m.loadConfig()
	for _, src := range cfg.Hub.Sources {
		if src.Name == name {
			return &src
		}
	}
	return nil
}

func (m *Manager) provenancePath() string {
	return filepath.Join(m.Home, "hub-installed.json")
}

func (m *Manager) loadProvenance() []Provenance {
	data, err := os.ReadFile(m.provenancePath())
	if err != nil {
		return nil
	}
	var entries []Provenance
	json.Unmarshal(data, &entries)
	return entries
}

func (m *Manager) saveProvenance(entries []Provenance) {
	data, _ := json.MarshalIndent(entries, "", "  ")
	os.WriteFile(m.provenancePath(), data, 0644)
}

func (m *Manager) addProvenance(p Provenance) {
	entries := m.loadProvenance()
	// Replace existing entry for same name+kind
	var updated []Provenance
	for _, e := range entries {
		if e.Name == p.Name && e.Kind == p.Kind {
			continue
		}
		updated = append(updated, e)
	}
	updated = append(updated, p)
	m.saveProvenance(updated)
}

func (m *Manager) removeProvenance(name, kind string) {
	entries := m.loadProvenance()
	var updated []Provenance
	for _, e := range entries {
		if e.Name == name && e.Kind == kind {
			continue
		}
		updated = append(updated, e)
	}
	m.saveProvenance(updated)
}

func isValidKind(kind string) bool {
	for _, k := range KnownKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// IsInstallableKind reports whether a hub kind can be installed as an operator
// component instance. Managed kinds are synced by hub update/upgrade instead.
func IsInstallableKind(kind string) bool {
	return isValidKind(kind) && !nonInstallableKinds[kind]
}
