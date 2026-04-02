---
name: doctor
description: Run Agency health checks — verify Docker, infrastructure, agent configurations, and security guarantees
user_invocable: true
---

Run `agency admin doctor` via Bash. Review the output and summarize:

1. Which checks passed
2. Which checks failed, with the specific issue
3. Recommended fixes for any failures

If Docker is not running, say so first — everything else depends on it.
