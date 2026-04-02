# E2E Integration Tests

Automated end-to-end tests that validate Agency platform features using real
Docker containers and infrastructure. These complement the ~1,700 unit tests
(which mock Docker) and the manual validation runbooks in `tests/validation/`.

## Prerequisites

- Docker running and accessible
- Agency installed (`./install.sh` or `cd agency-gateway && make install`)
- At least 4 GB RAM available (shared infra + test agents)

## Running Tests

```bash
# All E2E tests
pytest tests/e2e/ -m e2e -v

# Single group
pytest tests/e2e/test_e2e_bootstrap.py -m e2e -v
pytest tests/e2e/test_e2e_security.py -m e2e -v

# With output
pytest tests/e2e/ -m e2e -v -s
```

E2E tests are excluded from `pytest tests/` by default (via `addopts`).
You must explicitly pass `-m e2e` to run them.

## Test Groups

| File | Focus | When to Run |
|------|-------|-------------|
| `test_e2e_bootstrap.py` | Init, infra up/down/rebuild, doctor, status | Infra or core changes |
| `test_e2e_lifecycle.py` | Create, start, brief, stop, restart, delete | Agent lifecycle changes |
| `test_e2e_capabilities.py` | Registry, memory, skills, extra mounts | Capability or preset changes |
| `test_e2e_comms.py` | Channels, messaging, search, knowledge graph | Comms or knowledge changes |
| `test_e2e_security.py` | Network isolation, XPIA, egress, creds, budget, audit, hardening | **Never skip** |
| `test_e2e_governance.py` | Trust, policy exceptions, teams, function agents, halt authority | Governance changes |
| `test_e2e_deploy.py` | Packs, connectors, intake, hub | Deploy or integration changes |

## Architecture

- **Session-scoped fixtures**: Platform init and infrastructure startup happen once
  per session — not per test. This keeps the suite fast (~5-10 min total).
- **Automatic cleanup**: The `create_test_agent` / `started_agent` fixtures
  stop and delete agents after each test.
- **Docker exec pattern**: HTTP calls to comms/knowledge/analysis services go
  through `docker exec curl` to avoid WSL2 port-forwarding issues.
- **No real LLM calls**: Tests use dummy API keys. Budget and enforcer tests
  validate the pipeline without making actual model requests.

## Mapping to Manual Validation

These automated tests cover the same ground as `tests/validation/` exercises:

| Manual Exercise | Automated Test Class |
|----------------|---------------------|
| Platform Setup | `TestPlatformInit`, `TestInfrastructureLifecycle` |
| Doctor & Status | `TestDoctorSecurityGuarantees`, `TestSystemStatus` |
| Create & Configure | `TestAgentCreation` |
| Seven-Phase Start | `TestSevenPhaseStart` |
| Stop, Restart, Delete | `TestStopRestartResume`, `TestAgentDeletion` |
| Capability Registry | `TestCapabilityRegistry` |
| Persistent Memory | `TestPersistentMemory` |
| Skills & Presets | `TestSkillsAndPresets` |
| Extra Mounts | `TestExtraMounts` |
| Channels & Messaging | `TestChannelOperations`, `TestMessaging` |
| Knowledge Graph | `TestKnowledgeGraph` |
| Network Isolation | `TestNetworkIsolation` |
| XPIA Scanning | `TestXPIAScanning` |
| Egress Domain Control | `TestEgressDomainControl` |
| Credential Scoping | `TestCredentialScoping` |
| Budget Controls | `TestBudgetControls` |
| Policy Hard Floors | `TestPolicyHardFloors` |
| Audit & Limits | `TestAuditIntegrity` |
| Container Hardening | `TestContainerHardening` |
| Trust Calibration | `TestTrustCalibration` |
| Policy Exceptions | `TestPolicyExceptions` |
| Teams | `TestTeams` |
| Function Agent Authority | `TestFunctionAgentAuthority` |
| Pack Deploy | `TestPackDeploy` |
| Connectors | `TestConnectors` |
| Intake | `TestIntake` |
| Hub | `TestHub` |
