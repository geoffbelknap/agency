package features

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

type Tier string

const (
	TierCore         Tier = "core"
	TierExperimental Tier = "experimental"
	TierInternal     Tier = "internal"
)

const (
	Missions         = "missions"
	Teams            = "teams"
	Profiles         = "profiles"
	Hub              = "hub"
	Intake           = "intake"
	Events           = "events"
	Webhooks         = "webhooks"
	Notifications    = "notifications"
	Meeseeks         = "meeseeks"
	Packages         = "packages"
	Instances        = "instances"
	Authz            = "authz"
	RoutingOptimizer = "routing_optimizer"
	TrustAdmin       = "trust_admin"
	RegistryAdmin    = "registry_admin"
	DepartmentAdmin  = "department_admin"
	GraphAdmin       = "graph_admin"
	Relay            = "relay"
	WebFetch         = "web_fetch"
	Embeddings       = "embeddings"
	Cache            = "cache"
)

type Feature struct {
	ID   string `json:"id"`
	Tier Tier   `json:"tier"`
}

var registry = map[string]Feature{
	Missions:         {ID: Missions, Tier: TierExperimental},
	Teams:            {ID: Teams, Tier: TierExperimental},
	Profiles:         {ID: Profiles, Tier: TierExperimental},
	Hub:              {ID: Hub, Tier: TierExperimental},
	Intake:           {ID: Intake, Tier: TierExperimental},
	Events:           {ID: Events, Tier: TierExperimental},
	Webhooks:         {ID: Webhooks, Tier: TierExperimental},
	Notifications:    {ID: Notifications, Tier: TierExperimental},
	Meeseeks:         {ID: Meeseeks, Tier: TierExperimental},
	Packages:         {ID: Packages, Tier: TierExperimental},
	Instances:        {ID: Instances, Tier: TierExperimental},
	Authz:            {ID: Authz, Tier: TierExperimental},
	RoutingOptimizer: {ID: RoutingOptimizer, Tier: TierExperimental},
	TrustAdmin:       {ID: TrustAdmin, Tier: TierExperimental},
	RegistryAdmin:    {ID: RegistryAdmin, Tier: TierExperimental},
	DepartmentAdmin:  {ID: DepartmentAdmin, Tier: TierExperimental},
	GraphAdmin:       {ID: GraphAdmin, Tier: TierExperimental},
	Relay:            {ID: Relay, Tier: TierExperimental},
	WebFetch:         {ID: WebFetch, Tier: TierExperimental},
	Embeddings:       {ID: Embeddings, Tier: TierInternal},
	Cache:            {ID: Cache, Tier: TierInternal},
}

var commandFeatures = map[string]string{
	"hub":           Hub,
	"team":          Teams,
	"intake":        Intake,
	"mission":       Missions,
	"event":         Events,
	"webhook":       Webhooks,
	"meeseeks":      Meeseeks,
	"notify":        Notifications,
	"notifications": Notifications,
	"notification":  Notifications,
	"cache":         Cache,
	"registry":      RegistryAdmin,
	"package":       Packages,
	"instance":      Instances,
	"authz":         Authz,
}

func ExperimentalEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AGENCY_EXPERIMENTAL_SURFACES"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func TierOf(id string) Tier {
	if feature, ok := registry[id]; ok {
		return feature.Tier
	}
	return TierCore
}

func Enabled(id string) bool {
	switch TierOf(id) {
	case TierCore:
		return true
	case TierExperimental:
		return ExperimentalEnabled()
	default:
		return false
	}
}

func CommandFeature(name string) string {
	return commandFeatures[name]
}

func CommandIsExperimental(name string) bool {
	return TierOf(CommandFeature(name)) == TierExperimental
}

func CommandVisible(name string) bool {
	featureID := CommandFeature(name)
	if featureID == "" {
		return true
	}
	return Enabled(featureID)
}

func WebManifest() []Feature {
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]Feature, 0, len(ids))
	for _, id := range ids {
		out = append(out, registry[id])
	}
	return out
}

func WebManifestJSON() ([]byte, error) {
	return json.MarshalIndent(WebManifest(), "", "  ")
}
