package hub

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/credstore"
	hubpkg "github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/models"
)

// connectorRequirements returns the current requirement status for a connector.
// GET /api/v1/hub/connectors/{name}/requirements
func (h *handler) connectorRequirements(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cc, err := h.loadConnectorConfig(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	result := map[string]interface{}{
		"connector": cc.Name,
		"version":   cc.Version,
		"ready":     true,
	}

	if cc.Requires == nil {
		result["credentials"] = []interface{}{}
		result["egress_domains"] = []interface{}{}
		writeJSON(w, 200, result)
		return
	}

	// Check credentials
	creds := make([]map[string]interface{}, 0, len(cc.Requires.Credentials))
	for _, cred := range cc.Requires.Credentials {
		configured := h.isCredentialConfigured(cred)
		if !configured {
			result["ready"] = false
		}
		creds = append(creds, map[string]interface{}{
			"name":        cred.Name,
			"description": cred.Description,
			"type":        cred.Type,
			"scope":       cred.Scope,
			"grant_name":  cred.GrantName,
			"setup_url":   cred.SetupURL,
			"example":     cred.Example,
			"configured":  configured,
		})
	}
	result["credentials"] = creds

	// Check auth (informational — auto-provisioned during activation)
	if cc.Requires.Auth != nil && cc.Requires.Auth.Type != "none" {
		authConfigured := h.isAuthConfigured(cc.Requires.Auth, cc.Requires)
		result["auth"] = map[string]interface{}{
			"type":       cc.Requires.Auth.Type,
			"configured": authConfigured,
		}
	}

	// Check egress domains (informational — auto-provisioned during activation)
	domains := make([]map[string]interface{}, 0, len(cc.Requires.EgressDomains))
	for _, domain := range cc.Requires.EgressDomains {
		allowed := h.isDomainAllowed(domain)
		domains = append(domains, map[string]interface{}{
			"domain":  domain,
			"allowed": allowed,
		})
	}
	result["egress_domains"] = domains

	writeJSON(w, 200, result)
}

// connectorConfigure provisions credentials, auth, and egress for a connector.
// POST /api/v1/hub/connectors/{name}/configure
func (h *handler) connectorConfigure(w http.ResponseWriter, r *http.Request) {
	// Localhost enforcement (ASK Tenet 3 — secrets only via local path)
	if !isLocalOrTLS(r) {
		writeJSON(w, 403, map[string]string{"error": "configure endpoint requires localhost or TLS"})
		return
	}

	name := chi.URLParam(r, "name")
	cc, err := h.loadConnectorConfig(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	var body struct {
		Credentials map[string]string `json:"credentials"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Credentials == nil {
		body.Credentials = map[string]string{}
	}

	result := map[string]interface{}{
		"configured":           []string{},
		"auth_configured":      false,
		"egress_domains_added": []string{},
		"ready":                true,
	}

	if cc.Requires == nil {
		writeJSON(w, 200, result)
		return
	}

	configured := []string{}

	// Configure credentials
	for _, cred := range cc.Requires.Credentials {
		value, provided := body.Credentials[cred.Name]
		if !provided {
			if !h.isCredentialConfigured(cred) {
				result["ready"] = false
			} else {
				configured = append(configured, cred.Name)
			}
			continue
		}
		if h.isCredentialConfigured(cred) {
			configured = append(configured, cred.Name)
			continue
		}
		if err := h.writeCredential(cred, value); err != nil {
			writeJSON(w, 500, map[string]string{"error": fmt.Sprintf("failed to write credential %s: %v", cred.Name, err)})
			return
		}
		configured = append(configured, cred.Name)
	}
	result["configured"] = configured

	// Configure auth
	if cc.Requires.Auth != nil && cc.Requires.Auth.Type == "jwt-exchange" {
		if !h.isAuthConfigured(cc.Requires.Auth, cc.Requires) {
			if err := h.writeJWTSwap(cc.Requires.Auth, cc.Requires); err != nil {
				writeJSON(w, 500, map[string]string{"error": "failed to write jwt-swap config: " + err.Error()})
				return
			}
		}
		result["auth_configured"] = true
	}

	// Configure egress domains
	domainsAdded := []string{}
	for _, domain := range cc.Requires.EgressDomains {
		h.addEgressDomainProvenance(domain, "connector", cc.Name)
		domainsAdded = append(domainsAdded, domain)
	}
	result["egress_domains_added"] = domainsAdded

	// Regenerate credential-swaps.yaml
	h.regenerateSwapConfig()

	// Audit
	h.deps.Audit.Write(cc.Name, "connector_configure", map[string]interface{}{
		"credentials_set":    configured,
		"egress_domains_set": domainsAdded,
	})

	writeJSON(w, 200, result)
}

// egressDomains lists all egress domains with provenance.
// GET /api/v1/hub/egress/domains
func (h *handler) egressDomains(w http.ResponseWriter, r *http.Request) {
	data := h.loadDomainProvenance()
	domains := make([]map[string]interface{}, 0)
	for domain, entry := range data {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		domains = append(domains, map[string]interface{}{
			"domain":       domain,
			"sources":      entryMap["sources"],
			"auto_managed": entryMap["auto_managed"],
		})
	}
	writeJSON(w, 200, map[string]interface{}{"domains": domains})
}

// egressDomainProvenance returns provenance for a specific domain.
// GET /api/v1/hub/egress/domains/{domain}/provenance
func (h *handler) egressDomainProvenance(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")
	data := h.loadDomainProvenance()

	entry, ok := data[strings.ToLower(domain)]
	if !ok {
		writeJSON(w, 404, map[string]string{"error": fmt.Sprintf("domain %q not tracked", domain)})
		return
	}
	entryMap, ok := entry.(map[string]interface{})
	if !ok {
		writeJSON(w, 404, map[string]string{"error": fmt.Sprintf("domain %q has invalid provenance", domain)})
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"domain":       strings.ToLower(domain),
		"sources":      entryMap["sources"],
		"auto_managed": entryMap["auto_managed"],
	})
}

// ── helpers ────────────────────────────────────────────────────────────────

// loadConnectorConfig locates and parses a connector.yaml for the given name.
// It first checks the hub instance registry, then falls back to
// ~/.agency/connectors/{name}/connector.yaml.
func (h *handler) loadConnectorConfig(name string) (*models.ConnectorConfig, error) {
	if !requireNameStr(name) {
		return nil, fmt.Errorf("invalid connector name")
	}
	mgr := hubpkg.NewManager(h.deps.Config.Home)
	inst := mgr.Registry.Resolve(name)

	var yamlPath string
	if inst != nil {
		instDir := mgr.Registry.InstanceDir(name)
		yamlPath = filepath.Join(instDir, "connector.yaml")
	} else {
		yamlPath = filepath.Join(h.deps.Config.Home, "connectors", name, "connector.yaml")
	}

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("connector %q not found", name)
	}

	var cc models.ConnectorConfig
	if err := yaml.Unmarshal(data, &cc); err != nil {
		return nil, fmt.Errorf("invalid connector YAML: %w", err)
	}
	return &cc, nil
}

// isCredentialConfigured checks whether a credential is already provisioned.
func (h *handler) isCredentialConfigured(cred models.ConnectorCredential) bool {
	switch cred.Scope {
	case "service-grant":
		grantName := cred.GrantName
		if grantName == "" {
			grantName = cred.Name
		}
		if h.deps.CredStore != nil {
			_, err := h.deps.CredStore.Get(grantName)
			return err == nil
		}
		return false
	case "env-var":
		if os.Getenv(cred.Name) != "" {
			return true
		}
		return h.envFileHasKey(filepath.Join(h.deps.Config.Home, ".env"), cred.Name)
	default:
		return false
	}
}

// envFileHasKey checks whether a KEY=... line exists in an env-style file.
func (h *handler) envFileHasKey(path, key string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	prefix := key + "="
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// isAuthConfigured checks whether auth (jwt-exchange) is configured.
func (h *handler) isAuthConfigured(auth *models.ConnectorAuth, requires *models.ConnectorRequires) bool {
	if auth.Type != "jwt-exchange" {
		return true
	}
	path := filepath.Join(h.deps.Config.Home, "secrets", "jwt-swap.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var swapData map[string]interface{}
	if err := yaml.Unmarshal(data, &swapData); err != nil {
		return false
	}
	serviceName := findServiceName(requires)
	_, ok := swapData[serviceName]
	return ok
}

// isDomainAllowed checks the provenance file for a domain entry.
func (h *handler) isDomainAllowed(domain string) bool {
	data := h.loadDomainProvenance()
	_, ok := data[strings.ToLower(domain)]
	return ok
}

// loadDomainProvenance reads the egress domain provenance YAML.
func (h *handler) loadDomainProvenance() map[string]interface{} {
	path := filepath.Join(h.deps.Config.Home, "egress", "domain-provenance.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]interface{}{}
	}
	var result map[string]interface{}
	if err := yaml.Unmarshal(data, &result); err != nil {
		return map[string]interface{}{}
	}
	return result
}

// writeCredential writes a credential value to the appropriate store.
func (h *handler) writeCredential(cred models.ConnectorCredential, value string) error {
	switch cred.Scope {
	case "service-grant":
		grantName := cred.GrantName
		if grantName == "" {
			grantName = cred.Name
		}
		if h.deps.CredStore == nil {
			return fmt.Errorf("credential store not initialized")
		}
		now := time.Now().UTC().Format(time.RFC3339)
		if err := h.deps.CredStore.Put(credstore.Entry{
			Name:  grantName,
			Value: value,
			Metadata: credstore.Metadata{
				Kind:      credstore.KindService,
				Scope:     "platform",
				Protocol:  credstore.ProtocolAPIKey,
				Source:    "connector",
				CreatedAt: now,
				RotatedAt: now,
			},
		}); err != nil {
			return err
		}
		h.regenerateSwapConfig()
		return nil
	case "env-var":
		path := filepath.Join(h.deps.Config.Home, ".env")
		return h.upsertEnvEntry(path, cred.Name, value, 0644)
	default:
		return fmt.Errorf("unsupported credential scope: %s", cred.Scope)
	}
}

// upsertEnvEntry appends KEY=VALUE to a file if the key is not already present.
func (h *handler) upsertEnvEntry(path, key, value string, perm os.FileMode) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	existing, _ := os.ReadFile(path)
	prefix := key + "="
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.HasPrefix(line, prefix) {
			return nil // already present
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	_, err = fmt.Fprintf(f, "%s=%s\n", key, value)
	if err != nil {
		return err
	}
	return os.Chmod(path, perm)
}

// writeJWTSwap generates a jwt-swap.yaml entry for the connector.
func (h *handler) writeJWTSwap(auth *models.ConnectorAuth, requires *models.ConnectorRequires) error {
	path := filepath.Join(h.deps.Config.Home, "secrets", "jwt-swap.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	var existing map[string]interface{}
	data, err := os.ReadFile(path)
	if err == nil {
		yaml.Unmarshal(data, &existing)
	}
	if existing == nil {
		existing = map[string]interface{}{}
	}

	serviceName := findServiceName(requires)
	if _, ok := existing[serviceName]; ok {
		return nil // idempotent
	}

	matchDomains := make([]string, len(requires.EgressDomains))
	copy(matchDomains, requires.EgressDomains)

	existing[serviceName] = map[string]interface{}{
		"token_url":            auth.TokenURL,
		"token_params":         auth.TokenParams,
		"token_response_field": auth.TokenResponseField,
		"token_ttl_seconds":    auth.TokenTTLSeconds,
		"match_domains":        matchDomains,
	}

	out, err := yaml.Marshal(existing)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		return err
	}
	return nil
}

// addEgressDomainProvenance records a domain's provenance. Idempotent.
func (h *handler) addEgressDomainProvenance(domain, sourceType, sourceName string) {
	path := filepath.Join(h.deps.Config.Home, "egress", "domain-provenance.yaml")
	os.MkdirAll(filepath.Dir(path), 0755)

	domain = strings.ToLower(domain)

	var data map[string]interface{}
	raw, err := os.ReadFile(path)
	if err == nil {
		yaml.Unmarshal(raw, &data)
	}
	if data == nil {
		data = map[string]interface{}{}
	}

	entry, ok := data[domain]
	if !ok {
		data[domain] = map[string]interface{}{
			"sources": []interface{}{
				map[string]interface{}{
					"type":     sourceType,
					"name":     sourceName,
					"added_at": time.Now().UTC().Format(time.RFC3339),
				},
			},
			"auto_managed": sourceType != "operator",
		}
	} else if entryMap, ok := entry.(map[string]interface{}); ok {
		sources, _ := entryMap["sources"].([]interface{})
		for _, s := range sources {
			if sm, ok := s.(map[string]interface{}); ok {
				if sm["type"] == sourceType && sm["name"] == sourceName {
					return // already recorded
				}
			}
		}
		sources = append(sources, map[string]interface{}{
			"type":     sourceType,
			"name":     sourceName,
			"added_at": time.Now().UTC().Format(time.RFC3339),
		})
		entryMap["sources"] = sources
		if sourceType == "operator" {
			entryMap["auto_managed"] = false
		}
	}

	out, _ := yaml.Marshal(data)
	os.WriteFile(path, out, 0600)
}

// findServiceName extracts the service name for jwt-swap key.
func findServiceName(requires *models.ConnectorRequires) string {
	for _, cred := range requires.Credentials {
		if cred.Scope == "service-grant" && cred.GrantName != "" {
			return cred.GrantName
		}
	}
	if len(requires.Services) > 0 {
		return requires.Services[0]
	}
	return ""
}

// isLocalOrTLS checks if the request is local operator traffic or uses TLS.
func isLocalOrTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if isLoopbackHostPort(r.RemoteAddr) || isLoopbackHostPort(r.Host) {
		return true
	}
	return isLoopbackURLHeader(r.Header.Get("Origin")) || isLoopbackURLHeader(r.Header.Get("Referer"))
}

func isLoopbackURLHeader(value string) bool {
	if value == "" {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return isLoopbackHostPort(parsed.Host)
}

func isLoopbackHostPort(value string) bool {
	if value == "" {
		return false
	}
	hostPart, _, err := net.SplitHostPort(value)
	if err != nil {
		hostPart = value
	}
	host := strings.Trim(hostPart, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
