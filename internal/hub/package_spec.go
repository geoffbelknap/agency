package hub

import (
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/geoffbelknap/agency/internal/hubclient"
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
	if len(cfg.Runtime) > 0 {
		return map[string]any{
			"runtime": cfg.Runtime,
		}, nil
	}

	tools := connectorRuntimeTools(cfg)
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

func connectorRuntimeTools(cfg models.ConnectorConfig) []models.ConnectorMCPTool {
	seen := map[string]bool{}
	var tools []models.ConnectorMCPTool
	for _, tool := range cfg.Tools {
		if strings.TrimSpace(tool.Name) == "" || seen[tool.Name] {
			continue
		}
		seen[tool.Name] = true
		tools = append(tools, tool)
	}
	if cfg.MCP != nil {
		for _, tool := range cfg.MCP.Tools {
			if strings.TrimSpace(tool.Name) == "" || seen[tool.Name] {
				continue
			}
			seen[tool.Name] = true
			tools = append(tools, tool)
		}
	}
	return tools
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

func canonicalSourceName(source string) string {
	cleaned := filepath.Clean(strings.TrimSpace(source))
	if cleaned == "." || cleaned == "" {
		return ""
	}
	head, _, _ := strings.Cut(cleaned, "/")
	switch head {
	case "official", "default":
		return "official"
	default:
		return head
	}
}

func installedPackageTrust(source string) string {
	switch canonicalSourceName(source) {
	case "official":
		return "verified"
	default:
		return "local"
	}
}

func installedPackageAssurance(kind, source string) []string {
	statements := []string{"publisher_verified"}
	if canonicalSourceName(source) == "official" {
		statements = append(statements, "official_source")
	}
	switch {
	case canonicalSourceName(source) == "official" && kind == "connector":
		return append(statements, "ask_partial")
	default:
		return statements
	}
}

func installedPackageAssuranceFromStatements(statements []hubclient.AssuranceStatement) []string {
	if len(statements) == 0 {
		return nil
	}
	out := make([]string, 0, len(statements))
	appendIfMissing := func(value string) {
		for _, existing := range out {
			if existing == value {
				return
			}
		}
		out = append(out, value)
	}
	for _, stmt := range statements {
		switch {
		case stmt.StatementType == "source_verified" && stmt.Result == "verified":
			appendIfMissing("official_source")
		case stmt.StatementType == "publisher_verified" && stmt.Result == "verified":
			appendIfMissing("publisher_verified")
		case stmt.StatementType == "ask_reviewed" && stmt.Result == "ASK-Partial":
			appendIfMissing("ask_partial")
		case stmt.StatementType == "ask_reviewed" && stmt.Result == "ASK-Pass":
			appendIfMissing("ask_pass")
		}
	}
	return out
}

func buildInstalledPackage(name, kind, version, source, destPath string, statements []hubclient.AssuranceStatement) (InstalledPackage, error) {
	spec, err := deriveInstalledPackageSpec(kind, destPath)
	if err != nil {
		return InstalledPackage{}, err
	}
	assurance := installedPackageAssuranceFromStatements(statements)
	if len(assurance) == 0 {
		assurance = installedPackageAssurance(kind, source)
	}
	issuer := ""
	for _, stmt := range statements {
		if strings.TrimSpace(stmt.IssuerHubID) != "" {
			issuer = strings.TrimSpace(stmt.IssuerHubID)
			break
		}
	}
	return InstalledPackage{
		Kind:                kind,
		Name:                name,
		Version:             version,
		Trust:               installedPackageTrust(source),
		Path:                destPath,
		Spec:                spec,
		Assurance:           assurance,
		AssuranceStatements: statements,
		AssuranceIssuer:     issuer,
		Publisher:           source,
		ReviewScope:         "package-change",
	}, nil
}
