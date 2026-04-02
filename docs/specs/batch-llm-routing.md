---
description: "Infrastructure LLM calls (knowledge synthesis, evaluation, memory consolidation) are not latency-sensitive but use th..."
---

# Batch LLM Routing

## Problem

Infrastructure LLM calls (knowledge synthesis, evaluation, memory consolidation) are not latency-sensitive but use the same synchronous API path as agent conversation turns. This wastes money — every provider offers 50% batch discounts for calls that can tolerate minutes instead of milliseconds.

## Provider Batch APIs

| Provider | Discount | SLA | Format | Endpoint |
|----------|----------|-----|--------|----------|
| Anthropic | 50% | 24h (typically 1-5 min) | JSON array of message requests | `POST /v1/messages/batches` |
| OpenAI | 50% | 24h (typically 1-10 min) | JSONL file upload | `POST /v1/batches` |
| Google Gemini | 50% | 24h (typically 5-30 min) | Vertex AI BatchPrediction | `projects/{p}/locations/{l}/batchPredictionJobs` |

All three return a batch ID. Poll for completion or receive a webhook/callback.

## Design

### Routing Config

Add a `batch` tier to `routing.yaml` alongside the existing model definitions:

```yaml
models:
  claude-haiku:
    provider: anthropic
    provider_model: claude-haiku-4-5-20251001
    cost_per_mtok_in: 0.80
    cost_per_mtok_out: 4.00

batch:
  enabled: true
  # Callers with latency_budget >= this threshold get batch-routed automatically
  auto_threshold_ms: 30000
  # Max time to wait for batch completion before falling back to sync
  max_wait_ms: 300000
  # Batch discount factor (for cost estimation before provider confirms)
  discount_factor: 0.50
  # Minimum requests to justify a batch (single requests go sync)
  min_batch_size: 1
```

### Gateway Batch Endpoint

New internal endpoint alongside the existing `/api/v1/internal/llm`:

```
POST /api/v1/internal/llm/batch
```

Request body — same as `/internal/llm` but with an additional `batch_options` field:

```json
{
  "model": "claude-haiku",
  "messages": [{"role": "user", "content": "..."}],
  "max_tokens": 4096,
  "batch_options": {
    "callback_url": "",
    "max_wait_ms": 300000
  }
}
```

Response modes:

1. **Synchronous wait** (default): Gateway submits to provider batch API, polls for completion, returns the result when ready. Caller sees a normal response, just slower. Timeout controlled by `max_wait_ms`.

2. **Async callback**: If `callback_url` is set, gateway returns immediately with a batch ID. Posts the result to the callback URL when complete.

```json
// Immediate response (async mode)
{
  "batch_id": "batch_abc123",
  "status": "submitted",
  "estimated_completion_ms": 60000
}

// Callback POST to callback_url
{
  "batch_id": "batch_abc123",
  "status": "completed",
  "result": { /* standard chat completion response */ }
}
```

### Auto-Engagement Logic

The existing `/api/v1/internal/llm` endpoint gains automatic batch routing based on the caller's latency budget:

```go
func (h *handler) internalLLM(w http.ResponseWriter, r *http.Request) {
    caller := r.Header.Get("X-Agency-Caller")

    // Check if caller has declared a latency budget
    latencyBudget := r.Header.Get("X-Agency-Latency-Budget-Ms")

    // Auto-route to batch if:
    // 1. Batch is enabled in routing config
    // 2. Caller's latency budget exceeds the auto threshold
    // 3. The model's provider supports batching
    if shouldBatchRoute(latencyBudget, batchConfig) {
        return h.internalLLMBatch(w, r)
    }

    // ... existing sync path
}
```

Callers declare their latency tolerance via header:

```python
# Synthesizer — can wait up to 5 minutes
headers["X-Agency-Latency-Budget-Ms"] = "300000"

# Evaluator — can wait up to 2 minutes
headers["X-Agency-Latency-Budget-Ms"] = "120000"

# Context summarizer — needs response in 10 seconds (stays sync)
headers["X-Agency-Latency-Budget-Ms"] = "10000"
```

### Provider Adapters

Each provider needs a batch adapter in the gateway:

```go
type BatchAdapter interface {
    // Submit a batch of requests. Returns a batch ID.
    Submit(ctx context.Context, requests []BatchRequest) (string, error)

    // Poll for batch completion. Returns results when ready.
    Poll(ctx context.Context, batchID string) (*BatchResult, error)

    // Cancel a pending batch.
    Cancel(ctx context.Context, batchID string) error
}
```

**Anthropic adapter:**
```
POST /v1/messages/batches
Body: { requests: [{ custom_id: "req-1", params: { model: "...", messages: [...] } }] }

GET /v1/messages/batches/{id}
Response: { processing_status: "ended", results_url: "..." }

GET {results_url}
Response: JSONL of results
```

**OpenAI adapter:**
```
# Upload JSONL file
POST /v1/files  (purpose: "batch")

# Create batch
POST /v1/batches
Body: { input_file_id: "file-abc", endpoint: "/v1/chat/completions", completion_window: "24h" }

# Poll
GET /v1/batches/{id}
Response: { status: "completed", output_file_id: "file-xyz" }

# Download results
GET /v1/files/{output_file_id}/content
```

**Google adapter:**
```
# Submit via Vertex AI
POST /v1/projects/{p}/locations/{l}/batchPredictionJobs
Body: { model: "gemini-...", inputConfig: { instancesFormat: "jsonl", gcsSource: { uris: [...] } } }

# Poll
GET /v1/projects/{p}/locations/{l}/batchPredictionJobs/{id}
```

### Cost Tracking

Batch requests get a distinct `X-Agency-Cost-Source` value:

```
X-Agency-Cost-Source: synthesizer_batch
```

The `by_source` metrics dimension (already shipped) will automatically separate batch vs sync costs. The audit event includes `"batch": true` and `"batch_id": "..."` for traceability.

Estimated cost is recorded at submission time using the discount factor. Actual cost (from provider usage response) replaces it on completion.

### Synthesizer Integration

The synthesizer is the first consumer. Changes to `_call_llm`:

```python
def _call_llm(self, prompt: str) -> str | None:
    headers = {
        "X-Agency-Token": self._gateway_token,
        "X-Agency-Caller": "knowledge-synthesizer",
        "X-Agency-Latency-Budget-Ms": "300000",  # 5 minutes is fine
    }
    # Gateway auto-routes to batch if threshold met
    resp = self._http_gateway.post(
        f"{self._gateway_url}/api/v1/internal/llm",
        json={"model": self._fallback_model, "messages": [...], "max_tokens": 4096},
        headers=headers,
    )
```

No code change needed in the synthesizer beyond adding the header — the gateway handles batch routing transparently. The synchronous-wait mode means the synthesizer's existing flow (call LLM, parse response, apply extraction) works unchanged.

### Future Consumers

| Caller | Latency Budget | Batch Candidate? |
|--------|---------------|-----------------|
| `knowledge-synthesizer` | 5 min | Yes |
| `platform-evaluation` | 2 min | Yes |
| `knowledge-curator` | 5 min | Yes |
| Agent conversation turns | < 1 sec | No (enforcer path, not gateway internal) |

### Multi-Request Batching

The synchronous-wait mode submits one request per batch call. For higher throughput, the gateway can accumulate requests:

```go
// BatchAccumulator collects requests over a short window and submits them
// as a single provider batch. This amortizes the batch overhead.
type BatchAccumulator struct {
    mu       sync.Mutex
    pending  []pendingRequest
    timer    *time.Timer
    window   time.Duration  // e.g., 5 seconds
}
```

When a batch request arrives:
1. Add to accumulator
2. If timer isn't running, start a 5-second window
3. When window closes (or pending hits a count threshold), submit all accumulated requests as one provider batch
4. Each caller blocks on its own response channel until the batch completes

This is an optimization for later — single-request batching already gets the 50% discount.

### Failure Handling

- **Batch timeout**: If `max_wait_ms` elapses before the batch completes, fall back to a synchronous call. Log the fallback. The caller gets a response either way.
- **Batch rejected by provider**: Fall back to sync immediately.
- **Batch partially failed**: Return errors for failed items, results for succeeded. The synthesizer already handles LLM call failures gracefully.
- **Gateway restart during pending batch**: Batch IDs are persisted to `~/.agency/batch/pending.json`. On startup, the gateway checks for incomplete batches and resumes polling.

### Audit

Every batch operation is audited:

```json
{"type": "LLM_BATCH_SUBMIT", "source": "knowledge-synthesizer", "batch_id": "batch_abc", "model": "claude-haiku", "request_count": 1}
{"type": "LLM_BATCH_COMPLETE", "source": "knowledge-synthesizer", "batch_id": "batch_abc", "duration_ms": 45000, "input_tokens": 2100, "output_tokens": 800, "cost_usd": 0.0024}
{"type": "LLM_BATCH_FALLBACK", "source": "knowledge-synthesizer", "batch_id": "batch_abc", "reason": "timeout"}
```

These are `LLM_BATCH_*` types so the routing metrics `isLLMType` function picks them up automatically.

## Implementation Order

1. **Routing config** — add `batch` section to routing.yaml schema and parsing
2. **Anthropic batch adapter** — submit, poll, cancel (Anthropic is our primary provider)
3. **Gateway batch endpoint** — `/api/v1/internal/llm/batch` with sync-wait mode
4. **Auto-routing** — `X-Agency-Latency-Budget-Ms` header, threshold check in existing `/internal/llm`
5. **Synthesizer header** — add latency budget header to `_call_llm`
6. **OpenAI batch adapter** — same interface, different wire format
7. **Google batch adapter** — Vertex AI integration
8. **Multi-request accumulator** — batch window for throughput optimization
9. **Pending batch persistence** — survive gateway restarts

Steps 1-5 get the 50% discount working for the primary use case (Anthropic + synthesizer). Steps 6-9 are incremental.

## What This Enables

- **50% cost reduction** on all non-latency-sensitive LLM calls with zero code changes in callers (just add a header)
- **Transparent routing** — callers don't know or care whether they got batch or sync
- **Provider-agnostic** — same header works across Anthropic, OpenAI, Google
- **Observable** — batch vs sync costs visible in `by_source` metrics, separate audit trail
- **Graceful degradation** — timeout → sync fallback, never blocks callers indefinitely
