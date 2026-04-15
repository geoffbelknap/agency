package credstore

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/pkg/urlsafety"
	"gopkg.in/yaml.v3"
)

// Store is the Agency credential manager. It composes a SecretBackend for
// storage and adds scope routing, validation, group resolution, and swap
// config generation.
type Store struct {
	backend SecretBackend
	home    string // ~/.agency root
}

// NewStore creates a Store backed by the given SecretBackend.
func NewStore(backend SecretBackend, home string) *Store {
	return &Store{
		backend: backend,
		home:    home,
	}
}

// Backend returns the underlying SecretBackend for group resolution.
func (s *Store) Backend() SecretBackend {
	return s.backend
}

// Put validates and stores a credential entry.
func (s *Store) Put(entry Entry) error {
	// Validate protocol config.
	if err := ValidateProtocolConfig(entry, nil); err != nil {
		return fmt.Errorf("validate protocol config: %w", err)
	}

	// Warn on scope/dependency issues (non-fatal).
	if warnings := ValidateScopes(entry, s.home); len(warnings) > 0 {
		for _, w := range warnings {
			log.Printf("credstore: scope warning for %q: %s", entry.Name, w.Message)
		}
	}
	if warnings := ValidateDependencies(entry, s.home); len(warnings) > 0 {
		for _, w := range warnings {
			log.Printf("credstore: dependency warning for %q: %s", entry.Name, w.Message)
		}
	}

	name, value, metadata := entryToBackend(entry)
	if err := s.backend.Put(name, value, metadata); err != nil {
		return fmt.Errorf("backend put %q: %w", entry.Name, err)
	}

	sanitize := func(s string) string {
		return strings.NewReplacer("\n", "\\n", "\r", "\\r").Replace(s)
	}
	log.Printf("credstore: stored credential %q (kind=%s scope=%s)", sanitize(entry.Name), sanitize(string(entry.Metadata.Kind)), sanitize(string(entry.Metadata.Scope)))
	return nil
}

// Get retrieves a credential by name.
func (s *Store) Get(name string) (*Entry, error) {
	value, metadata, err := s.backend.Get(name)
	if err != nil {
		return nil, err
	}
	entry := entryFromBackend(name, value, metadata)
	return &entry, nil
}

// Delete removes a credential by name.
func (s *Store) Delete(name string) error {
	return s.backend.Delete(name)
}

// List returns all credentials matching the filter.
func (s *Store) List(filter Filter) ([]Entry, error) {
	refs, err := s.backend.List()
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, ref := range refs {
		if !matchesFilter(ref.Metadata, filter) {
			continue
		}
		value, meta, err := s.backend.Get(ref.Name)
		if err != nil {
			return nil, fmt.Errorf("get %q: %w", ref.Name, err)
		}
		entries = append(entries, entryFromBackend(ref.Name, value, meta))
	}
	return entries, nil
}

// Rotate updates a credential's value while preserving all metadata.
func (s *Store) Rotate(name, newValue string) error {
	entry, err := s.Get(name)
	if err != nil {
		return fmt.Errorf("rotate get %q: %w", name, err)
	}

	entry.Value = newValue
	entry.Metadata.RotatedAt = time.Now().UTC().Format(time.RFC3339)

	n, v, m := entryToBackend(*entry)
	if err := s.backend.Put(n, v, m); err != nil {
		return fmt.Errorf("rotate put %q: %w", name, err)
	}

	log.Printf("credstore: rotated credential %q", name)
	return nil
}

// ForService returns the first credential matching the given service name.
func (s *Store) ForService(serviceName string) (*Entry, error) {
	entries, err := s.List(Filter{Service: serviceName})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no credential for service %q", serviceName)
	}
	return &entries[0], nil
}

// ForAgent returns the best-scoped credential for an agent and service.
// Scope precedence: agent:{name} > team:{teamName} > platform.
func (s *Store) ForAgent(agentName, serviceName string) (*Entry, error) {
	entries, err := s.List(Filter{Service: serviceName})
	if err != nil {
		return nil, err
	}

	var agentMatch, teamMatch, platformMatch *Entry
	agentScope := "agent:" + agentName

	for i := range entries {
		e := &entries[i]
		switch {
		case e.Metadata.Scope == agentScope:
			agentMatch = e
		case strings.HasPrefix(e.Metadata.Scope, "team:"):
			if teamMatch == nil {
				teamMatch = e
			}
		case e.Metadata.Scope == "platform":
			platformMatch = e
		}
	}

	if agentMatch != nil {
		return agentMatch, nil
	}
	if teamMatch != nil {
		return teamMatch, nil
	}
	if platformMatch != nil {
		return platformMatch, nil
	}
	return nil, fmt.Errorf("no credential for agent %q service %q", agentName, serviceName)
}

// ForDomain returns a credential whose ProtocolConfig contains a matching domain.
func (s *Store) ForDomain(domain string) (*Entry, error) {
	domain = strings.ToLower(domain)

	refs, err := s.backend.List()
	if err != nil {
		return nil, err
	}

	for _, ref := range refs {
		pcRaw := ref.Metadata["protocol_config"]
		if pcRaw == "" {
			continue
		}
		var pc map[string]any
		if json.Unmarshal([]byte(pcRaw), &pc) != nil {
			continue
		}
		domains, ok := pc["domains"]
		if !ok {
			continue
		}
		domainList, ok := domains.([]any)
		if !ok {
			continue
		}
		for _, d := range domainList {
			if ds, ok := d.(string); ok && strings.ToLower(ds) == domain {
				value, meta, err := s.backend.Get(ref.Name)
				if err != nil {
					return nil, err
				}
				entry := entryFromBackend(ref.Name, value, meta)
				return &entry, nil
			}
		}
	}

	return nil, fmt.Errorf("no credential for domain %q", domain)
}

// Test performs an end-to-end health check on a credential.
func (s *Store) Test(name string) (*TestResult, error) {
	entry, err := s.Get(name)
	if err != nil {
		return &TestResult{OK: false, Message: err.Error()}, nil
	}

	// Resolve group if needed.
	resolved, err := ResolveGroup(*entry, s.backend)
	if err != nil {
		return &TestResult{OK: false, Message: fmt.Sprintf("group resolution: %s", err)}, nil
	}

	start := time.Now()

	switch resolved.Metadata.Protocol {
	case ProtocolJWTExchange:
		result := s.testJWTExchange(resolved)
		result.Latency = int(time.Since(start).Milliseconds())
		return result, nil
	case ProtocolAPIKey, ProtocolBearer:
		result := s.testAPIKey(resolved)
		result.Latency = int(time.Since(start).Milliseconds())
		return result, nil
	default:
		return &TestResult{
			OK:      false,
			Message: fmt.Sprintf("no test handler for protocol %q", resolved.Metadata.Protocol),
			Latency: int(time.Since(start).Milliseconds()),
		}, nil
	}
}

// Expiring returns all credentials that expire within the given duration.
func (s *Store) Expiring(within time.Duration) ([]Entry, error) {
	entries, err := s.List(Filter{})
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().Add(within)
	var expiring []Entry
	for _, e := range entries {
		if e.Metadata.ExpiresAt == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, e.Metadata.ExpiresAt)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			expiring = append(expiring, e)
		}
	}
	return expiring, nil
}

// GenerateSwapConfig builds credential-swaps.yaml from all store entries.
// It resolves groups and produces a SwapConfigFile matching the hub package
// format.
func (s *Store) GenerateSwapConfig() ([]byte, error) {
	entries, err := s.List(Filter{})
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}

	cfg := hub.SwapConfigFile{Swaps: map[string]hub.SwapEntry{}}

	for _, e := range entries {
		if e.Metadata.Kind == KindGroup || e.Metadata.Kind == KindInternal || e.Metadata.Kind == KindGateway {
			continue
		}

		resolved, err := ResolveGroup(e, s.backend)
		if err != nil {
			log.Printf("credstore: skip %q for swap config: %v", e.Name, err)
			continue
		}

		swapName := resolved.Metadata.Service
		if swapName == "" {
			swapName = resolved.Name
		}

		swap := hub.SwapEntry{
			KeyRef: resolved.Name,
		}

		switch resolved.Metadata.Protocol {
		case ProtocolAPIKey:
			swap.Type = "api-key"
			if h, ok := resolved.Metadata.ProtocolConfig["header"].(string); ok {
				swap.Header = h
			}
			if f, ok := resolved.Metadata.ProtocolConfig["format"].(string); ok {
				swap.Format = f
			}
			swap.Domains = extractDomains(resolved.Metadata.ProtocolConfig)
			// Fallback: infer defaults for well-known providers when not explicitly set.
			if defaults := inferProviderDefaults(resolved.Name); defaults != nil {
				if len(swap.Domains) == 0 {
					swap.Domains = defaults.Domains
				}
				if swap.Header == "" {
					swap.Header = defaults.Header
				}
				if swap.Format == "" {
					swap.Format = defaults.Format
				}
			}

		case ProtocolJWTExchange:
			swap.Type = "jwt-exchange"
			if u, ok := resolved.Metadata.ProtocolConfig["token_url"].(string); ok {
				swap.TokenURL = u
			}
			if params, ok := resolved.Metadata.ProtocolConfig["token_params"].(map[string]any); ok {
				swap.TokenParams = make(map[string]string, len(params))
				for k, v := range params {
					swap.TokenParams[k] = fmt.Sprintf("%v", v)
				}
			}
			if f, ok := resolved.Metadata.ProtocolConfig["token_response_field"].(string); ok {
				swap.TokenResponseField = f
			}
			if ttl, ok := resolved.Metadata.ProtocolConfig["token_ttl_seconds"].(float64); ok {
				swap.TokenTTLSeconds = int(ttl)
			}
			if h, ok := resolved.Metadata.ProtocolConfig["inject_header"].(string); ok {
				swap.InjectHeader = h
			}
			if f, ok := resolved.Metadata.ProtocolConfig["inject_format"].(string); ok {
				swap.InjectFormat = f
			}
			swap.Domains = extractDomains(resolved.Metadata.ProtocolConfig)

		case ProtocolBearer:
			swap.Type = "api-key"
			swap.Header = "Authorization"
			swap.Format = "Bearer {key}"
			swap.Domains = extractDomains(resolved.Metadata.ProtocolConfig)

		default:
			continue
		}

		cfg.Swaps[swapName] = swap
	}

	return yaml.Marshal(cfg)
}

// testJWTExchange attempts a JWT token exchange to verify the credential.
func (s *Store) testJWTExchange(entry Entry) *TestResult {
	tokenURL, _ := entry.Metadata.ProtocolConfig["token_url"].(string)
	if tokenURL == "" {
		return &TestResult{OK: false, Message: "no token_url in protocol_config"}
	}

	if err := urlsafety.Validate(tokenURL); err != nil {
		return &TestResult{OK: false, Message: fmt.Sprintf("unsafe token URL: %s", err)}
	}

	params := url.Values{}
	if tp, ok := entry.Metadata.ProtocolConfig["token_params"].(map[string]any); ok {
		for k, v := range tp {
			val := fmt.Sprintf("%v", v)
			// Replace ${credential} placeholder with actual value.
			val = strings.ReplaceAll(val, "${credential}", entry.Value)
			params.Set(k, val)
		}
	}

	client := urlsafety.SafeClient()
	resp, err := client.PostForm(tokenURL, params)
	if err != nil {
		return &TestResult{OK: false, Message: fmt.Sprintf("token exchange: %s", err)}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return &TestResult{OK: true, Status: resp.StatusCode, Message: "token exchange succeeded"}
	}
	return &TestResult{OK: false, Status: resp.StatusCode, Message: fmt.Sprintf("token exchange returned %d", resp.StatusCode)}
}

// testAPIKey calls the test endpoint from protocol_config if available.
func (s *Store) testAPIKey(entry Entry) *TestResult {
	testEndpoint, _ := entry.Metadata.ProtocolConfig["test_endpoint"].(string)
	if testEndpoint == "" {
		// No test endpoint configured — cannot test.
		return &TestResult{OK: true, Message: "no test_endpoint configured; skipped"}
	}

	domains := extractDomains(entry.Metadata.ProtocolConfig)
	if len(domains) == 0 {
		return &TestResult{OK: false, Message: "no domain in protocol_config for test"}
	}

	testURL := "https://" + domains[0] + testEndpoint

	if err := urlsafety.Validate(testURL); err != nil {
		return &TestResult{OK: false, Message: fmt.Sprintf("unsafe test URL: %s", err)}
	}

	req, err := http.NewRequest("GET", testURL, nil)
	if err != nil {
		return &TestResult{OK: false, Message: fmt.Sprintf("create request: %s", err)}
	}

	header, _ := entry.Metadata.ProtocolConfig["header"].(string)
	format, _ := entry.Metadata.ProtocolConfig["format"].(string)
	if header != "" {
		val := entry.Value
		if format != "" {
			val = strings.ReplaceAll(format, "{key}", entry.Value)
		}
		req.Header.Set(header, val)
	}

	client := urlsafety.SafeClient()
	resp, err := client.Do(req)
	if err != nil {
		return &TestResult{OK: false, Message: fmt.Sprintf("test request: %s", err)}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	expectedStatus := 200
	if es, ok := entry.Metadata.ProtocolConfig["test_expected_status"].(float64); ok {
		expectedStatus = int(es)
	}

	if resp.StatusCode == expectedStatus {
		return &TestResult{OK: true, Status: resp.StatusCode, Message: "test passed"}
	}
	return &TestResult{OK: false, Status: resp.StatusCode, Message: fmt.Sprintf("expected %d, got %d", expectedStatus, resp.StatusCode)}
}

// extractDomains returns the domains list from a ProtocolConfig map.
func extractDomains(pc map[string]any) []string {
	d, ok := pc["domains"]
	if !ok {
		return nil
	}
	list, ok := d.([]any)
	if !ok {
		return nil
	}
	var domains []string
	for _, v := range list {
		if s, ok := v.(string); ok {
			domains = append(domains, s)
		}
	}
	return domains
}

// providerDefaults holds default swap config for a well-known LLM provider.
type providerDefaults struct {
	Domains []string
	Header  string
	Format  string
}

// inferProviderDefaults returns swap defaults for well-known LLM provider
// credentials based on their name. Handles credentials stored before
// domain/header info was included in protocol_config.
func inferProviderDefaults(name string) *providerDefaults {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "anthropic"):
		return &providerDefaults{
			Domains: []string{"api.anthropic.com"},
			Header:  "x-api-key",
		}
	case strings.Contains(n, "openai"):
		return &providerDefaults{
			Domains: []string{"api.openai.com"},
			Header:  "Authorization",
			Format:  "Bearer {key}",
		}
	case strings.Contains(n, "google"), strings.Contains(n, "gemini"):
		return &providerDefaults{
			Domains: []string{"generativelanguage.googleapis.com"},
			Header:  "x-goog-api-key",
		}
	default:
		return nil
	}
}

// matchesFilter checks if flat backend metadata matches the given filter.
func matchesFilter(metadata map[string]string, filter Filter) bool {
	if filter.Kind != "" && metadata["kind"] != filter.Kind {
		return false
	}
	if filter.Scope != "" && metadata["scope"] != filter.Scope {
		return false
	}
	if filter.Service != "" && metadata["service"] != filter.Service {
		return false
	}
	if filter.Group != "" && metadata["group"] != filter.Group {
		return false
	}
	return true
}
