---
title: "Alpha Test Guide"
description: "A short first-run guide for early Agency alpha testers."
---

Use this guide when you are trying Agency for the first time. The goal is not to learn every feature or become a command-line user. The goal is to get one useful agent running in the Web UI, understand what it is doing, and know the few recovery commands to use if something gets confusing.

## What You Need

1. Docker installed and running.
2. One LLM provider API key. Google Gemini is a good alpha choice because it has a free tier.
3. About 20 minutes.

## Start Agency

Run this one terminal command:

```bash
agency quickstart
```

Quickstart will:

1. Check that Docker is running.
2. Ask which LLM provider to use. For alpha testing, choose Google Gemini unless you already have another provider key ready.
3. Store your API key in Agency's encrypted credential store.
4. Start the local infrastructure.
5. Create and start your first agent.
6. Send a short demo task so you can see a response.

When it finishes, quickstart opens the browser directly to your new agent's chat and prints both the Web UI URL and the direct chat URL:

```text
Web UI: http://localhost:8280
Chat:   http://localhost:8280/channels/dm-henry
```

After this point, stay in the Web UI unless the guide explicitly says to use the terminal. Developers, security operators, and advanced users can use the CLI heavily; basic alpha testers should treat it as setup and recovery tooling.

If you need to re-run guided setup later, open `/setup` in the Web UI or use **Admin → Setup Wizard**. That is the recovery and reconfiguration path after quickstart, not a separate onboarding flow.

## What To Try First

In the Web UI:

1. Use the agent chat that quickstart opened, or open the printed **Chat** URL.
2. If you are starting from `http://localhost:8280`, open **Channels** and find your agent under **Direct Messages**.
3. Send one of these messages:

```text
What can you help me with? Give me three practical things to try.
```

```text
Look at your current workspace and summarize what you can see.
```

```text
Create a short notes.md file explaining what you found.
```

If the Web UI is not working, you can send the same message from the terminal:

```bash
agency send henry "What can you help me with? Give me three practical things to try."
```

## What Good Looks Like

- The browser opens to your first agent's chat, or the printed chat URL works.
- Your agent appears as running and has an **AGENT** badge in Direct Messages.
- The agent answers in its direct-message channel.
- You can stop the agent from the Web UI.
- If asked to check the terminal, `agency status` shows infrastructure running.

## Operator Readiness Check

Before handing an alpha build to a tester, run the local readiness check:

```bash
./scripts/alpha-readiness-check.sh
```

This is an operator/developer check, not a user-facing command. It verifies the daemon, infrastructure, Web UI, direct chat route, configured provider, provider credential, temporary agent startup, live DM response, and cleanup path. It uses the current local Agency profile and deletes its temporary `alpha-readiness-*` agent when finished.

The first run can take several minutes if local agent images need to be built. The check fails if the temporary agent does not reach `running` within 7 minutes or does not answer within 2 minutes.

## If Something Breaks

First check:

```bash
agency status
agency admin doctor
```

If the Web UI does not open, run:

```bash
agency infra up
```

If an agent seems stuck:

```bash
agency stop henry --immediate
agency start henry
```

If test runs leave temporary agents, containers, or networks behind, inspect the matched cleanup set first:

```bash
./scripts/cleanup-live-test-runtimes.sh
```

If the dry run only lists disposable `alpha-*`, `playwright-*`, `e2e-*`, or temporary-home resources, remove them:

```bash
./scripts/cleanup-live-test-runtimes.sh --apply
```

If you want to start the alpha test over:

```bash
agency admin destroy --yes
agency quickstart
```

`agency admin destroy --yes` removes running infrastructure and local platform state while preserving the knowledge graph by default.

## Feedback To Send

Please write down:

- The command or screen where you got stuck.
- What you expected Agency to do.
- What Agency actually did.
- Whether the recovery commands above fixed it.
- Any moment where the product used words you did not understand.
