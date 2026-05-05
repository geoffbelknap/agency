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
./scripts/dev/dev-agent-loop-eval.sh --mode replay
```

Run one fixture:

```bash
./scripts/dev/dev-agent-loop-eval.sh --mode replay --fixture current_info_terminates_after_retry
```

Live mode creates and deletes disposable agents against the configured local
Agency home and infra:

```bash
./scripts/dev/dev-agent-loop-eval.sh --mode live --fixture current_info_terminates_after_retry
```

For artifact-based microagent validation on macOS, run the live gate helper
from a normal terminal so Apple Virtualization work does not run inside the
Codex sandbox:

```bash
scripts/dev/agent-loop-live-gate.sh --version 0.3.19-dev-7a7fa33
```

The helper creates a disposable Agency home, starts host services on isolated
ports, copies local routing and credential-swap config into the disposable
home, extracts the darwin/arm64 host enforcer from the published OCI artifact,
and then runs one live fixture through this harness.

Use `--keep-agent` to preserve the disposable agent for debugging.

The terminal report is intentionally evidence-first: it prints the task,
diagnosis, observed response or explicit no-response status, and check
breakdown before the numeric score. JSON result files include the same summary
fields plus the raw trace under `test-results/agent-loop/`.

## Fixture Intent

Fixtures are named by behavior, not by historical agent names. If a fixture
covers a known live incident, record that under `provenance`.

The runner currently scores:

- live progress phase diagnosis, including no-delivery, skipped-task,
  no-observable-loop, and loop-started-no-terminal cases
- expected PACT contract and verdict
- required audit events
- required evidence strings
- required strings in the visible agent response
- forbidden text and forbidden verdict reasons
- answer quality checks: concise, direct, no internal machinery, no fake tool
  transcripts, and no unsupported tool-use claims
- max agent message count
- max turn count when the trace exposes turn data

Core replay/live fixture IDs for the DM operating-model lane:

- `basic_dm_alive`
- `status_what_are_you_working_on`
- `plain_current_date`
- `current_info_with_source`
- `mission_agent_casual_dm`
- `tool_honesty_no_fake_transcript`

Replay fixtures may set `expect.expected_failure: true` with an expected
`diagnosis` to validate that the harness classifies a known-bad trace. Those
fixtures print as `XFAIL` in replay mode. Live runs do not treat expected
failures as acceptable outcomes.

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
