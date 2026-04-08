package hub

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SwapConfigFile is the top-level credential-swaps.yaml structure.
type SwapConfigFile struct {
	Swaps map[string]SwapEntry `yaml:"swaps"`
}

// SwapEntry is a single credential swap rule.
type SwapEntry struct {
	Type               string            `yaml:"type"`
	Domains            []string          `yaml:"domains"`
	Header             string            `yaml:"header,omitempty"`
	Format             string            `yaml:"format,omitempty"`
	KeyRef             string            `yaml:"key_ref,omitempty"`
	TokenURL           string            `yaml:"token_url,omitempty"`
	TokenParams        map[string]string `yaml:"token_params,omitempty"`
	TokenResponseField string            `yaml:"token_response_field,omitempty"`
	TokenTTLSeconds    int               `yaml:"token_ttl_seconds,omitempty"`
	InjectHeader       string            `yaml:"inject_header,omitempty"`
	InjectFormat       string            `yaml:"inject_format,omitempty"`
	AppIDRef           string            `yaml:"app_id_ref,omitempty"`
	PrivateKeyPath     string            `yaml:"private_key_path,omitempty"`
	InstallationIDRef  string            `yaml:"installation_id_ref,omitempty"`
	BodyField          string            `yaml:"body_field,omitempty"`
}

// GenerateSwapConfig builds credential-swaps.yaml content from service
// definitions, routing.yaml provider entries, and jwt-swap.yaml entries.
func GenerateSwapConfig(home string) ([]byte, error) {
	home = resolveHome(home)
	cfg := SwapConfigFile{Swaps: map[string]SwapEntry{}}

	// Pre-load JWT swap configs so service definitions that hit JWT-protected
	// domains get jwt-exchange entries instead of plain api-key entries.
	type jwtConfig struct {
		TokenURL           string            `yaml:"token_url"`
		TokenParams        map[string]string `yaml:"token_params"`
		TokenResponseField string            `yaml:"token_response_field"`
		TokenTTLSeconds    int               `yaml:"token_ttl_seconds"`
		InjectHeader       string            `yaml:"inject_header"`
		InjectFormat       string            `yaml:"inject_format"`
		MatchDomains       []string          `yaml:"match_domains"`
	}
	jwtByDomain := map[string]jwtConfig{}
	jwtPath := filepath.Join(home, "secrets", "jwt-swap.yaml")
	if data, err := os.ReadFile(jwtPath); err == nil {
		var jwtSwaps map[string]jwtConfig
		if yaml.Unmarshal(data, &jwtSwaps) == nil {
			for _, jwt := range jwtSwaps {
				for _, d := range jwt.MatchDomains {
					jwtByDomain[d] = jwt
				}
			}
		}
	}

	// 1. Read service definitions → swap entries (api-key or jwt-exchange)
	svcDir := filepath.Join(home, "registry", "services")
	entries, _ := os.ReadDir(svcDir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(svcDir, e.Name()))
		if err != nil {
			continue
		}
		var svc struct {
			Service    string `yaml:"service"`
			APIBase    string `yaml:"api_base"`
			Credential struct {
				EnvVar       string `yaml:"env_var"`
				Header       string `yaml:"header"`
				Format       string `yaml:"format"`
				ScopedPrefix string `yaml:"scoped_prefix"`
			} `yaml:"credential"`
		}
		if err := yaml.Unmarshal(data, &svc); err != nil || svc.Service == "" {
			continue
		}

		domain := extractSwapDomain(svc.APIBase)
		if domain == "" {
			continue
		}

		// Check if this domain requires JWT exchange
		if jwtCfg, ok := jwtByDomain[domain]; ok {
			// JWT exchange — use service's env_var as key_ref, inherit JWT params
			entry := SwapEntry{
				Type:               "jwt-exchange",
				Domains:            []string{domain},
				KeyRef:             svc.Credential.EnvVar,
				TokenURL:           jwtCfg.TokenURL,
				TokenParams:        jwtCfg.TokenParams,
				TokenResponseField: jwtCfg.TokenResponseField,
				TokenTTLSeconds:    jwtCfg.TokenTTLSeconds,
				InjectHeader:       jwtCfg.InjectHeader,
				InjectFormat:       jwtCfg.InjectFormat,
			}
			cfg.Swaps[svc.Service] = entry
		} else {
			// Plain API key injection
			entry := SwapEntry{
				Type:    "api-key",
				Domains: []string{domain},
				Header:  svc.Credential.Header,
				Format:  svc.Credential.Format,
				KeyRef:  svc.Credential.EnvVar,
			}
			cfg.Swaps[svc.Service] = entry
		}
	}

	// 2. Read routing.yaml → provider api-key entries
	routingPath := filepath.Join(home, "infrastructure", "routing.yaml")
	if data, err := os.ReadFile(routingPath); err == nil {
		var routing struct {
			Providers map[string]struct {
				APIBase    string `yaml:"api_base"`
				AuthEnv    string `yaml:"auth_env"`
				AuthHeader string `yaml:"auth_header"`
				AuthPrefix string `yaml:"auth_prefix"`
			} `yaml:"providers"`
		}
		if err := yaml.Unmarshal(data, &routing); err == nil {
			for name, prov := range routing.Providers {
				if prov.APIBase == "" || prov.AuthEnv == "" {
					continue
				}
				domain := extractSwapDomain(prov.APIBase)
				if domain == "" {
					continue
				}
				entry := SwapEntry{
					Type:    "api-key",
					Domains: []string{domain},
					Header:  prov.AuthHeader,
					KeyRef:  prov.AuthEnv,
				}
				if prov.AuthPrefix != "" {
					entry.Format = prov.AuthPrefix + "{key}"
				}
				if _, exists := cfg.Swaps[name]; !exists {
					cfg.Swaps[name] = entry
				}
			}
		}
	}

	// 3. Read jwt-swap.yaml → jwt-exchange entries (domain-based fallback)
	jwtPath = filepath.Join(home, "secrets", "jwt-swap.yaml")
	if data, err := os.ReadFile(jwtPath); err == nil {
		var jwtSwaps map[string]struct {
			TokenURL           string            `yaml:"token_url"`
			TokenParams        map[string]string `yaml:"token_params"`
			TokenResponseField string            `yaml:"token_response_field"`
			TokenTTLSeconds    int               `yaml:"token_ttl_seconds"`
			InjectHeader       string            `yaml:"inject_header"`
			InjectFormat       string            `yaml:"inject_format"`
			MatchDomains       []string          `yaml:"match_domains"`
		}
		if err := yaml.Unmarshal(data, &jwtSwaps); err == nil {
			for name, jwt := range jwtSwaps {
				// Exclude the token_url domain from match_domains to prevent
				// recursive credential injection on the token exchange request.
				tokenDomain := extractSwapDomain(jwt.TokenURL)
				var domains []string
				for _, d := range jwt.MatchDomains {
					if d != tokenDomain {
						domains = append(domains, d)
					}
				}
				entry := SwapEntry{
					Type:               "jwt-exchange",
					Domains:            domains,
					TokenURL:           jwt.TokenURL,
					TokenParams:        jwt.TokenParams,
					TokenResponseField: jwt.TokenResponseField,
					TokenTTLSeconds:    jwt.TokenTTLSeconds,
					InjectHeader:       jwt.InjectHeader,
					InjectFormat:       jwt.InjectFormat,
					KeyRef:             name,
				}
				cfg.Swaps[name] = entry

				// Auto-generate a body-key-swap for the JWT token URL domain.
				// This enables CLI tools (like limacharlie) that do their own
				// JWT exchange — the proxy transparently replaces the scoped
				// placeholder key in the POST body with the real credential.
				if tokenDomain != "" {
					bodySwapName := name + "-jwt"
					if _, exists := cfg.Swaps[bodySwapName]; !exists {
						// Determine which body field holds the credential.
						// Default to "secret" (LC convention); check token_params
						// for a ${credential} reference to find the actual field name.
						bodyField := "secret"
						for k, v := range jwt.TokenParams {
							if v == "${credential}" {
								bodyField = k
								break
							}
						}
						cfg.Swaps[bodySwapName] = SwapEntry{
							Type:      "body-key-swap",
							Domains:   []string{tokenDomain},
							KeyRef:    name,
							BodyField: bodyField,
						}
					}
				}
			}
		}
	}

	return yaml.Marshal(cfg)
}

// WriteSwapConfig generates and writes credential-swaps.yaml.
func WriteSwapConfig(home string) error {
	home = resolveHome(home)
	data, err := GenerateSwapConfig(home)
	if err != nil {
		return fmt.Errorf("generate swap config: %w", err)
	}
	destDir := filepath.Join(home, "infrastructure")
	os.MkdirAll(destDir, 0755)
	return os.WriteFile(filepath.Join(destDir, "credential-swaps.yaml"), data, 0644)
}

func extractSwapDomain(apiBase string) string {
	d := apiBase
	if idx := strings.Index(d, "://"); idx >= 0 {
		d = d[idx+3:]
	}
	d = strings.SplitN(d, "/", 2)[0]
	d = strings.SplitN(d, ":", 2)[0]
	return strings.ToLower(d)
}

func resolveHome(home string) string {
	if home != "" {
		return home
	}
	if envHome := os.Getenv("AGENCY_HOME"); envHome != "" {
		return envHome
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ".agency"
	}
	return filepath.Join(userHome, ".agency")
}
