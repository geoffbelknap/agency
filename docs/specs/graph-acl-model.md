---
description: "Classification-based access control for the knowledge graph — four tiers mapped to scopes via operator-configurable YAML."
status: "Draft"
---

# Graph ACL Model

*Classification-based access control for the knowledge graph — tiers map to scope rules, enforcement reuses existing scope filtering.*

**Date:** 2026-04-06
**Status:** Draft
**Depends on:** [Knowledge Graph Intelligence](knowledge-graph-intelligence.md) (Phase 1/1b scope enforcement), [Platform Identity](platform-identity-uuid-adoption.md) (registry)

---

## Overview

The knowledge graph scope model (Phase 1) stores a `classification` field on nodes — `public`, `internal`, `restricted`, `confidential` — but doesn't enforce it. This spec activates enforcement by mapping classification tiers to scope rules. When a node is classified, the tier's scope principals are merged into the node's scope. Existing scope enforcement (Phase 1b) does the rest.

No new enforcement mechanism. No federation. Classification is syntactic sugar for scope assignment.

---

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Enforcement mechanism | Reuse existing scope filtering | Phase 1b already enforces scope in all traversal methods. Classification just sets scope. |
| Tier definitions | Four fixed tiers (public/internal/restricted/confidential) | Well-understood model. Covers enterprise needs without over-engineering. |
| Scope mappings | Operator-configurable via YAML | Different deployments need different access rules per tier. |
| Federation | Deferred | No multi-deployment use case today. Design is federation-ready (UUIDs, scopes). |
| Auto-classification | Not included | Operators or agents set classification explicitly. |

---

## Classification Config

File: `~/.agency/knowledge/classification.yaml`

```yaml
version: 1
tiers:
  public:
    description: "No access restrictions — visible to all authenticated principals"
    scope: {}
  internal:
    description: "Standard access — any registered principal"
    scope:
      principals: ["role:internal"]
  restricted:
    description: "Limited access — security and operations teams"
    scope:
      principals: ["role:restricted"]
  confidential:
    description: "Need-to-know — designated principals only"
    scope:
      principals: ["role:confidential"]
```

**Operator customization:** Edit the `scope.principals` list for any tier. Example: restrict "confidential" to only the security team:

```yaml
  confidential:
    description: "Security team only"
    scope:
      principals: ["team:a1b2c3d4-..."]
```

**Default:** Written by `agency setup` with the four-tier default above. Operators can edit at any time — the knowledge service reloads on SIGHUP (same hot-reload pattern as ontology).

---

## Auto-Scope Assignment

When `add_node()` is called with a scope that includes `classification`, the knowledge service merges the tier's scope into the node's scope before storing.

**Flow:**

1. Node created with `scope: {"classification": "restricted", "channels": ["security"]}`
2. Knowledge service looks up `restricted` in classification config
3. Config says `restricted` → `scope.principals: ["role:restricted"]`
4. Merged result: `scope: {"classification": "restricted", "channels": ["security"], "principals": ["role:restricted"]}`
5. Node stored with merged scope
6. Existing scope enforcement in `find_nodes()`, `get_subgraph()`, `find_path()` etc. filters by principal overlap — only principals with `role:restricted` in their scope see this node

**Merge rules:**
- Tier principals are ADDED to the node's existing principals (union, not replace)
- Tier channels (if any) are ADDED to existing channels
- Classification field is preserved as-is
- If tier is `public` (empty scope), no principals are added — node remains unrestricted
- If classification is unrecognized, log a warning and treat as `internal` (fail to more restrictive)

**Where it runs:** In `KnowledgeStore.add_node()`, after scope is built from parameters or source_channels, but before the INSERT. The classification config is loaded once at startup and cached (reloaded on SIGHUP).

---

## Role Assignment

For classification enforcement to work, principals need the appropriate roles. The classification config references role-based principals like `role:restricted`. These roles are registered in the principal registry.

**Setup flow:**

`agency setup` creates the default classification roles:
```
role:internal      (all operators and agents get this by default)
role:restricted    (assigned to principals who need restricted access)
role:confidential  (assigned to principals who need confidential access)
```

**Assigning roles to principals:**

```bash
agency registry update <agent-name> --type agent --parent <role-uuid>
```

Or via the classification CLI:

```bash
agency knowledge classification grant restricted --principal agent:researcher
```

This adds `role:restricted` to the agent's scope so it can see restricted nodes.

---

## CLI

```bash
# Show current classification config
agency knowledge classification show

# Set tier scope (replaces principals for a tier)
agency knowledge classification set <tier> --principals role:X,team:Y

# Grant a principal access to a classification tier
agency knowledge classification grant <tier> --principal <type:name>

# Revoke a principal's access to a tier
agency knowledge classification revoke <tier> --principal <type:name>
```

---

## Server Endpoint

`GET /classification` — returns the current classification config as JSON.

The knowledge service reads the config file at startup. No write endpoint — operators edit the YAML directly. Hot-reload via SIGHUP.

---

## ASK Compliance

| Tenet | How this spec complies |
|---|---|
| Tenet 7 (Least privilege) | Higher classification = fewer principals with access. Default is restrictive. |
| Tenet 20 (Policy as config) | Classification mappings in version-controlled YAML, not ad-hoc. |
| Tenet 27 (Knowledge access bounded) | Classification maps to scope, scope enforced at query time by Phase 1b. |

---

## Implementation Phases

### Phase 1: Classification Config + Auto-Scope

- Classification config loader in knowledge service (read YAML, cache, SIGHUP reload)
- Auto-scope merge in `add_node()` — look up classification, merge tier scope
- Default config written by `agency setup`
- Default roles registered in principal registry

### Phase 2: CLI + Server Endpoint

- `agency knowledge classification show/set/grant/revoke`
- `GET /classification` endpoint on knowledge service
- Go gateway proxy + route

### Phase 3: Validation

- Tests for auto-scope merge
- Tests for classified nodes excluded from unauthorized queries
- Tests for config reload
- Full regression

---

## Testing

### Phase 1 Tests
- Config loads from YAML correctly
- Auto-scope merges tier principals into node scope
- Public classification adds nothing (unrestricted)
- Unrecognized classification defaults to internal
- Classified node excluded from find_nodes() for unauthorized principal
- Classified node visible to authorized principal
- Config reload on SIGHUP

### Phase 2 Tests
- CLI show displays config
- CLI set updates tier principals
- CLI grant adds principal to tier scope
- GET /classification returns config JSON
- Go gateway builds

### Phase 3 Tests
- Full regression across all knowledge graph tests
