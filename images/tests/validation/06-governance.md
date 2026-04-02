# Group 6: Governance

**Depends on:** Group 2 (agent lifecycle working).

---

## Trust Calibration

**Purpose:** Trust signals, level transitions, auto-restrictions, operator override.

### Step 1 — Create agent and check initial trust

```
agency_create(name="val-trust", preset="generalist")
agency_admin_trust(action="show", agent="val-trust")
```

**Expected:** Level = probation, Score = 0.0, Signals = 0.

### Step 2 — Record 5 positive signals

```
agency_admin_trust(action="record", agent="val-trust", signal_type="task_complete", description="completed task 1")
agency_admin_trust(action="record", agent="val-trust", signal_type="task_complete", description="completed task 2")
agency_admin_trust(action="record", agent="val-trust", signal_type="task_complete", description="completed task 3")
agency_admin_trust(action="record", agent="val-trust", signal_type="task_complete", description="completed task 4")
agency_admin_trust(action="record", agent="val-trust", signal_type="task_complete", description="completed task 5")
```

### Step 3 — Verify transition to standard

```
agency_admin_trust(action="show", agent="val-trust")
```

**Expected:** Score = 5.0, Level = standard (threshold: 5).

### Step 4 — Record a constraint violation

```
agency_admin_trust(action="record", agent="val-trust", signal_type="constraint_violation", description="test violation")
```

**Expected:** Score drops to 0.0. Level drops back to probation.

### Step 5 — Record failures to reach untrusted

```
agency_admin_trust(action="record", agent="val-trust", signal_type="task_failed", description="failure 1")
agency_admin_trust(action="record", agent="val-trust", signal_type="task_failed", description="failure 2")
```

### Step 6 — Verify untrusted with restrictions

```
agency_admin_trust(action="show", agent="val-trust")
```

**Expected:** Level = untrusted (score around -4.0). Auto-restrictions applied:
- `autonomous_mode_disabled`
- `requires_confirmation_all_actions`
- `max_concurrent_tasks_1`

### Step 7 — Operator elevates trust

```
agency_admin_trust(action="elevate", agent="val-trust", level="standard", description="operator override")
agency_admin_trust(action="show", agent="val-trust")
```

**Expected:** Effective level = standard (manual override takes precedence over computed score).

### Step 8 — Operator demotes trust

```
agency_admin_trust(action="demote", agent="val-trust", level="probation", description="testing demotion")
agency_admin_trust(action="show", agent="val-trust")
```

**Expected:** Effective level = probation.

### Step 9 — List all trust profiles

```
agency_admin_trust(action="list")
```

**Expected:** val-trust shown with current level and score.

### Step 10 — Verify YAML on disk

```bash
cat ~/.agency/agents/val-trust/trust.yaml
```

**Expected:** YAML with signals array (8 entries), elevations array (2 entries).

### Cleanup

```
agency_delete(agent="val-trust")
```

---

## Policy Exceptions

**Purpose:** Two-key exception model — request, approve, validate.

### Step 1 — Create agent

```
agency_create(name="val-exception", preset="generalist")
```

### Step 2 — Request an exception

```
agency_policy_exception(action="request", agent="val-exception", parameter="max_concurrent_tasks", requested_value="10", reason="Load test requires more concurrency")
```

**Expected:** Exception request created. Returns a `request_id`.

### Step 3 — List pending exceptions

```
agency_policy_exception(action="list")
```

**Expected:** Shows the pending exception request.

### Step 4 — Approve the exception

```
agency_policy_exception(action="approve", request_id="<request_id from step 2>")
```

**Expected:** Exception approved.

### Step 5 — Verify policy reflects exception

```
agency_policy_check(agent="val-exception")
```

**Expected:** Policy valid. Active exception noted.

### Cleanup

```
agency_delete(agent="val-exception")
```

---

## Policy Loosening Detection

**Purpose:** Lower levels can only restrict parameters, never expand them.

> **Note:** The Go policy engine validates loosening at every level of the 5-level chain (platform → org → department → team → agent).

### Step 1 — Create agent

```
agency_create(name="val-loosen", preset="generalist")
```

### Step 2 — Set org policy restriction

Edit `~/.agency/policy.yaml` to add:

```yaml
parameters:
  risk_tolerance: low
```

### Step 3 — Verify org restriction applies

```
agency_policy_show(agent="val-loosen")
```

**Expected:** `risk_tolerance: low` in effective parameters.

### Step 4 — Attempt agent-level loosening

Edit `~/.agency/agents/val-loosen/policy.yaml`:

```yaml
version: "0.1"
parameters:
  risk_tolerance: high
```

```
agency_policy_validate(agent="val-loosen")
```

**Expected:** Validation fails — parameter 'risk_tolerance' loosened.

### Step 5 — Restriction is allowed

Edit `~/.agency/agents/val-loosen/policy.yaml`:

```yaml
version: "0.1"
parameters:
  max_concurrent_tasks: 2
```

```
agency_policy_validate(agent="val-loosen")
```

**Expected:** Valid — reducing max_concurrent_tasks is a restriction.

### Cleanup

Remove `parameters:` section from `~/.agency/policy.yaml`. Delete agent:

```
agency_delete(agent="val-loosen")
```

---

## Department Policy Chain

**Purpose:** Department policy appears between org and team in the 5-level chain and can only restrict.

### Step 1 — Create department with policy

```bash
mkdir -p ~/.agency/departments/val-eng
cat > ~/.agency/departments/val-eng/policy.yaml << 'EOF'
version: "0.1"
parameters:
  max_concurrent_tasks: 3
EOF
```

### Step 2 — Create agent assigned to department

```
agency_create(name="val-dept-agent", preset="generalist")
```

Manually edit `~/.agency/agents/val-dept-agent/agent.yaml` to add a `policy` section:

```yaml
policy:
    inherits_from: "departments/val-eng"
```

Note: `inherits_from` goes in `agent.yaml` (under `policy:`), NOT in `policy.yaml`.

### Step 3 — Verify department in chain

```
agency_policy_show(agent="val-dept-agent")
```

**Expected:** Policy chain shows 5 levels: platform (ok) → org (ok) → department (ok, file: departments/val-eng/policy.yaml) → team (missing) → agent (ok). `max_concurrent_tasks` is 3 (department restriction applied).

### Cleanup

```
agency_delete(agent="val-dept-agent")
```

```bash
rm -rf ~/.agency/departments/val-eng
```

---

## Teams

**Purpose:** Team creation, membership, roles, and activity tracking.

### Step 1 — Create agents

```
agency_create(name="val-lead", preset="coordinator", agent_type="coordinator")
agency_create(name="val-dev", preset="engineer")
agency_create(name="val-reviewer", preset="code-reviewer")
```

### Step 2 — Create team

```
agency_team_create(name="val-team", coordinator="val-lead", members=["val-dev", "val-reviewer"])
```

**Expected:** Team created with coordinator and 2 members.

### Step 3 — List teams

```
agency_team_list()
```

**Expected:** val-team listed.

### Step 4 — Show team details

```
agency_team_show(name="val-team")
```

**Expected:** Coordinator (val-lead), members with roles, status.

### Step 5 — Team activity

```
agency_team_activity(name="val-team")
```

**Expected:** Activity register (may be empty if agents not started).

### Cleanup

```
agency_delete(agent="val-lead")
agency_delete(agent="val-dev")
agency_delete(agent="val-reviewer")
```

---

## Function Agent Authority

**Purpose:** Cross-boundary workspace visibility and halt authority.

### Step 1 — Create agents and team

```
agency_create(name="val-worker", preset="engineer")
agency_create(name="val-security", preset="function", agent_type="function")
agency_team_create(name="val-authority-team", coordinator="val-worker", members=["val-security"])
```

### Step 2 — Start both

```
agency_start(agent="val-worker")
agency_start(agent="val-security")
```

### Step 3 — Worker creates a file

```
agency_brief(agent="val-worker", task="Write a Python file at /workspace/app.py with a simple Flask app.")
```

Wait for completion.

### Step 4 — Function agent reads cross-boundary (visibility)

```
agency_brief(agent="val-security", task="Use list_directory to list /visibility/val-worker/ and then use read_file to read /visibility/val-worker/app.py.")
```

**Expected:** Function agent can read the worker's workspace through cross-boundary mount.

### Step 5 — Write to visibility mount fails

```
agency_brief(agent="val-security", task="Try to write_file at /visibility/val-worker/test.txt with content 'test'. Report success or failure.")
```

**Expected:** Write fails — visibility mount is read-only.

### Step 6 — Function agent halts worker

```
agency_brief(agent="val-security", task="Use the halt_agent tool to halt val-worker with reason 'security review required'.")
```

**Expected:** Worker halted successfully.

### Step 7 — Verify worker stopped

```
agency_show(agent="val-worker")
```

**Expected:** Worker is stopped/halted.

### Cleanup

```
agency_stop(agent="val-security", halt_type="immediate", reason="cleanup")
agency_delete(agent="val-worker")
agency_delete(agent="val-security")
```

---

## Capability Scoping

**Purpose:** Capabilities can be enabled/disabled per-agent.

### Step 1 — Create agent

```
agency_create(name="val-capscope", preset="generalist")
```

### Step 2 — Enable capability for specific agent

```
agency_cap_enable(name="brave-search", agents=["val-capscope"])
```

**Expected:** brave-search enabled for val-capscope only.

### Step 3 — Show agent capabilities

```
agency_cap_show(name="val-capscope")
```

**Expected:** brave-search listed.

### Step 4 — Disable capability

```
agency_cap_disable(name="brave-search")
agency_cap_show(name="val-capscope")
```

**Expected:** brave-search no longer available.

### Cleanup

```
agency_delete(agent="val-capscope")
```
