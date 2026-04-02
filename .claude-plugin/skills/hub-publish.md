---
name: hub-publish
description: Create and publish a hub component — packs, presets, connectors, or services for the Agency hub registry
user_invocable: true
---

Help the user create a hub component and publish it to the agency-hub registry. First ask what type of component:

1. **Pack** — a bundle of presets, services, and ontology for a use case (e.g., security-ops, dev-assistant)
2. **Preset** — an agent template with identity, capabilities, hard limits, and model tier
3. **Connector** — an integration that pulls data from external APIs into the knowledge graph
4. **Service** — an API endpoint definition that agents can use via the egress proxy

## Pack

Generate `pack.yaml`:

```yaml
name: <pack-name>
version: "1.0.0"
description: <what this pack enables>
presets:
  - <preset-name>
services:
  - <service-name>
ontology:
  entity_types: []
  relationship_types: []
```

Packs do NOT include missions — missions are created and assigned separately.

## Preset

Generate `preset.yaml`:

```yaml
name: <preset-name>
description: <one-line description>
model_tier: standard
identity:
  role: <agent's role>
  body: |
    <personality and behavioral instructions>
capabilities:
  - knowledge
  - comms
hard_limits:
  - <constraint the agent must never violate>
scopes:
  required:
    - <service-name>
  optional: []
```

## Connector

Generate `metadata.yaml`:

```yaml
name: <connector-name>
description: <what data this connector provides>
version: "1.0.0"
type: poll
requires:
  credentials:
    - name: <API_KEY_NAME>
      type: secret
      description: <what this key is for>
  config:
    - name: <CONFIG_NAME>
      type: config
      description: <what this config value is>
  egress_domains:
    - <api.example.com>
```

## Publishing

After creating the component files:

1. Copy them to the `agency-hub` repo in the correct path:
   - Packs: `packs/<name>/pack.yaml`
   - Presets: `presets/<name>/preset.yaml`
   - Connectors: `connectors/<name>/metadata.yaml`
   - Services: `services/<name>/service.yaml`
2. Create a PR in the `agency-hub` repo
3. After merge, run `agency hub update` to refresh local cache

Refer to the agency-hub repo CLAUDE.md for schema validation requirements and CI checks.
