# ASK Security Validation

Use this lane for security-sensitive changes. ASK tenets are non-negotiable:
external enforcement, complete auditability, complete mediation, explicit least
privilege, and visible/recoverable trust boundaries.

## Baseline

```bash
go test ./...
./scripts/dev/python-image-tests.sh body
agency admin doctor
```

Expected:

- No security or runtime failures.
- Any backend hygiene warnings are scoped to the selected backend.

## Enforcement And Mediation

Validate that an agent cannot bypass the enforcer or mutate its own trust
boundary.

Required observations:

- Agent outbound network traffic goes through mediated egress.
- Enforcer remains internal-only and does not attach to operator-facing
  networks.
- Agent constraints and identity are delivered as operator-owned inputs.
- Agent-authored content cannot directly mutate durable preferences, identity,
  or policy.

Use backend-native probes only as adapter evidence. The product contract must
still be visible through `agency admin doctor` and runtime validate.

## Credentials

Validate through gateway-managed credential surfaces rather than direct file
editing.

Expected:

- Real provider keys are not present in agent runtime environments.
- Runtime sees only scoped tokens or mediated service grants.
- Service URL and scope mismatches fail closed.
- Revocation removes future access.

## Audit

Required observations:

- Agent lifecycle events are written.
- Operator actions are written.
- Mediation and policy decisions are written.
- Denials include enough context to reconstruct why the boundary held.

Suggested checks:

```bash
agency admin doctor
agency admin audit stats
agency log <agent> --tail 50 --verbose
```

## Memory Boundary

Durable memory validation belongs here when memory affects preferences,
identity-shaped behavior, or long-lived procedure.

Required observations:

- Agent output creates proposals, not direct durable preference mutation.
- Preference-affecting memory requires review.
- Rejected or revoked memory is visible and recoverable.

Use [knowledge-memory.md](knowledge-memory.md) for the exact graph surfaces.
