# OCI Distribution for agency-hub

## Summary

Publish hub components (connectors, packs, presets, missions, skills, providers, services, ontology) as signed OCI artifacts to GHCR. Update the agency CLI to pull from OCI instead of git. Third-party contributions follow the Homebrew model: PR to the repo, CI publishes to the registry.

## Motivation

The hub currently distributes components via git clone. This has three problems:

1. **No per-component pull.** `agency hub install limacharlie` requires cloning the entire repo.
2. **No signing or provenance.** Users cannot verify that a component hasn't been tampered with.
3. **No enterprise registry mirroring.** Organizations that restrict external git access cannot mirror hub content to their own infrastructure.

OCI artifacts solve all three: per-component addressability, cosign/sigstore signing, and standard registry mirroring (GHCR → ECR/ACR/Artifactory).

## Artifact Layout

Each component is an OCI artifact at:

```
ghcr.io/geoffbelknap/agency-hub/{kind}/{name}:{version}
```

Examples:
- `ghcr.io/geoffbelknap/agency-hub/connector/limacharlie:0.5.0`
- `ghcr.io/geoffbelknap/agency-hub/pack/security-ops:0.3.0`
- `ghcr.io/geoffbelknap/agency-hub/preset/platform-expert:0.1.0`
- `ghcr.io/geoffbelknap/agency-hub/ontology/base-ontology:0.1.0`

### Layers

Each artifact has two layers:

| Layer | Media Type | Content |
|---|---|---|
| Component | `application/vnd.agency.hub.{kind}.v1+yaml` | The component YAML file |
| Metadata | `application/vnd.agency.hub.metadata.v1+yaml` | The stamped metadata.yaml |

### Tags

- `:{version}` (e.g., `:0.5.0`) — immutable semver tag, never overwritten
- `:latest` — mutable, points to the newest version

### Annotations

Standard OCI annotations on each artifact:

| Annotation | Value |
|---|---|
| `org.opencontainers.image.version` | Semver version |
| `org.opencontainers.image.source` | `https://github.com/geoffbelknap/agency-hub` |
| `org.opencontainers.image.revision` | Git SHA |
| `agency.hub.kind` | `connector`, `pack`, `preset`, etc. |
| `agency.hub.reviewed_by` | From metadata.yaml (`bot` or reviewer identity) |

## CI Publishing Pipeline

Triggered on merge to main when component paths change (extends the existing stamp-metadata workflow):

1. **stamp-metadata** (existing) — stamps version, build hash, timestamp into metadata.yaml
2. **oras push** — pushes changed components to GHCR as OCI artifacts with both layers and annotations
3. **cosign sign** — keyless signing with GitHub Actions OIDC identity (Sigstore Fulcio + Rekor)
4. **cosign attest** — SLSA provenance attestation (proves artifact was built by this CI from this source)

Only changed components are pushed — the stamp-metadata workflow already detects which paths changed.

### Registry Authentication

GitHub Actions get automatic GHCR access via `GITHUB_TOKEN`. No additional secrets needed for the official hub.

## Agency CLI Changes

### New Default Source

The built-in hub source changes from a git URL to an OCI registry base:

```yaml
# ~/.agency/hub.yaml (or equivalent config)
sources:
  - name: official
    type: oci
    registry: ghcr.io/geoffbelknap/agency-hub
```

### Command Changes

| Command | Current (git) | New (OCI) |
|---|---|---|
| `hub search [query]` | Scans dirs in cloned repo | OCI registry catalog API + filter by annotations |
| `hub install <name>` | Copies YAML from cloned repo | `oras pull` artifact, verify cosign signature, write to local hub dir |
| `hub list` | Lists installed from local state | Unchanged — reads local installed state |
| `hub update` | `git pull` on cloned repo | For each installed component: check registry for newer version, pull if available |
| `hub add-source <name> <url>` | Accepts git URL | Accepts OCI registry base URL |
| `hub remove-source <name>` | Removes git source | Removes OCI source |

### Signature Verification

On `hub install` and `hub update`:

1. Pull the artifact from the registry
2. Verify cosign signature against Sigstore transparency log (Rekor)
3. If verification fails: **reject the component, do not install** (ASK tenet 23 — unverified entities default to zero trust)
4. Log the artifact digest, signature status, and source registry to the audit trail

No `--skip-verify` flag. Signature verification is not optional. (ASK tenet 1 — constraints are external and inviolable.)

### ORAS Go Library

Add `oras.land/oras-go/v2` as a dependency in agency's `go.mod`. Used in `internal/hub/` for:
- `oras.Copy` — pull artifacts from registry to local OCI store
- Registry catalog/tag listing for search and update checks
- Media type handling for component and metadata layers

## Third-Party Contributions

### Homebrew Model (official hub)

Contributors submit PRs with component YAML files to the `agency-hub` repo. The review bot validates schema, security surface, and semver. Maintainers merge. CI publishes to GHCR. Contributors never interact with the registry directly.

### Private/Enterprise Registries

Organizations can host their own hub components:

```bash
agency hub add-source my-corp ghcr.io/my-corp/agency-hub
agency hub install my-corp/my-connector
```

They set up their own CI pipeline to push OCI artifacts to their registry. The agency CLI treats all OCI sources identically — same pull, same signature verification (against their own signing identity).

## Migration

For existing installations that have git-sourced components:

1. On CLI update, the default source entry switches from git URL to OCI registry base
2. On first `hub update`, detect locally-installed components and re-fetch from OCI
3. Installed components are just YAML files on disk — no data loss during migration
4. If a component exists locally but not yet in OCI (edge case during rollout), keep the local copy and log a warning

## Supply Chain Security

| Measure | Implementation | ASK Alignment |
|---|---|---|
| Keyless signing | Cosign + GitHub OIDC + Sigstore Fulcio | Tenet 6 — all trust is explicit and auditable |
| Signature verification on install | Mandatory, no skip flag | Tenet 1 — constraints are inviolable |
| Provenance attestation | SLSA via cosign attest | Tenet 5 — runtime is a known quantity |
| Audit trail | Every install/update logged with digest + sig status | Tenet 2 — every action leaves a trace |
| Immutable versions | Semver tags are never overwritten | Tenet 10 — constraint history is immutable |
| Zero trust default | Unverified artifacts rejected | Tenet 23 — unverified defaults to zero trust |

## Cost

GHCR is free for public repositories. agency-hub is public, so all artifact storage and pulls are free. Enterprise customers using private registries bear their own registry costs.

## Dependencies

- `oras.land/oras-go/v2` — OCI artifact push/pull (Go library, added to agency)
- `sigstore/cosign` — signing and verification (CI tool + Go library for verification in CLI)
- Existing: stamp-metadata CI workflow, review bot CI workflow
