# Policy And Governance Validation

Use this lane for policy resolution, trust, principals, teams, authority,
exceptions, and capability scoping.

## Automated Lane

```bash
go test ./internal/policy ./internal/models ./internal/api ./internal/orchestrate
make web-test-unit
```

## Policy Expectations

Required observations:

- Policy resolution follows the explicit hierarchy.
- Agent-level policy cannot loosen a platform, org, department, or team hard
  floor.
- Exceptions are visible, auditable, scoped, and expire.
- Denials identify the policy source that caused the decision.

## Trust And Authority

Required observations:

- Trust signals are visible to the operator.
- Manual trust changes require an explicit operator action and reason.
- Function or authority agents do not gain write access to another agent's
  workspace unless explicitly mediated.
- Halt authority remains explicit, reasoned, and audited.

## Teams

Required observations:

- Team membership is visible through gateway-backed surfaces.
- Coordinator/member roles do not erase individual agent boundaries.
- Shared context remains mediated.

## Manual Probe Shape

Prefer gateway/admin surfaces and web screens over direct file edits. Direct
file mutation is reserved for schema validation in [model-schema.md](model-schema.md).
