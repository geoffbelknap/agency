*A strategy for multi-agent systems that get smarter and cheaper over time.*

## Thesis

An agent organization should compound capability the way a human organization does — each task completed makes the next one faster, cheaper, and better informed. Naive multi-agent systems don't do this. They scale compute linearly with agent count, start every task cold, and throw away knowledge after each session. The result is a system that gets more expensive but not smarter.

Agency is designed around the opposite principle: the system should improve with use. Agents build shared organizational knowledge as a byproduct of doing their work. That knowledge feeds back into every subsequent task. Administrative overhead — curation, routing, synthesis — is progressively delegated to cheaper resources. Over time, the system converges toward higher capability at lower marginal cost.

This document describes the strategy, the mechanisms, and the principles behind that design.

---

## 1. The Problem

Multi-agent systems have a cost curve problem. It's not linear — it's superlinear:

- **Briefing multiplication.** Every message sent to a channel is potentially read by multiple agents. A team of five agents doesn't use 5x the tokens of one — it uses significantly more, because agents read each other's output. (Section 5 describes the mitigations: channel discipline and targeted reads.)
- **Cold starts.** Without shared memory, every agent begins every task from zero. The third time an agent encounters the same codebase, it still needs the same context window to understand it.
- **Knowledge evaporation.** Findings from one task don't survive to the next. An agent that spent 10,000 tokens discovering a vulnerability doesn't make it cheaper for the next agent to find a related one.
- **Coordinator overhead.** Task decomposition creates N subtasks from one request, each with its own LLM calls. Bad decomposition amplifies cost without proportional value.
- **Retry loops.** When agents are routed to models too weak for their task, they retry, clarify, or produce low-quality output that requires rework. The cost of a bad routing decision compounds.

In a naive system, doubling the number of agents more than doubles the cost, while capability improves sublinearly. This is the wrong curve.

## 2. Compounding Intelligence

The knowledge graph is the mechanism that reverses this curve.

**How it works.** Agency maintains a shared knowledge graph — a structured store of entities, relationships, and facts that agents have learned over time. It's implemented as a SQLite graph database with FTS5 full-text search, adjacency tables for nodes and edges, and free-form entity and relationship types.

When an agent starts a task, the system automatically retrieves relevant prior knowledge from the graph and injects it as a briefing block — a preface to the task content that provides context the agent didn't have to discover itself. This is GraphRAG: retrieval-augmented generation over a graph, not a flat document store. The agent receives not just matching facts but their connections — the subgraph around the relevant nodes.

**Where it sits in the cognitive model.** In ASK's Mind/Body/Workspace architecture, the knowledge graph is neither Constraints (operator-owned, read-only) nor Identity (agent-owned, writable) nor Session (ephemeral). It is **shared organizational infrastructure** — analogous to the comms service or audit log. It lives in the Body layer, mediated by the same infrastructure that mediates all other external access. This means: agent access to the graph goes through mediation (ASK Tenet 3), graph writes are audited (Tenet 2), and graph reads are ACL-filtered to enforce least privilege (Tenet 4). The graph is operator-owned infrastructure that agents contribute to but do not control.

**Why graphs, not vector search.** The standard RAG approach embeds documents into vectors and retrieves by similarity. This works for "find me something about X" but loses structure — it can't answer "what's connected to X" or "how does X relate to Y through Z." A graph preserves relationships. An agent learning about a vulnerability doesn't just get "CVE-2024-XXXX exists" — it gets "CVE-2024-XXXX → affects → nginx → runs on → prod-web → owned by → infra-team." The traversal context is what makes the briefing actionable, not just relevant.

**Short-term and long-term memory.** Individual agents maintain their own topic-based memory files in their workspace — fast, personal, unstructured notes about their current work. This is short-term memory. The knowledge graph is long-term memory: structured, shared, durable, and queryable in ways flat files aren't. Agent memory is a scratchpad. The graph is organizational intelligence.

**The compounding effect.** Each task an agent completes adds nodes and edges to the graph. Each subsequent task benefits from the accumulated graph. The tenth agent to work in a domain starts with a richer briefing than the first. The hundredth starts with something approaching genuine organizational expertise. The graph gets denser, retrieval gets more precise, and agents need fewer tokens to reach the same level of understanding.

**Provenance.** Every graph node carries provenance metadata: which agent contributed it, from which session, at what trust level, and from what source data. Provenance is written by the mediation layer, not the contributing agent (ASK Tenet 2). This makes poisoned subgraphs traceable — if a compromised agent is identified, every node it contributed can be located, reviewed, and quarantined.

## 3. Organizational Knowledge as a Product

The knowledge graph isn't just agent infrastructure. It's a knowledge asset the organization owns.

**Agents build it as a side effect of work.** When agents contribute findings to the graph — a security vulnerability, a system dependency, a process insight — they're creating structured knowledge that would otherwise live in Slack threads, documents no one reads, or the heads of people who leave. The graph captures this institutional knowledge in a form that's queryable, traversable, and permanent.

**Humans can use it directly.** The graph exports in multiple formats: JSONL for data pipelines, Cypher for Neo4j (run your own traversal queries), DOT for Graphviz visualization, and JSON for integration with other tools. An operator can ask the graph "what does our security team know?" and get a structured answer. A manager can visualize how knowledge clusters across teams. An auditor can trace how a specific finding propagated through the organization.

**It answers questions humans didn't know to ask.** Graph traversal reveals connections that aren't visible in flat data. The `/path` endpoint answers "how does agent A's work connect to agent B's domain?" The `/neighbors` endpoint shows what's adjacent to a concept. The `/who-knows` endpoint identifies which agents have expertise in a topic. These are questions that emerge from the graph's structure, not from any individual agent's work.

**It survives everything.** By design, the knowledge graph persists through agent restarts, team teardowns, and infrastructure resets. Even a full platform reset preserves it by default — explicit operator action is required to wipe organizational knowledge. Institutional memory should be harder to delete than to create.

## 4. Graph Quality

Compounding only works if what compounds is signal, not noise.

**The quality problem.** Not every message, every finding, or every agent contribution deserves a permanent place in the organizational graph. If ingestion is indiscriminate, the graph fills with low-signal nodes, retrieval relevance degrades, briefings get longer and less useful, and the compounding effect reverses — more data makes agents slower, not smarter.

**Two-stage ingestion (current).** Agency's knowledge service already runs a two-stage pipeline:
1. **Rule-based ingestion** extracts structured facts from agent messages in real-time using pattern matching. High precision, limited recall.
2. **LLM synthesis** periodically reviews message batches and decides what's worth adding as graph nodes. This catches insights that don't match patterns.

**The curation gap.** Both stages operate on incoming data in isolation. Neither understands the graph as a whole. A curator needs to answer questions like: Is this node a duplicate of something already in the graph? Does this finding connect meaningfully to existing knowledge, or is it an orphan? Is this cluster over-represented — are we adding a twentieth node about the same topic when two would suffice?

**Two kinds of knowledge about quality.** Agents know domain context — what's significant in the work they just did. A curator knows graph context — whether a new node improves or degrades the overall structure. Both perspectives are necessary. Agent contributions provide the raw material; curation ensures it compounds rather than accumulates.

**Contribution policy.** Not every agent should be able to contribute anything to the graph. The policy engine governs graph write scope: agents contribute knowledge within their authorized domain only. A security agent contributing nodes about infrastructure topology, or an infrastructure agent contributing security findings, may be operating outside scope. Graph contributions are subject to the same least-privilege model as every other agent action (ASK Tenet 4). The policy hierarchy (platform > org > department > team > agent) applies to graph write permissions the same way it applies to other capabilities.

**Measuring graph health.** Graph quality needs observable signals: orphan node ratio (nodes with no edges), duplicate density (near-identical labels within the same kind), cluster concentration (whether a few topics dominate the graph), and retrieval hit rate (how often GraphRAG queries return useful results vs. noise). Specific thresholds are TBD — they'll depend on real operational data — but these signals define what the curator optimizes for. An agent whose contributions are frequently pruned or flagged by the curator should see trust impact — graph contribution quality feeds back into Agency's trust calibration system (ASK Tenet 15).

**Graph quarantine.** If a section of the graph is suspected of poisoning, it must be quarantinable: immediately excluded from all GraphRAG retrieval while preserved for forensic analysis. This is analogous to agent quarantine (ASK Tenet 16) — immediate, silent, and complete. Because every node carries provenance, quarantine can target all contributions from a specific agent, session, or time window. Quarantined nodes are invisible to retrieval but remain in the database for audit.

**Bootstrap safety.** The early graph — when data is sparse and the curator is least calibrated — is when poisoning has the highest compounding impact. During the bootstrap phase, graph contributions should require human review before being trusted in briefings, analogous to ASK's profile-then-lock pattern for trust calibration. Once the graph reaches a density threshold and the curator has calibrated, contributions can flow through automated curation.

**The curator as a future subsystem.** A dedicated curation process — running against the graph after contributions arrive — can merge duplicates, prune low-signal nodes, strengthen connections between related findings, and maintain the graph's information density over time. The curator is infrastructure, not an agent — its graph writes are mediated, audited, and subject to policy (ASK Tenet 3). Graph mutations during curation must be atomic from the perspective of agents performing retrieval; no agent should see partial curation state. This is a natural candidate for a local model: the task is structured and verifiable (merge/prune decisions can be checked against graph health metrics), so a capable open model with deterministic post-checks meets the quality bar. It doesn't need frontier-model reasoning. It needs reliable graph maintenance. A separate spec will cover the design.

## 5. Cost Architecture

The system should get cheaper per unit of work as it matures. Here's how.

**The tiered model.** Not every LLM call in a multi-agent system requires the same capability. Agent reasoning on complex tasks justifies frontier model costs. Administrative work — curation, classification, synthesis, summarization — does not. The insight is that these tasks have different quality floors and different cost sensitivities:

| Task type | Quality floor | Cost sensitivity | Right model |
|---|---|---|---|
| Complex reasoning, planning | High | Low | Frontier (Opus, Sonnet) |
| Synthesis, summarization | Medium | Medium | Lightweight API (Haiku) |
| Curation, dedup, classification | Low-medium | High | Local model (Ollama/llama) |
| Pattern extraction, routing | N/A | N/A | Heuristic / rules |

**Local models for administrative work.** A local model running as a sidecar (Ollama exposes an OpenAI-compatible API) can handle curation, classification, and other high-frequency administrative tasks at zero marginal token cost. The quality floor for these tasks is low enough that a capable open model meets it. The existing enforcer already routes between Anthropic and OpenAI APIs — adding a local endpoint is architecturally the same operation.

**Dynamic routing optimization.** The data to make intelligent routing decisions already exists in the system. The enforcer logs every LLM call — model, tokens, latency — and tracks budget. A routing optimizer that observes call metadata (model, tokens, latency, task type, retry count — never prompt content) and tracks outcome quality can progressively learn which task types can be downshifted to cheaper models without quality loss.

The routing optimizer must be infrastructure, not an agent (ASK Tenet 1). If an agent could write routing policy that the enforcer consults, it would be influencing its own enforcement. The optimizer is a service that produces routing recommendations; those recommendations require operator approval before being applied to enforcer configuration. Alternatively, the optimizer writes a policy draft that the operator promotes — the enforcer never consults unapproved policy.

This is a bounded optimization problem: given a call's metadata (token count, task type, agent role, retry history), which model minimizes cost while meeting a quality threshold? Success is initially defined as absence of retry or error escalation — a call that completes without the agent needing to retry or ask for clarification was routed correctly. A simple bandit that tracks this success rate per (task-type, model) pair captures most of the value without requiring full RL infrastructure.

**The five cost levers, in priority order:**

1. **Local models for admin work** — curation, synthesis, classification. Moves high-frequency calls to zero marginal cost.
2. **Dynamic model routing** — right-size every call based on learned outcomes. Prevents over-provisioning.
3. **Graph quality** — better graph = tighter briefings = fewer retrieval tokens. Noise in the graph = longer prompts = higher cost.
4. **Channel discipline** — agents read only what's relevant. Targeted reads, @mention wakeup, unreads-only. Prevents briefing multiplication.
5. **Coordinator efficiency** — decomposition that creates too many subtasks is expensive. Quality checks on decomposition before dispatch prevent cost multiplication.

**The virtuous cycle.** Better graph quality improves retrieval precision, which shortens briefings, which reduces token spend. Lower-cost admin models enable more frequent curation, which improves graph quality. The routing optimizer learns from outcomes, shifting more calls to cheaper models as confidence grows. Each lever reinforces the others.

## 6. Failure Modes

The compounding thesis depends on feedback loops working correctly. When they don't:

- **Graph poisoning.** If agents contribute bad knowledge — hallucinated facts, misattributed relationships, confidently wrong summaries — the graph compounds misinformation. Every subsequent briefing reinforces the error. Mitigation: source priority is determined by infrastructure (the contributing agent's trust level as tracked by the trust calibration system), not self-reported by the agent. High-impact nodes (those with many edges) require confirmation from a higher trust source before being used in briefings. Provenance tracking on every node allows poisoned subgraphs to be traced and quarantined by contributing agent or session.
- **GraphRAG as XPIA vector.** The automatic injection of graph-retrieved content into agent context is an attack surface. If an attacker poisons the graph (via a compromised agent), they can inject arbitrary content into any agent that queries related topics — a graph-mediated cross-prompt injection attack. Mitigation: GraphRAG-injected content must pass through the same pre-call guardrail scanning (XPIA detection in the enforcer) that scans all other content entering the LLM context. The GraphRAG injection path must be listed in the platform's attack surface model.
- **Cross-authorization synthesis.** When agents from different authorization scopes contribute to the shared graph, an agent reading the graph could access a synthesized view that exceeds what any individual contributor was authorized to expose (ASK Tenet 12). Even the existence of a connection between nodes is information. Mitigation: graph traversal must enforce the querying agent's authorization scope. The ACL model must filter not just node content but edge traversal — an agent cannot traverse edges into nodes outside its visible channels. The graph API must enforce this at query time, not rely on post-retrieval filtering.
- **Curation over-pruning.** An aggressive curator that removes nodes too eagerly destroys organizational memory. A useful but infrequently-accessed node looks like noise to a model that optimizes for graph density. Mitigation: soft-delete with a recovery window rather than hard pruning; flag low-confidence prune decisions for human review.
- **Routing optimizer local minima.** The optimizer converges on "always use haiku for task type X" based on early data, but the task distribution shifts and haiku starts failing more often. Mitigation: exploration rate — always route a small percentage of calls to non-preferred models to keep learning. Standard bandit technique.
- **Knowledge silos via channel ACLs.** The graph filters results by channel visibility. If teams don't share channels, their knowledge subgraphs become disjoint — the organization has knowledge but agents can't see it. Mitigation: structural nodes (agents, teams, tasks) are always visible regardless of channel ACLs. Cross-team queries can surface that a connection exists without exposing the content of restricted nodes — the metadata "a related node exists in a channel you can't see" is itself useful without violating authorization boundaries.
- **Local model compromise.** A local model sidecar running on the mediation network is a new component with its own threat profile: weights could be poisoned, the model could be exploited to produce attacker-controlled curation decisions, or it could be used as an exfiltration channel. Mitigation: the local model integration spec must include a threat model addendum. The local model should have no network access beyond the mediation network, no access to prompt content (metadata only for routing), and its outputs must be treated as untrusted — subject to the same validation as any agent output.

These aren't theoretical — they're the predictable failure modes of any system that compounds state over time. The specs for curator, local models, and routing optimizer must each address the relevant failure modes explicitly.

## 7. Principles

Six principles govern this strategy. Two are security properties suitable for extraction into the ASK framework as tenets. Four are design principles that guide Agency's implementation.

### ASK Tenets (to be added to the framework)

**Tenet: Organizational knowledge is durable infrastructure, not agent state.**
Knowledge accumulated by agents must be structured, auditable, and operator-owned. It persists independently of any individual agent's lifecycle. Agents contribute to and consume from it but cannot control, suppress, or degrade it unilaterally. Destroying organizational knowledge requires more deliberate action than destroying any individual agent or team.

**Tenet: Knowledge access is bounded by authorization scope.**
Organizational knowledge is shared, but access to it is not unlimited. Graph traversal, retrieval, and contribution are subject to the same authorization model as every other agent action. No agent can read knowledge outside its authorized scope, and the synthesized view available through the graph must not exceed what the querying agent is individually authorized to access (Tenet 12).

### Design Principles (Agency implementation)

**Principle 1: Every task should leave the organization smarter.**
Agent work should produce durable organizational knowledge as a byproduct, not just task output. Contribution discipline — knowing what's worth recording — is as important as the contribution mechanism itself.

**Principle 2: Administrative work should be delegated to the cheapest capable resource.**
Not every operation in an agent organization requires frontier-model reasoning. Curation, classification, synthesis, and routing are bounded tasks with lower quality floors. These should be served by the least expensive resource that meets the quality threshold — local models, lightweight APIs, or heuristics. Administrative delegation must not reduce enforcement quality below the threshold required by the governance model.

**Principle 3: Capability and cost should improve together.**
A well-designed agent organization gets both smarter and cheaper per unit of work over time. The mechanisms that improve capability (knowledge accumulation, better retrieval) and the mechanisms that reduce cost (routing optimization, curation, local models) should reinforce each other, not trade off.

**Principle 4: Routing and resource allocation must be data-driven, not static.**
The system must track its own cost, quality, and performance metrics and use them to adjust model selection and task routing. Static configuration is a starting point, not a steady state. Every LLM call is an observation; the system should learn from the aggregate.

## 8. Agency Implementation

This section maps the strategy to concrete subsystems in Agency's codebase and identifies the specs needed to realize it.

**Already built** (implemented, covered by test suite, not yet validated in multi-agent production deployment):
- Knowledge graph store with FTS5 search, node/edge model, channel-based ACL filtering
- Knowledge HTTP server with query, traversal, ingestion, and export endpoints
- GraphRAG injection in the body runtime — automatic retrieval and briefing prepended to task content
- Two-stage ingestion pipeline: rule-based extraction + LLM synthesis
- Graph export in JSONL, Cypher, DOT, and JSON formats
- Graph traversal: `/neighbors`, `/path`, `/context` with configurable hops
- Agent contribution via `contribute_knowledge` tool
- Knowledge persistence through `admin destroy` (default preserves organizational knowledge)
- Budget tracking and rate limiting in the enforcer
- Enforcer-based LLM routing (Anthropic + OpenAI)

**Spec status:**
- **Knowledge graph curator** — ✅ Done. Infrastructure service that maintains graph quality by merging duplicates, pruning low-signal nodes, and monitoring information density. Mediated, audited, atomic mutations. Runs on local admin model. Spec: `knowledge-graph-curator.md`.
- **Local model integration** — ✅ Done. Managed Ollama container (`agency-infra-admin-model`) on mediation network. OpenAI-compatible API. Local-first synthesizer with Haiku fallback. Graduated trust validation. Lazy model pull. Spec: `local-model-integration.md`.
- **Dynamic routing optimizer** — Not started. An infrastructure service (not an agent) that observes LLM call metadata and progressively learns optimal model routing per task type. Produces routing policy drafts that require operator approval before the enforcer applies them. Must address: success signal definition, exploration rate, local minima recovery.
- **Graph ACL model** — Not started. Enforcement of authorization-scoped graph traversal. Query-time filtering that prevents cross-authorization synthesis (ASK Tenet 12). Must handle: edge traversal across authorization boundaries, metadata-only responses for restricted nodes.
- **GraphRAG security** — Not started. Pre-call XPIA scanning of graph-injected briefing content. Attack surface documentation. Provenance-based quarantine of poisoned subgraphs.

Each spec is designed and implemented as a separate spec → plan → implementation cycle.
