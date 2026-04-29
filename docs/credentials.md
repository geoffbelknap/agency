---
title: "Credentials"
description: "Centralized credential management for API keys, service tokens, and secrets. Agents never see real keys — the egress proxy injects them at the network boundary."
---


Agency uses a centralized encrypted credential store to manage all API keys and secrets. Real credentials never enter agent runtimes — the egress proxy resolves them at the network boundary via a socket connection to the gateway. The agent only ever sees a scoped proxy token.

## Quick Start

Add your first credential (an LLM provider key):

```bash
agency creds set ANTHROPIC_API_KEY sk-ant-... \
  --kind provider --scope platform --protocol api-key
```

Verify it works:

```bash
agency creds test ANTHROPIC_API_KEY
```

That's it. The credential is encrypted at rest, available to all agents (platform scope), and will be injected into outbound API calls by the egress proxy.

## Provider Credentials

LLM provider keys are platform-scoped — every agent can use them through the egress proxy.

```bash
agency creds set ANTHROPIC_API_KEY sk-ant-... \
  --kind provider --scope platform --protocol api-key

agency creds set OPENAI_API_KEY sk-... \
  --kind provider --scope platform --protocol api-key
```

Provider credentials use the `api-key` protocol. The egress proxy swaps the agent's scoped token for the real key on each outbound request.

## Service Credentials

API keys for external services like LimaCharlie, GitHub, Jira, etc. These are typically scoped to a specific agent or team.

```bash
agency creds set GITHUB_TOKEN ghp_... \
  --kind service --scope agent:dev-assistant --protocol bearer

agency creds set JIRA_API_KEY ... \
  --kind service --scope team:security --protocol api-key
```

For services that use JWT token exchange, specify the `--service`, `--protocol jwt-exchange`, and `--group`:

```bash
agency creds set LC_API_KEY_DETECTION_TUNER <key> \
  --kind service --scope agent:detection-tuner \
  --service limacharlie --protocol jwt-exchange --group limacharlie
```

The group provides shared protocol config. See the next section.

## Credential Groups

Some services need shared protocol configuration beyond a single API key — token exchange endpoints, org IDs, expiry settings. Credential groups hold this shared config so individual credentials can inherit it.

```bash
agency creds group create limacharlie \
  --protocol jwt-exchange \
  --token-url https://jwt.limacharlie.io \
  --token-param oid=<org-id> \
  --token-param expiry=3600
```

Now any credential in the `limacharlie` group inherits the token URL and exchange parameters. Each agent still gets its own API key — the group just centralizes the protocol config.

A typical setup for LimaCharlie with multiple agents:

```bash
# Shared protocol config
agency creds group create limacharlie \
  --protocol jwt-exchange \
  --token-url https://jwt.limacharlie.io \
  --token-param oid=<org-id> \
  --token-param expiry=3600

# Per-agent keys, each scoped to its own agent
agency creds set LC_API_KEY_DETECTION_TUNER <key> \
  --kind service --scope agent:detection-tuner \
  --service limacharlie --protocol jwt-exchange --group limacharlie

agency creds set LC_API_KEY_ALERT_TRIAGE <key> \
  --kind service --scope agent:alert-triage \
  --service limacharlie --protocol jwt-exchange --group limacharlie
```

## Listing and Inspecting

```bash
# List all credentials (values always redacted)
agency creds list

# Filter by kind or scope
agency creds list --kind service
agency creds list --kind service --scope agent:detection-tuner

# Show details for a specific credential
agency creds show ANTHROPIC_API_KEY

# Reveal the actual value (logged as an audit event)
agency creds show ANTHROPIC_API_KEY --show-value
```

Every `--show-value` call is recorded in the audit trail — there is no silent way to read a credential.

## Rotation and Testing

Rotate a credential without downtime:

```bash
agency creds rotate ANTHROPIC_API_KEY --value sk-ant-new-...
```

The new value takes effect immediately for all subsequent API calls. No agent restart needed — the egress proxy resolves credentials on each request.

Verify a credential can reach its target service:

```bash
agency creds test ANTHROPIC_API_KEY
```

This makes a lightweight connectivity check and reports success or failure with latency.

## Deleting

```bash
agency creds delete GITHUB_TOKEN
```

Deletion is immediate. Any agent relying on the credential will get errors on the next API call to that service.

## Scope Declarations in Presets

Agent presets declare which credentials they need. The platform validates these at assignment time and `agency admin doctor` audits them continuously.

```yaml
requires:
  credentials:
    - grant_name: limacharlie-api
      scopes:
        required: [insight.det.get, sensor.get]
        optional: [sensor.task]
    - grant_name: github
      scopes:
        required: [repo, read:org]
```

Required scopes must be satisfied before the agent can start. Optional scopes enable additional functionality but don't block startup.

## How Credentials Flow

```
agent  →  enforcer  →  egress proxy  →  external API
         (validates     (swaps scoped
          scopes)        token for
                         real key)
```

1. Operator stores a credential via `agency creds set`
2. The credential is encrypted (AES-256-GCM) and saved to `~/.agency/credentials/store.enc`
3. The gateway generates `credential-swaps.yaml` for the egress proxy
4. When an agent makes an API call, the request flows through the enforcer to the egress proxy
5. The egress proxy resolves the real credential from the gateway via Unix socket (`~/.agency/run/gateway.sock`)
6. The egress proxy injects the real API key into the outbound request
7. The agent never sees or handles the real credential

The enforcer validates that the agent has the required scopes for each tool call (`CheckScope()`), but it never holds actual credentials. The Unix socket for credential resolution is only bind-mounted into the egress container.

## Doctor Scope Audit

Run the doctor to verify credential configuration across all agents:

```bash
agency admin doctor
```

The output includes a credential scopes section showing each agent's required and optional scopes, and flags any agents with unsatisfied requirements. Run this after adding or rotating credentials to confirm everything is wired up.
