package orchestrate

import (
	"strings"

	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
)

func hostServiceRuntimeBackend(name string) bool {
	switch strings.TrimSpace(name) {
	case hostruntimebackend.BackendFirecracker, hostruntimebackend.BackendAppleVFMicroVM, hostruntimebackend.BackendMicroagent:
		return true
	default:
		return false
	}
}
