---
name: create-preset
description: Create a new agent preset — reusable agent template with identity, capabilities, model tier, and hard limits
user_invocable: true
---

Help the user create a reusable agent preset. Ask:

1. **Preset name** — lowercase, descriptive (e.g., `alert-triage`, `code-reviewer`, `data-analyst`)
2. **What does this type of agent do?** (role description)
3. **Model tier** — frontier, standard, fast, mini, nano
4. **Capabilities** — knowledge, comms, web-fetch, or specific services
5. **Autonomous or interactive?** — autonomous agents need hard limits prohibiting clarifying questions
6. **Hard limits** — things the agent must never do (e.g., "never modify production databases", "never send external emails")

Generate a preset YAML file:

```yaml
name: <preset-name>
description: <one-line description of this agent type>
model_tier: standard

identity:
  role: <what this agent does>
  body: |
    You are a <role description>.

    <behavioral instructions — how to approach tasks, what to prioritize>

capabilities:
  - knowledge
  - comms

hard_limits:
  - <inviolable constraint>
  - <inviolable constraint>

scopes:
  required:
    - <service the agent needs>
  optional:
    - <service the agent can use if available>
```

For autonomous agents (alert triage, monitoring, etc.), always include in `hard_limits`:
```yaml
hard_limits:
  - Never ask clarifying questions. Use available context and make reasonable assumptions.
  - If unable to proceed, pause and notify the operator.
```

Save the file as `<preset-name>.yaml`. To make it available locally:

```bash
cp <preset-name>.yaml ~/.agency/presets/<preset-name>/preset.yaml
```

To publish to the hub for others, use `/hub-publish`.

To create an agent from this preset:

```bash
agency create <agent-name> --preset <preset-name>
```
