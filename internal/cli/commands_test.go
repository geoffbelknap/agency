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
