package events

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/geoffbelknap/agency/internal/config"
	"gopkg.in/yaml.v3"
)

// NotificationStore manages notification destination persistence at ~/.agency/notifications.yaml.
type NotificationStore struct {
	mu      sync.RWMutex
	home    string
	configs []config.NotificationConfig
}

// NewNotificationStore creates a store rooted at the given agency home directory.
func NewNotificationStore(home string) *NotificationStore {
	return &NotificationStore{home: home}
}

func (s *NotificationStore) filePath() string {
	return filepath.Join(s.home, "notifications.yaml")
}

// Load reads notification configs from disk. Returns empty slice if file doesn't exist.
func (s *NotificationStore) Load() ([]config.NotificationConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath())
	if err != nil {
		if os.IsNotExist(err) {
			s.configs = nil
			return nil, nil
		}
		return nil, fmt.Errorf("read notifications: %w", err)
	}

	var configs []config.NotificationConfig
	if err := yaml.Unmarshal(data, &configs); err != nil {
		return nil, fmt.Errorf("parse notifications: %w", err)
	}

	s.configs = configs
	return configs, nil
}

func (s *NotificationStore) save() error {
	data, err := yaml.Marshal(s.configs)
	if err != nil {
		return fmt.Errorf("marshal notifications: %w", err)
	}
	return os.WriteFile(s.filePath(), data, 0600)
}

// List returns all notification configs.
func (s *NotificationStore) List() []config.NotificationConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]config.NotificationConfig, len(s.configs))
	copy(result, s.configs)
	return result
}

// Get returns a notification config by name.
func (s *NotificationStore) Get(name string) (*config.NotificationConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.configs {
		if s.configs[i].Name == name {
			nc := s.configs[i]
			return &nc, nil
		}
	}
	return nil, fmt.Errorf("notification %q not found", name)
}

// Add adds a notification config. Name must be unique.
func (s *NotificationStore) Add(nc config.NotificationConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.configs {
		if existing.Name == nc.Name {
			return fmt.Errorf("notification %q already exists", nc.Name)
		}
	}

	s.configs = append(s.configs, nc)
	return s.save()
}

// Remove removes a notification config by name.
func (s *NotificationStore) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := false
	filtered := make([]config.NotificationConfig, 0, len(s.configs))
	for _, nc := range s.configs {
		if nc.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, nc)
	}
	if !found {
		return fmt.Errorf("notification %q not found", name)
	}

	s.configs = filtered
	return s.save()
}
