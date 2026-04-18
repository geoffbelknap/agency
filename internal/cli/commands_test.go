package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteAgentListTextFormat(t *testing.T) {
	var out bytes.Buffer
	agents := []map[string]interface{}{
		{
			"name":        "alpha",
			"status":      "running",
			"preset":      "default",
			"last_active": "2026-04-10T17:00:00Z",
		},
	}

	if err := writeAgentList(&out, agents, "text"); err != nil {
		t.Fatalf("writeAgentList returned error: %v", err)
	}

	want := "alpha\trunning\tdefault\t2026-04-10T17:00:00Z\n"
	if out.String() != want {
		t.Fatalf("text output = %q, want %q", out.String(), want)
	}
}

func TestWriteAgentListJSONFormat(t *testing.T) {
	var out bytes.Buffer
	agents := []map[string]interface{}{
		{"name": "alpha", "status": "running"},
	}

	if err := writeAgentList(&out, agents, "json"); err != nil {
		t.Fatalf("writeAgentList returned error: %v", err)
	}

	var decoded []map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("json output did not decode: %v", err)
	}
	if got := decoded[0]["name"]; got != "alpha" {
		t.Fatalf("decoded name = %v, want alpha", got)
	}
}

func TestWriteAgentListTableEmpty(t *testing.T) {
	var out bytes.Buffer

	if err := writeAgentList(&out, nil, "table"); err != nil {
		t.Fatalf("writeAgentList returned error: %v", err)
	}
	if strings.TrimSpace(out.String()) != "No agents" {
		t.Fatalf("table empty output = %q, want No agents", out.String())
	}
}

func TestWriteAgentListRejectsUnknownFormat(t *testing.T) {
	var out bytes.Buffer

	err := writeAgentList(&out, nil, "xml")
	if err == nil {
		t.Fatal("writeAgentList returned nil error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("error = %q, want unsupported format", err.Error())
	}
}

func TestBuildCredentialSetBodyUsesPositionalName(t *testing.T) {
	body, err := buildCredentialSetBody(credentialSetInput{
		NameArg:  "GEMINI_API_KEY",
		Value:    "secret",
		Kind:     "provider",
		Scope:    "platform",
		Protocol: "api-key",
	})
	if err != nil {
		t.Fatalf("buildCredentialSetBody returned error: %v", err)
	}

	if got := body["name"]; got != "GEMINI_API_KEY" {
		t.Fatalf("name = %v, want GEMINI_API_KEY", got)
	}
	if got := body["kind"]; got != "provider" {
		t.Fatalf("kind = %v, want provider", got)
	}
	if got := body["scope"]; got != "platform" {
		t.Fatalf("scope = %v, want platform", got)
	}
	if got := body["protocol"]; got != "api-key" {
		t.Fatalf("protocol = %v, want api-key", got)
	}
}

func TestBuildCredentialSetBodyAllowsMatchingNameFlag(t *testing.T) {
	body, err := buildCredentialSetBody(credentialSetInput{
		NameArg:        "github-token",
		NameFlag:       "github-token",
		Value:          "secret",
		Kind:           "service",
		Scope:          "agent:henry",
		Protocol:       "bearer",
		Service:        "github",
		Group:          "github-readonly",
		ExternalScopes: "repo:read, issues:write",
		Requires:       "GITHUB_APP_ID, GITHUB_PRIVATE_KEY",
		ExpiresAt:      "2026-05-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("buildCredentialSetBody returned error: %v", err)
	}

	if got := body["service"]; got != "github" {
		t.Fatalf("service = %v, want github", got)
	}
	if got := body["external_scopes"]; strings.Join(got.([]string), ",") != "repo:read,issues:write" {
		t.Fatalf("external_scopes = %v, want trimmed scope list", got)
	}
	if got := body["requires"]; strings.Join(got.([]string), ",") != "GITHUB_APP_ID,GITHUB_PRIVATE_KEY" {
		t.Fatalf("requires = %v, want trimmed dependency list", got)
	}
}

func TestBuildCredentialSetBodyRejectsConflictingNames(t *testing.T) {
	_, err := buildCredentialSetBody(credentialSetInput{
		NameArg:  "one",
		NameFlag: "two",
		Value:    "secret",
	})
	if err == nil {
		t.Fatal("expected conflicting names to fail")
	}
}

func TestBuildCredentialSetBodyRequiresNameAndValue(t *testing.T) {
	if _, err := buildCredentialSetBody(credentialSetInput{Value: "secret"}); err == nil {
		t.Fatal("expected missing name to fail")
	}
	if _, err := buildCredentialSetBody(credentialSetInput{NameArg: "GEMINI_API_KEY"}); err == nil {
		t.Fatal("expected missing value to fail")
	}
}

func TestFormatBackendStatusLine(t *testing.T) {
	tests := []struct {
		name     string
		backend  string
		mode     string
		endpoint string
		want     string
	}{
		{
			name:    "backend only",
			backend: "docker",
			want:    "docker",
		},
		{
			name:    "backend and mode",
			backend: "containerd",
			mode:    "rootless",
			want:    "containerd (rootless)",
		},
		{
			name:     "backend mode and endpoint",
			backend:  "containerd",
			mode:     "rootful",
			endpoint: "/run/containerd/containerd.sock",
			want:     "containerd (rootful) /run/containerd/containerd.sock",
		},
		{
			name:     "empty backend suppresses line",
			mode:     "rootless",
			endpoint: "/run/user/1000/containerd/containerd.sock",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatBackendStatusLine(tt.backend, tt.mode, tt.endpoint); got != tt.want {
				t.Fatalf("formatBackendStatusLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteInfraBackendLine(t *testing.T) {
	var out bytes.Buffer
	writeInfraBackendLine(&out, "podman", "rootless", "unix:///run/user/1000/podman/podman.sock")
	if got := out.String(); got != "Backend: podman (rootless) unix:///run/user/1000/podman/podman.sock\n" {
		t.Fatalf("writeInfraBackendLine() = %q", got)
	}
}

func TestStatusCmdShowsBackendDetails(t *testing.T) {
	tests := []struct {
		name          string
		backend       string
		backendMode   string
		backendSocket string
		want          string
	}{
		{name: "docker", backend: "docker", backendSocket: "unix:///var/run/docker.sock", want: "Backend: docker unix:///var/run/docker.sock"},
		{name: "podman", backend: "podman", backendMode: "rootless", backendSocket: "unix:///run/user/1000/podman/podman.sock", want: "Backend: podman (rootless) unix:///run/user/1000/podman/podman.sock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newCLIInfraStatusServer(t, map[string]any{
				"version":          "0.2.2",
				"build_id":         "test-build",
				"gateway_url":      "http://127.0.0.1:8200",
				"web_url":          "http://127.0.0.1:8280",
				"docker":           "available",
				"backend":          tt.backend,
				"backend_mode":     tt.backendMode,
				"backend_endpoint": tt.backendSocket,
				"components":       []map[string]any{},
			})
			defer srv.Close()

			t.Setenv("AGENCY_GATEWAY_URL", srv.URL)
			t.Setenv("HOME", t.TempDir())
			var out bytes.Buffer
			cmd := statusCmd()
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("statusCmd.Execute() error = %v", err)
			}
			got := out.String()
			for _, needle := range []string{"Agency v0.2.2 (test-build,", "Infrastructure:", "Agents:", tt.want} {
				if !strings.Contains(got, needle) {
					t.Fatalf("status output = %q, want substring %q", got, needle)
				}
			}
		})
	}
}

func TestInfraStatusCmdShowsBackendDetails(t *testing.T) {
	tests := []struct {
		name          string
		backend       string
		backendMode   string
		backendSocket string
		want          string
	}{
		{name: "docker", backend: "docker", backendSocket: "unix:///var/run/docker.sock", want: "Backend: docker unix:///var/run/docker.sock"},
		{name: "podman", backend: "podman", backendMode: "rootful", backendSocket: "unix:///run/podman/podman.sock", want: "Backend: podman (rootful) unix:///run/podman/podman.sock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newCLIInfraStatusServer(t, map[string]any{
				"version":          "0.2.2",
				"build_id":         "test-build",
				"backend":          tt.backend,
				"backend_mode":     tt.backendMode,
				"backend_endpoint": tt.backendSocket,
				"components":       []map[string]any{},
			})
			defer srv.Close()

			t.Setenv("AGENCY_GATEWAY_URL", srv.URL)
			t.Setenv("HOME", t.TempDir())
			var out bytes.Buffer
			cmd := infraCmd()
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"status"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("infra status Execute() error = %v", err)
			}
			if !strings.Contains(out.String(), tt.want) {
				t.Fatalf("infra status output = %q, want substring %q", out.String(), tt.want)
			}
		})
	}
}

func newCLIInfraStatusServer(t *testing.T, infraStatus map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/infra/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(infraStatus)
	})
	mux.HandleFunc("/api/v1/agents", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})
	mux.HandleFunc("/api/v1/meeseeks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})
	return httptest.NewServer(mux)
}
