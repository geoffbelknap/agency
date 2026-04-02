**Date:** 2026-03-20
**Status:** Approved
**Goal:** Eliminate Python as a host dependency; improve config validation performance

## Scope

### In Scope

- Port 109 Pydantic model classes (19 files, ~2,000 lines) to Go structs with `go-playground/validator` tags
- Port schema detection and file validation dispatch from `models/__init__.py` (SCHEMA_MAP, `_detect_schema()`, `validate_file()`) into Go `loader.go`
- Complete the partially-ported Go policy engine — extend exception validation, reconcile hard floor implementations, fill remaining gaps
- Extract shared YAML test fixtures from the pytest suite
- Write Go table-driven tests against those fixtures
- Delete Python model and policy files as each group lands in Go

### Out of Scope

- Container images (body, comms, knowledge, intake, egress) — stay Python, run in Docker
- Go enforcer — already Go
- Agent body runtime — stays Python inside containers
- Pytest tests unrelated to models/policy

### Success Criteria

- `agency` binary validates all YAML config files without Python
- Go policy engine produces identical results to Python engine on shared fixtures
- `python3` is no longer required on the host machine
- No regression in validation error quality (file paths, field context in messages)

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Approach | Bottom-up by dependency order | Small, independently mergeable steps; dependencies always ported first |
| Struct validation | `go-playground/validator` | 29 field validators are mostly regex/enum/range — natural fit for struct tags |
| YAML parsing | `gopkg.in/yaml.v3` | Already in gateway dependency tree |
| Default values | Custom `applyDefaults()` in `loader.go` using struct tags | `go-playground/validator` does not handle defaults; `loader.go` applies `default:"..."` tags via reflection before validation |
| YAML type coercion | Strict types — reject what `yaml.v3` rejects | Pydantic silently coerces `"3"` → `3`, `"true"` → `true`. Go's `yaml.v3` is stricter. If cross-validation reveals real YAML files relying on coercion, add explicit coercion for those specific cases in `loader.go`. Do not replicate Pydantic's full coercion behavior. |
| Test strategy | Shared YAML fixtures, cross-validate during port, delete Python tests after | Catches coercion differences; no permanent dual-maintenance |
| Python deletion | Immediate after each group passes | Each step: port Go code, write Go tests, cross-validate, delete Python file, update `__init__.py` imports |
| Merge strategy | Incremental — one merge per model group | Low risk per step, consistent state at all times |

## Go Package Structure

All new code lives under `agency-gateway/internal/`:

```
agency-gateway/internal/
├── models/
│   ├── org.go              # OrgConfig
│   ├── egress.go           # EgressMode, EgressConfig
│   ├── routing.go          # RoutingConfig, VALID_TIERS
│   ├── policy.go           # PolicyConfig (YAML schema, not engine)
│   ├── hub.go              # HubEntry, HubManifest
│   ├── principal.go        # Principal, ExceptionRoute
│   ├── host.go             # HostConfig, HostCapacity
│   ├── workspace.go        # WorkspaceConfig, ExtraMount
│   ├── comms.go            # ChannelConfig, MessageSchema
│   ├── service.go          # ServiceDef, ServiceGrant
│   ├── capability.go       # CapabilityEntry + variants
│   ├── constraints.go      # ConstraintsConfig, MCPPolicy
│   ├── collective.go       # TeamConfig, Member, HaltAuthority
│   ├── pack.go             # PackDef, PackAgent, PackChannel
│   ├── preset.go           # PresetConfig
│   ├── agent.go            # AgentConfig
│   ├── connector.go        # ConnectorConfig, SourceConfig
│   ├── subscriptions.go    # SubscriptionConfig
│   ├── swarm.go            # SwarmConfig, ManifestEntry
│   ├── validate.go         # Shared validation helpers (regex, enum, error formatting)
│   └── loader.go           # YAML loading with file path context in errors
├── policy/
│   ├── engine.go           # Complete: hierarchy, hard floors, loosening, exceptions
│   ├── defaults.go         # Platform default policy bundle
│   ├── registry.go         # Named policy templates
│   └── routing.go          # Exception routing
```

One Go file per Python model file for clear 1:1 mapping. Existing ad-hoc structs in `orchestrate/*.go` get consolidated here.

### Prerequisite: `go.mod` dependencies

Add `github.com/go-playground/validator/v10` to `agency-gateway/go.mod` before starting Phase 1.

## Model Translation Patterns

### Basic model → Go struct with tags

```python
# Python
class OrgConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")
    version: str = "0.1"
    name: str
    deployment_mode: Literal["standalone", "team", "enterprise"] = "standalone"
```

```go
// Go
type OrgConfig struct {
    Version        string `yaml:"version" validate:"required" default:"0.1"`
    Name           string `yaml:"name" validate:"required"`
    DeploymentMode string `yaml:"deployment_mode" validate:"required,oneof=standalone team enterprise" default:"standalone"`
}
```

Note: the `default:"..."` tag is not handled by `go-playground/validator`. The `loader.go` `applyDefaults()` function reads these tags via reflection and sets zero-value fields before validation runs.

### Pattern mapping

| Pydantic | Go |
|----------|-----|
| `ConfigDict(extra="forbid")` | `yaml.v3` decoder with `KnownFields(true)` in `loader.go` |
| `field_validator` (regex/enum) | `validate:"..."` struct tags |
| `field_validator` (complex, cross-constant) | Custom validators registered on `go-playground/validator` instance |
| `model_validator(mode="after")` | `Validate() error` method on struct, called by `loader.go` after unmarshal |
| `field_validator(mode="before")` | Custom pre-unmarshal logic in `loader.go` — runs before struct population (e.g., `hub.py`'s `sources` coercion). Rare (1 occurrence). |
| `float \| None = None` | `*float64` pointer type |
| `Literal["a", "b"]` | `validate:"oneof=a b"` |
| Nested `BaseModel` | Nested Go struct |
| Default values (`= "0.1"`) | `default:"0.1"` struct tag, applied by `loader.go` before validation |

### Schema detection and file validation dispatch

The Python `models/__init__.py` contains `SCHEMA_MAP`, `_detect_schema()`, and `validate_file()` — non-trivial logic that determines which schema to use based on filename and path context (e.g., `policy.yaml` under `agents/` is `AgentPolicyConfig`, otherwise `PolicyConfig`; similarly for `workspace.yaml`). This logic is ported into `loader.go` as:

```go
func LoadAndValidate(path string) error       // replaces validate_file()
func detectSchema(path string, data map[string]interface{}) (interface{}, error) // replaces _detect_schema()
```

### Error formatting

All validation errors include source file path and field path:

```
org.yaml: field 'deployment_mode': must be one of [standalone, team, enterprise], got 'invalid'
```

Defined in `loader.go` with a consistent pattern.

## Policy Engine Completion

The existing `internal/policy/engine.go` already has significant infrastructure:
- `Engine`, `EffectivePolicy`, `Compute()`, `Validate()`, `Show()`
- `validateHardFloors()` — checks agent-level policy against `HardFloors` map
- `isLoosening()` — compares current vs proposed values using parameter ordering and numeric comparison
- `ValidatePolicy()` — separate pre-write validation checking `network.egress_mode`, `autonomous_max_duration`, and `hard_limits`

### What needs to be done

**1. Reconcile the two hard floor implementations.** `validateHardFloors()` checks the `HardFloors` map (abstract parameter names). `ValidatePolicy()` checks specific nested paths (`network.egress_mode`, `autonomy.autonomous_max_duration`, `hard_limits`). These should be unified into a single authoritative implementation. The Python engine's hard floor list (logging required, constraints read-only, LLM credentials isolated, network mediation required) is the reference.

**2. Extend loosening detection to all chain levels.** `isLoosening()` is already called during `Compute()` via `loadAndMerge()`, but only between org and agent levels. When department and team levels are implemented (currently stubbed), loosening checks must be added at each transition in the chain walk.

**3. Add exception validation (two-key model).** Not yet implemented. Requires both a delegation grant from a higher level AND an exception exercise at the agent level. Both must be present and unexpired.

```go
func validateExceptions(chain []PolicyStep) []PolicyViolation
```

**4. Port defaults and registry.** Platform default policy bundle and named templates ("restrictive", "permissive") as Go maps/constants. The `extends` field resolves against the registry before chain merging.

**5. Add exception routing.** Domain-based routing for exceptions — determines which principal or approval chain handles an exception request based on the policy domain (e.g., network exceptions route to security team, budget exceptions route to finance). Related to the two-key model: routing determines who can issue delegation grants.

## Test Strategy

### Fixture organization

```
agency-gateway/testdata/
├── models/
│   ├── org/
│   │   ├── valid_minimal.yaml
│   │   ├── valid_full.yaml
│   │   ├── invalid_extra_field.yaml
│   │   └── invalid_missing_name.yaml
│   ├── agent/
│   │   └── ...
│   └── ...
├── policy/
│   ├── chains/
│   │   ├── simple_two_level/
│   │   ├── full_five_level/
│   │   ├── loosening_violation/
│   │   ├── hard_floor_violation/
│   │   └── valid_exception/
│   └── defaults/
```

### Test structure

Table-driven tests loading fixtures:

```go
func TestOrgConfig(t *testing.T) {
    tests := []struct{
        file    string
        wantErr string  // empty = expect valid
    }{
        {"valid_minimal.yaml", ""},
        {"invalid_extra_field.yaml", "unknown field"},
    }
    for _, tt := range tests {
        // load fixture, validate, check error
    }
}
```

### Cross-validation

During the port, a shell script runs both Go and Python against the same fixtures and diffs pass/fail results. Deleted when the Python side is removed.

### Coverage

Every `field_validator` (29 total) and `model_validator` (5 total) gets at least one valid and one invalid fixture. 34 validators produce ~70 fixture files.

## Port Order

Each step is one merge. Per step: write Go structs + tests → cross-validate against Python → delete the corresponding Python model file → update `models/__init__.py` (remove import, remove from `__all__`, remove from `SCHEMA_MAP` if present).

### Phase 0 — Setup

| Step | Action |
|------|--------|
| 0a | Add `github.com/go-playground/validator/v10` to `go.mod` |
| 0b | Create `loader.go` with `LoadAndValidate()`, `detectSchema()`, `applyDefaults()`, `KnownFields(true)` decoder |
| 0c | Create `validate.go` with shared validation helpers and error formatting |
| 0d | Extract YAML test fixtures from pytest suite into `testdata/` |

### Phase 1 — Leaf models (no cross-model dependencies)

| Step | File | Models | Complexity |
|------|------|--------|------------|
| 1 | `org.go` | 1 | Trivial — proves the workflow |
| 2 | `egress.go` | 2 | Low — 4 field validators |
| 3 | `routing.go` | 6 | Medium — defines `VALID_TIERS`, 3 field validators |
| 4 | `policy.go` | 5 | Low — 1 field validator |
| 5 | `hub.go` | 4 | Low — 1 field validator |
| 6 | `principal.go` | 5 | Trivial |
| 7 | `host.go` | 2 | Medium — 1 model validator, computed property |
| 8 | `comms.go` | 3 | Medium — 3 field validators, 1 model validator, enums |
| 9 | `service.go` | 6 | Low — 3 field validators |

### Phase 2 — Models with dependencies on Phase 1

| Step | File | Models | Complexity |
|------|------|--------|------------|
| 10 | `workspace.go` | 7 | Medium — 2 field validators |
| 11 | `capability.go` | 11 | Medium — 2 field validators, multiple variants |
| 12 | `constraints.go` | 9 | Low — imports `EgressMode` |
| 13 | `collective.go` | 7 | Medium — 1 field validator |
| 14 | `subscriptions.go` | 5 | Medium — 3 field validators, enums |
| 15 | `pack.go` | 6 | Medium — 1 field validator, 1 model validator (no_duplicate_names) |
| 16 | `preset.go` | 7 | Medium — 2 field validators, imports `VALID_TIERS` |
| 17 | `agent.go` | 9 | High — 2 field validators, most fields |
| 18 | `connector.go` | 10 | High — 1 field validator, 2 model validators (cross-field) |
| 19 | `swarm.go` | 4 | Low — imports `ComponentLimits` |

### Phase 3 — Policy engine and schema dispatch

| Step | Component | Description |
|------|-----------|-------------|
| 20 | `engine.go` | Reconcile hard floor implementations, integrate loosening into chain walk, add exception validation (two-key model) |
| 21 | `defaults.go` | Platform default policy bundle |
| 22 | `registry.go` | Named policy templates |
| 23 | `routing.go` | Exception routing — domain-based routing for delegation grants and approval chains |
| 24 | `loader.go` | Port `_detect_schema()` and `validate_file()` from `models/__init__.py`, completing the schema dispatch logic |

### Phase 4 — Cleanup and consolidation

| Step | Action |
|------|--------|
| 25 | Delete `agency_core/models/__init__.py` (all individual model files already deleted in Phases 1-2) |
| 26 | Delete `agency_core/policy/` directory (ported in Phase 3) |
| 27 | Remove model/policy dependencies from `pyproject.toml` |
| 28 | Verify no remaining Python runtime imports of deleted modules |
| 29 | Consolidate ad-hoc Go structs in `orchestrate/*.go` — move model-type structs (`PackDef`, `AgentDetail`, `ConstraintsSummary`, `TaskSummary`, `DeployResult`, `HaltRecord`, `StartResult`) to `internal/models/`, update imports. Leave orchestration-internal structs (options, callbacks) in place. |

## Current Integration Point

The Go gateway does **not** call Python via subprocess for model validation. The gateway orchestration is already 100% Go — it creates containers, mounts files, and manages lifecycle directly via the Docker SDK. Python model validation is invoked only by:

1. The Python test suite (`pytest`)
2. The `validate_file()` function used during `agency setup` and constraint delivery (Phase 3 of the start sequence)

The cutover happens when `loader.go`'s `LoadAndValidate()` replaces `validate_file()`. The gateway's start sequence (`internal/orchestrate/start.go`) already handles constraint delivery in Go — it just needs to call `models.LoadAndValidate()` instead of trusting the file is valid.

## Risk Mitigation

| Risk | Impact | Mitigation |
|------|--------|------------|
| Pydantic implicit type coercion | Go rejects YAML that Python accepted | Policy: strict types by default. `yaml.v3` handles standard YAML type resolution (e.g., `true`/`yes`/`on` all become `bool`). If cross-validation reveals real YAML files relying on Pydantic-specific coercion (e.g., `"3"` → `int`), add targeted coercion in `loader.go` for those cases only. Do not replicate Pydantic's full coercion. |
| Validation error message regression | Operators depend on error format | Consistent format defined in `loader.go` (`file: field 'x': message`); same information, not identical wording |
| Cleanup breaks Python imports | Runtime failure | Verify no runtime Python code path imports models before deleting; Go gateway already doesn't call Python for orchestration. The `validate_file()` cutover is explicit (Phase 3 step 24). |
| Policy engine behavioral divergence | Subtle semantic differences | Edge case fixtures in `policy/chains/`; both engines must agree on every fixture before Python is deleted. Reconcile the two existing hard floor implementations before extending. |
| Blocking feature work | Port touches gateway files | Incremental merges keep window small; each merge is self-contained |
| Struct consolidation regressions | Moving structs from `orchestrate/` to `models/` breaks imports | Phase 4 step 29 is scoped to model-type structs only. Each moved struct gets a compile check (`go build`) before merge. Orchestration-internal types stay in place. |
