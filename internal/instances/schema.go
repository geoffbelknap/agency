package instances

import (
	"fmt"
	"strings"
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
