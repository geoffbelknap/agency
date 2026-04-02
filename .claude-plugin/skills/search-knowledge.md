---
name: search-knowledge
description: Search the Agency knowledge graph — find entities, relationships, and context across all agents
user_invocable: true
---

Help the user query the knowledge graph. Ask what they're looking for, then use the appropriate MCP tools:

1. **Entity search** — use `agency_admin_knowledge` with action `search` to find entities by name, type, or properties
2. **Relationship traversal** — find how entities are connected (e.g., "what does agent X know about service Y?")
3. **Agent-scoped queries** — search within a specific agent's knowledge

Present results as a concise summary. For each entity, show:
- Name and type
- Key properties
- Related entities (if relevant to the query)

If the knowledge graph is empty or the knowledge service isn't running, say so and suggest `agency infra up`.

Common entity types: `host`, `service`, `credential`, `person`, `organization`, `vulnerability`, `procedure`, `episode`, `detection`, `alert`.
