# DM Quality Live Gate - 2026-05-04

Developer validation for the ASK direct-chat operating-model milestone.

## Scope

- Validate that a casual direct message gets one concise coworker-style reply.
- Validate published runtime OCI artifacts, not only the local checkout.
- Run Apple Virtualization and microagent work outside the Codex sandbox.

## Build Under Test

- Source commit: `7a7fa33321d90b42db0edf2d8220629d0aeb3729`
- Runtime artifact workflow: `Release Runtime OCI Artifacts`
- Workflow run: <https://github.com/geoffbelknap/agency/actions/runs/25356278893>

## Runtime Artifacts

- Body: `ghcr.io/geoffbelknap/agency-runtime-body:v0.3.19-dev-7a7fa33`
- Body digest: `sha256:e66043c27d09da2e8656da5d7f5223b72c09cb01c3c6bd6d027647ddf66008a9`
- Enforcer: `ghcr.io/geoffbelknap/agency-runtime-enforcer:v0.3.19-dev-7a7fa33`
- Enforcer digest: `sha256:4cec7329541401f986411d3ef56eb51364c0dba8e85547f7c9ebaf25c4602995`
- Enforcer darwin/arm64 manifest: `sha256:9ab760e3348bef7b6954f3b47198b808f60c54c1e762afe2a361d155152a9f19`

## Live Gate

Command, run from a normal macOS terminal:

```bash
/private/tmp/agency-live-gate-7a7fa33.sh
```

Equivalent repo helper:

```bash
scripts/dev/agent-loop-live-gate.sh --version 0.3.19-dev-7a7fa33
```

Additional direct-message fixtures can be run as a small suite:

```bash
scripts/dev/agent-loop-live-gate.sh \
  --version 0.3.19-dev-7a7fa33 \
  --fixture basic_dm_alive \
  --fixture status_what_are_you_working_on \
  --fixture plain_current_date \
  --fixture mission_agent_casual_dm \
  --fixture tool_honesty_no_fake_transcript
```

Result:

```text
PASS basic_dm_alive
Task: you alive?
Diagnosis: agent_response_observed - agent-authored message was observed
Response: Yes, I'm here and ready to work.
Score: 100
```

Key checks passed:

- `contract`: `chat`
- `route`: `trivial_direct`
- `verdict`: `completed`
- `response_text`: required response text present
- `concise_answer`: 7 words
- `no_internal_machinery`: no internal terms
- `no_fake_tool_transcript`: no fake tool transcript
- `no_unsupported_tool_claim`: no tool-use claim
- `message_bound`: 1 agent message
- `response_received`: observed before timeout

## Local Validation

```bash
go test ./internal/config ./internal/orchestrate
./scripts/dev/dev-agent-loop-eval.sh --mode replay
python3 -m py_compile tools/dev/agent-loop-eval/runner.py
```

## Notes

- The live helper uses isolated host-service ports so it can coexist with a
  normal local Agency daemon.
- The helper copies `routing.yaml`, `credential-swaps.yaml`, and encrypted
  credential-store files into a disposable Agency home. It does not put
  provider secrets on the command line.
- The live gate is a developer/release-candidate check, not normal PR CI.
