# Hub Deployments: Durable, Portable Pack Configuration

**Date:** 2026-04-11
**Status:** Draft
**Scope:** Introduces `agency hub deployment` — a durable, portable unit that bundles one or more hub instances with their configuration, credential references, bindings, and audit trail, independent of the installed pack artifact. Does not change the hub artifact format, the hub install flow, or the credential store.

## Problem

Hub instances today (`agency hub instances`, `agency hub <name> activate`, `agency hub <name> config`) give each activated component a name, a UUID, and a config block with `${…}` placeholders plus credential references. This works for the single-agency happy path, but it has four gaps that surface as soon as a pack carries meaningful operator configuration:

1. **Not durable.** Hub instance state lives under the agency's local state directory. An `agency setup` rebuild, a host reimage, or a disk loss destroys the configuration, and reconstructing it is manual. The installed pack artifact is fungible (re-pullable from the hub); the config is not, and yet it has no preservation story of its own.
2. **Not portable.** There is no way to take the configuration from agency A and install it on agency B. This matters when a swarm moves a tenant between instances, when an operator rebuilds an agency on new hardware, or when a deployment is promoted from staging to production.
3. **Not coordinated across related instances.** A pack like `community-admin` depends on two connectors (`slack-interactivity`, `google-drive-admin`) that are each their own hub instance. These three instances share configuration semantics — Slack workspace ID, Google service account, admin user group handle — but today they are configured as three independent, uncoordinated instances.
4. **No standard schema mechanism for pack-level config.** Packs rely on `${…}` placeholder substitution in YAML, which covers secrets well but has no typed schema, no validation, and no prompt-generation for interactive configuration.

Operators who want their configuration to survive agency rebuilds currently have to snapshot `~/.agency/` manually and hope, or rewrite their configuration from memory. Swarm operators have no supported path for moving a configured pack between instances.

## Goals

1. Introduce a first-class **deployment**: a named, identified bundle that groups the hub instances belonging to a single configured pack, owns their shared configuration, holds references (not values) to their credentials, and is durable independent of the agency's local state.
2. Provide **export and import** verbs so a deployment can be moved between agency instances as a portable bundle, including shared config and bindings but never including credential values.
3. Define a **pack-level deployment schema** (`deployment_schema.yaml`) that packs ship to describe the typed configuration they need, enabling validation, interactive prompting, and schema-version migration.
4. Provide a **pluggable backend** for deployment storage so operators can point at filesystem (default), git, or an object store without packs caring.
5. Define a **swarm ownership model** — single-owner-at-a-time, with explicit handoff — that prevents split-brain updates.

## Non-Goals

- Changing the hub artifact format (`pack.yaml`, `connector.yaml`, `preset.yaml`) or the OCI distribution model.
- Changing the credential store. Deployments reference credentials by credstore key; values remain in credstore.
- Shared-backend multi-owner deployments (two agencies writing the same deployment simultaneously). Out of scope for v1; may be revisited.
- Migrating existing hub instances automatically. Deployments are a new opt-in layer; pre-existing instances keep working as-is.
- Replacing the hub install flow. `agency hub install <pack>` still pulls artifacts. Deployments are built on top.

## Design

### 1. Conceptual model

```
Hub artifact (pack)        →  Hub instance(s)         →  Hub deployment
(code, in the registry)       (activated component,       (configured bundle,
                               per-component)              durable + portable)
```

- **Hub artifact** is what the hub publishes: `pack.yaml` + its connector/preset/mission dependencies. Versioned. Distributed via OCI. Fungible.
- **Hub instance** is an activated hub component: a UUID, a human name, a config block, credential references. Lives in `~/.agency/hub/instances/<uuid>/`. Continues to work as today for components that don't opt into deployments.
- **Hub deployment** is a new container that groups the hub instances belonging to a single configured pack (the pack itself *and* any connectors/presets it depends on). Owns the shared config. Owns the audit trail of config changes. Portable as a unit.

A pack opts into deployments by shipping a `deployment_schema.yaml`. Packs that don't ship one continue to work via plain hub instances.

### 2. `deployment_schema.yaml`

A typed schema the pack ships alongside `pack.yaml`. It describes the configuration the pack needs at deployment time. Example:

```yaml
schema_version: 1

deployment:
  name: community-admin              # default deployment name (overridable at create time)
  description: >
    Administers a private, invite-gated Slack community with
    nomination/voting, conversation memory, and shared-document
    access management.

config:
  community_display_name:
    type: string
    description: Human-readable community name ("CSO Council", etc.)
    required: true

  intake_channel:
    type: string
    description: Slack channel where nominations are submitted and announced
    required: true

  admin_user_group:
    type: string
    description: Slack user group handle that identifies admins
    required: true
    pattern: "^@[a-zA-Z0-9_-]+$"

  rules_document_uri:
    type: string
    description: URI the bot loads at runtime to read community rules (file://, https://, drive://)
    required: true

  canonical_drive_folder_id:
    type: string
    description: Google Drive folder ID whose permissions the bot manages
    required: true

  managed_documents:
    type: list
    item_type: string
    description: Drive file/folder IDs the bot manages at boot (runtime-mutable after)
    default: []

  vote_threshold:
    type: int
    description: Plus-one reactions required to approve a nomination
    default: 2
    minimum: 1

  vote_window_hours:
    type: int
    description: Time window for collecting votes before a nomination expires
    default: 72
    minimum: 1

  annual_nomination_cap:
    type: int
    description: Nominations per member per calendar year (excluding admins)
    default: 2
    minimum: 0

  plus_one_emoji:
    type: list
    item_type: string
    default: [plus, "+1"]

  objection_emoji:
    type: list
    item_type: string
    default: [no_entry, "-1"]

  memory_distill_interval_hours:
    type: int
    default: 4
    minimum: 1

  observation_excluded_channels:
    type: list
    item_type: string
    default: []

  timezone:
    type: string
    default: "UTC"

credentials:
  slack_bot_token:
    description: Slack bot OAuth token (xoxb-...)
    credstore_scope: slack

  slack_signing_secret:
    description: Slack app signing secret
    credstore_scope: slack

  google_service_account:
    description: Google Workspace service account JSON for Drive API
    credstore_scope: google

instances:
  pack:
    component: community-admin         # the pack itself
    required: true

  connectors:
    - component: slack-events          # existing hub connector
      required: true
    - component: slack-interactivity
      required: true
    - component: google-drive-admin
      required: true
```

The schema is validated by the gateway before a deployment is created. Unknown fields are rejected (same discipline as the existing pack schema).

**Supported field grammar:**

- `type:` — one of `string`, `int`, `bool`, `list`, `object`
- `item_type:` — for `list`, the element type
- `required:` — bool, default false
- `required_if:` — string expression (e.g., `"slack_workspace_invite_method != manual"`) — required only when the condition holds
- `default:` — literal default value, applied when unset
- `pattern:` — regex applied to `string` values
- `enum:` — list of allowed values
- `minimum:`, `maximum:` — for `int`
- `description:` — human-readable help, used in interactive prompts
- `credstore_scope:` (credentials only) — credstore namespace hint

**Deployment config → child hub instance config flow.** Packs that declare `connector_config:` in their `deployment_schema.yaml` use it to declare how deployment-level config values map into each child hub instance's own config block. Two supported derivation forms:

```yaml
connector_config:
  google-drive-admin:
    # direct passthrough — deployment value is the instance value
    resource_whitelist:
      derived_from: [canonical_drive_folder_id, initial_managed_documents]
      # "derived_from" means: concatenate these deployment config values
      # into a list, wrapping each as {kind, drive_id} based on heuristic
      # (folder ids vs file ids) — the hub instance config handler for
      # the connector is responsible for this transform
    allow_whitelist_mutations: true

  slack-interactivity:
    interactivity_target_agent: admin-coordinator  # literal
```

Values may be (a) literals, (b) `derived_from: [...]` referring to one or more deployment config keys (the instance-side handler performs any transform), or (c) `${deployment.<key>}` placeholder strings that the primitive substitutes at `apply` time. On `agency hub deployment apply`, the primitive walks each entry, resolves the values, and writes them into the child instance's config before SIGHUPing.

Packs that don't declare `connector_config:` get default passthrough — every deployment config key that matches a child instance's config key name is copied through as-is.

### 3. Deployment object

```go
// internal/hub/deployments/deployment.go

type Deployment struct {
    ID              string              // UUID, immutable
    Name            string              // human name, unique within an agency
    Pack            PackRef             // { name, version, hub_source }
    SchemaVersion   int                 // from the pack's deployment_schema.yaml
    Config          map[string]any      // values matching the schema
    CredRefs        map[string]CredRef  // { key -> credstore ID }, never values
    Instances       []InstanceBinding   // the hub instance UUIDs this deployment owns
    Owner           OwnerRef            // { agency_id, claimed_at } — see swarm model
    CreatedAt       time.Time
    UpdatedAt       time.Time
    AuditLogPath    string              // relative path under the deployment root
}

type CredRef struct {
    Key           string // e.g. "slack_bot_token"
    CredstoreID   string // opaque credstore identifier on the owning agency
    ExportPolicy  string // "ref_only" (default) | "skip"
}

type InstanceBinding struct {
    Component string   // component name from the pack schema
    InstanceID string  // hub instance UUID
    Role string        // "pack" | "connector" | "preset"
}
```

Every mutation writes an entry to the deployment's audit log. The audit log is append-only and part of the portable bundle.

### 4. Storage: pluggable backend

A deployment's state lives at a backend path that the agency resolves via `DeploymentStore`:

```go
type DeploymentStore interface {
    Create(ctx context.Context, dep *Deployment) error
    Get(ctx context.Context, id string) (*Deployment, error)
    List(ctx context.Context) ([]*Deployment, error)
    Update(ctx context.Context, id string, mutator func(*Deployment) error) error
    Delete(ctx context.Context, id string) error
    Claim(ctx context.Context, id string, owner OwnerRef) error
    Release(ctx context.Context, id string) error
    Export(ctx context.Context, id string) (io.ReadCloser, error)
    Import(ctx context.Context, bundle io.Reader) (*Deployment, error)
}
```

**Default backend: filesystem.** Root at `$AGENCY_STATE/hub/deployments/<id>/`:

```
$AGENCY_STATE/hub/deployments/<uuid>/
├── deployment.yaml         # Deployment struct
├── config.yaml             # resolved config (values, not placeholders)
├── credrefs.yaml           # credstore keys only, never values
├── bindings.yaml           # hub instance UUIDs + roles
├── audit/
│   └── 2026-04-11T12-00-00Z.jsonl  # append-only audit entries
└── schema.yaml             # snapshot of the pack's deployment_schema.yaml at create time
```

The schema snapshot is deliberately copied into the deployment directory at create time so that schema evolution on the pack side cannot silently invalidate an existing deployment. Deployment migration (section 7) is the only path for moving to a new schema version.

**Other backends.** The interface is satisfied by implementations that write to git, object storage, or a KV store. v1 ships only the filesystem backend; the trait exists so operators can plug in others without a design change. Backend selection is configured via `~/.agency/config.yaml`:

```yaml
hub:
  deployment_backend: filesystem      # or "git" | "s3" | "custom"
  deployment_backend_config:
    root: $AGENCY_STATE/hub/deployments
```

### 5. CLI: `agency hub deployment`

```
agency hub deployment create <pack>
    [--name <name>]
    [--from-file <pack-config.yaml>]
    [--non-interactive]

agency hub deployment list
agency hub deployment show <name-or-id>
agency hub deployment configure <name-or-id>        # interactive or --from-file
agency hub deployment validate <name-or-id>         # schema-validate current config
agency hub deployment apply <name-or-id>            # write config into child hub instances, SIGHUP where needed
agency hub deployment export <name-or-id> <path>    # emit a portable bundle (.tar.gz)
agency hub deployment import <path> [--name <name>] # re-create on this agency
agency hub deployment claim <name-or-id>            # take ownership (swarm)
agency hub deployment release <name-or-id>          # release ownership (swarm)
agency hub deployment rebind <name-or-id>
    --pack <pack>@<version>                         # move to a new pack version
agency hub deployment destroy <name-or-id>
    [--keep-instances]                              # default: also destroys child hub instances
```

`create` is the canonical entry point. It:

1. Verifies the pack is installed in the hub registry.
2. Reads `deployment_schema.yaml` from the pack.
3. Prompts for each required field (or reads from `--from-file`).
4. Resolves credential references against the credstore (it does *not* read secret values).
5. Creates the child hub instances (pack + its declared dependencies) through the existing hub install/activate path.
6. Writes the deployment record and the schema snapshot to the backend.
7. Emits an audit entry.

`apply` is idempotent and used after any `configure`: it pushes the deployment's resolved config into its child hub instances' config blocks and SIGHUPs enforcers where the runtime needs to pick up changes.

### 6. Export and import

`export` produces a `.tar.gz` bundle:

```
deployment-<name>-<id>.tar.gz
├── deployment.yaml
├── config.yaml
├── credrefs.yaml            # keys only
├── bindings.yaml            # hub instance roles + component names (UUIDs are regenerated on import)
├── audit/                   # full audit history travels with the deployment
└── schema.yaml              # snapshot
```

The bundle is YAML-only and reproducible (no binary artifacts, no secrets, no agency-specific paths). It is **safe to commit to a private git repository**, which is the primary portability story.

`import` on a target agency:

1. Verifies the referenced pack+version is available in the target agency's hub registry (or prompts to install it).
2. Validates `config.yaml` against `schema.yaml`.
3. **Resolves credential references against the target agency's credstore.** If a referenced credstore key is missing, import prompts the operator to supply it. Credential values never travel in the bundle.
4. Creates fresh hub instance UUIDs on the target agency and wires them up according to `bindings.yaml`.
5. Writes a new deployment record with a new UUID but preserves the original `Name` and audit history (the audit log is appended to, not replaced — imports emit an `imported_from: <source_agency_id>` entry).
6. Returns the new deployment ID.

### 7. Schema evolution and rebinding

A pack may evolve its `deployment_schema.yaml` over time. The snapshot captured at create time is the source of truth for the deployment until it is explicitly rebound.

`agency hub deployment rebind <name> --pack <pack>@<version>` is the migration verb:

1. Loads the new pack's `deployment_schema.yaml`.
2. Runs a diff against the deployment's snapshotted schema.
3. For removed/renamed fields: prompts for a mapping or explicit drop.
4. For new required fields: prompts for values.
5. Validates the resulting config against the new schema.
6. On success, replaces the snapshot, applies the new config, and emits an audit entry.

If the new pack ships a `migrations/` directory with schema-version-to-schema-version transformers, `rebind` runs them in order. This is optional; simple additive changes don't need migrations.

### 8. Swarm ownership

v1 is single-owner with explicit handoff. Every deployment record has an `Owner` field:

```go
type OwnerRef struct {
    AgencyID    string
    AgencyName  string
    ClaimedAt   time.Time
    Heartbeat   time.Time
}
```

- On `create`, the creating agency is the owner.
- `claim` transfers ownership. It fails if another agency currently owns the deployment AND that owner's heartbeat is within the staleness window (default: 5 minutes). Stale ownership can be force-claimed with `--force`, which emits a prominent audit entry and notifies the operator.
- `release` clears ownership without destroying the deployment. A deployment with no owner is import-ready but not actively managed.
- Heartbeats are written whenever the owning agency runs `apply`, `configure`, or `validate` on the deployment.

Split-brain prevention is the backend's responsibility — the filesystem backend uses `flock(2)` on the deployment directory; other backends use whatever their native consistency primitive is. Imports and exports against a deployment owned by another agency are allowed (they're read-only snapshots); mutations are not.

### 9. Relationship to existing hub instances

- Packs that ship `deployment_schema.yaml` are deployment-enabled; their instances are always managed through a parent deployment.
- Packs that don't ship it continue to use bare hub instances. Existing installs are untouched.
- `agency hub deployment create` on a deployment-enabled pack is the **only** supported way to activate it; trying to call `agency hub <component> activate` directly on a deployment-enabled pack returns an error pointing the operator at `deployment create`.
- A deployment's child hub instances are still visible via `agency hub instances` (for debugging), but they're annotated with their owning deployment ID and cannot be destroyed directly — `agency hub deployment destroy` is the only way to remove them.

### 10. Audit trail

Every mutation — create, configure, apply, claim, release, rebind, destroy, import — writes a JSON-lines entry to `audit/`. Fields:

```json
{
  "ts": "2026-04-11T14:23:17Z",
  "actor": {"kind": "operator", "id": "geoff@local"},
  "action": "configure",
  "deployment_id": "...",
  "config_diff": { "intake_channel": {"from": "#invites-old", "to": "#invites"} },
  "result": "ok"
}
```

Audit entries are never edited or deleted. They travel with the deployment on export/import. ASK tenet 2 (every action leaves a trace) and tenet 7 (constraint history is immutable and complete) both apply here.

## Implementation plan

Staged so each phase is shippable independently:

1. **Phase 1: Deployment object, filesystem backend, CLI scaffold.** `create`, `list`, `show`, `destroy`. No export/import, no rebind, no swarm. Packs opt in by shipping `deployment_schema.yaml`.
2. **Phase 2: `configure`, `apply`, `validate`.** Schema-validated config edits, SIGHUP propagation to child instances.
3. **Phase 3: `export`, `import`.** Portable bundles. Credential re-resolution at import time.
4. **Phase 4: `rebind` + schema evolution.** Migration diffing and optional transformers.
5. **Phase 5: Swarm ownership.** Claim/release, heartbeats, force-claim, flock-based split-brain prevention on the filesystem backend.
6. **Phase 6: Alternate backends.** Git-backed backend ships first; S3 and custom follow as needed.

## Testing

- Unit tests for schema validation against a battery of good and bad `deployment_schema.yaml` files.
- Integration test: `create → configure → apply → export → destroy → import → apply` round-trip on the filesystem backend.
- Integration test: schema evolution via `rebind` with both additive and breaking changes.
- Integration test: concurrent `apply` attempts from two fake agency IDs — one must win, the other must be rejected with a clear error, no partial state written.
- End-to-end: a reference pack (stub) is deployment-enabled; the full CLI flow is exercised against a real agency instance.

## ASK alignment

- **Tenet 2 (every action leaves a trace):** every deployment mutation writes an audit entry; audit logs are append-only and portable.
- **Tenet 3 (mediation is complete):** deployment operations go through the gateway REST API; no direct state mutation.
- **Tenet 4 (least privilege):** deployments reference credentials by credstore key; values never leave credstore, never travel in bundles, never enter backend storage.
- **Tenet 5 (no blind trust):** every credential reference is explicit, visible in the bundle, and must be re-resolved on import against the target agency's credstore.
- **Tenet 7 (constraint history is immutable):** schema snapshots + audit logs together record every configuration state a deployment has operated under.
- **Tenet 15 (trust is earned and monitored continuously):** schema version bumps do not automatically apply; rebind is an explicit operator action.
- **Tenet 25 (identity mutations are auditable and recoverable):** a deployment's config history is the recovery path — rollback is `rebind` to a prior schema snapshot or re-import of an older bundle.

## Open questions

1. **Audit log rotation.** JSONL files grow unbounded. Do we rotate by size, by time, or never? Proposed: never for v1, revisit if files exceed 10 MB in practice.
2. **Bundle signing.** Should exported bundles be signable (cosign, minisign) so operators can verify a bundle came from a known agency before importing? Defer to v2.
3. **Deployment-enabled version of existing packs.** When `security-ops` ships its first `deployment_schema.yaml`, do existing installations auto-migrate? Proposed: no — operators must explicitly run a one-time migration command.
