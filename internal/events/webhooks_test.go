package events

import (
	"os"
	"testing"
)

func TestWebhookManagerCRUD(t *testing.T) {
	dir := t.TempDir()
	mgr := NewWebhookManager(dir)

	// Create
	wh, err := mgr.Create("test-hook", "issue_created")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if wh.Name != "test-hook" {
		t.Errorf("expected name test-hook, got %s", wh.Name)
	}
	if wh.Secret == "" || len(wh.Secret) != 64 {
		t.Errorf("expected 64 hex char secret, got %d chars", len(wh.Secret))
	}
	if wh.URL != "/api/v1/events/webhook/test-hook" {
		t.Errorf("unexpected URL: %s", wh.URL)
	}

	// Get
	got, err := mgr.Get("test-hook")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Secret != wh.Secret {
		t.Error("Get returned different secret")
	}

	// List
	list, err := mgr.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 webhook, got %d", len(list))
	}

	// Duplicate create
	_, err = mgr.Create("test-hook", "issue_created")
	if err == nil {
		t.Error("expected error on duplicate create")
	}

	// Delete
	if err := mgr.Delete("test-hook"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err = mgr.Get("test-hook")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestWebhookManagerRotateSecret(t *testing.T) {
	dir := t.TempDir()
	mgr := NewWebhookManager(dir)

	wh, _ := mgr.Create("rotate-test", "deploy")
	oldSecret := wh.Secret

	rotated, err := mgr.RotateSecret("rotate-test")
	if err != nil {
		t.Fatalf("RotateSecret failed: %v", err)
	}
	if rotated.Secret == oldSecret {
		t.Error("expected different secret after rotation")
	}
	if len(rotated.Secret) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(rotated.Secret))
	}
}

func TestWebhookManagerInvalidName(t *testing.T) {
	dir := t.TempDir()
	mgr := NewWebhookManager(dir)

	_, err := mgr.Create("Invalid-Name", "x")
	if err == nil {
		t.Error("expected error for invalid webhook name")
	}
}

func TestWebhookManagerEmptyDir(t *testing.T) {
	dir := t.TempDir()
	// Don't create webhooks dir
	os.RemoveAll(dir)
	mgr := NewWebhookManager(dir)

	list, err := mgr.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 webhooks, got %d", len(list))
	}
}
