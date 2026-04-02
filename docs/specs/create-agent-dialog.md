## Problem

The agency-web Agents screen has no UI for creating agents. The API client method `api.agents.create(name, preset, mode)` exists but nothing calls it. Users must fall back to the CLI.

## Solution

Add a "Create Agent" dialog to the Agents screen, backed by a new gateway endpoint for listing available presets.

## Components

### 1. Gateway Endpoint: `GET /api/v1/presets`

**File:** `agency-gateway/internal/api/routes.go`

Reads preset YAML files from `~/.agency/presets/` (populated by `agency setup`, which copies bundled presets). The handler scans the directory, parses each YAML, and returns a JSON array:

```json
[
  { "name": "generalist", "description": "Proactive generalist assistant with broad tool access", "type": "standard" },
  { "name": "engineer", "description": "Code development and debugging", "type": "standard" }
]
```

Each preset YAML has top-level `name`, `type`, and `description` fields. The `type` field (standard/coordinator/function) is returned for informational purposes but is not user-selectable — it is determined by the preset.

**Prerequisite:** `agency setup` must have been run so that `~/.agency/presets/` is populated. If the directory is missing or empty, the endpoint returns an empty array.

**Implementation notes:**
- Add a `PresetsDir()` helper to `config.go` returning `filepath.Join(cfg.Home, "presets")`.
- The handler body lives in `routes.go` alongside existing handlers (consistent with codebase pattern).

**Route registration:** Add `r.Get("/presets", h.listPresets)` to the router.

### 2. API Client Method: `api.presets.list()`

**File:** `agency-web/src/app/lib/api.ts`

```ts
presets: {
  list: () => req<{ name: string; description: string; type: string }[]>('/presets'),
},
```

### 3. Component: `CreateAgentDialog`

**File:** `agency-web/src/app/components/CreateAgentDialog.tsx`

**Props:**

| Prop | Type | Description |
|------|------|-------------|
| `open` | `boolean` | Controls dialog visibility |
| `onOpenChange` | `(open: boolean) => void` | Close handler |
| `onCreated` | `() => void` | Callback after successful creation |

**Form fields:**

| Field | Control | Default | Validation |
|-------|---------|---------|------------|
| Name | Text input | empty | Required. 2+ chars. Pattern: `^[a-z0-9][a-z0-9-]*[a-z0-9]$`. Not a reserved name (infra-egress, agency, enforcer, gateway, workspace). Inline error shown below input. |
| Preset | Select dropdown | "generalist" | Fetched from `GET /api/v1/presets`. Each option shows name + description. |

**Preset fetching:**
- Fetches on dialog open.
- Shows loading state in the Select while fetching.
- On fetch error, falls back to a plain text input for preset name (degraded but functional). Note: a typo in the fallback input could create an agent with a nonexistent preset, which would fail at `agency start` time. This is an acceptable tradeoff since the fallback is a rare error path.

**Submission behavior:**
- Calls `api.agents.create(name, preset)`.
- On success: toast notification ("Agent created"), calls `onCreated()`, closes dialog, resets form state.
- On error: toast with error message, dialog stays open.
- "Create" button and all inputs disabled during submission. Button shows spinner.

**UI components used:** Dialog, Select, Input, Label, Button (all existing shadcn/ui).

### 4. Integration into Agents Screen

**File:** `agency-web/src/app/screens/Agents.tsx`

- Add `+ Create Agent` button in the header bar, next to the refresh and group-by controls.
- Add `useState<boolean>` for dialog open state.
- Render `<CreateAgentDialog>` with `onCreated` wired to the existing `load()` function.

## Validation Rules

**Client-side (before submit):**
- Name: check length first (2+ chars, for a clear error message), then pattern `^[a-z0-9][a-z0-9-]*[a-z0-9]$`, then not reserved.
- Preset/mode: always have defaults, no validation needed.

**Server-side errors:**
- Duplicate agent name, invalid preset, etc. — displayed via toast, dialog stays open.

## Testing

**File:** `agency-web/src/test/CreateAgentDialog.test.tsx`

**Unit tests (Vitest + React Testing Library):**
- Renders all form fields when open
- Validates name input (shows inline error for invalid names)
- Calls `api.agents.create` with correct parameters on submit
- Calls `onCreated` callback and closes dialog on success
- Shows error toast and stays open on failure
- Fetches presets on dialog open
- Shows fallback text input when preset fetch fails

**MSW mock handlers:**
- `GET /api/v1/presets` — returns sample preset list
- `POST /api/v1/agents` — success and error scenarios

## Files Changed

| File | Change |
|------|--------|
| `agency-gateway/internal/api/routes.go` | Add `GET /presets` route and `listPresets` handler |
| `agency-web/src/app/lib/api.ts` | Add `presets.list()` method |
| `agency-web/src/app/components/CreateAgentDialog.tsx` | New file |
| `agency-web/src/app/screens/Agents.tsx` | Add create button + dialog integration |
| `agency-web/src/test/CreateAgentDialog.test.tsx` | New file |
