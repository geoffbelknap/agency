package instances

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Store struct {
	root string
}

func NewStore(root string) *Store {
	return &Store{root: root}
}

func (s *Store) Create(_ context.Context, inst *Instance) error {
	if err := ValidateInstance(inst); err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return fmt.Errorf("create instance root: %w", err)
	}
	path, err := s.instanceFile(inst.ID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("instance %q already exists", inst.ID)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat instance: %w", err)
	}

	now := time.Now().UTC()
	if inst.CreatedAt.IsZero() {
		inst.CreatedAt = now
	}
	inst.UpdatedAt = now
	return s.write(inst)
}

func (s *Store) Claim(ctx context.Context, id, owner string) error {
	if strings.TrimSpace(owner) == "" {
		return fmt.Errorf("owner is required")
	}
	return s.Update(ctx, id, func(inst *Instance) error {
		if inst.Claim != nil && inst.Claim.Owner != owner {
			return fmt.Errorf("instance %q already claimed by %q", id, inst.Claim.Owner)
		}
		if inst.Claim == nil {
			inst.Claim = &Claim{}
		}
		inst.Claim.Owner = owner
		inst.Claim.ClaimedAt = time.Now().UTC()
		return nil
	})
}

func (s *Store) Release(ctx context.Context, id string) error {
	return s.Update(ctx, id, func(inst *Instance) error {
		inst.Claim = nil
		return nil
	})
}

func (s *Store) Get(_ context.Context, id string) (*Instance, error) {
	path, err := s.instanceFile(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read instance %q: %w", id, err)
	}
	var inst Instance
	if err := yaml.Unmarshal(data, &inst); err != nil {
		return nil, fmt.Errorf("parse instance %q: %w", id, err)
	}
	return &inst, nil
}

func (s *Store) List(ctx context.Context) ([]*Instance, error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read instance root: %w", err)
	}

	items := make([]*Instance, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		inst, err := s.Get(ctx, entry.Name())
		if err != nil {
			return nil, err
		}
		items = append(items, inst)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (s *Store) Update(ctx context.Context, id string, fn func(*Instance) error) error {
	inst, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := fn(inst); err != nil {
		return err
	}
	if err := ValidateInstance(inst); err != nil {
		return err
	}
	inst.UpdatedAt = time.Now().UTC()
	return s.write(inst)
}

func (s *Store) write(inst *Instance) error {
	path, err := s.instanceFile(inst.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create instance dir: %w", err)
	}
	data, err := yaml.Marshal(inst)
	if err != nil {
		return fmt.Errorf("marshal instance %q: %w", inst.ID, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write instance %q: %w", inst.ID, err)
	}
	return nil
}

func (s *Store) instanceFile(id string) (string, error) {
	if !validInstanceID(id) {
		return "", fmt.Errorf("invalid instance id %q", id)
	}
	return filepath.Join(s.root, id, "instance.yaml"), nil
}

func (s *Store) InstanceDir(id string) (string, error) {
	if !validInstanceID(id) {
		return "", fmt.Errorf("invalid instance id %q", id)
	}
	return filepath.Join(s.root, id), nil
}

func validInstanceID(id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	if id == "." || id == ".." {
		return false
	}
	if strings.ContainsAny(id, `/\`) {
		return false
	}
	return filepath.Base(id) == id
}
