# Group 3: Capabilities

**Depends on:** Group 2 (agent lifecycle working).

---

## Capability Registry

**Purpose:** Full capability lifecycle — add, show, enable, disable, delete.

### Step 1 — List capabilities

```
agency_cap_list()
```

**Expected:** Shows bundled capabilities (brave-search, github, slack if imported).

### Step 2 — Add a service capability

```
agency_cap_add(kind="api", name="val-test-api", url="https://api.example.com", key_env="TEST_API_KEY")
```

**Expected:** Capability added to registry.

### Step 3 — Show capability details

```
agency_cap_show(name="val-test-api")
```

**Expected:** Shows name, kind, URL, key_env.

### Step 4 — Enable for an agent

```
agency_cap_enable(name="val-test-api", key="test-key-value-12345")
```

**Expected:** Capability enabled.

### Step 5 — Disable

```
agency_cap_disable(name="val-test-api")
```

**Expected:** Capability disabled.

### Step 6 — Delete

```
agency_cap_delete(name="val-test-api")
```

**Expected:** Removed from registry.

### Step 7 — Verify removal

```
agency_cap_list()
```

**Expected:** val-test-api no longer listed.

---

## Service Credentials

**Purpose:** Grant and revoke external service access.

### Step 1 — Create agent

```
agency_create(name="val-cred", preset="generalist")
```

### Step 2 — Grant service

```
agency_grant(agent="val-cred", service="brave-search", key="sk-test-brave-12345")
```

**Expected:** Service granted. Credential stored at egress layer.

### Step 3 — Verify grant

```
agency_show(agent="val-cred")
```

**Expected:** brave-search listed as granted service.

### Step 4 — Revoke service

```
agency_revoke(agent="val-cred", service="brave-search")
```

**Expected:** Service revoked.

### Cleanup

```
agency_delete(agent="val-cred")
```

---

## Persistent Memory

**Purpose:** Agent memory survives restarts.

### Step 1 — Create and start

```
agency_create(name="val-memory", preset="generalist")
agency_start(agent="val-memory")
```

### Step 2 — Save memories

```
agency_brief(agent="val-memory", task="Save these 3 memories using save_memory:\n1. topic='project-alpha', content='The deadline is March 15'\n2. topic='api-keys', content='Use the v2 endpoint for authentication'\n3. topic='team-notes', content='Alice handles frontend, Bob handles backend'")
```

**Expected:** 3 memories saved.

### Step 3 — Search memory

```
agency_brief(agent="val-memory", task="Search your memory for 'deadline' using search_memory.")
```

**Expected:** Finds project-alpha entry about March 15.

### Step 4 — List memories

```
agency_brief(agent="val-memory", task="List all your memories using list_memories.")
```

**Expected:** 3 memories listed.

### Step 5 — Restart and verify persistence

```
agency_stop(agent="val-memory", reason="memory persistence test")
agency_start(agent="val-memory")
agency_brief(agent="val-memory", task="List all your memories and search for 'Alice'.")
```

**Expected:** All 3 memories still present. Search finds team-notes.

### Step 6 — Delete a memory

```
agency_brief(agent="val-memory", task="Delete the memory with topic 'api-keys' using delete_memory. Then list all memories.")
```

**Expected:** 2 memories remain (project-alpha and team-notes).

### Cleanup

```
agency_stop(agent="val-memory", halt_type="immediate", reason="validation cleanup")
agency_delete(agent="val-memory")
```

---

## Skills & Presets

**Purpose:** Preset variety and skills directory access.

### Step 1 — Engineer preset with skills

```
agency_create(name="val-skills", preset="engineer")
agency_start(agent="val-skills")
```

### Step 2 — Agent sees skills

```
agency_brief(agent="val-skills", task="Check what skills are available to you. List any skills directories you can see.")
```

**Expected:** Agent reports available skills from its preset.

### Step 3 — Minimal preset

```
agency_stop(agent="val-skills", halt_type="immediate", reason="preset test")
agency_create(name="val-minimal", preset="minimal")
```

**Expected:** Minimal agent created with conservative, core-tools-only identity.

### Step 4 — Function preset

```
agency_create(name="val-func", preset="function", agent_type="function")
```

**Expected:** Function agent created with type=function.

### Step 5 — All presets create successfully

Create and immediately delete agents for each bundled preset to verify none are broken:

```
agency_create(name="val-p-researcher", preset="researcher")
agency_delete(agent="val-p-researcher")
agency_create(name="val-p-writer", preset="writer")
agency_delete(agent="val-p-writer")
agency_create(name="val-p-ops", preset="ops")
agency_delete(agent="val-p-ops")
agency_create(name="val-p-analyst", preset="analyst")
agency_delete(agent="val-p-analyst")
```

**Expected:** All create and delete without errors.

### Cleanup

```
agency_delete(agent="val-skills")
agency_delete(agent="val-minimal")
agency_delete(agent="val-func")
```

---

## Extra Mounts

**Purpose:** Read-only extra mount enforcement.

### Step 1 — Prepare mount source

```bash
mkdir -p /tmp/agency-val-mount
echo "VALIDATION_MOUNT_CONTENT_12345" > /tmp/agency-val-mount/test.txt
```

### Step 2 — Create agent with extra mount

```
agency_create(name="val-mount", preset="generalist")
```

Manually edit `~/.agency/agents/val-mount/workspace.yaml` to add:

```yaml
extra_mounts:
  - source: /tmp/agency-val-mount
    target: /test-data
```

### Step 3 — Start and read from mount

```
agency_start(agent="val-mount")
agency_brief(agent="val-mount", task="Read /test-data/test.txt and report its exact contents.")
```

**Expected:** Agent reports "VALIDATION_MOUNT_CONTENT_12345".

### Step 4 — Write to mount fails

```
agency_brief(agent="val-mount", task="Try to create /test-data/output.txt with content 'hello'. Report whether the write succeeded or failed.")
```

**Expected:** Write fails — mount is read-only.

### Cleanup

```
agency_stop(agent="val-mount", halt_type="immediate", reason="validation cleanup")
agency_delete(agent="val-mount")
rm -rf /tmp/agency-val-mount
```
