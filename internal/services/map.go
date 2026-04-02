package services

import (
	"fmt"
	"maps"
	"sync"
)

// Label constants for service discovery.
const (
	LabelServiceEnabled = "agency.service"
	LabelServiceName    = "agency.service.name"
	LabelServicePort    = "agency.service.port"
	LabelServiceHealth  = "agency.service.health"
	LabelServiceNetwork = "agency.service.network"
	LabelServiceHMAC    = "agency.service.hmac"
)

// Service represents a discovered service.
type Service struct {
	Name          string
	ContainerID   string
	ContainerName string
	Port          string
	HealthPath    string
	Network       string
	Healthy       bool
}

// ServiceMap maintains a live registry of discovered services.
type ServiceMap struct {
	mu       sync.RWMutex
	services map[string]*Service // name → service
	byID     map[string]string   // containerID → service name
	known    map[string]bool     // container IDs we created
	hmacKey  []byte
}

// NewServiceMap creates a new service map with the given HMAC key.
func NewServiceMap(hmacKey []byte) *ServiceMap {
	return &ServiceMap{
		services: make(map[string]*Service),
		byID:     make(map[string]string),
		known:    make(map[string]bool),
		hmacKey:  hmacKey,
	}
}

// TrackCreation records a container ID as created by the gateway.
func (m *ServiceMap) TrackCreation(containerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.known[containerID] = true
}

// Register adds a service from container labels after provenance verification.
func (m *ServiceMap) Register(containerID, containerName string, labels map[string]string) error {
	if labels[LabelServiceEnabled] != "true" {
		return nil
	}

	name := labels[LabelServiceName]
	if name == "" {
		return fmt.Errorf("service label missing name")
	}

	// Dual-layer provenance verification
	m.mu.RLock()
	knownCopy := make(map[string]bool, len(m.known))
	maps.Copy(knownCopy, m.known)
	m.mu.RUnlock()

	ok, _, _ := VerifyProvenance(containerID, containerName, knownCopy, labels[LabelServiceHMAC], m.hmacKey)
	if !ok {
		return fmt.Errorf("provenance verification failed for %s (%s)", containerName, containerID)
	}

	svc := &Service{
		Name:          name,
		ContainerID:   containerID,
		ContainerName: containerName,
		Port:          labels[LabelServicePort],
		HealthPath:    labels[LabelServiceHealth],
		Network:       labels[LabelServiceNetwork],
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.services[name] = svc
	m.byID[containerID] = name
	return nil
}

// Deregister removes a service by container ID.
func (m *ServiceMap) Deregister(containerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name, ok := m.byID[containerID]
	if !ok {
		return
	}
	delete(m.services, name)
	delete(m.byID, containerID)
	delete(m.known, containerID)
}

// URL returns the HTTP URL for a named service, or "" if not found.
func (m *ServiceMap) URL(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	svc, ok := m.services[name]
	if !ok {
		return ""
	}
	return fmt.Sprintf("http://%s:%s", svc.ContainerName, svc.Port)
}

// IsHealthy returns whether a named service is healthy.
func (m *ServiceMap) IsHealthy(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	svc, ok := m.services[name]
	if !ok {
		return false
	}
	return svc.Healthy
}

// SetHealthy updates a service's health status.
func (m *ServiceMap) SetHealthy(name string, healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if svc, ok := m.services[name]; ok {
		svc.Healthy = healthy
	}
}

// All returns all registered services.
func (m *ServiceMap) All() []Service {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Service, 0, len(m.services))
	for _, svc := range m.services {
		result = append(result, *svc)
	}
	return result
}
