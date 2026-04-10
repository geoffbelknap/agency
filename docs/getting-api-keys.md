---
title: "Getting API Keys for Agency"
description: "How to get API keys from each supported LLM provider, including free-tier options, to use with Agency agents."
---


Agency agents need an API key from at least one LLM provider. This guide walks through getting a key from each supported provider, including free-tier options where available.

You only need **one** key to get started. You can add more providers later.

## Quick Comparison

| Provider | Model Tiers | Free Tier | Time to Get Key | Best For |
|----------|-------------|-----------|-----------------|----------|
| **Anthropic** (Claude) | Frontier, Standard, Fast | No free tier; pay-as-you-go from $0 | ~2 min | Best reasoning, recommended default |
| **Google** (Gemini) | Frontier, Standard, Fast, Mini | Yes — generous free quota | ~2 min | Getting started at zero cost |
| **OpenAI** (GPT) | Frontier, Standard, Fast, Mini, Nano | $5 free credit for new accounts (may vary) | ~2 min | Broadest model range |

## Anthropic (Claude) — Recommended

Claude models are Agency's default and offer the strongest reasoning for complex agent tasks.

**Pricing:** Pay-as-you-go. No minimum. Typical agent workloads cost $1-10/day depending on model tier and volume.

### Steps

1. Go to [console.anthropic.com](https://console.anthropic.com/)
2. Sign up with email or Google/GitHub SSO
3. Complete phone verification (required)
4. Go to **Settings > API Keys**
5. Click **Create Key**, give it a name (e.g., "agency")
6. Copy the key — it starts with `sk-ant-`

### Set it

```bash
# Option A: Environment variable (add to your shell profile)
export ANTHROPIC_API_KEY="sk-ant-..."

# Option B: Pass directly during setup
agency setup    # will prompt for key if not in environment
```

## Google (Gemini) — Free Tier Available

Google AI Studio offers a free tier with generous rate limits — a great way to try Agency at zero cost.

**Free tier:** 15 requests/minute, 1M tokens/day, 1500 requests/day. No credit card required.

**Paid tier:** Pay-as-you-go after free quota. Gemini Flash models are among the cheapest available.

### Steps

1. Go to [aistudio.google.com/apikey](https://aistudio.google.com/apikey)
2. Sign in with your Google account
3. Click **Create API Key**
4. Select or create a Google Cloud project (auto-created if needed)
5. Copy the key

### Set it

```bash
export GEMINI_API_KEY="AI..."

# Or pass it during first-run setup
agency quickstart
```

`GOOGLE_API_KEY` is still accepted for older local setups, but new Agency installs use `GEMINI_API_KEY` because the Hub provider component is named `gemini`.

### Notes

- The free tier is rate-limited but sufficient for 1-3 agents doing light work
- For heavier workloads, enable billing in [Google Cloud Console](https://console.cloud.google.com/billing)
- Gemini Flash models offer excellent cost/quality for standard and fast tiers

## OpenAI (GPT)

**Pricing:** New accounts may receive a small free credit (historically $5, subject to change). After that, pay-as-you-go.

### Steps

1. Go to [platform.openai.com](https://platform.openai.com/)
2. Sign up with email, Google, Microsoft, or Apple SSO
3. Go to **API Keys** in the left sidebar (or visit [platform.openai.com/api-keys](https://platform.openai.com/api-keys))
4. Click **Create new secret key**, give it a name
5. Copy the key — it starts with `sk-`

### Set it

```bash
export OPENAI_API_KEY="sk-..."

# Or pass during setup
agency setup
```

### Notes

- You may need to add a payment method before the key works, even with free credits
- GPT-4.1-mini and GPT-4.1-nano are very cost-effective for fast/mini tier agents

## Using Multiple Providers

Agency's [model routing](/model-routing) automatically picks the best available model for each agent's tier. Adding multiple provider keys gives you:

- **Cost optimization** — route standard-tier agents to cheaper providers
- **Redundancy** — if one provider has an outage, agents continue on another
- **Best-of-breed** — frontier tasks to Claude, high-volume tasks to Gemini Flash

Add keys anytime after initial setup:

```bash
# Edit the environment file directly
nano ~/.agency/.env

# Or re-run setup to add interactively
agency setup
```

## Verifying Your Key

After setup, verify your key works:

```bash
agency admin doctor
```

The doctor check includes an LLM connectivity test. If it passes, your key is working.

## Cost Control

Agency includes built-in budget controls so you don't get surprised:

- **Hard budget** — agent stops when limit is reached (default: $10/agent/day)
- **Soft budget** — warning notification at threshold
- **Rate limiting** — requests per minute caps per agent

Configure in your agent's `policy.yaml` or set platform defaults during `agency setup`.

See [policies-and-governance.md](/policies-and-governance) for full budget configuration.
