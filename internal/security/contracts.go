package security

import "strings"

type DecisionOutcome string

const (
	DecisionAllow           DecisionOutcome = "allow"
	DecisionDeny            DecisionOutcome = "deny"
	DecisionConsentRequired DecisionOutcome = "consent_required"
)

type FindingStatus string

const (
	FindingPass FindingStatus = "pass"
	FindingWarn FindingStatus = "warn"
	FindingFail FindingStatus = "fail"
)

type Decision struct {
	Allow         bool            `json:"allow"`
	Outcome       DecisionOutcome `json:"outcome,omitempty"`
	Reasons       []string        `json:"reasons,omitempty"`
	ConsentNeeded bool            `json:"consent_needed,omitempty"`
}

type Finding struct {
	Name    string        `json:"name"`
	Agent   string        `json:"agent,omitempty"`
	Scope   string        `json:"scope,omitempty"`
	Backend string        `json:"backend,omitempty"`
	Status  FindingStatus `json:"status"`
	Detail  string        `json:"detail,omitempty"`
	Fix     string        `json:"fix,omitempty"`
}

type MutationStatus string

const (
	MutationApplied  MutationStatus = "applied"
	MutationRejected MutationStatus = "rejected"
)

type Mutation struct {
	Action string         `json:"action"`
	Agent  string         `json:"agent,omitempty"`
	Scope  string         `json:"scope,omitempty"`
	Target string         `json:"target,omitempty"`
	Status MutationStatus `json:"status"`
	Detail string         `json:"detail,omitempty"`
}

type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalRouted   ApprovalStatus = "routed"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
	ApprovalDenied   ApprovalStatus = "denied"
	ApprovalCanceled ApprovalStatus = "canceled"
)

type PolicyStepStatus string

const (
	PolicyStepOK        PolicyStepStatus = "ok"
	PolicyStepMissing   PolicyStepStatus = "missing"
	PolicyStepViolation PolicyStepStatus = "violation"
)

type PolicyExceptionStatus string

const (
	PolicyExceptionActive  PolicyExceptionStatus = "active"
	PolicyExceptionExpired PolicyExceptionStatus = "expired"
	PolicyExceptionInvalid PolicyExceptionStatus = "invalid"
)

type AuthorityExecutionStatus string

const (
	AuthorityExecutionDenied         AuthorityExecutionStatus = "denied"
	AuthorityExecutionExecuted       AuthorityExecutionStatus = "executed"
	AuthorityExecutionNotImplemented AuthorityExecutionStatus = "not_implemented"
)

type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

func ParseRiskLevel(raw string) (RiskLevel, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(RiskLow):
		return RiskLow, true
	case string(RiskMedium):
		return RiskMedium, true
	case string(RiskHigh):
		return RiskHigh, true
	case string(RiskCritical):
		return RiskCritical, true
	default:
		return "", false
	}
}

func IsSecurityAuditEvent(event string) bool {
	event = strings.ToLower(strings.TrimSpace(event))
	if event == "" {
		return false
	}
	return strings.Contains(event, "security") ||
		strings.Contains(event, "xpia") ||
		strings.Contains(event, "finding") ||
		strings.Contains(event, "blocked") ||
		strings.Contains(event, "denied") ||
		strings.Contains(event, "mcp_tool_mutation")
}
