package runtime

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Store struct {
	instanceDir string
}

func NewStore(instanceDir string) *Store {
	return &Store{instanceDir: instanceDir}
}

func (s *Store) SaveManifest(m *Manifest) error {
	if err := os.MkdirAll(filepath.Dir(s.manifestPath()), 0o755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(s.manifestPath(), data, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func (s *Store) LoadManifest() (*Manifest, error) {
	data, err := os.ReadFile(s.manifestPath())
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

func (s *Store) SaveAuthorityConfig(node RuntimeNode) error {
	path := filepath.Join(s.runtimeRoot(), node.Materialization)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create authority dir: %w", err)
	}
	data, err := yaml.Marshal(node)
	if err != nil {
		return fmt.Errorf("marshal authority config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write authority config: %w", err)
	}
	return nil
}

func (s *Store) manifestPath() string {
	return filepath.Join(s.runtimeRoot(), "manifest.yaml")
}

func (s *Store) runtimeRoot() string {
	return filepath.Join(s.instanceDir, "runtime")
}
