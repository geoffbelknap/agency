package backend

import (
	"fmt"
	"sync"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

type Factory func() (runtimecontract.Backend, error)

type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

func (r *Registry) Register(name string, factory Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

func (r *Registry) Build(name string) (runtimecontract.Backend, error) {
	r.mu.RLock()
	factory, ok := r.factories[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("runtime backend %q is not registered", name)
	}
	return factory()
}
