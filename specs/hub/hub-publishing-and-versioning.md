---
description: "---"
status: "Approved"
---

# Hub Publishing and Component Versioning

**Date:** 2026-03-28
**Status:** Approved

---

## Problem

Hub components (connectors, packs, presets) have no publish pipeline, no integrity verification, and no update channel. External connectors (like `agency-connector-limacharlie`) developed in their own repos have no path into the hub. Versioning uses bare semver strings with no build provenance.

Supply chain risk: if a connector author's repo is compromised, a malicious update could reach operators automatically. The npm model (auto-trust repo owners) is explicitly what we want to avoid.

---

## Versioning

### Scheme: Semver + Build Metadata

Components use SemVer 2.0 (`MAJOR.MINOR.PATCH`) for the human-facing version. Build metadata is stamped by the hub at publish time, not by the author.

**In the connector YAML (author-controlled):**
```yaml
version: 0.1.0
```

**In the hub registry entry (hub-controlled, added at merge):**
```yaml
version: 0.1.0
build: abc1234
published_at: 2026-03-28T17:30:00Z
reviewed_by: bot | human
```

Authors bump version to signal features, fixes, and breaking changes. The hub stamps the build hash (short commit from the author's source repo) and publish timestamp. Operators see both: `limacharlie 0.1.0 (build abc1234, 2026-03-28)`.

### Version Semantics

- **MAJOR**: Breaking changes — new required credentials, removed MCP tools, changed graph_ingest schema
- **MINOR**: New features — new MCP tools, new graph_ingest rules, new routes
- **PATCH**: Fixes — template corrections, description updates, threshold adjustments

### Dirty Gate

The publish pipeline rejects submissions where the source commit has uncommitted changes. Only clean commits are publishable. This is validated by the CI bot during PR review.

---

## Publishing Pipeline

### Trust Model

The hub repo is the trust boundary. Connector authors develop in their own repos. The PR to the hub repo is the gate. No unsigned content is installable.

```
Author repo                    Hub repo                     Operator
(development)                  (trust boundary)             (consumption)

connector.yaml  ──PR──>  CI bot review  ──merge──>  agency hub install
                         (auto or human)              (pulls from hub)
```

A compromised author repo can push anything — it doesn't reach operators until it passes through the hub review gate.

### Workflow

1. **Develop**: Author works in their own repo (`agency-connector-limacharlie`). Tests pass locally. Version bumped in connector.yaml.

2. **Submit**: Author submits PR to `agency-hub` repo. The PR adds or updates files under `connectors/{name}/`:
   ```
   agency-hub/
     connectors/
       limacharlie/
         connector.yaml
         sensors.yaml        # companion connectors
         README.md
         metadata.yaml       # hub-managed, added by CI
   ```

3. **CI Bot Review**: Automated review runs on the PR. Checks (in order):
   - **Schema validation**: connector.yaml parses against ConnectorConfig model
   - **Credential diff**: any new credential requirements vs previous version?
   - **Egress domain diff**: any new URLs/domains in source.url or mcp.api_base?
   - **Template safety scan**: Jinja2 templates checked for injection patterns
   - **MCP tool diff**: any new tools, removed tools, or changed tool definitions?
   - **Version bump check**: version must be higher than the currently published version
   - **Source provenance**: PR description must include source repo URL + commit hash

4. **Review Decision**:
   - **Auto-approve** (bot merges): No new credentials, no new egress domains, no new MCP tools, no template changes that reference new fields. Routine updates only.
   - **Flag for human review**: Any of the above changed. Bot posts a summary of what changed and why it needs human review. Labels the PR `needs-human-review`.

5. **Merge**: On merge, CI stamps `metadata.yaml`:
   ```yaml
   name: limacharlie
   version: 0.1.0
   build: abc1234
   source_repo: geoffbelknap/agency-connector-limacharlie
   source_commit: abc1234567890
   published_at: 2026-03-28T17:30:00Z
   reviewed_by: bot            # or "human:<github-username>"
   review_checks:
     schema: pass
     credentials: unchanged
     egress_domains: unchanged
     templates: unchanged
     mcp_tools: unchanged
   ```

6. **Install**: `agency hub install limacharlie` pulls from the hub repo. The metadata.yaml provides provenance.

7. **Update**: `agency hub update` checks for new versions in the hub repo. Only reviewed, merged content is visible.

### What the CI Bot Reviews

| Check | Auto-Approve | Flag for Human |
|-------|--------------|----------------|
| Schema validation fails | Block | Block |
| Version not bumped | Block | Block |
| New credential requirement | - | Flag |
| New egress domain in URL | - | Flag |
| New MCP tool added | - | Flag |
| MCP tool definition changed | - | Flag |
| Template references new nested fields | Auto-approve | - |
| Description/docs only changes | Auto-approve | - |
| Threshold/interval changes | Auto-approve | - |
| New graph_ingest rule (same credential scope) | Auto-approve | - |
| Route priority/SLA changes | Auto-approve | - |

The principle: **changes that expand the security surface require human review; changes within the existing security envelope are auto-approved.**

### What Gets Human Review

When the bot flags a PR, the human reviewer checks:

- **New credentials**: Is there a legitimate reason for the new credential? Does the connector actually need it?
- **New egress domains**: Is the domain the legitimate API endpoint for the service? Is it documented?
- **New MCP tools**: Do the tools make sense for the connector's purpose? Do the parameters look safe?
- **Tool definition changes**: What changed and why? Could a compromised tool definition exfiltrate data?

The reviewer does not need to audit the full YAML — the bot has already validated schema, templates, and structure. The human review is focused on intent and authorization scope.

---

## Update Safety

### Operator Controls

Operators control how updates are applied:

- **Pin version**: `agency hub pin limacharlie 0.1.0` — locks to a specific version, ignores updates
- **Pin major**: `agency hub pin limacharlie 0.x` — accepts minor/patch updates within major version
- **Auto-update** (default): `agency hub update` pulls the latest reviewed version
- **Review before update**: `agency hub update --dry-run` shows what would change before applying

### Breaking Change Protection

When a connector bumps its major version:
- `agency hub update` does NOT auto-install major version bumps
- Operator must explicitly: `agency hub install limacharlie@1.0.0`
- The hub CLI prints a summary of breaking changes (from the connector's changelog)

### Rollback

`agency hub rollback limacharlie` reverts to the previously installed version. The hub keeps one previous version locally for instant rollback.

---

## Component Types in the Hub

The same publish pipeline applies to all hub component types:

| Type | Directory | Key Review Checks |
|------|-----------|-------------------|
| Connector | `connectors/{name}/` | Credentials, egress domains, MCP tools, templates |
| Pack | `packs/{name}/` | Agent configs, capability grants, mission definitions |
| Preset | `presets/{name}/` | Model configs, behavioral parameters |
| Ontology extension | `ontology/{name}/` | New entity/relationship types |
| Skill | `skills/{name}/` | Skill definitions, tool usage patterns |

### Pack-Specific Review

Packs include agent configurations and capability grants — these are higher trust than connectors. Pack PRs always require human review (no auto-approve path), because:
- A pack can grant capabilities to agents
- A pack can define missions with budget allocations
- A pack can include connector references that expand egress scope

---

## Hub Repo Structure

```
agency-hub/
  connectors/
    limacharlie/
      connector.yaml
      sensors.yaml
      metadata.yaml       # hub-managed
      README.md
    slack/
      connector.yaml
      metadata.yaml
      README.md
  packs/
    soc-triage/
      pack.yaml
      metadata.yaml
      README.md
  presets/
    ...
  ontology/
    ...
  .github/
    workflows/
      review-bot.yml      # CI bot review pipeline
    CODEOWNERS            # human reviewers for flagged PRs
```

---

## Non-Goals (v1)

- **Paid/marketplace components**: All components are free and open. Paid marketplace is a future concern.
- **Private hub registries**: Operators use the single agency-hub repo. Private forks are possible but not officially supported yet.
- **Dependency resolution**: Components don't declare dependencies on other components. A pack that needs a connector documents it in README, but there's no automated dependency install.
- **Code signing with detached signatures**: The hub repo's merge commits are the trust anchor. Detached cryptographic signatures (cosign/sigstore) can be added later but are not required for v1.

---

## Security Properties

- **No auto-trust of authors**: A compromised author repo does not affect operators until a malicious change passes the hub review gate
- **Graduated review**: Routine changes auto-approved by bot; security-surface changes require human review
- **Provenance chain**: Every installed component traces back to a specific source commit, review decision, and publish timestamp
- **Rollback capability**: Operators can instantly revert to previous version
- **Version pinning**: Operators can lock versions to prevent any updates
- **Breaking change protection**: Major version bumps require explicit operator action
