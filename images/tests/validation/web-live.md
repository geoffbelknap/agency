# Web Live Validation

Use this lane for web UI changes that need a browser or a real local Agency
stack.

## Mocked Browser Lane

```bash
make web-test-all
```

This covers deterministic route rendering, component behavior, and mocked API
contracts.

## Live Safe Lane

```bash
./scripts/e2e-live-disposable.sh --skip-build
```

Expected:

- Runs against a cloned disposable Agency home.
- Uses isolated host ports and infra namespace.
- Cleans up after itself.
- Does not execute destructive or external side effects.

## Live Risky Lane

```bash
./scripts/e2e-live-disposable.sh --skip-build --risky
```

Use for connector activation, hub install/remove, notification test-send,
agent lifecycle mutation, mission mutation, and similar operator flows.

## Live Danger Lane

```bash
./scripts/e2e-live-danger-disposable.sh
```

Danger flows require explicit confirmation guards and should run only against
disposable state.

## Classification Source

See `web/tests/COVERAGE_TIERS.md` for the current inventory and risk
classification.
