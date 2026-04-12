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

func (s *Store) SaveIngressConfig(node RuntimeNode) error {
	if node.Ingress == nil {
		return fmt.Errorf("ingress node %q missing ingress spec", node.NodeID)
	}
	path := filepath.Join(s.runtimeRoot(), node.Materialization)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create ingress dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(node.Ingress.ConnectorYAML), 0o644); err != nil {
		return fmt.Errorf("write ingress config: %w", err)
	}
	return nil
}

func (s *Store) SaveNodeStatus(status NodeStatus) error {
	path := s.nodeStatusPath(status.NodeID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create node status dir: %w", err)
	}
	data, err := yaml.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshal node status: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write node status: %w", err)
	}
	return nil
}

func (s *Store) LoadNodeStatus(nodeID string) (*NodeStatus, error) {
	data, err := os.ReadFile(s.nodeStatusPath(nodeID))
	if err != nil {
		return nil, fmt.Errorf("read node status: %w", err)
	}
	var status NodeStatus
	if err := yaml.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("parse node status: %w", err)
	}
	return &status, nil
}

func (s *Store) ListNodeStatuses() ([]NodeStatus, error) {
	root := filepath.Join(s.runtimeRoot(), "status")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read node status dir: %w", err)
	}
	out := make([]NodeStatus, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		status, err := s.LoadNodeStatus(entry.Name()[:len(entry.Name())-len(".yaml")])
		if err != nil {
			return nil, err
		}
		out = append(out, *status)
	}
	return out, nil
}

func (s *Store) manifestPath() string {
	return filepath.Join(s.runtimeRoot(), "manifest.yaml")
}

func (s *Store) nodeStatusPath(nodeID string) string {
	return filepath.Join(s.runtimeRoot(), "status", nodeID+".yaml")
}

func (s *Store) runtimeRoot() string {
	return filepath.Join(s.instanceDir, "runtime")
}

func (s *Store) InstanceDir() string {
	return s.instanceDir
}
