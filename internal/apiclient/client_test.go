package apiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentRuntimeClientEndpoints(t *testing.T) {
	t.Run("manifest", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s, want GET", r.Method)
			}
			if r.URL.Path != "/api/v1/agents/runtime-agent/runtime/manifest" {
				t.Fatalf("path = %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"spec": map[string]any{"runtimeId": "runtime-agent"}})
		}))
		defer srv.Close()

		c := NewClient(srv.URL)
		result, err := c.ShowAgentRuntimeManifest(context.Background(), "runtime-agent")
		if err != nil {
			t.Fatalf("ShowAgentRuntimeManifest(): %v", err)
		}
		spec, ok := result["spec"].(map[string]any)
		if !ok || spec["runtimeId"] != "runtime-agent" {
			t.Fatalf("unexpected result %#v", result)
		}
	})

	t.Run("status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s, want GET", r.Method)
			}
			if r.URL.Path != "/api/v1/agents/runtime-agent/runtime/status" {
				t.Fatalf("path = %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"phase": "reconciled"})
		}))
		defer srv.Close()

		c := NewClient(srv.URL)
		result, err := c.ShowAgentRuntimeStatus(context.Background(), "runtime-agent")
		if err != nil {
			t.Fatalf("ShowAgentRuntimeStatus(): %v", err)
		}
		if result["phase"] != "reconciled" {
			t.Fatalf("phase = %v, want reconciled", result["phase"])
		}
	})

	t.Run("validate", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			if r.URL.Path != "/api/v1/agents/runtime-agent/runtime/validate" {
				t.Fatalf("path = %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "valid"})
		}))
		defer srv.Close()

		c := NewClient(srv.URL)
		result, err := c.ValidateAgentRuntime(context.Background(), "runtime-agent")
		if err != nil {
			t.Fatalf("ValidateAgentRuntime(): %v", err)
		}
		if result["status"] != "valid" {
			t.Fatalf("status = %v, want valid", result["status"])
		}
	})
}
