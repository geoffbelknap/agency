package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceLookupRegistered(t *testing.T) {
	sr := NewServiceRegistry()
	sr.Register("github", &ServiceCredential{
		Header:  "Authorization",
		Value:   "test-value",
		APIBase: "api.github.com",
	})

	cred := sr.Lookup("github")
	if cred == nil {
		t.Fatal("expected credential for github")
	}
	if cred.Header != "Authorization" {
		t.Errorf("wrong header: %s", cred.Header)
	}
	if cred.APIBase != "api.github.com" {
		t.Errorf("wrong api_base: %s", cred.APIBase)
	}
}

func TestServiceLookupUnknown(t *testing.T) {
	sr := NewServiceRegistry()
	cred := sr.Lookup("nonexistent")
	if cred != nil {
		t.Error("expected nil for unknown service")
	}
}

func TestServiceLoadFromFiles(t *testing.T) {
	dir := t.TempDir()

	servicesDir := filepath.Join(dir, "services")
	os.MkdirAll(servicesDir, 0755)
	os.WriteFile(filepath.Join(servicesDir, "github.yaml"), []byte(`
service: github
api_base: https://api.github.com
credential:
  header: Authorization
  format: "Bearer {key}"
  env_var: GITHUB_TOKEN
  scoped_prefix: agency-scoped-github
`), 0644)

	agentDir := filepath.Join(dir, "agent")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "services.yaml"), []byte(`
agent: test-agent
grants:
  - service: github
    granted_at: "2026-01-01T00:00:00Z"
    granted_by: operator
`), 0644)

	sr := NewServiceRegistry()
	err := sr.LoadFromFiles(servicesDir, agentDir)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	cred := sr.Lookup("github")
	if cred == nil {
		t.Fatal("expected credential for github")
	}
	if cred.Header != "Authorization" {
		t.Errorf("wrong header: %s", cred.Header)
	}
	if cred.APIBase != "https://api.github.com" {
		t.Errorf("wrong api_base: %s", cred.APIBase)
	}
}

func TestServiceBlockedHeaders(t *testing.T) {
	dir := t.TempDir()

	servicesDir := filepath.Join(dir, "services")
	os.MkdirAll(servicesDir, 0755)
	os.WriteFile(filepath.Join(servicesDir, "bad.yaml"), []byte(`
service: bad-service
api_base: https://example.com
credential:
  header: Host
  env_var: BAD_KEY
  scoped_prefix: agency-scoped-bad
`), 0644)

	agentDir := filepath.Join(dir, "agent")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "services.yaml"), []byte(`
agent: test-agent
grants:
  - service: bad-service
    granted_at: "2026-01-01T00:00:00Z"
    granted_by: operator
`), 0644)

	sr := NewServiceRegistry()
	sr.LoadFromFiles(servicesDir, agentDir)

	cred := sr.Lookup("bad-service")
	if cred != nil {
		t.Error("service with blocked header should not be registered")
	}
}

func TestServiceReloadViaRegister(t *testing.T) {
	sr := NewServiceRegistry()
	sr.Register("svc1", &ServiceCredential{
		Header: "X-Api-Key",
		Value:  "old-key",
	})

	sr.Register("svc1", &ServiceCredential{
		Header: "X-Api-Key",
		Value:  "new-key",
	})

	cred := sr.Lookup("svc1")
	if cred.Value != "new-key" {
		t.Errorf("expected new-key after reload, got: %s", cred.Value)
	}
}

func TestServiceMultipleGrants(t *testing.T) {
	dir := t.TempDir()

	servicesDir := filepath.Join(dir, "services")
	os.MkdirAll(servicesDir, 0755)
	os.WriteFile(filepath.Join(servicesDir, "svc1.yaml"), []byte(`
service: svc1
api_base: https://svc1.example.com
credential:
  header: X-Api-Key
  env_var: SVC1_KEY
  scoped_prefix: agency-scoped-svc1
`), 0644)
	os.WriteFile(filepath.Join(servicesDir, "svc2.yaml"), []byte(`
service: svc2
api_base: https://svc2.example.com
credential:
  header: Authorization
  format: "Token {key}"
  env_var: SVC2_KEY
  scoped_prefix: agency-scoped-svc2
`), 0644)

	agentDir := filepath.Join(dir, "agent")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "services.yaml"), []byte(`
agent: test-agent
grants:
  - service: svc1
    granted_at: "2026-01-01T00:00:00Z"
    granted_by: operator
  - service: svc2
    granted_at: "2026-01-01T00:00:00Z"
    granted_by: operator
`), 0644)

	sr := NewServiceRegistry()
	sr.LoadFromFiles(servicesDir, agentDir)

	cred1 := sr.Lookup("svc1")
	if cred1 == nil {
		t.Fatal("expected credential for svc1")
	}
	if cred1.Header != "X-Api-Key" {
		t.Errorf("wrong svc1 header: %s", cred1.Header)
	}

	cred2 := sr.Lookup("svc2")
	if cred2 == nil {
		t.Fatal("expected credential for svc2")
	}
	if cred2.Header != "Authorization" {
		t.Errorf("wrong svc2 header: %s", cred2.Header)
	}
}

func TestServiceScopeCheck(t *testing.T) {
	dir := t.TempDir()

	servicesDir := filepath.Join(dir, "services")
	os.MkdirAll(servicesDir, 0755)
	os.WriteFile(filepath.Join(servicesDir, "lc.yaml"), []byte(`
service: limacharlie
api_base: https://api.limacharlie.io
credential:
  header: Authorization
  env_var: LC_KEY
  scoped_prefix: agency-scoped-lc
tools:
  - name: list_detections
    scope: insight.det.get
  - name: create_fp_rule
    scope: fp.ctrl
`), 0644)

	agentDir := filepath.Join(dir, "agent")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "services.yaml"), []byte(`
agent: test-agent
grants:
  - service: limacharlie
    granted_at: "2026-01-01T00:00:00Z"
    granted_by: operator
    allowed_scopes:
      - insight.det.get
`), 0644)

	sr := NewServiceRegistry()
	sr.LoadFromFiles(servicesDir, agentDir)

	// list_detections requires insight.det.get — agent has it
	_, ok := sr.CheckScope("limacharlie", "list_detections")
	if !ok {
		t.Error("expected list_detections to be allowed")
	}

	// create_fp_rule requires fp.ctrl — agent doesn't have it
	scope, ok := sr.CheckScope("limacharlie", "create_fp_rule")
	if ok {
		t.Error("expected create_fp_rule to be denied")
	}
	if scope != "fp.ctrl" {
		t.Errorf("expected required scope fp.ctrl, got: %s", scope)
	}
}

func TestServiceLoadConsentFromDeploymentDir(t *testing.T) {
	dir := t.TempDir()

	servicesDir := filepath.Join(dir, "services")
	agentDir := filepath.Join(dir, "agent")
	deploymentsDir := filepath.Join(dir, "deployments")
	os.MkdirAll(servicesDir, 0o755)
	os.MkdirAll(agentDir, 0o755)
	os.MkdirAll(filepath.Join(deploymentsDir, "dep-123"), 0o755)

	os.WriteFile(filepath.Join(servicesDir, "drive.yaml"), []byte(`
service: drive
api_base: https://example.com
credential:
  header: Authorization
  env_var: DRIVE_TOKEN
  scoped_prefix: agency-scoped-drive
tools:
  - name: drive_add_whitelist_entry
    requires_consent_token:
      operation_kind: add_managed_doc
      token_input_field: consent_token
      target_input_field: drive_id
`), 0o644)

	os.WriteFile(filepath.Join(agentDir, "services.yaml"), []byte(`
agent: test-agent
grants:
  - service: drive
    granted_at: "2026-01-01T00:00:00Z"
    granted_by: operator
`), 0o644)

	refData, _ := json.Marshal(map[string]string{"deployment_id": "dep-123"})
	os.WriteFile(filepath.Join(agentDir, "consent-deployment.json"), refData, 0o644)

	cfgData, _ := json.Marshal(map[string]interface{}{
		"deployment_id":     "dep-123",
		"max_ttl_seconds":   900,
		"clock_skew_millis": 30000,
		"verification_keys": map[string]string{},
	})
	os.WriteFile(filepath.Join(deploymentsDir, "dep-123", "consent-verification-keys.json"), cfgData, 0o644)
	t.Setenv("CONSENT_DEPLOYMENTS_DIR", deploymentsDir)

	sr := NewServiceRegistry()
	if err := sr.LoadFromFiles(servicesDir, agentDir); err != nil {
		t.Fatalf("load error: %v", err)
	}

	cred := sr.Lookup("drive")
	if cred == nil {
		t.Fatal("expected credential for drive")
	}
	if _, ok := cred.ToolConsent["drive_add_whitelist_entry"]; !ok {
		t.Fatal("expected consent requirement to load from service definition")
	}
}
