package runtimebackend

import (
	"strings"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

func DefaultRuntimeBackend() string {
	return BackendMicroagent
}

func NormalizeRuntimeBackend(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	switch name {
	case "", "auto", BackendFirecracker, BackendAppleVFMicroVM, BackendMicroagent:
		return BackendMicroagent
	default:
		return runtimehost.NormalizeContainerBackend(name)
	}
}
