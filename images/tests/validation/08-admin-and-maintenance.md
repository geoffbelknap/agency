# Group 8: Admin & Maintenance

**Depends on:** Group 4 (communication exercises, for knowledge content).

---

## Knowledge Curation

**Purpose:** Knowledge graph admin — health, flagging, curation log.

**Prerequisite:** Run Group 4 (Knowledge Graph exercise) first to populate the graph.

### Step 1 — Health check

```
agency_admin_knowledge(action="health")
```

**Expected:** Curation health metrics OR "No curation metrics available yet."

### Step 2 — Check flagged nodes

```
agency_admin_knowledge(action="flags")
```

**Expected:** Flagged node list OR "No flagged nodes."

### Step 3 — Curation log

```
agency_admin_knowledge(action="log")
```

**Expected:** Curation history OR "No curation log entries."

### Step 4 — Unflag a node (if any flagged)

If step 2 returned flagged nodes:

```
agency_admin_knowledge(action="unflag", node_id="<node_id from step 2>")
```

**Expected:** Node unflagged.

### Step 5 — Verify log updated

```
agency_admin_knowledge(action="log")
```

**Expected:** Log now includes the unflag entry.

---

## Departments

**Purpose:** Department creation and policy scoping.

### Step 1 — Create a department

```
agency_admin_department(action="create", name="val-engineering", risk_tolerance="medium", max_concurrent_tasks=5)
```

**Expected:** Department created with policy.

### Step 2 — List departments

```
agency_admin_department(action="list")
```

**Expected:** val-engineering listed.

### Step 3 — Show department

```
agency_admin_department(action="show", name="val-engineering")
```

**Expected:** Shows department policy details.

### Step 4 — Verify policy hierarchy

Create an agent and check that department policy is in the chain:

```
agency_create(name="val-dept-agent", preset="generalist")
agency_policy_check(agent="val-dept-agent")
```

**Expected:** Policy chain includes department level (if agent is assigned to department).

### Cleanup

```
agency_delete(agent="val-dept-agent")
```

---

## Audit Export & Retention

**Purpose:** Audit log export and retention policy.

### Step 1 — Audit stats

```
agency_admin_audit(action="stats")
```

**Expected:** Returns agent count, total files, total size, oldest entry.

### Step 2 — Export logs

```
agency_admin_audit(action="export", format="jsonl")
```

**Expected:** Logs exported to a file. Path returned.

### Step 3 — Export with time filter

```
agency_admin_audit(action="export", format="json", since="2026-03-01T00:00:00Z")
```

**Expected:** Filtered export. Path returned.

### Step 4 — Apply retention policy

```
agency_admin_audit(action="retention")
```

**Expected:** Retention policy applied. Reports archived/deleted counts.
