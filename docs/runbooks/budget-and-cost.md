# Budget & Cost Management

## Trigger

Configuring agent budgets, investigating cost overruns, understanding cost attribution, or reviewing economics data.

## Budget Model

Agency uses USD-denominated cost control at three granularities:

| Level | Scope | Persistence |
|-------|-------|-------------|
| Task | Single task execution | Enforcer in-process |
| Daily | Rolling 24h window | Gateway state |
| Monthly | Calendar month | Gateway state |

Budget exhaustion is a **hard stop** — the agent cannot continue. There is no override. This replaces the old turn-limit model.

### Per-task overage behavior

When a single task exceeds its `task` budget, the enforcer fails the task in-process and returns the cost-overage error to the gateway. The agent is halted at that boundary; partial work up to the overage point is preserved in audit logs and recoverable via `agency log <agent>`. Daily and monthly budgets are checked at the gateway before each task is dispatched — an exhausted daily/monthly bucket prevents new tasks from starting.

There is no "soft" overage mode. Task-level budgets are always fail-closed.

### Mission-based fallback _(experimental)_

If an agent is running under a mission with a configured `fallback_policy` (a `TierExperimental` feature; see [Mission Management](experimental/mission-management.md)), the mission's policy determines what happens *after* a budget halt — for example, routing to a cheaper model tier on retry, escalating to an operator notification, or pausing the mission. The fail-closed behavior at the budget boundary is unchanged; mission fallbacks operate on the *next* task, not the in-flight one.

Outside of missions (the default 0.2.x core path), there is no automated fallback — operators handle overages manually via budget adjustment or agent reconfiguration.

## Configuring Budgets

Budgets are set in the agent config or mission YAML:

```yaml
# In agent.yaml or mission YAML
budget:
  task: 0.50      # USD per task
  daily: 5.00     # USD per day
  monthly: 50.00  # USD per month
```

Budget thresholds for graduated alerting:

```yaml
budget:
  task: 0.50
  daily: 5.00
  monthly: 50.00
  alert_thresholds: [0.5, 0.8, 0.95]  # percentage triggers
```

## Checking Budget State

```bash
agency show <agent-name>
```

Via API:

```bash
# Current budget state
curl -sf -H "Authorization: Bearer $TOKEN" \
  http://localhost:8200/api/v1/agents/<name>/budget

# Remaining budget
curl -sf -H "Authorization: Bearer $TOKEN" \
  http://localhost:8200/api/v1/agents/<name>/budget/remaining
```

## Cost Attribution

Every LLM call is tagged with a cost source via the `X-Agency-Cost-Source` header:

| Source | What It Is |
|--------|-----------|
| `agent_task` | Normal agent task execution |
| `reflection` | Reflection loop evaluation |
| `evaluation` | Success criteria LLM evaluation |
| `memory_capture` | Procedural/episodic memory extraction |
| `consolidation` | Memory consolidation (50+ procedures) |
| `context_summary` | Context compression/summarization |

This lets you understand where tokens are being spent.

## Economics Observability

The enforcer records per-call metrics in the HMAC-signed audit log:

- **TTFT** — Time to first token
- **TPOT** — Time per output token
- **StepIndex** — Position in the task execution
- **ToolCallValid** — Whether the tool call was valid
- **RetryOf** — If this call is a retry of a previous call

The gateway aggregates into per-workflow rollups:

```bash
# Per-agent economics
curl -sf -H "Authorization: Bearer $TOKEN" \
  http://localhost:8200/api/v1/agents/<name>/economics

# Platform-wide summary
curl -sf -H "Authorization: Bearer $TOKEN" \
  http://localhost:8200/api/v1/agents/economics/summary
```

### Key rollup metrics

| Metric | What It Tells You |
|--------|------------------|
| Loop cost | Average USD per task completion |
| Steps to resolution | How many tool calls per task |
| Context expansion rate | How fast the context window fills |
| Tool hallucination rate | % of invalid tool calls |
| Retry waste | Tokens spent on retries |

## LLM Usage Report

```bash
agency admin usage
agency admin usage --since <timestamp>
```

Shows: total calls, tokens in/out, errors, cost estimates.

## Cost Mode Impact

Mission cost modes (`frugal`/`balanced`/`thorough`) directly affect token spend:

| Mode | Token Multiplier | Why |
|------|-----------------|-----|
| `frugal` | ~1x | Minimal prompt, no memory injection, no reflection |
| `balanced` | ~1.5-2x | Memory injection, fallback guidance, memory capture |
| `thorough` | ~2-3x | + reflection loop, LLM evaluation, full memory |

Choose `frugal` for simple tasks. Use `thorough` only when result quality justifies the cost.

## Semantic Cache Economics

The semantic cache (`cached_result` nodes in the knowledge graph) avoids re-running identical investigations:

- **Full hit** (similarity >= 0.92): Skips LLM entirely. Zero tokens.
- **Partial hit** (0.80-0.92): Injects cached context. Reduced tokens.
- **Miss**: Normal execution.

```bash
# Clear cache if results are stale
agency cache clear --agent <agent-name>
```

Cache TTL defaults to 24h. Configurable in mission YAML:

```yaml
cache:
  ttl: 24h
  enabled: true
```

## Investigating Cost Overruns

### 1. Check which cost sources are dominant

```bash
agency admin usage --since "$(date -d '24 hours ago' +%Y-%m-%dT%H:%M:%SZ)"
```

### 2. Check per-agent economics

Via API: `GET /api/v1/agents/<name>/economics`

Look for:
- High retry waste → tool errors or hallucinations
- High context expansion → agent accumulating too much context
- High steps to resolution → agent not making progress efficiently

### 3. Check routing suggestions

The routing optimizer may have found cheaper models:

```bash
agency infra routing suggestions
```

If a cheaper model has >= 90% success rate and >= 30% savings, consider approving:

```bash
agency infra routing approve <suggestion-id>
```

### 4. Consider cost mode downgrade

If the mission doesn't need reflection or LLM evaluation, switch from `thorough` to `balanced` or `frugal`.

## Budget Exhaustion Recovery

Budget exhaustion is a hard stop. The agent cannot continue its current task.

```bash
agency show <agent-name>   # check budget state
agency stop <agent-name>
agency start <agent-name>  # fresh task budget
```

To increase the budget, edit the agent config or mission YAML and reassign.

## Verification

- [ ] `agency show <agent-name>` shows correct budget limits
- [ ] Budget alerts fire at configured thresholds
- [ ] Budget exhaustion stops the agent (hard stop)
- [ ] `agency admin usage` shows reasonable error rates
- [ ] Economics endpoints return data after task execution

## See Also

- [Mission Management](experimental/mission-management.md) — cost modes, reflection configuration
- [Routing & Providers](routing-and-providers.md) — routing optimizer for cost reduction
- [Monitoring & Observability](monitoring-and-observability.md) — trajectory and signal monitoring
