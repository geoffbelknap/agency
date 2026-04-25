package providerenv

import (
	"net/url"
	"sort"
	"strings"

	"github.com/geoffbelknap/agency/internal/providercatalog"
)

func CredentialEnvVars() []string {
	providers, err := providercatalog.List()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, provider := range providers {
		envVar, _ := provider.Credential["env_var"].(string)
		envVar = strings.TrimSpace(envVar)
		if envVar == "" || seen[envVar] {
			continue
		}
		seen[envVar] = true
		out = append(out, envVar)
	}
	sort.Strings(out)
	return out
}

func APIBaseDomains() []string {
	providers, err := providercatalog.List()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, provider := range providers {
		apiBase, _ := provider.Routing["api_base"].(string)
		if strings.TrimSpace(apiBase) == "" {
			continue
		}
		parsed, err := url.Parse(strings.TrimSpace(apiBase))
		if err != nil {
			continue
		}
		host := strings.TrimSpace(parsed.Hostname())
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, host)
	}
	sort.Strings(out)
	return out
}

func ForbiddenWorkspaceEnvVars() []string {
	seen := map[string]bool{
		"AWS_SECRET_ACCESS_KEY": true,
	}
	out := []string{"AWS_SECRET_ACCESS_KEY"}
	for _, envVar := range CredentialEnvVars() {
		if seen[envVar] {
			continue
		}
		seen[envVar] = true
		out = append(out, envVar)
	}
	sort.Strings(out)
	return out
}

func LeakedWorkspaceCredentialNames(env []string) []string {
	keys := ForbiddenWorkspaceEnvVars()
	var leaked []string
	for _, envVar := range env {
		for _, key := range keys {
			if !strings.HasPrefix(envVar, key+"=") {
				continue
			}
			parts := strings.SplitN(envVar, "=", 2)
			if len(parts) == 2 && parts[1] != "" {
				leaked = append(leaked, key)
			}
		}
	}
	return leaked
}
