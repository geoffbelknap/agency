---
description: "Determine whether the knowledge graph actually generates value: useful entities, meaningful relationships, and knowle..."
---

# Knowledge Graph Test Runbook

## Purpose

Determine whether the knowledge graph actually generates value: useful entities, meaningful relationships, and knowledge that compounds across sessions — or whether it just accumulates noise that wastes tokens and storage.

## Pre-Test Setup

### 1. Clean slate

```bash
# Stop agents
agency stop jarvis

# Wipe existing graph (keep backup)
cp ~/.agency/knowledge/data/knowledge.db ~/.agency/knowledge/data/knowledge.db.bak
rm ~/.agency/knowledge/data/knowledge.db*
rm -f ~/.agency/knowledge/data/.ontology-migrated

# Rebuild with latest code (includes ontology migration + validation)
make deploy

# Verify ontology is loaded
docker exec agency-infra-knowledge cat /app/ontology.yaml | head -5
# Should show: version: 1, name: default
```

### 2. Verify curator is running

```bash
# Check knowledge container logs for curator startup
agency log jarvis 2>&1 | grep -i curator
# Should show: "Curation loop started (interval=600s, mode=auto)"
```

### 3. Baseline graph state

```bash
# Should show only structural nodes (agent, channel) from rule ingester
curl -s localhost:8200/api/v1/admin/knowledge/stats | jq .
```

Record: `__baseline_nodes__`, `__baseline_edges__`, `__baseline_kinds__`

---

## Test Scenarios

Each scenario is a conversation with an agent that exercises a different knowledge extraction pattern. After each scenario, inspect the graph to evaluate what was captured.

### Scenario 1: People and Preferences (5 min)

**Goal**: Test whether the synthesizer extracts person entities with useful attributes.

**Conversation** (send via `agency send jarvis`):

```
Message 1: "Hey Jarvis, I'm Alex Chen, the new SRE on the platform team. I'm based in Austin, TX and I prefer Slack over email for anything non-urgent."

Message 2: "My manager is Sarah Kim — she runs the SRE team. Our director is Marcus Reeves in the infrastructure department."

Message 3: "For on-call escalation, page Sarah first, then Marcus. My timezone is CT so I'm usually available 8am-6pm Central."
```

Wait 5 minutes for synthesis cycle.

**Expected graph additions**:
- Entities: `person:Alex Chen`, `person:Sarah Kim`, `person:Marcus Reeves`, `team:SRE`, `organization:infrastructure department`
- Relationships: `Alex Chen -[member_of]-> SRE`, `Sarah Kim -[manages]-> SRE`, `SRE -[part_of]-> infrastructure department`, `Alex Chen -[escalate_to]-> Sarah Kim -[escalate_to]-> Marcus Reeves`
- Preferences: `Alex Chen -[prefers]-> Slack for non-urgent`

**Evaluate**:
- [ ] Did person entities get created with correct kinds? (not `fact` or `finding`)
- [ ] Did relationships use ontology types? (not freeform like `is_manager_of`)
- [ ] Was the escalation chain captured?
- [ ] Were preferences captured?
- [ ] Any duplicates? (e.g., "Alex" and "Alex Chen" as separate nodes)

### Scenario 2: Technical Decision (5 min)

**Goal**: Test decision capture with rationale and alternatives.

**Conversation**:

```
Message 1: "We decided to migrate from Redis to DragonflyDB for the session cache. Sarah approved it last Thursday."

Message 2: "The main reason is Dragonfly's multi-threaded architecture — we're hitting Redis single-thread limits at 50K concurrent sessions. We also looked at KeyDB and Memcached but KeyDB's MVCC overhead was worse for our write pattern."

Message 3: "The migration is scheduled for next sprint. Risk: Dragonfly's Lua scripting isn't 100% compatible, so we need to audit our 12 Lua scripts first."
```

Wait for synthesis.

**Expected graph additions**:
- Entities: `decision:Migrate from Redis to DragonflyDB`, `system:Redis`, `system:DragonflyDB`, `system:session cache`, `risk:Lua scripting compatibility`, `task:audit Lua scripts`
- Relationships: `decision -[decided_by]-> Sarah Kim`, `session cache -[depends_on]-> DragonflyDB`, `risk -[relates_to]-> decision`

**Evaluate**:
- [ ] Was this captured as a `decision` (not `finding` or `fact`)?
- [ ] Were alternatives mentioned? (KeyDB, Memcached)
- [ ] Was the rationale in the summary?
- [ ] Was the risk captured as a separate entity?
- [ ] Did it link to the `Sarah Kim` person from Scenario 1? (not create a duplicate)

### Scenario 3: Incident and Resolution (5 min)

**Goal**: Test incident → root cause → resolution chain.

**Conversation**:

```
Message 1: "We had an outage on the payments API today from 2pm-3:15pm CT. P1 severity. The checkout flow was returning 503s."

Message 2: "Root cause: the connection pool to Postgres was exhausted. We had a query regression in the order lookup — a missing index on orders.customer_id caused full table scans under load."

Message 3: "Fix was adding the index (deployed at 3:10pm). We also added a connection pool alert at 80% utilization so we catch this earlier next time. Marcus signed off on the postmortem."
```

Wait for synthesis.

**Expected graph additions**:
- Entities: `incident:payments API outage`, `system:payments API`, `system:Postgres`, `cause:connection pool exhaustion`, `resolution:add index on orders.customer_id`, `metric:connection pool utilization at 80%`
- Relationships: `incident -[caused_by]-> cause`, `cause -[resolved_by]-> resolution`, `Marcus Reeves -[decided]-> resolution`

**Evaluate**:
- [ ] Was this an `incident` (not `finding`)?
- [ ] Was root cause captured as `cause`?
- [ ] Was the resolution linked to the incident?
- [ ] Did `Marcus Reeves` link to the existing node? (not create "Marcus")
- [ ] Was the new alert captured as a `metric` or `configuration`?

### Scenario 4: Cross-Session Recall (5 min)

**Goal**: Test whether knowledge from previous scenarios is usable in a new session.

```bash
# Restart agent to get a fresh session
agency restart jarvis
```

**Conversation**:

```
Message 1: "Who manages the SRE team?"

Message 2: "What was the root cause of the last payments outage?"

Message 3: "What did we decide about the session cache?"
```

**Evaluate**:
- [ ] Could the agent answer from graph knowledge? (not hallucinate)
- [ ] Were answers accurate to what was captured in Scenarios 1-3?
- [ ] Did the agent cite knowledge graph entities?

### Scenario 5: Ontology Compliance Under Ambiguity (5 min)

**Goal**: Test whether the LLM stays within ontology types when the input is ambiguous.

**Conversation**:

```
Message 1: "FYI the Kubernetes API server has been flapping since Tuesday. Not an outage but it's annoying — pods take 30s to schedule instead of 2s."

Message 2: "I think it's related to the etcd compaction job we added. That's my hunch, not confirmed."

Message 3: "Also, the new hire orientation doc is at go/onboarding — it's outdated though, still references the old VPN setup."
```

Wait for synthesis.

**Expected graph additions**:
- The flapping should be an `incident` (not `finding` or `observation`)
- The hunch should be an `assumption` (not `fact`)
- The doc should be a `document` with a `url`
- Relationships should all be ontology-defined types

**Evaluate**:
- [ ] Zero freeform `kind` values in new nodes? (`agency admin knowledge` → check kinds)
- [ ] Zero freeform `relation` values in new edges?
- [ ] The hunch was marked as `assumption`, not `fact`?
- [ ] The doc URL was captured?

---

## Measurement

After all 5 scenarios, collect metrics:

```bash
# Graph stats
curl -s localhost:8200/api/v1/admin/knowledge/stats | jq .

# All entity types in use
sqlite3 ~/.agency/knowledge/data/knowledge.db \
  "SELECT kind, COUNT(*) FROM nodes WHERE curation_status IS NULL GROUP BY kind ORDER BY COUNT(*) DESC"

# All relationship types in use
sqlite3 ~/.agency/knowledge/data/knowledge.db \
  "SELECT relation, COUNT(*) FROM edges GROUP BY relation ORDER BY COUNT(*) DESC"

# Curation log — what did the curator catch?
curl -s localhost:8200/api/v1/admin/knowledge/curation/log | jq '.entries | length'

# Health metrics
docker exec agency-infra-knowledge python3 -c "
from store import KnowledgeStore; from curator import Curator
s = KnowledgeStore('/data'); c = Curator(s, mode='active')
import json; print(json.dumps(c.compute_health_metrics(), indent=2))
"
```

### Scorecard

| Metric | Target | Actual |
|--------|--------|--------|
| Entity types used | >= 8 distinct ontology types | |
| Relationship types used | >= 5 distinct ontology types | |
| Freeform kinds (non-ontology) | 0 | |
| Freeform relations (non-ontology) | 0 | |
| Duplicate entities created | 0 | |
| Cross-session recall accuracy | >= 3/3 correct | |
| Orphan ratio (health metric) | < 0.30 | |
| Curator merges triggered | 0 (no duplicates to merge) | |
| Noise nodes (session checkpoints, debug logs) | 0 | |

### Pass / Fail Criteria

**Pass**: >= 7 of 9 metrics hit target. The graph produces structured, queryable knowledge with ontology-consistent types and meaningful relationships.

**Fail**: < 7 of 9. Common failure modes:
- LLM ignores ontology types → extraction prompt isn't constraining enough
- Everything becomes `fact` or `finding` → type descriptions aren't distinctive enough
- No relationships extracted → relationship prompt section needs examples
- Duplicates proliferate → synthesizer dedup (label matching) is too strict/loose
- Session noise still appears → episodic/procedural memory is leaking into synthesis

### What to Fix Based on Results

| Failure | Root Cause | Fix |
|---------|-----------|-----|
| Freeform kinds appear | LLM ignoring type list | Add few-shot examples to extraction prompt |
| Everything is `fact` | Type descriptions too similar | Make descriptions more distinctive, add negative examples |
| No relationships | LLM focused on entities only | Add relationship examples to prompt, increase relationship weight |
| Duplicates | Label matching too strict | Lower fuzzy threshold or add normalization |
| Cross-session recall fails | Knowledge not in system prompt | Check memory injection, verify ontology version stamp |
| High orphan ratio | Entities created without relationships | Add prompt instruction: "every entity should have at least one relationship" |
