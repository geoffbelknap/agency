package registry

import (
	"fmt"
	"strings"
)

// Permits checks whether the given permission set grants the required permission.
// Rules:
//   - "*" matches everything (superuser)
//   - Exact match: "agent.read" matches "agent.read"
//   - Namespace wildcard: "knowledge.*" matches any "knowledge.<suffix>"
//   - Empty perms always returns false
func Permits(perms []string, required string) bool {
	for _, p := range perms {
		if p == "*" {
			return true
		}
		if p == required {
			return true
		}
		if strings.HasSuffix(p, ".*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(required, prefix) && len(required) > len(prefix) {
				return true
			}
		}
	}
	return false
}

// ApplyPermissionCeiling returns the subset of requested permissions permitted
// by the parent ceiling.
func ApplyPermissionCeiling(parentPerms, requested []string) []string {
	var effective []string
	for _, perm := range requested {
		if Permits(parentPerms, perm) {
			effective = append(effective, perm)
		}
	}
	if effective == nil {
		effective = []string{}
	}
	return effective
}

// EffectivePermissions resolves the effective permission set for a principal,
// accounting for status, parent hierarchy, and ceiling enforcement.
//
// Algorithm:
//  1. Resolve principal by UUID
//  2. If status is "suspended" or "revoked" -> empty (no permissions)
//  3. If no parent -> return own permissions
//  4. Get parent's effective permissions (recursive)
//  5. If own permissions is empty -> inherit parent's full set
//  6. Otherwise -> intersection: keep only own permissions the parent ceiling permits
func (r *Registry) EffectivePermissions(uuid string) ([]string, error) {
	p, err := r.Resolve(uuid)
	if err != nil {
		return nil, fmt.Errorf("resolve principal %s: %w", uuid, err)
	}

	if p.Status == "suspended" || p.Status == "revoked" {
		return []string{}, nil
	}

	if p.Parent == "" {
		return p.Permissions, nil
	}

	parentPerms, err := r.EffectivePermissions(p.Parent)
	if err != nil {
		return nil, fmt.Errorf("resolve parent permissions: %w", err)
	}

	// Empty own permissions -> inherit parent's full set.
	if len(p.Permissions) == 0 {
		return parentPerms, nil
	}

	// Intersection: keep only permissions the parent ceiling permits.
	return ApplyPermissionCeiling(parentPerms, p.Permissions), nil
}
