package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type AuthorityExecutionResult struct {
	StatusCode int    `json:"status_code"`
	Body       any    `json:"body,omitempty"`
	RawBody    string `json:"raw_body,omitempty"`
}

var (
	pathTokenPattern = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)

	tokenSourceMu    sync.Mutex
	tokenSourceCache = map[string]oauth2.TokenSource{}
)

func ExecuteAuthority(ctx context.Context, manifest *Manifest, node *RuntimeNode, req AuthorityInvokeRequest) (*AuthorityExecutionResult, error) {
	if node.Executor == nil {
		return nil, nil
	}
	switch node.Executor.Kind {
	case "http_json":
		return executeHTTPJSON(ctx, manifest, node, req)
	default:
		return nil, fmt.Errorf("unsupported executor kind %q", node.Executor.Kind)
	}
}

func executeHTTPJSON(ctx context.Context, manifest *Manifest, node *RuntimeNode, req AuthorityInvokeRequest) (*AuthorityExecutionResult, error) {
	action, ok := node.Executor.Actions[req.Action]
	if !ok {
		return nil, fmt.Errorf("action %q not defined for node %q", req.Action, node.NodeID)
	}
	method := action.Method
	if method == "" {
		method = http.MethodPost
	}
	input := inputMap(req.Input)
	if err := enforceActionWhitelist(node, action, input); err != nil {
		return nil, err
	}
	path, err := renderActionPath(action.Path, input)
	if err != nil {
		return nil, fmt.Errorf("render executor path: %w", err)
	}
	baseURL := strings.TrimRight(node.Executor.BaseURL, "/")
	requestURL, err := url.Parse(baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse executor URL: %w", err)
	}
	if err := applyActionQuery(requestURL, action.Query, input); err != nil {
		return nil, fmt.Errorf("render executor query: %w", err)
	}

	bodyReader, contentType, err := buildActionBody(method, action, input)
	if err != nil {
		return nil, fmt.Errorf("build executor body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, requestURL.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build executor request: %w", err)
	}
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	for key, value := range action.Headers {
		httpReq.Header.Set(key, value)
	}
	if node.Executor.Auth != nil {
		headerValue, err := resolveExecutorAuthHeader(ctx, manifest, node.Executor.Auth)
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set(node.Executor.Auth.Header, headerValue)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute authority request: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read executor response: %w", err)
	}
	result := &AuthorityExecutionResult{
		StatusCode: resp.StatusCode,
		RawBody:    string(rawBody),
	}
	var parsed any
	if len(rawBody) > 0 && json.Unmarshal(rawBody, &parsed) == nil {
		result.Body = parsed
	}
	return result, nil
}

func inputMap(raw map[string]any) map[string]any {
	if raw == nil {
		return map[string]any{}
	}
	return raw
}

func renderActionPath(path string, input map[string]any) (string, error) {
	var renderErr error
	rendered := pathTokenPattern.ReplaceAllStringFunc(path, func(token string) string {
		if renderErr != nil {
			return ""
		}
		match := pathTokenPattern.FindStringSubmatch(token)
		if len(match) != 2 {
			return token
		}
		value, ok := input[match[1]]
		if !ok {
			renderErr = fmt.Errorf("missing input %q", match[1])
			return ""
		}
		return url.PathEscape(fmt.Sprint(value))
	})
	if renderErr != nil {
		return "", renderErr
	}
	return rendered, nil
}

func applyActionQuery(requestURL *url.URL, queryMap map[string]string, input map[string]any) error {
	if len(queryMap) == 0 {
		return nil
	}
	values := requestURL.Query()
	for key, field := range queryMap {
		field = strings.TrimSpace(field)
		if field == "" {
			return fmt.Errorf("query field mapping for %q is empty", key)
		}
		value, ok := input[field]
		if !ok {
			continue
		}
		values.Set(key, fmt.Sprint(value))
	}
	requestURL.RawQuery = values.Encode()
	return nil
}

func buildActionBody(method string, action RuntimeHTTPAction, input map[string]any) (io.Reader, string, error) {
	if len(action.Body) > 0 {
		payload := make(map[string]any, len(action.Body))
		for outKey, inKey := range action.Body {
			inKey = strings.TrimSpace(inKey)
			if inKey == "" {
				return nil, "", fmt.Errorf("body field mapping for %q is empty", outKey)
			}
			value, ok := input[inKey]
			if !ok {
				continue
			}
			payload[outKey] = value
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, "", fmt.Errorf("marshal mapped body: %w", err)
		}
		return bytes.NewReader(raw), "application/json", nil
	}
	switch method {
	case http.MethodGet, http.MethodDelete:
		return nil, "", nil
	}
	if len(input) == 0 {
		return nil, "", nil
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, "", fmt.Errorf("marshal executor input: %w", err)
	}
	return bytes.NewReader(raw), "application/json", nil
}

func resolveExecutorAuthHeader(ctx context.Context, manifest *Manifest, auth *RuntimeExecutorAuth) (string, error) {
	switch auth.Type {
	case "bearer", "header":
		secret, err := resolveExecutorBinding(manifest, auth.Binding)
		if err != nil {
			return "", err
		}
		value := secret
		if auth.Type == "bearer" {
			value = auth.Prefix + secret
		}
		return value, nil
	case "google_service_account":
		token, err := resolveGoogleServiceAccountToken(ctx, manifest, auth)
		if err != nil {
			return "", err
		}
		return auth.Prefix + token, nil
	default:
		return "", fmt.Errorf("unsupported executor auth type %q", auth.Type)
	}
}

func enforceActionWhitelist(node *RuntimeNode, action RuntimeHTTPAction, input map[string]any) error {
	field := strings.TrimSpace(action.WhitelistField)
	if field == "" {
		return nil
	}
	value, ok := input[field]
	if !ok {
		return fmt.Errorf("missing whitelist input %q", field)
	}
	targetID := strings.TrimSpace(fmt.Sprint(value))
	if targetID == "" {
		return fmt.Errorf("whitelist input %q is empty", field)
	}
	expectedKind := strings.TrimSpace(action.WhitelistKind)
	if expectedKind == "" {
		expectedKind = inferWhitelistKind(field)
	}
	for _, entry := range node.ResourceWhitelist {
		if entry.ID != targetID {
			continue
		}
		if expectedKind == "" || entry.Kind == "" || strings.EqualFold(entry.Kind, expectedKind) {
			return nil
		}
	}
	if expectedKind != "" {
		return fmt.Errorf("resource %q of kind %q is not whitelisted", targetID, expectedKind)
	}
	return fmt.Errorf("resource %q is not whitelisted", targetID)
}

func inferWhitelistKind(field string) string {
	switch {
	case strings.HasPrefix(field, "file_"):
		return "file"
	case strings.HasPrefix(field, "folder_"):
		return "folder"
	default:
		return ""
	}
}

func resolveGoogleServiceAccountToken(ctx context.Context, manifest *Manifest, auth *RuntimeExecutorAuth) (string, error) {
	key := auth.Binding + "::" + strings.Join(auth.Scopes, ",")

	tokenSourceMu.Lock()
	source, ok := tokenSourceCache[key]
	tokenSourceMu.Unlock()

	if !ok {
		secret, err := resolveExecutorBinding(manifest, auth.Binding)
		if err != nil {
			return "", err
		}
		cfg, err := google.JWTConfigFromJSON([]byte(secret), auth.Scopes...)
		if err != nil {
			return "", fmt.Errorf("parse google service account credentials: %w", err)
		}
		source = cfg.TokenSource(ctx)
		tokenSourceMu.Lock()
		tokenSourceCache[key] = source
		tokenSourceMu.Unlock()
	}

	token, err := source.Token()
	if err != nil {
		return "", fmt.Errorf("fetch google service account token: %w", err)
	}
	return token.AccessToken, nil
}

func resolveExecutorBinding(manifest *Manifest, bindingName string) (string, error) {
	var binding *RuntimeBinding
	for i := range manifest.Runtime.Bindings {
		if manifest.Runtime.Bindings[i].Name == bindingName {
			binding = &manifest.Runtime.Bindings[i]
			break
		}
	}
	if binding == nil {
		return "", fmt.Errorf("binding %q not found", bindingName)
	}
	if binding.Type != "credref" {
		return "", fmt.Errorf("binding %q uses unsupported type %q", bindingName, binding.Type)
	}
	target := strings.TrimPrefix(binding.Target, "credref:")
	if target == "" {
		return "", fmt.Errorf("binding %q has empty credref target", bindingName)
	}

	cfg := config.Load()
	backend, err := credstore.NewFileBackend(
		filepath.Join(cfg.Home, "credentials", "store.enc"),
		filepath.Join(cfg.Home, "credentials", ".key"),
	)
	if err != nil {
		return "", fmt.Errorf("init credstore backend: %w", err)
	}
	store := credstore.NewStore(backend, cfg.Home)
	entry, err := store.Get(target)
	if err != nil {
		return "", fmt.Errorf("resolve credref %q: %w", target, err)
	}
	return entry.Value, nil
}
