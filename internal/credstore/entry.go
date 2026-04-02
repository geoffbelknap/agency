package credstore

// Kind constants for credential classification.
const (
	KindProvider = "provider"
	KindService  = "service"
	KindGateway  = "gateway"
	KindInternal = "internal"
	KindGroup    = "group"
)

// Protocol constants for credential authentication methods.
const (
	ProtocolAPIKey      = "api-key"
	ProtocolJWTExchange = "jwt-exchange"
	ProtocolGitHubApp   = "github-app"
	ProtocolOAuth2      = "oauth2"
	ProtocolBearer      = "bearer"
)

// Entry is the rich Agency credential with typed metadata.
type Entry struct {
	Name     string   `json:"name"`
	Value    string   `json:"value"`
	Metadata Metadata `json:"metadata"`
}

// Metadata holds all classification and routing information for a credential.
type Metadata struct {
	Kind           string         `json:"kind"`
	Scope          string         `json:"scope"`
	Service        string         `json:"service,omitempty"`
	Group          string         `json:"group,omitempty"`
	Protocol       string         `json:"protocol"`
	ProtocolConfig map[string]any `json:"protocol_config,omitempty"`
	Source         string         `json:"source"`
	ExpiresAt      string         `json:"expires_at,omitempty"`
	Requires       []string       `json:"requires,omitempty"`
	ExternalScopes []string       `json:"external_scopes,omitempty"`
	CreatedAt      string         `json:"created_at"`
	RotatedAt      string         `json:"rotated_at"`
}

// SecretRef is a reference to a secret without the value, used for list operations.
type SecretRef struct {
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata"`
}

// Filter defines criteria for listing credentials.
type Filter struct {
	Kind    string `json:"kind,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Service string `json:"service,omitempty"`
	Group   string `json:"group,omitempty"`
}

// Warning represents a validation warning on a credential.
type Warning struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// TestResult represents the outcome of an end-to-end credential health check.
type TestResult struct {
	OK      bool   `json:"ok"`
	Status  int    `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
	Latency int    `json:"latency_ms"`
}
