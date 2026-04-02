package logs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriter_InjectsLifecycleID(t *testing.T) {
	home := t.TempDir()
	w := NewWriter(home)
	w.SetLifecycleID("testbot", "test-lifecycle-uuid")
	err := w.Write("testbot", "agent_started", nil)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(home, "audit", "testbot", "gateway.jsonl"))
	var event map[string]interface{}
	json.Unmarshal(data, &event)

	if event["lifecycle_id"] != "test-lifecycle-uuid" {
		t.Errorf("lifecycle_id = %v, want test-lifecycle-uuid", event["lifecycle_id"])
	}
}

func TestWriter_NoLifecycleID_WhenNotRegistered(t *testing.T) {
	home := t.TempDir()
	w := NewWriter(home)
	w.Write("unknownbot", "agent_started", nil)

	data, _ := os.ReadFile(filepath.Join(home, "audit", "unknownbot", "gateway.jsonl"))
	var event map[string]interface{}
	json.Unmarshal(data, &event)

	if _, exists := event["lifecycle_id"]; exists {
		t.Error("lifecycle_id should not be present for unregistered agent")
	}
}
