---
name: create-agent
description: Create a new agent — choose a preset or build a custom configuration with identity, capabilities, and policies
user_invocable: true
---

Help the user create a new agent. Ask:

1. **Agent name** — lowercase, no spaces (e.g., `security-analyst`, `dev-assistant`)
2. **Use a preset?** — run `agency hub search --kind preset` to show available presets. If they want a preset, use it. If custom, continue.

### From preset

```bash
agency create <name> --preset <preset-name>
```

Done. Suggest next steps: `agency start <name>`, create a mission.

### Custom agent

Ask about:

3. **Role** — what does this agent do? (one sentence)
4. **Model tier** — frontier (most capable, expensive), standard (default), fast (cheaper), mini (lightweight)
5. **Capabilities** — which services does it need? (knowledge, comms, web-fetch, etc.)
6. **Autonomous or interactive?** — autonomous agents should have `hard_limits` that prohibit asking clarifying questions

Then run:

```bash
agency create <name>
```

After creation, help configure identity and capabilities:

```bash
agency show <name>                    # verify creation
agency cap add <name> knowledge       # add capabilities
agency cap add <name> comms
```

If the user wants a custom identity or hard limits, edit the agent's preset file at `~/.agency/agents/<name>/preset.yaml`.

Suggest next steps: start the agent, create and assign a mission.
