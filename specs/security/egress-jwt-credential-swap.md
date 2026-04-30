---
description: "---"
status: "Approved"
---

# Egress JWT Credential Swap

**Date:** 2026-03-28
**Status:** Approved
**Last updated:** 2026-04-01

---

## Problem

Some APIs (LimaCharlie, and likely others in the future) require a two-step authentication flow: exchange a long-lived API key for a short-lived JWT, then use the JWT as a Bearer token on subsequent requests. The current egress credential swap supports direct key injection and GitHub App token exchange, but not generic JWT exchange.

Without this, either the intake container holds the raw API key (violating ASK Tenet 4 — only egress holds real credentials) or connectors can't authenticate to JWT-based APIs at all.

## Solution

Add a fourth credential swap type to the egress proxy: **JWT exchange**. Configured per-domain in the service keys, it exchanges a stored API key for a JWT via a token endpoint, caches the JWT, and injects it as a Bearer token on matching requests.

## Design

### Configuration

JWT swap credentials are stored in the encrypted credential store (`~/.agency/credentials/store.enc`) via `agency creds set`. JWT exchange parameters are defined as `protocol_config` metadata on the credential entry, or inherited from a credential group.

```bash
# Store the service credential (inherits JWT config from limacharlie group)
agency creds set --name limacharlie-api --kind service \
  --scope platform --group limacharlie --value <api-key>
```

The credential store generates the corresponding entry in `credential-swaps.yaml`:

```yaml
# Generated in ~/.agency/infrastructure/credential-swaps.yaml
limacharlie-api:
  type: jwt-exchange
  domains: [api.limacharlie.io]
  token_url: "https://jwt.limacharlie.io"
  token_params:
    oid: "${LC_ORG_ID}"      # config value substitution
    secret: "${credential}"   # replaced with the credential value at runtime
  token_response_field: "jwt"
  token_ttl_seconds: 3000
  inject_header: "Authorization"
  inject_format: "Bearer {token}"
  key_ref: limacharlie-api
```

### Swap Flow

1. Request arrives at egress proxy destined for `api.limacharlie.io`
2. Egress checks `credential-swaps.yaml` — domain matches `limacharlie-api` config
3. Check JWT cache: if cached token exists and hasn't expired, inject and forward
4. If no cached token or expired:
   a. Resolve the raw API key from the credential store via SocketKeyResolver (gateway credential socket at `~/.agency/run/gateway-cred.sock`)
   b. POST to `token_url` with `token_params` (substituting `${credential}` with the key and config values)
   c. Parse JSON response, extract token from `token_response_field`
   d. Cache the token for `token_ttl_seconds`
   e. Inject as `Authorization: Bearer {token}` header
5. Forward request to upstream

### Cache

- In-memory dict: `{service_name: (token, expiry_timestamp)}`
- Checked on every request to a matching domain
- Refreshed when `time.time() > expiry_timestamp`
- Cleared on SIGHUP reload (forces fresh exchange on next request)
- No persistence — tokens re-fetched on egress restart

### Connector YAML Impact

Connectors using JWT-based APIs don't need to know about JWT exchange. They reference a service grant name and the egress handles the rest:

```yaml
# Before (broken — raw key doesn't work):
source:
  headers:
    Authorization: "Bearer ${LC_API_KEY}"

# After (correct — egress does JWT swap):
source:
  url: "https://api.limacharlie.io/v1/insight/${LC_ORG_ID}/detections"
  headers: {}   # no auth header needed — egress injects it
```

The connector's requests flow through the egress proxy (via `HTTPS_PROXY`). The egress sees the destination domain, matches it against `credential-swaps.yaml`, and handles authentication transparently.

### Credential Flow

```
Intake container          Egress proxy              Gateway cred socket    LimaCharlie
                          (resolves via socket)      (~/.agency/run/
                                                      gateway-cred.sock)

  GET /detections -------> domain matches
                           credential-swaps.yaml

                           cache miss?
                           resolve key_ref ---------> returns raw API key
                           POST jwt.limacharlie.io
                           {oid, secret} -------------------------------------------> returns {jwt: "..."}
                           cache token

                           inject Bearer header
                           forward request ------------------------------------------------> 200 {detects: [...]}
                           <--------------------------------------------------------------
  <-----------------------
  (never sees key or JWT)
```

### Implementation

JWT exchange is handled by the unified swap handler dispatch in `services/egress/credential_swap.py`. The `CredentialSwapAddon` loads `credential-swaps.yaml` entries of type `jwt-exchange` and manages them via `_JWTSwapAuth` instances:

- On startup or SIGHUP: load `credential-swaps.yaml`, build swap registry
- For `jwt-exchange` entries: create `_JWTSwapAuth` instance with `SocketKeyResolver` for credential resolution
- On request: domain match → dispatch to JWT handler → resolve credential via gateway socket → exchange for token → cache → inject

The `SocketKeyResolver` connects to the gateway credential socket at `~/.agency/run/gateway-cred.sock` to resolve `key_ref` values to real credential values at runtime. This socket is dedicated to credential resolution and is only mounted into the egress container — credentials never traverse a Docker network.

### Error Handling

- Token exchange failure: log warning, forward request without auth (upstream will 401, intake retries on next poll cycle)
- Malformed response from token endpoint: log warning, same as above
- Token endpoint unreachable: log warning, use cached token if available (even if expired — some APIs accept slightly expired JWTs)
- Invalid `credential-swaps.yaml`: log error at startup, skip JWT swap (other swaps still work)

### Generic Design

The config format is intentionally generic — not LimaCharlie-specific:

- `token_url`: any HTTP endpoint that returns tokens
- `token_params`: arbitrary key-value pairs (supports form-encoded POST)
- `token_response_field`: works for any JSON response shape
- `inject_format`: supports `Bearer {token}`, `token {token}`, or any custom format

This handles OAuth2 client_credentials, custom JWT exchange, and any other "POST credentials, get token" pattern.

### Future: OAuth2 Client Credentials

The same mechanism supports OAuth2 `client_credentials` grant:

```yaml
some-oauth-api:
  token_url: "https://auth.example.com/oauth/token"
  token_params:
    grant_type: "client_credentials"
    client_id: "${OAUTH_CLIENT_ID}"
    client_secret: "${credential}"
  token_response_field: "access_token"
  token_ttl_seconds: 3600
  match_domains:
    - "api.example.com"
```

No code changes needed — the generic config format covers it.

---

## Changes Required

### Egress proxy

| File | Change |
|------|--------|
| `services/egress/credential_swap.py` | `_JWTSwapAuth` class, `SocketKeyResolver` for credential resolution, unified handler dispatch from `credential-swaps.yaml` |
| `services/egress/Dockerfile` | No change (PyJWT already available for GitHub App auth) |

### Configuration

| File | Change |
|------|--------|
| `~/.agency/infrastructure/credential-swaps.yaml` | Generated from credential store — JWT exchange entries included automatically |
| `agency-gateway/internal/credstore/` | Credential store with JWT protocol_config support |
| `agency-gateway/internal/orchestrate/infra.go` | Mount `credential-swaps.yaml` + gateway socket into egress |

### LimaCharlie connector

| File | Change |
|------|--------|
| `connectors/limacharlie/connector.yaml` | Remove `Authorization` header, fix endpoint URLs and field names |
| `connectors/limacharlie-sensors/connector.yaml` | Same auth fix |

### Operator setup

```bash
# 1. Create credential group for shared JWT exchange config
agency creds group create limacharlie \
  --protocol jwt-exchange \
  --token-url https://jwt.limacharlie.io \
  --requires LC_ORG_ID

# 2. Set the config dependency
agency config set LC_ORG_ID <your-org-id>

# 3. Store the service credential (inherits group config)
agency creds set --name limacharlie-api --kind service \
  --scope platform --group limacharlie --value <api-key>

# credential-swaps.yaml is regenerated automatically, egress receives SIGHUP
```

---

## ASK Compliance

- **Tenet 3 (mediation complete)**: JWT exchange happens in the egress proxy, which is the mediation boundary. No unmediated path.
- **Tenet 4 (least privilege)**: Raw API key and JWT both stay in the egress container. Intake/agents never see either.
- **Tenet 2 (trace)**: Egress audit log records the domain, service name, and whether JWT was refreshed. Token values are not logged.

---

## Tests

- JWT exchange: mock token endpoint, verify token cached and reused
- Cache expiry: verify refresh after TTL
- Domain matching: verify correct service selected by domain
- Credential substitution: `${credential}` and `${ENV}` expanded correctly
- Token endpoint failure: request forwarded without auth (graceful degradation)
- SIGHUP: cache cleared, config reloaded
- Multiple services: each domain maps to its own JWT config
