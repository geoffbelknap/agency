package infratier

import "github.com/geoffbelknap/agency/internal/features"

var (
	coreStartupComponents         = []string{"egress", "comms", "knowledge", "web"}
	experimentalStartupComponents = []string{"intake", "web-fetch", "relay", "embeddings"}
	coreStatusComponents          = []string{"egress", "comms", "knowledge", "web"}
	experimentalStatusComponents  = []string{"intake", "web-fetch", "embeddings"}
	coreReloadComponents          = []string{"egress", "comms", "knowledge"}
	coreRebuildComponents         = []string{"egress", "comms", "knowledge", "web"}
)

func StartupComponents() []string {
	out := copyOf(coreStartupComponents)
	if features.Enabled(features.Intake) {
		out = append(out, "intake")
	}
	if features.Enabled(features.WebFetch) {
		out = append(out, "web-fetch")
	}
	if features.Enabled(features.Relay) {
		out = append(out, "relay")
	}
	return out
}

func StatusComponents() []string {
	out := copyOf(coreStatusComponents)
	if features.Enabled(features.Intake) {
		out = append(out, "intake")
	}
	if features.Enabled(features.WebFetch) {
		out = append(out, "web-fetch")
	}
	return out
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
