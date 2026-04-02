*Spec for a managed admin model container that provides zero-cost inference for platform administrative tasks.*

**Parent:** [Compounding Agent Organizations](/specs/compounding-agent-organizations) — spec #2 of 5.

---

## Overview

Agency's knowledge service currently calls the Anthropic API directly for LLM synthesis — a mediation violation (ASK Tenet 3) and a recurring token cost for administrative work. This spec introduces a managed Ollama container on the mediation network that provides an OpenAI-compatible inference endpoint for platform admin tasks: entity extraction, classification, and (in future specs) semantic curation.

The container runs a small CPU-optimized model (~2GB) at zero marginal token cost. The synthesizer is fixed to route through this endpoint first, with Haiku fallback on failure. The enforcer gains a new `admin` provider so future consumers can route to the same endpoint via standard model aliases.

This is infrastructure, not an agent capability. The admin model serves platform internals only — agents continue using their configured models (Sonnet, Haiku, etc.) for reasoning.

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Container vs. host Ollama | Managed container (`agency-infra-admin-model`) | Self-contained infrastructure. Host Ollama is a separate provider for operators with GPU hardware. |
| Model | `qwen2.5:3b` (configurable) | Best structured JSON output in the ~2GB class. CPU-friendly. Operator can swap via env var. |
| Model download | Lazy pull on first use | Synthesizer is a background batch process (10-msg or 1-hour trigger). First-call latency is invisible. |
| Weight persistence | Docker named volume | Survives container restarts and `admin destroy`. Only wiped with `--permadeath`. |
| Synthesizer routing | Direct call to admin model on mediation network | Both are internal infrastructure on the same private network. No credentials involved. No enforcer needed for infra-to-infra calls. |
| Fallback | Haiku via existing path | If local model fails (parse error, empty extraction, connection refused), retry with Haiku. Logged for reliability tracking. |
| Provider naming | `admin` (not `local-model`) | Communicates purpose: platform administrative tasks. Distinct from `ollama` (operator's own GPU models). |

## Architecture

### Container: `agency-infra-admin-model`

Uses the upstream `ollama/ollama` Docker image directly — pulled, not built. This is managed separately from the build pipeline in `infrastructure.py` via `_ensure_admin_model()`, similar to how external base images are handled. The container is not added to `SHARED_IMAGES` (which drives the buildable image pipeline). Instead, `infrastructure.py` pulls the upstream image if not present and manages its lifecycle alongside other shared infrastructure.

Runs on the `agency-mediation` network alongside comms and knowledge.

**Lifecycle:**
- Image pulled at first `agency infra up` if not present (`docker.images.pull("ollama/ollama")`)
- Container started at `agency infra up`, listens on port 11434
- No model downloaded at startup — pulled lazily on first inference request
- Health check: `GET /` returns 200 when Ollama is ready
- Stopped at `agency infra down`

**Resource limits:**
- Memory: 3GB (sufficient for a quantized 3B parameter model on CPU plus Ollama server overhead)
- No GPU passthrough in default config. Operators can override Docker run args for GPU acceleration.

**Network position:**
```
agency-mediation network:
  agency-infra-admin-model:11434  ← Ollama (OpenAI-compatible API)
  agency-infra-knowledge          ← Knowledge service (synthesizer consumer)
  agency-comms                    ← Channel messaging
  ...
```

The admin model container has no internet access — mediation network is internal-only. It requires internet only during image pull (at `infra up`) and model weight download (lazy, on first use). Both operations go through the host's network, not the container's runtime network.

**Data volume:**
- Named volume `agency-infra-admin-model-data` mounted at `/root/.ollama`
- Stores model weights (~2GB for qwen2.5:3b)
- Preserved through `admin destroy` (same as knowledge data)
- Wiped only with `--permadeath`

### Enforcer Routing

A new `admin` provider added to the default routing.yaml:

```yaml
providers:
  admin:
    api_base: "http://agency-infra-admin-model:11434/v1/"
    auth_env: ""

models:
  admin:
    provider: admin
    provider_model: "qwen2.5:3b"
    cost_per_mtok_in: 0.0
    cost_per_mtok_out: 0.0
```

This enables any future consumer to route through the enforcer with `model: "admin"`. Zero cost entries ensure budget tracking remains accurate without counting admin work against spend.

The existing `ollama` provider (`host.docker.internal:11434`) is untouched — that serves operators with their own GPU hardware and models.

No Go enforcer code changes required. The enforcer's existing model resolution logic handles the new provider identically to Anthropic/OpenAI.

### Synthesizer Changes

The synthesizer (`agency/images/knowledge/synthesizer.py`) gets three changes:

**1. Fix Tenet 3 violation.** The current `_call_llm()` method calls `api.anthropic.com` directly. Replace with calls to the admin model on the mediation network (`http://agency-infra-admin-model:11434/v1/chat/completions`).

**2. Local-first with Haiku fallback.** New call flow:

```
synthesize() called
  → try admin model (http://agency-infra-admin-model:11434/v1/chat/completions)
    → parse JSON response
    → if valid extraction with entities: use it, done
    → if parse failure, empty extraction, or connection error:
      → log the failure
      → fall back to Haiku via enforcer
      → if Haiku also fails: log error, skip this batch
```

The fallback path calls Haiku through the egress proxy on the mediation network. The knowledge container already has `HTTPS_PROXY` configured to the egress proxy (`http://egress:3128`). The fallback uses the same `httpx.Client` with proxy settings to call `api.anthropic.com` — but now the request goes through egress (credential swap, domain filtering, audit) rather than directly to the internet. This fixes the Tenet 3 violation for the fallback path too. The API key for Anthropic is injected by the egress proxy's credential swap addon, so the knowledge container never holds it directly.

If `KNOWLEDGE_SYNTH_FALLBACK=false`, no Haiku fallback is attempted — the batch is skipped and retried next cycle.

**Timeout and connection cleanup.** Each call (admin model and Haiku fallback) has its own independent timeout budget of `KNOWLEDGE_SYNTH_TIMEOUT` seconds. If the admin model call times out, the `httpx.Client` connection is immediately closed before the fallback path begins. The Haiku fallback is a separate HTTP call through the egress proxy — it does not inherit connection state from the failed admin model call. If both calls time out, the synthesis batch is skipped with no partial state written and retried next cycle.

**3. Lazy model pull.** On first call, if the admin model returns a "model not found" error, the synthesizer triggers `POST http://agency-infra-admin-model:11434/api/pull {"name": "<model>"}` and waits for completion. Pull progress is logged. Subsequent calls hit the cached model.

**Configuration:**

| Variable | Default | Description |
|---|---|---|
| `KNOWLEDGE_LOCAL_MODEL` | `qwen2.5:3b` | Ollama model name to pull and use |
| `KNOWLEDGE_LOCAL_MODEL_ENABLED` | `true` | Set `false` to skip admin model container entirely and route synthesis to Haiku |
| `KNOWLEDGE_SYNTH_TIMEOUT` | `120` | Timeout in seconds for synthesis LLM calls |
| `KNOWLEDGE_SYNTH_FALLBACK` | `true` | Enable Haiku fallback on local model failure |

When `KNOWLEDGE_LOCAL_MODEL_ENABLED=false`, the admin model container is not started and the synthesizer routes directly to Haiku via the egress proxy. This is a single control — no separate `KNOWLEDGE_SYNTH_MODEL` variable needed.

**Lazy model pull timeout.** The `POST /api/pull` call to Ollama has a 10-minute timeout. If the download exceeds this (very slow connection), the synthesis cycle is skipped and the pull is retried on the next cycle. This is an Ollama-specific API endpoint (`/api/pull`), not part of the OpenAI-compatible surface — if the admin model backend were ever swapped to a non-Ollama provider, the pull mechanism would need to be adapted.

**Source type tracking.** Nodes created by the local model get `source_type: "local"` (vs. `"llm"` for Haiku, `"rule"` for rule-based). The `_SOURCE_PRIORITY` in `store.py` places `"local"` below `"llm"`: `{"agent": 3, "llm": 2, "local": 1, "rule": 1}`. This means when the curator merges duplicate nodes, an LLM-sourced summary takes precedence over a local-model-sourced summary — reflecting the capability difference. This provides provenance for quality comparison — operators can see whether local model extractions are retained, merged, or pruned at different rates than Haiku extractions.

### Operator Controls

**CLI commands** under `agency admin model`:

- `agency admin model` — Show admin model status: container health, model name, download state, size
- `agency admin model pull` — Force download or update the configured model

**MCP tool:** New `agency_admin_model` tool with actions:
- `status` — Model container health, model info, download state
- `pull` — Trigger model download/update

**Disabling the admin model:**
- Set `KNOWLEDGE_LOCAL_MODEL_ENABLED=false` before `agency infra up`
- Container is not started
- Synthesizer routes directly to Haiku (or skips if no fallback configured)
- CLI commands report "Admin model disabled"

### `admin destroy` Behavior

The `agency-infra-admin-model-data` volume is preserved by default during `admin destroy`, alongside the knowledge graph and API keys. Only destroyed with `--permadeath`. The admin model container itself is always destroyed (it's stateless — the volume holds the state).

Add to the destroy summary output:
```
  Model volume:   preserved
```

Or with `--permadeath`:
```
  Model volume:   destroyed
```

## Module Structure

```
agency/images/knowledge/
├── synthesizer.py          ← Modified: local-first + fallback, lazy pull, egress-proxied Haiku

agency/images/knowledge/
├── store.py                ← Minor: source_type "local" in _SOURCE_PRIORITY

agency/core/
├── infrastructure.py       ← Modified: admin-model container lifecycle (pull upstream image, volume, network)
├── admin_model.py          ← New: model status, pull, health check helpers

agency/commands/
├── admin_cmd.py            ← Modified: admin model subcommands

agency_core/models/
├── routing.py              ← Modified: admin provider in default config

agency/
├── mcp_server.py           ← Modified: agency_admin_model tool
```

Note: No `agency/images/admin-model/` directory. The container uses the upstream `ollama/ollama` image directly — pulled by `infrastructure.py`, not built from a Dockerfile.

## Threat Model

### Attack Surface

The admin model container introduces a new component on the mediation network with its own threat profile.

**Model weight poisoning.** An attacker who gains write access to the Docker volume could swap model weights to produce attacker-controlled extraction output. Mitigation: the volume is local-only with no network exposure. All model output is validated (must parse as JSON with expected schema). Malformed output triggers Haiku fallback. The curator (spec #1) catches anomalous nodes downstream.

**Model download integrity.** Model weights are downloaded via Ollama's `/api/pull` endpoint over the host's network connection to the Ollama registry (`registry.ollama.ai`). Ollama enforces TLS verification on registry connections and validates model weight integrity via SHA-256 digest checksums after download. If a pull is interrupted mid-download, subsequent synthesis calls retry the pull from scratch. Operators can verify a model before first synthesis with `agency admin model pull`. For high-security deployments where registry MITM is a concern, operators should pre-pull the model on the host and verify its checksum before starting agency infrastructure.

**Output injection via graph.** The local model produces entity/relationship extractions that flow into the knowledge graph. A compromised model could inject plausible-but-wrong entities or relationships designed to influence agents via GraphRAG briefings. Mitigation: local model output receives the same structural validation as Haiku output (JSON schema compliance). Additionally, all local model extractions are created with `source_type: "local"` and lower source priority than `"llm"` — when the curator merges duplicates, LLM-sourced summaries take precedence. The curator's anomaly detection flags burst contributions. Operators can compare local vs. Haiku extraction quality via curation health metrics. See "Semantic validation" below for the graduated trust mechanism.

**Resource exhaustion.** A runaway inference request or pathological input could consume excessive CPU/memory. Mitigation: container memory limit (3GB hard cap). Synthesis requests have a timeout (configurable, default 120s). The synthesizer's existing min-interval guard (5 minutes) rate-limits calls.

**No exfiltration path.** The container sits on the mediation network only — no internet access, no egress proxy connection, no credentials. It receives prompts and returns completions. The only data it sees is the synthesis extraction prompt (message content from channels). It cannot reach any external endpoint.

**Prompt content exposure.** The admin model processes the same extraction prompt that Haiku currently receives — message content from agent channels. This is equivalent trust: the model sees what the synthesis pipeline already exposes to an external API. Moving to a local model actually reduces exposure (data stays on-premises rather than going to Anthropic).

**Semantic validation (Tenet 5).** Valid JSON with correct schema does not guarantee semantically correct extractions. A small local model may hallucinate entities, invent relationships, or produce low-quality summaries. Mitigation: graduated trust mechanism. The synthesizer tracks a `local_model_validated` flag (persisted in knowledge service state). On first use, the first 3 synthesis batches are run through both the admin model and Haiku. If the admin model's entity extraction recall is ≥70% compared to Haiku (measured by label overlap), the flag is set to `true` and subsequent batches use local-only with Haiku fallback. If recall is below threshold, a warning is logged and the synthesizer continues dual-running until the threshold is met or the operator explicitly sets `KNOWLEDGE_LOCAL_MODEL_VALIDATED=true` to bypass. This ensures the local model is producing reasonable output before it's trusted as the primary extraction source.

**Authorization boundary (Tenet 12).** The admin model's synthesis output does not grant authorization to merge, delete, or modify graph nodes. Synthesis creates entities with `source_type: "local"` — the curator (spec #1) independently decides whether to merge duplicates, subject to its own cross-channel merge prevention rules. Specifically, the curator's `fuzzy_duplicate_scan` checks `source_channels` overlap before auto-merging: if two nodes share no channels, the merge is blocked and the pair is flagged for operator review instead. A synthesis extraction from channel `#private` cannot cause a merge with a node sourced from `#other-private` — the curator enforces this boundary regardless of extraction source. The admin model has no awareness of or influence over curation policy. See the [Knowledge Graph Curator spec](/specs/knowledge-graph-curator) for full cross-channel merge policy.

**Model swap atomicity (Tenet 6).** When an operator changes `KNOWLEDGE_LOCAL_MODEL` and restarts, there is a window during model pull where the admin model is unavailable. This is expected behavior — the Haiku fallback path covers this window. The synthesizer logs `model_swap_in_progress` during this period. The `local_model_validated` flag is reset on model change, triggering the graduated trust mechanism again for the new model.

### Comparison to Status Quo

The current design is worse from a security perspective: the synthesizer calls `api.anthropic.com` directly, bypassing all mediation (Tenet 3 violation). The admin model fixes this by keeping synthesis on the internal network.

## ASK Compliance

| Tenet | How this spec complies |
|---|---|
| **Foundation** | |
| Tenet 1 (Constraints external) | Admin model is infrastructure, not an agent. It does not influence enforcement. Constraint enforcement remains in the enforcer. |
| Tenet 2 (Every action traced) | Synthesis activity logged with structured fields: `model_used`, `fallback_triggered`, `fallback_reason`, `entities_extracted`, `relationships_extracted`, `source_type`. Model pull/status operations logged. See "Fallback audit schema" below. |
| Tenet 3 (Mediation complete) | Fixes the existing Tenet 3 violation. Synthesizer no longer calls Anthropic directly. Local inference stays on mediation network. Haiku fallback goes through egress proxy. |
| Tenet 4 (Least privilege) | Container has no credentials, no internet access, no volume mounts beyond its own model data. Cannot reach egress or external networks. |
| Tenet 5 (No blind trust) | Graduated trust mechanism validates local model output quality against Haiku baseline before trusting it as primary source. Source provenance tracked via `source_type: "local"` with lower merge priority. See "Semantic validation" in Threat Model. |
| **Constraint Lifecycle** | |
| Tenet 6 (Atomic and acknowledged) | Separate container on mediation network. Not collapsed into the knowledge service. Model swap window covered by Haiku fallback with explicit logging. Graduated trust resets on model change. |
| Tenet 7 (History immutable) | Knowledge service logs are append-only per existing design. Synthesis audit records are write-once. No mechanism to alter prior log entries. |
| **Halt Governance** | |
| Tenet 8 (Halts auditable) | Admin model container can be stopped via `agency infra down` or `admin destroy`. All lifecycle transitions logged. Container stop is immediate and clean (stateless process). |
| Tenet 9 (Halt authority asymmetric) | Only operators can start/stop the admin model container. The container cannot self-restart or influence its own lifecycle. |
| Tenet 10 (Authority monitored) | Model pull, status checks, and enable/disable operations are logged via CLI/MCP audit trail. Operators can review all admin model management actions. |
| **Multi-Agent Bounds** | |
| Tenet 11 (Delegation bounded) | Not directly applicable — admin model is infrastructure, not a delegated agent. It has no delegated permissions and cannot delegate further. |
| Tenet 12 (Synthesis ≠ authorization) | Synthesis output does not grant authorization to merge or modify graph nodes. The curator independently enforces merge policy including cross-channel boundaries. See "Authorization boundary" in Threat Model. |
| **Principal Model** | |
| Tenet 13 (Independent lifecycles) | Admin model container lifecycle is independent of any agent or principal. Stopping an agent does not affect the admin model; stopping the admin model does not affect agents. |
| Tenet 14 (Authority not orphaned) | Not applicable — admin model holds no authority. It is a passive inference endpoint with no permissions. |
| Tenet 15 (Trust earned) | Graduated trust mechanism: local model must demonstrate ≥70% entity recall vs. Haiku across 3 batches before being trusted as primary source. Trust resets on model swap. No self-elevation. |
| **Security** | |
| Tenet 16 (Quarantine) | Not applicable — admin model is infrastructure, not an agent subject to quarantine. Container can be stopped immediately if compromised. |
| Tenet 17 (Verified principals only) | Admin model receives prompts only from the knowledge service on the mediation network. No external entity can submit prompts to it. XPIA scanning occurs upstream in the enforcer before messages reach synthesis. |
| Tenet 18 (Zero trust default) | Local model output starts untrusted (graduated trust). Unknown/malformed output triggers fallback, not acceptance. New models require validation before trust. |
| Tenet 19 (External agents can't instruct) | Admin model is not an agent and cannot receive instructions. It processes extraction prompts and returns structured data. No instruction channel exists. |
| **Coordination** | |
| Tenet 20 (Yield and flag) | Not applicable — admin model does not participate in multi-agent coordination. It is a stateless inference endpoint. |
| Tenet 21 (Human termination operator-only) | Not applicable — no human principal interaction. |
| Tenet 22 (Humans not quarantined) | Not applicable — no human principal interaction. |
| **Organizational Knowledge** | |
| Tenet 23 (Knowledge is infrastructure) | Model weights persist as durable infrastructure in a named Docker volume. Operator controls lifecycle via CLI/MCP. Preserved through `admin destroy`. Knowledge extractions flow into the persistent graph. |
| Tenet 24 (Knowledge access bounded) | No change to graph ACLs. Local model extractions carry `source_channels` from the messages they were extracted from. Channel-scoped ACLs enforced at query time, not extraction time. |

### Fallback Audit Schema

Every synthesis attempt logs a structured record to the knowledge service log:

```json
{
  "event": "synthesis_attempt",
  "timestamp": "ISO-8601",
  "model_attempted": "qwen2.5:3b",
  "model_used": "qwen2.5:3b | claude-haiku-4-5-20251001",
  "fallback_triggered": false,
  "fallback_reason": null,
  "entities_extracted": 5,
  "relationships_extracted": 3,
  "source_type": "local | llm",
  "batch_size": 10,
  "duration_ms": 2340
}
```

When fallback is triggered, `fallback_reason` contains one of: `connection_refused`, `parse_error`, `empty_extraction`, `model_not_found`, `timeout`. This enables operators to track local model reliability and tune the configuration.

## Configuration

All configuration via environment variables with sensible defaults:

| Variable | Default | Description |
|---|---|---|
| `KNOWLEDGE_LOCAL_MODEL_ENABLED` | `true` | Enable/disable the admin model container and local-first synthesis |
| `KNOWLEDGE_LOCAL_MODEL` | `qwen2.5:3b` | Ollama model name to pull and use |
| `KNOWLEDGE_SYNTH_TIMEOUT` | `120` | Timeout in seconds for synthesis LLM calls |
| `KNOWLEDGE_SYNTH_FALLBACK` | `true` | Enable Haiku fallback on local model failure |
| `KNOWLEDGE_SYNTH_PULL_TIMEOUT` | `600` | Timeout in seconds for model weight download |
| `KNOWLEDGE_LOCAL_MODEL_VALIDATED` | `false` | Set `true` to bypass graduated trust validation |
| `KNOWLEDGE_LOCAL_MODEL_VALIDATION_BATCHES` | `3` | Number of dual-run batches for graduated trust |
| `KNOWLEDGE_LOCAL_MODEL_VALIDATION_THRESHOLD` | `0.70` | Minimum entity recall vs. Haiku to pass validation |

## Future Consumers

This spec establishes the admin model infrastructure. Future specs that will consume it:

- **Semantic curation pass** (deferred from curator spec #1): Uses the admin model to detect semantic duplicates, evaluate summary accuracy, and suggest new edges. Plugs into the curator's periodic loop as an additional operation after the heuristic pass.
- **Classification/routing** (spec #3, dynamic routing optimizer): Could use the admin model for task-type classification to inform routing decisions.
- **Summarization** (body runtime): Agent context summarization could be delegated to the admin model to reduce API costs.

Each consumer adds its own prompts and validation logic. The infrastructure (container, routing, lifecycle) is shared.

## Testing

### Unit Tests

- **Synthesizer local-first flow:** mock admin model response → verify extraction applied → verify `source_type: "local"` on created nodes
- **Synthesizer fallback:** mock admin model failure → verify Haiku fallback called → verify fallback logged
- **Synthesizer parse validation:** malformed JSON from admin model → verify fallback triggered, not crash
- **Lazy model pull:** mock "model not found" error → verify pull request sent → verify retry succeeds
- **Model pull already cached:** mock successful response → verify no pull triggered
- **Source priority:** verify `_SOURCE_PRIORITY` includes `"local"` at appropriate level
- **Graduated trust - dual run:** unvalidated model → verify both admin model and Haiku called → verify recall comparison
- **Graduated trust - validation pass:** recall ≥70% after 3 batches → verify `local_model_validated` set → subsequent calls local-only
- **Graduated trust - validation fail:** recall <70% → verify warning logged → verify dual-run continues
- **Graduated trust - bypass:** `KNOWLEDGE_LOCAL_MODEL_VALIDATED=true` → verify no dual-run, direct local
- **Graduated trust - model swap reset:** model name changes → verify `local_model_validated` reset to false
- **Fallback audit log:** verify structured log entry with all schema fields on each synthesis attempt
- **Disabled mode:** `KNOWLEDGE_LOCAL_MODEL_ENABLED=false` → verify no admin model calls, direct Haiku

### Integration Tests

- **Full synthesis cycle:** start admin model container → ingest messages → trigger synthesis → verify nodes created with `source_type: "local"`
- **Fallback integration:** admin model unavailable → verify Haiku fallback produces valid nodes
- **Model pull integration:** fresh container → first synthesis call → verify model downloaded → second call fast
- **Infrastructure lifecycle:** `infra up` starts container → `infra down` stops it → volume persists → `infra up` again → model still cached
- **CLI status:** `admin model` returns health info when running, appropriate message when disabled
- **Destroy behavior:** `admin destroy` preserves volume → `admin destroy --permadeath` wipes it
