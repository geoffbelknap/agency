# Hub Upgrade Routing Merge

## Problem

`syncRouting()` in `internal/hub/hub.go:174` overwrites `~/.agency/infrastructure/routing.yaml` with the hub cache version on every `hub upgrade`. This clobbers provider entries added by `hub install` for non-default providers (e.g., a third-party OpenAI-compatible provider).

The hub base ships defaults for the four first-class providers (Anthropic, OpenAI, Google/Gemini, Ollama). These are authoritative — hub updates to defaults (new models, corrected pricing) should always take effect. Non-default providers installed via `hub install` are the entries at risk of being lost.

Operators who want to customize default provider settings use `routing.local.yaml` or create a custom provider hub component.

## Routing File Ownership

| File | Owner | Written by |
|------|-------|------------|
| `routing.yaml` | System | Hub upgrade, hub install, gateway automation |
| `routing.local.yaml` | Operator | Manual edits, never touched by automation |

Read-time merge in `loadRoutingConfig()` (`handlers_routing.go:108`) overlays `routing.local.yaml` on top of `routing.yaml`, with local winning on conflicts. This behavior is unchanged by this spec.

## Merge Strategy

`syncRouting()` changes from overwrite to merge:

1. **Read hub cache base** — `hub-cache/<source>/pricing/routing.yaml`
2. **Validate** — `validateRoutingConfig()` (unchanged)
3. **Unmarshal** — parse validated hub base into a routing config map
4. **Identify default providers** — extract the set of provider names from the hub base's `providers` map (Anthropic, OpenAI, Google/Gemini, Ollama)
5. **Query installed providers** — `m.Registry.List("provider")`
6. **Filter to non-defaults** — skip any installed provider whose name matches a key in the hub base providers map
7. **Merge non-defaults** — for each remaining provider, read its YAML from `Registry.InstanceDir(name)/provider.yaml` and merge into the base config using the same field-level merge logic as `MergeProviderRouting` (providers, models, tiers sections)
8. **Validate merged output** — `validateRoutingConfig()` on the final merged config (catches auth_env allowlist changes since install time)
9. **Marshal and write** — atomic write to `infrastructure/routing.yaml`

### Conflict Rules

- **Default providers** (in hub base): hub base is authoritative, always overwrites. Operator customizations go in `routing.local.yaml` or a custom hub component.
- **Non-default providers** (installed via `hub install`): preserved across upgrades by re-merging from instance dir.
- **Model alias collision** between hub base and non-default provider: non-default provider wins (operator explicitly installed it).
- **Removed providers**: already gone from the registry, so step 6 skips them. No zombie entries.

### Validation

Hub base is validated at step 2 (existing behavior). After the merge in step 7, run `validateRoutingConfig` on the final merged output before writing to disk. This closes the TOCTOU window between install-time and upgrade-time validation — if the `allowedAuthEnvVars` allowlist changes between install and upgrade, a previously-valid provider entry gets caught.

## Implementation

### Changes

**`internal/hub/hub.go` — `syncRouting()`**

Replace the current body (read → validate → WriteFile) with the merge algorithm above. Extract the merge loop into a helper or inline it — the function is small enough that inline is fine.

The function already has access to `m.Registry` and `m.Home`, so no signature change needed.

**`internal/hub/provider.go` — extract merge logic**

`MergeProviderRouting` currently loads routing.yaml from disk, merges, and writes. Extract the pure merge step (merge provider config into an existing map) into an unexported helper so `syncRouting` can call it without the load/write side effects:

```go
// mergeProviderInto merges a single provider's routing block into cfg.
// cfg is modified in place.
func mergeProviderInto(cfg map[string]interface{}, providerName string, providerData []byte) error
```

`MergeProviderRouting` becomes: load → `mergeProviderInto` → write. No behavior change for the install path.

### Test

Add `TestUpgradePreservesInstalledProviders` to `hub_test.go`:

1. Set up a hub cache with the default routing.yaml (Anthropic, OpenAI, Gemini, Ollama)
2. Install a non-default provider (e.g., "together-ai") via `MergeProviderRouting`
3. Verify the provider exists in routing.yaml
4. Run `Upgrade(nil)` (full upgrade with managed file sync)
5. Assert: together-ai provider, its models, and its tier entries still exist in routing.yaml
6. Assert: default providers match the hub cache version (not stale)

### Existing Test

`TestUpgradeAllSyncsManagedFiles` (`hub_test.go:68`) tests ontology sync but not provider preservation. The new test covers the gap.

## Ordering Constraint

`WriteSwapConfig` (hub.go:747) runs after `Upgrade()` and reads `routing.yaml` from disk to generate `credential-swaps.yaml` for the egress proxy. Since `syncRouting` writes routing.yaml before `Upgrade()` returns, the ordering is correct — swap config sees the merged output. No change needed, but this dependency must be preserved: sync writes first, swap reads after.

## Non-Goals

- No changes to `routing.local.yaml` handling
- No changes to `loadRoutingConfig()` read-time overlay
- No changes to `MergeProviderRouting` install-time behavior (beyond extracting the helper)
- No fragment directory (`routing.d/`) — rejected in favor of the simpler merge approach
- No changes to how default providers are managed — hub base is authoritative for them
