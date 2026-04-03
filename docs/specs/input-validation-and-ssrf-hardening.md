# Input Validation and SSRF Hardening

Fixes two classes of security vulnerability in the gateway API: path traversal via unsanitized resource names, and SSRF via unvalidated outbound URLs.

## Problem

### Path Traversal

The codebase validates resource names at creation time (`validateAgentName`, `reMissionName`, `validPresetName` — all whitelist regexes). But read/modify handlers skip validation entirely. A request to `GET /api/v1/teams/../credentials/store.enc` passes chi's URL routing (chi blocks literal `/` in params but not `..`) and reaches `filepath.Join(home, "teams", name, ...)` which resolves outside the intended directory.

~25 handler entry points are affected across agents, teams, missions, canvases, policies, grants, capabilities, webhooks, hub instances, and MCP tools. Operations include reads, writes, and deletes.

### SSRF

Three outbound HTTP paths accept user-controlled URLs without host/scheme validation:

1. **Notification webhook URLs** (`POST /api/v1/notifications`): operator supplies arbitrary URL, gateway POSTs event payloads to it. Gateway runs on the host network — direct path to cloud IMDS (169.254.169.254), localhost services, RFC 1918 addresses.

2. **Credential test endpoints** (`POST /api/v1/credentials/{name}/test`): `testJWTExchange` uses `token_url` from ProtocolConfig as-is. Worse, it exfiltrates the credential secret via `${credential}` substitution in the POST body. `testAPIKey` forces HTTPS but the host is fully user-controlled.

3. **Web-fetch redirect following** (`images/web-fetch/handler.go`): blocklist checks only the initial hostname. `CheckRedirect` counts hops but does not re-validate redirect targets. An open redirect on an allowed domain bypasses the blocklist to reach internal services.

### CodeQL False Positives

GitHub's default CodeQL scanning flags all `filepath.Join` calls with user input as `go/path-injection`. No CodeQL configuration exists in the repo. A previous attempt (PR #2) tried to fix this by sprinkling `filepath.Base()` and `strings.Contains(.., "..")` at every call site, producing 6 commits of increasingly inconsistent changes that fought CodeQL's taint analysis. That PR is superseded by this work.

## Design

### Path Traversal: Boundary Validation

A single validation function in `internal/api/` applied at every handler entry point:

```go
var validResourceName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// requireName validates a user-supplied resource name.
// Returns the name and true if valid, or writes a 400 response and returns ("", false).
func requireName(w http.ResponseWriter, raw string) (string, bool) {
    if !validResourceName.MatchString(raw) {
        writeJSON(w, 400, map[string]string{"error": "invalid name"})
        return "", false
    }
    return raw, true
}
```

This is a whitelist regex — `..`, `/`, `\`, `%2F`, spaces, and any non-alphanumeric-hyphen character categorically cannot match. No need for `filepath.Base()` or blocklist-style checks.

**Where it is applied:** Every API handler and MCP tool handler that takes a resource name from URL params (`chi.URLParam`), query params (`r.URL.Query().Get`), or JSON body fields and uses it in filesystem paths. Approximately 25 entry points.

**Where it is NOT applied:** Internal functions (`MissionManager.Get`, `policy.Engine.Compute`, `logs.Reader.ReadAgentLog`, etc.) receive already-validated input from the handler boundary. No changes to internal code.

**Alignment with existing patterns:** The existing creation-time validators use equivalent regexes:
- `validateAgentName`: `^[a-z0-9][a-z0-9-]*[a-z0-9]$`
- `reMissionName`: `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`
- `validPresetName`: `^[a-z0-9][a-z0-9\-]{0,63}$`

`validResourceName` is a superset that matches all of these. Single-character names (e.g., `a`) are allowed — this matches `reMissionName`'s existing behavior.

Preset handlers already have `validPresetName`; those are updated to use the shared `requireName` for consistency.

### SSRF: Outbound URL Validation

A shared URL validation function for gateway-originated outbound requests:

```go
func validateOutboundURL(raw string) error
```

Checks:
1. **Scheme allowlist**: `https` required. `http` allowed only for `localhost`/`127.0.0.1` (ntfy local dev).
2. **Hostname blocklist** (string-level, before DNS):
   - `localhost`, `127.0.0.1`, `::1` (unless scheme is http and explicitly allowed)
   - `169.254.*` (link-local / IMDS)
   - `metadata.google.internal`, `*.internal`
   - `10.*`, `172.16-31.*`, `192.168.*` (RFC 1918)
3. **DNS resolution check**: resolve hostname, verify all returned IPs are public. Defends against DNS rebinding where a public hostname resolves to a private IP.

Applied at:
- `addNotification` in `handlers_events.go` — before persisting the webhook URL
- `testJWTExchange` in `credstore/store.go` — before the outbound POST
- `testAPIKey` in `credstore/store.go` — before the outbound GET

**Web-fetch redirect fix** (separate, in container image source):

In `images/web-fetch/handler.go`, the `CheckRedirect` function is updated to call `s.blocklist.IsBlocked()` on each redirect target's hostname. If blocked, return `http.ErrUseLastResponse` to stop the chain. This requires passing the blocklist into the HTTP client builder.

### CodeQL Configuration

Three files added:

1. **`.github/workflows/codeql.yml`** — CodeQL analysis workflow. Runs on PRs to main and weekly schedule. Go language. References the custom config.

2. **`.github/codeql/codeql-config.yml`** — points to the extensions directory, sets query suite to `security-and-quality`, excludes `images/tests/` and any vendored paths.

3. **`.github/codeql/extensions/agency-sanitizers.yml`** — CodeQL data extension (Models as Data) that registers `requireName` as a sanitizer for the `go/path-injection` query. Also registers `validateOutboundURL` as a sanitizer for `go/ssrf`.

Additionally, the CI workflow (`.github/workflows/ci.yml`) gets top-level `permissions: contents: read` to scope default token permissions.

## Files Changed

### New files
- `internal/api/validation.go` — `requireName`, `validResourceName`
- `internal/api/urlvalidation.go` — `validateOutboundURL` and private IP checking helpers
- `.github/workflows/codeql.yml` — CodeQL analysis workflow
- `.github/codeql/codeql-config.yml` — CodeQL configuration
- `.github/codeql/extensions/agency-sanitizers.yml` — sanitizer model pack

### Modified files (path traversal — add `requireName` calls)
- `internal/api/handlers_agent.go` — showTeam, teamActivity, createTeam
- `internal/api/handlers_agent_config.go` — agentConfig, updateAgentConfig
- `internal/api/handlers_admin.go` — adminTrust, adminEgress, adminAudit, rebuildAgent, adminDepartment
- `internal/api/handlers_canvas.go` — getCanvas, putCanvas, deleteCanvas
- `internal/api/handlers_grants.go` — grantAgent, revokeAgent
- `internal/api/handlers_missions.go` — showMission, deleteMission, missionHealth
- `internal/api/handlers_trajectory.go` — getAgentTrajectory
- `internal/api/handlers_hub.go` — autoActivate (validate inst.Name/Kind before use)
- `internal/api/handlers_connector_setup.go` — loadConnectorConfig
- `internal/api/handlers_presets.go` — replace `validPresetName` with shared `requireName`
- `internal/api/routes.go` — showPolicy, validatePolicy, startAgent, registerEnforcerWSClient
- `internal/api/manifest.go` — generateAgentManifest, loadPresetScopes
- `internal/api/mcp_register.go` — MCP tool handlers that accept names
- `internal/api/mcp_admin.go` — MCP admin tool handlers

### Modified files (SSRF)
- `internal/api/handlers_events.go` — addNotification: validate URL before persist
- `internal/credstore/store.go` — testJWTExchange, testAPIKey: validate URL before request
- `images/web-fetch/handler.go` — CheckRedirect: re-check blocklist on each redirect hop

### Modified files (CI)
- `.github/workflows/ci.yml` — add `permissions: contents: read`

## What This Does NOT Change

- Internal functions (`MissionManager.Get`, `policy.Engine.Compute`, `logs.Reader.ReadAgentLog`, etc.) — boundary validation is sufficient
- LLM proxy URL handling — host is operator-controlled, traffic goes through egress proxy
- Trajectory proxy — agent name is registry-gated
- Knowledge proxy — base URL is a hard-coded constant

## Relationship to PR #2

PR #2 is closed without merging. This work supersedes it entirely. The CI permissions change from PR #2 is included here.
