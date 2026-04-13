package relayhooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	instancepkg "github.com/geoffbelknap/agency/internal/instances"
	runpkg "github.com/geoffbelknap/agency/internal/runtime"
)

func TestBuildIncludesWebhookIngressRoutes(t *testing.T) {
	home := t.TempDir()
	store := instancepkg.NewStore(filepath.Join(home, "instances"))
	inst := &instancepkg.Instance{
		ID:   "inst_123",
		Name: "slack-alpha",
		Source: instancepkg.InstanceSource{
			Package: instancepkg.PackageRef{Kind: "connector", Name: "slack-interactivity", Version: "1.1.0"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.Create(context.Background(), inst); err != nil {
		t.Fatalf("create instance: %v", err)
	}
	instanceDir, _ := store.InstanceDir(inst.ID)
	rtStore := runpkg.NewStore(instanceDir)
	manifest := &runpkg.Manifest{
		Metadata: runpkg.ManifestMeta{
			InstanceID:   inst.ID,
			InstanceName: inst.Name,
		},
		Runtime: runpkg.RuntimeSpec{
			Nodes: []runpkg.RuntimeNode{
				{
					NodeID: "slack_ingress",
					Kind:   "connector.ingress",
					Package: runpkg.RuntimePackageRef{
						Kind: "connector", Name: "slack-interactivity", Version: "1.1.0",
					},
					Ingress: &runpkg.RuntimeIngressSpec{
						PublishedName: "slack-alpha",
						ConnectorYAML: "name: slack-alpha\nsource:\n  type: webhook\n  path: /webhooks/slack-alpha\n",
					},
				},
				{
					NodeID: "drive_authority",
					Kind:   "connector.authority",
					Package: runpkg.RuntimePackageRef{
						Kind: "connector", Name: "google-drive-admin", Version: "1.1.0",
					},
				},
			},
		},
	}
	if err := rtStore.SaveManifest(manifest); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	got, err := Build(context.Background(), store, []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	if len(got.Routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(got.Routes))
	}
	route := got.Routes[0]
	if route.Provider != "slack" {
		t.Fatalf("provider = %q", route.Provider)
	}
	if route.LocalPath != "/webhooks/slack-alpha" {
		t.Fatalf("local_path = %q", route.LocalPath)
	}
	if route.PublicPath == "" || route.RouteID == "" {
		t.Fatalf("missing public route identifiers: %+v", route)
	}
}

func TestStoreSaveWritesRelayWebhookManifest(t *testing.T) {
	home := t.TempDir()
	store := Store{Home: home}
	err := store.Save(&Manifest{
		Version: manifestVersion,
		Routes:  []Route{{RouteID: "wh_123", PublicPath: "/webhooks/wh_123"}},
	})
	if err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "relay-webhooks.json")); err != nil {
		t.Fatalf("stat relay-webhooks.json: %v", err)
	}
}
