# Credential Store Migration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate all legacy `.env` file paths for credential storage and resolution. All credentials flow through the encrypted credential store — no fallbacks, no flat files.

**Architecture:** `agency setup` stores LLM API keys in the credential store via the gateway API (after daemon start). All credential resolution paths (egress, gateway internal LLM, health checks, model selection) query the credential store. The `FileKeyResolver` and `.service-keys.env` fallbacks are removed. Config vars (non-secret operator settings like `LC_ORG_ID`) remain in `~/.agency/.env` — that's config, not credentials.

**Tech Stack:** Go (gateway), Python (egress image)

---

## Scope

### What changes

| # | File | Change |
|---|------|--------|
| 1 | `internal/config/init.go` | Stop writing API keys to `.env`. Add `StoreCredential()` helper that POSTs to gateway API. |
| 2 | `cmd/gateway/main.go` | After daemon starts, call credential store API to persist the LLM key. |
| 3 | `internal/api/handlers_internal_llm.go` | `loadProviderKey()` checks credential store instead of `.env`. |
| 4 | `internal/orchestrate/start.go` | Model selection credential check queries credential store instead of `.env`. |
| 5 | `internal/api/handlers_credentials.go` | Remove `.service-keys.env` fallback from `resolveCredential()`. |
| 6 | `internal/orchestrate/mission_health_check.go` | Check credential store instead of `.service-keys.env`. |
| 7 | `internal/orchestrate/hub_health.go` | Check credential store instead of `.service-keys.env`. |
| 8 | `internal/orchestrate/infra.go` | Remove `.service-keys.env` bind-mount from egress container. |
| 9 | `internal/orchestrate/meeseeks_start.go` | Remove `.service-keys.env` bind-mount from meeseeks enforcer. |
| 10 | `images/egress/credential_swap.py` | Remove `FileKeyResolver` fallback — require `GATEWAY_SOCKET`. |
| 11 | `images/egress/key_resolver.py` | Delete `FileKeyResolver` class entirely. |

### What stays the same

- `~/.agency/.env` for **config vars** (non-secret operator settings). The `envfile.Load()` calls in `infra.go:465`, `infra.go:711`, and `workspace_provision.go:97` are correct — they read config vars, not credentials.
- `SocketKeyResolver` — this is the correct primary path.
- `credential-swaps.yaml` generation — `key_ref` values stay as logical names (e.g., `ANTHROPIC_API_KEY`), resolved at runtime by the socket resolver from the credential store.
- The `envfile` package itself — still needed for config var loading.

---

## Task 1: Setup stores LLM key in credential store

The root cause: `agency setup` writes the API key to `~/.agency/.env` but nothing reads credentials from there. The key must go into the encrypted credential store.

**Files:**
- Modify: `cmd/gateway/main.go:475-543` (function `runSetup`)
- Modify: `internal/config/init.go:234-238` (stop writing API keys to `.env`)

- [ ] **Step 1: Write test for setup credential storage**

No unit test needed here — this is a CLI integration path. We'll verify manually at the end.

- [ ] **Step 2: Remove API key writes from `RunInit`**

In `internal/config/init.go`, remove the loop at lines 234-238 that calls `upsertEnvFile` for credential entries. Keep the config var logic (provider name in `config.yaml`) and the `ReadExistingKeys` function (needed for migration).

```go
// In RunInit(), REMOVE these lines (234-238):
// 	for _, e := range entries {
// 		if err := upsertEnvFile(agencyHome, e.provider, e.key); err != nil {
// 			return err
// 		}
// 	}
```

Replace with storing the entries in `config.yaml` temporarily so `runSetup` can read them after daemon start:

```go
	// Store pending credentials in config for post-daemon-start storage.
	// These are written to the credential store once the daemon is running.
	if len(entries) > 0 {
		pending := make([]map[string]string, 0, len(entries))
		for _, e := range entries {
			pending = append(pending, map[string]string{
				"provider": e.provider,
				"env_var":  strings.ToUpper(e.provider) + "_API_KEY",
			})
		}
		cfg["pending_credentials"] = pending
		// Store the raw key in memory only — pass back via return value
	}
```

Actually, simpler approach: keep the key in the calling function (`runSetup`) and POST it after the daemon starts. `RunInit` returns the entries it would have stored, and `runSetup` stores them via the API.

Change `RunInit` signature to return the key entries:

In `internal/config/init.go`:

```go
// KeyEntry holds a provider credential to be stored after daemon start.
type KeyEntry struct {
	Provider string
	EnvVar   string
	Key      string
}

// RunInit creates the ~/.agency/ directory structure and config files.
// Returns any LLM API key entries that need to be stored in the credential
// store after the daemon starts.
func RunInit(opts InitOptions) ([]KeyEntry, error) {
```

Change the entries section (around line 142-166) to build `[]KeyEntry` and return them instead of writing to `.env`:

```go
	var pendingKeys []KeyEntry

	if opts.AnthropicAPIKey != "" {
		pendingKeys = append(pendingKeys, KeyEntry{"anthropic", "ANTHROPIC_API_KEY", opts.AnthropicAPIKey})
	}
	if opts.OpenAIAPIKey != "" {
		pendingKeys = append(pendingKeys, KeyEntry{"openai", "OPENAI_API_KEY", opts.OpenAIAPIKey})
	}
	if opts.Provider != "" && opts.APIKey != "" {
		alreadyCovered := false
		for _, e := range pendingKeys {
			if e.Provider == opts.Provider {
				alreadyCovered = true
				break
			}
		}
		if !alreadyCovered {
			pendingKeys = append(pendingKeys, KeyEntry{opts.Provider, strings.ToUpper(opts.Provider) + "_API_KEY", opts.APIKey})
		}
	}
```

Remove the `upsertEnvFile` loop entirely. Remove the `upsertEnvFile` function if no other callers exist.

Return `pendingKeys, nil` at the end of the function.

- [ ] **Step 3: Update all `RunInit` callers to handle the new return value**

Search for all callers of `config.RunInit`:

```bash
grep -rn "config.RunInit\|RunInit(" internal/ cmd/ --include="*.go"
```

In `cmd/gateway/main.go` (`runSetup` function, line 482):
```go
	pendingKeys, err := config.RunInit(config.InitOptions{
		Provider:  provider,
		APIKey:    apiKey,
		NotifyURL: notifyURL,
	})
	if err != nil {
		return err
	}
```

In any MCP tool callers (check `mcp_register.go`), update similarly — likely they can ignore the return value with `_, err :=`.

- [ ] **Step 4: Store credentials via gateway API after daemon starts**

In `cmd/gateway/main.go`, after the daemon starts successfully (after line 503 "Daemon started successfully"), add credential storage:

```go
	// Store LLM credentials in the encrypted credential store
	if len(pendingKeys) > 0 {
		cfg := config.Load()
		c := apiclient.NewClient("http://" + cfg.GatewayAddr)
		for _, key := range pendingKeys {
			fmt.Printf("Storing %s credential...\n", key.Provider)
			if err := c.StoreCredential(key.EnvVar, key.Key, "provider", "platform", "api-key"); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to store %s credential: %v\n", key.Provider, err)
				fmt.Fprintf(os.Stderr, "  Run manually: agency creds set %s <key> --kind provider --scope platform --protocol api-key\n", key.EnvVar)
			}
		}
	}
```

- [ ] **Step 5: Add `StoreCredential` to the API client**

Check if `apiclient` already has a credential store method. If not, add one. The API client is at `internal/apiclient/` or similar — find it:

```bash
grep -rn "type Client struct" internal/apiclient/ --include="*.go"
```

Add a method that POSTs to `/api/v1/credentials`:

```go
// StoreCredential stores a credential in the encrypted credential store.
func (c *Client) StoreCredential(name, value, kind, scope, protocol string) error {
	body := map[string]string{
		"name":     name,
		"value":    value,
		"kind":     kind,
		"scope":    scope,
		"protocol": protocol,
	}
	return c.post("/api/v1/credentials", body)
}
```

Adapt to match the actual `apiclient` patterns (JSON encoding, auth token, error handling).

- [ ] **Step 6: Migrate existing `.env` keys on setup**

If the user has already run setup and has keys in `~/.agency/.env`, we should migrate them. Add a migration step in `runSetup` (or as a helper called from setup):

```go
	// Migrate any existing .env API keys to credential store
	existingProviders := config.ReadExistingKeys(agencyHome)
	if len(existingProviders) > 0 && len(pendingKeys) == 0 {
		envVars := envfile.Load(filepath.Join(agencyHome, ".env"))
		providerEnvMap := map[string]string{
			"anthropic": "ANTHROPIC_API_KEY",
			"openai":    "OPENAI_API_KEY",
			"google":    "GOOGLE_API_KEY",
		}
		for _, provider := range existingProviders {
			envVar := providerEnvMap[provider]
			if val, ok := envVars[envVar]; ok && val != "" {
				fmt.Printf("Migrating %s credential to secure store...\n", provider)
				if err := c.StoreCredential(envVar, val, "provider", "platform", "api-key"); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to migrate %s: %v\n", provider, err)
				}
			}
		}
	}
```

- [ ] **Step 7: Build and test**

```bash
go build ./cmd/gateway/
```

- [ ] **Step 8: Commit**

```bash
git add cmd/gateway/main.go internal/config/init.go internal/apiclient/
git commit -m "feat: store LLM credentials in encrypted credential store during setup

agency setup now stores API keys in the credential store instead of
the legacy .env file. Existing .env keys are migrated on next setup run."
```

---

## Task 2: Gateway internal LLM uses credential store

The gateway makes internal LLM calls (e.g., for success criteria evaluation). `loadProviderKey()` currently reads from `.env` — it should use the credential store.

**Files:**
- Modify: `internal/api/handlers_internal_llm.go:245-259`

- [ ] **Step 1: Update `loadProviderKey` to check credential store**

```go
func (h *handler) loadProviderKey(provider *models.ProviderConfig) string {
	if provider.AuthEnv == "" {
		return ""
	}
	// Check process environment first (for dev/CI overrides)
	if v := os.Getenv(provider.AuthEnv); v != "" {
		return v
	}
	// Check credential store
	if h.credStore != nil {
		entry, err := h.credStore.Get(provider.AuthEnv)
		if err == nil && entry.Value != "" {
			return entry.Value
		}
	}
	return ""
}
```

Remove the `envfile.Load` call and the `envfile` import if unused elsewhere in the file.

- [ ] **Step 2: Build and test**

```bash
go build ./cmd/gateway/
```

- [ ] **Step 3: Commit**

```bash
git add internal/api/handlers_internal_llm.go
git commit -m "fix: gateway internal LLM reads provider keys from credential store"
```

---

## Task 3: Model selection credential check uses credential store

During agent start, the model selector checks if a provider has credentials. Currently reads `.env`.

**Files:**
- Modify: `internal/orchestrate/start.go:465-489`

- [ ] **Step 1: Find the credential check and understand the context**

The function at line ~465 loads `.env` and checks `envVars[authEnv]`. It needs access to the credential store. Check how `StartSequence` is constructed — does it have access to a `credStore`? If not, it needs one passed in.

- [ ] **Step 2: Add credential store to StartSequence**

If `StartSequence` doesn't already have a `credStore` field, add one and wire it through from the handler/caller:

```go
// In the StartSequence struct (or wherever the model selection lives):
credStore *credstore.Store
```

- [ ] **Step 3: Update the credential check**

Replace the `.env`-based check:

```go
	// Old:
	// envVars := envfile.Load(filepath.Join(ss.Home, ".env"))
	// ...
	// if authEnv == "" || envVars[authEnv] != "" || os.Getenv(authEnv) != "" {

	// New: check credential store, then process env
	hasCredential := func(authEnv string) bool {
		if authEnv == "" {
			return true
		}
		if os.Getenv(authEnv) != "" {
			return true
		}
		if ss.credStore != nil {
			if entry, err := ss.credStore.Get(authEnv); err == nil && entry.Value != "" {
				return true
			}
		}
		return false
	}
	// ...
	if hasCredential(authEnv) {
```

- [ ] **Step 4: Build and test**

```bash
go build ./cmd/gateway/
```

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrate/start.go
git commit -m "fix: model selection checks credential store for provider keys"
```

---

## Task 4: Remove `.service-keys.env` fallback from credential resolver

The `resolveCredential` handler has a fallback that reads `.service-keys.env`. Remove it — the credential store is the single source of truth.

**Files:**
- Modify: `internal/api/handlers_credentials.go:405-417`

- [ ] **Step 1: Remove the fallback block**

In `resolveCredential()`, delete lines 405-417 (the `.service-keys.env` fallback). The function should go directly from "credential store not found" to "404 not found":

```go
	// After the credential store check (line 402):
	// Credential not found in store
	writeJSON(w, 404, map[string]string{"error": "credential not found"})
```

Remove the `envfile` import if no longer used in this file.

- [ ] **Step 2: Build and test**

```bash
go build ./cmd/gateway/
```

- [ ] **Step 3: Commit**

```bash
git add internal/api/handlers_credentials.go
git commit -m "fix: remove .service-keys.env fallback from credential resolver

The credential store is now the single source of truth. Flat file
fallback was a migration path that is no longer needed."
```

---

## Task 5: Health checks use credential store

Both `mission_health_check.go` and `hub_health.go` read `.service-keys.env` to verify credentials exist. They should query the credential store.

**Files:**
- Modify: `internal/orchestrate/mission_health_check.go:172-214`
- Modify: `internal/orchestrate/hub_health.go:183-195, 225-260`

- [ ] **Step 1: Check how health checkers access dependencies**

Find the struct definitions for the health checkers:

```bash
grep -n "type.*HealthChecker struct\|type.*healthCheck struct" internal/orchestrate/mission_health_check.go internal/orchestrate/hub_health.go
```

Add a `credStore *credstore.Store` field if not present, and wire it from the caller.

- [ ] **Step 2: Update mission health check**

Replace the `.service-keys.env` read with a credential store lookup:

```go
// Old (lines 172-174):
// keysPath := filepath.Join(hc.Home, "infrastructure", ".service-keys.env")
// keysData, _ := os.ReadFile(keysPath)
// keys := string(keysData)

// New: check credential store
hasCredential := func(name string) (bool, bool) { // (exists, nonEmpty)
	if hc.credStore == nil {
		return false, false
	}
	entry, err := hc.credStore.Get(name)
	if err != nil {
		return false, false
	}
	return true, entry.Value != ""
}
```

Update the check at line 188:
```go
// Old: if !strings.Contains(keys, grantName+"=") {
// New:
exists, nonEmpty := hasCredential(grantName)
if !exists {
```

Update the fix message at line 192:
```go
// Old: Fix: fmt.Sprintf("echo '%s=YOUR_KEY' >> ~/.agency/infrastructure/.service-keys.env ...")
// New:
Fix: fmt.Sprintf("agency creds set %s <YOUR_KEY> --kind service --scope platform --protocol api-key", grantName),
```

Update the empty-value check at line 199:
```go
// Old: val := strings.TrimPrefix(kline, grantName+"=")
//      if strings.TrimSpace(val) == "" {
// New:
} else if !nonEmpty {
	checks = append(checks, HealthCheck{
		Name: "credential_health", Status: "fail",
		Detail: fmt.Sprintf("%s: credential value is empty", grantName),
		Fix:    fmt.Sprintf("agency creds set %s <YOUR_KEY> --kind service --scope platform --protocol api-key", grantName),
	})
} else {
	checks = append(checks, HealthCheck{
		Name: "credential_health", Status: "pass",
		Detail: fmt.Sprintf("%s: configured", grantName),
	})
}
```

- [ ] **Step 3: Update hub health check**

Same pattern — replace `.service-keys.env` reads at lines 183 and 225 with credential store lookups.

```go
// Old (line 183-185):
// keysPath := filepath.Join(hc.Home, "infrastructure", ".service-keys.env")
// keysData, _ := os.ReadFile(keysPath)
// if strings.Contains(string(keysData), inst.Name+"=") {

// New:
if hc.credStore != nil {
	if _, err := hc.credStore.Get(inst.Name); err == nil {
		checks = append(checks, HealthCheck{
			Name: "service_key", Status: "pass",
			Detail: "service key configured",
		})
	} else {
		checks = append(checks, HealthCheck{
			Name: "service_key", Status: "warn",
			Detail: "service key not found (may not be required)",
		})
	}
}
```

- [ ] **Step 4: Build and test**

```bash
go build ./cmd/gateway/
```

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrate/mission_health_check.go internal/orchestrate/hub_health.go
git commit -m "fix: health checks verify credentials via credential store

Replace .service-keys.env file reads with credential store queries.
Fix messages now show the correct 'agency creds set' command."
```

---

## Task 6: Remove `.service-keys.env` container mounts

With the `FileKeyResolver` fallback removed, there's no need to mount `.service-keys.env` into containers.

**Files:**
- Modify: `internal/orchestrate/infra.go:471-475`
- Modify: `internal/orchestrate/meeseeks_start.go:182-184`

- [ ] **Step 1: Remove egress `.service-keys.env` mount**

In `infra.go`, remove lines 472-475:

```go
// DELETE:
// serviceKeys := filepath.Join(infraDir, ".service-keys.env")
// if fileExists(serviceKeys) {
// 	binds = append(binds, serviceKeys+":/app/secrets/.service-keys.env:ro")
// }
```

- [ ] **Step 2: Remove meeseeks `.service-keys.env` mount**

In `meeseeks_start.go`, remove lines 182-184:

```go
// DELETE:
// if fileExists(serviceKeys) {
// 	binds = append(binds, serviceKeys+":/agency/enforcer/service-keys.env:ro")
// }
```

Also remove the `serviceKeys` variable declaration if it's now unused.

- [ ] **Step 3: Build and test**

```bash
go build ./cmd/gateway/
```

- [ ] **Step 4: Commit**

```bash
git add internal/orchestrate/infra.go internal/orchestrate/meeseeks_start.go
git commit -m "fix: remove .service-keys.env bind-mounts from containers

Egress and meeseeks enforcers no longer need the legacy flat file.
Credentials are resolved via the gateway socket."
```

---

## Task 7: Remove `FileKeyResolver` from egress image

The Python egress proxy falls back to `FileKeyResolver` when the gateway socket isn't available. Since the socket is always mounted (Task 6 kept that), the fallback should be an error, not a silent degradation to an insecure file.

**Files:**
- Modify: `images/egress/credential_swap.py:51-68`
- Modify: `images/egress/key_resolver.py` (delete `FileKeyResolver` class)

- [ ] **Step 1: Make `SocketKeyResolver` required**

In `credential_swap.py`, replace the fallback logic:

```python
def __init__(
    self,
    swap_config_path: str = "/app/secrets/credential-swaps.yaml",
    swap_local_path: str = "/app/secrets/credential-swaps.local.yaml",
):
    self._swap_config_path = swap_config_path
    self._swap_local_path = swap_local_path

    # Gateway socket is required for credential resolution.
    gateway_socket = os.environ.get("GATEWAY_SOCKET", "")
    if not gateway_socket or not os.path.exists(gateway_socket):
        raise RuntimeError(
            "GATEWAY_SOCKET not set or socket not found. "
            "The egress proxy requires the gateway socket for credential resolution."
        )
    self._resolver = SocketKeyResolver(gateway_socket)
    logger.info("Using SocketKeyResolver (socket: %s)", gateway_socket)
```

Remove the `service_keys_path` parameter and the `FileKeyResolver` import.

- [ ] **Step 2: Delete `FileKeyResolver` from `key_resolver.py`**

Remove the `FileKeyResolver` class (lines 69-99) from `images/egress/key_resolver.py`. Keep `SocketKeyResolver` and the `KeyResolver` protocol.

- [ ] **Step 3: Update swap handler comments**

In `images/egress/swap_handlers.py`, update any comments referencing `.service-keys.env`:

```bash
grep -n "service-keys" images/egress/swap_handlers.py
```

Replace with references to "credential store" or "gateway socket resolver".

- [ ] **Step 4: Update tests**

Update `images/tests/test_key_resolver.py` — remove `FileKeyResolver` tests. Update `images/tests/test_credential_swap.py` — update constructor to not use `service_keys_path`.

- [ ] **Step 5: Build egress image**

```bash
make egress
```

- [ ] **Step 6: Commit**

```bash
git add images/egress/ images/tests/test_key_resolver.py images/tests/test_credential_swap.py
git commit -m "fix: remove FileKeyResolver — egress requires gateway socket

The flat .service-keys.env fallback path is removed. Credential
resolution always goes through the gateway socket, which queries
the encrypted credential store."
```

---

## Task 8: Clean up dead code and imports

- [ ] **Step 1: Remove `upsertEnvFile` if unused**

Check if `upsertEnvFile` in `internal/config/init.go` has any remaining callers:

```bash
grep -rn "upsertEnvFile" internal/ cmd/ --include="*.go"
```

If only the definition remains, delete the function.

- [ ] **Step 2: Remove unused `envfile` imports**

Check each modified file for unused `envfile` imports:

```bash
go build ./cmd/gateway/
```

The compiler will flag unused imports.

- [ ] **Step 3: Clean up `ReadExistingKeys`**

This function reads `.env` to find existing provider keys. After migration, keep it temporarily (needed for the migration path in Task 1, Step 6), but add a comment marking it as migration-only:

```go
// ReadExistingKeys returns provider names that have keys in ~/.agency/.env.
// MIGRATION ONLY: Used to detect legacy .env keys and migrate them to the
// credential store during setup. Remove once migration period ends.
func ReadExistingKeys(agencyHome string) []string {
```

- [ ] **Step 4: Build and run full test suite**

```bash
go build ./cmd/gateway/
go test ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/config/init.go
git commit -m "chore: remove dead .env credential code, mark migration helpers"
```

---

## Task 9: End-to-end verification

- [ ] **Step 1: Fresh setup test**

```bash
# Clean slate
rm -rf ~/.agency

# Run setup
agency setup

# Verify credential is in store
agency creds list
# Expected: ANTHROPIC_API_KEY (or whichever provider) with kind=provider, scope=platform

# Verify credential resolves
agency creds show ANTHROPIC_API_KEY
```

- [ ] **Step 2: Agent test**

```bash
agency create test-agent --preset generalist
agency start test-agent
agency send test-agent "What is 2+2?"
# Expected: agent responds (no 401 error)

agency log test-agent
# Expected: LLM calls succeed
```

- [ ] **Step 3: Migration test**

```bash
# Simulate legacy state: key in .env but not in cred store
agency creds delete ANTHROPIC_API_KEY
echo "ANTHROPIC_API_KEY=sk-ant-test123" >> ~/.agency/.env

# Run setup again (should migrate)
agency setup --provider anthropic --api-key sk-ant-real-key

# Verify migration
agency creds list
# Expected: ANTHROPIC_API_KEY in store
```

- [ ] **Step 4: Verify no .service-keys.env references remain**

```bash
grep -rn "service-keys.env" internal/ cmd/ images/ --include="*.go" --include="*.py" | grep -v "_test.go" | grep -v "test_"
# Expected: no matches (or only in docs/comments)
```

- [ ] **Step 5: Commit any fixes from testing**
