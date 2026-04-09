package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
)

type OntologyCandidate struct {
	ID            string `json:"id"`
	Value         string `json:"value"`
	Count         int    `json:"count,omitempty"`
	Source        string `json:"source,omitempty"`
	Status        string `json:"status,omitempty"`
	CandidateType string `json:"candidate_type,omitempty"`
}

type ontologyCandidatesEnvelope struct {
	Candidates []OntologyCandidate `json:"candidates"`
}

// ListOntologyCandidates returns a flattened candidate shape regardless of the
// raw node format returned by the knowledge service.
func ListOntologyCandidates(ctx context.Context, proxy *Proxy) ([]OntologyCandidate, error) {
	raw, err := proxy.Get(ctx, "/ontology/candidates")
	if err != nil {
		return nil, err
	}
	return ParseOntologyCandidates(raw)
}

// ParseOntologyCandidates normalizes ontology candidate responses into a
// stable API shape consumable by the web, CLI, and MCP tools.
func ParseOntologyCandidates(raw []byte) ([]OntologyCandidate, error) {
	var envelope struct {
		Candidates []json.RawMessage `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}

	candidates := make([]OntologyCandidate, 0, len(envelope.Candidates))
	for _, item := range envelope.Candidates {
		candidate, err := parseOntologyCandidate(item)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

// ResolveOntologyCandidateID converts a candidate value into its backing node
// ID so legacy callers can keep addressing ontology actions by value.
func ResolveOntologyCandidateID(ctx context.Context, proxy *Proxy, nodeID string, value string) (string, error) {
	if nodeID != "" {
		return nodeID, nil
	}
	if value == "" {
		return "", fmt.Errorf("node_id or value is required")
	}

	candidates, err := ListOntologyCandidates(ctx, proxy)
	if err != nil {
		return "", err
	}
	for _, candidate := range candidates {
		if candidate.Value == value {
			return candidate.ID, nil
		}
	}
	return "", fmt.Errorf("ontology candidate %q not found", value)
}

func parseOntologyCandidate(raw json.RawMessage) (OntologyCandidate, error) {
	var flat OntologyCandidate
	if err := json.Unmarshal(raw, &flat); err == nil && flat.ID != "" && flat.Value != "" {
		return flat, nil
	}

	var node struct {
		ID         string          `json:"id"`
		Label      string          `json:"label"`
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &node); err != nil {
		return OntologyCandidate{}, err
	}

	var props struct {
		Value         string `json:"value"`
		Occurrence    int    `json:"occurrence_count"`
		SourceCount   int    `json:"source_count"`
		Status        string `json:"status"`
		CandidateType string `json:"candidate_type"`
	}
	if len(node.Properties) > 0 {
		if err := json.Unmarshal(node.Properties, &props); err != nil {
			return OntologyCandidate{}, err
		}
	}

	value := props.Value
	if value == "" {
		value = node.Label
	}
	source := ""
	if props.SourceCount > 0 {
		source = "knowledge"
	}

	return OntologyCandidate{
		ID:            node.ID,
		Value:         value,
		Count:         props.Occurrence,
		Source:        source,
		Status:        props.Status,
		CandidateType: props.CandidateType,
	}, nil
}
