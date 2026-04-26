# Agent Loop Eval Dev Tool

This is a developer-only harness for checking whether Agency's runtime harness
is improving actual agent-loop behavior. It is intentionally not wired into
release gates, CI requirements, user docs, or shipped product flows.

The harness does not depend on pre-existing agents. In live mode it creates a
fresh disposable agent for each fixture. In replay mode it scores fixture-owned
trace data without starting Agency.

## Run

Replay all fixtures:

```bash
./scripts/dev-agent-loop-eval.sh --mode replay
```

Run one fixture:

```bash
./scripts/dev-agent-loop-eval.sh --mode replay --fixture current_info_terminates_after_retry
```

Live mode creates and deletes disposable agents against the configured local
Agency home and infra:

```bash
./scripts/dev-agent-loop-eval.sh --mode live --fixture current_info_terminates_after_retry
```

Use `--keep-agent` to preserve the disposable agent for debugging.

## Fixture Intent

Fixtures are named by behavior, not by historical agent names. If a fixture
covers a known live incident, record that under `provenance`.

The runner currently scores:

- live progress phase diagnosis, including no-delivery, skipped-task,
  no-observable-loop, and loop-started-no-terminal cases
- expected PACT contract and verdict
- required audit events
- required evidence strings
- forbidden text and forbidden verdict reasons
- max agent message count
- max turn count when the trace exposes turn data

Useful live diagnoses include:

- `task_not_delivered`: no operator message or current task was observed
- `task_delivered_no_observable_loop`: the runtime accepted the task, but no
  LLM/PACT/reply activity was visible
- `loop_started_no_terminal`: LLM activity started but no terminal outcome was
  observed before timeout
- `loop_error`: the body emitted an explicit loop error, usually with provider
  or mediation details
- `terminal_outcome_observed`: PACT or task completion produced a terminal
  outcome

Replay fixtures should be small and deterministic. Live fixtures should be few,
expensive only when worth it, and run manually or as a separate nightly/dev
benchmark.
