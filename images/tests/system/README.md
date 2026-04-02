# System Tests

Full end-to-end system tests that exercise the Agency platform through its MCP
handler interface — the same code path used when Claude Code or any MCP client
operates the platform. Real Docker containers, real infrastructure, real flows.

## Prerequisites

- Docker running and accessible
- Agency installed (`./install.sh` or `cd agency-gateway && make install`)
- At least one API key in env (or dummy — tests don't make real LLM calls)

## Running

```bash
# All groups (full suite)
python -m tests.system.run

# Shortcuts
python -m tests.system.run smoke     # bootstrap only (~60s)
python -m tests.system.run core      # bootstrap + lifecycle + security (~5min)
python -m tests.system.run full      # everything (~10-15min)

# Individual groups
python -m tests.system.run security
python -m tests.system.run comms governance

# Single file standalone
python -m tests.system.test_security

# List available groups
python -m tests.system.run --list
```

## Test Groups

| Group | Tests | Focus | When to Run |
|-------|-------|-------|-------------|
| `bootstrap` | 11 | Init, infra up/down/rebuild, doctor, status | Infra or core changes |
| `lifecycle` | 19 | Create, start, brief, stop, restart, delete (all 3 agent types) | Agent lifecycle changes |
| `capabilities` | 10 | Cap registry, service grants, presets, memory persistence | Capability or preset changes |
| `comms` | 14 | Channels, messaging, search, knowledge graph, admin knowledge | Comms or knowledge changes |
| `security` | 27 | Network isolation, XPIA, egress, creds, budget, policy, audit, hardening | **Never skip** |
| `governance` | 19 | Trust signals/levels/elevation, policy exceptions, teams, halt authority | Governance changes |
| `deploy` | 14 | Pack deploy/teardown, connectors, intake, hub search/list | Deploy or integration changes |

## How This Differs From Other Test Layers

| Layer | Location | Interface | Docker? | Speed |
|-------|----------|-----------|---------|-------|
| Unit tests | `tests/test_*.py` | Python functions, mocked Docker | No | Fast (~30s) |
| E2E integration | `tests/e2e/` | Python functions, real Docker | Yes | Medium (~5min) |
| **System tests** | **`tests/system/`** | **MCP handlers (full stack)** | **Yes** | **~10-15min** |
| Manual validation | `tests/validation/` | MCP tools / CLI (human) | Yes | ~30-45min |

System tests call the MCP handlers directly — the same `_HANDLERS` dict that
the MCP server dispatches to. This means:

- Same code path as real operation (no shortcuts)
- Full module reloading on each call
- ASK violation enforcement active
- Audit logging happens
- No subprocess overhead

## Framework

Tests use a lightweight framework in [framework.py](framework.py):

```python
from tests.system.framework import SystemTest, run_group

class MyTests(SystemTest):
    group_name = "My Feature"

    def setup(self):
        self.mcp("agency_infra_up")

    def test_something(self):
        result = self.mcp("agency_create", name="test", preset="minimal")
        self.assert_ok(result)
        self.assert_contains(result, "test")

    def test_security_boundary(self):
        result = self.mcp("agency_stop", agent="test", halt_type="emergency")
        self.assert_ask_violation(result, tenet=2)

    def teardown(self):
        self.mcp("agency_delete", agent="test")
```

### Available Helpers

| Method | Purpose |
|--------|---------|
| `self.mcp(tool, **kwargs)` | Call MCP handler, returns `{"ok": bool, "text": str}` |
| `self.exec_in(container, cmd)` | Run command inside Docker container |
| `self.http_in(container, url)` | HTTP request inside container, returns JSON |
| `self.assert_ok(result)` | Assert MCP call succeeded |
| `self.assert_error(result)` | Assert MCP call failed |
| `self.assert_contains(result, s)` | Assert result text contains substring |
| `self.assert_ask_violation(result)` | Assert ASK tenet violation |
| `self.assert_true/equal/in(...)` | Standard assertions |

## Output

```
══════════════════════════════════════════════════════════════════════
  Security & Enforcement
══════════════════════════════════════════════════════════════════════
  [PASS] workspace cannot reach internet (1203ms)
  [PASS] workspace isolated from host network (45ms)
  [PASS] xpia detects injection (234ms)
  [PASS] xpia passes clean content (189ms)
  [FAIL] budget blocks when exceeded (56ms)
         Budget should be exceeded: {'allowed': True}
  ...

  24/27 passed, 3 failed

══════════════════════════════════════════════════════════════════════
  SUMMARY: 108/114 passed, 6 FAILED
    ✓ Platform Bootstrap: 11/11
    ✓ Agent Lifecycle: 19/19
    ✗ Security & Enforcement: 24/27
    ...
══════════════════════════════════════════════════════════════════════
```

Exit code is 0 if all tests pass, 1 if any fail.
