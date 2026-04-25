package models

import (
	"fmt"

	agencyconsent "github.com/geoffbelknap/agency/internal/consent"
)

type ConsentRequirement = agencyconsent.Requirement

func validateConsentRequirement(toolName string, requirement *ConsentRequirement, params map[string]bool) error {
	if requirement == nil {
		return nil
	}
	if err := requirement.Validate(); err != nil {
		return fmt.Errorf("tool %q requires_consent_token %w", toolName, err)
	}
	if !params[requirement.TokenInputField] {
		return fmt.Errorf("tool %q requires_consent_token references unknown token_input_field %q", toolName, requirement.TokenInputField)
	}
	if !params[requirement.TargetInputField] {
		return fmt.Errorf("tool %q requires_consent_token references unknown target_input_field %q", toolName, requirement.TargetInputField)
	}
	return nil
}
