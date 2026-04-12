package deployments

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

func testSchema() *Schema {
	return &Schema{
		SchemaVersion: 1,
		Deployment:    SchemaDeployment{Name: "community-admin"},
		Config: map[string]ConfigField{
			"community_display_name": {Type: "string", Required: true},
			"vote_threshold":         {Type: "int", Default: 2},
		},
		Credentials: map[string]CredentialField{
			"slack_bot_token": {CredstoreScope: "slack"},
		},
		Instances: SchemaInstances{
			Pack:       SchemaInstance{Component: "community-admin", Required: true},
			Connectors: []SchemaInstance{{Component: "slack-interactivity", Required: true}},
		},
	}
}

func testDeployment() *Deployment {
	return &Deployment{
		Name:          "community-admin-prod",
		Pack:          PackRef{Name: "community-admin", Version: "1.0.0", HubSource: "official"},
		SchemaVersion: 1,
		Config: map[string]interface{}{
			"community_display_name": "CSO Council",
			"vote_threshold":         2,
		},
		CredRefs: map[string]CredRef{
			"slack_bot_token": {Key: "slack_bot_token", CredstoreID: "cred-slack-bot", ExportPolicy: "ref_only"},
		},
		Owner: OwnerRef{
			AgencyID:   "agency-a",
			AgencyName: "agency-a",
			ClaimedAt:  time.Now().UTC(),
			Heartbeat:  time.Now().UTC(),
		},
	}
}

func TestFilesystemStoreExportImportRoundTrip(t *testing.T) {
	sourceStore := NewFilesystemStore(t.TempDir())
	dep := testDeployment()
	schema := testSchema()

	if err := sourceStore.Create(context.Background(), dep, schema); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := sourceStore.AppendAudit(context.Background(), dep.ID, AuditEntry{
		Action:       "create",
		DeploymentID: dep.ID,
		Result:       "ok",
	}); err != nil {
		t.Fatalf("append audit: %v", err)
	}

	rc, err := sourceStore.Export(context.Background(), dep.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}

	targetStore := NewFilesystemStore(t.TempDir())
	imported, importedSchema, err := targetStore.Import(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if imported.ID == dep.ID {
		t.Fatalf("import preserved id %q; want a new deployment id", imported.ID)
	}
	if imported.Name != dep.Name {
		t.Fatalf("imported name = %q, want %q", imported.Name, dep.Name)
	}
	if imported.Owner.AgencyID != "" {
		t.Fatalf("imported owner = %+v, want empty owner", imported.Owner)
	}
	if len(imported.Instances) != 0 {
		t.Fatalf("imported bindings = %+v, want regenerated instance bindings", imported.Instances)
	}
	if importedSchema.Deployment.Name != schema.Deployment.Name {
		t.Fatalf("imported schema deployment name = %q, want %q", importedSchema.Deployment.Name, schema.Deployment.Name)
	}
}

func TestFilesystemStoreClaimHonorsFreshOwner(t *testing.T) {
	store := NewFilesystemStore(t.TempDir())
	dep := testDeployment()
	schema := testSchema()
	if err := store.Create(context.Background(), dep, schema); err != nil {
		t.Fatalf("create: %v", err)
	}

	err := store.Claim(context.Background(), dep.ID, OwnerRef{AgencyID: "agency-b", AgencyName: "agency-b"}, false)
	if err == nil {
		t.Fatal("claim without force should fail when current owner heartbeat is fresh")
	}
	if err := store.Claim(context.Background(), dep.ID, OwnerRef{AgencyID: "agency-b", AgencyName: "agency-b"}, true); err != nil {
		t.Fatalf("force claim: %v", err)
	}
	claimed, _, err := store.Get(context.Background(), dep.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if claimed.Owner.AgencyID != "agency-b" {
		t.Fatalf("owner = %q, want agency-b", claimed.Owner.AgencyID)
	}
}
