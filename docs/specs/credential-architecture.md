---
description: "80% of credential management (encrypted storage, audit, rotation, expiry,"
status: "Phase 1 implemented, Phase 2 in progress"
---

# Credential Architecture

Last updated: 2026-04-01

## Status: Phase 1 implemented, Phase 2 in progress

## Design Philosophy

80% of credential management (encrypted storage, audit, rotation, expiry,
backup) is solved infrastructure. Vault, AWS Secrets Manager, Azure Key
Vault, GCP Secret Manager, and SOPS all handle it well. We don't compete
with them — we integrate with them.

The 20% that's unique to Agency — scope routing, preset validation,
enforcer integration, X-Agency-Service dispatch — is where we invest.
The built-in backend is MVP: good enough for single-host and small teams,
designed to be swapped out for Vault or cloud secret managers when
operators need enterprise-grade secret management.

**Build order priority:**
1. The `SecretBackend` interface (the swap boundary) — get this right
2. Agency-specific routing layer (scope resolution, preset validation) — invest here
3. Built-in encrypted file backend — MVP, works out of the box
4. Vault/cloud backends — clean swap-in via the interface

## Problem Statement

The credential system grew organically across 8+ files with 5 write paths,
2 key stores, 4 swap handler types, and inconsistent patterns (append-only
vs upsert, single vs double dash prefix). Credentials work but the system
is fragile: any new integration requires touching multiple files in the
right order, and mismatches between generated config files cause silent
failures.

## Design Requirements

1. **Secure storage** — credentials at rest are protected, access is audited
2. **Idempotent operations** — writing the same credential twice produces
   the same state, no duplicates, no corruption
3. **Single code path** — one function to store, one to retrieve, one to
   delete. No parallel implementations
4. **Protocol flexibility** — support static API keys, JWT exchange, OAuth,
   GitHub App tokens, and arbitrary header/body injection without changing
   the core storage model
5. **Flexible scoping** — a credential can be platform-wide, per-team,
   per-agent, or per-service. Scoping is metadata on the credential, not
   a separate system
6. **Agent security** — agents never see real credentials. The credential
   resolution boundary is in shared infrastructure (egress), not in
   per-agent containers (enforcer)

## Architecture

### The Credential Store

One logical store, one interface, one implementation (with a Vault-ready
abstraction for future backends).

```
┌─────────────────────────────────────────────────┐
│                 Credential Store                 │
│                                                  │
│  Interface:                                      │
│    Put(name, value, metadata) → error            │
│    Get(name) → value, metadata, error            │
│    Delete(name) → error                          │
│    List(filter) → []CredentialEntry              │
│                                                  │
│  Metadata per credential:                        │
│    kind: provider | service | gateway | internal │
│    scope: platform | agent:{name} | team:{name}  │
│    protocol: api-key | jwt-exchange | github-app │
│    protocol_config: {header, format, token_url,  │
│                      token_params, ...}          │
│    source: operator | hub:{component} | system   │
│    created_at, rotated_at                        │
│                                                  │
│  Backend (v1): encrypted file at                 │
│    ~/.agency/credentials/store.enc               │
│  Backend (v2): HashiCorp Vault / SOPS / age      │
│                                                  │
│  Access: gateway process only. No bind mount     │
│    to any container. Egress receives resolved     │
│    credentials via in-memory IPC or tmpfs.        │
└─────────────────────────────────────────────────┘
```

### Current State

**Credentials live in one store + one routing table:**
```
~/.agency/credentials/store.enc         ← ALL credentials + metadata (AES-256-GCM)
~/.agency/infrastructure/credential-swaps.yaml  ← generated (derived from store)
~/.agency/agents/*/state/enforcer-auth/ ← scoped tokens (unchanged, ephemeral)
```

The `.service-keys.env` file has been eliminated. The `.env` file still exists for
legacy non-secret config values but config is moving to `config.yaml`. All credentials
live in the encrypted store with metadata that drives routing.

### Credential Entry Schema

```yaml
name: LC_API_KEY_DETECTION_ENGINEER
value: "<encrypted-api-key>"  # encrypted at rest
metadata:
  kind: service
  scope: agent:detection-engineer
  service: limacharlie-detection-engineer
  group: limacharlie                # shared JWT exchange config
  protocol: jwt-exchange
  protocol_config:
    token_url: "https://jwt.limacharlie.io"
    token_params:
      oid: "${LC_ORG_ID}"
      secret: "${credential}"
    token_response_field: jwt
    token_ttl_seconds: 3000
    inject_header: Authorization
    inject_format: "Bearer {token}"
  source: operator
  requires:
    - LC_ORG_ID                     # dependency: org ID must be set
  external_scopes:                  # what this key can do in LC
    - insight.det.get
    - sensor.get
    - dr.list
    - dr.set
    - dr.del
    - fp.ctrl
    - sensor.task
    - insight.evt.get
    - ext.request
  created_at: "2026-03-30T18:00:00Z"
  rotated_at: "2026-03-30T18:00:00Z"
```

```yaml
name: PROVIDER_A_API_KEY
value: "sk-provider-a-..."  # encrypted at rest
metadata:
  kind: provider
  scope: platform
  protocol: api-key
  protocol_config:
    header: Authorization
    format: "Bearer {key}"
    domains:
      - provider-a.example.com
  source: operator
  expires_at: "2027-03-15T10:00:00Z"  # optional — API keys with expiry
  created_at: "2026-03-15T10:00:00Z"
```

```yaml
name: GATEWAY_TOKEN
value: "a3f8..."  # encrypted at rest
metadata:
  kind: gateway
  scope: platform
  protocol: bearer
  source: system
  created_at: "2026-03-15T09:00:00Z"
```

### Single Code Path: `credstore` Package

### Layer 1: SecretBackend Interface (swappable)

This is the boundary. Built-in encrypted file for day one. Swap in
Vault, AWS Secrets Manager, Azure Key Vault, or GCP Secret Manager
without changing any Agency code above this layer.

```go
package credstore

// SecretBackend is the swap boundary. Implement this for Vault, AWS SM,
// Azure KV, GCP SM, or any other secret manager. The built-in file
// backend implements it with AES-256-GCM encryption.
type SecretBackend interface {
    Put(name string, value string, metadata map[string]string) error
    Get(name string) (value string, metadata map[string]string, err error)
    Delete(name string) error
    List() ([]SecretRef, error) // returns names + metadata, never values
}

// SecretRef is a reference to a secret without the value.
type SecretRef struct {
    Name     string
    Metadata map[string]string
}
```

That's it. Four methods. The backend stores bytes and flat key-value
metadata. It knows nothing about Agency concepts (scopes, protocols,
services). Vault's KV v2 maps to this directly. AWS Secrets Manager
maps to this directly. A flat encrypted file maps to this directly.

### Layer 2: Credential Store (Agency's value — invest here)

This is where Agency-specific intelligence lives. It uses the backend
for storage but adds scope routing, protocol handling, preset validation,
group inheritance, dependency checking, and audit.

```go
// Entry is the rich Agency credential with typed metadata.
type Entry struct {
    Name     string   `json:"name"`
    Value    string   `json:"value"`      // plaintext in memory, encrypted at rest
    Metadata Metadata `json:"metadata"`
}

type Metadata struct {
    Kind           string            `json:"kind"`            // provider, service, gateway, internal
    Scope          string            `json:"scope"`           // platform, agent:{name}, team:{name}
    Service        string            `json:"service"`         // service definition name (for service kind)
    Group          string            `json:"group,omitempty"` // credential group (shared protocol config)
    Protocol       string            `json:"protocol"`        // api-key, jwt-exchange, github-app, oauth2, bearer
    ProtocolConfig map[string]any    `json:"protocol_config"` // protocol-specific fields
    Source         string            `json:"source"`          // operator, hub:{component}, system
    ExpiresAt      string            `json:"expires_at,omitempty"` // optional expiry (ISO 8601)
    Requires       []string          `json:"requires,omitempty"`   // config dependencies (e.g., "LC_ORG_ID")
    ExternalScopes []string          `json:"external_scopes,omitempty"` // declared scopes on the external key
    CreatedAt      string            `json:"created_at"`
    RotatedAt      string            `json:"rotated_at"`
}

// Store is the Agency credential manager. Uses SecretBackend for storage,
// adds scope routing, validation, audit, and protocol intelligence.
type Store struct {
    backend SecretBackend
    audit   AuditWriter
    presets PresetReader  // reads agent preset scope declarations
    config  ConfigReader  // reads platform config (for dependency validation)
}

// Core operations — all go through here, single code path
func (s *Store) Put(entry Entry) error          // validate, encrypt via backend, audit, regenerate routing
func (s *Store) Get(name string) (*Entry, error) // decrypt via backend, audit
func (s *Store) Delete(name string) error        // backend delete, audit, regenerate routing
func (s *Store) List(filter Filter) ([]Entry, error) // backend list, filter by Agency metadata
func (s *Store) Rotate(name, newValue string) error  // Put with preserved metadata + rotated_at

// Agency-specific routing — THIS IS WHERE WE INVEST
func (s *Store) ForService(serviceName string) (*Entry, error)            // scope-aware lookup
func (s *Store) ForAgent(agentName, serviceName string) (*Entry, error)   // agent > team > platform resolution
func (s *Store) ForDomain(domain string) (*Entry, error)                  // LLM provider lookup

// Validation — catches misconfiguration at write time, not at first failure
func (s *Store) ValidateScopes(entry Entry) []Warning                     // cross-ref external_scopes vs preset
func (s *Store) ValidateDependencies(entry Entry) []Warning               // check requires are set
func (s *Store) ValidateProtocolConfig(entry Entry) error                 // token_url domain check, sandbox ${} expansion

// Health
func (s *Store) Test(name string) (*TestResult, error)                    // end-to-end credential verification
func (s *Store) Expiring(within time.Duration) ([]Entry, error)           // expiry check

// Groups
func (s *Store) ResolveGroup(entry Entry) Entry                           // merge group config into entry
func (s *Store) GroupMembers(groupName string) ([]Entry, error)           // list entries in a group

// Routing table generation
func (s *Store) GenerateSwapConfig() ([]byte, error)                      // replaces hub.GenerateSwapConfig

type TestResult struct {
    OK      bool   `json:"ok"`
    Status  int    `json:"status,omitempty"`
    Message string `json:"message,omitempty"`
    Latency int    `json:"latency_ms"`
}

type Filter struct {
    Kind    string // empty = all
    Scope   string // empty = all, "platform", "agent:detection-engineer"
    Service string // empty = all
    Group   string // empty = all
}

type Warning struct {
    Field   string
    Message string
}
```

### Built-in Backend: Encrypted File (MVP)

Good enough for single-host, small team, no external dependencies.
Operators who need more swap it out.

```go
// FileBackend implements SecretBackend with AES-256-GCM encryption.
// One JSON file, values individually encrypted, metadata in plaintext.
type FileBackend struct {
    path    string // ~/.agency/credentials/store.enc
    keyPath string // ~/.agency/credentials/.key
}
```

**What it does:** Put/Get/Delete/List with file locking and atomic writes.
**What it doesn't do:** access control, audit, versioning, HA, auto-rotation.
Those belong in the Store layer (Agency-specific) or in Vault (external).

### Backend Implementations (swap-in)

```go
// Vault
type VaultBackend struct {
    client *vault.Client
    mount  string // "secret/agency/"
}

// AWS Secrets Manager
type AWSSecretsBackend struct {
    client *secretsmanager.Client
    prefix string // "agency/"
}

// Azure Key Vault
type AzureKeyVaultBackend struct {
    client *azsecrets.Client
}

// GCP Secret Manager
type GCPSecretBackend struct {
    client *secretmanager.Client
    project string
}
```

Each is ~100 lines mapping the four interface methods to the provider's
SDK. No Agency logic — just storage. Configuration via `~/.agency/config.yaml`:

```yaml
credentials:
  backend: file  # file | vault | aws | azure | gcp
  # Backend-specific config:
  vault:
    address: https://vault.example.com
    mount: secret/agency
    auth_method: token  # or kubernetes, approle, etc.
  aws:
    region: us-west-2
    prefix: agency/
  # etc.
```

### How Credentials Flow

```
Operator: agency creds set --name LC_KEY --kind service \
          --scope agent:detection-engineer \
          --service limacharlie-detection-engineer \
          --protocol jwt-exchange \
          --token-url https://jwt.limacharlie.io \
          --value <key>

    │
    ▼
Gateway: credstore.Put(entry)
    │  - Validates entry
    │  - Encrypts value
    │  - Writes to store.enc
    │  - Regenerates credential-swaps.yaml
    │  - SIGHUPs egress to reload
    │  - Emits credential_stored audit event
    │
    ▼
Egress (on reload or startup):
    │  - Reads credential-swaps.yaml (routing table)
    │  - For each swap entry, resolves credential from gateway
    │    via SocketKeyResolver (Unix socket at ~/.agency/run/gateway.sock)
    │  - Caches resolved credentials in-memory
    │  - On request: matches domain or X-Agency-Service → injects credential
    │
    ▼
External API: receives properly authenticated request
```

### Credential Resolution at the Egress

The egress resolves credentials from the gateway via the `SocketKeyResolver`,
which calls the gateway's internal resolve API over the Unix socket at
`~/.agency/run/gateway.sock`:

```
GET /internal/credentials/resolve?name=LC_API_KEY_DETECTION_ENGINEER
→ {"value": "<encrypted>", "protocol": "jwt-exchange", "protocol_config": {...}}
```

The gateway socket is mounted into the egress container. The egress calls
the resolver at startup and on SIGHUP to populate its in-memory credential
cache. Real credentials never touch the filesystem inside any container.

### Scope Resolution

When the enforcer passes `X-Agency-Service: limacharlie-detection-engineer`
to egress:

1. Egress looks up the swap config by service name
2. Swap config has `key_ref: LC_API_KEY_DETECTION_ENGINEER`
3. Egress resolves credential from in-memory cache
4. If `protocol: jwt-exchange`, egress does the token exchange
5. Egress injects the final credential (JWT) into the request

For platform-scope credentials, for example `PROVIDER_A_API_KEY`:
1. Egress matches by domain (`provider-a.example.com`)
2. Resolves credential from cache
3. Injects using the provider descriptor's credential protocol

For agent-scoped credentials with the same service name:
1. The credential store has multiple entries for the same service,
   differentiated by scope
2. The swap config generator creates per-agent entries
   (e.g., `limacharlie-detection-engineer` → `LC_API_KEY_DETECTION_ENGINEER`)
3. Each agent's enforcer sends the agent-specific service name via
   `X-Agency-Service`

### Protocol Handlers

The egress swap handlers remain the execution layer, but their
configuration comes from `protocol_config` in the credential metadata
instead of hand-written YAML:

```python
# Dispatch by protocol field from credential metadata
PROTOCOL_HANDLERS = {
    "api-key":      handle_api_key,
    "jwt-exchange": handle_jwt_exchange,
    "github-app":   handle_github_app,
    "oauth2":       handle_oauth2,        # future
    "mtls":         handle_mtls,          # future
}
```

Adding a new protocol = adding a handler function + a protocol name.
No changes to storage, routing, or the credential store interface.

### Idempotency

`credstore.Put(entry)` is idempotent:
- If name exists with same value and metadata: no-op
- If name exists with different value: update value, set rotated_at
- If name exists with different metadata: update metadata
- If name doesn't exist: create
- Concurrent writes are serialized (file lock or Vault transactions)

Derived files (`credential-swaps.yaml`) are regenerated after every
Put/Delete. Regeneration is also idempotent — same store state produces
same output.

### Encryption at Rest

v1 backend: AES-256-GCM encryption using a key derived from a passphrase
stored in `~/.agency/credentials/.key` (mode 0400, generated on init).
The store file is a JSON array of entries where each `value` field is
individually encrypted. Metadata is plaintext (needed for routing).

v2 backend: age/SOPS encryption with operator's age key, or HashiCorp
Vault with transit backend.

### Credential Health Checks

`credstore.Test(name)` validates a credential end-to-end:

1. Resolve the credential from the store
2. If protocol is `jwt-exchange`: attempt token exchange
3. Make a lightweight authenticated request to the external API
   (e.g., `GET /v1/orgs/{oid}` for LC, `GET /v1/models` for Anthropic)
4. Return pass/fail with HTTP status, latency, and error detail

Test endpoints are defined per-protocol in the protocol config:
```yaml
protocol_config:
  test_endpoint: "/v1/orgs/${LC_ORG_ID}"  # GET this to verify
  test_expected_status: 200
```

`agency creds test LC_KEY` calls this. `agency admin doctor` tests
all credentials automatically and flags failures.

### Expiry Tracking

The optional `expires_at` field enables:
- `agency admin doctor` warns on credentials expiring within 7 days
- Platform event `credential_expiring` fires at 7d, 3d, and 1d before expiry
- `credstore.Expiring(7 * 24 * time.Hour)` returns all soon-to-expire entries
- Expired credentials are flagged in `agency creds list` output

For JWT exchange, the exchanged token's TTL is tracked separately from the
API key expiry. The API key might be permanent but the JWT expires every
3000 seconds — that's handled by the protocol handler's cache, not by the
store's `expires_at`.

### Credential Dependencies

The `requires` field lists config values that must be set for the credential
to work. `credstore.Put()` validates at write time:

```
$ agency creds set --name LC_KEY --requires LC_ORG_ID ...
Error: dependency LC_ORG_ID is not set. Set it with:
  agency config set LC_ORG_ID <value>
```

Dependencies are checked again by `agency admin doctor` and on agent start.
A credential with unmet dependencies is stored but flagged as `incomplete`.

### Credential Groups

Multiple credentials that share the same protocol config can reference a
group. The group defines shared config; individual entries override or
extend it.

```yaml
# Group definition (stored as a special entry with kind: group)
name: limacharlie
metadata:
  kind: group
  protocol: jwt-exchange
  protocol_config:
    token_url: "https://jwt.limacharlie.io"
    token_params:
      oid: "${LC_ORG_ID}"
      secret: "${credential}"
    token_response_field: jwt
    token_ttl_seconds: 3000
    inject_header: Authorization
    inject_format: "Bearer {token}"
  requires:
    - LC_ORG_ID
```

Individual credentials reference the group:
```yaml
name: LC_API_KEY_DETECTION_TUNER
metadata:
  kind: service
  scope: agent:detection-tuner
  service: limacharlie-detection-tuner
  group: limacharlie          # inherits protocol + protocol_config + requires
  external_scopes: [insight.det.get, sensor.get, dr.list, fp.ctrl, insight.stat]
```

At resolution time, the store merges group config with entry config
(entry wins on conflicts). This eliminates duplicating JWT exchange
parameters across 5 LC keys.

### Hot Rotation (Phase 2)

With the internal credential resolution API, key rotation is zero-downtime:

1. `agency creds rotate LC_KEY --value <new-key>`
2. Store updates value, sets `rotated_at`
3. Gateway pushes invalidation event to egress via WebSocket
4. Egress drops cached credential, re-fetches on next request
5. No agent restart, no infra reload, no SIGHUP

For JWT exchange: the old JWT stays valid until its TTL expires. The new
API key is used for the next token exchange. Overlap period = JWT TTL.

### Import/Export for Disaster Recovery

```bash
# Export all credentials (encrypted with store key)
agency creds export > credentials-backup.enc

# Export with a specific age recipient (for off-host backup)
agency creds export --recipient age1... > credentials-backup.age

# Import (merges with existing store, idempotent)
agency creds import credentials-backup.enc

# Import with conflict resolution
agency creds import --on-conflict=skip credentials-backup.enc
agency creds import --on-conflict=overwrite credentials-backup.enc
```

The export format includes all metadata. Import is idempotent — importing
the same backup twice produces the same store state.

### Least-Privilege Validation at Store Time

When storing a credential with `scope: agent:detection-tuner`, the store
cross-references:
1. The agent's preset scope declarations (`scopes.required` + `scopes.optional`)
2. The credential's `external_scopes` field

```
$ agency creds set --name LC_KEY --scope agent:detection-tuner \
    --external-scopes "insight.det.get,sensor.get,dr.list,dr.set,fp.ctrl"

Warning: credential has scopes beyond agent's declared needs:
  detection-tuner declares: insight.det.get, sensor.get, dr.list, fp.ctrl, insight.stat
  credential has:          insight.det.get, sensor.get, dr.list, dr.set, fp.ctrl
  excess scopes:           dr.set
  missing declared scopes: insight.stat

Stored with warning. Consider creating a more precisely scoped key.
```

This catches misconfiguration at write time, not at first failed API call.

### Audit: Reads and Writes

Every store operation emits an audit event:

| Operation | Event Type | Severity | Logged Fields |
|-----------|-----------|----------|---------------|
| Put (create) | `credential_created` | info | name, kind, scope, source |
| Put (update metadata) | `credential_updated` | info | name, changed_fields |
| Rotate | `credential_rotated` | info | name, scope, old_rotated_at |
| Delete | `credential_deleted` | warn | name, scope |
| List | `credential_listed` | info | filter, result_count |
| Show | `credential_shown` | info | name |
| Show --show-value | `credential_value_revealed` | **high** | name, operator |
| Get (internal resolve) | `credential_resolved` | debug | name, requester (egress/gateway) |
| Resolve (auth failure) | `credential_resolve_denied` | warn | requester_ip, attempted_name |
| Test | `credential_tested` | info | name, result (pass/fail), latency |
| Export | `credential_exported` | warn | count, recipient_key_id |
| Import | `credential_imported` | info | count, conflicts |
| Group change | `credential_group_changed` | warn | group, changed_fields, affected_count |

Values are NEVER logged. `credential_value_revealed` is high-severity —
operator chose to view a plaintext credential value.

The `credential_resolved` events enable rate anomaly detection — if
egress resolves a credential 10,000 times in an hour when normal is
100, that's worth investigating. Operator authority monitoring (Tenet 10):
anomaly detection should also flag unusual operator behavior — bulk
exports, late-night rotations, or `--show-value` on credentials the
operator doesn't normally manage.

### Migration (Complete)

Migration from the old file-based system is complete:

1. `.service-keys.env` has been eliminated. All credentials are in the
   encrypted store.
2. `.env` still exists for legacy non-secret config values. Config values
   are moving to `config.yaml` `config:` section.
3. `credential-swaps.yaml` is generated from the credential store.
4. New credentials are added exclusively via `agency creds set`.

### CLI Surface

The CLI command is `agency creds` (aliases: `credential`, `credentials`).

```bash
# Store a credential
agency creds set --name SLACK_KEY --value xoxb-... \
  --kind service --scope platform --protocol api-key \
  --header Authorization --format "Bearer {key}"

# Store with JWT exchange (inherits group config)
agency creds set --name LC_KEY --value <api-key> \
  --kind service --scope agent:detection-engineer \
  --service limacharlie-detection-engineer \
  --group limacharlie \
  --external-scopes "insight.det.get,dr.set,fp.ctrl"

# Store with expiry
agency creds set --name TRIAL_KEY --value <key> \
  --kind provider --expires 2026-06-30

# List credentials (values redacted)
agency creds list
agency creds list --kind service --scope agent:detection-engineer
agency creds list --group limacharlie
agency creds list --expiring 7d   # expiring within 7 days

# Show credential metadata (value redacted unless --show-value)
agency creds show LC_KEY

# Delete
agency creds delete LC_KEY

# Rotate (update value, keep metadata, zero-downtime in Phase 2)
agency creds rotate LC_KEY --value <new-key>

# Verify credential works against external API
agency creds test LC_KEY
agency creds test --all           # test every credential

# Import/export for backup (Phase 3)
agency creds export > backup.enc
agency creds import backup.enc

# Create a credential group (shared protocol config)
agency creds group create limacharlie \
  --protocol jwt-exchange \
  --token-url https://jwt.limacharlie.io \
  --requires LC_ORG_ID

# Set a config dependency
agency config set LC_ORG_ID <your-org-id>
```

### Audit

See "Audit: Reads and Writes" above for the full event table.
Values are NEVER logged — only name, kind, scope, source, and timestamp.

### Credential History (Tenet 7)

The store does not preserve old credential values — `Rotate` overwrites.
This is a deliberate security tradeoff: keeping old secrets increases
breach exposure. The forensic question "what credential was agent X using
at time T?" is answered indirectly:

- `credential_rotated` audit event records when a rotation happened
- `credential_resolved` audit events (planned) record when egress served
  a credential to a specific agent
- Temporal reasoning: if rotation happened at T1 and the agent used the
  credential at T0 < T1, it was the pre-rotation value

This provides temporal bounds without storing old secrets. If stricter
forensic needs arise, a future version can add a time-bounded encrypted
value history (e.g., keep previous value for 30 days, then destroy).

### Halt and Quarantine Interaction (Tenets 8, 16)

**On agent halt (supervised/immediate/emergency):**
- Credentials are NOT revoked. They remain in the store unchanged.
- Network severance (enforcer container stopped) prevents the agent from
  making new requests, which effectively stops credential use.
- On resume, the agent's enforcer restarts and credential access resumes
  with no store changes needed.

**On agent quarantine (Tenet 16):**
- Network severance + process termination + filesystem freeze happen
  simultaneously. No new requests can reach egress.
- Credentials remain in the store for forensic review.
- If a quarantined agent shares team-scoped credentials with
  non-quarantined agents, the credential remains active for the team.
  Quarantine is agent-level isolation, not credential revocation.
  If the operator determines the credential itself is compromised,
  they should rotate it independently: `agency creds rotate <name>`.

### Authority and Key Recovery (Tenet 14)

**Encryption key escrow:**
- On `agency setup`, the store encryption key is generated and written to
  `~/.agency/credentials/.key` (mode 0400).
- The init process also outputs: `Credential store key: <base64>. Save
  this in your password manager. It cannot be recovered if lost.`
- `agency creds key show` displays the key for backup purposes
  (emits `credential_value_revealed` audit event).
- `agency creds key set <base64>` restores a key from backup.

**Multi-operator:**
- v1 uses a single encryption key. All operators who can `ssh` to the
  host can manage credentials.
- v2 (Vault backend) supports per-operator auth with policies.
- The export/import mechanism (Phase 3) provides a secondary recovery
  path: if the store is lost but an export exists, import restores it.

### Protocol Config Validation (Tenet 17)

The `protocol_config` map can contain URLs and parameters. To prevent
a malicious hub component from exfiltrating credentials:

- `token_url` is validated against the service's declared
  `egress_domains`. A JWT exchange URL that doesn't match the service's
  domain allowlist is rejected at write time.
- `${...}` placeholder expansion is limited to a fixed allowlist:
  `${credential}` (the credential value) and `${config:VARNAME}` (from
  `~/.agency/config/`). No env var access, no credential cross-reference.
- Hub-sourced credentials (`source: hub:*`) are flagged in
  `agency admin doctor` for operator review.

### Scope Resolution Precedence

When multiple credentials match a request, the most specific scope wins:

```
agent:{name} > team:{name} > platform
```

If `LC_API_KEY_DETECTION_ENGINEER` (scope: `agent:detection-engineer`)
and `LC_API_KEY_GLOBAL` (scope: `platform`) both exist, the agent-scoped
credential is used for detection-engineer and the platform credential is
used for any agent without a specific entry.

### Write Atomicity and Crash Recovery (Tenet 6)

Store writes use the atomic pattern:
1. Write to `store.enc.tmp`
2. `fsync` the file
3. Rename `store.enc.tmp` → `store.enc` (atomic on POSIX)

If the gateway crashes between writing the store and regenerating
`credential-swaps.yaml`, the derived file is stale. On gateway startup,
derived files are unconditionally regenerated from the store.

File locking uses `flock(2)` (advisory, auto-released on process death)
— not a `.lock` file that could be left stale after a crash.

### Agent Deletion and Credential Cleanup (Tenet 13)

`agency delete <agent>` does NOT delete the agent's scoped credentials.
Credentials persist in the store. This is intentional:
- Forensic review may need to see what credentials the agent had
- A new agent with the same name inherits the credentials (continuity)
- Operator can explicitly clean up: `agency creds delete --scope agent:<name>`

`agency creds list --scope agent:<name>` shows orphaned credentials
for deleted agents. `agency admin doctor` flags them.

### ASK Tenet Alignment

| Tenet | How this spec addresses it |
|-------|---------------------------|
| 1 (External constraints) | Credential store is gateway-only. No agent can read, write, or influence it. Hub-triggered puts are operator-gated (activation requires operator action). |
| 2 (Audit trail) | Full audit table: create, update, rotate, delete, list, show, show-value (high severity), resolve, resolve-denied, test, export, import, group change. |
| 3 (Complete mediation) | All credential resolution via SocketKeyResolver (gateway Unix socket). credential-swaps.yaml contains routing metadata only (no secrets). |
| 4 (Least privilege) | Scope field + external_scopes validation + agent preset cross-reference. Scope resolution: agent > team > platform. |
| 5 (No blind trust) | Credential metadata makes trust relationships explicit. Dependencies, scopes, source, expiry all visible. |
| 6 (Atomic changes) | Atomic write pattern (write-tmp-fsync-rename). File lock via flock(2). Derived files regenerated on startup. |
| 7 (Constraint history) | Deliberate tradeoff: old values destroyed for security. Temporal bounds via audit events. Rationale documented. |
| 8 (Halt interaction) | Network severance stops credential use. Credentials preserved for resume. No revocation on halt. |
| 14 (Authority recovery) | Encryption key escrow via init output + `credential key show`. Export/import for DR. |
| 15 (No self-elevation) | No agent API can call credstore.Put. Only operator CLI/REST. |
| 16 (Quarantine) | Network cut is sufficient. Shared team credentials remain active for non-quarantined agents. Operator rotates if credential itself compromised. |
| 17 (Verified principals) | token_url validated against egress domains. ${} expansion sandboxed to fixed allowlist. Hub credentials flagged for review. |
| 25 (Identity mutations) | Audit events for all mutations. Group changes log affected_count. Export provides recovery checkpoint. |

## Implementation Phases

### Phase 1: Interface + Routing Layer + File Backend -- DONE

Delivered: the interface, Agency-specific routing, and working file backend.
Operators use `agency creds` commands for all credential management.

**SecretBackend interface (the swap boundary):** Done.
- `SecretBackend` interface: Put/Get/Delete/List — 4 methods
- `FileBackend` implementation: AES-256-GCM, atomic writes, flock

**Credential Store (Agency-specific):** Done.
- `Entry` and `Metadata` types with all fields
- `Put/Get/Delete/List/Rotate` — single code path, idempotent
- `ForService/ForAgent/ForDomain` — scope-aware resolution (agent > team > platform)
- `ValidateScopes` — cross-reference external_scopes vs agent preset declarations
- `ValidateDependencies` — check requires are set at write time
- `ValidateProtocolConfig` — token_url domain check, sandboxed ${} expansion
- `ResolveGroup` — merge group config into individual entries
- `GenerateSwapConfig` — replaces `hub.GenerateSwapConfig`
- Audit events for all operations

**CLI + REST + MCP:** Done.
- `agency creds set/list/show/delete/rotate/test`
- `agency creds group create`
- REST endpoints for all operations
- MCP tools for all operations
- `agency admin doctor` integration (expiry, dependencies, health checks)

**Migration:** Done.
- `.service-keys.env` eliminated. Credentials in encrypted store.
- `.env` retained for legacy config values (moving to `config.yaml`).

### Phase 2: Socket-Based Resolution + Hot Rotation -- IN PROGRESS

Focus: egress resolves credentials via gateway Unix socket. Enables
zero-downtime rotation.

- Internal credential resolution API (`GET /internal/credentials/resolve`) — Done (Unix socket at `~/.agency/run/gateway.sock`)
- SocketKeyResolver in egress — Done
- `.service-keys.env` bind mounts removed from egress — Done
- `credential-swaps.yaml` generated from credential store — Done
- Hot rotation: gateway pushes invalidation to egress via WebSocket — Not started
- Zero-downtime key rotation (no agent restart needed) — Not started
- `credential_resolved` audit events for read tracking — Not started
- Rate anomaly detection on resolve events — Not started

### Phase 3: External Backends + DR

Focus: let operators swap in their preferred secret manager.

- `VaultBackend` implementation (~100 lines)
- `AWSSecretsBackend` implementation (~100 lines)
- `AzureKeyVaultBackend` implementation (~100 lines)
- `GCPSecretBackend` implementation (~100 lines)
- Backend selection via `~/.agency/config.yaml`
- `agency creds export/import` for disaster recovery
- OAuth2 protocol handler
- mTLS protocol handler
- Credential auto-rotation policies

## What Has Been Eliminated

- `~/.agency/infrastructure/.service-keys.env` — deleted, merged into store
- `~/.agency/secrets/.service-keys.env` — deleted, merged into store
- `~/.agency/secrets/jwt-swap.yaml` — merged into store as protocol_config
- `~/.agency/.capability-keys.env` — deleted, merged into store
- 5 write paths to `.service-keys.env` → 1 `credstore.Put`
- 4 read paths for env files → 1 `credstore.Get`
- Credential migration logic (removed)

**Still exists (moving to config.yaml):**
- `~/.agency/.env` — retained for legacy non-secret config values. Config values
  moving to `config.yaml` `config:` section.

**Handler files (current):**
- `handlers_credentials.go` — credential CRUD REST API
- `handlers_hub.go`, `handlers_agent.go`, `handlers_capabilities.go`,
  `handlers_grants.go`, `handlers_infra.go`, `handlers_admin.go` — other handlers
- `manifest.go` — single manifest generator (`generateAgentManifest`)
- `internal/pkg/envfile/` — single env file reader/writer (for remaining `.env` usage)
