# Group 7: Deploy & Integration

**Depends on:** Group 1 (infrastructure running).

---

## Pack Deploy

**Purpose:** Declarative team deployment and teardown via pack YAML.

### Step 1 — Create a pack file

Create `/tmp/val-pack.yaml`:

```yaml
kind: pack
name: val-test-pack
version: "1.0.0"
team:
  name: val-deploy-team
  agents:
    - name: val-frontend
      preset: engineer
      role: coordinator
    - name: val-backend
      preset: engineer
      role: standard
  channels:
    - name: val-dev-chat
      topic: "Development discussion"
```

### Step 2 — Dry run

```
agency_deploy(pack_file="/tmp/val-pack.yaml", dry_run=true)
```

**Expected:** Validation passes. Shows what would be created (agents, team, channel).

### Step 3 — Full deploy

```
agency_deploy(pack_file="/tmp/val-pack.yaml")
```

**Expected:** Agents created, team created, channel created, agents started.

### Step 4 — Verify everything created

```
agency_list()
agency_team_list()
agency_channel_list()
```

**Expected:**
- val-frontend and val-backend in agent list
- val-deploy-team in team list
- val-dev-chat in channel list

### Step 5 — Teardown with delete

```
agency_teardown(pack_name="val-test-pack", delete=true)
```

**Expected:** All agents stopped, removed, deleted. Team and channel cleaned up.

### Step 6 — Verify cleanup

```
agency_list()
agency_team_list()
```

**Expected:** No val-* objects remain.

### Cleanup

```bash
rm /tmp/val-pack.yaml
```

---

## Connectors

**Purpose:** Connector activation, deactivation, and status.

### Step 1 — List connectors

```
agency_connector_list()
```

**Expected:** Shows installed connectors (may be empty).

### Step 2 — Create a test connector

Create `~/.agency/connectors/val-webhook.yaml`:

```yaml
kind: connector
name: val-webhook
version: "1.0.0"
source:
  type: webhook
routes:
  - match:
      type: test
    target:
      agent: val-connector-worker
    brief: "Process webhook: {{ payload }}"
```

And create the target agent:

```
agency_create(name="val-connector-worker", preset="generalist")
```

### Step 3 — Activate connector

```
agency_connector_activate(name="val-webhook")
```

**Expected:** Connector activated.

### Step 4 — Check status

```
agency_connector_status(name="val-webhook")
```

**Expected:** Shows active status, event counts.

### Step 5 — List shows active

```
agency_connector_list()
```

**Expected:** val-webhook shown as active.

### Step 6 — Deactivate

```
agency_connector_deactivate(name="val-webhook")
agency_connector_list()
```

**Expected:** val-webhook shown as inactive.

### Cleanup

```
agency_delete(agent="val-connector-worker")
```

```bash
rm ~/.agency/connectors/val-webhook.yaml
```

---

## Intake

**Purpose:** Work item listing and statistics.

### Step 1 — Intake stats

```
agency_intake_stats()
```

**Expected:** Returns statistics (counts by status and connector).

### Step 2 — List work items

```
agency_intake_items(limit=10)
```

**Expected:** Work items list (may be empty if no webhook events received).

### Step 3 — Filter by status

```
agency_intake_items(status="pending", limit=5)
```

**Expected:** Filtered results (may be empty).

---

## Hub Operations

**Purpose:** Hub sync, search, list, and component info.

### Step 1 — Update hub sources

```
agency_hub_update()
```

**Expected:** Hub sources synced (or "no sources configured").

### Step 2 — Search for components

```
agency_hub_search(query="engineer")
```

**Expected:** Matching components or "no components found."

### Step 3 — List installed

```
agency_hub_list()
```

**Expected:** Installed components with provenance (may be empty).

### Step 4 — Component info

If components were found in step 2:

```
agency_hub_info(component="<component-name>")
```

**Expected:** Metadata, source, install status.

### Step 5 — Install (requires agency-hub source)

If agency-hub is configured as a source:

```
agency_hub_install(component="<component-name>")
agency_hub_list()
agency_hub_info(component="<component-name>")
```

**Expected:** Component installed. Appears in list with provenance.

### Step 6 — Remove

```
agency_hub_remove(component="<component-name>")
agency_hub_list()
```

**Expected:** Component no longer installed.

> **Note:** Steps 5–6 require agency-hub configured as a git source. Skip if not available.
