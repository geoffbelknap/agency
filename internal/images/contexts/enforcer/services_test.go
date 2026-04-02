package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mockResolver(keys map[string]string) KeyResolver {
	return func(name string) string {
		return keys[name]
	}
}

func TestBlockedHeaders(t *testing.T) {
	// All hop-by-hop and dangerous headers must be blocked.
	expected := []string{
		"host",
		"transfer-encoding",
		"proxy-authorization",
		"proxy-authenticate",
		"proxy-connection",
		"connection",
		"content-length",
		"te",
		"upgrade",
	}

	for _, h := range expected {
		if !blockedHeaders[h] {
			t.Errorf("header %q should be blocked but is not", h)
		}
	}
}

func TestBlockedHeadersCaseInsensitive(t *testing.T) {
	// Service credential loading lowercases headers before checking.
	// Verify the map keys are all lowercase.
	for h := range blockedHeaders {
		if h != strings.ToLower(h) {
			t.Errorf("blockedHeaders key %q is not lowercase", h)
		}
	}
}

func TestServiceRegistryRejectsBlockedHeader(t *testing.T) {
	// Set up temp files to simulate service loading with a blocked header.
	dir := t.TempDir()
	servicesDir := filepath.Join(dir, "services")
	agentDir := filepath.Join(dir, "agent")
	os.MkdirAll(servicesDir, 0o755)
	os.MkdirAll(agentDir, 0o755)

	blockedTestHeaders := []string{
		"Host", "Transfer-Encoding", "Proxy-Authorization",
		"Connection", "Content-Length", "Te", "Upgrade",
	}

	for _, header := range blockedTestHeaders {
		safeName := strings.ReplaceAll(strings.ToLower(header), "-", "")

		// Write service definition with the blocked header
		svcYAML := "service: blocked-" + safeName + "\napi_base: https://example.com\ncredential:\n  header: " + header + "\n  env_var: KEY_" + strings.ToUpper(safeName) + "\n"
		os.WriteFile(filepath.Join(servicesDir, "svc-"+safeName+".yaml"), []byte(svcYAML), 0o644)

		// Write grants
		grantsYAML := "agent: test\ngrants:\n  - service: blocked-" + safeName + "\n    granted_at: \"2026-01-01\"\n    granted_by: test\n"
		os.WriteFile(filepath.Join(agentDir, "services.yaml"), []byte(grantsYAML), 0o644)

		// Mock key resolver
		envVar := "KEY_" + strings.ToUpper(safeName)
		resolver := mockResolver(map[string]string{envVar: "secret-value"})

		sr := NewServiceRegistry()
		err := sr.LoadFromFiles(servicesDir, agentDir, resolver)
		if err != nil {
			t.Fatalf("LoadFromFiles failed for header %q: %v", header, err)
		}

		cred := sr.Lookup("blocked-" + safeName)
		if cred != nil {
			t.Errorf("header %q should be blocked, but service was registered with credential", header)
		}

		// Clean up for next iteration
		os.Remove(filepath.Join(servicesDir, "svc-"+safeName+".yaml"))
	}
}

func TestServiceRegistryAllowsNonBlockedHeader(t *testing.T) {
	dir := t.TempDir()
	servicesDir := filepath.Join(dir, "services")
	agentDir := filepath.Join(dir, "agent")
	os.MkdirAll(servicesDir, 0o755)
	os.MkdirAll(agentDir, 0o755)

	// Authorization is a normal header that should be allowed.
	svcYAML := "service: allowed-svc\napi_base: https://example.com\ncredential:\n  header: Authorization\n  format: \"Bearer {key}\"\n  env_var: API_KEY\n"
	os.WriteFile(filepath.Join(servicesDir, "allowed.yaml"), []byte(svcYAML), 0o644)

	grantsYAML := "agent: test\ngrants:\n  - service: allowed-svc\n    granted_at: \"2026-01-01\"\n    granted_by: test\n"
	os.WriteFile(filepath.Join(agentDir, "services.yaml"), []byte(grantsYAML), 0o644)

	resolver := mockResolver(map[string]string{"API_KEY": "my-secret-key"})

	sr := NewServiceRegistry()
	err := sr.LoadFromFiles(servicesDir, agentDir, resolver)
	if err != nil {
		t.Fatalf("LoadFromFiles failed: %v", err)
	}

	cred := sr.Lookup("allowed-svc")
	if cred == nil {
		t.Fatal("expected allowed-svc to be registered")
	}
	if cred.Header != "Authorization" {
		t.Errorf("expected header Authorization, got %s", cred.Header)
	}
	if cred.Value != "Bearer my-secret-key" {
		t.Errorf("expected formatted value, got %s", cred.Value)
	}
}
