package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type providerRequestContext struct {
	RequestPath   string
	TargetURL     string
	ProviderName  string
	ProviderModel string
	Provider      Provider
	Body          map[string]interface{}
	Stream        bool
}

type providerPreparedRequest struct {
	TargetURL string
	Body      []byte
}

type providerRelayContext struct {
	Response         *http.Response
	ModelAlias       string
	ProviderName     string
	ProviderModel    string
	CorrelationID    string
	EventID          string
	Start            time.Time
	StepIndex        int
	RetryOf          string
	ProviderToolUses []ProviderToolUse
	Stream           bool
	ResponsesPath    bool
}

type providerAdapter interface {
	PrepareRequest(providerRequestContext) (providerPreparedRequest, error)
	AddHeaders(*http.Request)
	RelayResponse(*LLMHandler, http.ResponseWriter, providerRelayContext)
}

func providerAdapterFor(_ string, provider Provider) providerAdapter {
	switch provider.APIFormat {
	case "gemini":
		return geminiProviderAdapter{}
	case "anthropic":
		return anthropicProviderAdapter{}
	}
	return openAICompatibleProviderAdapter{}
}

type openAICompatibleProviderAdapter struct{}

func (openAICompatibleProviderAdapter) PrepareRequest(ctx providerRequestContext) (providerPreparedRequest, error) {
	targetURL := ctx.TargetURL
	if strings.HasPrefix(ctx.RequestPath, "/v1/") {
		endpoint := ctx.RequestPath[3:]
		base := strings.TrimRight(ctx.Provider.APIBase, "/")
		targetURL = base + endpoint
	}
	ctx.Body["model"] = ctx.ProviderModel
	if ctx.Stream && ctx.RequestPath != "/v1/responses" {
		ensureStreamUsageRequested(ctx.Body)
	}
	body, err := json.Marshal(ctx.Body)
	if err != nil {
		return providerPreparedRequest{}, fmt.Errorf("rewrite request body: %w", err)
	}
	return providerPreparedRequest{TargetURL: targetURL, Body: body}, nil
}

func (openAICompatibleProviderAdapter) AddHeaders(*http.Request) {}

func (openAICompatibleProviderAdapter) RelayResponse(lh *LLMHandler, w http.ResponseWriter, ctx providerRelayContext) {
	if ctx.Stream {
		lh.relayStream(w, ctx.Response, ctx.ModelAlias, ctx.ProviderName, ctx.ProviderModel, ctx.CorrelationID, ctx.EventID, ctx.Start, ctx.StepIndex, ctx.RetryOf, ctx.ProviderToolUses)
		return
	}
	lh.relayBuffered(w, ctx.Response, ctx.ModelAlias, ctx.ProviderName, ctx.ProviderModel, ctx.CorrelationID, ctx.EventID, ctx.Start, ctx.StepIndex, ctx.RetryOf, ctx.ProviderToolUses)
}

type anthropicProviderAdapter struct{}

func (anthropicProviderAdapter) PrepareRequest(ctx providerRequestContext) (providerPreparedRequest, error) {
	if ctx.RequestPath == "/v1/responses" {
		return providerPreparedRequest{}, fmt.Errorf("responses endpoint is not supported for %s models", ctx.ProviderName)
	}
	ctx.Body["model"] = ctx.ProviderModel
	body, err := json.Marshal(ctx.Body)
	if err != nil {
		return providerPreparedRequest{}, fmt.Errorf("rewrite request body: %w", err)
	}
	body, err = translateToAnthropic(body, ctx.Provider.CachingEnabled())
	if err != nil {
		return providerPreparedRequest{}, fmt.Errorf("translate request: %w", err)
	}
	return providerPreparedRequest{TargetURL: ctx.TargetURL, Body: body}, nil
}

func (anthropicProviderAdapter) AddHeaders(req *http.Request) {
	req.Header.Set("anthropic-version", "2023-06-01")
}

func (anthropicProviderAdapter) RelayResponse(lh *LLMHandler, w http.ResponseWriter, ctx providerRelayContext) {
	if ctx.Stream {
		lh.relayAnthropicStream(w, ctx.Response, ctx.ModelAlias, ctx.ProviderName, ctx.ProviderModel, ctx.CorrelationID, ctx.EventID, ctx.Start, ctx.StepIndex, ctx.RetryOf, ctx.ProviderToolUses)
		return
	}
	lh.relayAnthropicBuffered(w, ctx.Response, ctx.ModelAlias, ctx.ProviderName, ctx.ProviderModel, ctx.CorrelationID, ctx.EventID, ctx.Start, ctx.StepIndex, ctx.RetryOf, ctx.ProviderToolUses)
}

type geminiProviderAdapter struct{}

func (geminiProviderAdapter) PrepareRequest(ctx providerRequestContext) (providerPreparedRequest, error) {
	targetURL := ctx.TargetURL
	if ctx.Stream {
		targetURL = strings.Replace(targetURL, ":generateContent", ":streamGenerateContent", 1)
		if strings.Contains(targetURL, "?") {
			targetURL += "&alt=sse"
		} else {
			targetURL += "?alt=sse"
		}
	}
	ctx.Body["model"] = ctx.ProviderModel
	body, err := json.Marshal(ctx.Body)
	if err != nil {
		return providerPreparedRequest{}, fmt.Errorf("rewrite request body: %w", err)
	}
	body, err = translateToGemini(body)
	if err != nil {
		return providerPreparedRequest{}, fmt.Errorf("translate request: %w", err)
	}
	return providerPreparedRequest{TargetURL: targetURL, Body: body}, nil
}

func (geminiProviderAdapter) AddHeaders(*http.Request) {}

func (geminiProviderAdapter) RelayResponse(lh *LLMHandler, w http.ResponseWriter, ctx providerRelayContext) {
	if ctx.Stream {
		mode := geminiStreamChat
		if ctx.ResponsesPath {
			mode = geminiStreamResponses
		}
		lh.relayGeminiStream(w, ctx.Response, ctx.ModelAlias, ctx.ProviderName, ctx.ProviderModel, ctx.CorrelationID, ctx.EventID, ctx.Start, ctx.StepIndex, ctx.RetryOf, ctx.ProviderToolUses, mode)
		return
	}
	if ctx.ResponsesPath {
		lh.relayGeminiResponsesBuffered(w, ctx.Response, ctx.ModelAlias, ctx.ProviderName, ctx.ProviderModel, ctx.CorrelationID, ctx.EventID, ctx.Start, ctx.StepIndex, ctx.RetryOf, ctx.ProviderToolUses)
		return
	}
	lh.relayGeminiBuffered(w, ctx.Response, ctx.ModelAlias, ctx.ProviderName, ctx.ProviderModel, ctx.CorrelationID, ctx.EventID, ctx.Start, ctx.StepIndex, ctx.RetryOf, ctx.ProviderToolUses)
}
