package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// EntityType defines a kind of entity in the knowledge graph.
type EntityType struct {
	Description string   `yaml:"description" json:"description"`
	Attributes  []string `yaml:"attributes" json:"attributes"`
}

// RelationshipType defines a kind of relationship between entities.
type RelationshipType struct {
	Description string `yaml:"description" json:"description"`
	Inverse     string `yaml:"inverse" json:"inverse"`
}

// ChangelogEntry records a version change in the ontology.
type ChangelogEntry struct {
	Version int    `yaml:"version" json:"version"`
	Date    string `yaml:"date" json:"date"`
	Changes string `yaml:"changes" json:"changes"`
}

// OntologyConfig is the full ontology definition.
type OntologyConfig struct {
	Version           int                         `yaml:"version" json:"version"`
	Name              string                      `yaml:"name" json:"name"`
	Description       string                      `yaml:"description,omitempty" json:"description,omitempty"`
	LastModified      string                      `yaml:"last_modified,omitempty" json:"last_modified,omitempty"`
	EntityTypes       map[string]EntityType        `yaml:"entity_types" json:"entity_types"`
	RelationshipTypes map[string]RelationshipType  `yaml:"relationship_types" json:"relationship_types"`
	Changelog         []ChangelogEntry             `yaml:"changelog,omitempty" json:"changelog,omitempty"`
}

// OntologyExtension is a hub-distributed ontology extension.
type OntologyExtension struct {
	Name              string                      `yaml:"name"`
	Kind              string                      `yaml:"kind"`
	Description       string                      `yaml:"description,omitempty"`
	Extends           string                      `yaml:"extends"`
	EntityTypes       map[string]EntityType        `yaml:"entity_types,omitempty"`
	RelationshipTypes map[string]RelationshipType  `yaml:"relationship_types,omitempty"`
}

// LoadOntology reads the base ontology and merges any extensions from ontology.d/.
// Returns the merged ontology config.
func LoadOntology(home string) (*OntologyConfig, error) {
	knowledgeDir := filepath.Join(home, "knowledge")
	basePath := filepath.Join(knowledgeDir, "base-ontology.yaml")

	data, err := os.ReadFile(basePath)
	if err != nil {
		return nil, fmt.Errorf("read base ontology: %w", err)
	}

	var cfg OntologyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse base ontology: %w", err)
	}

	// Merge extensions from ontology.d/
	extDir := filepath.Join(knowledgeDir, "ontology.d")
	entries, err := os.ReadDir(extDir)
	if err != nil {
		// No extensions directory is fine
		return &cfg, nil
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		extPath := filepath.Join(extDir, entry.Name())
		extData, err := os.ReadFile(extPath)
		if err != nil {
			continue
		}

		var ext OntologyExtension
		if err := yaml.Unmarshal(extData, &ext); err != nil {
			continue
		}

		if ext.Extends != "" && ext.Extends != cfg.Name {
			continue // Extension doesn't match base ontology
		}

		// Merge entity types (extensions can only add, not modify)
		for k, v := range ext.EntityTypes {
			if _, exists := cfg.EntityTypes[k]; exists {
				return nil, fmt.Errorf("ontology extension %q conflicts: entity type %q already defined", ext.Name, k)
			}
			cfg.EntityTypes[k] = v
		}

		// Merge relationship types
		for k, v := range ext.RelationshipTypes {
			if _, exists := cfg.RelationshipTypes[k]; exists {
				return nil, fmt.Errorf("ontology extension %q conflicts: relationship type %q already defined", ext.Name, k)
			}
			cfg.RelationshipTypes[k] = v
		}

		// Add changelog entry
		cfg.Version++
		cfg.LastModified = time.Now().UTC().Format(time.RFC3339)
		cfg.Changelog = append([]ChangelogEntry{{
			Version: cfg.Version,
			Date:    time.Now().UTC().Format("2006-01-02"),
			Changes: fmt.Sprintf("Added %s extension (%d entity types, %d relationship types)",
				ext.Name, len(ext.EntityTypes), len(ext.RelationshipTypes)),
		}}, cfg.Changelog...)
	}

	return &cfg, nil
}

// WriteOntology writes the merged ontology to ~/.agency/knowledge/ontology.yaml.
func WriteOntology(home string, cfg *OntologyConfig) error {
	knowledgeDir := filepath.Join(home, "knowledge")
	os.MkdirAll(knowledgeDir, 0755)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal ontology: %w", err)
	}

	return os.WriteFile(filepath.Join(knowledgeDir, "ontology.yaml"), data, 0644)
}

// ValidateNode checks if a kind string matches the ontology. Returns:
//   - The corrected kind if exact or fuzzy match found
//   - "fact" as fallback for unknown kinds
//   - A boolean indicating if the kind was changed
func ValidateNode(kind string, ontology *OntologyConfig) (string, bool) {
	if ontology == nil || kind == "" {
		return "fact", kind != "fact"
	}

	lower := strings.ToLower(kind)

	// Exact match
	if _, ok := ontology.EntityTypes[lower]; ok {
		return lower, lower != kind
	}

	// Common aliases / fuzzy matches
	aliases := map[string]string{
		"agent":       "system",
		"application": "system",
		"app":         "software",
		"platform":    "system",
		"database":    "system",
		"repository":  "system",
		"repo":        "system",
		"topic":       "concept",
		"idea":        "concept",
		"notion":      "concept",
		"observation": "finding",
		"discovery":   "finding",
		"insight":     "finding",
		"issue":       "incident",
		"bug":         "incident",
		"problem":     "incident",
		"choice":      "decision",
		"resolution_decision": "decision",
		"company":     "organization",
		"org":         "organization",
		"vendor":      "organization",
		"department":  "organization",
		"member":      "person",
		"user":        "person",
		"operator":    "person",
		"customer":    "person",
		"workflow":    "process",
		"runbook":     "process",
		"sop":         "process",
		"ticket":      "task",
		"pr":          "task",
		"pull_request": "task",
		"meeting":     "event",
		"deadline":    "event",
		"release":     "event",
		"milestone":   "event",
		"fix":         "resolution",
		"patch":       "resolution",
		"hotfix":      "resolution",
		"hack":        "workaround",
		"temp_fix":    "workaround",
		"doc":         "document",
		"spec":        "document",
		"report":      "document",
		"wiki":        "document",
		"policy":      "rule",
		"kpi":         "metric",
		"sla":         "metric",
		"link":        "url",
		"reference":   "url",
		"file":        "artifact",
		"dashboard":   "artifact",
		"api":         "service",
		"endpoint":    "service",
		"term":        "terminology",
		"jargon":      "terminology",
		"concern":     "risk",
		"threat":      "risk",
		"note":        "fact",
		"info":        "fact",
		"information": "fact",
		"data":        "fact",
		// Asset inventory aliases
		"package":   "software",
		"library":   "software",
		"firmware":  "software",
		"binary":    "software",
		"config":    "config_item",
		"setting":   "config_item",
		"parameter": "config_item",
		"behavior":  "behavior_pattern",
		"pattern":   "behavior_pattern",
	}

	if mapped, ok := aliases[lower]; ok {
		return mapped, true
	}

	// Check for substring match against defined types
	for typeName := range ontology.EntityTypes {
		if strings.Contains(lower, typeName) || strings.Contains(typeName, lower) {
			return typeName, true
		}
	}

	// Fallback to fact
	return "fact", true
}

// EntityTypeNames returns a sorted list of entity type names.
func EntityTypeNames(ontology *OntologyConfig) []string {
	if ontology == nil {
		return nil
	}
	names := make([]string, 0, len(ontology.EntityTypes))
	for k := range ontology.EntityTypes {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// RelationshipTypeNames returns a sorted list of relationship type names.
func RelationshipTypeNames(ontology *OntologyConfig) []string {
	if ontology == nil {
		return nil
	}
	names := make([]string, 0, len(ontology.RelationshipTypes))
	for k := range ontology.RelationshipTypes {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
