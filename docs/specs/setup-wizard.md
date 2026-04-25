# Setup Wizard

**Status:** Draft  
**Date:** 2026-04-02  
**Scope:** agency-web (primary), agency (backend additions), agency-hub (new component kinds)

## Overview

A conversational onboarding wizard for the Agency web UI. Guides first-time users through platform initialization, LLM provider configuration, agent creation, and capability enablement — then drops them into a live chat with their first agent.

The wizard auto-triggers on first launch (when no providers are configured) and is accessible from Admin for re-setup at any time.

## Design Principles

- **Validate as you go.** Every credential, every API call is verified at the step where it's entered. No surprises at the end.
- **Hub-sourced data.** Provider catalog, capability tiers, presets, and wizard configuration all come from the hub — not hardcoded in the frontend.
- **Conversational feel.** Full-viewport steps with minimal chrome. Dot indicators, not step counters. Spacious, personal, not form-like. Think Linear/Notion onboarding.
- **Re-setup friendly.** Pre-fills existing config, shows "Reconfigure" instead of "Add", makes skip prominent. Veterans can jump to any step or skip entirely.

## Wizard Flow

### Step 0: Hub Sync (automatic)

On first launch, the wizard fires `POST /api/v1/hub/update` followed by `POST /api/v1/hub/upgrade` before rendering any configuration steps. This populates the hub cache with provider components, capability tiers, presets, and the wizard config — all of which drive subsequent steps.

**UI:** A "Preparing your platform..." screen with a progress indicator. Non-skippable on first launch (without hub data, the wizard has nothing to offer). On re-setup, skipped if the hub was synced within the last hour.

**Error handling:** If the sync fails (no network, bad git config), show the error with a "Retry" button and a "Continue anyway" escape hatch (for air-gapped setups where the hub was pre-populated).

### Step 1: Welcome

Operator name input. Pre-filled on re-setup. Validated inline (alphanumeric + hyphens, 1-64 chars — same rules as `RunInit`).

Calls `POST /api/v1/init` with operator name (and `force: true` on re-setup). Must succeed before proceeding.

Brief welcome message explaining what the wizard will set up.

A "Skip Setup" link for veterans doing re-setup, which jumps straight to the main app.

### Step 2: LLM Providers

**Data source:** Hub-sourced provider components (kind: `provider`). Fetched via `GET /api/v1/infra/providers`.

**UI:** A grid of provider cards grouped by category:
- **Cloud** — Anthropic, OpenAI, Google, Mistral, etc.
- **Local** — Ollama (no key needed, API base configurable)
- **Compatible** — OpenAI-compatible (LiteLLM, OpenRouter, Azure Foundry, AWS Bedrock). API base + key configurable.

Each card shows provider logo/icon, name, and description. Clicking expands it with:
- Credential input field(s) (labeled per provider — "API Key", "Project ID", etc.)
- A "Get an API key" link to the provider's console
- API base URL field (for local/compatible providers)
- A "Verify & Save" button

**Validation flow:**
1. Store credential via `POST /api/v1/creds`
2. Test via `POST /api/v1/creds/{name}/test`
3. Card shows spinner during test, then green check (success) or red error with message (failure)
4. At least one provider must validate successfully to proceed

**Re-setup:** Configured providers show a green check with "Reconfigure" option. Existing keys are never displayed (redacted by API).

**Tier strategy:** After at least one provider is configured, the wizard presents routing strategy:
- **Strict** — requested tier must have a mapped model or the request fails. For cost control.
- **Best effort** — unmapped tiers fall through to the nearest available model. Default for multi-provider setups.
- **Catch-all** — all tiers route to whatever is available. Auto-suggested when only one model is configured.

### Step 3: Your First Agent

**Agent naming:**
- Default name: "Henry"
- A "Generate name" button that suggests random names (lightweight, fun — static list with shuffle)
- Free text input to type any name

**Preset selection:** Dropdown or card picker populated from installed presets (fetched from the API). The wizard pre-selects a general-purpose preset.

**Platform Expert toggle:** Enabled by default. Adds a built-in "Agency Platform Expert" persona to the agent — deep knowledge of Agency's architecture, commands, capabilities, connectors, and common workflows. The agent can answer questions about the platform but cannot modify configuration. Users can uncheck this if they want a vanilla agent.

The platform expert persona is a hub-sourced preset (e.g., `platform-expert` or `concierge`).

**Validation:** The wizard calls the agent create API and then starts the agent. Both must succeed before proceeding.

**Docker unavailable:** If the agent fails to start because Docker is down, the wizard shows a clear message ("Docker is required to run agents") with two options: "Retry" (check again) and "Skip agent creation" (proceed to capabilities without an agent — the Chat step becomes unavailable and the wizard finishes after capabilities).

### Step 4: Capabilities

**Data source:** Hub-sourced wizard config (kind: `setup`) provides the tier definitions. Capability list from `GET /api/v1/admin/capabilities`.

**Three tiers** shown as selectable cards:
- **Minimal** — no optional capabilities. LLM access only.
- **Standard** — curated set of commonly useful capabilities (e.g., brave-search, web-fetch). Defined in the hub's setup config.
- **Custom** — starts from Standard, then shows the full capability list for toggling.

Selecting Minimal or Standard pre-checks the appropriate capabilities in a list below. Selecting Custom pre-checks Standard's capabilities and then expands the full capability list for additional toggling.

**Capabilities that need credentials** (e.g., Brave Search needs a Brave API key) show an inline credential prompt when toggled on. Same store-then-test validation cycle as the Providers step.

**Validation:** Each capability is enabled via `POST /capabilities/{name}/enable`. The API confirms `state: "enabled"` before marking it done. Failures show inline with the reason.

### Step 5: Chat

The "it works!" moment.

An embedded chat panel appears within the wizard, connected to the agent created in Step 3. Uses the same components as the main Channels view (`MessageList`, `MessageInput`, `TypingIndicator`, WebSocket hook) — no separate chat implementation.

A suggested first message is pre-filled (e.g., "What can you help me with?"). The user sends it, sees the agent respond with live streaming and activity indicators.

If the platform expert toggle was enabled, the agent can immediately answer questions like "What did we just set up?" or "How do I add a connector?"

**Two buttons:**
- **Finish Setup** — marks setup complete, redirects to the main app
- **Skip** — for re-setup users who don't need to test

## First-Launch Detection

In the root app layout, before rendering the main UI:
1. Call `GET /api/v1/infra/routing/config`
2. If `configured: false` (no providers), redirect to `/setup`
3. Otherwise, render the app normally

This check runs once on app mount. No explicit `setup_complete` flag — setup state is derived from platform state (has at least one configured provider with a valid model route).

## Re-Setup Access

An entry in Admin: "Setup Wizard" that links to `/setup`. The wizard detects re-setup (providers configured, agents exist) and adjusts:
- Pre-fills all fields with current values
- Shows "Reconfigure" instead of "Add" for providers
- "Skip" is more prominent
- Hub sync is skipped if recent

## Hub Changes

### New component kind: `provider`

Lives in `providers/` directory in agency-hub. Each provider is a YAML file defining display metadata, credential configuration, and routing config including model tiers.

```yaml
# providers/provider-a/provider.yaml
name: provider-a
display_name: Provider A
description: Provider A models
category: cloud
credential:
  name: provider-a-api-key
  label: API Key
  env_var: PROVIDER_A_API_KEY
  api_key_url: https://provider-a.example.com/keys
routing:
  api_base: https://provider-a.example.com/v1
  api_format: openai
  auth_header: Authorization
  auth_prefix: "Bearer "
  models:
    provider-a-frontier:
      provider_model: provider-a-frontier
      cost_per_mtok_in: 5.0
      cost_per_mtok_out: 25.0
    provider-a-standard:
      provider_model: provider-a-standard
      cost_per_mtok_in: 3.0
      cost_per_mtok_out: 15.0
    provider-a-fast:
      provider_model: provider-a-fast
      cost_per_mtok_in: 1.0
      cost_per_mtok_out: 5.0
  tiers:
    frontier: provider-a-frontier
    standard: provider-a-standard
    fast: provider-a-fast
    mini: null
    nano: null
    batch: null
```

```yaml
# providers/ollama/provider.yaml
name: ollama
display_name: Ollama
description: Run open models locally — Llama, Mistral, Gemma
category: local
credential: null
routing:
  api_base: http://localhost:11434/v1
  api_base_configurable: true
  auth_header: null
  models: {}
  tiers: {}
```

Custom or third-party LLM services are represented as real provider adapter
descriptors, not as a shared `openai-compatible` pseudo-provider. If a service
uses the OpenAI wire format, that belongs in its adapter metadata via
`api_format`; provider identity stays specific to the service being configured.

**Install behavior:** `hub install anthropic --kind provider` merges the provider's routing block (api_base, auth config, models, tiers) into the local `~/.agency/routing.yaml`. Remove reverses this.

### New component kind: `setup`

Wizard configuration. Defines capability tiers and other wizard defaults.

```yaml
# setup/default-wizard/setup.yaml
name: default-wizard
kind: setup

capability_tiers:
  minimal:
    display_name: Minimal
    description: LLM access only. No optional tools or services.
    capabilities: []
  standard:
    display_name: Standard
    description: Web search, web fetch, and commonly useful tools.
    capabilities:
      - brave-search
      - web-fetch
```

### Platform expert preset

A hub-sourced preset for the first agent's platform expert persona.

```yaml
# presets/platform-expert/preset.yaml
name: platform-expert
description: Agency platform expert — answers questions about capabilities, commands, architecture, and workflows
capabilities:
  - file_read
  - file_write
  - shell_exec
  - web_search
identity:
  purpose: "Help operators understand and use the Agency platform"
  body: |
    You are an Agency platform expert. You have deep knowledge of how Agency
    works — its architecture, commands, capabilities, connectors, agents,
    presets, missions, the hub, and common workflows.

    You help operators learn the platform, troubleshoot issues, and discover
    features. You answer questions clearly and suggest next steps.

    You have full use of your workspace — you can read, write, and execute
    within it. You do not have platform admin tools (trust elevation,
    credential management, infrastructure control).

    ## What you know about
    - Agent lifecycle (create, start, stop, presets, trust levels)
    - Capabilities (what they are, how to enable/disable, credential requirements)
    - Connectors (what they do, how to set up, credential flow)
    - The Hub (search, install, update, upgrade)
    - Knowledge graph (ontology, nodes, relationships)
    - Missions (assignment, objectives, success criteria)
    - Teams and departments
    - Policy framework and trust model
    - LLM routing (providers, tiers, strategies)
    - Common troubleshooting steps
```

## Backend Changes (agency repo)

### Hub manager

- Add `"provider"` and `"setup"` to `KnownKinds`
- Update `discover()` in `hub.go` to check `doc["provider"]` as a name key (same pattern as the `doc["service"]` fix for service components)
- Provider install: parse `routing` block from provider YAML, merge into `~/.agency/routing.yaml` (additive — add provider config, add models, merge tier entries by preference)
- Provider remove: remove that provider's entries from routing config
- Setup component: no special install behavior — just discoverable via search/info

### Routing config

Add `tier_strategy` to `RoutingSettings`:

```go
type RoutingSettings struct {
    XPIAScan       bool   `yaml:"xpia_scan" default:"true"`
    DefaultTimeout int    `yaml:"default_timeout" default:"300"`
    DefaultTier    string `yaml:"default_tier" default:"standard"`
    TierStrategy   string `yaml:"tier_strategy" validate:"oneof=strict best_effort catch_all" default:"best_effort"`
}
```

Add `"batch"` to `VALID_TIERS`.

Update `ResolveTier` to implement the three strategies:
- **strict:** return nil if requested tier has no entries
- **best_effort:** walk to nearest available tier (down first, then up)
- **catch_all:** return whatever model is available regardless of tier

### New API endpoints

- `GET /api/v1/infra/providers` — returns installed + available (from hub cache) providers with credential status (configured/unconfigured) and category. Merges hub cache with installed state.
- `GET /api/v1/infra/setup/config` — returns the hub-sourced wizard configuration (capability tiers, defaults)

## Frontend Architecture (agency-web)

### New files

```
src/app/screens/
  Setup.tsx                  # Main wizard — step state, transitions, navigation
  setup/
    HubSyncStep.tsx          # Step 0 — hub update/upgrade with progress
    WelcomeStep.tsx          # Step 1 — operator name, init
    ProvidersStep.tsx         # Step 2 — provider catalog, credentials, tier strategy
    AgentStep.tsx            # Step 3 — name (with generator), preset, expert toggle
    CapabilitiesStep.tsx     # Step 4 — tier cards, toggleable capability list
    ChatStep.tsx             # Step 5 — embedded chat, finish/skip
```

### Routing

```tsx
{ path: '/setup', element: <Setup /> }
```

### State management

A `useReducer` in `Setup.tsx`:

```ts
type WizardState = {
  step: number
  operatorName: string
  providers: Map<string, ProviderStatus>
  tierStrategy: 'strict' | 'best_effort' | 'catch_all'
  agentName: string
  agentPreset: string
  platformExpert: boolean
  capabilities: string[]
}
```

Each step component receives the relevant slice of state and a dispatch function. Steps own their own API calls and dispatch results. No global store, no context.

### API client additions (`api.ts`)

```ts
api.credentials.list(filters?)        // GET /credentials
api.credentials.store(name, value, opts)  // POST /credentials
api.credentials.test(name)            // POST /credentials/{name}/test

api.providers.list()                  // GET /providers
api.setup.config()                    // GET /setup/config
```

### Shared components

`ChatStep` reuses `MessageList`, `MessageInput`, `TypingIndicator`, and the WebSocket hook from the existing channels feature. No new chat implementation.

## ASK Compliance Notes

**Tenet 7 (Least privilege):** Agents have full access to their own workspace — read, write, execute, use tools. This is expected and normal, the same way an employee has full use of their own laptop. Least privilege applies at the *platform boundary*, not within the workspace. The platform expert agent cannot modify platform configuration, access other agents' workspaces, manage credentials, or control infrastructure — not because its workspace capabilities are restricted, but because it lacks platform admin tools. Policy can further constrain workspace capabilities for specific use cases, but the default is practical, not maximally restrictive.

**Tenet 8 (Operations are bounded):** The `catch_all` tier strategy can cause unexpectedly high costs if the only configured model is a frontier-tier model. The wizard should display a cost warning when auto-suggesting catch-all with an expensive model, noting that all requests will route to it regardless of task complexity.

**Tenet 17 (Trust is earned):** The first agent created by the wizard starts at the platform's default (lowest) trust level. The wizard does not elevate trust. Operators can elevate trust later through Admin > Trust.

**Tenet 5/24 (Runtime integrity, verified principals):** The platform expert preset is hub-sourced and subject to the same integrity verification as all hub components. Its system prompt constitutes instructions to the agent and must come from a verified source (the configured hub). This is not a new trust boundary — it's the same one all presets, connectors, and services use.

## Navigation & Chrome

- **Progress:** Minimal dot indicators (one per step). No "Step 3 of 6" labels.
- **Back/Forward:** Subtle text links. Back always available. Forward disabled until current step validates.
- **Transitions:** Smooth, spacious. Each step owns the full viewport.
- **Skip:** Available at every step for re-setup users. Visible but not prominent on first launch.

## Validation Summary

| Step | What's validated | How | Gate |
|------|-----------------|-----|------|
| Hub Sync | Hub update + upgrade succeed | API response status | Non-skippable on first launch |
| Welcome | Operator name format, init API | Inline regex + `POST /init` response | Init must succeed |
| Providers | Each credential stores and tests | `POST /credentials` then `POST /credentials/{name}/test` | At least one green check |
| Agent | Agent creates and starts | Create + start API responses | Agent must be running |
| Capabilities | Each capability enables | `POST /capabilities/{name}/enable`, confirm state | All toggled caps must be enabled |
| Chat | Agent responds | WebSocket message received | None — this step is optional |
