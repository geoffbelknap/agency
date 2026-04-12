package hub

import (
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/geoffbelknap/agency/internal/models"
)

var runtimePathParamPattern = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)

func deriveInstalledPackageSpec(kind, sourcePath string) (map[string]any, error) {
	switch kind {
	case "connector":
		return deriveConnectorPackageSpec(sourcePath)
	default:
		return nil, nil
	}
}

func deriveConnectorPackageSpec(sourcePath string) (map[string]any, error) {
	var cfg models.ConnectorConfig
	if err := models.Load(sourcePath, &cfg); err != nil {
		return nil, err
	}

	tools := cfg.Tools
	if len(tools) == 0 && cfg.MCP != nil {
		tools = cfg.MCP.Tools
	}
	if len(tools) == 0 || cfg.MCP == nil || cfg.MCP.APIBase == nil || strings.TrimSpace(*cfg.MCP.APIBase) == "" {
		return nil, nil
	}

	executor := map[string]any{
		"kind":     "http_json",
		"base_url": strings.TrimSpace(*cfg.MCP.APIBase),
		"actions":  map[string]any{},
	}
	if auth := deriveConnectorRuntimeAuth(&cfg); auth != nil {
		executor["auth"] = auth
	}

	actions := executor["actions"].(map[string]any)
	for _, tool := range tools {
		if strings.TrimSpace(tool.Path) == "" {
			continue
		}
		action := map[string]any{
			"method": strings.ToUpper(strings.TrimSpace(tool.Method)),
			"path":   tool.Path,
		}
		if action["method"] == "" {
			action["method"] = http.MethodGet
		}
		if tool.WhitelistCheck != "" {
			action["whitelist_field"] = tool.WhitelistCheck
		}
		if len(tool.QueryParams) > 0 {
			query := map[string]any{}
			for _, field := range tool.QueryParams {
				query[field] = field
			}
			action["query"] = query
		}
		body := deriveConnectorActionBody(tool)
		if len(body) > 0 {
			action["body"] = body
		}
		actions[tool.Name] = action
	}
	if len(actions) == 0 {
		return nil, nil
	}

	return map[string]any{
		"runtime": map[string]any{
			"executor": executor,
		},
	}, nil
}

func deriveConnectorRuntimeAuth(cfg *models.ConnectorConfig) map[string]any {
	if cfg == nil || cfg.MCP == nil || strings.TrimSpace(cfg.MCP.Credential) == "" {
		return nil
	}
	binding := cfg.MCP.Credential
	if cfg.Requires == nil || cfg.Requires.Auth == nil {
		return map[string]any{
			"type":    "bearer",
			"binding": binding,
		}
	}

	auth := cfg.Requires.Auth
	switch strings.TrimSpace(auth.Type) {
	case "", "none":
		return nil
	case "bearer":
		return map[string]any{
			"type":    "bearer",
			"binding": binding,
		}
	case "google_service_account":
		out := map[string]any{
			"type":    "google_service_account",
			"binding": binding,
		}
		if len(auth.Scopes) > 0 {
			scopes := make([]any, 0, len(auth.Scopes))
			for _, scope := range auth.Scopes {
				scopes = append(scopes, scope)
			}
			out["scopes"] = scopes
		}
		return out
	default:
		return nil
	}
}

func deriveConnectorActionBody(tool models.ConnectorMCPTool) map[string]any {
	method := strings.ToUpper(strings.TrimSpace(tool.Method))
	if method == http.MethodGet || method == http.MethodDelete {
		return nil
	}
	schema := tool.Parameters
	if len(schema) == 0 {
		schema = tool.InputSchema
	}
	if len(schema) == 0 {
		return nil
	}
	pathParams := map[string]bool{}
	for _, name := range runtimePathParamPattern.FindAllStringSubmatch(tool.Path, -1) {
		if len(name) == 2 {
			pathParams[name[1]] = true
		}
	}
	queryParams := map[string]bool{}
	for _, field := range tool.QueryParams {
		queryParams[field] = true
	}
	var consentTokenField string
	if tool.RequiresConsentToken != nil {
		consentTokenField = tool.RequiresConsentToken.TokenInputField
	}
	body := map[string]any{}
	for name := range schema {
		if pathParams[name] || queryParams[name] || name == consentTokenField {
			continue
		}
		body[name] = name
	}
	if len(body) == 0 {
		return nil
	}
	return body
}

func installedPackageTrust(source string) string {
	switch filepath.Clean(source) {
	case "official":
		return "verified"
	default:
		return "local"
	}
}

func (m *Manager) publishInstalledPackage(name, kind, version, source, destPath string) error {
	spec, err := deriveInstalledPackageSpec(kind, destPath)
	if err != nil {
		return err
	}
	return m.Registry.PutPackage(InstalledPackage{
		Kind:    kind,
		Name:    name,
		Version: version,
		Trust:   installedPackageTrust(source),
		Path:    destPath,
		Spec:    spec,
	})
}
