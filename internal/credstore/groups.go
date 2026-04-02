package credstore

import "fmt"

// ResolveGroup merges group-level Protocol, ProtocolConfig, and Requires
// into entry. Entry values win on conflict. If the entry has no group set,
// it is returned unchanged.
func ResolveGroup(entry Entry, backend SecretBackend) (Entry, error) {
	if entry.Metadata.Group == "" {
		return entry, nil
	}

	groupValue, groupMeta, err := backend.Get(entry.Metadata.Group)
	if err != nil {
		return entry, fmt.Errorf("resolve group %q: %w", entry.Metadata.Group, err)
	}

	group := entryFromBackend(entry.Metadata.Group, groupValue, groupMeta)

	// Protocol: entry wins if set.
	if entry.Metadata.Protocol == "" {
		entry.Metadata.Protocol = group.Metadata.Protocol
	}

	// ProtocolConfig: merge group into entry, entry wins on conflict.
	if len(group.Metadata.ProtocolConfig) > 0 {
		if entry.Metadata.ProtocolConfig == nil {
			entry.Metadata.ProtocolConfig = make(map[string]any, len(group.Metadata.ProtocolConfig))
		}
		for k, v := range group.Metadata.ProtocolConfig {
			if _, exists := entry.Metadata.ProtocolConfig[k]; !exists {
				entry.Metadata.ProtocolConfig[k] = v
			}
		}
	}

	// Requires: merge, deduplicate.
	if len(group.Metadata.Requires) > 0 {
		seen := make(map[string]bool, len(entry.Metadata.Requires))
		for _, r := range entry.Metadata.Requires {
			seen[r] = true
		}
		for _, r := range group.Metadata.Requires {
			if !seen[r] {
				entry.Metadata.Requires = append(entry.Metadata.Requires, r)
				seen[r] = true
			}
		}
	}

	return entry, nil
}

// GroupMembers returns all entries whose Metadata.Group matches groupName.
func GroupMembers(groupName string, backend SecretBackend) ([]Entry, error) {
	refs, err := backend.List()
	if err != nil {
		return nil, fmt.Errorf("list for group %q: %w", groupName, err)
	}

	var members []Entry
	for _, ref := range refs {
		if ref.Metadata["group"] != groupName {
			continue
		}
		value, meta, err := backend.Get(ref.Name)
		if err != nil {
			return nil, fmt.Errorf("get group member %q: %w", ref.Name, err)
		}
		members = append(members, entryFromBackend(ref.Name, value, meta))
	}
	return members, nil
}
