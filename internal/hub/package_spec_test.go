package hub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeriveConnectorPackageSpecPreservesWhitelistAndConsentMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connector.yaml")
	data := []byte(`kind: connector
name: google-drive-admin
version: "1.1.0"
source:
  type: none
requires:
  auth:
    type: bearer
mcp:
  name: google-drive-admin
  credential: google-drive-admin
  api_base: https://www.googleapis.com
  tools:
    - name: drive_share_file
      method: POST
      path: /drive/v3/files/{file_id}/permissions
      parameters:
        file_id:
          type: string
        role:
          type: string
        type:
          type: string
        emailAddress:
          type: string
        sendNotificationEmail:
          type: boolean
        consent_token:
          type: string
      query_params: [sendNotificationEmail]
      whitelist_check: file_id
      requires_consent_token:
        operation_kind: grant_drive_file_permission
        token_input_field: consent_token
        target_input_field: file_id
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	spec, err := deriveConnectorPackageSpec(path)
	if err != nil {
		t.Fatalf("deriveConnectorPackageSpec(): %v", err)
	}
	runtimeSpec, ok := spec["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("runtime spec missing: %#v", spec)
	}
	executor, ok := runtimeSpec["executor"].(map[string]any)
	if !ok {
		t.Fatalf("executor missing: %#v", runtimeSpec)
	}
	actions, ok := executor["actions"].(map[string]any)
	if !ok {
		t.Fatalf("actions missing: %#v", executor)
	}
	action, ok := actions["drive_share_file"].(map[string]any)
	if !ok {
		t.Fatalf("drive_share_file missing: %#v", actions)
	}
	if got := action["whitelist_field"]; got != "file_id" {
		t.Fatalf("whitelist_field = %#v, want file_id", got)
	}
	query, ok := action["query"].(map[string]any)
	if !ok || query["sendNotificationEmail"] != "sendNotificationEmail" {
		t.Fatalf("query mapping = %#v", action["query"])
	}
	body, ok := action["body"].(map[string]any)
	if !ok {
		t.Fatalf("body missing: %#v", action)
	}
	if _, exists := body["consent_token"]; exists {
		t.Fatalf("consent token should not be forwarded in request body: %#v", body)
	}
	if body["role"] != "role" || body["type"] != "type" || body["emailAddress"] != "emailAddress" {
		t.Fatalf("unexpected body mapping: %#v", body)
	}
}
