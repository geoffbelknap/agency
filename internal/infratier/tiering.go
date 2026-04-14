package infratier

import (
	"os"
	"strings"
)

var (
	coreStartupComponents         = []string{"egress", "comms", "knowledge", "web"}
	experimentalStartupComponents = []string{"intake", "web-fetch", "relay", "embeddings"}
	coreStatusComponents          = []string{"egress", "comms", "knowledge", "web"}
	experimentalStatusComponents  = []string{"intake", "web-fetch", "embeddings"}
	coreReloadComponents          = []string{"egress", "comms", "knowledge"}
	coreRebuildComponents         = []string{"egress", "comms", "knowledge", "web"}
)

func ExperimentalSurfacesEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AGENCY_EXPERIMENTAL_SURFACES"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func StartupComponents() []string {
	if ExperimentalSurfacesEnabled() {
		return append(copyOf(coreStartupComponents), experimentalStartupComponents...)
	}
	return copyOf(coreStartupComponents)
}

func StatusComponents() []string {
	if ExperimentalSurfacesEnabled() {
		return append(copyOf(coreStatusComponents), experimentalStatusComponents...)
	}
	return copyOf(coreStatusComponents)
}

func ReloadComponents() []string {
	return copyOf(coreReloadComponents)
}

func RebuildComponents() []string {
	return copyOf(coreRebuildComponents)
}

func copyOf(items []string) []string {
	out := make([]string, len(items))
	copy(out, items)
	return out
}
