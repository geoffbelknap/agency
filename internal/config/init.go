package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"gopkg.in/yaml.v3"
)

// validOperatorName matches safe operator names: starts and ends with alphanumeric,
// middle may contain spaces, dots, underscores, or hyphens. 1–64 characters total.
// Single-character names are also accepted (start and end are the same character).
var validOperatorName = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9 ._-]{0,62}[a-zA-Z0-9])?$`)

// ValidateOperatorName checks that an operator name is safe for use in YAML
// files and file paths. It rejects empty names, names exceeding 64 characters,
// and names containing characters that could cause YAML injection or path traversal.
func ValidateOperatorName(name string) error {
	if name == "" {
		return fmt.Errorf("operator name must not be empty")
	}
	if len(name) > 64 {
		return fmt.Errorf("operator name must not exceed 64 characters")
	}
	if !validOperatorName.MatchString(name) {
		return fmt.Errorf("operator name %q is invalid: must start and end with alphanumeric, and contain only letters, digits, spaces, dots, underscores, or hyphens", name)
	}
	return nil
}

// InitOptions holds the parameters for initializing the Agency platform.
type InitOptions struct {
	Provider        string // e.g. "anthropic", "openai", "google"
	APIKey          string // primary provider API key
	AnthropicAPIKey string // explicit Anthropic key (overrides APIKey when Provider=="anthropic")
	OpenAIAPIKey    string // explicit OpenAI key
	Operator        string // operator name (informational)
	Force           bool   // reinitialize even if already set up
	NotifyURL       string // optional ntfy or webhook URL for operator alerts
	GatewayAddr     string // optional gateway listen address to persist

	// DeploymentBackend is the container backend Agency should drive —
	// "docker", "podman", "apple-container", or "containerd". When empty, any
	// existing value in config.yaml is preserved; if no existing value is set,
	// the backend stays unset and the gateway's legacy default (docker) applies.
	// Callers performing fresh installs are expected to detect the available
	// backend (see runtimehost.ProbeAllBackends) and set this explicitly so the
	// config captures the chosen backend persistently.
	DeploymentBackend string

	// DeploymentBackendConfig holds backend-specific wiring, typically the
	// resolved socket path under key "host" (docker/podman) or
	// "native_socket" (containerd). Persisted verbatim under
	// hub.deployment_backend_config.
	DeploymentBackendConfig map[string]string
}

// KeyEntry holds a provider API key to be stored in the credential store
// after the daemon is running.
type KeyEntry struct {
	Provider string
	EnvVar   string
	Key      string
}

// providerEnvVar returns the environment variable name for a given provider.
func providerEnvVar(provider string) string {
	switch strings.ToLower(provider) {
	case "google":
		return "GEMINI_API_KEY"
	default:
		return strings.ToUpper(provider) + "_API_KEY"
	}
}

// ProviderCredentialName returns the canonical credential-store name used by
// Hub provider components. Credential resolution still accepts env-var names
// through normalization, but storing canonical names keeps Web setup status
// aligned with Hub metadata.
func ProviderCredentialName(provider string) string {
	switch strings.ToLower(provider) {
	case "anthropic":
		return "anthropic-api-key"
	case "openai":
		return "openai-api-key"
	case "google":
		return "gemini-api-key"
	default:
		return providerEnvVar(provider)
	}
}

// ProviderDomains returns the API domains for well-known LLM providers.
// Used by credential storage to populate protocol_config.domains so the
// egress proxy knows which requests to inject credentials into.
func ProviderDomains(provider string) []string {
	switch strings.ToLower(provider) {
	case "anthropic":
		return []string{"api.anthropic.com"}
	case "openai":
		return []string{"api.openai.com"}
	case "google":
		return []string{"generativelanguage.googleapis.com"}
	default:
		return nil
	}
}

// detectNotificationType infers "ntfy" or "webhook" from a URL.
func detectNotificationType(url string) string {
	lower := strings.ToLower(url)
	if strings.Contains(lower, "ntfy.") || strings.Contains(lower, "ntfy.sh") {
		return "ntfy"
	}
	return "webhook"
}

// RunInit creates the ~/.agency/ directory structure and config files.
// It is idempotent: existing values (token, existing keys) are preserved
// unless Force is set.
func RunInit(opts InitOptions) ([]KeyEntry, error) {
	// Validate operator name before any writes — prevents YAML injection and path traversal.
	if opts.Operator != "" {
		if err := ValidateOperatorName(opts.Operator); err != nil {
			return nil, fmt.Errorf("invalid operator name: %w", err)
		}
	}

	agencyHome, err := resolveAgencyHome()
	if err != nil {
		return nil, err
	}

	// Create directory structure
	dirs := []string{
		agencyHome,
		filepath.Join(agencyHome, "agents"),
		filepath.Join(agencyHome, "teams"),
		filepath.Join(agencyHome, "departments"),
		filepath.Join(agencyHome, "connectors"),
		filepath.Join(agencyHome, "hub"),
		filepath.Join(agencyHome, "profiles"),
		filepath.Join(agencyHome, "registry", "services"),
		filepath.Join(agencyHome, "registry", "mcp-servers"),
		filepath.Join(agencyHome, "registry", "skills"),
		filepath.Join(agencyHome, "infrastructure"),
		filepath.Join(agencyHome, "infrastructure", "comms", "data"),
		filepath.Join(agencyHome, "infrastructure", "comms", "data", "channels"),
		filepath.Join(agencyHome, "infrastructure", "comms", "data", "cursors"),
		filepath.Join(agencyHome, "infrastructure", "egress"),
		filepath.Join(agencyHome, "infrastructure", "egress", "certs"),
		filepath.Join(agencyHome, "infrastructure", "egress", "blocklists"),
		filepath.Join(agencyHome, "run"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("create directory %s: %w", d, err)
		}
	}
	// Audit directory is sensitive — restrict to owner only
	auditDir := filepath.Join(agencyHome, "audit")
	if err := os.MkdirAll(auditDir, 0700); err != nil {
		return nil, fmt.Errorf("create directory %s: %w", auditDir, err)
	}

	// Pre-generate mitmproxy CA cert so it exists before any containers start.
	// Without this, the enforcer may boot before the egress proxy generates its
	// cert, causing TLS handshake failures on the first LLM call (502).
	egressCertsDir := filepath.Join(agencyHome, "infrastructure", "egress", "certs")
	if err := ensureEgressCACert(egressCertsDir); err != nil {
		// Log but don't fail — mitmproxy will generate it on first run (race risk remains)
		fmt.Fprintf(os.Stderr, "warning: could not pre-generate egress CA cert: %v\n", err)
	}

	// Create knowledge directory structure
	knowledgeDir := filepath.Join(agencyHome, "knowledge")
	os.MkdirAll(knowledgeDir, 0755)
	os.MkdirAll(filepath.Join(knowledgeDir, "ontology.d"), 0755)

	// Seed agentic-memory ontology extension if not already present.
	// Defines procedure, episode, and cached_result entity types used by
	// the body runtime for procedural/episodic memory and semantic caching.
	if err := knowledge.EnsureBaseOntology(agencyHome); err != nil {
		return nil, fmt.Errorf("seed base ontology: %w", err)
	}
	seedAgenticMemoryOntology(filepath.Join(knowledgeDir, "ontology.d"))

	// Create infrastructure and registry directories.
	// Services, routing, and ontology are synced from hub via `agency hub update`.
	os.MkdirAll(filepath.Join(agencyHome, "infrastructure"), 0755)
	os.MkdirAll(filepath.Join(agencyHome, "registry", "services"), 0755)

	// Load or create config
	configPath := filepath.Join(agencyHome, "config.yaml")
	var cfg map[string]interface{}
	if data, err := os.ReadFile(configPath); err == nil {
		yaml.Unmarshal(data, &cfg) //nolint:errcheck
	}
	if cfg == nil {
		cfg = map[string]interface{}{}
	}

	// Configure default hub source if not already present
	if _, ok := cfg["hub"]; !ok {
		cfg["hub"] = map[string]interface{}{
			"sources": []map[string]string{
				{
					"name":     "default",
					"type":     "oci",
					"registry": "ghcr.io/geoffbelknap/agency-hub",
				},
			},
		}
	}

	// Persist the selected container backend (and its socket config) when
	// the caller supplied one — typically a fresh install after probing
	// for docker/podman/containerd. Empty values preserve whatever the
	// existing config.yaml already specifies, so re-running setup never
	// downgrades an explicit choice.
	if opts.DeploymentBackend != "" {
		hubMap, ok := cfg["hub"].(map[string]interface{})
		if !ok {
			hubMap = map[string]interface{}{}
			cfg["hub"] = hubMap
		}
		hubMap["deployment_backend"] = opts.DeploymentBackend
		if len(opts.DeploymentBackendConfig) > 0 {
			backendCfg := make(map[string]interface{}, len(opts.DeploymentBackendConfig))
			for k, v := range opts.DeploymentBackendConfig {
				backendCfg[k] = v
			}
			hubMap["deployment_backend_config"] = backendCfg
		}
	}
	if gatewayAddr := strings.TrimSpace(opts.GatewayAddr); gatewayAddr != "" {
		cfg["gateway_addr"] = gatewayAddr
	}

	// Generate auth token if not already present
	if _, ok := cfg["token"]; !ok {
		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err != nil {
			return nil, fmt.Errorf("generate token: %w", err)
		}
		cfg["token"] = hex.EncodeToString(tokenBytes)
	}

	// Generate scoped egress token if not already present.
	// This token only authorizes credential resolution — not full API access.
	if _, ok := cfg["egress_token"]; !ok {
		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err != nil {
			return nil, fmt.Errorf("generate egress token: %w", err)
		}
		cfg["egress_token"] = hex.EncodeToString(tokenBytes)
	}

	// Apply provider / key settings from options
	// Explicit per-provider keys take precedence over the generic APIKey field.
	var pendingKeys []KeyEntry
	if opts.AnthropicAPIKey != "" {
		pendingKeys = append(pendingKeys, KeyEntry{"anthropic", providerEnvVar("anthropic"), opts.AnthropicAPIKey})
	}
	if opts.OpenAIAPIKey != "" {
		pendingKeys = append(pendingKeys, KeyEntry{"openai", providerEnvVar("openai"), opts.OpenAIAPIKey})
	}
	// Generic provider+key (CLI path)
	if opts.Provider != "" && opts.APIKey != "" {
		// Only add if not already covered by an explicit key above
		alreadyCovered := false
		for _, e := range pendingKeys {
			if e.Provider == opts.Provider {
				alreadyCovered = true
				break
			}
		}
		if !alreadyCovered {
			pendingKeys = append(pendingKeys, KeyEntry{opts.Provider, providerEnvVar(opts.Provider), opts.APIKey})
		}
	}

	// If no new keys provided, check existing .env for configured providers
	if len(pendingKeys) == 0 {
		existingKeys := ReadExistingKeys(agencyHome)
		if len(existingKeys) > 0 && opts.Provider == "" {
			// Preserve existing provider from config or infer from .env
			if _, ok := cfg["llm_provider"]; !ok {
				// Infer from existing keys
				for _, k := range existingKeys {
					cfg["llm_provider"] = k
					break
				}
			}
		}
	}

	// Set the primary provider if we have new keys
	if opts.Provider != "" {
		cfg["llm_provider"] = opts.Provider
	} else if opts.AnthropicAPIKey != "" {
		cfg["llm_provider"] = "anthropic"
	} else if opts.OpenAIAPIKey != "" && opts.AnthropicAPIKey == "" {
		cfg["llm_provider"] = "openai"
	}

	// API keys are stored in the encrypted credential store (not config.yaml).
	// Strip any legacy keys from config.yaml (may exist from older init).
	for _, suffix := range []string{"anthropic_api_key", "openai_api_key", "google_api_key"} {
		delete(cfg, suffix)
	}

	// Store operator name (already validated above)
	if opts.Operator != "" {
		cfg["operator"] = opts.Operator
	}

	// Notification config — write to notifications.yaml (separate from config.yaml)
	if opts.NotifyURL != "" {
		notifType := detectNotificationType(opts.NotifyURL)
		notifConfigs := []NotificationConfig{
			{
				Name:   "operator-alerts",
				Type:   notifType,
				URL:    opts.NotifyURL,
				Events: []string{"operator_alert", "enforcer_exited", "mission_health_alert"},
			},
		}
		notifData, err := yaml.Marshal(notifConfigs)
		if err != nil {
			return nil, fmt.Errorf("marshal notifications: %w", err)
		}
		notifPath := filepath.Join(agencyHome, "notifications.yaml")
		if err := os.WriteFile(notifPath, notifData, 0600); err != nil {
			return nil, fmt.Errorf("write notifications: %w", err)
		}
	}

	// Write config.yaml (mode 0600 — contains token)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	// Generate credential-swaps.yaml (best-effort).
	hub.WriteSwapConfig(agencyHome) //nolint:errcheck

	return pendingKeys, nil
}

func resolveAgencyHome() (string, error) {
	if home := os.Getenv("AGENCY_HOME"); home != "" {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".agency"), nil
}

// seedAgenticMemoryOntology writes the agentic-memory ontology extension to
// ontology.d/ if it doesn't already exist. This defines the entity types used
// by the body runtime for procedural memory, episodic memory, and semantic
// caching. Idempotent — won't overwrite an existing file.
func seedAgenticMemoryOntology(ontologyDir string) {
	path := filepath.Join(ontologyDir, "agentic-memory.yaml")
	if _, err := os.Stat(path); err == nil {
		return // already exists
	}

	const content = `name: agentic-memory
kind: ontology-extension
description: Entity types for agentic memory — procedural, episodic, and cached results
extends: default

entity_types:
  procedure:
    description: A learned task-execution pattern — approach, tools, outcome, lessons
    attributes: [task_id, summary, approach, tools_used, outcome, lessons, agent, mission, duration_minutes, timestamp]
  episode:
    description: A recorded task episode — what happened, what was notable, entities involved
    attributes: [task_id, summary, notable_events, entities_involved, agent, mission, timestamp]
  cached_result:
    description: Cached task result for semantic deduplication
    attributes: [task_description, trigger_context, agent, mission, outcome, cost_usd, duration_s, steps, tools_used, ttl_hours, full_result, created_at]

relationship_types:
  produced_by:
    description: Was produced by a procedure or process
    inverse: produced
`
	os.WriteFile(path, []byte(content), 0644) //nolint:errcheck
}

// ReadExistingKeys returns provider names that have keys in ~/.agency/.env.
func ReadExistingKeys(agencyHome string) []string {
	envFile := filepath.Join(agencyHome, ".env")
	data, err := os.ReadFile(envFile)
	if err != nil {
		return nil
	}
	providerMap := map[string]string{
		"ANTHROPIC_API_KEY": "anthropic",
		"OPENAI_API_KEY":    "openai",
		"GEMINI_API_KEY":    "google",
	}
	var providers []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		for envVar, provider := range providerMap {
			if strings.HasPrefix(line, envVar+"=") && len(line) > len(envVar)+1 {
				providers = append(providers, provider)
			}
		}
	}
	return providers
}
