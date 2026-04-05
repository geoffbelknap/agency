# Hybrid Model Routing

**Status:** Draft
**Depends on:** OpenAI-compatible provider adapter (for local/cheap model targets)

## Problem

Every LLM call from an agent uses the same model regardless of task complexity. A 10-step workflow sends frontier-model requests for planning, reasoning, JSON formatting, tool output summarization, and entity extraction — all at the same cost and latency.

The tier system exists in the gateway (`RoutingConfig.Tiers` with `frontier/standard/fast/mini/nano/batch` and `ResolveTierWithStrategy`), but the body runtime sends a static model alias (`AGENCY_MODEL` env var) for every call. No per-call routing decisions are made.

## What Already Exists

**Gateway routing model** (`internal/models/routing.go`):
- `TierConfig` with 6 tiers: frontier, standard, fast, mini, nano, batch
- `ResolveTier()` and `ResolveTierWithStrategy()` with strict/best_effort/catch_all fallback
- Per-model cost config (`cost_per_mtok_in/out/cached`)
- Agent-level `model_tier` override in agent config

**Enforcer LLM proxy** (`images/enforcer/llm.go`):
- Intercepts all LLM calls from body runtime
- Resolves model alias → provider endpoint via `ResolveModel()`
- Has full visibility into request content (messages, tool calls, system prompt)
- Already rewrites model field in request body before forwarding

**Body runtime** (`images/body/body.py`):
- Static `self.model` set once at startup from `AGENCY_MODEL` env
- Sends model alias in every `/chat/completions` request
- No per-call model selection logic

**Cost source header** (`X-Agency-Cost-Source`):
- Already tags calls by purpose: `agent_task`, `reflection`, `evaluation`, `memory_capture`, `consolidation`, `context_summary`

## Design

### Where routing decisions happen: the enforcer

The enforcer is the correct location for hybrid routing decisions because:
- It's the mediation boundary (ASK Tenet 3 — mediation is complete)
- Enforcement runs outside the agent boundary (ASK Tenet 1)
- It already has full request visibility and rewrites the model field
- The agent cannot perceive or influence which model actually serves its request

The body runtime continues sending its configured model alias. The enforcer may reroute to a different tier based on routing rules. From the agent's perspective, nothing changes.

### Routing rules

Add a `routing_rules` section to the enforcer's routing config:

```yaml
routing_rules:
  # Route by cost source (X-Agency-Cost-Source header)
  - match:
      cost_source: reflection
    tier: fast
    reason: "Reflection is self-evaluation — doesn't need frontier reasoning"

  - match:
      cost_source: memory_capture
    tier: mini
    reason: "Memory capture is structured extraction"

  - match:
      cost_source: consolidation
    tier: fast
    reason: "Consolidation is summarization"

  - match:
      cost_source: context_summary
    tier: mini
    reason: "Context summarization is compression, not reasoning"

  - match:
      cost_source: evaluation
    tier: fast
    reason: "Success criteria evaluation is checklist comparison"

  # Route by message pattern
  - match:
      last_message_role: tool
      tool_output_tokens_gt: 5000
    tier: fast
    reason: "Large tool output processing — summarize, don't reason"

  # Default: agent's configured tier (no rule matches)
```

Rules are evaluated in order, first match wins. If no rule matches, the request uses the agent's configured model (current behavior).

### Rule match conditions

| Condition | Source | Description |
|---|---|---|
| `cost_source` | `X-Agency-Cost-Source` header | Matches the call's purpose tag |
| `last_message_role` | Last message in the messages array | `user`, `assistant`, `tool`, `system` |
| `tool_output_tokens_gt` | Estimated token count of last tool result | Threshold for "large tool output" |
| `has_tool_calls` | Whether the request includes tool definitions | Boolean — tool-calling requests vs. plain generation |
| `step_index_gt` | Current step index (from economics observability) | Late-loop calls may be cheaper to serve on fast tier |
| `model_override` | Explicit `X-Agency-Model-Tier` header from body | Allows body to hint at tier (opt-in, not required) |

### Enforcer implementation

In `LLMHandler.ServeHTTP()`, after parsing the request body and before resolving the model:

1. Read `X-Agency-Cost-Source` header (already present)
2. Evaluate routing rules against the request
3. If a rule matches:
   - Resolve the target tier via `ResolveTier()` (using the gateway's tier config)
   - Rewrite the model alias to the tier's resolved model
   - Log the reroute in the audit entry (new field: `ReroutedFrom`, `RerouteRule`)
4. If no rule matches:
   - Use the original model alias (current behavior)

### New audit fields

| Field | Type | Purpose |
|---|---|---|
| `ReroutedFrom` | string | Original model alias before reroute (empty if no reroute) |
| `RerouteRule` | string | Name/index of the routing rule that matched |
| `TargetTier` | string | Tier the request was routed to |
| `ModelTierHint` | string | Value of `X-Agency-Model-Tier` header if present (for operator audit of agent hint patterns) |

These feed into economics observability — operators can see cost savings from rerouting.

### Tier resolution at the enforcer

The enforcer currently has a simpler `RoutingConfig` than the gateway. Two options:

**Option A: Extend the enforcer's routing config** to include the full `TierConfig` from the gateway model. The gateway already generates the enforcer's `routing.yaml` — it can include tier definitions.

**Option B: Enforcer calls gateway** to resolve tiers via a lightweight internal endpoint. Adds latency per rerouted call.

**Recommended: Option A.** The routing config is already generated by the gateway and delivered to the enforcer. Adding tiers to it is a config change, not an architecture change. The enforcer already has `ResolveTier()` logic in the gateway model — port it to the enforcer's config package.

### Operator configuration

Routing rules are part of the agent's routing config, which means they can be:
- Set globally in `routing.yaml` (applies to all agents)
- Overridden per-agent in `routing.local.yaml` (operator customization)
- Managed via hub presets (packs can ship recommended routing rules)

Operators who don't configure routing rules get current behavior — no rerouting.

### Provider health and failover

Hybrid routing naturally extends to health-aware failover. If a tier's primary model is unavailable (HTTP 5xx, timeout), `ResolveTierWithStrategy()` already falls back to the next preference in the tier, or to adjacent tiers under `best_effort`/`catch_all` strategies.

Add to routing rules:
```yaml
routing_rules:
  - match:
      provider_error: true
    tier: standard
    strategy: catch_all
    reason: "Provider failure — fall back to any available model"
```

The enforcer already sees provider errors in the response. On error, re-evaluate with the `provider_error` flag set and retry once on the fallback tier.

### Body runtime opt-in hints

For cases where the body runtime has useful context the enforcer can't see (e.g., "I'm about to do a simple formatting step"), add an optional `X-Agency-Model-Tier` header.

**ASK Tenet 1 compliance:** The hint is a preference, not an instruction. The enforcer is never obligated to honor it — operator-configured routing rules always take precedence. A compromised agent sending adversarial hints cannot expand its capabilities beyond the operator-configured routing rules. The enforcer logs hint values in the audit entry (`ModelTierHint` field) so operators can detect adversarial or anomalous hint patterns. The hint can only select from tiers the operator has configured — it cannot reference arbitrary models or providers.

This is opt-in. The body runtime's default behavior (no header) is unchanged.

## What This Does NOT Include

- **Automatic complexity classification** — no LLM-based analysis of "is this request simple or complex." Rule-based only. Complexity classification could be a future enhancement.
- **Local model hosting** — this spec assumes models are accessible via the existing provider config. The OpenAI-compatible adapter spec handles adding local endpoints (Ollama, vLLM).
- **Context compression** — rerouting a large-tool-output call to a fast tier is not the same as compressing the output. Context compression is a separate spec.
- **Dynamic cost optimization** — no automatic "switch to cheaper model when budget is low." Budget enforcement already handles hard stops. Cost-aware routing could layer on top of this.

## Sequencing

1. Port tier resolution to enforcer config package (extend `RoutingConfig` with `Tiers`)
2. Add routing rules parser and evaluator in enforcer
3. Add reroute logic in `LLMHandler.ServeHTTP()` before model resolution
4. Add `ReroutedFrom`, `RerouteRule`, `TargetTier` to `AuditEntry`
5. Add `X-Agency-Model-Tier` header support in body runtime (optional)
6. Add `provider_error` retry-on-fallback logic
7. Update gateway routing config generation to include tiers and rules
8. Document routing rules syntax for operators

Steps 1-4 are the core. Steps 5-8 are enhancements that can follow.
