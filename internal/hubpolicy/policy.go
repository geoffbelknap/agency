package hubpolicy

import "github.com/geoffbelknap/agency/internal/hubclient"

type Policy struct{}

func DefaultPolicy() Policy {
	return Policy{}
}

func (Policy) AllowsInstall(kind string, statements []string) bool {
	structured := make([]hubclient.AssuranceStatement, 0, len(statements))
	for _, stmt := range statements {
		switch stmt {
		case "official_source":
			structured = append(structured, hubclient.AssuranceStatement{
				StatementType: "source_verified",
				Result:        "verified",
			})
		case "publisher_verified":
			structured = append(structured, hubclient.AssuranceStatement{
				StatementType: "publisher_verified",
				Result:        "verified",
			})
		case "ask_partial":
			structured = append(structured, hubclient.AssuranceStatement{
				StatementType: "ask_reviewed",
				Result:        "ASK-Partial",
			})
		case "ask_pass":
			structured = append(structured, hubclient.AssuranceStatement{
				StatementType: "ask_reviewed",
				Result:        "ASK-Pass",
			})
		}
	}
	return (Policy{}).AllowsInstallStatements(kind, structured)
}

func (Policy) AllowsInstallStatements(kind string, statements []hubclient.AssuranceStatement) bool {
	if hasStatement(statements, "source_verified", "verified") {
		return true
	}
	required := "publisher_verified"
	switch kind {
	case "connector":
		required = "ask_partial"
	case "provider":
		required = "ask_pass"
	}
	switch required {
	case "publisher_verified":
		return hasStatement(statements, "publisher_verified", "verified")
	case "ask_partial":
		return hasStatement(statements, "ask_reviewed", "ASK-Partial") ||
			hasStatement(statements, "ask_reviewed", "ASK-Pass")
	case "ask_pass":
		return hasStatement(statements, "ask_reviewed", "ASK-Pass")
	}
	return false
}

func hasStatement(items []hubclient.AssuranceStatement, statementType, result string) bool {
	for _, item := range items {
		if item.StatementType == statementType && item.Result == result {
			return true
		}
	}
	return false
}
