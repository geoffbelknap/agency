# Smoke Validation

Use this lane for quick confidence that an installation or local build is
usable. It should avoid destructive operations and should finish quickly.

## Preconditions

- `agency --help` works.
- A supported runtime backend is available if you will start agents.
- If validating a patched build, use `./agency` consistently.

## Checks

```bash
./agency --version
agency --version
agency status
agency infra status
agency admin doctor
```

Expected:

- The intended binary is the one being exercised.
- Gateway health is reachable.
- Infra status reports the expected services.
- `agency admin doctor` has no runtime failures. Backend hygiene warnings must
  be interpreted in the selected backend context.

## Optional Agent Smoke

```bash
agency create smoke-agent --preset generalist
agency start smoke-agent
agency runtime status smoke-agent
agency runtime validate smoke-agent
agency halt smoke-agent --tier supervised --reason "smoke validation"
agency delete smoke-agent
```

Expected:

- The agent reaches a running phase.
- Runtime validate succeeds.
- Halt and delete leave no running agent runtime.
