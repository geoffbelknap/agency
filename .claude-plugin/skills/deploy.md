---
name: deploy
description: Build, install, and bring up the Agency platform — rebuild binary, restart daemon, start infrastructure
user_invocable: true
---

Run the Agency deploy sequence:

1. `make install` — build the Go binary and restart the gateway daemon
2. `agency infra up` — ensure all shared infrastructure is running
3. `agency status` — verify everything is healthy

Report any failures with the specific component that failed.
