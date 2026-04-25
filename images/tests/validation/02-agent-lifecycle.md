# Group 2: Agent Lifecycle

**Depends on:** Group 1 (infrastructure running).

---

## Create & Configure

**Purpose:** Agent creation, preset application, config validation, and input rejection.

### Step 1 — Create a generalist agent

```
agency_create(name="val-gen", preset="generalist")
```

**Expected:** Agent created. Files exist:
- `~/.agency/agents/val-gen/agent.yaml`
- `~/.agency/agents/val-gen/constraints.yaml`
- `~/.agency/agents/val-gen/identity.md`

### Step 2 — Show agent details

```
agency_show(agent="val-gen")
```

**Expected:** Shows agent name, type (standard), preset (generalist), mode, status (stopped).

### Step 3 — Create an engineer agent

```
agency_create(name="val-eng", preset="engineer")
```

**Expected:** Different identity from generalist. Engineer identity references coding, debugging, or development.

### Step 4 — List agents

```
agency_list()
```

**Expected:** Both val-gen and val-eng listed with status.

### Step 5 — Reject invalid names

```
agency_create(name="../../etc")
agency_create(name="enforcer")
agency_create(name="a")
```

**Expected:** All three fail:
- Path traversal rejected
- Reserved name rejected
- Name too short rejected

### Step 6 — Reject unknown fields in config

Manually add `unknown_field: "injection"` to `~/.agency/agents/val-gen/agent.yaml`, then:

```
agency_show(agent="val-gen")
```

**Expected:** Validation warning about unknown field. Remove the added field after testing.

### Cleanup

```
agency_delete(agent="val-gen")
agency_delete(agent="val-eng")
```

---

## Seven-Phase Start

**Purpose:** Full start sequence, container creation, and hardening verification.

### Step 1 — Start an agent

```
agency_create(name="val-start", preset="generalist", mode="assisted")
agency_start(agent="val-start")
```

**Expected:** 7-phase sequence completes:
1. File validation
2. Infrastructure health
3. Constraints mounted
4. Workspace check
5. Identity delivery
6. Body + enforcer containers created
7. Session started — returns unique `session_id`

### Step 2 — Verify containers

```bash
docker ps --filter name=val-start
```

**Expected:** Two containers running:
- `agency-val-start-workspace`
- `agency-val-start-enforcer`

### Step 3 — Verify container hardening

```bash
docker inspect agency-val-start-workspace --format '{{.HostConfig.ReadonlyRootfs}}'
docker inspect agency-val-start-workspace --format '{{.HostConfig.CapDrop}}'
docker inspect agency-val-start-workspace --format '{{.HostConfig.SecurityOpt}}'
```

**Expected:**
- ReadonlyRootfs: `true`
- CapDrop: `[ALL]`
- SecurityOpt: contains `no-new-privileges:true`

### Step 4 — Verify credential isolation

```bash
docker exec agency-val-start-workspace printenv AGENCY_LLM_API_KEY
```

**Expected:** Shows `agency-scoped-<random>` — NOT the real API key.

### Step 5 — Verify constraints are read-only

```bash
docker exec agency-val-start-workspace sh -c 'echo test >> /agency/constraints.yaml' 2>&1
```

**Expected:** Read-only file system error.

### Step 6 — Start nonexistent agent fails

```
agency_start(agent="does-not-exist-xyz")
```

**Expected:** Error — agent not found.

### Cleanup

```
agency_stop(agent="val-start", halt_type="immediate", reason="validation cleanup")
agency_delete(agent="val-start")
```

---

## Brief & Task Delivery

**Purpose:** Task delivery, mode override, and audit log inspection.

### Step 1 — Create and start

```
agency_create(name="val-brief", preset="generalist", mode="assisted")
agency_start(agent="val-brief")
```

### Step 2 — Brief the agent

```
agency_brief(agent="val-brief", task="List the files in /workspace and report what you see.")
```

**Expected:** Task delivered. Agent responds with file listing.

### Step 3 — Check audit log

```
agency_log(agent="val-brief", verbose=true, tail=10)
```

**Expected:** Shows task_delivered event, LLM calls, tool usage.

### Step 4 — Brief with mode override

```
agency_brief(agent="val-brief", task="Write 'hello from agency' to /workspace/hello.txt", mode="autonomous")
```

**Expected:** Task delivered with autonomous mode override.

### Step 5 — Verify file was created

```
agency_brief(agent="val-brief", task="Read /workspace/hello.txt and report its contents.")
```

**Expected:** Agent reports "hello from agency".

### Cleanup

```
agency_stop(agent="val-brief", halt_type="immediate", reason="validation cleanup")
agency_delete(agent="val-brief")
```

---

## Stop, Restart, Delete

**Purpose:** Graceful halt, restart with data persistence, and clean deletion.

### Step 1 — Create, start, and write data

```
agency_create(name="val-lifecycle", preset="generalist")
agency_start(agent="val-lifecycle")
agency_brief(agent="val-lifecycle", task="Write 'persistence test' to /workspace/persist.txt")
```

Wait for the agent to complete the task.

### Step 2 — Stop the agent

```
agency_stop(agent="val-lifecycle", reason="testing stop")
```

**Expected:** Agent stops. Halt event logged.

### Step 3 — Verify halt in audit log

```
agency_log(agent="val-lifecycle", verbose=true, tail=5)
```

**Expected:** Halt event with reason "testing stop".

### Step 4 — Restart the agent

```
agency_restart(agent="val-lifecycle")
```

**Expected:** Agent restarts with a new session_id.

### Step 5 — Verify data persisted

```
agency_brief(agent="val-lifecycle", task="Read /workspace/persist.txt and report its contents.")
```

**Expected:** Agent reports "persistence test" — workspace data survived restart.

### Step 6 — Emergency halt without reason (should fail)

```
agency_stop(agent="val-lifecycle", halt_type="emergency")
```

**Expected:** Blocked — ASK Tenet 2 violation. Emergency halt requires a reason.

### Step 7 — Emergency halt with reason

```
agency_stop(agent="val-lifecycle", halt_type="emergency", reason="validation test")
```

**Expected:** Agent stops.

### Step 8 — Delete and verify

```
agency_delete(agent="val-lifecycle")
agency_list()
```

**Expected:** Agent deleted. No longer in list. Audit logs preserved (not destroyed).

### Step 9 — Delete nonexistent fails

```
agency_delete(agent="val-lifecycle")
```

**Expected:** Error — agent not found.
