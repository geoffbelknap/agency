# Group 1: Platform Bootstrap

**Depends on:** Nothing — run this first.

---

## Init & Infrastructure

**Purpose:** Verify platform initialization, image builds, and infrastructure lifecycle.

### Step 1 — Initialize Agency

```
agency_init()
```
CLI: `agency init --provider anthropic`

**Expected:**
- `~/.agency/` directory created with subdirectories: agents, audit, connectors, departments, hub, infrastructure, knowledge, services, teams
- `config.yaml` written with LLM provider, HMAC key, hub sources, auth token
- Gateway daemon started automatically

> **Note:** The Go gateway `init` does not create `.env`, `presets/`, `org.yaml`, `policy.yaml`, `principals.yaml`, or `host.yaml` — these were Python-era artifacts. The `--force` and `--operator` flags are also removed.

### Step 2 — Verify init files

```bash
ls ~/.agency/config.yaml
ls ~/.agency/agents/
```

**Expected:** `config.yaml` exists. `agents/` directory exists (empty).

### Step 3 — Infrastructure status (before start)

```
agency_infra_status()
```
CLI: `agency infra status`

**Expected:** All containers missing or stopped.

### Step 4 — Start infrastructure

```
agency_infra_up()
```
CLI: `agency infra up`

**Expected:** Images built (first run) or exist. Containers started:
- agency-infra-egress — running
- agency-infra-analysis — running
- agency-infra-comms — running
- agency-infra-knowledge — running
- agency-infra-intake — running

### Step 5 — Verify health

```
agency_infra_status()
```
CLI: `agency infra status`

**Expected:** All 5 infrastructure containers healthy.

### Step 6 — Rebuild single component

```
agency_infra_rebuild(component="comms")
```
CLI: `agency infra rebuild comms`

**Expected:** Comms image rebuilt, container restarted, becomes healthy.
Other components unaffected.

### Step 7 — Down/up cycle

```
agency_infra_down()
agency_infra_status()
agency_infra_up()
agency_infra_status()
```
CLI: `agency infra down && agency infra status && agency infra up && agency infra status`

**Expected:** All containers stop, then start again. All healthy after up.

### Step 8 — Hot reload

```
agency_infra_reload()
```
CLI: `agency infra reload`

**Expected:** SIGHUP sent to all enforcers. No container restarts.

**Cleanup:** Leave infrastructure running for remaining groups.

**PASS criteria:** All 7 steps succeed. Infrastructure is running and healthy.

---

## Doctor & Status

**Purpose:** Verify security guarantee checks and system status reporting.

### Step 1 — Create and start a test agent

```
agency_create(name="val-doctor", preset="generalist")
agency_start(agent="val-doctor")
```
CLI: `agency create val-doctor --preset generalist && agency start val-doctor`

**Expected:** Agent created and started. All 7 phases pass.

### Step 2 — Run doctor

```
agency_admin_doctor()
```
CLI: `agency admin doctor`

**Expected:** All seven security guarantees verified:
1. Credential isolation — no LLM API keys in workspace
2. Network mediation — workspace on internal network only
3. Constraints read-only — constraints.yaml mounted read-only
4. Enforcer audit — enforcer running and healthy
5. Audit not writable — audit directory not writable by agent
6. Halt functional — workspace container running and pauseable
7. Operator override — enforcer reachable on mediation network

Each should show `[PASS]`.

### Step 3 — System status

```
agency_status()
```
CLI: `agency status`

**Expected:** Shows infrastructure health, running agents, overall system state.

### Cleanup

```
agency_stop(agent="val-doctor", halt_type="immediate", reason="validation cleanup")
agency_delete(agent="val-doctor")
```

**PASS criteria:** Doctor shows all 7 guarantees PASS. Status shows healthy infrastructure.
