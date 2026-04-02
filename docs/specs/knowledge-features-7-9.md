---
description: "---"
status: "Approved"
---

# Knowledge Features 7-9: Ontology Emergence, Hybrid Retrieval, Asset Ontology

**Date:** 2026-03-28
**Status:** Approved

---

## Feature 7: Ontology Emergence Tracking

### Summary

Add emergence detection to the curator's periodic cycle. Scans the graph for novel kind and relation values that appear frequently enough to be candidates for ontology promotion.

### Emergence Scan

New method `Curator.emergence_scan()` called at the end of `CurationLoop._run_cycle()` after `compute_health_metrics()`. Runs in both active and observe modes — writing OntologyCandidate nodes is safe (no mutations to existing nodes).

**Kind candidates query** (note: `source_channels` is a JSON array, must unnest with `json_each`):
```sql
SELECT kind, COUNT(*) as cnt, COUNT(DISTINCT jc.value) as src_cnt
FROM nodes, json_each(nodes.source_channels) as jc
WHERE kind != 'OntologyCandidate'
GROUP BY kind
HAVING cnt >= :node_threshold AND src_cnt >= :min_sources
```

**Relation candidates query** (note: `source_channel` on edges is a plain string — no unnesting needed):
```sql
SELECT relation, COUNT(*) as cnt, COUNT(DISTINCT source_channel) as src_cnt
FROM edges
GROUP BY relation
HAVING cnt >= :edge_threshold AND src_cnt >= :min_sources
```

Filter out values already present in the loaded ontology (`self._ontology.entity_types` / `self._ontology.relationship_types`). If no ontology is loaded, all values are candidates.

### Thresholds (env vars)

| Variable | Default | Purpose |
|----------|---------|---------|
| `ONTOLOGY_CANDIDATE_NODE_THRESHOLD` | 10 | Min node count for kind candidate |
| `ONTOLOGY_CANDIDATE_EDGE_THRESHOLD` | 10 | Min edge count for relation candidate |
| `ONTOLOGY_CANDIDATE_MIN_SOURCES` | 3 | Min distinct sources for either |

### OntologyCandidate Node Structure

```yaml
kind: OntologyCandidate
label: "candidate:<value>"
source_type: rule
properties:
  candidate_type: "kind" | "relation"
  value: "<the kind or relation string>"
  occurrence_count: 47
  source_count: 8
  example_labels: ["macbook-pro", "raspberry-pi", "ubiquiti-switch"]  # up to 5
  first_seen: "<ISO timestamp>"
  last_updated: "<ISO timestamp>"
  status: "candidate"  # candidate | promoted | rejected
  rejection_count_at: null  # occurrence count at rejection time, for re-surface logic
```

Upserted via `store.add_node()` — deduplication on `(label, kind)` is natural. Each cycle updates `occurrence_count`, `source_count`, `example_labels`, `last_updated` for existing candidates.

**Rejection re-surface:** A rejected candidate is not re-surfaced unless `occurrence_count > 2 * rejection_count_at`.

### Exclusions

OntologyCandidate nodes are excluded from:
- `find_nodes()` — add `WHERE kind != 'OntologyCandidate'` to the FTS query
- Orphan pruner — add `'OntologyCandidate'` to the structural exemption set alongside `{agent, channel, task}`
- Synthesizer extraction batches — skip nodes with `kind='OntologyCandidate'`
- Embedding (Feature 8) — not in the embeddable kinds list

### CLI Commands

Added to the existing `agency admin knowledge` group:

| Command | Action |
|---------|--------|
| `agency admin knowledge ontology candidates` | Lists all OntologyCandidate nodes with status=candidate. Columns: value, type, occurrences, sources, first_seen, examples |
| `agency admin knowledge ontology promote <value>` | Sets status=promoted. Writes type to base ontology YAML. Bumps version. Sends SIGHUP to knowledge container. Prints confirmation. |
| `agency admin knowledge ontology reject <value>` | Sets status=rejected. Records `rejection_count_at` = current occurrence_count. Will not re-surface until occurrences double. |

### MCP Tool Actions

Three new actions on `agency_admin_knowledge` MCP tool:
- `ontology_candidates` — returns list of candidate nodes
- `ontology_promote` (required: `value`) — promotes candidate to ontology
- `ontology_reject` (required: `value`) — rejects candidate

### Knowledge Service Endpoints

- `GET /ontology/candidates` — returns `{candidates: [...]}`
- `POST /ontology/promote` — body: `{value: "device"}` — returns `{promoted: true, version: N}`
- `POST /ontology/reject` — body: `{value: "device"}` — returns `{rejected: true}`

### Promote Flow (atomic)

1. Read `~/.agency/knowledge/base-ontology.yaml`
2. Add entity type (for kind candidates) or relationship type (for relation candidates) with auto-generated description
3. Bump version integer
4. Add changelog entry
5. Write file atomically (write to temp, rename)
6. Update candidate node status=promoted
7. Write curation_log entry: `action="ontology_promote", detail={value, old_version, new_version}` (ASK Tenet 7 — constraint history must be complete)
8. Send SIGHUP to knowledge container for hot-reload

If any step fails, no changes are persisted.

### Reject Flow

1. Set candidate node status=rejected, `rejection_count_at` = current occurrence_count
2. Write curation_log entry: `action="ontology_reject", detail={value, occurrence_count}`

Both promote and reject write to the append-only curation_log so the full ontology decision history is queryable.

### Tests

- Emergence scan finds novel kinds above threshold
- Emergence scan ignores kinds already in ontology
- OntologyCandidate nodes upserted (not duplicated) across cycles
- Rejected candidates not re-surfaced until occurrence doubles
- `find_nodes()` excludes OntologyCandidate results
- Orphan pruner skips OntologyCandidate nodes
- Promote writes to ontology file and bumps version
- Reject sets status and records rejection_count_at
- `json_each()` correctly counts distinct channels from JSON arrays

---

## Feature 8: Hybrid Retrieval — sqlite-vec + Embedding Providers

### Summary

Add semantic vector search alongside FTS5 using sqlite-vec for storage and a pluggable embedding provider abstraction. Default to NoOpProvider (no embeddings). Build an evaluation harness to compare providers before committing.

### Provider Abstraction

```python
# images/knowledge/embedding.py

class EmbeddingProvider:
    """Abstract base for embedding providers."""
    def embed(self, text: str) -> list[float]: ...
    def embed_batch(self, texts: list[str]) -> list[list[float]]: ...
    @property
    def dimensions(self) -> int: ...
    @property
    def name(self) -> str: ...

class NoOpProvider(EmbeddingProvider):
    """Returns empty vectors. Used when embeddings disabled."""
    # dimensions = 0, embed returns []

class OllamaProvider(EmbeddingProvider):
    """Local Ollama embeddings via /api/embed endpoint."""
    # Endpoint: POST http://agency-infra-embeddings:11434/api/embed
    # Uses /api/embed (NOT /v1/embeddings — unreliable across versions)
    # Batch: pass input as array, response["embeddings"] is array of vectors
    # Vectors are L2-normalized by Ollama — cosine = dot product
    # Model configurable: KNOWLEDGE_EMBED_OLLAMA_MODEL (no default — must be set)
    # Dimension fetched from model info endpoint at init, not hardcoded

class OpenAIProvider(EmbeddingProvider):
    """OpenAI embeddings via egress proxy."""
    # Endpoint: POST https://api.openai.com/v1/embeddings via egress
    # Model: text-embedding-3-small (1536 dims)
    # Auth: egress credential swap for openai service grant
    # Normalize explicitly if not pre-normalized
    # MUST route through egress proxy (HTTPS_PROXY env var) for credential
    # swap — knowledge container never holds real API keys (ASK Tenet 3/4)

class VoyageProvider(EmbeddingProvider):
    """Voyage AI embeddings via egress proxy."""
    # Endpoint: POST https://api.voyageai.com/v1/embeddings via egress
    # Models: voyage-3-lite (512 dims), voyage-3 (1024 dims)
    # Auth: VOYAGE_API_KEY via service grant
    # api.voyageai.com added to egress allowlist
    # Normalize explicitly if not pre-normalized
    # MUST route through egress proxy (HTTPS_PROXY env var) for credential
    # swap — knowledge container never holds real API keys (ASK Tenet 3/4)
```

### Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `KNOWLEDGE_EMBED_PROVIDER` | `none` | Provider: `none`, `ollama`, `openai`, `voyage` |
| `KNOWLEDGE_EMBED_OLLAMA_MODEL` | _(none)_ | Ollama model name (required when provider=ollama) |
| `KNOWLEDGE_EMBED_OPENAI_MODEL` | `text-embedding-3-small` | OpenAI model |
| `KNOWLEDGE_EMBED_VOYAGE_MODEL` | `voyage-3-lite` | Voyage model |
| `KNOWLEDGE_EMBED_KINDS` | `Software,ConfigItem,BehaviorPattern,Vulnerability,Finding,ThreatIndicator,HuntHypothesis` | Comma-separated kinds to embed |

**Provider init failure:** Fall back to NoOpProvider and log warning. Never crash.

### sqlite-vec Setup

In `KnowledgeStore.__init__()`:
```python
try:
    import sqlite_vec
    self._db.enable_load_extension(True)
    sqlite_vec.load(self._db)
    self._db.enable_load_extension(False)
    self._vec_available = True
except (ImportError, Exception) as e:
    logger.warning("sqlite-vec not available: %s — vector search disabled", e)
    self._vec_available = False
```

Vec0 table creation deferred to first use — created when provider dimensions are known:
```sql
CREATE VIRTUAL TABLE IF NOT EXISTS nodes_vec USING vec0(
    id TEXT PRIMARY KEY,
    embedding FLOAT[{dimensions}]
)
```

### Dimension Mismatch Handling

On store init, if `nodes_vec` exists, check its dimension against the configured provider. If they differ:
1. Log: `"Embedding provider changed: dimensions {old} → {new}. Recreating vec table. Backfill required."`
2. Drop `nodes_vec`
3. Recreate with new dimension
4. Set flag to trigger backfill

### Embeddable Kinds

Only embed nodes whose `kind` (case-insensitive) is in the `KNOWLEDGE_EMBED_KINDS` list. Skip all others including: `OntologyCandidate`, structural kinds (`agent`, `channel`, `task`).

### _generate_embedding()

Private method on KnowledgeStore:
```python
def _generate_embedding(self, node_id: str, kind: str, label: str, summary: str) -> None:
    # 1. Check kind is in embed list — return immediately if not
    # 2. Check provider is not NoOp — return if so
    # 3. Input text: f"{label}: {summary}" truncated to 512 tokens (~2048 chars)
    # 4. Call provider.embed(text)
    # 5. Upsert into nodes_vec
    # 6. On any failure: log warning, do not raise
```

Called from `add_node()` and `update_node()` after the SQLite write completes.

### Backfill Job

`KnowledgeStore.backfill_embeddings()`:
- Find all nodes of embeddable kinds with no entry in `nodes_vec`
- Generate embeddings in batches of 20
- 100ms sleep between batches
- Log progress: `"Backfilling embeddings: {N} nodes to process"`

Called from knowledge server startup in a background asyncio task, after the ingestion loop starts.

### Hybrid Retrieval in find_nodes()

When `nodes_vec` is populated and provider is not NoOp:

1. Run existing FTS5 query → up to `limit*2` candidates with FTS rank scores
2. Generate query embedding, run ANN search on `nodes_vec` for top `limit*2` candidates
3. Merge using Reciprocal Rank Fusion: `RRF(d) = 1/(k + rank_fts) + 1/(k + rank_vec)` where `k=60`
4. Return top `limit` results by RRF score, applying channel filter
5. Exclude `kind='OntologyCandidate'` from both FTS and ANN results

If `nodes_vec` empty or provider is NoOp: FTS5 only, no behavior change.

**`semantic_only` parameter:** When `True`, skip FTS5, return ANN results directly. For pure vector similarity search (e.g., behavior pattern matching).

### query_graph Extension

Fourth pattern on the existing `query_graph` tool:
```python
query_graph(pattern="find_similar", node_id="<id>", limit=10)
```

Gets the node's vector from `nodes_vec`, runs ANN search, returns most similar nodes. Returns empty list gracefully if vectors unavailable.

### Knowledge Container Dockerfile

Add to requirements.txt:
```
sqlite-vec>=0.1.0
```

### Tests

- sqlite-vec loads without error (or degrades gracefully)
- NoOpProvider returns empty vectors, dimensions=0
- OllamaProvider calls correct endpoint (`/api/embed`, not `/v1/embeddings`)
- Embedding generated on `add_node` for embeddable kind
- Embedding skipped for non-embeddable kind
- Embedding failure does not prevent node write
- Dimension mismatch triggers table recreation
- Hybrid retrieval returns merged RRF results
- Fallback to FTS5 when nodes_vec empty or provider is NoOp
- `find_similar` pattern returns nearest neighbors
- `find_similar` returns empty list when vectors unavailable
- Backfill processes nodes missing vectors
- `find_nodes()` excludes OntologyCandidate from both FTS and ANN
- All tests pass with NoOpProvider (default)

---

## Evaluation Harness: tools/eval_embeddings.py

### Summary

Standalone script — no Agency runtime imports. Tests provider/model combinations against a fixed asset intelligence corpus to compare embedding quality, latency, and cost.

### CLI Interface

```
python tools/eval_embeddings.py --provider ollama --model embeddinggemma
python tools/eval_embeddings.py --provider openai --model text-embedding-3-small
python tools/eval_embeddings.py --provider voyage --model voyage-3-lite
python tools/eval_embeddings.py --all   # all combinations sequentially
```

**Pre-flight checks:**
- Ollama: query `GET http://localhost:11434/api/tags`, verify model is listed. If not: print error with `ollama pull <model>` instruction, exit non-zero. Do NOT auto-pull.
- OpenAI: check `OPENAI_API_KEY` env var
- Voyage: check `VOYAGE_API_KEY` env var

Ollama endpoint: `localhost:11434` (local dev, not container address).

### Evaluation Corpus — 25 Nodes

**Software (5):**
- "nginx 1.24.0": "web server, internet-facing, running on prod-web"
- "openssh 8.9": "SSH daemon, accepts password auth, running on all hosts"
- "python 3.11.2": "runtime, installed in /usr/local, multiple apps depend on it"
- "log4j 2.14.1": "java logging library, known RCE vulnerability CVE-2021-44228"
- "docker 24.0.5": "container runtime, socket exposed to non-root users"

**ConfigItem (5):**
- "sshd PasswordAuthentication": "value=yes, service=sshd, asset=jump-host"
- "ufw default-incoming": "value=allow, service=firewall, asset=web-server"
- "nginx worker_processes": "value=auto, service=nginx, asset=prod-web"
- "postgres max_connections": "value=100, service=postgresql, asset=db-primary"
- "fail2ban maxretry": "value=10, service=fail2ban, asset=jump-host"

**BehaviorPattern (5):**
- "outbound-beacon": "device makes periodic outbound connections to same IP every 60s"
- "lateral-movement": "process spawns new process that connects to internal host"
- "dns-exfil": "high volume of DNS queries with long subdomain strings"
- "port-scan": "single source IP attempts connections to 20+ ports in under 5s"
- "privilege-escalation": "process running as www-data spawns shell as root"

**Vulnerability (5):**
- "CVE-2021-44228": "critical RCE in log4j via JNDI lookup, affects log4j < 2.15"
- "CVE-2023-44487": "HTTP/2 rapid reset attack, affects nginx and apache"
- "CVE-2021-3156": "sudo heap overflow allows local privilege escalation"
- "weak-ssh-config": "password authentication enabled on internet-facing host"
- "exposed-docker-socket": "docker socket readable by non-root, container escape risk"

**Finding (5):**
- "prod-web nginx finding": "prod-web has nginx 1.24 which is affected by CVE-2023-44487, internet-facing"
- "jump-host ssh finding": "jump-host allows password SSH auth, fail2ban threshold too high for brute force"
- "db-primary postgres finding": "db-primary postgres connection limit may cause denial of service under load"
- "build-server log4j finding": "unpatched log4j 2.14.1 found on build-server, used by CI pipeline"
- "dev-host docker finding": "docker socket exposure on dev-host creates container escape path to host"

Embedding input format: `"{label}: {summary}"`

### Evaluation Queries — 15

**Structural (5)** — keyword match should work:
1. "nginx vulnerability" → expects: CVE-2023-44487, prod-web nginx finding
2. "SSH password authentication" → expects: openssh 8.9, weak-ssh-config
3. "log4j RCE" → expects: log4j 2.14.1, CVE-2021-44228
4. "docker socket exposed" → expects: docker 24.0.5, dev-host docker finding
5. "sudo privilege escalation" → expects: CVE-2021-3156, privilege-escalation

**Semantic (5)** — requires understanding beyond keywords:
6. "web server with known exploitable flaw" → expects: nginx 1.24.0, CVE-2023-44487, prod-web nginx finding
7. "process that spawns unexpected children" → expects: lateral-movement, privilege-escalation
8. "periodic outbound network activity" → expects: outbound-beacon
9. "authentication weakness on exposed host" → expects: weak-ssh-config, jump-host ssh finding
10. "java application with critical vulnerability" → expects: log4j 2.14.1, CVE-2021-44228, build-server log4j finding

**Cross-domain (5)** — requires connecting across node types:
11. "what makes prod-web risky" → expects: prod-web nginx finding, CVE-2023-44487, nginx 1.24.0
12. "container security issues" → expects: docker 24.0.5, dev-host docker finding, exposed-docker-socket
13. "database availability risk" → expects: postgres max_connections, db-primary postgres finding
14. "build pipeline security" → expects: log4j 2.14.1, build-server log4j finding, CVE-2021-44228
15. "host with multiple security problems" → expects: jump-host ssh finding, openssh 8.9, weak-ssh-config

### Metrics

Per query: retrieve top-5 by cosine similarity (dot product for pre-normalized, explicit cosine otherwise).

- **Precision@3**: fraction of top-3 matching expected labels
- **Precision@5**: fraction of top-5 matching expected labels
- **MRR**: mean reciprocal rank of first expected result

Aggregated per provider/model:
- Mean P@3, P@5, MRR across all 15 queries
- Same metrics broken out for structural / semantic / cross-domain subsets
- Latency: avg ms per single embed, avg ms per batch of 25
- Cost per 1000 nodes: published rates, $0.00 for Ollama

### Output

Comparison table:
```
Provider    Model                 Dims  P@3   P@5   MRR   Lat(ms)  $/1k
ollama      embeddinggemma        NNNN  0.xx  0.xx  0.xx  xx       $0.00
ollama      qwen3-embedding       NNNN  0.xx  0.xx  0.xx  xx       $0.00
ollama      all-minilm            384   0.xx  0.xx  0.xx  xx       $0.00
openai      text-embedding-3-sm   1536  0.xx  0.xx  0.xx  xx       $x.xx
voyage      voyage-3-lite         512   0.xx  0.xx  0.xx  xx       $x.xx
voyage      voyage-3              1024  0.xx  0.xx  0.xx  xx       $x.xx
```

Per-subset breakdown for best and worst MRR performers.

Save full results to `eval_results_<timestamp>.json`.

**Dependencies:** httpx, numpy. No Agency runtime imports.

---

## Feature 9: Asset Inventory Entity Types in Base Ontology

### Summary

Add three entity types and six relationship types to the base ontology. These are first-class types required by the embedding selection logic in Feature 8 and will be written by connectors from day one.

### Entity Types

```yaml
software:
  description: >
    An application, library, package, or firmware installed on an asset.
    Use for anything with a name and version that runs on or is part of a
    device. Attributes: name, version, vendor, install_date, install_path, hash.
  attributes: [name, version, vendor, install_date, install_path, hash]

config_item:
  description: >
    A configuration setting, parameter, or policy applied to an asset or
    service. Use for key-value configuration state that is worth tracking
    over time. Attributes: key, value, asset, service, last_changed, changed_by.
  attributes: [key, value, asset, service, last_changed, changed_by]

behavior_pattern:
  description: >
    An observed behavioral pattern on an asset or between assets — a
    recurring sequence of actions, communications, or resource usage that
    is worth tracking. May be normal (baseline) or anomalous.
    Attributes: description, asset, first_seen, last_seen, frequency,
    anomaly_score, baseline.
  attributes: [description, asset, first_seen, last_seen, frequency, anomaly_score, baseline]
```

### Relationship Types

```yaml
has_software:
  description: Asset or system has software installed
  inverse: installed_on

has_config:
  description: Asset or service has a configuration item applied
  inverse: config_of

exhibited:
  description: Asset exhibited or displayed a behavior pattern
  inverse: exhibited_by

similar_to:
  description: Structurally or behaviorally similar to (used for pattern matching)
  inverse: similar_to

preceded:
  description: This pattern or event preceded another event or incident
  inverse: preceded_by

communicates_with:
  description: Asset communicates with another asset over the network
  inverse: communicates_with
```

### Where Changes Go

1. **Go gateway** (`agency-gateway/internal/knowledge/ontology.go`) — Add types to the `DefaultEntityTypes` and `DefaultRelationshipTypes` maps (or equivalent embedded definition). Bump version. Add changelog entry.

2. **Base ontology file** (`~/.agency/knowledge/base-ontology.yaml`) — Written by `agency setup`. Will include new types automatically from the Go definition.

3. **Python `_validate_kind()` aliases** (`images/body/knowledge_tools.py`) — Add aliases: `app` → `software`, `package` → `software`, `library` → `software`, `config` → `config_item`, `setting` → `config_item`, `behavior` → `behavior_pattern`, `pattern` → `behavior_pattern`.

4. **Synthesizer `_validate_kind()`** (`images/knowledge/synthesizer.py`) — Same alias additions for extraction validation.

5. **`KNOWLEDGE_EMBED_KINDS` default** (Feature 8) — `Software,ConfigItem,BehaviorPattern` already in the default list. Verify casing matches.

### Tests

- Base ontology loads and includes the three new entity types
- Base ontology loads and includes the six new relationship types
- `_validate_kind()` accepts "software", "config_item", "behavior_pattern" without falling back to "fact"
- `_validate_kind()` resolves aliases: "app" → "software", "config" → "config_item", "behavior" → "behavior_pattern"
- Embedding generated for nodes with these kinds (integration with Feature 8, when provider is active)
- Ontology version is bumped and changelog entry present

---

## Cross-Cutting Constraints

- **No broken tests.** Each feature's tests pass before starting the next. All tests pass with NoOpProvider (default).
- **Idempotent emergence scan.** Running twice produces same candidates, not duplicates.
- **Atomic promote.** Either ontology file updated + version bumped, or nothing changes.
- **Best-effort embeddings.** Failures never block node writes.
- **Graceful sqlite-vec degradation.** Missing sqlite-vec degrades to FTS5-only, never crashes.
- **OntologyCandidate exclusion.** Never in agent-facing `find_nodes()` results.
- **Embeddings container.** Embeddings go through the `agency-infra-embeddings` Ollama container on the mediation network, or remote providers via the egress proxy.
- **Eval harness is manual.** Not part of the automated test suite or build.
