package instances

import (
	"fmt"
	"strings"

	agencyconsent "github.com/geoffbelknap/agency/internal/consent"
)

func ValidateInstance(inst *Instance) error {
	if inst == nil {
		return fmt.Errorf("instance is required")
	}
	if strings.TrimSpace(inst.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if strings.TrimSpace(inst.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if err := validatePackageRef("source.template", inst.Source.Template); err != nil && err != errEmptyPackageRef {
		return err
	}
	if err := validatePackageRef("source.package", inst.Source.Package); err != nil && err != errEmptyPackageRef {
		return err
	}
	if isEmptyPackageRef(inst.Source.Template) && isEmptyPackageRef(inst.Source.Package) {
		return fmt.Errorf("source.template or source.package is required")
	}
	seenNodeIDs := make(map[string]struct{}, len(inst.Nodes))
	for i, node := range inst.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			return fmt.Errorf("nodes[%d].id is required", i)
		}
		if strings.TrimSpace(node.Kind) == "" {
			return fmt.Errorf("nodes[%d].kind is required", i)
		}
		if _, ok := seenNodeIDs[node.ID]; ok {
			return fmt.Errorf("duplicate node id %q", node.ID)
		}
		seenNodeIDs[node.ID] = struct{}{}
	}
	consentDeploymentID := strings.TrimSpace(stringValue(inst.Config["consent_deployment_id"]))
	for i, grant := range inst.Grants {
		if strings.TrimSpace(grant.Principal) == "" {
			return fmt.Errorf("grants[%d].principal is required", i)
		}
		if strings.TrimSpace(grant.Action) == "" {
			return fmt.Errorf("grants[%d].action is required", i)
		}
		if req, ok := consentRequirementFromGrant(grant.Config); ok {
			if err := req.Validate(); err != nil {
				return fmt.Errorf("grants[%d] consent requirement: %w", i, err)
			}
			if consentDeploymentID == "" {
				return fmt.Errorf("config.consent_deployment_id is required for consent-gated grants")
			}
		}
	}
	return nil
}

var errEmptyPackageRef = fmt.Errorf("empty package ref")

func validatePackageRef(path string, ref PackageRef) error {
	if isEmptyPackageRef(ref) {
		return errEmptyPackageRef
	}
	if strings.TrimSpace(ref.Kind) == "" {
		return fmt.Errorf("%s.kind is required", path)
	}
	if strings.TrimSpace(ref.Name) == "" {
		return fmt.Errorf("%s.name is required", path)
	}
	return nil
}

func isEmptyPackageRef(ref PackageRef) bool {
	return strings.TrimSpace(ref.Kind) == "" &&
		strings.TrimSpace(ref.Name) == "" &&
		strings.TrimSpace(ref.Version) == ""
}

func consentRequirementFromGrant(cfg map[string]any) (agencyconsent.Requirement, bool) {
	if len(cfg) == 0 {
		return agencyconsent.Requirement{}, false
	}
	raw := cfg
	if nested, ok := cfg["requires_consent_token"].(map[string]any); ok {
		raw = nested
	}
	req := agencyconsent.Requirement{
		OperationKind:    strings.TrimSpace(stringValue(raw["operation_kind"])),
		TokenInputField:  strings.TrimSpace(stringValue(raw["token_input_field"])),
		TargetInputField: strings.TrimSpace(stringValue(raw["target_input_field"])),
	}
	switch v := raw["min_witnesses"].(type) {
	case int:
		req.MinWitnesses = v
	case int64:
		req.MinWitnesses = int(v)
	case float64:
		req.MinWitnesses = int(v)
	}
	if req.OperationKind == "" && req.TokenInputField == "" && req.TargetInputField == "" {
		return agencyconsent.Requirement{}, false
	}
	return req, true
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
