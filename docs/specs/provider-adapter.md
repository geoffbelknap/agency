# Provider Adapter

## Summary

Add model capability declarations to routing config, enforce capability-aware tier routing, add Gemini as a first-class provider, and add `agency provider add` for discovering and configuring local/custom providers.

## Motivation

Agency needs to route tasks to different models based on what those models can do. The hybrid routing pillar (E2) depends on the enforcer knowing which tiers support tools, vision, and streaming so it can route cheap tasks to cheap models without silent failures. Operators adding local models (Ollama, LiteLLM) need a way to configure them without hand-editing YAML.

## Design Decisions

- **Body stays OpenAI format only.** The enforcer handles Anthropic translation. All other providers (OpenAI, Gemini, Ollama) speak OpenAI-compatible natively. No provider format awareness in the body runtime.
- **Capabilities are data, not code.** Declared in routing config per model, shipped in hub provider YAMLs, discoverable via `agency provider add`, overridable in `routing.local.yaml`.
- **Tier capabilities are derived.** A tier's capabilities are the intersection of its models' capabilities.
- **Mismatch handling:** Automatic tier fallback to the nearest capable tier. Reject if no tier supports the required capabilities.

## Model Capabilities

### Capability Set

Three capabilities, extensible later:

| Capability | Meaning |
|---|---|
| `tools` | Model supports function/tool calling |
| `vision` | Model accepts image content in messages |
| `streaming` | Model supports SSE streaming responses |

### In Routing Config

`capabilities` is a required field on every model entry:

```yaml
models:
  claude-sonnet:
    provider: anthropic
    provider_model: claude-sonnet-4-20250514
    capabilities: [tools, vision, streaming]
    cost_per_mtok_in: 3.0
    cost_per_mtok_out: 15.0
    cost_per_mtok_cached: 0.30
  gemini-2.5-flash:
    provider: gemini
    provider_model: gemini-2.5-flash
    capabilities: [tools, vision, streaming]
    cost_per_mtok_in: 0.15
    cost_per_mtok_out: 0.60
    cost_per_mtok_cached: 0.0375
  llama-8b:
    provider: my-ollama
    provider_model: llama3.1:8b
    capabilities: [streaming]
    cost_per_mtok_in: 0
    cost_per_mtok_out: 0
```

### In Go Models

Add to `ModelConfig` in `internal/models/routing.go`:

```go
type ModelConfig struct {
    Provider          string   `yaml:"provider" validate:"required"`
    ProviderModel     string   `yaml:"provider_model" validate:"required"`
    Capabilities      []string `yaml:"capabilities" validate:"required"`
    CostPerMTokIn     float64  `yaml:"cost_per_mtok_in" validate:"gte=0"`
    CostPerMTokOut    float64  `yaml:"cost_per_mtok_out" validate:"gte=0"`
    CostPerMTokCached float64  `yaml:"cost_per_mtok_cached" validate:"gte=0"`
}

func (m ModelConfig) HasCapability(cap string) bool {
    for _, c := range m.Capabilities {
        if c == cap {
            return true
        }
    }
    return false
}
```

### Tier Capability Derivation

A tier's effective capabilities are the intersection of all its models' capabilities. If standard tier has Claude Sonnet `[tools, vision, streaming]` and Gemini Pro `[tools, vision, streaming]`, the tier supports all three. If a mini tier has only Llama 8B `[streaming]`, the tier supports only streaming.

Add to `RoutingConfig`:

```go
func (rc *RoutingConfig) TierCapabilities(tier string) []string {
    // Get models in tier, intersect their capabilities
}
```

## Enforcer Mismatch Handling

### Request Capability Detection

Before routing, the enforcer inspects the request body:

| Request feature | Required capability |
|---|---|
| `tools` array present and non-empty | `tools` |
| Any message content contains image blocks | `vision` |
| `stream: true` | `streaming` |

### Routing with Capabilities

1. Resolve the requested tier (from `X-Agency-Tier` header or default)
2. Check if the tier's capabilities satisfy the request's requirements
3. If mismatch: walk to the nearest tier (up or down the hierarchy) that satisfies all requirements
4. If no tier satisfies: reject with HTTP 422 and a clear error message listing what's missing and what's available
5. If match: proceed with normal routing

The tier walk order follows the existing `best_effort` strategy — prefer the nearest tier in the hierarchy (standard → fast → frontier → mini → nano).

## Tier Capability Manifest for Body

The enforcer serves `/config/tiers.json` on port 8081 (config port), hot-reloaded on SIGHUP:

```json
{
  "tiers": {
    "frontier": {"capabilities": ["tools", "vision", "streaming"]},
    "standard": {"capabilities": ["tools", "vision", "streaming"]},
    "fast": {"capabilities": ["tools", "streaming"]},
    "mini": {"capabilities": ["streaming"]},
    "nano": {"capabilities": ["streaming"]}
  },
  "default_tier": "standard"
}
```

The body reads this at session start and uses it to choose tiers for subtasks. The body never sees model names or provider names — just tier capabilities. Full body-side tier selection logic is part of E2 (hybrid model routing) — this spec just exposes the data.

## Gemini Provider

New hub component at `agency-hub/providers/gemini/provider.yaml`:

```yaml
name: gemini
display_name: Google Gemini
description: Gemini models — 2.5 Pro, 2.5 Flash
version: "0.1.0"
category: cloud
credential:
  name: gemini-api-key
  label: API Key
  env_var: GEMINI_API_KEY
  api_key_url: https://aistudio.google.com/apikey
routing:
  api_base: https://generativelanguage.googleapis.com/v1beta/openai
  auth_header: Authorization
  auth_prefix: "Bearer "
  models:
    gemini-2.5-pro:
      provider_model: gemini-2.5-pro
      capabilities: [tools, vision, streaming]
      cost_per_mtok_in: 1.25
      cost_per_mtok_out: 10.0
      cost_per_mtok_cached: 0.315
    gemini-2.5-flash:
      provider_model: gemini-2.5-flash
      capabilities: [tools, vision, streaming]
      cost_per_mtok_in: 0.15
      cost_per_mtok_out: 0.60
      cost_per_mtok_cached: 0.0375
  tiers:
    frontier: gemini-2.5-pro
    standard: gemini-2.5-pro
    fast: gemini-2.5-flash
    mini: gemini-2.5-flash
    nano: gemini-2.5-flash
    batch: null
```

Uses Google's OpenAI-compatible endpoint. No enforcer changes needed — pass-through like OpenAI.

Flash at $0.15/MTok input is a strong cheap-tier candidate for E2 hybrid routing.

## Existing Provider Updates

Add `capabilities` to all model entries in existing hub providers:

| Provider | Models | Capabilities |
|---|---|---|
| Anthropic | claude-opus, claude-sonnet, claude-haiku | `[tools, vision, streaming]` |
| OpenAI | gpt-4.1, gpt-4o, gpt-4.1-mini, gpt-4.1-nano | `[tools, vision, streaming]` |
| Ollama | (empty, user-configured) | Discovered via `agency provider add` |
| OpenAI-Compatible | (empty, user-configured) | Discovered or manually declared |

## `agency provider add`

### Command

```bash
agency provider add <name> <base-url> [--credential <env-var>]
```

### Discovery Flow

1. **List models:** `GET <base-url>/models` (OpenAI-compatible model listing endpoint)
2. **Probe capabilities per model:**
   - Send a minimal tool call request → 200 = `tools` supported
   - Check model metadata for vision support if available
   - `streaming` assumed supported (safe default — nearly universal)
   - Probe timeout: 5 seconds per model
3. **Present results for confirmation:**

```
Found 3 models at http://localhost:11434/v1:

  llama3.1:8b       tools, streaming     $0/MTok (local)
  mistral:7b        streaming            $0/MTok (local)
  qwen2.5-coder:7b  tools, streaming     $0/MTok (local)

Assign tiers (leave blank to skip):
  fast tier:  llama3.1:8b
  mini tier:  mistral:7b
  nano tier:  qwen2.5-coder:7b

Write to routing.local.yaml? [Y/n]
```

4. **On confirmation:** Write provider config, models with capabilities, and tier assignments to `routing.local.yaml`. If `--credential` was provided, prompt for the key value and store via `agency creds set`.

### Probing Details

Capability probing is best-effort. If the model listing endpoint doesn't exist or probes fail:
- Fail the `provider add` command with a clear error
- Suggest `--no-probe` flag to skip discovery and write a skeleton config the operator fills in manually

### Where Config Lands

`agency provider add` writes to `routing.local.yaml` (operator overrides), not to hub-managed `routing.yaml`. This means:
- `agency hub update` never overwrites custom providers
- The operator owns the config
- Multiple custom providers can coexist

## Changes Summary

| Layer | Change |
|---|---|
| `internal/models/routing.go` | Add `Capabilities []string` to `ModelConfig`. Add `HasCapability()`. Add `TierCapabilities()` to `RoutingConfig`. |
| `images/enforcer/config.go` | Add `Capabilities` to enforcer's `Model` struct. |
| `images/enforcer/llm.go` | Before routing: detect required caps from request body. On mismatch: tier fallback. On no match: reject 422. Serve `/config/tiers.json`. |
| `internal/cli/commands.go` | New `agency provider add` command with discovery + confirmation. |
| `internal/hub/provider.go` | `MergeProviderRouting()` passes through `capabilities` field. |
| `agency-hub/providers/gemini/` | New Gemini provider YAML. |
| `agency-hub/providers/anthropic/` | Add `capabilities` to model entries. |
| `agency-hub/providers/openai/` | Add `capabilities` to model entries. |
| `agency-hub/providers/ollama/` | Add `capabilities` field documentation. |
| `agency-hub/providers/openai-compatible/` | Add `capabilities` field documentation. |

## What Doesn't Change

- The enforcer's Anthropic translation code (stays as-is)
- The body's wire format (OpenAI always)
- The OCI publishing pipeline
- The existing `agency hub install` flow (just gains capabilities data)
- The egress proxy credential handling
