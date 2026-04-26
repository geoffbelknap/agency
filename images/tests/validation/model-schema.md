# Model And Schema Validation

Use this lane for Go model changes, OpenAPI changes, strict YAML behavior,
connector schema changes, pack schema changes, and web/client contract drift.

## Automated Lane

```bash
go test ./internal/models ./internal/openapispec ./internal/api ./internal/cli
make web-test-unit
```

If OpenAPI generation changed, regenerate and test the canonical spec through
the repo's established generator command.

## Strict Config Behavior

Manual file edits are appropriate in this lane when the behavior under test is
strict parsing or validation.

Required observations:

- Unknown fields are rejected or surfaced as validation warnings according to
  the owning model contract.
- Required fields fail closed.
- Enum values reject unknown strings.
- Validation errors identify the file/path/operator action that needs repair.

## Connector And Pack Schemas

Required observations:

- Connector source type drives required fields.
- Routes require explicit target or relay semantics.
- Pack agent names are unique.
- Empty or malformed pack definitions fail before deployment side effects.

## API And Web Drift

Required observations:

- `internal/api/openapi.yaml` remains the canonical API spec.
- Web uses REST client behavior aligned with the backend.
- Experimental/internal surfaces remain explicitly gated.
