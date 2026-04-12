package hubclient

type AssuranceStatement struct {
	ArtifactKind    string                 `json:"artifact_kind"`
	ArtifactName    string                 `json:"artifact_name"`
	ArtifactVersion string                 `json:"artifact_version"`
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
