package knowledge

import (
	"context"
	"testing"
)

func TestParseOntologyCandidatesNormalizesKnowledgeNodes(t *testing.T) {
	raw := []byte(`{"candidates":[{"id":"cand-1","label":"candidate:device","properties":{"value":"device","occurrence_count":15,"source_count":4,"status":"candidate","candidate_type":"kind"}}]}`)

	candidates, err := ParseOntologyCandidates(raw)
	if err != nil {
		t.Fatalf("ParseOntologyCandidates returned error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].ID != "cand-1" {
		t.Fatalf("expected normalized ID, got %q", candidates[0].ID)
	}
	if candidates[0].Value != "device" {
		t.Fatalf("expected normalized value, got %q", candidates[0].Value)
	}
	if candidates[0].Count != 15 {
		t.Fatalf("expected normalized count, got %d", candidates[0].Count)
	}
	if candidates[0].Status != "candidate" {
		t.Fatalf("expected normalized status, got %q", candidates[0].Status)
	}
}

func TestResolveOntologyCandidateIDFindsLegacyValue(t *testing.T) {
	proxy := NewProxy()
	proxy.client = stubHTTPClient(func() []byte {
		return []byte(`{"candidates":[{"id":"cand-2","label":"candidate:service","properties":{"value":"service","occurrence_count":5,"status":"candidate"}}]}`)
	})

	id, err := ResolveOntologyCandidateID(context.Background(), proxy, "", "service")
	if err != nil {
		t.Fatalf("ResolveOntologyCandidateID returned error: %v", err)
	}
	if id != "cand-2" {
		t.Fatalf("expected resolved ID cand-2, got %q", id)
	}
}
