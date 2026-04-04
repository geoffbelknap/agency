Infrastructure components (knowledge synthesizer, enforcer XPIA scanner) need LLM access for tasks like entity extraction and XPIA scanning. Today each component calls provider APIs directly through the egress proxy, with hardcoded models, URLs, and formats. This bypasses the routing stack that agents use and makes provider switching a per-component configuration change.

This spec centralizes all infrastructure LLM calls through a gateway internal endpoint that handles model resolution, format translation, cost tracking, and audit — the same capabilities agents get through their enforcers.

## The Problem

Today's infrastructure LLM flow:

```
knowledge container → https://api.anthropic.com/v1/messages (via egress proxy)
                      ^ hardcoded URL, model, format per component
                      ^ no cost tracking, no audit, no routing
```

Each component independently configures:
- Provider API URL
- Model name (provider-specific, not an alias)
- API format (Anthropic vs OpenAI)
- Egress proxy settings and CA certificates
- Authentication headers

Switching from Anthropic to OpenAI requires changing code or env vars in every infrastructure component. Cost tracking for infrastructure LLM usage doesn't exist. There's no audit trail for infrastructure LLM calls.

## Solution: Gateway Internal LLM Endpoint

The gateway exposes an internal endpoint that infrastructure components call instead of provider APIs:

```
POST /api/v1/internal/llm
```

Flow:
```
infra container → http://gateway:8200/api/v1/internal/llm
                  → gateway socket proxy → gateway.sock
                  → gateway resolves model alias from routing.yaml
                  → gateway translates format (OpenAI ↔ Anthropic)
                  → gateway proxies to provider via egress
                  → cost tracked under infrastructure budget
                  → audit logged
                  → response returned in OpenAI format
```

Infrastructure containers reach the gateway via `http://gateway:8200` on the Docker mediation network (gateway socket proxy). See `docs/specs/gateway-socket-proxy.md`.

### Request Format

Callers always send OpenAI-compatible format:

```json
POST /api/v1/internal/llm
X-Agency-Token: {gateway_token}
X-Agency-Caller: knowledge-synthesizer

{
  "model": "claude-haiku",
  "messages": [{"role": "user", "content": "..."}],
  "max_tokens": 4096
}
```

### Response Format

Always OpenAI-compatible, regardless of upstream provider:

```json
{
  "choices": [{
    "message": {
      "role": "assistant",
      "content": "..."
    }
  }],
  "usage": {
    "prompt_tokens": 1234,
    "completion_tokens": 567,
    "total_tokens": 1801
  }
}
```

### Gateway Processing

1. Authenticate via `X-Agency-Token`
2. Resolve model alias (`claude-haiku`) to provider + provider model via `routing.yaml`
3. Translate request format if provider requires it (e.g. OpenAI format → Anthropic Messages API)
4. Proxy to provider API via egress (egress handles credential injection)
5. Translate response back to OpenAI format
6. Calculate cost from token usage + model pricing in `routing.yaml`
7. Record cost under infrastructure budget
8. Write audit event: `infra_llm_call`
9. Return response to caller

### Caller Identification

Infrastructure containers include an `X-Agency-Caller` header identifying which component is making the call. Valid callers:

- `knowledge-synthesizer` — entity extraction from conversations
- `knowledge-curator` — (future) LLM-assisted curation

The gateway rejects calls without a recognized caller header.

## Authentication

The internal endpoint uses the same `X-Agency-Token` authentication as all gateway endpoints. Infrastructure containers receive the token via environment variable at startup:

```
AGENCY_GATEWAY_URL=http://gateway:8200
AGENCY_GATEWAY_TOKEN={token}
```

These are set by the gateway when creating infrastructure containers (same pattern as existing env vars like `HTTPS_PROXY`).

## Infrastructure Budget

Infrastructure LLM calls get their own budget category, separate from agent budgets:

```yaml
# In ~/.agency/config.yaml
budget:
  agent_daily: 10.00
  agent_monthly: 200.00
  per_task: 2.00
  infrastructure_daily: 5.00     # daily cap for all infra LLM calls combined
```

The gateway tracks infrastructure spend in `~/.agency/budget/_infrastructure.json` using the same `BudgetStore` as agent budgets.

When infrastructure budget is exhausted:
1. Gateway rejects infra LLM calls with 429
2. Alert sent to `#operator`: "Infrastructure LLM budget exhausted ($5.00/$5.00). Synthesis and scanning paused until tomorrow."
3. Knowledge synthesis and XPIA scanning skip until budget resets at midnight
4. Agent LLM calls are unaffected — they have their own budgets

Infrastructure budget shows in `agency status`:
```
Agency v0.1.0 (abc1234, 2026-03-24)
Infrastructure LLM: $1.23 / $5.00 today

Infrastructure:
  ● egress         running    abc1234 ✓
  ...
```

## What Changes in Infrastructure Components

### Knowledge Synthesizer

Replace direct provider API calls with the gateway endpoint:

```python
# Before
resp = self._http_fallback.post(
    "https://api.anthropic.com/v1/messages",
    json={"model": self._fallback_model, "messages": [...], "max_tokens": 4096},
    headers={"anthropic-version": "2023-06-01"},
)
data = resp.json()
return data["content"][0]["text"]

# After
resp = self._http.post(
    f"{self._gateway_url}/api/v1/internal/llm",
    json={"model": self._model, "messages": [...], "max_tokens": 4096},
    headers={
        "X-Agency-Token": self._gateway_token,
        "X-Agency-Caller": "knowledge-synthesizer",
    },
)
data = resp.json()
return data["choices"][0]["message"]["content"]
```

Removed env vars (no longer needed):
- `KNOWLEDGE_SYNTH_API_URL`
- `KNOWLEDGE_SYNTH_API_FORMAT`
- `HTTPS_PROXY` (for LLM calls — may still be needed for other external access)
- `SSL_CERT_FILE` (for egress CA — no longer calling external APIs directly)

Kept env vars:
- `KNOWLEDGE_SYNTH_MODEL` — the model alias (e.g. `claude-haiku`)
- `AGENCY_GATEWAY_URL` — gateway address (new)
- `AGENCY_GATEWAY_TOKEN` — gateway auth token (new)

### Enforcer XPIA Scanner

The enforcer's XPIA scanning LLM calls follow the same pattern — replace direct provider calls with the gateway endpoint. The `X-Agency-Caller` header is `enforcer-xpia`.

### Future Components

Any new infrastructure component needing LLM access calls the gateway endpoint. No provider-specific code, no format handling, no egress proxy configuration for LLM calls.

## Gateway Implementation

### Handler

```go
func (h *handler) internalLLM(w http.ResponseWriter, r *http.Request) {
    caller := r.Header.Get("X-Agency-Caller")
    if !validInfraCaller(caller) {
        writeJSON(w, 403, map[string]string{"error": "unrecognized caller"})
        return
    }

    // Parse OpenAI-format request
    // Resolve model alias via routing config
    // Check infrastructure budget
    // Translate format if needed
    // Proxy to provider via egress
    // Translate response to OpenAI format
    // Record cost
    // Audit log
    // Return response
}
```

### Route

```go
r.Post("/api/v1/internal/llm", h.internalLLM)
```

### Audit Events

- `infra_llm_call` — caller, model, input_tokens, output_tokens, cost_usd, duration_ms
- `infra_budget_warning` — at 80% of daily infrastructure budget
- `infra_budget_exhausted` — at 100%

## Container Configuration

The gateway passes these env vars to infrastructure containers at creation time (in `orchestrate/infra.go`):

```go
env["AGENCY_GATEWAY_URL"] = "http://gateway:8200"
env["AGENCY_GATEWAY_TOKEN"] = cfg.Token
```

This follows the existing pattern — infrastructure containers already receive env vars for comms URL, egress proxy, etc.

## ASK Compliance

| Tenet | Compliance |
|-------|-----------|
| **2. Every action traced** | All infrastructure LLM calls logged as `infra_llm_call` audit events with caller, model, tokens, cost. |
| **3. Mediation improved** | Infrastructure calls now go through the gateway (mediation) instead of directly to provider APIs. Centralized control point. |
| **4. Least privilege** | Gateway can restrict which models infrastructure is allowed to use. Caller identification prevents unauthorized use. |
| **7. Constraint history** | Routing config changes are versioned. Infrastructure automatically picks up new config — no per-component reconfiguration. |

Note: Infrastructure components are part of the enforcement machinery, not agents. Agent-specific tenets (1, 11, 12, etc.) don't apply. The relevant tenets are about mediation, audit, and least privilege — all improved by this change.

## Scope Boundaries

This spec covers infrastructure LLM routing only. It does not cover:
- **Agent LLM routing** — agents already route through their own enforcers. No change.
- **Model auto-selection** — choosing the optimal model based on task complexity. Future work.
- **Infrastructure model hot-swap** — changing the infrastructure model without restart. The gateway reads routing.yaml on each request, so this works automatically.

## What This Enables

- Switch LLM providers in one place (`routing.yaml`) — all infrastructure components follow automatically
- Infrastructure cost tracking — visible in `agency status`, budgetable, auditable
- Format-agnostic infrastructure code — components send OpenAI format, gateway handles translation
- New infrastructure components get LLM access with two env vars and one HTTP call
