# Security & Governance Family

A short map of the security-adjacent packages in `internal/`. Use this when
deciding where new types or logic belong. The canonical home for shared
type contracts (anything multiple subsystems agree on) is this package
(`internal/security/`); engines that consume those contracts live in their
own packages.

## The family

| Package | Role | Holds |
|---|---|---|
| [`internal/security/`](.) | **Canonical type contracts.** No logic, no I/O. Just the types every other governance package agrees on. | `Decision`, `Finding`, `Mutation`, `ApprovalStatus`, `PolicyStepStatus`, `PolicyExceptionStatus`, `AuthorityExecutionStatus`, `RiskLevel`, `ConsentRequirement`, `IsSecurityAuditEvent` |
| [`internal/policy/`](../policy/) | **Policy engine.** Resolves the rule chain (org → department → workspace), evaluates against requests, returns a `Decision`. | `engine.go`, `routing.go`, `defaults.go` (hard floors), `registry.go`, testdata |
| [`internal/consent/`](../consent/) | **Cryptographic consent tokens.** Ed25519-signed CBOR tokens that gate specific tool calls on prior human consent. The enforcer's verification hook lives here. | `consent.go` (token format, verification, replay-protection), `bootstrap.go` (signing key lifecycle) |
| [`internal/audit/`](../audit/) | **Audit summarization + pricing.** Reads the audit log, produces operator-facing rollups; holds per-model `ModelPrice` tables for cost attribution. | `summarizer.go`, `pricing.go` |
| [`internal/principal/`](../principal/) | **Transport-layer helper, not a governance engine.** Typed `context.Context` key for carrying an authenticated principal across HTTP handlers. Lives in its own package only to break an import cycle between handler subpackages. | `context.go` |

## Where things go

- **Adding a new type that more than one governance package needs?** Put
  it in `internal/security/contracts.go`. Other packages should import
  it as a one-way dependency (engine → security, never the reverse).
- **Adding policy logic (rules, exceptions, evaluation)?** That belongs
  in `internal/policy/`. The shared types it returns (decisions, statuses)
  come from `internal/security/`.
- **Adding consent verification logic?** That belongs in
  `internal/consent/`. The `ConsentRequirement` type itself is in
  `internal/security/` because the connector loader and enforcer middleware
  also use it.
- **Adding audit projection / summarization?** That belongs in
  `internal/audit/`. Raw audit emission happens at the call site (each
  handler writes its own audit record); this package is for reading and
  rolling up.
- **Adding HTTP-layer principal plumbing?** That belongs in
  `internal/principal/`. Anything more substantial (registry CRUD,
  trust calibration, authority hierarchy) lives elsewhere — see
  `internal/registry/` for principal CRUD and `internal/api/middleware_auth.go`
  for bearer-auth middleware.

## Why these packages are separate

The split is deliberate, driven by ASK Tenets 1 and 4:

- **Tenet 1 (constraints external).** The enforcer's trust surface is the
  set of code that runs inside the mediation boundary. Every line in
  `internal/security/contracts.go` is part of that boundary. Keeping it as
  pure types — no I/O, no state, no decisions — keeps the trust surface
  small and reviewable.
- **Tenet 4 (least privilege).** Policy, consent, and audit each handle
  different scopes of authority. Splitting them keeps each package's
  responsibility tight and prevents one engine from accidentally
  short-circuiting another (e.g. policy can't issue a consent token).

## Cross-references

- [ASK Framework](https://github.com/geoffbelknap/ask) — the 27 tenets
  these packages enforce.
- [`docs/specs/consent-tokens-design.md`](../../docs/specs/consent-tokens-design.md)
  — design rationale for the consent token primitive.
- [`docs/specs/policy-framework.md`](../../docs/specs/policy-framework.md)
  — design rationale for the policy engine.
- [`docs/specs/credential-architecture.md`](../../docs/specs/credential-architecture.md)
  — how credentials interact with the audit and consent paths.
- [`CLAUDE.md`](../../CLAUDE.md) — project-level invariants (Current
  Contracts, Operational Rules).
