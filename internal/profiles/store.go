// Package profiles provides a file-backed profile store for operators and agents.
// Profiles are stored as individual YAML files under ~/.agency/profiles/.
package profiles

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/models"
)

// validID matches safe profile IDs: alphanumeric with hyphens and underscores, 1–64 chars.
var validID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// Store manages profile YAML files on disk.
type Store struct {
	dir string // ~/.agency/profiles
	mu  sync.RWMutex
}

// NewStore creates a profile store rooted at the given directory.
// The directory is created if it does not exist.
func NewStore(dir string) *Store {
	os.MkdirAll(dir, 0755)
	return &Store{dir: dir}
}

// Put creates or updates a profile.
func (s *Store) Put(p models.Profile) error {
	if !validID.MatchString(p.ID) {
		return fmt.Errorf("invalid profile ID %q: must be 1-64 alphanumeric characters with hyphens, underscores, or dots", p.ID)
	}
	if p.Type != "operator" && p.Type != "agent" {
		return fmt.Errorf("invalid profile type %q: must be operator or agent", p.Type)
	}
	if p.DisplayName == "" {
		return fmt.Errorf("display_name is required")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if p.Created == "" {
		// Preserve existing created timestamp on update.
		if existing, err := s.Get(p.ID); err == nil {
			p.Created = existing.Created
		} else {
			p.Created = now
		}
	}
	p.Updated = now

	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, p.ID+".yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write profile: %w", err)
	}
	return nil
}

// Get retrieves a profile by ID.
func (s *Store) Get(id string) (models.Profile, error) {
	id = filepath.Base(id)
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dir, id+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return models.Profile{}, fmt.Errorf("profile %q not found", id)
		}
		return models.Profile{}, fmt.Errorf("read profile: %w", err)
	}

	var p models.Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return models.Profile{}, fmt.Errorf("unmarshal profile: %w", err)
	}
	return p, nil
}

// Delete removes a profile by ID.
func (s *Store) Delete(id string) error {
	id = filepath.Base(id)
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, id+".yaml")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("profile %q not found", id)
		}
		return fmt.Errorf("delete profile: %w", err)
	}
	return nil
}

// List returns all profiles, optionally filtered by type ("operator" or "agent").
func (s *Store) List(filterType string) ([]models.Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read profiles dir: %w", err)
	}

	var profiles []models.Profile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var p models.Profile
		if err := yaml.Unmarshal(data, &p); err != nil {
			continue
		}
		if filterType != "" && p.Type != filterType {
			continue
		}
		profiles = append(profiles, p)
	}

	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].ID < profiles[j].ID
	})
	return profiles, nil
}
