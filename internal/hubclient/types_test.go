package hubclient

import "testing"

func TestAssuranceStatementCarriesScopeAndResult(t *testing.T) {
	stmt := AssuranceStatement{
		StatementType:  "ask_reviewed",
		Result:         "ASK-Pass",
		ReviewScope:    "package-change",
		ReviewerType:   "automated",
		PolicyVersion:  "2026-04-12",
		ArtifactKind:   "connector",
		ArtifactName:   "slack-interactivity",
		ArtifactVersion:"1.1.0",
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
