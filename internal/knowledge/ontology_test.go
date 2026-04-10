package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOntology(t *testing.T) {
	// Create temp home
	home := t.TempDir()
	knowledgeDir := filepath.Join(home, "knowledge")
	os.MkdirAll(knowledgeDir, 0755)

	// Write a minimal base ontology
	base := `
version: 1
name: default
description: Test ontology
entity_types:
  person:
    description: A person
    attributes: [name, role]
  fact:
    description: A fact
    attributes: [description]
relationship_types:
  owns:
    description: Has ownership of
    inverse: owned_by
changelog:
  - version: 1
    date: "2026-03-24"
    changes: Initial
`
	os.WriteFile(filepath.Join(knowledgeDir, "base-ontology.yaml"), []byte(base), 0644)

	cfg, err := LoadOntology(home)
	if err != nil {
		t.Fatalf("LoadOntology: %v", err)
	}

	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}
	if len(cfg.EntityTypes) != 2 {
		t.Errorf("expected 2 entity types, got %d", len(cfg.EntityTypes))
	}
	if len(cfg.RelationshipTypes) != 1 {
		t.Errorf("expected 1 relationship type, got %d", len(cfg.RelationshipTypes))
	}
	if cfg.EntityTypes["person"].Description != "A person" {
		t.Errorf("wrong person description: %s", cfg.EntityTypes["person"].Description)
	}
}

func TestLoadOntologyUsesEmbeddedDefaultWhenBaseMissing(t *testing.T) {
	home := t.TempDir()

	cfg, err := LoadOntology(home)
	if err != nil {
		t.Fatalf("LoadOntology: %v", err)
	}

	if cfg.Name != "default" {
		t.Fatalf("expected default ontology, got %q", cfg.Name)
	}
	if len(cfg.EntityTypes) == 0 {
		t.Fatal("expected embedded entity types")
	}
	if len(cfg.RelationshipTypes) == 0 {
		t.Fatal("expected embedded relationship types")
	}
}

func TestEnsureBaseOntologyWritesMissingDefault(t *testing.T) {
	home := t.TempDir()
	if err := EnsureBaseOntology(home); err != nil {
		t.Fatalf("EnsureBaseOntology: %v", err)
	}

	path := filepath.Join(home, "knowledge", "base-ontology.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("base ontology not written: %v", err)
	}

	cfg, err := LoadOntology(home)
	if err != nil {
		t.Fatalf("LoadOntology: %v", err)
	}
	if cfg.Name != "default" {
		t.Fatalf("expected default ontology, got %q", cfg.Name)
	}
}

func TestLoadOntologyWithExtension(t *testing.T) {
	home := t.TempDir()
	knowledgeDir := filepath.Join(home, "knowledge")
	extDir := filepath.Join(knowledgeDir, "ontology.d")
	os.MkdirAll(extDir, 0755)

	base := `
version: 1
name: default
entity_types:
  person:
    description: A person
    attributes: [name]
relationship_types:
  owns:
    description: Owns
    inverse: owned_by
`
	os.WriteFile(filepath.Join(knowledgeDir, "base-ontology.yaml"), []byte(base), 0644)

	ext := `
name: security
kind: ontology
extends: default
entity_types:
  vulnerability:
    description: A security weakness
    attributes: [cve, severity]
relationship_types:
  mitigates:
    description: Mitigates
    inverse: mitigated_by
`
	os.WriteFile(filepath.Join(extDir, "security.yaml"), []byte(ext), 0644)

	cfg, err := LoadOntology(home)
	if err != nil {
		t.Fatalf("LoadOntology with extension: %v", err)
	}

	if len(cfg.EntityTypes) != 2 {
		t.Errorf("expected 2 entity types, got %d", len(cfg.EntityTypes))
	}
	if _, ok := cfg.EntityTypes["vulnerability"]; !ok {
		t.Error("vulnerability type not merged")
	}
	if len(cfg.RelationshipTypes) != 2 {
		t.Errorf("expected 2 relationship types, got %d", len(cfg.RelationshipTypes))
	}
	if cfg.Version != 2 {
		t.Errorf("expected version 2 after extension merge, got %d", cfg.Version)
	}
}

func TestLoadOntologyExtensionConflict(t *testing.T) {
	home := t.TempDir()
	knowledgeDir := filepath.Join(home, "knowledge")
	extDir := filepath.Join(knowledgeDir, "ontology.d")
	os.MkdirAll(extDir, 0755)

	base := `
version: 1
name: default
entity_types:
  person:
    description: A person
    attributes: [name]
relationship_types: {}
`
	os.WriteFile(filepath.Join(knowledgeDir, "base-ontology.yaml"), []byte(base), 0644)

	ext := `
name: conflicting
extends: default
entity_types:
  person:
    description: Duplicate person
    attributes: [name]
`
	os.WriteFile(filepath.Join(extDir, "conflicting.yaml"), []byte(ext), 0644)

	_, err := LoadOntology(home)
	if err == nil {
		t.Error("expected conflict error, got nil")
	}
}

func TestWriteOntology(t *testing.T) {
	home := t.TempDir()
	cfg := &OntologyConfig{
		Version: 1,
		Name:    "test",
		EntityTypes: map[string]EntityType{
			"fact": {Description: "A fact", Attributes: []string{"description"}},
		},
		RelationshipTypes: map[string]RelationshipType{
			"owns": {Description: "Owns", Inverse: "owned_by"},
		},
	}

	if err := WriteOntology(home, cfg); err != nil {
		t.Fatalf("WriteOntology: %v", err)
	}

	path := filepath.Join(home, "knowledge", "ontology.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("ontology.yaml not created: %v", err)
	}
}

func TestWriteOntologyRepairsEmptyDirectoryPath(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "knowledge", "ontology.yaml")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir ontology path: %v", err)
	}

	cfg := &OntologyConfig{
		Version:           1,
		Name:              "test",
		EntityTypes:       map[string]EntityType{"fact": {Description: "A fact"}},
		RelationshipTypes: map[string]RelationshipType{},
	}

	if err := WriteOntology(home, cfg); err != nil {
		t.Fatalf("WriteOntology: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("ontology.yaml not created: %v", err)
	}
	if info.IsDir() {
		t.Fatal("ontology.yaml is still a directory")
	}
}

func TestWriteOntologyDoesNotRemoveNonEmptyDirectoryPath(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "knowledge", "ontology.yaml")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir ontology path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "keep.txt"), []byte("operator data"), 0644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	cfg := &OntologyConfig{
		Version:           1,
		Name:              "test",
		EntityTypes:       map[string]EntityType{"fact": {Description: "A fact"}},
		RelationshipTypes: map[string]RelationshipType{},
	}

	err := WriteOntology(home, cfg)
	if err == nil {
		t.Fatal("expected error for non-empty ontology directory")
	}
	if !strings.Contains(err.Error(), "non-empty directory") {
		t.Fatalf("expected non-empty directory error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, "keep.txt")); err != nil {
		t.Fatalf("nested file was removed: %v", err)
	}
}

func TestValidateNode(t *testing.T) {
	ontology := &OntologyConfig{
		EntityTypes: map[string]EntityType{
			"person":   {Description: "A person"},
			"fact":     {Description: "A fact"},
			"finding":  {Description: "A finding"},
			"system":   {Description: "A system"},
			"concept":  {Description: "A concept"},
			"incident": {Description: "An incident"},
			"decision": {Description: "A decision"},
		},
	}

	tests := []struct {
		input    string
		expected string
		changed  bool
	}{
		{"person", "person", false},
		{"fact", "fact", false},
		{"Person", "person", true},
		{"agent", "system", true},
		{"topic", "concept", true},
		{"observation", "finding", true},
		{"bug", "incident", true},
		{"widget_xyz", "fact", true},
		{"", "fact", true},
	}

	for _, tt := range tests {
		result, changed := ValidateNode(tt.input, ontology)
		if result != tt.expected {
			t.Errorf("ValidateNode(%q) = %q, want %q", tt.input, result, tt.expected)
		}
		if changed != tt.changed {
			t.Errorf("ValidateNode(%q) changed=%v, want %v", tt.input, changed, tt.changed)
		}
	}
}

func TestValidateNodeNilOntology(t *testing.T) {
	result, changed := ValidateNode("anything", nil)
	if result != "fact" {
		t.Errorf("expected fact fallback with nil ontology, got %q", result)
	}
	if !changed {
		t.Error("expected changed=true with nil ontology")
	}
}

func TestEntityTypeNames(t *testing.T) {
	ontology := &OntologyConfig{
		EntityTypes: map[string]EntityType{
			"zebra": {},
			"alpha": {},
			"mid":   {},
		},
	}
	names := EntityTypeNames(ontology)
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "alpha" || names[1] != "mid" || names[2] != "zebra" {
		t.Errorf("names not sorted: %v", names)
	}
}

func TestRelationshipTypeNames(t *testing.T) {
	ontology := &OntologyConfig{
		RelationshipTypes: map[string]RelationshipType{
			"z_rel": {},
			"a_rel": {},
		},
	}
	names := RelationshipTypeNames(ontology)
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if names[0] != "a_rel" {
		t.Errorf("expected a_rel first, got %s", names[0])
	}
}
