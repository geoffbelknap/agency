package hubclient

import "encoding/json"

type AssuranceIssuer struct {
	HubID       string `json:"hub_id,omitempty"`
	StatementID string `json:"statement_id,omitempty"`
}

type AssuranceStatement struct {
	ArtifactKind    string                 `json:"artifact_kind"`
	ArtifactName    string                 `json:"artifact_name"`
	ArtifactVersion string                 `json:"artifact_version"`
	IssuerHubID     string                 `json:"issuer_hub_id,omitempty"`
	StatementType   string                 `json:"statement_type"`
	Result          string                 `json:"result"`
	ReviewScope     string                 `json:"review_scope"`
	ReviewerType    string                 `json:"reviewer_type"`
	PolicyVersion   string                 `json:"policy_version"`
	Evidence        map[string]interface{} `json:"evidence,omitempty"`
}

type AssuranceSummary struct {
	SchemaVersion int                  `json:"schema_version"`
	HubID         string               `json:"hub_id"`
	GeneratedAt   string               `json:"generated_at"`
	Statements    []AssuranceStatement `json:"statements"`
}

type PublisherRecord struct {
	PublisherID string `json:"publisher_id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
}

func (s *AssuranceStatement) UnmarshalJSON(data []byte) error {
	type flatStatement AssuranceStatement
	var aux struct {
		Artifact *struct {
			Kind    string `json:"kind"`
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"artifact"`
		Issuer *AssuranceIssuer `json:"issuer"`
		flatStatement
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*s = AssuranceStatement(aux.flatStatement)
	if aux.Artifact != nil {
		if s.ArtifactKind == "" {
			s.ArtifactKind = aux.Artifact.Kind
		}
		if s.ArtifactName == "" {
			s.ArtifactName = aux.Artifact.Name
		}
		if s.ArtifactVersion == "" {
			s.ArtifactVersion = aux.Artifact.Version
		}
	}
	if aux.Issuer != nil && s.IssuerHubID == "" {
		s.IssuerHubID = aux.Issuer.HubID
	}
	return nil
}
