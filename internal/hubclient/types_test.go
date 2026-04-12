package hubclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAssuranceStatementCarriesScopeAndResult(t *testing.T) {
	stmt := AssuranceStatement{
		StatementType:   "ask_reviewed",
		Result:          "ASK-Pass",
		ReviewScope:     "package-change",
		ReviewerType:    "automated",
		PolicyVersion:   "2026-04-12",
		ArtifactKind:    "connector",
		ArtifactName:    "slack-interactivity",
		ArtifactVersion: "1.1.0",
	}
	if stmt.Result != "ASK-Pass" {
		t.Fatalf("unexpected result: %q", stmt.Result)
	}
	if stmt.ReviewScope != "package-change" {
		t.Fatalf("unexpected review scope: %q", stmt.ReviewScope)
	}
}

func TestPublisherRecordCarriesVerifiedIdentityFields(t *testing.T) {
	record := PublisherRecord{
		PublisherID: "org:agency-platform",
		Kind:        "organization",
		DisplayName: "Agency Platform",
	}
	if record.Kind != "organization" {
		t.Fatalf("unexpected kind: %q", record.Kind)
	}
}

func TestFetchArtifactAssurance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/v1/hubs/official/artifacts/connector/slack-interactivity/1.1.0/assurance"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema_version":1,"hub_id":"hub:official:agency","statements":[{"artifact_kind":"connector","artifact_name":"slack-interactivity","artifact_version":"1.1.0","statement_type":"ask_reviewed","result":"ASK-Partial","review_scope":"package-change","reviewer_type":"automated","policy_version":"2026-04-12"}]}`))
	}))
	defer server.Close()

	summary, err := Client{BaseURL: server.URL}.FetchArtifactAssurance(context.Background(), "official", "connector", "slack-interactivity", "1.1.0")
	if err != nil {
		t.Fatalf("FetchArtifactAssurance(): %v", err)
	}
	if len(summary.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(summary.Statements))
	}
	if summary.Statements[0].Result != "ASK-Partial" {
		t.Fatalf("unexpected result: %q", summary.Statements[0].Result)
	}
}
