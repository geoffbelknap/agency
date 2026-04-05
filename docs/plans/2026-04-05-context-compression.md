# Context Compression Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Compress large tool outputs before they enter the messages array, reducing context window growth from multiplicative to linear.

**Architecture:** Body runtime intercepts tool results exceeding a token threshold, sends them to a cheap LLM summarization call (routed via hybrid routing to fast/mini tier), and appends the compressed summary instead. Raw outputs stored on disk for recall via `recall_tool_output` built-in tool. Compression is proactive (before context bloat) vs. the existing reactive `_manage_context` (after 70% threshold).

**Tech Stack:** Python (body runtime)

**Spec:** `docs/specs/context-compression.md`

---

### Task 1: Add _compress_tool_output method

**Files:**
- Modify: `images/body/body.py`

- [ ] **Step 1: Add compression constants and config**

Add near the top of body.py (near other constants):

```python
COMPRESS_THRESHOLD_CHARS = 8000   # ~2000 tokens
COMPRESS_TARGET_TOKENS = 500     # target summary size
COMPRESS_MAX_INPUT_CHARS = 80000  # cap input to summarizer (~20K tokens)
```

- [ ] **Step 2: Add _get_compression_config helper**

Add to the Body class:

```python
def _get_compression_config(self) -> dict:
    """Get compression configuration from mission or defaults."""
    mission = getattr(self, '_current_mission', None) or {}
    defaults = {
        "enabled": True,
        "threshold_chars": COMPRESS_THRESHOLD_CHARS,
        "target_tokens": COMPRESS_TARGET_TOKENS,
    }
    ctx_config = mission.get("context", {})
    return {
        "enabled": ctx_config.get("compress_enabled", defaults["enabled"]),
        "threshold_chars": ctx_config.get("compress_threshold", 2000) * 4,  # tokens to chars
        "target_tokens": ctx_config.get("compress_target", defaults["target_tokens"]),
    }
```

- [ ] **Step 3: Add _compress_tool_output method**

```python
def _compress_tool_output(self, tool_name: str, tool_call_id: str, raw_output: str) -> str:
    """Compress a large tool output to a dense summary via LLM."""
    config = self._get_compression_config()
    if not config["enabled"]:
        return raw_output

    if len(raw_output) <= config["threshold_chars"]:
        return raw_output

    # Store raw output for recall
    self._store_raw_tool_output(tool_call_id, raw_output)

    estimated_tokens = len(raw_output) // 4
    target = config["target_tokens"]
    task_context = getattr(self, '_task_content', 'general task')

    # Build summarization prompt
    summarize_messages = [
        {"role": "system", "content": (
            f"You are a data compression assistant. Summarize the following tool output "
            f"in approximately {target} tokens. Preserve specific values (IPs, hashes, "
            f"timestamps, error codes, names, counts). Flag anomalies. Note what was omitted."
        )},
        {"role": "user", "content": (
            f"Tool: {tool_name}\n"
            f"Agent's current task: {task_context[:500]}\n\n"
            f"Tool output ({estimated_tokens} tokens):\n\n"
            f"{raw_output[:COMPRESS_MAX_INPUT_CHARS]}"
        )},
    ]

    try:
        url = f"{self.enforcer_url}/chat/completions"
        headers = {"X-Agency-Cost-Source": "context_compression"}
        if getattr(self, "_event_id", None):
            headers["X-Agency-Event-Id"] = self._event_id
        if self._current_task_id:
            headers["X-Agency-Task-Id"] = self._current_task_id
        api_key = os.environ.get("OPENAI_API_KEY")
        if api_key:
            headers["Authorization"] = f"Bearer {api_key}"

        resp = self._http_client.post(
            url,
            json={
                "model": self.model,
                "messages": summarize_messages,
                "max_tokens": target * 2,
            },
            headers=headers,
            timeout=30.0,
        )
        resp.raise_for_status()
        summary = resp.json().get("choices", [{}])[0].get("message", {}).get("content", "")

        if summary:
            return (
                f"[Compressed from {estimated_tokens} tokens]\n"
                f"{summary}\n"
                f"[Full output available via recall_tool_output('{tool_call_id}')]"
            )
    except Exception as e:
        log.warning("Tool output compression failed: %s — using raw output", e)

    return raw_output  # fallback: use raw if compression fails
```

- [ ] **Step 4: Add raw output storage**

```python
def _store_raw_tool_output(self, tool_call_id: str, raw_output: str) -> None:
    """Store raw tool output for later recall."""
    if not hasattr(self, '_raw_tool_outputs'):
        self._raw_tool_outputs = {}
    self._raw_tool_outputs[tool_call_id] = raw_output
```

- [ ] **Step 5: Build check**

```bash
cd images/body && python -c "import body" 2>&1 || echo "import check"
```

- [ ] **Step 6: Commit**

```bash
git add images/body/body.py
git commit -m "feat(body): add _compress_tool_output for proactive context compression"
```

---

### Task 2: Add recall_tool_output built-in tool

**Files:**
- Modify: `images/body/body.py` (tool registration)

- [ ] **Step 1: Find where built-in tools are registered**

Search for where tools like `complete_task` or `spawn_meeseeks` are registered. Look for tool definitions array or a register function.

```bash
grep -n "complete_task\|def.*register.*tool\|built.in.*tool\|_builtin_tools" images/body/body.py | head -10
```

- [ ] **Step 2: Add recall_tool_output tool definition**

In the tool definitions (wherever they're registered), add:

```python
{
    "type": "function",
    "function": {
        "name": "recall_tool_output",
        "description": "Retrieve the full raw output of a previous tool call that was compressed. Use when a compressed summary doesn't have enough detail.",
        "parameters": {
            "type": "object",
            "properties": {
                "tool_call_id": {
                    "type": "string",
                    "description": "The tool call ID from the compressed output tag",
                },
            },
            "required": ["tool_call_id"],
        },
    },
}
```

- [ ] **Step 3: Add the handler**

In the tool call dispatch (where `_handle_tool_call` routes to handlers), add:

```python
def _handle_recall_tool_output(self, args: dict) -> str:
    """Handle recall_tool_output tool call."""
    tool_call_id = args.get("tool_call_id", "")
    outputs = getattr(self, '_raw_tool_outputs', {})
    raw = outputs.get(tool_call_id)
    if raw is None:
        return json.dumps({"error": f"No stored output for tool_call_id '{tool_call_id}'. It may have expired or the ID is incorrect."})
    return raw
```

Wire it into the tool dispatch (find `_handle_tool_call` and add the case).

- [ ] **Step 4: Commit**

```bash
git add images/body/body.py
git commit -m "feat(body): add recall_tool_output built-in tool for compressed output retrieval"
```

---

### Task 3: Wire compression into conversation loop

**Files:**
- Modify: `images/body/body.py` (tool result appends, two locations)

- [ ] **Step 1: Read body.py to find tool result append locations**

There are two places where tool results are appended to messages:

1. **Single tool call** (around line 1709-1713):
```python
messages.append({
    "role": "tool",
    "tool_call_id": _tc["id"],
    "content": result,
})
```

2. **Parallel tool calls** (around line 1752-1757):
```python
messages.append({
    "role": "tool",
    "tool_call_id": tc["id"],
    "content": results[tc["id"]],
})
```

- [ ] **Step 2: Add compression to single tool call path**

Before the append at line 1709, add:

```python
                    # Compress large tool outputs proactively
                    result = self._compress_tool_output(_tool_name, _tc["id"], result)
```

- [ ] **Step 3: Add compression to parallel tool call path**

Before the append loop at line 1752, add compression for each result:

```python
                    # Compress large tool outputs proactively
                    for tc in tool_calls:
                        _tn = tc.get("function", {}).get("name", "")
                        results[tc["id"]] = self._compress_tool_output(_tn, tc["id"], results[tc["id"]])
```

Put this before the existing append loop, not inside it.

- [ ] **Step 4: Clean up raw outputs on task finalization**

In `_finalize_task`, add cleanup:

```python
    # Clean up stored raw tool outputs
    self._raw_tool_outputs = {}
```

- [ ] **Step 5: Run tests**

```bash
pytest images/tests/ -x -q --timeout=30 --ignore=images/tests/test_realtime_comms_e2e.py --ignore=images/tests/test_comms_e2e.py
```

- [ ] **Step 6: Commit**

```bash
git add images/body/body.py
git commit -m "feat(body): wire tool output compression into conversation loop"
```

---

### Task 4: Add routing rule + config + docs + PR

**Files:**
- Modify: `internal/config/init.go` (add compression routing rule to defaults)
- Modify: `images/body/task_tier.py` (add compression config to cost modes)
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add compression routing rule**

In `internal/config/init.go`, find where default routing rules are seeded in routing.local.yaml (added in E2). Add:

```yaml
  - match:
      cost_source: context_compression
    tier: mini
    reason: "Compression is structured summarization"
```

- [ ] **Step 2: Add compression config to cost modes**

In `images/body/task_tier.py`, find `COST_MODE_DEFAULTS` and add a `context` block:

For "frugal":
```python
"context": {"compress_enabled": True, "compress_threshold": 1500, "compress_target": 300},
```

For "balanced":
```python
"context": {"compress_enabled": True, "compress_threshold": 2000, "compress_target": 500},
```

For "thorough":
```python
"context": {"compress_enabled": True, "compress_threshold": 3000, "compress_target": 800},
```

- [ ] **Step 3: Documentation**

Add to CLAUDE.md Key Rules:

```
- **Context compression**: Large tool outputs (>2000 tokens) are summarized by a cheap LLM call before entering the messages array. Compression calls tagged `X-Agency-Cost-Source: context_compression` route to mini tier via hybrid routing. Raw outputs stored on disk and retrievable via `recall_tool_output` built-in tool. Configuration in mission YAML `context:` block or cost_mode defaults.
```

- [ ] **Step 4: Build and test**

```bash
go build ./...
pytest images/tests/ -x -q --timeout=30 --ignore=images/tests/test_realtime_comms_e2e.py --ignore=images/tests/test_comms_e2e.py
```

- [ ] **Step 5: Create branch and push**

```bash
git checkout -b feature/context-compression
git push -u origin feature/context-compression
```

- [ ] **Step 6: Create PR**

```bash
gh pr create --title "feat: proactive context compression for large tool outputs" --body "$(cat <<'EOF'
## Summary

Proactive compression of large tool outputs before they enter the messages array:

- **_compress_tool_output()**: Sends large outputs (>2000 tokens) through a cheap LLM summarization call
- **recall_tool_output**: Built-in tool for agents to retrieve full raw output when summary is insufficient
- **Routing integration**: Compression calls tagged `context_compression` → routed to mini tier
- **Configuration**: Per-mission via `context:` block or cost_mode defaults

## Key files

- `images/body/body.py` — compression method, recall tool, conversation loop wiring
- `images/body/task_tier.py` — cost mode defaults
- `internal/config/init.go` — routing rule seed

## Test plan

- [ ] `pytest images/tests/` passes
- [ ] `go build ./...` clean
- [ ] Manual: tool returning >8000 chars gets compressed, recall_tool_output retrieves original

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 7: Commit all**

Three commits: routing rule, cost mode config, docs. Or combine into one:

```bash
git add internal/config/init.go images/body/task_tier.py CLAUDE.md
git commit -m "feat: context compression config, routing rule, and documentation"
```
