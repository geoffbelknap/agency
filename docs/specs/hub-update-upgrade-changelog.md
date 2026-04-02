---
description: "Restructure agency hub lifecycle commands to mirror Homebrew's update/upgrade/outdated pattern, with rich reporting o..."
---

# Hub Update / Upgrade / Outdated

Restructure `agency hub` lifecycle commands to mirror Homebrew's update/upgrade/outdated pattern, with rich reporting of what changed and what's available.

## Problem

`agency hub update` currently pulls sources, syncs managed files, and prints "Hub sources updated" with no detail about what actually changed. Operators have no visibility into whether an update brought new ontology types, routing changes, or component version bumps. There's also no way to upgrade installed components to newer versions without manually re-installing them.

## Command Model

| Command | Network | Writes to ~/.agency/ | Purpose |
|---------|---------|---------------------|---------|
| `hub update` | yes (git pull) | no | Refresh hub cache, report what's new |
| `hub outdated` | no | no | Show what would be upgraded from current cache |
| `hub upgrade` | no | yes | Sync managed files + upgrade all installed components |
| `hub upgrade <name> [...]` | no | yes | Upgrade specific components only (no managed file sync) |

### Breaking change from current behavior

`hub update` currently syncs managed files (ontology, routing, services) to `~/.agency/`. After this change, that sync moves to `hub upgrade`. Operators who run `hub update` today and expect files to be synced will need to also run `hub upgrade`.

## Design

### Return Types

```go
// UpdateReport is returned by hub update.
type UpdateReport struct {
    Sources   []SourceUpdate     `json:"sources"`
    Available []AvailableUpgrade `json:"available,omitempty"`
    Warnings  []string           `json:"warnings,omitempty"`
}

type SourceUpdate struct {
    Name        string `json:"name"`
    OldCommit   string `json:"old_commit"`
    NewCommit   string `json:"new_commit"`
    CommitCount int    `json:"commit_count"`
}

type AvailableUpgrade struct {
    Name             string `json:"name"`
    Kind             string `json:"kind"`     // "connector", "pack", "managed"
    Category         string `json:"category,omitempty"` // for managed: "ontology", "routing", "services"
    InstalledVersion string `json:"installed_version"`
    AvailableVersion string `json:"available_version"`
    Summary          string `json:"summary,omitempty"`
}

// UpgradeReport is returned by hub upgrade.
type UpgradeReport struct {
    Files     []FileUpgrade      `json:"files,omitempty"`
    Components []ComponentUpgrade `json:"components,omitempty"`
    Warnings  []string           `json:"warnings,omitempty"`
}

type FileUpgrade struct {
    Category string `json:"category"` // "ontology", "routing", "services"
    Path     string `json:"path"`
    Status   string `json:"status"`   // "upgraded", "unchanged", "added", "error"
    Summary  string `json:"summary,omitempty"`
}

type ComponentUpgrade struct {
    Name       string `json:"name"`
    Kind       string `json:"kind"`
    OldVersion string `json:"old_version"`
    NewVersion string `json:"new_version"`
    Status     string `json:"status"` // "upgraded", "unchanged", "error"
    Error      string `json:"error,omitempty"`
}
```

### hub update

1. For each hub source, `git pull` into hub-cache/. Capture old and new HEAD commit.
2. If HEAD changed, count commits between old and new.
3. Compare installed component versions against hub cache versions.
4. Compare managed file hashes (`~/.agency/` vs hub-cache/) for ontology, routing, services.
5. Return `UpdateReport` with source updates and available upgrades.

Does **not** write to `~/.agency/`.

### hub outdated

Same comparison logic as step 3-4 of `hub update`, but no git pull. Reads only from the current hub-cache/ state. Returns `[]AvailableUpgrade` (reuses the same type).

### hub upgrade

1. Snapshot current managed file hashes and installed component versions.
2. Sync managed files from hub-cache/ to `~/.agency/`:
   - `ontology/base-ontology.yaml` → `~/.agency/knowledge/base-ontology.yaml`
   - `pricing/routing.yaml` → `~/.agency/infrastructure/routing.yaml`
   - `services/*.yaml` → `~/.agency/registry/services/`
3. For each installed component, if hub cache has a newer version:
   - Copy new component files from cache to installed location
   - Re-run activation validation if the component is active (credential requirements may change)
   - If validation fails (e.g., new required credential), mark as `error` with message, leave old version in place
4. Diff against snapshot, generate per-file summaries.
5. Return `UpgradeReport`.

### hub upgrade \<name\> [...]

Same as `hub upgrade` but:
- Only upgrades named components
- Does **not** sync managed files
- Returns `UpgradeReport` with only the named components

### Comparison Logic

**Component versions**: String comparison of the `version` field in connector/pack/preset YAML. A version is "newer" if it differs from installed. Semver ordering is not required — any difference is reported.

**Managed files**: SHA-256 hash of file contents. Different hash = upgrade available.

**Summary generation** is category-specific:
- **Ontology**: Parse both YAML files, diff entity type and relationship type map sizes, report version change (e.g., `v1 → v2 (+3 entity types, +6 relationships)`)
- **Routing**: Report provider count delta (e.g., `+1 provider`)
- **Services**: Report added/removed/updated service file names

### CLI Output

**`hub update` — nothing new:**
```
✓ Hub sources up to date
```

**`hub update` — changes available:**
```
✓ Hub sources updated
  Sources:
    default  3 new commits (720f2ab → 60d2b43)
  Upgrades available:
    ontology            v1 → v2 (+3 entity types, +6 relationships)
    nextdns-blocked     connector  0.1.0 → 0.2.0
    nextdns-analytics   connector  0.1.0 → 0.2.0
  Run 'agency hub upgrade' to apply.
```

**`hub outdated` — upgrades available:**
```
Upgrades available:
  ontology            managed    v1 → v2
  nextdns-blocked     connector  0.1.0 → 0.2.0
  nextdns-analytics   connector  0.1.0 → 0.2.0
```

**`hub outdated` — everything current:**
```
All components up to date.
```

**`hub upgrade` — success:**
```
✓ Hub upgraded
  Synced:
    ontology   v1 → v2 (+3 entity types, +6 relationships)
    routing    unchanged
    services   unchanged
  Upgraded:
    nextdns-blocked     connector  0.1.0 → 0.2.0
    nextdns-analytics   connector  0.1.0 → 0.2.0
```

**`hub upgrade` — partial failure:**
```
✓ Hub upgraded (1 error)
  Synced:
    ontology   v1 → v2
    routing    unchanged
    services   unchanged
  Upgraded:
    nextdns-analytics   connector  0.1.0 → 0.2.0
  Errors:
    nextdns-blocked     connector  0.1.0 → 0.2.0  ✗ new required credential: NEXTDNS_ORG_ID
```

**`hub upgrade nextdns-blocked` — specific component:**
```
✓ nextdns-blocked upgraded  0.1.0 → 0.2.0
```

### API Endpoints

| Method | Path | Body | Returns |
|--------|------|------|---------|
| `POST` | `/api/v1/hub/update` | none | `UpdateReport` |
| `GET` | `/api/v1/hub/outdated` | none | `[]AvailableUpgrade` |
| `POST` | `/api/v1/hub/upgrade` | `{"components": ["name", ...]}` (optional) | `UpgradeReport` |

### MCP Tools

- `agency_hub_update` — existing, returns `UpdateReport` instead of bare status
- `agency_hub_outdated` — new, returns `[]AvailableUpgrade`
- `agency_hub_upgrade` — new, optional `components` parameter

### Affected Components

| Component | Change |
|-----------|--------|
| `agency-gateway/internal/hub/hub.go` — `Update()` | Remove managed file sync, add snapshot/diff logic, return `UpdateReport` |
| `agency-gateway/internal/hub/hub.go` — new `Outdated()` | Compare installed vs cache |
| `agency-gateway/internal/hub/hub.go` — new `Upgrade()` | Managed file sync + component upgrade with validation |
| `agency-gateway/internal/hub/types.go` — new file | `UpdateReport`, `UpgradeReport`, sub-types |
| `agency-gateway/internal/api/handlers_phase7.go` | New `hubOutdated`, `hubUpgrade` handlers; update `hubUpdate` response |
| `agency-gateway/internal/api/routes.go` | Register new endpoints |
| `agency-gateway/internal/cli/commands.go` | New `outdated`, `upgrade` subcommands; update `update` output formatting |
| `agency-gateway/internal/apiclient/client.go` | New `HubOutdated()`, `HubUpgrade()` methods |
| `agency-gateway/internal/api/mcp_admin.go` | New MCP tool registrations |

## Scope

**In scope:**
- `hub update` behavior change: cache-only refresh + reporting
- `hub outdated` command, API endpoint, MCP tool
- `hub upgrade` command (all + named), API endpoint, MCP tool
- `UpdateReport`, `UpgradeReport` structured types
- Category-specific summary generation (ontology, routing, services)
- Activation re-validation on component upgrade
- CLI formatting for all three commands

**Not in scope:**
- Downgrade support
- Pinning components to specific versions
- Upgrade across kind changes (renamed/split components)
- Changelog persistence (reports are ephemeral)
- Automatic upgrade on `hub update`
