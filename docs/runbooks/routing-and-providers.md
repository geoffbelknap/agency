# Routing & Providers

> Status: Mixed operator runbook. Basic provider setup, model tiering, and
> routing configuration are part of the supported `0.2.x` path. Routing
> optimizer suggestions and approval workflows are experimental.

## Trigger

Adding LLM providers, configuring model tiers, reviewing routing optimizer suggestions, or troubleshooting model routing.

## Provider Setup

Agency supports five first-class providers:

| Provider | Format | Setup |
|----------|--------|-------|
| Anthropic | Requires format translation (enforcer handles OpenAI↔Anthropic) | `agency setup` or manual |
| OpenAI | Native OpenAI format | `agency setup` or manual |
| Google Gemini | OpenAI-compatible endpoint | `agency setup` or manual |
| Ollama | OpenAI-compatible (local) | Manual |
| OpenAI-Compatible | Any OpenAI-format endpoint | Manual |

### Adding a provider via setup

```bash
agency quickstart
```

The quickstart flow prompts for provider and API key and stores the key in the
encrypted credential store.

### Adding a provider manually

```bash
# Store the API key
agency creds set ANTHROPIC_API_KEY --value sk-ant-...

# For OpenAI-compatible providers, discover models
agency hub provider add my-provider https://api.my-provider.com/v1
```

`agency hub provider add` probes the endpoint for available models and adds them to `routing.yaml`.

### Listing configured providers

```bash
agency infra providers
```

Via API: `GET /api/v1/infra/providers`

## Model Tiers

Five tiers, from most to least capable:

| Tier | Purpose | Example Models |
|------|---------|---------------|
| `frontier` | Complex reasoning, investigation | Claude Opus, GPT-4o |
| `standard` | General-purpose tasks | Claude Sonnet, GPT-4o-mini |
| `fast` | Quick responses, formatting | Claude Haiku, GPT-4o-mini |
| `mini` | Extraction, validation | Small/cheap models |
| `nano` | Classification, routing | Smallest models |

Each agent preset declares a tier. The platform resolves to the best available model based on configured credentials.

### Model capabilities

Every model in `routing.yaml` declares capabilities:

```yaml
models:
  claude-sonnet-4-20250514:
    provider: anthropic
    tier: standard
    capabilities: [tools, vision, streaming]
```

The enforcer validates that the target model supports what the request needs. On mismatch, returns HTTP 422. Tier capabilities are the intersection of models in the tier.

## Routing Configuration

### Hub-managed config

The base provider catalog ships with Agency. Generated routing config should be
treated as managed data; put local changes in the operator override file rather
than editing generated config directly.

### Operator overrides

Custom routing goes in `routing.local.yaml`:

```yaml
# routing.local.yaml — survives hub update
overrides:
  frontier:
    preferred: claude-opus-4-20250514
  standard:
    preferred: claude-sonnet-4-20250514
```

The routing optimizer writes approved suggestions here.

## Routing Optimizer

A background goroutine in the gateway aggregates LLM call data per (task_type, model). It computes success rates and real USD costs.

### View suggestions

```bash
agency infra routing suggestions
```

A suggestion appears when:
- Cheaper model has >= 90% success rate
- Savings >= 30%
- Based on >= 20 calls of data

### Approve a suggestion

```bash
agency infra routing approve <suggestion-id>
```

Writes the change to `routing.local.yaml`. Takes effect immediately.

### Reject a suggestion

```bash
agency infra routing reject <suggestion-id>
```

### View routing statistics

```bash
agency infra routing stats
```

Shows per-model, per-task-type success rates and costs.

Via API:

- `GET /api/v1/infra/routing/suggestions`
- `POST /api/v1/infra/routing/suggestions/{id}/approve`
- `POST /api/v1/infra/routing/suggestions/{id}/reject`
- `GET /api/v1/infra/routing/stats`
- `GET /api/v1/infra/routing/metrics`
- `GET /api/v1/infra/routing/config`

## Troubleshooting

### Agent getting 422 errors

The model doesn't support a required capability (e.g., agent needs `vision` but the tier's model doesn't support it).

```bash
agency infra routing config
```

Check the tier's model capabilities. Either switch the agent to a different tier or add a model with the required capability.

### No routing suggestions appearing

The optimizer needs >= 20 calls per (task_type, model) before generating suggestions. Run more tasks or wait for sufficient data.

### Provider key not working

```bash
agency creds test <key-name>
```

Provider credentials (Anthropic, OpenAI, Google) don't have a `test_endpoint` by default. To verify, send a test message to an agent:

```bash
agency send <agent-name> "Hello, confirm you're working."
```

Check agent logs for LLM errors:

```bash
agency log <agent-name>
```

### Format translation errors (Anthropic)

Anthropic is the only provider requiring format translation. The enforcer handles OpenAI→Anthropic and back. If translation errors appear in enforcer logs:

```bash
docker logs agency-<agent-name>-enforcer 2>&1 | tail -20
```

## Verification

- [ ] `agency infra providers` lists all configured providers
- [ ] `agency infra routing config` shows correct tier→model mappings
- [ ] `agency infra routing stats` shows call data after task execution
- [ ] Agents can make LLM calls (send a test message)
- [ ] `routing.local.yaml` reflects approved suggestions

## See Also

- [Budget & Cost](budget-and-cost.md) — cost optimization via routing
- [Credential Rotation](credential-rotation.md) — rotating provider API keys
- [Infrastructure Recovery](infrastructure-recovery.md) — egress proxy issues
