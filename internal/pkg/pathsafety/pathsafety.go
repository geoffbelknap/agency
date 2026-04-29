package pathsafety

import (
	"fmt"
	"path/filepath"
	"strings"
)

func Segment(kind, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("%s is required", kind)
	}
	if value == "." || value == ".." {
		return "", fmt.Errorf("%s %q is not a safe path segment", kind, raw)
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return "", fmt.Errorf("%s %q is not a safe path segment", kind, raw)
		}
	}
	return value, nil
}

func Join(base string, elems ...string) (string, error) {
	cleanBase, err := filepath.Abs(filepath.Clean(base))
	if err != nil {
		return "", err
	}
	parts := []string{cleanBase}
	for _, elem := range elems {
		segment, err := Segment("path segment", elem)
		if err != nil {
			return "", err
		}
		parts = append(parts, segment)
	}
	out := filepath.Join(parts...)
	rel, err := filepath.Rel(cleanBase, out)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes base directory")
	}
	return out, nil
}
