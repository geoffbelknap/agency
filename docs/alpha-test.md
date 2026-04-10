---
title: "Alpha Test Guide"
description: "A short first-run guide for early Agency alpha testers."
---

Use this guide when you are trying Agency for the first time. The goal is not to learn every feature. The goal is to get one useful agent running, understand what it is doing, and know how to recover if something gets confusing.

## What You Need

1. Docker installed and running.
2. One LLM provider API key. Google Gemini is a good alpha choice because it has a free tier.
3. About 20 minutes.

## Start Agency

Run:

```bash
agency quickstart
```

Quickstart will:

1. Check that Docker is running.
2. Ask which LLM provider to use.
3. Store your API key in Agency's encrypted credential store.
4. Start the local infrastructure.
5. Create and start your first agent.
6. Send a short demo task so you can see a response.

When it finishes, open the Web UI:

```text
http://localhost:8280
```

## What To Try First

In the Web UI, open **Channels**, find your agent under **Direct Messages**, and send one of these:

```text
What can you help me with? Give me three practical things to try.
```

```text
Look at your current workspace and summarize what you can see.
```

```text
Create a short notes.md file explaining what you found.
```

You can also use the terminal:

```bash
agency send henry "What can you help me with? Give me three practical things to try."
```

## What Good Looks Like

- `agency status` shows infrastructure running.
- The Web UI opens at `http://localhost:8280`.
- Your agent appears as running.
- The agent answers in its direct-message channel.
- You can stop the agent from the UI or with `agency stop <agent-name>`.

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
