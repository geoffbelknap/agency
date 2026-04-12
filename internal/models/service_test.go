// agency-gateway/internal/models/service_test.go
package models

import (
	"testing"
)

func TestServiceCredentialConfig_Validate_ValidPrefix(t *testing.T) {
	c := ServiceCredentialConfig{
		EnvVar:       "BRAVE_API_KEY",
		Header:       "X-Subscription-Token",
		ScopedPrefix: "agency-scoped-brave",
	}
	if err := c.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

func TestServiceCredentialConfig_Validate_InvalidPrefix(t *testing.T) {
	c := ServiceCredentialConfig{
		EnvVar:       "BRAVE_API_KEY",
		Header:       "X-Subscription-Token",
		ScopedPrefix: "bad-prefix",
	}
	err := c.Validate()
	if err == nil {
		t.Error("Validate() expected error for invalid scoped_prefix, got nil")
	}
}

func TestServiceCredentialConfig_Validate_EmptyPrefix(t *testing.T) {
	c := ServiceCredentialConfig{
		EnvVar:       "SOME_KEY",
		Header:       "Authorization",
		ScopedPrefix: "",
	}
	err := c.Validate()
	if err == nil {
		t.Error("Validate() expected error for empty scoped_prefix, got nil")
	}
}

func TestServiceDefinition_Validate_ValidName(t *testing.T) {
	s := ServiceDefinition{
		Service:     "brave-search",
		DisplayName: "Brave Search",
		APIBase:     "https://api.search.brave.com",
		Credential: ServiceCredentialConfig{
			EnvVar:       "BRAVE_API_KEY",
			Header:       "X-Subscription-Token",
			ScopedPrefix: "agency-scoped-brave",
		},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

func TestServiceDefinition_Validate_ValidNameWithUnderscore(t *testing.T) {
	s := ServiceDefinition{
		Service:     "my_service_1",
		DisplayName: "My Service",
		APIBase:     "https://api.example.com",
		Credential: ServiceCredentialConfig{
			EnvVar:       "MY_SERVICE_KEY",
			Header:       "Authorization",
			ScopedPrefix: "agency-scoped-my-service",
		},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

func TestServiceDefinition_Validate_NameWithSpecialChars(t *testing.T) {
	s := ServiceDefinition{
		Service:     "bad name!",
		DisplayName: "Bad Service",
		APIBase:     "https://api.example.com",
		Credential: ServiceCredentialConfig{
			EnvVar:       "KEY",
			Header:       "Authorization",
			ScopedPrefix: "agency-scoped-bad",
		},
	}
	err := s.Validate()
	if err == nil {
		t.Error("Validate() expected error for name with special chars, got nil")
	}
}

func TestServiceDefinition_Validate_PropagatesCredentialError(t *testing.T) {
	s := ServiceDefinition{
		Service:     "valid-service",
		DisplayName: "Valid Service",
		APIBase:     "https://api.example.com",
		Credential: ServiceCredentialConfig{
			EnvVar:       "KEY",
			Header:       "Authorization",
			ScopedPrefix: "not-scoped-correctly",
		},
	}
	err := s.Validate()
	if err == nil {
		t.Error("Validate() expected error for invalid credential prefix, got nil")
	}
}

func TestServiceGrant_Validate_Valid(t *testing.T) {
	g := ServiceGrant{
		Service:   "brave-search",
		GrantedAt: "2026-03-20T10:00:00Z",
		GrantedBy: "operator",
	}
	if err := g.Validate(); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

func TestServiceGrant_Validate_MissingGrantedAt(t *testing.T) {
	g := ServiceGrant{
		Service:   "brave-search",
		GrantedAt: "",
		GrantedBy: "operator",
	}
	err := g.Validate()
	if err == nil {
		t.Error("Validate() expected error for missing granted_at, got nil")
	}
}

func TestAgentServiceGrants_Struct(t *testing.T) {
	format := "Bearer {token}"
	grants := AgentServiceGrants{
		Agent: "researcher",
		Grants: []ServiceGrant{
			{
				Service:   "brave-search",
				GrantedAt: "2026-03-20T10:00:00Z",
				GrantedBy: "operator",
			},
		},
	}
	if grants.Agent != "researcher" {
		t.Errorf("Agent = %q, want %q", grants.Agent, "researcher")
	}
	if len(grants.Grants) != 1 {
		t.Errorf("len(Grants) = %d, want 1", len(grants.Grants))
	}
	if grants.Grants[0].Service != "brave-search" {
		t.Errorf("Grants[0].Service = %q, want %q", grants.Grants[0].Service, "brave-search")
	}

	// Verify ServiceCredentialConfig optional Format field works
	cred := ServiceCredentialConfig{
		EnvVar:       "KEY",
		Header:       "Authorization",
		Format:       &format,
		ScopedPrefix: "agency-scoped-test",
	}
	if cred.Format == nil || *cred.Format != format {
		t.Errorf("Format = %v, want %q", cred.Format, format)
	}
}

func TestAgentServiceGrants_EmptyGrants(t *testing.T) {
	grants := AgentServiceGrants{
		Agent:  "analyst",
		Grants: []ServiceGrant{},
	}
	if len(grants.Grants) != 0 {
		t.Errorf("len(Grants) = %d, want 0", len(grants.Grants))
	}
}

func TestServiceDefinition_Validate_ConsentRequirementValid(t *testing.T) {
	s := ServiceDefinition{
		Service:     "drive-admin",
		DisplayName: "Drive Admin",
		APIBase:     "https://api.example.com",
		Credential: ServiceCredentialConfig{
			EnvVar:       "KEY",
			Header:       "Authorization",
			ScopedPrefix: "agency-scoped-drive-admin",
		},
		Tools: []ServiceTool{
			{
				Name:        "drive_add_whitelist_entry",
				Description: "Add whitelist entry",
				Path:        "/drive",
				Parameters: []ServiceToolParameter{
					{Name: "drive_id", Description: "Drive ID"},
					{Name: "consent_token", Description: "Consent token"},
				},
				RequiresConsentToken: &ConsentRequirement{
					OperationKind:    "add_managed_doc",
					TokenInputField:  "consent_token",
					TargetInputField: "drive_id",
					MinWitnesses:     2,
				},
			},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate() returned unexpected error: %v", err)
	}
}

func TestServiceDefinition_Validate_ConsentRequirementRejectsUnknownField(t *testing.T) {
	s := ServiceDefinition{
		Service:     "drive-admin",
		DisplayName: "Drive Admin",
		APIBase:     "https://api.example.com",
		Credential: ServiceCredentialConfig{
			EnvVar:       "KEY",
			Header:       "Authorization",
			ScopedPrefix: "agency-scoped-drive-admin",
		},
		Tools: []ServiceTool{
			{
				Name:        "drive_add_whitelist_entry",
				Description: "Add whitelist entry",
				Path:        "/drive",
				Parameters: []ServiceToolParameter{
					{Name: "drive_id", Description: "Drive ID"},
				},
				RequiresConsentToken: &ConsentRequirement{
					OperationKind:    "add_managed_doc",
					TokenInputField:  "consent_token",
					TargetInputField: "drive_id",
				},
			},
		},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("Validate() expected error for unknown consent token field, got nil")
	}
}
