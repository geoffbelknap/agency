# Auth Hardening

**Date:** 2026-04-06
**Status:** Draft
**Scope:** Spec 2 of 4 from the architecture review response. Hardens gateway authentication defaults so the gateway always has a token and never runs with auth disabled.

## Problem

The gateway's `BearerAuth` middleware bypasses all authentication when the token is empty (`if token == "" { next.ServeHTTP... }`). This is intended for local development but creates a risk: if the gateway starts before `agency setup` generates a token (e.g., daemon auto-start, direct `agency serve`), the API is fully unauthenticated. While the gateway only listens on localhost by default, config drift could combine empty token with non-localhost binding.

## Goals

1. Ensure the gateway always has an auth token — auto-generate on first load if none exists.
2. Remove the empty-token bypass from the auth middleware — fail-closed.
3. Warn when listening on non-localhost addresses.

## Non-Goals

- Changing the auth scheme (tokens, OAuth, mTLS, etc.).
- Multi-user auth or role-based access control (handled separately by the principal registry).
- Restricting the gateway to localhost only (operator's choice).

## Design

### 1. Auto-generate token in config.Load()

In `internal/config/config.go`, after loading `config.yaml`, if `cfg.Token` is empty:

1. Generate a 32-byte random token: `rand.Read(tokenBytes)`, `hex.EncodeToString(tokenBytes)`.
2. Persist it to `config.yaml` (same write-back pattern as the existing HMAC key auto-generation).
3. Log to stderr: `"auto-generated gateway auth token (no token in config.yaml)"`.

Same for `cfg.EgressToken` — if empty after load, generate and persist.

This follows the existing pattern at `config.go:99-108` where the HMAC key is auto-generated and persisted. The token generation uses the same `crypto/rand` source.

After this change, `cfg.Token` is guaranteed non-empty after `config.Load()` returns, regardless of whether `agency setup` has been run.

### 2. Remove empty-token bypass from BearerAuth

In `internal/api/middleware_auth.go`, remove the bypass block:

```go
// REMOVE:
// Dev/local mode: no token configured, allow all requests.
if token == "" {
    next.ServeHTTP(w, r)
    return
}
```

With Change 1, the token is always non-empty. If somehow it is empty (code path that doesn't go through `config.Load()`), requests receive 401 — fail-closed per ASK Tenet 4.

### 3. Log warning for non-localhost binding

In `cmd/gateway/main.go`, after config is loaded and before the HTTP server starts, check `cfg.GatewayAddr`:

```go
if !strings.HasPrefix(cfg.GatewayAddr, "127.0.0.1") && !strings.HasPrefix(cfg.GatewayAddr, "localhost") {
    logger.Warn("gateway listening on non-localhost address — ensure network access is restricted",
        "addr", cfg.GatewayAddr)
}
```

This is informational — the gateway still starts. The operator is warned.

## Testing

- **Unit test:** `config.Load()` with no `config.yaml` returns a config with non-empty `Token` and `EgressToken`. Verify the tokens are persisted to `config.yaml` on disk.
- **Unit test:** `config.Load()` with existing token in `config.yaml` preserves it (doesn't overwrite).
- **Unit test:** `BearerAuth` with empty token string rejects all non-exempt requests with 401 (fail-closed).
- **Existing tests:** `middleware_auth_test.go` already has table-driven tests for `BearerAuth`. Update or remove the "dev mode" test case that expects empty-token bypass.

## Migration

- **Existing users with `agency setup` done:** No change — their `config.yaml` already has a token.
- **Existing users without `agency setup`:** First gateway start after this change auto-generates a token and persists it. The token is logged so they can use it. Existing CLI commands that auto-start the daemon will work because the CLI reads the token from `config.yaml` for its requests.
- **CI/test environments:** Any environment that relied on empty-token bypass needs to either run `agency setup` first or read the auto-generated token from `config.yaml`.

## Risks

| Risk | Mitigation |
|------|-----------|
| Auto-generated token logged to stderr could leak in CI logs | Token is logged once at first generation. CI environments should treat gateway logs as sensitive (same as any service that logs credentials on init). |
| Config.Load() writing to disk is a side effect | Already established pattern — HMAC key uses identical approach. |
| Breaking change for scripts that don't pass a token | Scripts must read the token from `config.yaml`. This is already the expected pattern — `agency` CLI does it. |
