# Knowledge Graph Intelligence — Phase 1: Schema Foundations

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add edge provenance tiers, principal UUID registry, and authorization scopes to the knowledge graph — the schema foundation for universal ingestion, community detection, and query feedback.

**Architecture:** Three schema additions to the knowledge store's SQLite database: (1) `provenance` column on edges with migration from existing `source_type`, (2) `principal_registry` table for UUID-based identity, (3) `scope` JSON column on nodes and edges replacing flat `source_channels`. All changes go through `KnowledgeStore` methods. Go gateway gets proxy methods and CLI commands for the principal registry.

**Tech Stack:** Python (knowledge service), Go (gateway/CLI), SQLite, aiohttp

**Spec:** `docs/specs/knowledge-graph-intelligence.md` — Phase 1 section

---

## File Structure

### Files to Create

| File | Purpose |
|------|---------|
| `images/knowledge/principal_registry.py` | PrincipalRegistry class — CRUD for UUID-based principal identity |
| `images/knowledge/scope.py` | Scope model — filtering logic, intersection, overlap checks |
| `images/tests/test_edge_provenance.py` | Tests for provenance column, migration, filtering |
| `images/tests/test_principal_registry.py` | Tests for principal registry CRUD, UUID resolution |
| `images/tests/test_scope_model.py` | Tests for scope filtering, traversal enforcement, edge scope inheritance |

### Files to Modify

| File | Changes |
|------|---------|
| `images/knowledge/store.py` | Add `provenance` column to edges, `scope` column to nodes/edges, migration logic, update `find_nodes()` and `add_edge()` |
| `images/knowledge/curator.py` | Use provenance in health metrics, add performance benchmark timers |
| `images/knowledge/synthesizer.py` | Tag edges with `provenance='AMBIGUOUS'` |
| `images/knowledge/ingester.py` | Tag edges with `provenance='EXTRACTED'` |
| `images/knowledge/server.py` | Register PrincipalRegistry, add endpoints, pass provenance/scope params |
| `images/body/knowledge_tools.py` | Pass `min_provenance` param in query_graph |
| `internal/knowledge/proxy.go` | Add principal registry proxy methods |
| `internal/api/routes.go` | Add principal registry routes |
| `internal/api/handlers_admin.go` | Add principal registry admin actions |
| `internal/cli/commands.go` | Add `agency knowledge principals` subcommand |
| `internal/apiclient/client.go` | Add principal registry client methods |

---

## Task 1: Edge Provenance Column and Migration

**Files:**
- Modify: `images/knowledge/store.py`
- Test: `images/tests/test_edge_provenance.py`

- [ ] **Step 1: Write the failing test for provenance column existence**

```python
# images/tests/test_edge_provenance.py
"""Tests for edge provenance tiers."""
import os
import sqlite3
import tempfile
import pytest

# Add parent to path for imports
import sys
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from store import KnowledgeStore


@pytest.fixture
def store(tmp_path):
    db_path = str(tmp_path / "test.db")
    s = KnowledgeStore(db_path)
    return s


def test_edge_has_provenance_column(store):
    """Edges table must have a provenance column."""
    cursor = store._db.execute("PRAGMA table_info(edges)")
    columns = {row[1] for row in cursor.fetchall()}
    assert "provenance" in columns


def test_edge_provenance_defaults_to_ambiguous(store):
    """New edges without explicit provenance default to AMBIGUOUS."""
    nid1 = store.add_node("node-a", "fact", "test node a")
    nid2 = store.add_node("node-b", "fact", "test node b")
    eid = store.add_edge(nid1, nid2, "relates_to")
    row = store._db.execute("SELECT provenance FROM edges WHERE id = ?", (eid,)).fetchone()
    assert row[0] == "AMBIGUOUS"


def test_edge_provenance_accepts_valid_values(store):
    """Edges accept EXTRACTED, INFERRED, AMBIGUOUS provenance."""
    nid1 = store.add_node("node-c", "fact", "test")
    nid2 = store.add_node("node-d", "fact", "test")
    for prov in ("EXTRACTED", "INFERRED", "AMBIGUOUS"):
        eid = store.add_edge(nid1, nid2, f"rel_{prov.lower()}", provenance=prov)
        row = store._db.execute("SELECT provenance FROM edges WHERE id = ?", (eid,)).fetchone()
        assert row[0] == prov


def test_edge_provenance_rejects_invalid_value(store):
    """Invalid provenance values should raise ValueError."""
    nid1 = store.add_node("node-e", "fact", "test")
    nid2 = store.add_node("node-f", "fact", "test")
    with pytest.raises(ValueError, match="provenance"):
        store.add_edge(nid1, nid2, "relates_to", provenance="GUESSED")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_edge_provenance.py -v`
Expected: FAIL — `provenance` column doesn't exist, `add_edge()` doesn't accept `provenance` param

- [ ] **Step 3: Add provenance column to schema and add_edge()**

In `images/knowledge/store.py`, find the `edges` table creation in `__init__`:

```python
# In __init__, after the CREATE TABLE edges statement, add:
        try:
            self._db.execute("ALTER TABLE edges ADD COLUMN provenance TEXT DEFAULT 'AMBIGUOUS'")
        except Exception:
            pass  # Column already exists
```

Find the `add_edge` method signature and add the `provenance` parameter:

```python
    def add_edge(self, source_id, target_id, relation, weight=1.0,
                 properties=None, source_channel="", provenance_id="",
                 provenance="AMBIGUOUS"):
        """Add or update an edge between two nodes."""
        valid_provenance = ("EXTRACTED", "INFERRED", "AMBIGUOUS")
        if provenance not in valid_provenance:
            raise ValueError(f"provenance must be one of {valid_provenance}, got '{provenance}'")
```

Then in the INSERT statement for edges, add the `provenance` column and value.

Find the existing INSERT for edges (it will look like `INSERT INTO edges (id, source_id, target_id, relation, weight, properties, source_channel, provenance_id, timestamp)`). Add `provenance` to both the column list and values:

```sql
INSERT INTO edges (id, source_id, target_id, relation, weight, properties, source_channel, provenance_id, timestamp, provenance)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
```

And add the `provenance` value to the params tuple.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_edge_provenance.py -v`
Expected: All 4 tests PASS

- [ ] **Step 5: Commit**

```bash
cd /home/geoff/agency-workspace/agency
git add images/knowledge/store.py images/tests/test_edge_provenance.py
git commit -m "feat(knowledge): add provenance column to edges table

Three-tier provenance model: EXTRACTED (deterministic), INFERRED
(logical), AMBIGUOUS (semantic/LLM). Defaults to AMBIGUOUS.
Validates on insert."
```

---

## Task 2: Provenance Migration for Existing Edges

**Files:**
- Modify: `images/knowledge/store.py`
- Test: `images/tests/test_edge_provenance.py`

- [ ] **Step 1: Write the failing test for migration**

Append to `images/tests/test_edge_provenance.py`:

```python
def test_migrate_provenance_from_source_type(store):
    """Existing edges should get provenance based on their creation context.

    Since edges don't have source_type directly, migration uses the source
    node's source_type as a proxy:
    - Nodes with source_type='rule' → their edges get EXTRACTED
    - Nodes with source_type='llm' → their edges get AMBIGUOUS
    - Nodes with source_type='agent' → their edges get INFERRED
    """
    # Create nodes with different source_types
    rule_id = store.add_node("rule-node", "fact", "from rule", source_type="rule")
    llm_id = store.add_node("llm-node", "fact", "from llm", source_type="llm")
    agent_id = store.add_node("agent-node", "fact", "from agent", source_type="agent")
    target_id = store.add_node("target", "fact", "target node")

    # Create edges — they'll default to AMBIGUOUS
    eid_rule = store.add_edge(rule_id, target_id, "relates_to")
    eid_llm = store.add_edge(llm_id, target_id, "relates_to")
    eid_agent = store.add_edge(agent_id, target_id, "relates_to")

    # Run migration
    stats = store.migrate_edge_provenance()

    # Verify
    row_rule = store._db.execute("SELECT provenance FROM edges WHERE id = ?", (eid_rule,)).fetchone()
    row_llm = store._db.execute("SELECT provenance FROM edges WHERE id = ?", (eid_llm,)).fetchone()
    row_agent = store._db.execute("SELECT provenance FROM edges WHERE id = ?", (eid_agent,)).fetchone()

    assert row_rule[0] == "EXTRACTED"
    assert row_llm[0] == "AMBIGUOUS"
    assert row_agent[0] == "INFERRED"
    assert stats["migrated"] == 3


def test_migrate_provenance_is_idempotent(store):
    """Running migration twice should not change already-migrated edges."""
    nid1 = store.add_node("node-x", "fact", "test", source_type="rule")
    nid2 = store.add_node("node-y", "fact", "test")
    store.add_edge(nid1, nid2, "relates_to")

    stats1 = store.migrate_edge_provenance()
    stats2 = store.migrate_edge_provenance()

    assert stats1["migrated"] == 1
    assert stats2["migrated"] == 0  # Already migrated, skip
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_edge_provenance.py::test_migrate_provenance_from_source_type images/tests/test_edge_provenance.py::test_migrate_provenance_is_idempotent -v`
Expected: FAIL — `migrate_edge_provenance` method doesn't exist

- [ ] **Step 3: Implement migrate_edge_provenance()**

Add to `images/knowledge/store.py` in the `KnowledgeStore` class:

```python
    def migrate_edge_provenance(self):
        """Migrate existing edges to provenance tiers based on source node's source_type.

        Mapping: rule → EXTRACTED, llm → AMBIGUOUS, agent → INFERRED, other → AMBIGUOUS.
        Only migrates edges still at default AMBIGUOUS that haven't been explicitly set.
        Uses a migration marker in edge properties to track what's been migrated.
        """
        source_type_to_provenance = {
            "rule": "EXTRACTED",
            "llm": "AMBIGUOUS",
            "agent": "INFERRED",
            "local": "AMBIGUOUS",
        }
        migrated = 0

        # Find edges that are AMBIGUOUS (default) and whose source node has a known source_type
        rows = self._db.execute("""
            SELECT e.id, n.source_type
            FROM edges e
            JOIN nodes n ON e.source_id = n.id
            WHERE e.provenance = 'AMBIGUOUS'
            AND json_extract(e.properties, '$._provenance_migrated') IS NULL
        """).fetchall()

        for edge_id, node_source_type in rows:
            target_provenance = source_type_to_provenance.get(node_source_type, "AMBIGUOUS")
            self._db.execute("""
                UPDATE edges
                SET provenance = ?,
                    properties = json_set(properties, '$._provenance_migrated', 1)
                WHERE id = ?
            """, (target_provenance, edge_id))
            migrated += 1

        self._db.commit()
        return {"migrated": migrated}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_edge_provenance.py -v`
Expected: All 6 tests PASS

- [ ] **Step 5: Commit**

```bash
cd /home/geoff/agency-workspace/agency
git add images/knowledge/store.py images/tests/test_edge_provenance.py
git commit -m "feat(knowledge): add provenance migration for existing edges

Maps source node source_type to edge provenance: rule→EXTRACTED,
agent→INFERRED, llm/other→AMBIGUOUS. Idempotent via property marker."
```

---

## Task 3: Tag Provenance at Creation Time

**Files:**
- Modify: `images/knowledge/ingester.py`
- Modify: `images/knowledge/synthesizer.py`
- Test: `images/tests/test_edge_provenance.py`

- [ ] **Step 1: Write failing tests for ingester and synthesizer provenance**

Append to `images/tests/test_edge_provenance.py`:

```python
def test_ingester_creates_extracted_edges(store):
    """RuleIngester should create edges with EXTRACTED provenance."""
    from ingester import RuleIngester

    ingester = RuleIngester(store)
    ingester.ingest_message({
        "agent": "test-agent",
        "channel": "test-channel",
        "content": "test message",
        "flags": {},
    })

    # The member_of edge from agent to channel should be EXTRACTED
    edges = store._db.execute("""
        SELECT provenance FROM edges WHERE relation = 'member_of'
    """).fetchall()
    assert len(edges) > 0
    assert all(row[0] == "EXTRACTED" for row in edges)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_edge_provenance.py::test_ingester_creates_extracted_edges -v`
Expected: FAIL — ingester doesn't pass `provenance` to `add_edge()`

- [ ] **Step 3: Update RuleIngester to pass provenance='EXTRACTED'**

In `images/knowledge/ingester.py`, find the `_ensure_edge` method. It calls `self.store.add_edge()`. Add `provenance="EXTRACTED"` to every `add_edge()` call within this method:

```python
    def _ensure_edge(self, source_id, target_id, relation, weight=1.0,
                     properties=None, source_channel="", provenance_id=""):
        cache_key = (source_id, target_id, relation)
        if cache_key in self._edge_cache:
            return self._edge_cache[cache_key]
        eid = self.store.add_edge(
            source_id, target_id, relation,
            weight=weight, properties=properties,
            source_channel=source_channel, provenance_id=provenance_id,
            provenance="EXTRACTED"
        )
        self._edge_cache[cache_key] = eid
        return eid
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_edge_provenance.py::test_ingester_creates_extracted_edges -v`
Expected: PASS

- [ ] **Step 5: Update LLMSynthesizer to pass provenance='AMBIGUOUS'**

In `images/knowledge/synthesizer.py`, find the `_apply_extraction` method. It calls `self.store.add_edge()` for relationships. Add `provenance="AMBIGUOUS"` to those calls:

```python
        # In _apply_extraction, where edges are created from extraction.relationships:
        eid = self.store.add_edge(
            source_id, target_id, relation,
            weight=weight, source_channel=source_channel,
            provenance="AMBIGUOUS"
        )
```

- [ ] **Step 6: Run full provenance test suite**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_edge_provenance.py -v`
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
cd /home/geoff/agency-workspace/agency
git add images/knowledge/ingester.py images/knowledge/synthesizer.py images/tests/test_edge_provenance.py
git commit -m "feat(knowledge): tag edge provenance at creation time

RuleIngester creates EXTRACTED edges. LLMSynthesizer creates
AMBIGUOUS edges. Curator relationship inference already uses
source_type='inferred' — will be INFERRED in a follow-up."
```

---

## Task 4: Provenance Filtering in find_nodes()

**Files:**
- Modify: `images/knowledge/store.py`
- Test: `images/tests/test_edge_provenance.py`

- [ ] **Step 1: Write failing test for provenance filtering**

Append to `images/tests/test_edge_provenance.py`:

```python
def test_get_edges_filters_by_min_provenance(store):
    """get_edges should support min_provenance filtering."""
    nid1 = store.add_node("hub-node", "system", "central node")
    nid2 = store.add_node("extracted-peer", "system", "peer a")
    nid3 = store.add_node("inferred-peer", "system", "peer b")
    nid4 = store.add_node("ambiguous-peer", "system", "peer c")

    store.add_edge(nid1, nid2, "depends_on", provenance="EXTRACTED")
    store.add_edge(nid1, nid3, "depends_on", provenance="INFERRED")
    store.add_edge(nid1, nid4, "depends_on", provenance="AMBIGUOUS")

    # No filter — all 3 edges
    all_edges = store.get_edges(nid1, direction="outgoing")
    assert len(all_edges) == 3

    # Min INFERRED — EXTRACTED + INFERRED only
    filtered = store.get_edges(nid1, direction="outgoing", min_provenance="INFERRED")
    assert len(filtered) == 2
    provs = {e["provenance"] for e in filtered}
    assert provs == {"EXTRACTED", "INFERRED"}

    # Min EXTRACTED — only EXTRACTED
    strict = store.get_edges(nid1, direction="outgoing", min_provenance="EXTRACTED")
    assert len(strict) == 1
    assert strict[0]["provenance"] == "EXTRACTED"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_edge_provenance.py::test_get_edges_filters_by_min_provenance -v`
Expected: FAIL — `get_edges()` doesn't accept `min_provenance`, edges don't have `provenance` in returned dicts

- [ ] **Step 3: Update get_edges() to support min_provenance**

In `images/knowledge/store.py`, update the `get_edges` method:

1. Add `min_provenance=None` parameter to the method signature.
2. Add provenance to the SELECT columns (it should already be returned if the column exists, but verify it's included in the dict construction).
3. Add a WHERE clause for provenance filtering:

```python
    _PROVENANCE_RANK = {"EXTRACTED": 1, "INFERRED": 2, "AMBIGUOUS": 3}

    def get_edges(self, node_id, direction="outgoing", relation=None, min_provenance=None):
        # ... existing query building ...

        # Add provenance filter
        if min_provenance:
            max_rank = self._PROVENANCE_RANK.get(min_provenance, 3)
            allowed = [p for p, r in self._PROVENANCE_RANK.items() if r <= max_rank]
            placeholders = ",".join("?" * len(allowed))
            # Append to WHERE clause:
            query += f" AND provenance IN ({placeholders})"
            params.extend(allowed)

        # ... rest of method ...
```

Also ensure the edge dict returned includes `"provenance": row[N]` for the provenance column.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_edge_provenance.py -v`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
cd /home/geoff/agency-workspace/agency
git add images/knowledge/store.py images/tests/test_edge_provenance.py
git commit -m "feat(knowledge): add min_provenance filter to get_edges()

Tier ranking: EXTRACTED(1) > INFERRED(2) > AMBIGUOUS(3).
Filter returns edges at or above the requested tier."
```

---

## Task 5: Principal Registry — Module and Tests

**Files:**
- Create: `images/knowledge/principal_registry.py`
- Test: `images/tests/test_principal_registry.py`

- [ ] **Step 1: Write the failing tests**

```python
# images/tests/test_principal_registry.py
"""Tests for the principal UUID registry."""
import os
import sqlite3
import tempfile
import pytest
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from principal_registry import PrincipalRegistry


@pytest.fixture
def registry(tmp_path):
    db_path = str(tmp_path / "test.db")
    conn = sqlite3.connect(db_path)
    reg = PrincipalRegistry(conn)
    return reg


def test_register_principal_returns_uuid(registry):
    """Registering a principal returns a UUID string."""
    uuid = registry.register("operator", "geoff")
    assert len(uuid) == 36  # Standard UUID format: 8-4-4-4-12
    assert uuid.count("-") == 4


def test_register_same_name_returns_same_uuid(registry):
    """Registering the same type+name twice returns the existing UUID."""
    uuid1 = registry.register("operator", "geoff")
    uuid2 = registry.register("operator", "geoff")
    assert uuid1 == uuid2


def test_register_different_types_different_uuids(registry):
    """Same name but different types get different UUIDs."""
    op_uuid = registry.register("operator", "alpha")
    agent_uuid = registry.register("agent", "alpha")
    assert op_uuid != agent_uuid


def test_resolve_uuid_to_name(registry):
    """Resolve a UUID back to type and name."""
    uuid = registry.register("agent", "security-auditor")
    result = registry.resolve(uuid)
    assert result["type"] == "agent"
    assert result["name"] == "security-auditor"
    assert result["uuid"] == uuid


def test_resolve_unknown_uuid_returns_none(registry):
    """Unknown UUID returns None."""
    result = registry.resolve("00000000-0000-0000-0000-000000000000")
    assert result is None


def test_resolve_name_to_uuid(registry):
    """Resolve type+name to UUID."""
    uuid = registry.register("operator", "geoff")
    result = registry.resolve_name("operator", "geoff")
    assert result == uuid


def test_resolve_name_unknown_returns_none(registry):
    """Unknown type+name returns None."""
    result = registry.resolve_name("operator", "nobody")
    assert result is None


def test_list_principals_by_type(registry):
    """List all principals of a given type."""
    registry.register("agent", "agent-a")
    registry.register("agent", "agent-b")
    registry.register("operator", "op-a")

    agents = registry.list_by_type("agent")
    assert len(agents) == 2
    names = {p["name"] for p in agents}
    assert names == {"agent-a", "agent-b"}


def test_format_principal_id(registry):
    """Format a principal as type:uuid string."""
    uuid = registry.register("operator", "geoff")
    pid = registry.format_id("operator", uuid)
    assert pid == f"operator:{uuid}"


def test_parse_principal_id(registry):
    """Parse a type:uuid string into components."""
    uuid = registry.register("operator", "geoff")
    pid = f"operator:{uuid}"
    ptype, puuid = registry.parse_id(pid)
    assert ptype == "operator"
    assert puuid == uuid


def test_parse_legacy_name_format(registry):
    """Parse a type:name string (legacy) and resolve to UUID if possible."""
    uuid = registry.register("operator", "geoff")
    ptype, resolved = registry.parse_id("operator:geoff")
    assert ptype == "operator"
    assert resolved == uuid  # Resolved name to UUID


def test_parse_legacy_unresolvable(registry):
    """Unresolvable type:name returns the name as-is."""
    ptype, value = registry.parse_id("operator:unknown-person")
    assert ptype == "operator"
    assert value == "unknown-person"  # Can't resolve, return as-is
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_principal_registry.py -v`
Expected: FAIL — `principal_registry` module doesn't exist

- [ ] **Step 3: Implement PrincipalRegistry**

```python
# images/knowledge/principal_registry.py
"""UUID-based principal identity registry.

Every principal (operator, agent, team, role, channel) gets a stable UUID.
Human-readable names are display labels, not identifiers. The registry
maps between UUIDs and names, and handles legacy type:name format during
the migration window.
"""
import sqlite3
import uuid as uuid_mod
from datetime import datetime, timezone


class PrincipalRegistry:
    """CRUD for UUID-based principal identity."""

    VALID_TYPES = ("operator", "agent", "team", "role", "channel")

    def __init__(self, db: sqlite3.Connection):
        self._db = db
        self._init_table()

    def _init_table(self):
        self._db.execute("""
            CREATE TABLE IF NOT EXISTS principal_registry (
                uuid TEXT PRIMARY KEY,
                type TEXT NOT NULL,
                name TEXT NOT NULL,
                created_at TEXT NOT NULL,
                metadata TEXT DEFAULT '{}'
            )
        """)
        self._db.execute("""
            CREATE INDEX IF NOT EXISTS idx_principal_type_name
            ON principal_registry(type, name)
        """)
        self._db.commit()

    def register(self, principal_type, name, metadata=None):
        """Register a principal. Returns existing UUID if already registered."""
        if principal_type not in self.VALID_TYPES:
            raise ValueError(
                f"principal_type must be one of {self.VALID_TYPES}, "
                f"got '{principal_type}'"
            )

        # Check if already registered
        row = self._db.execute(
            "SELECT uuid FROM principal_registry WHERE type = ? AND name = ?",
            (principal_type, name)
        ).fetchone()
        if row:
            return row[0]

        # Create new
        new_uuid = str(uuid_mod.uuid4())
        now = datetime.now(timezone.utc).isoformat()
        import json
        meta_json = json.dumps(metadata or {})
        self._db.execute(
            "INSERT INTO principal_registry (uuid, type, name, created_at, metadata) "
            "VALUES (?, ?, ?, ?, ?)",
            (new_uuid, principal_type, name, now, meta_json)
        )
        self._db.commit()
        return new_uuid

    def resolve(self, principal_uuid):
        """Resolve a UUID to its principal record. Returns None if not found."""
        row = self._db.execute(
            "SELECT uuid, type, name, created_at, metadata "
            "FROM principal_registry WHERE uuid = ?",
            (principal_uuid,)
        ).fetchone()
        if not row:
            return None
        return {
            "uuid": row[0],
            "type": row[1],
            "name": row[2],
            "created_at": row[3],
            "metadata": row[4],
        }

    def resolve_name(self, principal_type, name):
        """Resolve type+name to UUID. Returns None if not found."""
        row = self._db.execute(
            "SELECT uuid FROM principal_registry WHERE type = ? AND name = ?",
            (principal_type, name)
        ).fetchone()
        return row[0] if row else None

    def list_by_type(self, principal_type):
        """List all principals of a given type."""
        rows = self._db.execute(
            "SELECT uuid, type, name, created_at, metadata "
            "FROM principal_registry WHERE type = ? ORDER BY name",
            (principal_type,)
        ).fetchall()
        return [
            {"uuid": r[0], "type": r[1], "name": r[2],
             "created_at": r[3], "metadata": r[4]}
            for r in rows
        ]

    def list_all(self):
        """List all registered principals."""
        rows = self._db.execute(
            "SELECT uuid, type, name, created_at, metadata "
            "FROM principal_registry ORDER BY type, name"
        ).fetchall()
        return [
            {"uuid": r[0], "type": r[1], "name": r[2],
             "created_at": r[3], "metadata": r[4]}
            for r in rows
        ]

    @staticmethod
    def format_id(principal_type, principal_uuid):
        """Format as type:uuid string."""
        return f"{principal_type}:{principal_uuid}"

    def parse_id(self, principal_id):
        """Parse a type:identifier string.

        Handles both type:uuid and legacy type:name formats.
        For type:name, attempts resolution to UUID via the registry.
        Returns (type, uuid_or_name).
        """
        if ":" not in principal_id:
            raise ValueError(f"Invalid principal ID format: '{principal_id}' (expected type:id)")
        principal_type, identifier = principal_id.split(":", 1)

        # Check if identifier is already a UUID (contains dashes, 36 chars)
        if len(identifier) == 36 and identifier.count("-") == 4:
            return principal_type, identifier

        # Legacy name format — try to resolve
        resolved = self.resolve_name(principal_type, identifier)
        if resolved:
            return principal_type, resolved

        # Can't resolve — return name as-is for migration window
        return principal_type, identifier
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_principal_registry.py -v`
Expected: All 12 tests PASS

- [ ] **Step 5: Commit**

```bash
cd /home/geoff/agency-workspace/agency
git add images/knowledge/principal_registry.py images/tests/test_principal_registry.py
git commit -m "feat(knowledge): add principal UUID registry

UUID-based identity for operators, agents, teams, roles, channels.
Handles legacy type:name format during migration window. Names are
display labels, UUIDs are identifiers."
```

---

## Task 6: Scope Model — Module and Tests

**Files:**
- Create: `images/knowledge/scope.py`
- Test: `images/tests/test_scope_model.py`

- [ ] **Step 1: Write the failing tests**

```python
# images/tests/test_scope_model.py
"""Tests for the authorization scope model."""
import os
import pytest
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))

from scope import Scope


def test_scope_from_empty_dict():
    """Empty dict creates a scope with no restrictions."""
    s = Scope.from_dict({})
    assert s.channels == []
    assert s.principals == []
    assert s.classification is None


def test_scope_from_dict():
    """Scope parses all fields from dict."""
    s = Scope.from_dict({
        "channels": ["ch-uuid-1", "ch-uuid-2"],
        "principals": ["operator:op-uuid-1"],
        "classification": "internal",
    })
    assert s.channels == ["ch-uuid-1", "ch-uuid-2"]
    assert s.principals == ["operator:op-uuid-1"]
    assert s.classification == "internal"


def test_scope_to_dict():
    """Scope serializes to dict."""
    s = Scope(channels=["ch-1"], principals=["agent:a-1"], classification="restricted")
    d = s.to_dict()
    assert d == {
        "channels": ["ch-1"],
        "principals": ["agent:a-1"],
        "classification": "restricted",
    }


def test_scope_overlaps_by_channel():
    """Two scopes overlap if they share a channel."""
    a = Scope(channels=["ch-1", "ch-2"])
    b = Scope(channels=["ch-2", "ch-3"])
    assert a.overlaps(b)
    assert b.overlaps(a)


def test_scope_overlaps_by_principal():
    """Two scopes overlap if they share a principal."""
    a = Scope(principals=["operator:op-1"])
    b = Scope(principals=["operator:op-1", "agent:a-1"])
    assert a.overlaps(b)


def test_scope_no_overlap():
    """Disjoint scopes don't overlap."""
    a = Scope(channels=["ch-1"], principals=["operator:op-1"])
    b = Scope(channels=["ch-2"], principals=["agent:a-1"])
    assert not a.overlaps(b)


def test_empty_scope_overlaps_everything():
    """An empty scope (no restrictions) overlaps with any scope."""
    empty = Scope()
    restricted = Scope(channels=["ch-1"])
    assert empty.overlaps(restricted)
    assert restricted.overlaps(empty)


def test_scope_intersection():
    """Intersection returns only shared channels and principals."""
    a = Scope(channels=["ch-1", "ch-2"], principals=["operator:op-1"])
    b = Scope(channels=["ch-2", "ch-3"], principals=["operator:op-1", "agent:a-1"])
    result = a.intersection(b)
    assert result.channels == ["ch-2"]
    assert result.principals == ["operator:op-1"]


def test_scope_intersection_empty():
    """Intersection of disjoint scopes is empty."""
    a = Scope(channels=["ch-1"])
    b = Scope(channels=["ch-2"])
    result = a.intersection(b)
    assert result.channels == []
    assert result.principals == []


def test_scope_is_narrower_than():
    """A scope can check if it's a subset of another."""
    wide = Scope(channels=["ch-1", "ch-2", "ch-3"])
    narrow = Scope(channels=["ch-1", "ch-2"])
    assert narrow.is_narrower_than(wide)
    assert not wide.is_narrower_than(narrow)


def test_scope_from_source_channels():
    """Convenience: create scope from legacy source_channels list."""
    s = Scope.from_source_channels(["alpha", "beta"])
    assert s.channels == ["alpha", "beta"]
    assert s.principals == []
    assert s.classification is None


def test_scope_json_roundtrip():
    """Scope survives JSON serialization."""
    import json
    original = Scope(channels=["ch-1"], principals=["op:uuid"], classification="internal")
    json_str = json.dumps(original.to_dict())
    restored = Scope.from_dict(json.loads(json_str))
    assert restored.channels == original.channels
    assert restored.principals == original.principals
    assert restored.classification == original.classification
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_scope_model.py -v`
Expected: FAIL — `scope` module doesn't exist

- [ ] **Step 3: Implement Scope model**

```python
# images/knowledge/scope.py
"""Authorization scope model for the knowledge graph.

Scopes define visibility of nodes and edges. A principal can see a node
if its scope overlaps with the node's scope (channel match OR principal match).
Empty scopes (no restrictions) are treated as universally visible.
"""
from dataclasses import dataclass, field
from typing import Optional


@dataclass
class Scope:
    """Authorization scope for a graph node or edge."""

    channels: list = field(default_factory=list)
    principals: list = field(default_factory=list)
    classification: Optional[str] = None

    def to_dict(self):
        """Serialize to dict for JSON storage."""
        d = {
            "channels": self.channels,
            "principals": self.principals,
        }
        if self.classification:
            d["classification"] = self.classification
        return d

    @classmethod
    def from_dict(cls, data):
        """Deserialize from dict."""
        if not data:
            return cls()
        return cls(
            channels=data.get("channels", []),
            principals=data.get("principals", []),
            classification=data.get("classification"),
        )

    @classmethod
    def from_source_channels(cls, channels):
        """Create scope from legacy source_channels list."""
        return cls(channels=list(channels) if channels else [])

    def overlaps(self, other):
        """Check if this scope has any overlap with another.

        Empty scopes (no channels AND no principals) are treated as
        unrestricted — they overlap with everything.
        """
        # Empty scopes overlap with everything
        if not self.channels and not self.principals:
            return True
        if not other.channels and not other.principals:
            return True

        # Check channel overlap
        if self.channels and other.channels:
            if set(self.channels) & set(other.channels):
                return True

        # Check principal overlap
        if self.principals and other.principals:
            if set(self.principals) & set(other.principals):
                return True

        # Check cross: if one has only channels and other has only principals,
        # they don't overlap (different dimensions)
        # But if one has channels and other has channels, we already checked above
        return False

    def intersection(self, other):
        """Return a new scope containing only shared channels and principals."""
        shared_channels = sorted(set(self.channels) & set(other.channels))
        shared_principals = sorted(set(self.principals) & set(other.principals))
        return Scope(channels=shared_channels, principals=shared_principals)

    def is_narrower_than(self, other):
        """Check if this scope is a subset of another.

        True if all channels and principals in self are also in other.
        """
        if self.channels and not set(self.channels).issubset(set(other.channels)):
            return False
        if self.principals and not set(self.principals).issubset(set(other.principals)):
            return False
        return True
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_scope_model.py -v`
Expected: All 13 tests PASS

- [ ] **Step 5: Commit**

```bash
cd /home/geoff/agency-workspace/agency
git add images/knowledge/scope.py images/tests/test_scope_model.py
git commit -m "feat(knowledge): add Scope model for authorization

Dataclass with channels, principals, classification. Supports overlap
detection, intersection (for save_insight), narrower-than checks (for
edge scope inheritance), and legacy source_channels conversion."
```

---

## Task 7: Scope Column on Nodes and Edges

**Files:**
- Modify: `images/knowledge/store.py`
- Test: `images/tests/test_scope_model.py`

- [ ] **Step 1: Write failing tests for scope on store**

Append to `images/tests/test_scope_model.py`:

```python
import tempfile
from store import KnowledgeStore


@pytest.fixture
def store(tmp_path):
    db_path = str(tmp_path / "test.db")
    return KnowledgeStore(db_path)


def test_node_has_scope_column(store):
    """Nodes table must have a scope column."""
    cursor = store._db.execute("PRAGMA table_info(nodes)")
    columns = {row[1] for row in cursor.fetchall()}
    assert "scope" in columns


def test_edge_has_scope_column(store):
    """Edges table must have a scope column."""
    cursor = store._db.execute("PRAGMA table_info(edges)")
    columns = {row[1] for row in cursor.fetchall()}
    assert "scope" in columns


def test_add_node_with_scope(store):
    """Nodes can be created with a scope dict."""
    import json
    scope_dict = {"channels": ["ch-1"], "principals": ["operator:uuid-1"]}
    nid = store.add_node("test-node", "fact", "test", scope=scope_dict)
    row = store._db.execute("SELECT scope FROM nodes WHERE id = ?", (nid,)).fetchone()
    stored = json.loads(row[0])
    assert stored["channels"] == ["ch-1"]
    assert stored["principals"] == ["operator:uuid-1"]


def test_add_node_scope_defaults_from_source_channels(store):
    """When scope is not provided, it defaults from source_channels."""
    import json
    nid = store.add_node("legacy-node", "fact", "test",
                         source_channels=["alpha", "beta"])
    row = store._db.execute("SELECT scope FROM nodes WHERE id = ?", (nid,)).fetchone()
    stored = json.loads(row[0])
    assert "alpha" in stored["channels"]
    assert "beta" in stored["channels"]


def test_find_nodes_filters_by_principal_scope(store):
    """find_nodes() with principal param filters by scope overlap."""
    import json
    scope_a = json.dumps({"channels": [], "principals": ["operator:uuid-a"]})
    scope_b = json.dumps({"channels": [], "principals": ["operator:uuid-b"]})

    store.add_node("visible-node", "fact", "visible to operator A", scope={"principals": ["operator:uuid-a"]})
    store.add_node("hidden-node", "fact", "visible to operator B only", scope={"principals": ["operator:uuid-b"]})

    results = store.find_nodes("node", principal="operator:uuid-a")
    labels = [r["label"] for r in results]
    assert "visible-node" in labels
    assert "hidden-node" not in labels
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_scope_model.py::test_node_has_scope_column images/tests/test_scope_model.py::test_add_node_with_scope images/tests/test_scope_model.py::test_find_nodes_filters_by_principal_scope -v`
Expected: FAIL — `scope` column doesn't exist, `add_node()` doesn't accept `scope`, `find_nodes()` doesn't accept `principal`

- [ ] **Step 3: Add scope columns to schema**

In `images/knowledge/store.py`, in `__init__`, after the existing ALTER TABLE statements, add:

```python
        # Add scope column to nodes and edges
        for table in ("nodes", "edges"):
            try:
                self._db.execute(f"ALTER TABLE {table} ADD COLUMN scope TEXT DEFAULT '{{}}'")
            except Exception:
                pass  # Column already exists
```

- [ ] **Step 4: Update add_node() to accept scope**

In `images/knowledge/store.py`, update `add_node()`:

1. Add `scope=None` parameter to the method signature.
2. When scope is provided as a dict, serialize to JSON and store.
3. When scope is not provided but source_channels is, create scope from channels:

```python
    def add_node(self, label, kind, summary="", properties=None,
                 source_type="rule", source_channels=None, scope=None):
        # ... existing dedup logic ...

        # Build scope from explicit scope or source_channels
        import json
        if scope is not None:
            scope_json = json.dumps(scope) if isinstance(scope, dict) else scope
        elif source_channels:
            scope_json = json.dumps({"channels": list(source_channels), "principals": []})
        else:
            scope_json = "{}"

        # Add scope_json to the INSERT and UPDATE statements
```

Add `scope` to the INSERT column list and the corresponding value in the params tuple. Also update the "already exists" merge branch to union scope channels.

- [ ] **Step 5: Update find_nodes() to accept principal**

In `images/knowledge/store.py`, update `find_nodes()`:

1. Add `principal=None` parameter.
2. After FTS5 + vector search and RRF merge, add a post-filter step:

```python
        # Post-filter by principal scope
        if principal:
            from scope import Scope
            query_scope = Scope(principals=[principal])
            results = [
                r for r in results
                if Scope.from_dict(json.loads(r.get("scope", "{}"))).overlaps(query_scope)
            ]
```

This is a post-filter approach. For large result sets a SQL-based approach would be better, but the current `find_nodes()` already post-filters by `visible_channels`, so this follows the established pattern.

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_scope_model.py -v`
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
cd /home/geoff/agency-workspace/agency
git add images/knowledge/store.py images/tests/test_scope_model.py
git commit -m "feat(knowledge): add scope column to nodes and edges

JSON scope field with channels, principals, classification.
Defaults from source_channels for backward compat. find_nodes()
gains principal param for scope-based filtering."
```

---

## Task 8: Scope Migration for Existing Nodes

**Files:**
- Modify: `images/knowledge/store.py`
- Test: `images/tests/test_scope_model.py`

- [ ] **Step 1: Write failing test for scope migration**

Append to `images/tests/test_scope_model.py`:

```python
def test_migrate_scope_from_source_channels(store):
    """Existing nodes should get scope populated from source_channels."""
    import json

    # Create a node the old way (source_channels but no scope)
    nid = store.add_node("old-node", "fact", "legacy",
                         source_channels=["alpha", "beta"])

    # Manually clear scope to simulate pre-migration state
    store._db.execute("UPDATE nodes SET scope = '{}' WHERE id = ?", (nid,))
    store._db.commit()

    stats = store.migrate_node_scopes()

    row = store._db.execute("SELECT scope FROM nodes WHERE id = ?", (nid,)).fetchone()
    scope = json.loads(row[0])
    assert "alpha" in scope["channels"]
    assert "beta" in scope["channels"]
    assert stats["migrated"] >= 1


def test_migrate_scope_is_idempotent(store):
    """Running scope migration twice doesn't duplicate data."""
    import json
    nid = store.add_node("idem-node", "fact", "test",
                         source_channels=["gamma"])
    # Clear scope
    store._db.execute("UPDATE nodes SET scope = '{}' WHERE id = ?", (nid,))
    store._db.commit()

    store.migrate_node_scopes()
    store.migrate_node_scopes()

    row = store._db.execute("SELECT scope FROM nodes WHERE id = ?", (nid,)).fetchone()
    scope = json.loads(row[0])
    assert scope["channels"].count("gamma") == 1
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_scope_model.py::test_migrate_scope_from_source_channels -v`
Expected: FAIL — `migrate_node_scopes` doesn't exist

- [ ] **Step 3: Implement migrate_node_scopes()**

Add to `images/knowledge/store.py`:

```python
    def migrate_node_scopes(self):
        """Migrate existing nodes: populate scope from source_channels.

        Only migrates nodes where scope is empty ('{}') and source_channels
        is non-empty.
        """
        import json
        migrated = 0

        rows = self._db.execute("""
            SELECT id, source_channels
            FROM nodes
            WHERE (scope IS NULL OR scope = '{}')
            AND source_channels IS NOT NULL
            AND source_channels != '[]'
            AND source_channels != ''
        """).fetchall()

        for node_id, sc_json in rows:
            try:
                channels = json.loads(sc_json) if sc_json else []
            except (json.JSONDecodeError, TypeError):
                channels = []
            if not channels:
                continue
            scope = json.dumps({"channels": channels, "principals": []})
            self._db.execute(
                "UPDATE nodes SET scope = ? WHERE id = ?",
                (scope, node_id)
            )
            migrated += 1

        self._db.commit()
        return {"migrated": migrated}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_scope_model.py -v`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
cd /home/geoff/agency-workspace/agency
git add images/knowledge/store.py images/tests/test_scope_model.py
git commit -m "feat(knowledge): add scope migration from source_channels

Populates scope.channels from existing source_channels JSON arrays.
Idempotent — skips nodes that already have scope populated."
```

---

## Task 9: Performance Benchmarks in Curator Health Metrics

**Files:**
- Modify: `images/knowledge/curator.py`
- Test: `images/tests/test_edge_provenance.py`

- [ ] **Step 1: Write failing test for benchmark metrics**

Append to `images/tests/test_edge_provenance.py`:

```python
def test_health_metrics_include_benchmarks(store):
    """Health metrics should include performance benchmark fields."""
    from curator import Curator

    curator = Curator(store, mode="active")
    metrics = curator.compute_health_metrics()

    # New benchmark fields from the spec
    assert "graph_size" in metrics
    assert "traversal_p95_ms" in metrics
    assert "scope_resolution_ms" in metrics
    # community_detection_ms will be added in Phase 3
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_edge_provenance.py::test_health_metrics_include_benchmarks -v`
Expected: FAIL — metrics don't include the new fields

- [ ] **Step 3: Add benchmark metrics to compute_health_metrics()**

In `images/knowledge/curator.py`, find the `compute_health_metrics` method. Add the new benchmark fields:

```python
    def compute_health_metrics(self):
        # ... existing metric computation ...
        import time

        # Graph size
        node_count = self.store._db.execute("SELECT COUNT(*) FROM nodes WHERE curation_status IS NULL OR curation_status = 'flagged'").fetchone()[0]
        edge_count = self.store._db.execute("SELECT COUNT(*) FROM edges").fetchone()[0]

        # Traversal benchmark — sample a multi-hop query
        traversal_ms = 0.0
        sample_node = self.store._db.execute("SELECT id FROM nodes LIMIT 1").fetchone()
        if sample_node:
            t0 = time.monotonic()
            self.store.get_subgraph(sample_node[0], max_hops=2)
            traversal_ms = (time.monotonic() - t0) * 1000

        # Scope resolution benchmark — sample find_nodes with principal
        scope_ms = 0.0
        t0 = time.monotonic()
        self.store.find_nodes("test", principal="benchmark:noop", limit=5)
        scope_ms = (time.monotonic() - t0) * 1000

        # Add to metrics dict
        metrics = {
            # ... existing fields ...
            "graph_size": node_count + edge_count,
            "traversal_p95_ms": round(traversal_ms, 2),
            "scope_resolution_ms": round(scope_ms, 2),
        }
```

Integrate these new keys into the existing metrics dict that `compute_health_metrics` returns (don't replace the existing fields — merge into the existing dict).

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_edge_provenance.py::test_health_metrics_include_benchmarks -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /home/geoff/agency-workspace/agency
git add images/knowledge/curator.py images/tests/test_edge_provenance.py
git commit -m "feat(knowledge): add performance benchmarks to curator health metrics

graph_size, traversal_p95_ms, scope_resolution_ms tracked per cycle.
community_detection_ms will be added in Phase 3."
```

---

## Task 10: Register PrincipalRegistry in Knowledge Server

**Files:**
- Modify: `images/knowledge/server.py`

- [ ] **Step 1: Initialize PrincipalRegistry in create_app()**

In `images/knowledge/server.py`, find the `create_app()` function. After the `KnowledgeStore` is created, add:

```python
    from principal_registry import PrincipalRegistry
    principal_registry = PrincipalRegistry(store._db)
    app["principal_registry"] = principal_registry
```

- [ ] **Step 2: Add principal registry endpoints**

In the same file, add endpoint handlers and register routes:

```python
async def handle_principals_list(request):
    """GET /principals — list all principals, optionally filtered by type."""
    registry = request.app["principal_registry"]
    ptype = request.query.get("type")
    if ptype:
        principals = registry.list_by_type(ptype)
    else:
        principals = registry.list_all()
    return web.json_response({"principals": principals})


async def handle_principals_register(request):
    """POST /principals — register a new principal."""
    registry = request.app["principal_registry"]
    body = await request.json()
    ptype = body.get("type")
    name = body.get("name")
    metadata = body.get("metadata")
    if not ptype or not name:
        return web.json_response({"error": "type and name required"}, status=400)
    try:
        uuid = registry.register(ptype, name, metadata=metadata)
    except ValueError as e:
        return web.json_response({"error": str(e)}, status=400)
    return web.json_response({"uuid": uuid, "type": ptype, "name": name})


async def handle_principals_resolve(request):
    """GET /principals/{uuid} — resolve a principal UUID."""
    registry = request.app["principal_registry"]
    uuid = request.match_info["uuid"]
    result = registry.resolve(uuid)
    if not result:
        return web.json_response({"error": "not found"}, status=404)
    return web.json_response(result)
```

Register the routes in `create_app()` alongside existing routes:

```python
    app.router.add_get("/principals", handle_principals_list)
    app.router.add_post("/principals", handle_principals_register)
    app.router.add_get("/principals/{uuid}", handle_principals_resolve)
```

- [ ] **Step 3: Run startup migration on server start**

In `server.py`, find the startup hooks (e.g., `on_startup` handlers). Add migration calls:

```python
async def _run_migrations(app):
    """Run schema migrations on startup."""
    store = app["store"]
    # Provenance migration
    stats = store.migrate_edge_provenance()
    if stats["migrated"] > 0:
        logger.info("Migrated %d edges to provenance tiers", stats["migrated"])
    # Scope migration
    stats = store.migrate_node_scopes()
    if stats["migrated"] > 0:
        logger.info("Migrated %d node scopes from source_channels", stats["migrated"])

# In create_app():
app.on_startup.append(_run_migrations)
```

- [ ] **Step 4: Verify server starts without errors**

Run: `cd /home/geoff/agency-workspace/agency && python -c "import sys; sys.path.insert(0, 'images/knowledge'); from server import create_app; print('OK')"`
Expected: `OK` (no import errors)

- [ ] **Step 5: Commit**

```bash
cd /home/geoff/agency-workspace/agency
git add images/knowledge/server.py
git commit -m "feat(knowledge): register principal registry and migration hooks

Principal CRUD endpoints at /principals. Provenance and scope
migrations run automatically on server startup."
```

---

## Task 11: Go Gateway — Principal Registry Proxy

**Files:**
- Modify: `internal/knowledge/proxy.go`
- Modify: `internal/api/routes.go`
- Modify: `internal/api/handlers_admin.go`
- Modify: `internal/apiclient/client.go`
- Modify: `internal/cli/commands.go`

- [ ] **Step 1: Add proxy methods for principal registry**

In `internal/knowledge/proxy.go`, add:

```go
// Principals lists all principals, optionally filtered by type.
func (p *Proxy) Principals(ctx context.Context, principalType string) (json.RawMessage, error) {
	path := "/principals"
	if principalType != "" {
		path += "?type=" + url.QueryEscape(principalType)
	}
	return p.get(ctx, path)
}

// RegisterPrincipal registers a new principal and returns its UUID.
func (p *Proxy) RegisterPrincipal(ctx context.Context, principalType, name string) (json.RawMessage, error) {
	body := map[string]string{"type": principalType, "name": name}
	b, _ := json.Marshal(body)
	return p.post(ctx, "/principals", b)
}

// ResolvePrincipal resolves a UUID to principal details.
func (p *Proxy) ResolvePrincipal(ctx context.Context, uuid string) (json.RawMessage, error) {
	return p.get(ctx, "/principals/"+url.PathEscape(uuid))
}
```

- [ ] **Step 2: Add gateway API routes**

In `internal/api/routes.go`, in the knowledge route group, add:

```go
r.Get("/knowledge/principals", h.knowledgePrincipalsList)
r.Post("/knowledge/principals", h.knowledgePrincipalsRegister)
r.Get("/knowledge/principals/{uuid}", h.knowledgePrincipalsResolve)
```

- [ ] **Step 3: Add handler implementations**

In `internal/api/handlers_hub.go` (or a new `handlers_knowledge_principals.go` if preferred — follow existing pattern), add:

```go
func (h *handler) knowledgePrincipalsList(w http.ResponseWriter, r *http.Request) {
	ptype := r.URL.Query().Get("type")
	data, err := h.knowledge.Principals(r.Context(), ptype)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (h *handler) knowledgePrincipalsRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	data, err := h.knowledge.RegisterPrincipal(r.Context(), body.Type, body.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (h *handler) knowledgePrincipalsResolve(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	data, err := h.knowledge.ResolvePrincipal(r.Context(), uuid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
```

- [ ] **Step 4: Add API client methods**

In `internal/apiclient/client.go`, add:

```go
func (c *Client) KnowledgePrincipals(principalType string) ([]byte, error) {
	path := "/api/v1/knowledge/principals"
	if principalType != "" {
		path += "?type=" + url.QueryEscape(principalType)
	}
	return c.get(path)
}

func (c *Client) KnowledgeRegisterPrincipal(principalType, name string) ([]byte, error) {
	body := map[string]string{"type": principalType, "name": name}
	b, _ := json.Marshal(body)
	return c.post("/api/v1/knowledge/principals", b)
}
```

- [ ] **Step 5: Add CLI command**

In `internal/cli/commands.go`, add a `principals` subcommand under the `knowledge` command:

```go
func knowledgePrincipalsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "principals",
		Short: "Manage principal registry",
	}

	listCmd := &cobra.Command{
		Use:   "list [--type operator|agent|team|role|channel]",
		Short: "List registered principals",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := clientFromContext(cmd)
			ptype, _ := cmd.Flags().GetString("type")
			data, err := c.KnowledgePrincipals(ptype)
			if err != nil {
				return err
			}
			return printOutput(cmd, data)
		},
	}
	listCmd.Flags().String("type", "", "Filter by principal type")

	registerCmd := &cobra.Command{
		Use:   "register <type> <name>",
		Short: "Register a new principal",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := clientFromContext(cmd)
			data, err := c.KnowledgeRegisterPrincipal(args[0], args[1])
			if err != nil {
				return err
			}
			return printOutput(cmd, data)
		},
	}

	cmd.AddCommand(listCmd, registerCmd)
	return cmd
}
```

Then add `knowledgePrincipalsCmd()` to the `knowledgeCmd()` subcommands list.

- [ ] **Step 6: Verify Go builds**

Run: `cd /home/geoff/agency-workspace/agency && go build ./cmd/gateway/`
Expected: Build succeeds with no errors

- [ ] **Step 7: Commit**

```bash
cd /home/geoff/agency-workspace/agency
git add internal/knowledge/proxy.go internal/api/routes.go internal/api/handlers_hub.go internal/apiclient/client.go internal/cli/commands.go
git commit -m "feat(gateway): add principal registry proxy, routes, and CLI

GET/POST /api/v1/knowledge/principals for listing and registering.
GET /api/v1/knowledge/principals/{uuid} for resolution.
CLI: agency knowledge principals list/register."
```

---

## Task 12: Run Full Test Suite

**Files:** None (validation only)

- [ ] **Step 1: Run Python knowledge tests**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/test_edge_provenance.py images/tests/test_principal_registry.py images/tests/test_scope_model.py -v`
Expected: All tests PASS

- [ ] **Step 2: Run existing knowledge tests to check for regressions**

Run: `cd /home/geoff/agency-workspace/agency && python -m pytest images/tests/ -k knowledge -v --timeout=30`
Expected: All existing tests PASS (the new `provenance` param defaults to `AMBIGUOUS`, so existing `add_edge()` calls are unaffected)

- [ ] **Step 3: Run Go tests**

Run: `cd /home/geoff/agency-workspace/agency && go test ./internal/knowledge/ ./internal/api/ -v -count=1`
Expected: All Go tests PASS

- [ ] **Step 4: Build gateway binary**

Run: `cd /home/geoff/agency-workspace/agency && go build -o /dev/null ./cmd/gateway/`
Expected: Build succeeds

- [ ] **Step 5: Final commit if any fixes were needed**

If any test fixes were needed, commit them:

```bash
cd /home/geoff/agency-workspace/agency
git add -A
git commit -m "fix: address test regressions from Phase 1 schema changes"
```

---

## Summary

Phase 1 delivers the schema foundation for the full Knowledge Graph Intelligence spec:

| Component | What it adds |
|-----------|-------------|
| Edge provenance | `EXTRACTED`/`INFERRED`/`AMBIGUOUS` column on edges, migration from source_type, min_provenance filtering |
| Principal registry | UUID-based identity for all principals, CRUD API, legacy name resolution |
| Scope model | `Scope` dataclass with overlap/intersection/narrower-than, JSON column on nodes/edges |
| Scope migration | Automatic population from existing source_channels |
| Performance benchmarks | graph_size, traversal_p95_ms, scope_resolution_ms in curator health |
| Gateway integration | Proxy methods, REST routes, CLI commands for principal registry |

**Deferred to Phase 1b (follow-up plan):**
- Scope enforcement in `get_subgraph()` and `get_neighbors()` (traversal stops at scope boundaries)
- Scope enforcement in `get_context()` and `find_path()`
- Edge scope inheritance validation (edge scope cannot be wider than source node scope)
- GraphRAG retrieval weighting by provenance tier
- Body runtime `query_graph` patterns updated with `min_provenance` param

These are scope-aware traversal features that build on the schema foundation delivered here but require careful integration testing with the full graph API surface.

**Next:** Phase 2 (Universal Ingestion Pipeline), Phase 3 (Graph Intelligence), and Phase 4 (Feedback Loop) each get their own plan, built on this foundation.
