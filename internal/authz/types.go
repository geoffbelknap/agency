package authz

type ConsentRequirement struct {
	RequiredFor []string `json:"required_for,omitempty" yaml:"required_for,omitempty"`
}

type Grant struct {
	Subject string              `json:"subject" yaml:"subject"`
	Target  string              `json:"target" yaml:"target"`
	Actions []string            `json:"actions" yaml:"actions"`
	Consent *ConsentRequirement `json:"consent,omitempty" yaml:"consent,omitempty"`
}

type Request struct {
	Subject         string  `json:"subject" yaml:"subject"`
	Target          string  `json:"target" yaml:"target"`
	Action          string  `json:"action" yaml:"action"`
	Instance        string  `json:"instance,omitempty" yaml:"instance,omitempty"`
	ConsentProvided bool    `json:"consent_provided,omitempty" yaml:"consent_provided,omitempty"`
	Grants          []Grant `json:"grants,omitempty" yaml:"grants,omitempty"`
}

type Decision struct {
	Allow         bool     `json:"allow"`
	Reasons       []string `json:"reasons,omitempty"`
	ConsentNeeded bool     `json:"consent_needed,omitempty"`
}
