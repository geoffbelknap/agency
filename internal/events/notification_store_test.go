package events

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geoffbelknap/agency/internal/config"
)

func TestNotificationStore_AddAndList(t *testing.T) {
	home := t.TempDir()
	store := NewNotificationStore(home)

	nc := config.NotificationConfig{
		Name:   "test-alerts",
		Type:   "ntfy",
		URL:    "https://ntfy.sh/test-topic",
		Events: []string{"operator_alert"},
	}

	if err := store.Add(nc); err != nil {
		t.Fatalf("Add: %v", err)
	}

	list := store.List()
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if list[0].Name != "test-alerts" {
		t.Fatalf("expected test-alerts, got %s", list[0].Name)
	}

	// Verify file was written
	data, err := os.ReadFile(filepath.Join(home, "notifications.yaml"))
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("file is empty")
	}
}

func TestNotificationStore_DuplicateName(t *testing.T) {
	home := t.TempDir()
	store := NewNotificationStore(home)

	nc := config.NotificationConfig{
		Name: "dup",
		Type: "ntfy",
		URL:  "https://ntfy.sh/dup",
	}
	if err := store.Add(nc); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if err := store.Add(nc); err == nil {
		t.Fatal("expected error on duplicate name")
	}
}

func TestNotificationStore_Get(t *testing.T) {
	home := t.TempDir()
	store := NewNotificationStore(home)

	nc := config.NotificationConfig{
		Name: "ops",
		Type: "webhook",
		URL:  "https://hooks.example.com/ops",
	}
	store.Add(nc)

	got, err := store.Get("ops")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.URL != nc.URL {
		t.Fatalf("expected %s, got %s", nc.URL, got.URL)
	}

	_, err = store.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent")
	}
}

func TestNotificationStore_Remove(t *testing.T) {
	home := t.TempDir()
	store := NewNotificationStore(home)

	store.Add(config.NotificationConfig{Name: "rm-me", Type: "ntfy", URL: "https://ntfy.sh/rm"})

	if err := store.Remove("rm-me"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(store.List()) != 0 {
		t.Fatal("expected empty list after remove")
	}

	if err := store.Remove("nonexistent"); err == nil {
		t.Fatal("expected error removing nonexistent")
	}
}

func TestNotificationStore_LoadFromDisk(t *testing.T) {
	home := t.TempDir()
	store := NewNotificationStore(home)
	store.Add(config.NotificationConfig{Name: "persist", Type: "ntfy", URL: "https://ntfy.sh/persist", Events: []string{"operator_alert"}})

	// Create a new store instance — should load from disk
	store2 := NewNotificationStore(home)
	configs, err := store2.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(configs) != 1 || configs[0].Name != "persist" {
		t.Fatalf("expected [persist], got %v", configs)
	}
}

func TestNotificationStore_EmptyFile(t *testing.T) {
	home := t.TempDir()
	store := NewNotificationStore(home)

	// No file exists yet
	configs, err := store.Load()
	if err != nil {
		t.Fatalf("Load on empty: %v", err)
	}
	if len(configs) != 0 {
		t.Fatalf("expected empty, got %d", len(configs))
	}
}
