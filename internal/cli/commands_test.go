package cli

import (
	"bytes"
	"encoding/json"
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
