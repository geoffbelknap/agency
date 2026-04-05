# Stateful Context Compression

**Status:** Draft
**Depends on:** Hybrid model routing (for cheap summarization model)

## Problem

Tool outputs are appended raw to the conversation. A tool returning a 15,000-token JSON payload adds 15,000 tokens to every subsequent LLM call. Over a 10-step workflow, context grows multiplicatively — step 10 pays for the accumulated weight of steps 1-9.

Existing context management (`_manage_context` in `body.py`) only triggers at 70% of the context window (~140K tokens). By that point, the agent has already burned significant tokens on inflated context across many calls. The summarization is reactive — it compresses after damage is done, not before.

## What Already Exists

**Context management** (`body.py` lines 2080-2194):
- Token estimation: `len(content) / 4`
- Threshold trigger: 70% of context window (default 140K tokens)
- LLM summarization of old messages (keeps system + last 10)
- Naive fallback: truncate each old message to 200 chars
- Cost source `context_summary` defined but not yet wired

**Tool result handling** (`body.py` lines 1648-1706):
- Tool results appended directly: `messages.append({"role": "tool", "content": result})`
- No transformation, truncation, or summarization before append
- Parallel tool calls append all results in order

**Enforcer**:
- Sees messages array but does not modify it (ASK Tenet 3 — mediates, doesn't alter agent reasoning)
- XPIA scans tool-role messages (read-only)

## Design

### Proactive tool output compression

Compress large tool outputs *before* they enter the messages array, not after the context window is already bloated.

When a tool returns output exceeding a configurable token threshold, the body runtime:
1. Stores the raw output in a local reference (for crash recovery / audit)
2. Sends the raw output to a summarization call (cheap/fast model via hybrid routing)
3. Appends the compressed summary to messages instead of the raw output
4. Tags the message so the agent knows it's a summary, not raw data

### Where this happens: the body runtime

Context compression lives in the body runtime, not the enforcer, because:
- The body runtime owns the messages array and the conversation loop
- Compression is an agent-internal optimization, not an enforcement action
- The enforcer should see the final messages the agent reasons over (for XPIA scanning and audit)
- The body runtime already has the summarization infrastructure (`_summarize_messages`)

### Implementation

**New method in `body.py`:**

```python
COMPRESS_THRESHOLD = 2000  # tokens (~8000 chars)
COMPRESS_MAX_OUTPUT = 500  # target summary size in tokens

def _compress_tool_output(self, tool_name: str, raw_output: str, task_context: str) -> str:
    """Compress a large tool output to a dense summary."""
    estimated_tokens = len(raw_output) // CHARS_PER_TOKEN
    if estimated_tokens <= COMPRESS_THRESHOLD:
        return raw_output  # Small enough, pass through

    # Store raw output for reference
    self._store_raw_output(tool_call_id, raw_output)

    # Summarize via LLM (routed to fast/mini tier by hybrid routing rules)
    summary = self._call_llm_summarize(
        tool_name=tool_name,
        raw_output=raw_output[:MAX_SUMMARIZE_INPUT],  # cap input to summarizer
        task_context=task_context,  # what the agent is trying to accomplish
        cost_source="context_compression",
    )

    return f"[Compressed from {estimated_tokens} tokens]\n{summary}\n[Full output available via recall_tool_output('{tool_call_id}')]"
```

**Integration in conversation loop** (around line 1658):

```python
# Before: append raw
messages.append({"role": "tool", "tool_call_id": id, "content": result})

# After: compress if large, then append
compressed = self._compress_tool_output(tool_name, result, task_context=current_objective)
messages.append({"role": "tool", "tool_call_id": id, "content": compressed})
```

### Summarization prompt

The summarization call receives:
- The tool name (so it knows what kind of data this is)
- The raw output (capped at ~20K tokens to avoid blowing up the summarizer's context)
- The agent's current objective (from the task/mission description)

The prompt instructs the summarizer to:
- Extract only findings relevant to the agent's current objective
- Preserve specific values (IPs, hashes, timestamps, error codes, names)
- Flag anomalies or unexpected results
- Note what was omitted so the agent can request the full output if needed

### Raw output recall

Compressed tool outputs include a pointer to the full data. A new built-in tool allows the agent to retrieve it:

```python
def recall_tool_output(tool_call_id: str, section: str = None) -> str:
    """Retrieve the full raw output of a previous tool call.
    
    Args:
        tool_call_id: The tool call ID from the compressed output tag.
        section: Optional - retrieve only a specific section (e.g., "errors", "first 100 lines").
    """
```

This ensures compression is lossless from the agent's perspective — if the summary omits something important, the agent can get the original.

### Storage for raw outputs

Raw outputs are stored in a task-scoped temporary directory:
- Path: `/agency/agent/tool-outputs/{task_id}/{tool_call_id}.json`
- Cleaned up on task completion (after memory capture)
- Included in crash recovery persistence
- Not sent to the knowledge graph (ephemeral, task-scoped)

### Cost attribution

Compression LLM calls use a new cost source: `context_compression`. This:
- Appears in audit entries and economics metrics
- Is a natural hybrid routing target (route to fast/mini tier)
- Lets operators see the cost of compression vs. the savings from smaller context

### Configurable thresholds

In the agent's mission config or platform defaults:

```yaml
context:
  compress_threshold: 2000    # tokens — outputs larger than this get compressed
  compress_target: 500        # tokens — target summary size
  compress_model_tier: fast   # tier for compression calls (default: fast)
  compress_enabled: true      # can be disabled per-agent
```

### Interaction with existing context management

The existing `_manage_context` (70% threshold summarization) remains as a safety net. With proactive compression:
- Individual tool outputs stay small (500 tokens instead of 15,000)
- Context grows linearly instead of multiplicatively
- The 70% threshold triggers much later (or never for typical workflows)
- When it does trigger, there's less to summarize

### Interaction with hybrid routing

Compression calls are tagged with `X-Agency-Cost-Source: context_compression`. The hybrid routing rules (from the hybrid-model-routing spec) can route these to a fast/mini tier automatically:

```yaml
routing_rules:
  - match:
      cost_source: context_compression
    tier: mini
    reason: "Compression is structured summarization — doesn't need frontier"
```

## What This Does NOT Include

- **Semantic caching** — compression reduces per-call context size; caching avoids making the call entirely. Separate concern.
- **Cross-agent context sharing** — compressed summaries are per-agent, per-task. Multi-agent context passing is a coordination problem.
- **Enforcer-side compression** — the enforcer doesn't modify messages. Compression is a body runtime optimization.

## Sequencing

1. Add `_compress_tool_output()` method to body runtime
2. Add raw output storage and `recall_tool_output` built-in tool
3. Wire compression into tool result handling in conversation loop
4. Add `context_compression` cost source to enforcer recognition
5. Add configuration support (thresholds, enable/disable)
6. Wire existing `context_summary` cost source for `_manage_context` calls
7. Add compression metrics to economics observability (compression ratio, cost saved)

Steps 1-3 are the core. Steps 4-7 are instrumentation and configuration.
