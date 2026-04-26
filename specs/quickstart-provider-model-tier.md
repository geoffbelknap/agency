# Quickstart Provider Model Tier

Status: Implemented.

## Problem

`agency quickstart --provider <provider>` installed one provider and its routing
config, but the agent could start with a different provider's default model and
get a 401 because only the selected provider's credentials were available.

Three bugs combine:

1. **Agent creation skips `model_tier`** — `agent.go:172-191` copies `expertise` and `responsiveness` from preset but not `model_tier`. The preset declares `model_tier: frontier` but the agent.yaml never gets it.
2. **`resolveModelTier()` is broken** — `start.go:529-584` walks `providers` as an array and looks for `tier` fields inside model objects. But routing.yaml has `providers` as a map and tiers are a separate top-level section (`tiers:`). The function never matches anything.
3. **Hardcoded provider-model default** — `start.go:249` falls back to a
   provider-specific model when tier resolution fails. With only another
   provider's credentials, this guarantees a 401.

## Fix

### A. Copy `model_tier` in agent creation

`internal/orchestrate/agent.go` copies `model_tier` to the generated `agent.yaml`, same pattern as expertise and responsiveness.

### B. Rewrite `resolveModelTier()`

`internal/orchestrate/start.go` resolves tiers against `routing.yaml`, prefers the configured `llm_provider`, and verifies provider credentials before selecting a model:

1. Unmarshal routing.yaml into `models.RoutingConfig`
2. Call `rc.ResolveTier(tier, nil)` — returns `(*ProviderConfig, *ModelConfig)` or nil
3. Check credential availability for the resolved provider (keep existing `hasCredential` helper)
4. Return the model alias if credentials are available, empty string otherwise

The `RoutingConfig` struct and `ResolveTier()` method already exist in `internal/models/routing.go` and work correctly. The struct uses typed fields (`map[string]ProviderConfig`, `TierConfig` with named tier fields, etc.).

### C. Fix the default fallback

`internal/orchestrate/start.go` no longer falls back to a provider-specific
model when tier resolution fails:

1. Resolve `model_tier` from agent.yaml (already handled by existing code at lines 253-258, once bug A is fixed)
2. If no `model_tier` in agent.yaml, resolve the default tier from routing settings (`settings.default_tier`, defaults to "standard")
3. If nothing resolves, fail startup with a clear setup/routing error instead of silently picking a model that might not have credentials

## Fail-Closed Behavior

When no model resolves (no tiers have credentialed providers), the agent fails to start with a clear error message. Starting with a broken or empty model is not acceptable — this is the correct fail-closed behavior per ASK Tenet 4.

## Credential Store Ordering

`resolveModelTier()` runs in `phase3Constraints()`. The `hasCredential` helper checks both env vars and `ss.CredStore`. Verify that the credential store is initialized before phase 3 runs — if it isn't, credential-store-only keys (not in env vars) will be missed and tier resolution will silently fail.

## Non-Goals

- No changes to quickstart command
- No changes to routing.yaml format or tier structure
- No changes to preset files (they already declare model_tier correctly)
- No provider-specific logic — the tier system abstracts this
