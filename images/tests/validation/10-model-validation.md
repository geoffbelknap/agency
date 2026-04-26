# Group 10: Model & Schema Validation

**Depends on:** Group 1 (infrastructure running).

> **Note:** This group validates the Go model validation layer
> (`internal/models/`). Run the focused Go unit tests first:
> `go test ./internal/models ./internal/policy`. These operator exercises
> verify the validation is wired into the live platform.

---

## YAML Strict Validation

**Purpose:** Unknown fields rejected, required fields enforced, enum values validated.

### Step 1 — Create agent and verify valid config

```
agency_create(name="val-schema", preset="generalist")
agency_show(agent="val-schema")
```

**Expected:** Agent created successfully.

### Step 2 — Add unknown field to agent.yaml

Manually edit `~/.agency/agents/val-schema/agent.yaml` to add:

```yaml
bogus_field: injection_attempt
```

Then:

```
agency_show(agent="val-schema")
```

**Expected:** Validation warning about unknown field.

### Step 3 — Remove required field from constraints.yaml

Manually edit `~/.agency/agents/val-schema/constraints.yaml` to remove the `agent:` field.

Then attempt to start:

```
agency_start(agent="val-schema")
```

**Expected:** Start fails with validation error — required field missing.

### Step 4 — Restore and verify

Restore the original files (re-create the agent):

```
agency_delete(agent="val-schema")
agency_create(name="val-schema", preset="generalist")
agency_start(agent="val-schema")
```

**Expected:** Starts successfully after clean files.

### Cleanup

```
agency_stop(agent="val-schema", halt_type="immediate", reason="cleanup")
agency_delete(agent="val-schema")
```

---

## Connector Schema Validation

**Purpose:** Cross-field validation — source type determines required fields.

### Step 1 — Valid webhook connector

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
      agent: val-schema-worker
    brief: "Process: {{ payload }}"
```

```
agency_connector_list()
```

**Expected:** val-webhook listed.

### Step 2 — Invalid poll connector (missing url)

Create `~/.agency/connectors/val-bad-poll.yaml`:

```yaml
kind: connector
name: val-bad-poll
version: "1.0.0"
source:
  type: poll
  interval: "5m"
routes:
  - match:
      any: true
    target:
      agent: test
    brief: "test"
```

```
agency_connector_activate(name="val-bad-poll")
```

**Expected:** Validation error — poll source requires 'url'.

### Step 3 — Invalid route (no target or relay)

Create `~/.agency/connectors/val-bad-route.yaml`:

```yaml
kind: connector
name: val-bad-route
version: "1.0.0"
source:
  type: webhook
routes:
  - match:
      type: test
```

**Expected:** Validation error — route must specify either 'target' or 'relay'.

### Cleanup

```bash
rm ~/.agency/connectors/val-webhook.yaml
rm ~/.agency/connectors/val-bad-poll.yaml
rm ~/.agency/connectors/val-bad-route.yaml
```

---

## Pack Schema Validation

**Purpose:** Pack validation — duplicate names rejected, empty agents rejected.

### Step 1 — Valid pack deploys

Create `/tmp/val-pack-valid.yaml`:

```yaml
kind: pack
name: val-pack-valid
version: "1.0.0"
team:
  name: val-pack-team
  agents:
    - name: val-pack-a
      preset: generalist
    - name: val-pack-b
      preset: generalist
```

```
agency_deploy(pack_file="/tmp/val-pack-valid.yaml")
```

**Expected:** Deploys successfully.

### Step 2 — Duplicate agent names rejected

Create `/tmp/val-pack-dupes.yaml`:

```yaml
kind: pack
name: val-pack-dupes
version: "1.0.0"
team:
  name: val-pack-team
  agents:
    - name: same-name
      preset: generalist
    - name: same-name
      preset: engineer
```

```
agency_deploy(pack_file="/tmp/val-pack-dupes.yaml")
```

**Expected:** Validation error — duplicate agent names.

### Step 3 — Empty agents rejected

Create `/tmp/val-pack-empty.yaml`:

```yaml
kind: pack
name: val-pack-empty
version: "1.0.0"
team:
  name: val-pack-team
  agents: []
```

```
agency_deploy(pack_file="/tmp/val-pack-empty.yaml")
```

**Expected:** Validation error — pack must define at least one agent.

### Cleanup

```
agency_teardown(pack_name="val-pack-valid", delete=true)
```

```bash
rm /tmp/val-pack-valid.yaml /tmp/val-pack-dupes.yaml /tmp/val-pack-empty.yaml
```
