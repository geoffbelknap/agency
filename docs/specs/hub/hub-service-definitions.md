---
description: "**Date**: 2026-03-11 **Status**: Approved **Author**: Agency team"
---

**Date**: 2026-03-11
**Status**: Approved
**Author**: Agency team

## Problem

Service definitions (e.g., `github.yaml`, `jira.yaml`) describe how the egress proxy handles credentials for external APIs. They currently live only in `agency/services/` in the agency repo and get copied to `~/.agency/services/` during `agency setup`.

This means every new external integration requires a change to the agency repo before a hub-distributed pack or connector can use it. The hub is supposed to be self-contained — `agency hub install some-pack` should bring everything needed to deploy. Today it can't, because the service definition must already exist on the operator's system.

## Solution

Make service definitions a hub-distributable component kind, alongside connectors, presets, skills, workspaces, and policies.

## Design

### Hub repo structure

Service definitions live in `services/` in the hub repo, alongside the other component kinds:

```
agency-hub/
├── services/
│   ├── jira.yaml
│   ├── pagerduty.yaml
│   ├── linear.yaml
│   └── slack.yaml
├── connectors/
│   └── ...
├── packs/
│   └── ...
└── ...
```

### Dependency declaration

Packs declare service dependencies in `requires.services`:

```yaml
kind: pack
name: my-pack
requires:
  connectors:
    - my-connector
  services:
    - some-service
```

Connectors can also declare service requirements directly:

```yaml
kind: connector
name: my-connector
requires:
  services:
    - some-service
```

This means a connector installed standalone (not via a pack) still pulls in its service dependency.

### Install flow

`agency hub install <pack-name>` resolves the full dependency tree:

1. Resolve the pack
2. Resolve `requires.connectors` — find each connector
3. Resolve `requires.services` (from pack and/or connectors) — find each service definition
4. Install in dependency order: services → connectors → pack

Service definitions are single-file components (same install path as presets). They get copied to `~/.agency/services/{name}.yaml`.

### Conflict resolution

If a service definition with the same name already exists in `~/.agency/services/`:

- **Hub install skips it.** The operator's existing definition wins.
- A warning is logged: "Service '{name}' already exists, skipping hub version."
- The operator can manually update with `agency hub install <name> --kind service --force` (or just edit the file).

This respects tenet 5 (governance is operator-owned). The hub provides defaults; the operator has final say.

### Removal

`agency hub remove <pack>` removes the pack and connector but leaves the service definition in place. Service definitions may be shared across multiple packs, so removing one pack shouldn't break another.

`agency hub remove <name> --kind service` explicitly removes a service definition.

## Changes required

### agency repo

**`agency/core/hub.py`**:
- Add `"service"` to `KNOWN_KINDS` tuple (line 17)

**`agency/core/hub.py`** (`discover_components`):
- Service YAML files use `service:` as their name field, not `name:`. Update discovery with a kind-aware name field mapping (e.g. `NAME_FIELDS = {"service": "service"}`) and fall back to `yaml_path.stem` when neither is present.

**`agency_core/models/pack.py`** (`PackRequires`):
- Add `services: list[str]` field (currently has connectors, presets, skills, workspaces, policies)

**`agency_core/models/connector.py`** (`ConnectorConfig`):
- Add optional `requires` field for service dependencies. Connectors can only require services (not other connectors, presets, etc. — that is pack-level composition). Model shape:
  ```python
  class ConnectorRequires(BaseModel):
      services: list[str] = Field(default_factory=list)
  ```
- Add `requires: ConnectorRequires | None = None` to `ConnectorConfig`

**`agency/core/hub.py`** (`resolve_install_tree`):
- Currently only recurses into `requires` for packs (the `if comp["kind"] == "pack"` branch). Add a second branch: when `comp["kind"] == "connector"`, parse its `requires` field and resolve service dependencies. This ensures installing a standalone connector also pulls in its service dependencies.

**`agency/core/hub.py`** (`install_component`):
- Service definitions are single-file, so they follow the existing else branch (same as presets): copy YAML to `~/.agency/services/{name}.yaml`
- Add skip-if-exists logic for service kind with warning
- Add `force: bool = False` parameter. When True, overwrite existing service definitions instead of skipping.

**`agency/commands/hub_cmd.py`**:
- Add `"service"` to the `click.Choice` lists for `--kind` in `hub_install`, `hub_search`, and `hub_remove` commands
- Add `--force` flag to `hub_install` command, passed through to `install_component`

### agency-hub repo

- Add `services/` directory
- `jira.yaml` goes directly to the hub (not bundled in the agency repo). Remove `agency/services/jira.yaml` from the agency repo as part of this work.
- Future service definitions go in the hub, not the agency repo

### Bundled vs hub services

The agency repo keeps a small set of bundled service definitions that ship with `agency setup`:
- `github.yaml` — needed for GitHub App auth (platform-level feature)
- `brave-search.yaml` — common default

All other service definitions (jira, pagerduty, slack, linear, etc.) live in the hub. This keeps the agency repo lean and lets the ecosystem grow independently.

## ASK Framework Compliance

1. **Constraints are external and inviolable.** Service definitions are installed to `~/.agency/services/`, outside agent isolation. Agents never see or modify them.

2. **Every action leaves a trace.** Hub installs are tracked in `hub-installed.json` with provenance (commit SHA, source, timestamp).

3. **Mediation is complete.** No new paths from agent to external resources. Service definitions describe credential format for the egress proxy, which is already in the mediation chain.

4. **Access matches purpose.** A service definition is descriptive, not permissive. It tells the egress proxy how to format auth headers. It does not grant any agent access. Access requires an explicit `agency grant` by the operator.

5. **Governance is operator-owned and read-only.** Service definitions in `~/.agency/services/` are operator-controlled. Hub install is a convenience; operator can edit, delete, or override. Existing definitions are never overwritten without explicit force flag.

6. **Each enforcement layer has its own isolation boundary.** No layers collapsed. This adds a distribution mechanism for config the egress layer already consumes.

## User experience

```bash
# Sync hub sources (agency setup configures the default source)
agency hub update

# Install a pack — dependencies resolve automatically
agency hub install <pack-name>
# → Installing service '<service>' from hub source 'official'
# → Installing connector '<connector>' from hub source 'official'
# → Installing pack '<pack-name>' from hub source 'official'
# → Done. 3 components installed.

# Provide credentials (operator step, always manual)
agency grant <agent-name> <service> --key <api-key>

# Deploy the team
agency deploy <pack-name>
```

### Example: installing a pack that needs Jira

```bash
agency hub update
agency hub install jira-ops
# → Installing service 'jira' from hub source 'official'
# → Installing connector 'jira-ops' from hub source 'official'
# → Installing pack 'jira-ops' from hub source 'official'

agency grant ops-coordinator jira --key <base64-email:token>
agency deploy jira-ops
```

### Example: installing just a service definition

```bash
agency hub install pagerduty --kind service
# → Installing service 'pagerduty' from hub source 'official'
```

## Non-goals

- Service definitions do not auto-grant credentials. The operator always runs `agency grant`.
- Service definitions do not configure egress domain allowlists. That is handled by the policy engine.
- Hub install does not validate that the operator has the required API keys. That happens at `agency deploy` / `agency start`.
