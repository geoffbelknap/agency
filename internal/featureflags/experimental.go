package featureflags

import (
	"os"
	"strings"
)

func ExperimentalSurfacesEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AGENCY_EXPERIMENTAL_SURFACES"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
