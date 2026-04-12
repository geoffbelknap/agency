package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
)

type AuthorityExecutionResult struct {
	StatusCode int    `json:"status_code"`
	Body       any    `json:"body,omitempty"`
	RawBody    string `json:"raw_body,omitempty"`
}

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
	url := strings.TrimRight(node.Executor.BaseURL, "/") + action.Path

	bodyData := req.Input
	if bodyData == nil {
		bodyData = map[string]any{}
	}
	payload, err := json.Marshal(bodyData)
	if err != nil {
		return nil, fmt.Errorf("marshal executor input: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build executor request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for key, value := range action.Headers {
		httpReq.Header.Set(key, value)
	}
	if node.Executor.Auth != nil {
		secret, err := resolveExecutorBinding(manifest, node.Executor.Auth.Binding)
		if err != nil {
			return nil, err
		}
		value := secret
		if node.Executor.Auth.Type == "bearer" {
			value = node.Executor.Auth.Prefix + secret
		}
		httpReq.Header.Set(node.Executor.Auth.Header, value)
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
