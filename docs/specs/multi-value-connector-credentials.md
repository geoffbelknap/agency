---
description: "Platform-level support for connector credentials that accept multiple values, expanding a single connector instance i..."
---

# Multi-Value Connector Credentials

Platform-level support for connector credentials that accept multiple values, expanding a single connector instance into independent virtual poll targets.

## Problem

Some external APIs scope data by an account-level identifier (e.g., NextDNS profile ID, Slack workspace ID). Operators managing multiple scopes must install and activate N duplicate connector instances that differ only in a single credential value. This creates unnecessary setup overhead and clutters `hub instances` output.

## Design

### Credential Model Extension

Add two optional fields to `ConnectorCredential` (Python Pydantic model and Go struct mirror):

```python
class ConnectorCredential(BaseModel):
    name: str
    description: str = ""
    type: Literal["secret", "config"] = "secret"
    scope: Literal["service-grant", "env-var", "file"] = "service-grant"
    grant_name: Optional[str] = None
    setup_url: Optional[str] = None
    example: Optional[str] = None
    multi: bool = False          # NEW — credential accepts multiple values
    separator: str = ","         # NEW — delimiter for splitting, only meaningful when multi=True
```

Defaults ensure full backward compatibility. Existing connectors are unaffected.

Connector YAML example:

```yaml
requires:
  credentials:
    - name: NEXTDNS_PROFILE_ID
      description: NextDNS profile ID (visible in dashboard URL)
      type: config
      scope: env-var
      multi: true
      separator: ","
      example: "cf235c,ab9912"
```

### Activation & Credential Storage

No changes to the activation flow. `--set NEXTDNS_PROFILE_ID=cf235c,ab9912` stores the raw comma-separated string as a single value, same as today. The split happens at poll time, not activation time.

Storage behavior:

- `config.yaml` stores `NEXTDNS_PROFILE_ID: "cf235c,ab9912"`
- `resolved.yaml` receives the raw string via template substitution
- The poller detects `multi: true` on the credential and splits before making requests

Activation-time validation:

- If `multi: true`, the provided value must be non-empty after splitting
- Each split value must be non-empty (reject `"cf235c,,ab9912"`)
- Warning (not error) if only one value provided for a multi credential

### Virtual Poll Targets

For each connector with `multi: true` credentials, the poller expands it into N virtual poll targets at connector load time (and on config reload).

Expansion logic:

1. Scan the connector's `requires.credentials` for any with `multi: true`
2. For each multi credential, split the resolved value on `separator`
3. Compute the cartesian product if multiple credentials are multi (e.g., multi orgs x multi regions)
4. Each combination becomes a virtual poll target with:
   - **Unique dedup namespace**: `{connector_name}:{credential_name}={value}` (e.g., `nextdns-blocked:NEXTDNS_PROFILE_ID=cf235c`)
   - **Own poll timestamp**: tracked independently
   - **Own env overlay**: the multi credential's `${VAR}` resolves to the single value for this target
   - **Shared everything else**: interval, routes, auth, follow-up config

In the poll loop, virtual targets are iterated the same way connectors are today. `_poll_once` receives the target's env overlay, so `Template(url).safe_substitute(env)` naturally produces the correct per-value URL.

Rate limiting: each virtual target counts as its own poll. If a connector has interval `30m` and 3 profile IDs, that's 3 API calls every 30 minutes. The connector's rate limit (if any) is shared across targets to respect upstream API limits.

### Dedup & State Isolation

The intake poller uses SQLite for dedup state. Today it keys on `connector.name`. With virtual targets:

| Scenario | Dedup key format |
|----------|-----------------|
| Single-value connector (unchanged) | `{connector_name}` |
| Multi-value target | `{connector_name}:{multi_cred}={value}` |
| Multi-value follow-up | `{connector_name}:{multi_cred}={value}:fu:{parent_id}` |
| Cartesian product target | `{connector_name}:{cred_A}={val}:{cred_B}={val}` |

Adding values: re-activate with the expanded list. New values get fresh dedup namespaces and start polling immediately.

Removing values: orphaned dedup state rows are harmless (never queried again). No cleanup required; a janitor can be added later if needed.

No migration required. Existing single-value connectors keep their current keys.

### Observability & Audit

`agency hub show <connector>` with multi credentials displays per-target stats:

```
Component: nextdns-blocked (id=7a80f6bb)
State: active
Poll targets (2):
  NEXTDNS_PROFILE_ID=cf235c   last_poll=2m ago  items=12  errors=0
  NEXTDNS_PROFILE_ID=ab9912   last_poll=2m ago  items=3   errors=0
```

`agency intake stats`: aggregate counts roll up to the connector level. Per-target breakdown available via `--verbose`.

Audit log entries: add a `poll_target` field with the credential expansion label (e.g., `NEXTDNS_PROFILE_ID=cf235c`). Omitted for single-value connectors.

Error handling: if one target fails (401, timeout), it is logged and skipped. Other targets continue. Consecutive failures for a single target emit a warning signal but do not deactivate the connector.

### Affected Components

| Component | Change |
|-----------|--------|
| `agency_core/models/connector.py` — `ConnectorCredential` | Add `multi`, `separator` fields |
| `agency-gateway/internal/models/` — Go connector structs | Mirror `multi`, `separator` fields |
| `images/intake/server.py` — poll loop | Virtual target expansion, per-target env overlay, namespaced dedup |
| `agency-gateway/internal/hub/config.go` — activation validation | Validate multi values on activate |
| `agency-gateway/internal/api/handlers_phase7.go` — hub show | Per-target stats in response |
| NextDNS connector YAMLs (agency-hub) | Add `multi: true` to `NEXTDNS_PROFILE_ID` credential |

## Scope

**In scope:**

- `multi` and `separator` fields on ConnectorCredential (Python + Go models)
- Virtual poll target expansion in the intake poller
- Per-target dedup state isolation
- Per-target poll timestamp tracking
- Shared rate limiting across targets
- `hub show` per-target stats
- Audit log `poll_target` field
- Activation-time validation of multi values

**Not in scope:**

- Per-target activation/deactivation (all values active or none)
- Cartesian product caps or warnings
- Orphaned dedup state cleanup
- Multi-value support for `type: secret` / `scope: service-grant` credentials (secrets route through egress swap with a single grant name — extending to N grants is a separate design)
