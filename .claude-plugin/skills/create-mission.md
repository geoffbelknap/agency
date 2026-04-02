---
name: create-mission
description: Create a new mission YAML file for an agent — standing instructions with triggers, success criteria, cost mode, and fallback policies
user_invocable: true
---

Help the user create a mission YAML file. Ask them:

1. **What agent is this for?** (must already exist)
2. **What should the agent do?** (the mission instructions)
3. **What triggers it?** (schedule/cron, channel message, webhook, manual)
4. **Cost mode?** (frugal, balanced, thorough — defaults to balanced)
5. **Success criteria?** (optional — how to know the mission succeeded)

Then generate a mission YAML file following this structure:

```yaml
name: <mission-name>
description: <one-line summary>
instructions: |
  <detailed instructions for the agent>
cost_mode: balanced
triggers:
  - name: <trigger-name>
    source: <schedule|channel|webhook>
    # cron: "0 9 * * 1-5"  # for schedule triggers
    # channel: alerts       # for channel triggers
success_criteria:
  mode: checklist_only
  checklist:
    - <measurable outcome>
fallback:
  - trigger: consecutive_errors
    count: 3
    action: pause_and_assess
```

Save the file to the current directory as `<mission-name>.yaml`. Then run:

```bash
agency mission create <mission-name>.yaml
agency mission assign <mission-name> --agent <agent-name>
```

Refer to `docs/specs/missions.md` for the full schema reference if the user asks for advanced options (reflection, procedural memory, episodic memory, evaluation mode).
