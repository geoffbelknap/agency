# Knowledge Ontology

> **Tier:** Experimental. Ontology management is part of the broader graph
> governance / advanced review workflow surface that `CLAUDE.md` classifies
> as non-default for 0.2.x. Core graph retrieval is `TierCore` (`GraphAdmin`
> in `internal/features/registry.go`); ontology authoring and lifecycle is
> not part of the default surface. See [Core Pruning Rationale](core-pruning-rationale.md).

A typed ontology for the knowledge graph that defines entity types, relationship types, and attributes. The ontology guides LLM-based extraction, validates agent contributions, enables hub-distributable domain extensions, and compounds organizational intelligence by ensuring consistent, queryable knowledge across all agents.

## Why an Ontology

Today the knowledge graph has freeform `kind` values. The synthesizer tells the LLM "use whatever kind and relation values make sense." This produces inconsistent, unqueryable knowledge — one extraction calls something a "fact," another calls the same concept a "finding," a third invents "observation."

With an ontology:
- Extraction is consistent — the LLM uses defined types, not freeform labels
- Knowledge is queryable — "show me all decisions" works because `decision` is a defined type
- Agents get smarter over time — patterns, lessons, preferences, and narratives compound across sessions and across agents
- Domain extensions are installable — `agency hub install security --kind ontology` adds security-specific types

## Ontology File

The active ontology lives at `~/.agency/knowledge/ontology.yaml`:

```yaml
version: 1
name: default
description: General-purpose ontology for knowledge worker agents
last_modified: 2026-03-24T17:00:00Z

entity_types:
  person:
    description: An individual — operator, stakeholder, customer, team member
    attributes: [name, role, location, timezone, preferences, contact]
  system:
    description: An application, platform, database, or repository
    attributes: [name, type, owner, status, environment]
  decision:
    description: A choice that was made, including rationale and who decided
    attributes: [description, rationale, decided_by, date, alternatives_considered]
  # ... (full type list below)

relationship_types:
  owns:
    description: Has ownership of
    inverse: owned_by
  depends_on:
    description: Depends on
    inverse: depended_on_by
  # ... (full relationship list below)

changelog:
  - version: 1
    date: 2026-03-24
    changes: Initial default ontology
```

## Default Entity Types

### People and Organizations

| Type | Description |
|------|-------------|
| `person` | An individual — operator, stakeholder, customer, team member. Attributes: name, role, location, timezone, preferences, contact. |
| `organization` | A company, department, vendor, customer, or group. Attributes: name, type, industry, location. |
| `team` | A group of people who work together. Attributes: name, purpose, members. |
| `role` | A job function, responsibility, or expertise area. Attributes: title, scope, authority. |
| `stakeholder` | Someone with interest or authority in a project or domain. Attributes: name, interest, influence, organization. |
| `contact` | How to reach someone — email, slack handle, phone, timezone. Attributes: person, type, value, preferred. |

### Systems and Infrastructure

| Type | Description |
|------|-------------|
| `system` | An application, platform, database, or repository. Attributes: name, type, owner, status, environment. |
| `service` | An API, endpoint, or integration. Attributes: name, url, provider, status. |
| `environment` | A deployment context — prod, staging, dev, region. Attributes: name, type, region. |
| `configuration` | Settings, thresholds, or parameters that matter. Attributes: name, system, value, reason. |
| `product` | A software product, hardware, or computing tool. Attributes: name, vendor, version, purpose. |
| `credential` | An access grant or token — metadata only, never the value. Attributes: name, type, scope, expiry, owner. |

### Work

| Type | Description |
|------|-------------|
| `project` | An ongoing initiative or body of work. Attributes: name, status, owner, deadline, stakeholders. |
| `task` | A specific unit of work — ticket, issue, PR, request. Attributes: title, status, assignee, priority, source. |
| `process` | How things actually get done — runbooks, workflows, SOPs. Attributes: name, steps, owner, frequency. |
| `requirement` | Something that must be true — a constraint or acceptance criteria. Attributes: description, source, priority. |
| `goal` | An objective, milestone, or target. Attributes: description, owner, deadline, status. |

### Decisions and Context

| Type | Description |
|------|-------------|
| `decision` | A choice that was made, including rationale and who decided. Attributes: description, rationale, decided_by, date, alternatives_considered. |
| `finding` | Something learned through investigation or research. Attributes: description, source, confidence, date. |
| `preference` | How someone likes things done — communication style, priorities. Attributes: person, description, strength. |
| `fact` | Verified information about the domain. Attributes: description, source, verified_date. |
| `concept` | Domain terminology, mental models, or abstractions. Attributes: name, definition, context. |
| `opinion` | Someone's stated position — not a fact but important context. Attributes: person, description, strength. |
| `assumption` | Something believed true but not verified. Attributes: description, basis, risk_if_wrong. |
| `terminology` | Domain jargon with definitions — prevents agent misinterpretation. Attributes: term, definition, context, common_confusion. |

### Issues and Events

| Type | Description |
|------|-------------|
| `incident` | Something that went wrong. Attributes: title, severity, status, root_cause, resolution, date. |
| `change` | A modification to a system, process, or configuration. Attributes: description, system, author, date, impact. |
| `risk` | A potential problem to watch for. Attributes: description, likelihood, impact, mitigation. |
| `event` | A meeting, deadline, release, or milestone. Attributes: name, date, participants, outcome. |
| `schedule` | A recurring event or window — maintenance, on-call, business hours. Attributes: name, pattern, scope. |

### Patterns and Learning

| Type | Description |
|------|-------------|
| `pattern` | A recurring theme agents have noticed. Attributes: description, frequency, significance, examples. |
| `lesson` | Something learned from experience. Attributes: description, context, source_incident. |
| `resolution` | How something was fixed. Attributes: description, incident, effectiveness. |
| `workaround` | A temporary fix still in use. Attributes: description, for_issue, expiry, permanent_fix. |
| `cause` | Why something happened — root cause analysis. Attributes: description, event, confidence. |

### Living Context

| Type | Description |
|------|-------------|
| `narrative` | The ongoing story of a project, team, or initiative — updated as things evolve. Attributes: subject, current_state, last_updated. |
| `priority` | What matters most right now and to whom. Attributes: description, owner, scope, expiry. |
| `constraint` | A real-world limitation — budget, timeline, policy. Attributes: description, source, non_negotiable. |
| `context` | Situational info — code freeze, reorg, incident in progress. Attributes: description, scope, expiry. |
| `status` | Current state of something important. Attributes: subject, state, since, expected_resolution. |
| `tension` | A known conflict or disagreement. Attributes: description, parties, impact. |

### Documents and Artifacts

| Type | Description |
|------|-------------|
| `document` | A spec, report, runbook, wiki page, policy doc. Attributes: title, url, type, owner, last_updated. |
| `artifact` | A file, repo, dashboard, board, or deliverable. Attributes: name, url, type, owner. |
| `template` | A reusable pattern for a deliverable. Attributes: name, purpose, format. |

### Rules and Standards

| Type | Description |
|------|-------------|
| `rule` | A business rule, policy, or hard constraint. Attributes: description, source, scope, exceptions. |
| `metric` | Something measured and tracked — SLA, KPI, threshold. Attributes: name, value, target, unit, scope. |
| `standard` | A compliance requirement, framework, or best practice. Attributes: name, source, scope, requirements. |

### Location

| Type | Description |
|------|-------------|
| `location` | A physical or virtual place. Attributes: name, type, address, coordinates, timezone. |

### Skills and Quantities

| Type | Description |
|------|-------------|
| `skill` | An ability or competency — useful for expertise matching. Attributes: name, level, person. |
| `quantity` | A notable amount — budget, cost, measurement. Attributes: value, unit, context. |
| `url` | A web address for reference. Attributes: url, description, type. |

## Default Relationship Types

### Ownership and Authority

| Relationship | Description | Inverse |
|-------------|-------------|---------|
| `owns` | Has ownership of | `owned_by` |
| `manages` | Manages or oversees | `managed_by` |
| `responsible_for` | Is responsible for | `responsibility_of` |
| `escalate_to` | Should escalate issues to | `escalation_target_for` |

### Work and Assignment

| Relationship | Description | Inverse |
|-------------|-------------|---------|
| `works_on` | Is working on | `worked_on_by` |
| `assigned_to` | Is assigned to | `assigned_from` |
| `created_by` | Was created by | `created` |
| `contributed_to` | Contributed to | `received_contribution_from` |

### Dependencies and Structure

| Relationship | Description | Inverse |
|-------------|-------------|---------|
| `depends_on` | Depends on | `depended_on_by` |
| `blocked_by` | Is blocked by | `blocks` |
| `part_of` | Is part of | `contains` |
| `relates_to` | Is related to | `relates_to` |
| `supersedes` | Replaces or supersedes | `superseded_by` |

### Knowledge and Causality

| Relationship | Description | Inverse |
|-------------|-------------|---------|
| `knows_about` | Has knowledge of | `known_by` |
| `decided` | Made a decision about | `decided_by` |
| `caused` | Caused or led to | `caused_by` |
| `resolved_by` | Was resolved by | `resolved` |
| `learned_from` | Was learned from | `taught` |
| `triggered_by` | Was triggered by | `triggers` |

### Reference and Documentation

| Relationship | Description | Inverse |
|-------------|-------------|---------|
| `documented_in` | Is documented in | `documents` |
| `reported_by` | Was reported by | `reported` |
| `located_in` | Is located in | `location_of` |
| `uses` | Uses or employs | `used_by` |
| `prefers` | Prefers or favors | `preferred_by` |
| `scheduled_for` | Is scheduled for | `schedule_of` |

## Synthesizer Integration

The knowledge synthesizer's extraction prompt is updated to use the ontology:

```
You are extracting knowledge from team conversations. Given the messages below,
extract entities and relationships using ONLY the types defined in this ontology.

## Entity Types
{formatted from ontology.yaml — type: description}

## Relationship Types
{formatted from ontology.yaml — type: description}

## Existing Entities
{existing labels from the knowledge graph — merge into these, don't duplicate}

## Messages
{messages to analyze}

Output valid JSON. Use only defined entity types for "kind" and defined
relationship types for "relation". If something doesn't fit any defined type,
use the closest match and note it in the summary. Do not invent new types.
```

The knowledge container reads `ontology.yaml` at startup and formats it into the extraction prompt. When the ontology changes (extension installed, base edited), the synthesizer picks it up on the next synthesis cycle — no container restart needed.

## Agent-Side Validation

When an agent calls `contribute_knowledge`, the body runtime validates the `kind` field against the ontology:

1. If `kind` matches a defined entity type — accept as-is
2. If `kind` is close to a defined type (fuzzy match) — auto-correct and log: "Mapped 'observation' to 'finding'"
3. If `kind` is unrecognized — fall back to `fact` and log: "Unknown kind 'widget_status', stored as 'fact'"

The body runtime reads the ontology from `/agency/knowledge/ontology.yaml` (mounted read-only, same pattern as mission.yaml).

## Hub Distribution

Ontology extensions are a new hub component kind (`ontology`). An extension adds types — it cannot remove or modify base types.

```yaml
# agency-hub: ontologies/security.yaml
name: security
kind: ontology
description: Security operations — vulnerabilities, threats, controls, compliance
extends: default

entity_types:
  vulnerability:
    description: A security weakness in a system or process
    attributes: [cve, severity, system, status, discovered_date, remediation]
  threat:
    description: A potential attack vector or threat actor
    attributes: [name, type, target, likelihood, source]
  control:
    description: A security measure or safeguard
    attributes: [name, type, status, covers, effectiveness]
  compliance_requirement:
    description: A regulatory or policy requirement
    attributes: [name, framework, scope, status, deadline]
  indicator:
    description: An indicator of compromise or suspicious activity
    attributes: [type, value, confidence, source, first_seen]

relationship_types:
  mitigates: { description: "Mitigates or reduces", inverse: mitigated_by }
  exploits: { description: "Exploits or targets", inverse: exploited_by }
  complies_with: { description: "Satisfies a requirement", inverse: compliance_of }
  detected_in: { description: "Was detected in", inverse: detection_source_for }
```

Installation:
```bash
agency hub install security --kind ontology
# Copies to ~/.agency/knowledge/ontology.d/security.yaml
# Gateway merges base + extensions → ontology.yaml
# Ontology version incremented
```

Other hub ontology examples: `finance`, `hr`, `devops`, `legal`, `healthcare`, `customer-success`.

### Extension Merge Rules

- Extensions can only add entity types and relationship types
- Extensions cannot remove or modify base types
- If two extensions define the same type name, the gateway rejects the second install with a conflict error
- The `extends` field must match the base ontology name (currently only `default`)

## Versioning

The ontology has an integer version that increments on any change:

```yaml
version: 3
last_modified: 2026-03-24T17:00:00Z
changelog:
  - version: 3
    date: 2026-03-24
    changes: "Added security extension (5 entity types, 4 relationship types)"
  - version: 2
    date: 2026-03-24
    changes: "Added 'narrative' and 'tension' entity types"
  - version: 1
    date: 2026-03-24
    changes: "Initial default ontology"
```

Each knowledge graph node gets an `_ontology_version` field at extraction time. This enables forensics: "this node was extracted when the ontology had 35 types, before the security extension."

## Ontology Changes

### Adding types

Safe. New types become available for future extractions. Existing nodes are unaffected.

### Modifying descriptions or attributes

Safe. Changes how future extractions interpret the type. Existing nodes keep their data.

### Removing or renaming types

Requires migration. The gateway detects removed types and checks the graph:

```bash
# Operator edits base-ontology.yaml, removes "fact" type
agency graph ontology validate
# Output: "Type 'fact' removed but 47 nodes use it."
# Suggests: agency graph ontology migrate fact finding

agency graph ontology migrate fact finding
# Output: "Migrated 47 nodes from 'fact' to 'finding'. Ontology version bumped to 4."
```

The `validate` command catches drift between the ontology and the graph at any time.

## Migration from Freeform Kinds

On first startup with the ontology, the knowledge container runs a one-time migration:

1. Read all existing nodes from the graph
2. Map freeform `kind` values to ontology types:
   - `agent` → `system`
   - `channel` → platform metadata (not a knowledge entity)
   - `team` → `team`
   - `topic` → `concept`
   - `fact` → `fact`
   - `decision` → `decision`
   - `finding` → `finding`
   - Unrecognized → `fact` (safe fallback)
3. Update node `kind` fields in place, add `_migrated: true` flag
4. Log: "Migrated N entities: X remapped, Y unchanged"
5. Write marker: `~/.agency/knowledge/.ontology-migrated`

## File Layout

```
~/.agency/knowledge/
  ontology.yaml              # active merged ontology (base + extensions)
  ontology.d/                # installed extensions
    security.yaml
    devops.yaml
  base-ontology.yaml         # the default ontology (operator-editable)
  .ontology-migrated         # one-time migration marker
  graph.db                   # SQLite knowledge graph (existing)
  validation_state.json      # synthesizer state (existing)
```

The gateway ships with `base-ontology.yaml` embedded. On `agency setup`, it writes to `~/.agency/knowledge/`. On startup, the gateway merges base + extensions into `ontology.yaml`.

## CLI Commands

| Command | Effect |
|---------|--------|
| `agency graph ontology show` | Display active merged ontology |
| `agency graph ontology types` | List entity types with descriptions |
| `agency graph ontology relationships` | List relationship types |
| `agency graph ontology validate` | Check graph nodes against ontology |
| `agency graph ontology migrate <from> <to>` | Re-type nodes from one kind to another |

## REST API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/graph/ontology` | Get active ontology |
| `GET` | `/api/v1/graph/ontology/types` | List entity types |
| `GET` | `/api/v1/graph/ontology/relationships` | List relationship types |
| `POST` | `/api/v1/graph/ontology/validate` | Validate graph against ontology |
| `POST` | `/api/v1/graph/ontology/migrate` | Migrate nodes between types |

## What This Enables

- Consistent knowledge extraction — every agent and every synthesis cycle uses the same types
- Queryable knowledge — "show me all decisions" or "what risks are associated with this system" work reliably
- Compounding intelligence — patterns, lessons, preferences, and narratives accumulate across agents and sessions
- Domain adaptation — install a security ontology and agents immediately extract vulnerabilities, threats, and controls
- Institutional memory — new agents inherit the full organizational knowledge graph on day one, typed and searchable
