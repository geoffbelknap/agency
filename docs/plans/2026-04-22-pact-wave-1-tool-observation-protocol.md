# PACT Wave 1 #2 — Structured Tool Observation Protocol

## Reference

- Spec: `docs/specs/pact-governed-agent-execution.md` → Wave 1 #2
- Wave: Wave 1 — Harness Foundations, item #2
- Builds on: Wave 1 #1 (`ExecutionState`, merged in #255)
- Unblocks: Wave 2 #5 (recovery state machine), Wave 4 #3 (`external_side_effect` evaluator)

## Objective

Promote `ToolObservation` from its Wave 1 #1 minimal placeholder (`tool`, `status`,
`summary`, `observed_at`) into the full protocol the spec specifies: status,
data, provenance, timestamps, errors, retryability, side effects, and evidence
classification. Thread the protocol through the three existing tool-recording
call sites in `body.py` so every tool-produced evidence entry inherits the
shape, while preserving byte-for-byte backwards compatibility in downstream
projections (verdict payloads, frontmatter, audit report).

## Why

Three Wave 2+ items are blocked on this protocol:

- Wave 2 #5 (recovery state machine) needs **retryability** to decide retry
  vs escalate.
- Wave 4 #3 (`external_side_effect` evaluator) needs **side-effect class** to
  validate external mutations.
- Wave 2 #4 (general pre-commit evaluator) needs **evidence classification**
  to know which observations count toward contract satisfaction.

Today, tool results flow through `EvidenceLedger.record_tool_result(tool, ok,
metadata)` — a `bool` plus an arbitrary dict. Downstream consumers have to
infer classifications from prose or peek at `metadata`. That inference is the
abstraction we called out in the spec as the reason NOT to build
`external_side_effect` before this protocol exists.

## Scope (in this PR)

### Extend `ToolObservation`

In `images/body/pact_engine.py`, extend `ToolObservation` with the following
fields. Keep field names identical to the list below so the spec's prose maps
cleanly onto the type.

- `tool: str` — unchanged
- `status: ToolStatus` — enum; see below
- `data: dict` — structured tool output payload (may be empty)
- `provenance: ToolProvenance` — enum; see below
- `producer: str` — specific tool/handler name (e.g., `provider-web-search`,
  `write_file`, `runtime:artifact`)
- `started_at: datetime | None` — call start; may be `None` if unknown
- `observed_at: datetime` — completion/observation time (existing field; now
  required)
- `error: ToolError | None` — populated only when `status == ToolStatus.error`
- `retryability: Retryability` — enum; see below
- `side_effects: SideEffectClass` — enum; see below
- `evidence_classification: tuple[str, ...]` — ordered labels linking the
  observation to contract evidence requirements (e.g.,
  `("current_source", "source_url")`, `("artifact_path",)`,
  `("validation_result",)`)
- `summary: str` — unchanged, optional

Enum values (keep minimal; do not invent extras):

- `ToolStatus` (StrEnum): `ok`, `error`, `partial`, `unknown`
- `ToolProvenance` (StrEnum): `mediated`, `provider`, `runtime`, `unknown`
- `Retryability` (StrEnum): `retry_safe`, `retry_with_backoff`,
  `not_retryable`, `unknown`
- `SideEffectClass` (StrEnum): `read_only`, `local_state`, `external_state`,
  `unknown`

Supporting dataclass:

- `ToolError` — `message: str`, `kind: str` (one of
  `timeout`/`permission_denied`/`not_found`/`validation`/`transient`/`unknown`),
  `retry_after_ms: int | None`

All types must serialize deterministically via `to_dict()`, and
`ExecutionState.to_dict()` must round-trip the full protocol.

### Projection helper

Add `ExecutionState.record_tool_observation(obs: ToolObservation) -> None`
(or a clearly-named free function in `pact_engine`) that:

1. Appends `obs` to `ExecutionState.tool_observations`.
2. Projects `obs` into the equivalent legacy `EvidenceLedger` entries, using
   `evidence_classification` to choose which `record_*` call(s) to make:
   - `"tool_result"` or always present → `record_tool_result(tool, ok, metadata)`
     with `ok = (status == ToolStatus.ok or status == ToolStatus.partial)` and
     `metadata` derived from `data`
   - `"current_source"` → also `observe("current_source", producer=...)`
   - `"source_url"` → also `record_source_url(url, producer=...)` for each URL
     found in `data.get("source_urls", [])`
   - `"artifact_path"` → also `record_artifact_path(path, metadata=...)`
   - `"changed_file"` → also `record_changed_file(path, producer=...)`
   - `"validation_result"` → also `record_validation_result(...)`
3. Bumps `ExecutionState.updated_at`.

The helper is the **canonical** path for new observations. Existing direct
`ledger.record_*` calls remain functional but are no longer used by `body.py`.

### Migrate the three call sites in `body.py`

The existing sites at approximately:

- `body.py:3412` — local tool call wrapper
- `body.py:3469` — provider tool events
- `body.py:3487` — artifact recording

Each site constructs a `ToolObservation` with the appropriate fields and
calls `self._execution_state.record_tool_observation(obs)`. Remove the
previously-duplicated direct `ledger.record_*` calls at these sites (they are
replaced by the projection helper).

For fields the call site cannot determine today:

- `retryability` → `Retryability.unknown`, with TODO tag referencing Wave 2 #5
- `side_effects` → for provider tools of unknown class, `SideEffectClass.unknown`,
  with TODO tag referencing Wave 4 #3. For local `write_file` → `external_state`
  (file system is external to the body container); for `execute_command` for
  tests/builds → `external_state` when the command mutates filesystem,
  `read_only` otherwise — best-effort is fine; do not invent a classifier.
- `data` → populate with the fields the call site already has (URLs, path,
  validation command text, exit code)

### Diff-equivalence guarantee

Add a targeted test fixture that, for each of the three call sites, runs the
same underlying tool-call scenario twice:

1. Against a freshly-constructed `ExecutionState` using the new projection
   helper.
2. Against a bare `EvidenceLedger` using the legacy `record_*` API directly,
   matching the same producer / ok / metadata / urls / paths.

Assert `EvidenceLedger.to_dict()` on both is byte-for-byte identical. This is
the ironclad backwards-compat check — if it fails, the PR is rejected.

### Spec Checkpoint update

Update the "### Execution State Type" subsection added in Wave 1 #1 to:

- Move `tool_observations` out of the placeholder-fields list and into the
  populated-fields list.
- Add a short paragraph describing the `ToolObservation` protocol (fields and
  enum classes), and explicitly note that `retryability` and `side_effects`
  are classified but not yet consumed by any evaluator (those are Wave 2 #5 /
  Wave 4 #3).

### Tests

New file `images/body/test_tool_observation_protocol.py`:

1. `ToolObservation` construction with the full field set.
2. `to_dict` round-trip preserves every field including enum values.
3. Defaults produce `unknown` for provenance / retryability / side effects
   when not supplied.
4. `ToolError` carries `kind` and `message`; `retry_after_ms` is optional.
5. `evidence_classification` is an ordered tuple (not a set).
6. `ExecutionState.record_tool_observation` appends to `tool_observations`
   and bumps `updated_at`.
7. Projection helper: `ok` observation with `evidence_classification =
   ("current_source", "source_url")` produces a ledger whose `to_dict`
   includes the `tool_result`, `observed`, and `source_urls` entries
   identical to direct legacy `record_*` calls.
8. Diff-equivalence fixture for the three body.py call sites (see above).

## Non-Scope

- **Wave 2 #5** (recovery state machine) — classifications are surfaced but no
  runtime reasons about `retryability` yet. The existing retry path in
  `body.py` (`_work_contract_retry_sent`) remains unchanged.
- **Wave 4 #3** (`external_side_effect` evaluator) — `side_effects` is
  classified but no evaluator reasons about it yet.
- **Wave 2 #4** (general pre-commit evaluator) — existing contract evaluators
  (`current_info`, `file_artifact`, `code_change`, `operator_blocked`)
  continue to consume the legacy `EvidenceLedger` projection. Do NOT rewire
  them to consume `evidence_classification` directly.
- **Wave 4 #2** (durable typed evidence ledger as a standalone resource) —
  in-memory `ExecutionState.tool_observations` is sufficient.
- Rewriting the `EvidenceLedger.record_*` API.
- Adding new enum values beyond the ones named above. If Codex believes a
  new value is needed, stop and report.
- OpenAPI, web UI, feature registry, audit-report endpoint shape.

## Acceptance Criteria

1. `ToolObservation` carries the full field set named in Scope with
   deterministic `to_dict()`.
2. Four enum types (`ToolStatus`, `ToolProvenance`, `Retryability`,
   `SideEffectClass`) and `ToolError` dataclass exist, each with a docstring
   naming what it classifies.
3. `ExecutionState.record_tool_observation` exists, appends to
   `tool_observations`, projects into the legacy `EvidenceLedger`, and bumps
   `updated_at`.
4. All three existing tool-recording call sites in `body.py` produce
   `ToolObservation`s via the helper. The previously-duplicated direct
   `ledger.record_*` calls at these sites are removed. The same underlying
   observations end up in both `ExecutionState.tool_observations` and the
   legacy `EvidenceLedger` via the helper.
5. **Diff-equivalence holds**: for each of the three call-site scenarios, the
   new path produces `EvidenceLedger.to_dict()` output byte-for-byte identical
   to the legacy path. Regression fixture asserts this.
6. Every public API shape is preserved exactly: `pact_verdict` signal payload,
   result frontmatter, `/api/v1/agents/{name}/pact/runs/{taskId}`,
   `/audit-report`, `/audit-report/verify`, admin audit enrichment.
   Audit-report hash is stable across repeated reads of the same run.
7. `images/body/test_tool_observation_protocol.py` exists with the eight test
   cases listed above.
8. Call sites with unknown classifications carry `TODO(Wave 2 #5)` or
   `TODO(Wave 4 #3)` comments pointing at the future wave that refines the
   default.
9. `pytest images/tests/` and `go build ./cmd/gateway/` both succeed.
10. Spec Checkpoint "### Execution State Type" subsection updated with
    `ToolObservation` protocol description.

## Review Gates

**Reject** the PR if:

- Wave 2 / Wave 4 / Wave 5 scope crosses in (recovery logic, side-effect
  evaluator, durable ledger promotion, signed reports).
- Public API shapes change (verdict, frontmatter, PACT run projection, audit
  report, verify, admin audit enrichment).
- Audit-report hash becomes unstable.
- Diff-equivalence fixture fails for any of the three call sites.
- New evaluators or contract kinds are introduced.
- Existing contract evaluators (`current_info`, `file_artifact`,
  `code_change`, `operator_blocked`) start consuming
  `evidence_classification` instead of their existing evidence checks.
  Keep them on their legacy paths; Wave 2 #4 does that consolidation.
- Enum values are expanded beyond the minimal sets listed in Scope.
- A call site self-reports its own provenance as `mediated` or classifies its
  own side effects without the actual mediation metadata — classification
  must come from the runtime path, not from the tool's own prose.

**Ask for changes** (not reject) if:

- Protocol fields or enum types lack docstrings.
- Projection helper has more than one responsibility.
- Call sites lack TODO annotations pointing at the future wave that will
  refine `unknown` defaults.
- Enum choices could be ambiguous or require operator documentation.

## Files Likely To Touch

- `images/body/pact_engine.py` — extend `ToolObservation`, add enums +
  `ToolError`, add projection helper on `ExecutionState`
- `images/body/body.py` — three call sites migrated to use the projection
  helper
- `images/body/test_tool_observation_protocol.py` — new
- `images/body/work_contract.py` — re-export the new types
- `docs/specs/pact-governed-agent-execution.md` — Checkpoint subsection
  update only

## ASK Compliance

This PR touches observability and evidence representation, which is sensitive
to several ASK tenets. All are preserved or improved.

- **#1 external enforcement / #3 complete mediation / #4 fail-closed** —
  observations are work state, not enforcement state. Mediation paths
  (gateway/enforcer) are not modified. Fail-closed startup and runtime
  behavior unchanged.
- **#2 audit** — backwards compatibility in the legacy `EvidenceLedger` dict
  projection is enforced by the diff-equivalence fixture. `pact_verdict`
  signal, audit JSONL, result frontmatter, audit-report hash, and admin
  audit enrichment remain byte-for-byte identical for unchanged underlying
  tool calls.
- **#5 runtime is a known quantity** — typed observations with explicit
  status / provenance / retryability / side-effect / evidence classification
  make runtime tool behavior meaningfully more inspectable for operators.
  Net ASK gain.
- **#7 least privilege / #8 bounded operations** — no new capabilities, no
  new budget exemptions. Retry loops continue to be bounded by existing
  body logic; the `retryability` classification is advisory, not
  authoritative.
- **#24 instructions only from verified principals** — `provenance` is an
  explicit field with values `{mediated, provider, runtime, unknown}`.
  Observations with `provenance == provider` or `unknown` remain *data*;
  they do not grant instructions. This is the foundation for stricter XPIA
  defense in Wave 2 and later.
- **#25 identity mutations auditable** — observations are per-task. No
  identity writes.

**Forward-looking ASK notes for downstream waves:**

- **Wave 2 #5 recovery state machine**: `retryability` decisions must remain
  **runtime-owned**, not agent-directed. An agent-authored claim of
  `retry_safe` does not grant retry authority; classification must come from
  mediation metadata.
- **Wave 4 #3 external_side_effect evaluator**: `side_effects` classification
  **must not be agent-self-reported**. Classification comes from mediation
  metadata (which tool was called, what it did at the runtime boundary), not
  from model-authored claims.

Both of these are forward guidance for later wave briefs; neither imposes
additional work in this PR.

## Out-of-band Notes For Codex

- Keep enum values minimal as listed. Do not invent states for situations
  that don't exist yet. If you believe a new value is needed, stop and
  report.
- `retryability` and `side_effects` for existing call sites should default
  to `unknown` / `read_only` (or best-effort where obvious) with a TODO tag.
  Refining defaults is future-wave work; this PR just threads the protocol.
- `EvidenceLedger` dict projection byte-equality is the canonical
  backwards-compat check. The diff-equivalence fixture is not optional.
- If a protocol-design decision needs a product call (e.g., whether to
  include a field, what its enum values should be), stop and report rather
  than guessing.
- Commit style: plain commit title, no Co-Authored-By trailer. Match repo
  convention.
- PR target: `main` (Wave 1 #1 is already merged; no stacked dependency).
