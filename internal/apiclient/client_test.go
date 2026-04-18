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

func TestInfraStatusDecodesOperatorFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/v1/infra/status" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version":                 "0.2.2",
			"build_id":                "build-123",
			"gateway_url":             "http://127.0.0.1:8200",
			"web_url":                 "http://127.0.0.1:8280",
			"docker":                  "available",
			"backend":                 "podman",
			"backend_endpoint":        "unix:///run/user/1000/podman/podman.sock",
			"backend_mode":            "rootless",
			"infra_control_available": true,
			"host_runtime":            "available",
			"components":              []map[string]any{{"name": "gateway-proxy", "state": "running", "health": "healthy"}},
			"infra_llm_daily_used":    1.25,
			"infra_llm_daily_limit":   10.0,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.InfraStatus()
	if err != nil {
		t.Fatalf("InfraStatus(): %v", err)
	}
	if resp.Backend != "podman" || resp.BackendMode != "rootless" {
		t.Fatalf("backend visibility = %#v", resp)
	}
	if !resp.InfraControlAvailable {
		t.Fatal("InfraControlAvailable = false, want true")
	}
	if resp.HostRuntime != "available" {
		t.Fatalf("HostRuntime = %q, want available", resp.HostRuntime)
	}
	if len(resp.Components) != 1 || resp.Components[0]["name"] != "gateway-proxy" {
		t.Fatalf("components = %#v", resp.Components)
	}
}
