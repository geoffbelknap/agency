# Consent Tokens: Bounded Tool Authority Without In-Enforcer State Machines

**Date:** 2026-04-11
**Status:** Draft
**Scope:** Introduces consent tokens — a cryptographic primitive that lets packs gate specific agent tool calls on explicit human consent, without adding a state machine, approval store, or record-type registry to the enforcer. Defines the token format, the issuer contract, the enforcer validation hook, the `requires_consent_token:` tool-schema directive, and the deployment-scoped signing key story. Does not define any specific issuer's UX or any specific pack's operation kinds.

## Problem

ASK tenets 15 (trust earned, no self-elevation) and 17 (instructions only from verified principals) require that consequential agent operations — especially identity mutations — are gated on explicit human consent. A community administrator deciding to remove a member, an admin adding a document to a managed-access set, a vote-close meeseek executing a workspace invite — all of these are cases where the agent's body is the wrong place to make the authorization decision, because a compromised body can make arbitrary decisions.

The naive solution is to add an approval state store to the enforcer: pending approvals, state machines, expiration sweepers, record-type extensibility. This works, but it **inflates the enforcer's trust surface** in a way that's incompatible with ASK tenet 1 (constraints external and inviolable — every line of logic inside the enforcement boundary is a potential bug in the boundary). The enforcer today is deliberately small, deliberately stateless except for rate limits and trajectory windows, and deliberately credential-free. A full approval state machine would be qualitatively different from what the enforcer does today.

This spec introduces a different primitive: **consent tokens**. Instead of the enforcer tracking "is there an approved record for this operation," consent becomes a **signed artifact** that the agent carries from the point where consent was given to the point where the gated tool call is made. The enforcer's new responsibility is reduced to signature verification, timestamp checking, and a small consumed-nonce set for replay protection. No state machines. No extensibility registry. No mutable approval state inside the boundary.

## Goals

1. Define a **consent token format** that binds a specific operation (kind, target) to a specific consent event (issuer, witnesses, timestamp, nonce) with cryptographic integrity.
2. Define an **issuer contract**: what containers can produce tokens, how they hold signing keys, and what invariants they must satisfy before signing.
3. Define an **enforcer validation hook** that gates tool calls declaring `requires_consent_token:` on valid, unexpired, target-matching, single-use tokens.
4. Preserve the enforcer's **credential-free property** by using asymmetric signing (Ed25519): issuers hold private keys, the enforcer holds only public verification keys.
5. Keep the enforcer's new state footprint **small, bounded, and aggressively prunable** — a consumed-nonce set with a bounded window, not a state machine.
6. Integrate with Agency's existing audit, credstore, and deployment primitives without growing any of them beyond their current responsibilities.

## Non-Goals

- **No approval state machine.** There is no in-enforcer concept of "awaiting second approval" or "pending execution." Whatever state is needed to collect consent lives in the issuer container, not in the enforcer.
- **No operator revocation of issued tokens.** Once a token is issued and within its window, the only way to prevent its use is to halt the executing agent (Agency already supports this via `agency agent halt`). Tokens expire; they do not get cancelled. If you need cancelable consent, use Agency's existing halt primitives and accept the cost of an explicit halt-and-resume cycle.
- **No token storage outside the issuer → agent → enforcer chain.** Tokens are not persisted by the enforcer (beyond the replay-protection nonce set), not stored in the knowledge graph, not exported in deployment bundles, not logged with full contents. They are in-flight artifacts with a short TTL.
- **No pack-independent operation-kind registry.** Packs choose their own `operation_kind` strings. The enforcer treats these as opaque — it compares strings for equality but does not understand their semantics.
- **No issuer-side revocation coordination.** If two issuer containers exist in a single deployment (hypothetically), they don't coordinate. An issuer issues tokens it's authorized to issue. Coordination, if needed, is outside this primitive.
- **No credentialing the enforcer.** The enforcer remains credential-free. It holds only the public verification keys, which are not secrets.

## Design

### 1. Token format

A consent token is a compact binary structure (proposed: [CBOR](https://datatracker.ietf.org/doc/html/rfc8949) with a fixed schema) wrapped in a detached Ed25519 signature. The serialized token is base64url-encoded for transit through JSON tool-call arguments.

```
ConsentToken {
    version:           u8                 // currently 1
    deployment_id:     text (36 bytes)    // UUID of the issuing deployment
    operation_kind:    text (≤ 64 bytes)  // opaque to the enforcer
    operation_target:  bytes (≤ 256 bytes) // opaque, compared for equality
    issuer:            text (≤ 64 bytes)  // name of the issuing component
    witnesses:         array<text>        // list of principal IDs that consented; may be empty for non-human issuers
    issued_at:         u64 (unix ms)
    expires_at:        u64 (unix ms)
    nonce:             bytes (16 bytes)   // cryptographically random
    signing_key_id:    text (≤ 64 bytes)  // deployment_id + key rotation index
}

SignedConsentToken {
    token:             ConsentToken       // CBOR-serialized
    signature:         bytes (64 bytes)   // Ed25519 signature over the serialized token
}
```

Maximum serialized size: ~750 bytes. The token is designed to fit comfortably in a JSON tool-call argument without bloating prompts.

**Why CBOR:** deterministic serialization is required for signature verification. JSON's key ordering and whitespace ambiguity makes it unsuitable. CBOR with canonical encoding gives a stable byte representation.

**Why detached signature:** makes the signed content inspectable without parsing the signature envelope.

**Why Ed25519:** small keys, small signatures, fast verification, widely supported in Go, no signing-key-size decisions to make.

### 2. Issuance

**An issuer is any containerized component that:**

1. Holds a private Ed25519 signing key scoped to a specific deployment, stored in credstore.
2. Has deterministic, non-agent logic for deciding when to issue a token. (The decision is made by container code, not by an LLM body.)
3. Queries whatever state it needs to verify its issuance predicates from outside agent reach — the issuer never trusts the requesting agent's claims about whether a predicate holds.
4. Writes an audit entry for every issued token through the gateway's audit path before returning the token to the requester.

**Issuer configuration in deployment schema.** Packs declare their issuers in the `deployment_schema.yaml`:

```yaml
consent_issuers:
  - name: slack-interactivity
    component: slack-interactivity       # which hub component hosts the issuer
    allowed_operation_kinds:
      - remove_member
      - rebind_email
      - cap_override
      - force_close_nomination
      - freeze_inviter
      - add_managed_doc
      - remove_managed_doc
      - workspace_invite
    max_token_ttl_seconds: 900            # 15 minutes cap
    requires_witnesses_per_kind:
      remove_member: 2
      rebind_email: 2
      cap_override: 2
      force_close_nomination: 2
      freeze_inviter: 2
      add_managed_doc: 2
      remove_managed_doc: 2
      workspace_invite: 1                  # single-click execution after a successful vote
```

At deployment creation, the primitive:

1. Generates a fresh Ed25519 key pair for the deployment.
2. Writes the private key to credstore under a deployment-scoped key (`deployment:<id>:consent_signing_private_key`).
3. Writes the public key to the deployment's config under `consent_verification_public_keys[<key_id>]`. Multiple keys support rotation.
4. Grants the listed issuer components read access to the private key via the existing credstore scope mechanism.
5. Re-resolves the enforcer's verification key list at `apply` time (SIGHUP).

Private keys are never written outside credstore. Issuer containers receive the key at startup via the same gateway socket they use for other credstore secrets.

**Issuers are not agents.** An agent cannot become an issuer by compromise, because the signing key is in the issuer's container, not the agent's. If the issuer's container itself is compromised, the attacker can forge tokens — but that's the same failure mode any HMAC-based audit or signing system has, and it's bounded by container isolation and credstore scoping.

### 3. Validation (the enforcer hook)

The enforcer adds one new capability: when a tool call arrives for a tool whose schema declares `requires_consent_token:`, the enforcer runs a validation pass before forwarding.

**Pseudocode:**

```go
func (e *Enforcer) validateConsentToken(
    deploymentID string,
    toolCall ToolCall,
    directive RequiresConsentToken,
) error {
    // 1. Extract the token from the named input field
    tokenB64, ok := toolCall.Input[directive.TokenInputField].(string)
    if !ok {
        return ErrConsentTokenMissing
    }

    signed, err := DecodeSignedConsentToken(tokenB64)
    if err != nil {
        return ErrConsentTokenMalformed
    }

    // 2. Look up the verification public key for this deployment + key_id
    pubkey, ok := e.deploymentConsentKeys[deploymentID][signed.Token.SigningKeyID]
    if !ok {
        return ErrConsentTokenUnknownKey
    }

    // 3. Verify the Ed25519 signature
    if !ed25519.Verify(pubkey, signed.Token.Marshal(), signed.Signature) {
        return ErrConsentTokenInvalidSignature
    }

    // 4. Check deployment scope — a token from deployment A cannot authorize deployment B's tool calls
    if signed.Token.DeploymentID != deploymentID {
        return ErrConsentTokenWrongDeployment
    }

    // 5. Check expiration against the enforcer's clock with a small tolerance
    now := time.Now().UnixMilli()
    if now > signed.Token.ExpiresAt+clockSkewMillis {
        return ErrConsentTokenExpired
    }

    // 6. Check operation match
    if signed.Token.OperationKind != directive.OperationKind {
        return ErrConsentTokenWrongOperation
    }
    if !bytes.Equal(signed.Token.OperationTarget, toolCall.ResolvedOperationTarget(directive.TargetInputField)) {
        return ErrConsentTokenWrongTarget
    }

    // 7. Check maximum TTL — the issuer cannot produce tokens with longer lifetime than the deployment permits
    if signed.Token.ExpiresAt-signed.Token.IssuedAt > e.maxTokenTTLMillis(deploymentID) {
        return ErrConsentTokenTTLExceeded
    }

    // 8. Check single-use via the consumed-nonce set
    if e.consumedNonces[deploymentID].Contains(signed.Token.Nonce) {
        return ErrConsentTokenReplayed
    }

    // 9. On pass: record the nonce as consumed, then forward the call
    e.consumedNonces[deploymentID].Add(signed.Token.Nonce, signed.Token.ExpiresAt)
    return nil
}
```

Every check is a pure function or a bounded-set lookup. No state machines, no record-type dispatch, no extensibility logic. The enforcer's new responsibility is bounded to these nine steps.

**On validation failure:** the tool call is rejected, the caller receives a structured `error: consent_token_*` response, the enforcer emits an audit entry with the failure reason, and an `operator_alert` signal is emitted for the "unknown key," "wrong deployment," "invalid signature," and "replay" cases (the more suspicious failure modes).

### 4. Replay protection: the consumed-nonce set

The only new state the enforcer acquires. Per-deployment, to respect deployment isolation.

**Structure.** An in-memory `map[deploymentID]NonceSet`, where `NonceSet` is a hash set of 16-byte nonces keyed on the nonce bytes, each entry carrying the token's `expires_at` for pruning.

**Persistence.** Backed by a small JSONL file at `$AGENCY_STATE/enforcer/consumed-nonces/<deployment_id>.jsonl` for crash recovery. On gateway startup, the enforcer rehydrates the set by reading the file and dropping any entries whose `expires_at + max_clock_skew` is in the past. The file is append-only during normal operation; the prune sweep rewrites it.

**Pruning.** A background goroutine runs once per hour, dropping nonces whose `expires_at + max_clock_skew` is in the past. Bounded memory: if `max_token_ttl = 15 minutes` and a deployment issues up to N tokens per minute, the set has at most ~15N entries at any time.

**Crash semantics.** On crash, any in-flight-but-not-yet-persisted consumption is lost, meaning a narrow replay window exists between the last flush and the crash. Mitigation: fsync after every consumption. Slightly slower but bounded-replay. Acceptable for the low issuance rate expected in the community-admin use case; revisit if a pack emerges with high token throughput.

### 5. Tool-schema directive: `requires_consent_token`

Connector-exposed tools declare their gate via a new top-level directive alongside the existing `whitelist_check:` directive.

```yaml
tools:
  - name: drive_add_whitelist_entry
    description: >
      Add a resource to the connector's whitelist so subsequent
      tool calls can target it.
    input_schema:
      kind: string
      drive_id: string
      consent_token: string         # base64url-encoded consent token
    whitelist_check: drive_id
    requires_consent_token:
      operation_kind: add_managed_doc
      token_input_field: consent_token
      target_input_field: drive_id
```

**Fields of the directive:**

- `operation_kind` (required) — the expected `operation_kind` in the token. If the token's kind doesn't match, the call is rejected.
- `token_input_field` (required) — which input parameter carries the consent token.
- `target_input_field` (required) — which input parameter holds the operation target that must equal the token's `operation_target`.
- `min_witnesses` (optional, default 1) — if set, the enforcer additionally verifies the token carries at least this many entries in the `witnesses` array. Lets specific gated tools require stronger consent than the issuer's default for the operation kind.

The directive is validated at tool-schema parse time. A schema declaring `requires_consent_token:` but missing the named input fields is rejected at connector activation.

### 6. Audit integration

Every issuance and every consumption writes an audit entry through Agency's existing enforcer audit log. No new audit surface.

**Issuance audit (written by the issuer container):**

```json
{
  "ts": "2026-04-11T14:23:17Z",
  "event": "consent_token_issued",
  "deployment_id": "...",
  "issuer": "slack-interactivity",
  "operation_kind": "remove_member",
  "operation_target_hash": "sha256:abc...",
  "witnesses": ["U012345", "U067890"],
  "token_nonce": "base64:...",
  "expires_at": "2026-04-11T14:38:17Z",
  "signing_key_id": "...v1"
}
```

**Consumption audit (written by the enforcer):**

```json
{
  "ts": "2026-04-11T14:23:42Z",
  "event": "consent_token_consumed",
  "deployment_id": "...",
  "tool": "drive_add_whitelist_entry",
  "agent_principal": "...",
  "operation_kind": "add_managed_doc",
  "operation_target_hash": "sha256:abc...",
  "token_nonce": "base64:...",
  "original_issuer": "slack-interactivity",
  "witnesses": ["U012345", "U067890"]
}
```

Both entries reference the same `token_nonce`, so an auditor can reconstruct the issuance→consumption pair. The operation target is hashed in the audit log so that sensitive targets (e.g., personal emails) are not written in plaintext; the raw target is only in the token itself, which lives for minutes, not years.

**Critical audit invariant:** the issuance audit is written *before* the token is returned to the requester. If issuance audit writing fails, the issuer must not return the token. This preserves ASK tenet 2 — every action (including the issuance of a capability) is traceable, and the trace is written by mediation, not by the agent.

### 7. Key rotation

Signing keys rotate via the deployment primitive's existing `configure` → `apply` flow:

1. Operator runs `agency hub deployment rotate-consent-key <deployment-name>`.
2. The primitive generates a new Ed25519 key pair with a new `signing_key_id` (format: `<deployment_id>:v<n+1>`).
3. The primitive writes both the old and the new public keys to the deployment's `consent_verification_public_keys` block, with the old key marked `retiring`.
4. The primitive writes the new private key to credstore.
5. SIGHUP propagates: issuer containers switch to the new private key for new tokens; the enforcer accepts tokens signed by either key.
6. After a grace period (default: 2× `max_token_ttl`), the retiring key is removed from the public key list and credstore. Any tokens signed with the retiring key that have not been consumed by then are invalidated.

Rotation is an operator action, not an agent action. It is audited like any other deployment configuration change.

### 8. Deployment-scoped isolation

Consent tokens are scoped to exactly one deployment:

- The `deployment_id` field is checked during validation. A token issued by deployment A cannot authorize a tool call in deployment B, even if the two deployments share issuer components.
- The public key lookup is keyed on `(deployment_id, signing_key_id)`. A deployment cannot verify another deployment's tokens.
- The consumed-nonce set is per-deployment. Cross-deployment nonce collisions are structurally impossible.

This matches the deployment primitive's other isolation properties and satisfies ASK tenet 4 at the deployment granularity.

### 9. Relationship to the deployment primitive

Consent tokens are a runtime extension of the deployment primitive:

- **Key material** lives in the deployment's credstore scope.
- **Public keys** live in the deployment's config.
- **Issuer declarations** live in the deployment's schema (`consent_issuers:` block).
- **Audit entries** carry the deployment ID.
- **The consumed-nonce set** is partitioned by deployment ID.
- **Export/import** of a deployment generates a fresh key pair on the target agency — **consent keys do not travel in export bundles**. Any in-flight tokens expire on the source; the target must re-initiate any pending consents. This matches the non-portability of ephemeral state.

No changes to the hub deployments spec are required beyond adding the `consent_issuers:` schema field and the key generation step to `deployment create`. These are additive.

### 10. What an issuer actually looks like

As a concrete example — the `slack-interactivity` connector's two-click approval flow (which belongs in the slack-interactivity spec, not this one, but described here for completeness):

1. Consuming pack posts a Block Kit confirmation card in an admin-visible channel. The card's metadata includes a pending-approval ID, `operation_kind`, `operation_target`, `required_witnesses`, and the initiating principal.
2. First admin clicks `Approve`. `slack-interactivity` receives the interactivity payload, verifies the clicker is in the relevant group (live Slack API lookup, not a cache), records a partial approval record in the connector's local memory (or a small bounded file), and updates the card to show "awaiting second approval." The partial record has its own expiration (bounded by `max_token_ttl`).
3. Second admin clicks `Approve`. The connector validates: the clicker is in the group, the clicker is **not** the same principal as the first approver, the click is within the window. On pass, the connector:
    - Calls the audit path to emit `consent_token_issued`
    - Generates a fresh nonce and builds a `ConsentToken` with `witnesses = [first_clicker, second_clicker]`
    - Signs with the deployment's private key
    - Returns the signed token to the agent via the interactivity response path

Single-click flows (e.g., `workspace_invite` after a successful vote) skip step 2 — one click suffices and the witnesses list has one entry. The issuer validates the single click against whatever predicate is appropriate for that operation kind.

The key point: the two-click, one-click, or predicate-based decision is **entirely inside the issuer container**. The enforcer doesn't know or care how witnesses were collected. All the enforcer sees is "the token is valid, the witnesses list meets the `min_witnesses` requirement declared on the tool, forward the call."

### 11. Failure modes and mitigations

| Failure | Impact | Mitigation |
|---|---|---|
| Issuer container compromise | Attacker forges tokens within the issuer's allowed operation kinds | Bounded by container isolation; audit detects anomalous issuance patterns; signing key rotation cuts off the forged keys |
| Private key leak | Any holder of the key can forge tokens | Rotate via `rotate-consent-key`; revocation via retiring the old public key |
| Enforcer clock skew | Tokens accepted slightly after expiration | Bounded by `clockSkewMillis` (default 30 s); tokens are short-lived, so the attack window is narrow |
| Consumed-nonce set loss on crash | Narrow replay window between last flush and crash | `fsync` after every consumption; accept a ~tens-of-ms window of possible replay |
| Audit pipeline failure during issuance | Token issued without trace | Issuers MUST NOT return tokens unless the audit write succeeded; on audit failure, the issuer returns an error to the requester and does not issue |
| Agent refuses to consume a validly-issued token | Operation silently fails to execute | Issuer-side audit records the issuance; operator monitoring surfaces "issued but never consumed" as an anomaly |
| Issuer logic bug produces over-broad tokens | Tokens gate operations the issuer shouldn't have authorized | Audit logs surface the anomaly; deployment-scoped isolation bounds the blast radius to one deployment; `allowed_operation_kinds` in the deployment schema gates what issuers can legally sign |

### 12. Implementation plan

Staged:

1. **Phase 1: Token format + validation.** CBOR schema, Ed25519 signing/verification, base64url wire format. Unit tests. No integration with the enforcer yet.
2. **Phase 2: Enforcer validation hook.** The `requires_consent_token:` directive, the validation pseudocode from Section 3, the consumed-nonce set (in-memory only, no persistence).
3. **Phase 3: Persistence + rotation.** JSONL-backed consumed-nonce set with crash recovery. Key rotation verb in the deployment primitive. Audit integration.
4. **Phase 4: Deployment schema integration.** `consent_issuers:` block, key generation at `deployment create`, SIGHUP propagation.
5. **Phase 5: Reference issuer — slack-interactivity.** The slack-interactivity connector gets the two-click approval UX and the issuer-side audit writes. This is primarily a slack-interactivity spec concern; it's listed here for pipeline visibility.

Each phase is independently testable and each is shippable in the sense that the enforcer can gracefully reject tool calls that require consent tokens if the primitive isn't yet wired up (fail-closed).

### 13. Testing

- **Unit:** token encoding/decoding, signature verification, nine validation steps individually and in combination.
- **Unit:** consumed-nonce set semantics (add, contains, prune, crash recovery).
- **Integration:** issuer produces token → agent carries it → enforcer validates → tool call forwards → consumed-nonce set prevents replay. End-to-end against a fake deployment.
- **Integration:** key rotation during flight — tokens signed with the old key are accepted during the grace period, rejected after, new keys work immediately.
- **Integration:** cross-deployment isolation — a token issued for deployment A is rejected on a tool call in deployment B, even with the same operation kind and target.
- **Adversarial:**
  - Agent attempts to reuse a consumed token → rejected with `consent_token_replayed`, audit alert
  - Agent attempts to use an expired token → rejected
  - Agent modifies a field in the token body → signature fails
  - Agent uses a token from a different deployment → `consent_token_wrong_deployment`, audit alert
  - Agent invokes a gated tool without a token → `consent_token_missing`
  - Agent passes a malformed token → `consent_token_malformed`
- **Performance:** validation overhead per gated tool call — target < 1 ms under load. Ed25519 is fast; CBOR parse is bounded; the nonce-set lookup is O(1).

## ASK alignment

- **Tenet 1 (constraints external and inviolable):** the enforcer's new logic is nine bounded validation steps and a small nonce set. No state machine, no extensibility registry, no mutable approval state. The surface is small enough to audit directly. Private signing keys never enter the enforcer.
- **Tenet 2 (every action leaves a trace):** issuance and consumption both write audit entries through the existing enforcer audit log. Both reference the same nonce, making issuance↔consumption pairs correlatable.
- **Tenet 3 (mediation complete):** all tool calls declaring `requires_consent_token:` are validated by the enforcer before being forwarded to their connector. No path bypasses validation.
- **Tenet 4 (least privilege):** the enforcer remains credential-free — it holds only public verification keys. Issuer containers hold private keys scoped per-deployment via credstore. The private key's authority is bounded to the operations declared in the issuer's `allowed_operation_kinds`.
- **Tenet 7 (constraint history immutable):** key rotation writes to the audit log; old public keys remain in the deployment's config during the grace period so historical tokens can be verified for forensics.
- **Tenet 15 (trust earned, no self-elevation):** the consent-granting decision is made in the issuer container by deterministic code, not in the agent's body. Agents cannot issue tokens on their own behalf; compromised agents cannot forge tokens.
- **Tenet 17 (instructions from verified principals):** consent tokens carry explicit witness identities bound to the consent event. Tool calls gated on consent tokens receive instructions that are traceable to specific human principals via the `witnesses` field.
- **Tenet 25 (identity mutations auditable and recoverable):** identity-changing operations gated on consent tokens have two audit trails — issuance and consumption — tied by nonce. Rollback can reference either side.

## Open questions

1. **Token carrier syntax in tool calls.** Consent tokens arrive in tool-call arguments as base64url strings. Should the enforcer also support a header-style carrier (e.g., a dedicated `_consent_tokens` metadata field in the tool call wrapper) so tokens don't bloat the visible argument list? Flag: consider if a specific connector adds many gated tools.
2. **Issuer's view of past issuances.** Should issuers have read access to their own prior issuances (via audit query) so they can surface "this approval was already issued, here's the token" to idempotent consumers? Probably not in v1 — issuer-side idempotency is the issuer's internal concern.
3. **CBOR vs. JWT.** JWTs are more widely supported and have existing tooling. CBOR is smaller and simpler to canonicalize. Proposed: CBOR for v1 because deterministic serialization is a hard requirement and JWT's signing-over-JSON has known canonicalization pitfalls. Revisit if the tooling friction turns out to be high.
4. **Multiple issuers per deployment.** Supported structurally (the `consent_issuers:` block is a list), but this spec doesn't address cross-issuer conflict resolution. Proposed: no conflict exists — two issuers for the same `operation_kind` both validly produce tokens, the enforcer accepts either, the pack chooses which issuer it calls. Revisit if a real conflict scenario emerges.
5. **Witness identity spoofing.** The issuer records witness principal IDs in the token based on authenticated interactivity payloads. If the issuer's authentication of the clicker is weak, the witness claim is weak. Mitigation is in the issuer's spec (e.g., slack-interactivity uses live Slack API lookups, not cached rosters). Flag: each issuer spec should explicitly describe its witness authentication story.
