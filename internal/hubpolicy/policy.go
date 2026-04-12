package hubpolicy

type Policy struct{}

func DefaultPolicy() Policy {
	return Policy{}
}

func (Policy) AllowsInstall(kind string, statements []string) bool {
	if contains(statements, "official_source") {
		return true
	}
	required := "publisher_verified"
	switch kind {
	case "connector":
		required = "ask_partial"
	case "provider":
		required = "ask_pass"
	}
	for _, stmt := range statements {
		if stmt == required {
			return true
		}
	}
	return false
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
