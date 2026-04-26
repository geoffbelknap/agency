---
description: "Agency Hub is the package manager for Agency. What Homebrew is to macOS, Agency Hub is to Agency. It's how people dis..."
status: "Implemented"
---

# Agency Hub Package Manager

**Date:** 2026-03-30
**Status:** Implemented

## Vision

Agency Hub is the package manager for Agency. What Homebrew is to macOS, Agency Hub is to Agency. It's how people discover, install, configure, and share components.

## Component Types

Every hub component follows the same lifecycle: search → install → activate → running.

| Kind | What it is | Example |
|------|-----------|---------|
| connector | Data source — polls APIs, receives webhooks, watches channels | limacharlie, nextdns-blocked, unifi |
| service | API credential + tool definition | limacharlie-api, nextdns-api, slack |
| preset | Agent configuration template | security-triage, security-explorer |
| mission | Standing instructions for an agent | alert-triage, security-patrol |
| pack | Bundle — team + agents + channels + dependency references | security-ops, slack-ops |
| skill | Agent capability extension | (future) |
| policy | Security/governance policy template | (future) |
| ontology | Knowledge graph type definitions | base-ontology |

## Package Format

Every component is a directory in a hub source repo containing at minimum a YAML file with `kind`, `name`, `version`, and `description`:

```
connectors/
  limacharlie/
    connector.yaml     # the component definition
    metadata.yaml      # stamped by CI on merge (build hash, publish date)
    README.md          # optional documentation
```

### Universal Fields

Every component YAML has:

```yaml
kind: connector          # component type
name: limacharlie        # unique name within kind
version: 0.3.0           # semver
description: >           # one-line description
  LimaCharlie endpoint security connector.
author: agency-hub       # who made it
license: MIT             # SPDX identifier

requires:                # dependencies — resolved transitively on install
  connectors: []         # other connectors this depends on
  services: []           # service definitions needed
  presets: []            # presets needed (for packs)
  missions: []           # missions needed (for packs)
  credentials:           # credentials the operator must provide
    - name: LC_API_KEY
      description: LimaCharlie API key
      type: secret
      scope: service-grant
      grant_name: limacharlie-api
  egress_domains:        # external domains this component accesses
    - api.limacharlie.io
```

### Mission as Component

Missions become installable hub components:

```yaml
kind: mission
name: alert-triage
version: 0.1.0
description: Triage LimaCharlie security alerts for home networks

requires:
  connectors: [limacharlie]
  channels: [security-findings]

instructions: |
  Triage security alerts. Act immediately. Do not ask questions.
  ...

triggers:
  - source: connector
    connector: limacharlie
    event_type: alert_created

budget:
  per_task: 0.10
  daily: 1.00

cost_mode: frugal
```

Hub directory structure:
```
missions/
  alert-triage/
    mission.yaml
    metadata.yaml
```

### Pack Dependencies

Packs declare everything they need. Deploy resolves the full tree:

```yaml
kind: pack
name: security-ops
version: 0.1.0

requires:
  connectors: [limacharlie, limacharlie-sensors, nextdns-blocked]
  services: [limacharlie-api]
  presets: [security-triage, security-explorer]
  missions: [alert-triage, security-explorer-mission]

team:
  name: security-ops
  agents:
    - name: alert-triage
      preset: security-triage
    - name: security-explorer
      preset: security-explorer
  channels:
    - name: security-findings
      topic: Security findings and observations

mission_assignments:
  - mission: alert-triage
    agent: alert-triage
  - mission: security-explorer-mission
    agent: security-explorer
```

## CLI Commands

### Discovery

```bash
agency hub search <query>              # search all component types
agency hub search <query> --kind connector
agency hub info <name>                 # detailed info + README
```

### Sources

```bash
agency hub update                      # git pull all sources
agency hub add-source <name> <url>     # add a third-party hub repo
agency hub remove-source <name>        # remove a source
agency hub list-sources                # show configured sources
```

### Install / Remove

```bash
agency hub install <name>              # install + dependencies
agency hub install <name> --kind connector
agency hub remove <name>               # remove (fails if depended on)
agency hub remove <name> --force       # remove even if depended on
```

Install resolves `requires` transitively:
- `install security-ops` installs the pack + all referenced connectors, services, presets, missions
- Already-installed dependencies are skipped
- Missing dependencies fail the install with a clear message

### Activate / Deactivate

```bash
agency hub activate <name>             # provision credentials, egress, auth
agency hub activate <name> --set KEY=VALUE
agency hub deactivate <name>           # stop but keep installed
```

Activate handles everything:
- Writes credentials to service-keys.env
- Provisions egress domain access
- Configures JWT swap if needed
- Publishes resolved YAML for intake
- Prompts for missing credentials interactively

### Deploy / Teardown (Packs Only)

```bash
agency hub deploy <name>               # install deps + create team + assign missions
agency hub deploy <name> --set KEY=VALUE
agency hub teardown <name>             # stop agents, archive channels
```

Deploy is the "one command" experience:
1. Install all dependencies (connectors, services, presets, missions)
2. Activate all connectors (prompt for credentials)
3. Create the team (agents + channels)
4. Create and assign missions
5. Rebuild intake
6. Verify health

### Upgrade

```bash
agency hub upgrade                     # upgrade all outdated components
agency hub upgrade <name>              # upgrade specific component
agency hub outdated                    # show what's upgradable
agency hub pin <name>                  # prevent auto-upgrade
agency hub unpin <name>
```

### Health

```bash
agency hub check <name>                # is this component working?
agency hub doctor                      # overall hub installation health
```

`hub check` for a connector:
- Is the poll succeeding?
- Are credentials configured?
- Is the egress domain accessible?
- Is the graph_ingest writing nodes?

`hub doctor` system-wide:
- Are all activated connectors polling?
- Are all service keys present?
- Are there unresolved dependencies?
- Are there version mismatches?

### Create / Publish

```bash
agency hub create connector <name>     # scaffold a new connector
agency hub create pack <name>          # scaffold a new pack
agency hub audit <path>                # validate before publishing
agency hub publish <path>              # submit PR to hub source
```

`hub create` scaffolds:
- Valid YAML with required fields
- README template
- Example graph_ingest / routes / triggers
- .gitignore

`hub audit` validates locally:
- Schema validation
- Dependency existence
- Template safety
- Credential reference validity

`hub publish` creates a PR to the hub source repo:
- Runs audit locally first
- Creates branch, commits, pushes, opens PR
- Review bot validates on CI

## Dependency Resolution

Install builds a dependency graph and installs in order:

```
security-ops (pack)
├── limacharlie (connector)
│   └── limacharlie-api (service)
├── limacharlie-sensors (connector)
│   └── limacharlie-api (service)  ← already installed, skip
├── nextdns-blocked (connector)
│   └── nextdns-api (service)
├── security-triage (preset)
├── security-explorer (preset)
├── alert-triage (mission)
└── security-explorer-mission (mission)
```

Rules:
- Install dependencies before dependents
- Skip already-installed components (check by name + kind)
- Fail if a dependency doesn't exist in any source
- No circular dependencies (validate on publish)

## Component Lifecycle

```
(not installed) → installed → active → running
                      ↑           ↓
                      └── deactivated
```

| State | Meaning |
|-------|---------|
| not installed | Not in hub registry |
| installed | YAML in registry, not configured |
| active | Credentials provisioned, egress configured |
| running | Actively polling / processing (connectors) |

Packs add: `deployed` (team created, agents running, missions assigned).

## Hub Sources

A hub source is a git repo with a known directory structure:

```
my-hub/
  connectors/
    my-connector/
      connector.yaml
  services/
    my-service/
      service.yaml
  packs/
    my-pack/
      pack.yaml
      AGENTS.md
  presets/
    my-preset/
      preset.yaml
  missions/
    my-mission/
      mission.yaml
  ontology/
    base-ontology.yaml
  pricing/
    routing.yaml
  .github/
    workflows/
      review-bot.yml
      stamp-metadata.yml
```

Multiple sources supported:
```yaml
# ~/.agency/config.yaml
hub:
  sources:
    - name: default
      url: https://github.com/geoffbelknap/agency-hub.git
    - name: community
      url: https://github.com/agency-community/hub.git
```

Components from different sources can have the same name — source is disambiguated on install:
```bash
agency hub install my-connector --source community
```

## Implementation Phases

### Phase 1: Dependency Resolution (build now)
- `hub install` resolves `requires` transitively
- `hub deploy` auto-installs dependencies + creates missions + assigns
- Mission as a hub component kind
- `hub deploy <name>` by name (not file path)

### Phase 2: Health and Validation
- `hub check <name>` — test a component is working
- `hub doctor` — system-wide health
- `hub audit <path>` — validate before publishing

### Phase 3: Multi-Source and Publishing
- `hub add-source` / `hub remove-source`
- `hub create` — scaffold new components
- `hub publish` — submit PR to source repo
- Version pinning

## Credential Management

Credentials are managed via file editing or agency-web — never via CLI arguments that would leak to terminal history.

### Storage

Flat file at `~/.agency/infrastructure/.service-keys.env`. Format:

```env
# limacharlie connector — api.limacharlie.io
limacharlie-api=<your-limacharlie-api-key>

# nextdns connector — api.nextdns.io
nextdns-api=<your-nextdns-api-key>
```

### Write Safety

- **Atomic writes** — write to `.service-keys.env.tmp`, then `rename()` to `.service-keys.env`. No truncation risk from partial writes or crashes.
- **Backup on every write** — copy to `.service-keys.env.bak` before writing (already built).
- **Auto-restore** — if the file is empty/missing but backup exists, restore from backup (already built).
- **Component-scoped comments** — when a credential is written by `hub install --set`, include a comment with the component name and egress domain.

### Credential Lifecycle

When a component is installed with `--set KEY=VALUE`:
1. Write the key to `.service-keys.env` with a component comment
2. The egress proxy picks it up on next rebuild/reload

When a component is removed:
1. Optionally remove its credential entries (prompt: "Remove credentials for limacharlie-api? [y/N]")
2. Never auto-delete credentials without confirmation

### Editing Credentials

Three ways, in order of preference:

1. **Edit the file directly** — `nano ~/.agency/infrastructure/.service-keys.env`
2. **Via agency-web** — credentials management panel with:
   - List of all configured credentials (key names only, values masked)
   - Per-credential: component that uses it, egress domain, last modified
   - Edit button → masked input field, save writes atomically
   - Test button → attempt an API call using the credential, report success/failure
   - Delete button with confirmation
3. **Via `--set` on install** — `agency hub install limacharlie --set LC_API_KEY=xxx` for automation where history isn't a concern

### What NOT to Build

- No `agency credentials set` CLI command — puts secrets in shell history
- No keychain integration — flat file is sufficient for single-machine deployment
- No encryption at rest — the file is 0600, same security model as `~/.ssh/id_rsa`
- No credential rotation automation — operator edits the file or uses agency-web

### Agency-Web Credentials Page

New page or Admin tab section:

```
Credentials

NAME               COMPONENT          DOMAIN                STATUS
limacharlie-api    limacharlie        api.limacharlie.io    ✓ configured
nextdns-api        nextdns-blocked    api.nextdns.io        ✓ configured
unifi-api          unifi              api.ui.com            ✓ configured
slack              (not installed)    slack.com             ✗ not configured

[+ Add Credential]
```

Each row expandable to show:
- Masked value (●●●●●●●●) with show/copy button
- Edit field
- Test button (hits the API, reports HTTP status)
- Last modified timestamp (from file mtime or comment)

### API Endpoints

```
GET  /api/v1/credentials           → list credentials (names + metadata, NOT values)
PUT  /api/v1/credentials/{name}    → update credential value (body: {value: "..."})
DELETE /api/v1/credentials/{name}  → remove credential
POST /api/v1/credentials/{name}/test → test credential against its service
```

The GET endpoint never returns credential values — only names, associated components, and configured/unconfigured status. Values are only written, never read through the API. This is a security boundary: agency-web can show metadata but can't exfiltrate keys.

## Install Consent and Trust

When installing a component, the operator is shown what the component requires and must consent before it's installed.

### Consent Prompt

```
agency hub install bob/network-scanner

  ⚠ This component requires:

  Credential → Domain mapping:
    SHODAN_API_KEY    → api.shodan.io         ✓ matches service
    CENSYS_API_KEY    → api.censys.io         ✓ matches service

  Egress domains:
    api.shodan.io          (new — not currently allowed)
    api.censys.io          (new — not currently allowed)

  Routes work items to:
    high/critical alerts   → mission: network-triage
    medium alerts          → channel: security-findings

  Source: bob (https://github.com/bob/agency-hub.git)
  Author: bob
  License: MIT

  Install and grant access? [y/N]
```

### Credential-Domain Validation

The platform validates that each credential's service definition matches the domains the component accesses. Mismatches are flagged:

```
  Credential → Domain mapping:
    SHODAN_API_KEY    → collect.evil.com      ✗ credential sent to unrelated domain

  ⚠ WARNING: Credentials are being sent to domains that don't match
  their service providers. This may indicate credential exfiltration.
```

Validation logic:
1. For each credential in `requires.credentials`, find its `grant_name` service definition
2. Check that the service's configured domains overlap with the component's `egress_domains`
3. If a credential would be sent to a domain not in its service definition → flag as suspicious
4. The egress proxy enforces this at runtime regardless — credential injection only happens on matching domains (ASK Tenet 3)

### Trust Levels by Source

| Source | Consent behavior |
|--------|-----------------|
| Default hub (`agency-hub`) | Show summary, auto-approve (operator trusts the official hub) |
| Third-party source | Full consent prompt, require explicit `y` |
| Unknown/unregistered source | Reject — must `hub add-source` first |

### The `--yes` Flag

For automation:
```bash
agency hub install bob/network-scanner --yes    # skip prompt
```

Only available for sources the operator has explicitly trusted via `hub add-source`.

### Enforcement Architecture (Defense in Depth)

The consent prompt is the first line of defense. The architecture enforces regardless:

1. **Egress proxy** — only injects credentials into requests to their configured service domains. A connector can't redirect a credential to a different domain even if the operator approves.
2. **Domain allowlist** — the egress proxy blocks all domains not in the allowlist. Adding a connector adds its domains to the allowlist only on operator consent.
3. **Credential swap config** — maps each credential to specific domains. Generated from service definitions, not from connector YAML.
4. **Hub review bot** — validates credential-domain consistency at publish time for the official hub.

This means: even if a malicious component gets installed, the architecture prevents credential exfiltration. The consent prompt makes the risk visible; the enforcement makes the risk structural.

## Multi-Source (Taps)

```bash
agency hub add-source bob https://github.com/bob/agency-hub.git
agency hub add-source community https://github.com/agency-community/hub.git
agency hub remove-source bob
agency hub list-sources
```

### Name Resolution

Names are unique within a source. If a name exists in multiple sources, the operator must specify:

```bash
# Unambiguous — only one source has this
agency hub install network-scanner

# Ambiguous — multiple sources
agency hub install network-scanner
# Error: network-scanner found in multiple sources: default, bob
# Use --source to specify: agency hub install network-scanner --source bob

# Explicit source
agency hub install network-scanner --source bob
```

### Source Trust

Each source has a trust level:

| Trust | Behavior |
|-------|----------|
| `official` | Auto-approve on install (default hub only) |
| `trusted` | Show consent prompt, `--yes` works |
| `untrusted` | Show consent prompt, `--yes` blocked, always prompt |

Set via:
```bash
agency hub add-source bob https://github.com/bob/hub.git --trust trusted
```

## Scope

**In scope:** Everything above in Phases 1-3 plus credential management, install consent, and multi-source.

**Not in scope:**
- Binary distribution (components are YAML, not executables)
- Paid components / marketplace
- Component signing / verification (trust is per-source, not per-component)
- Automatic conflict resolution between sources
- Credential encryption at rest
- CLI commands that accept secret values as arguments
