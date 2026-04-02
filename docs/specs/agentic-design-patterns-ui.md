---
description: "Agency added 6 agentic design pattern features to the backend. The gateway API, OpenAPI spec, and MCP tools are updat..."
status: "Approved"
---

# Agentic Design Patterns UI

**Date:** 2026-03-27
**Status:** Approved
**Scope:** agency-web — UI for 6 new agentic design pattern features

## Context

Agency added 6 agentic design pattern features to the backend. The gateway API, OpenAPI spec, and MCP tools are updated. Agency-web needs UI to expose these features to operators.

### New Gateway Endpoints

All GET, documented in OpenAPI spec at `/api/v1/openapi.yaml`:

- `GET /api/v1/agents/{name}/procedures` — procedural memory records (filters: mission, outcome)
- `GET /api/v1/agents/{name}/episodes` — episodic memory records (filters: mission, from, to, outcome, tag)
- `GET /api/v1/agents/{name}/trajectory` — trajectory monitor state
- `GET /api/v1/missions/{name}/procedures` — procedures for a mission (filter: agent)
- `GET /api/v1/missions/{name}/episodes` — episodes for a mission
- `GET /api/v1/missions/{name}/evaluations` — success criteria evaluation results (filter: limit)

### New Mission YAML Fields

Returned by existing `GET /api/v1/missions/{name}`:

- `cost_mode` — frugal | balanced | thorough
- `reflection` — { enabled, max_rounds, criteria }
- `success_criteria` — { checklist: [...], evaluation: { enabled, mode, on_failure } }
- `fallback` — { policies: [...], default_policy }
- `procedural_memory` — { capture, retrieve, max_retrieved }
- `episodic_memory` — { capture, retrieve, max_retrieved, tool_enabled }
- `min_task_tier` — minimal | standard | full

### New WebSocket Signal Types

- `reflection_cycle` — { round, verdict, issues }
- `fallback_activated` — { trigger, tool, policy_steps }
- `trajectory_anomaly` — { detector, detail, severity }
- `task_evaluation_failed` — { mode, on_failure, criteria }

### New task_complete Signal Fields

- `reflection_rounds`, `reflection_forced`
- `evaluation` — { passed, mode, criteria_results }
- `tier` — which task tier was used

## Architecture Decision: Approach B — Component Decomposition

Decompose the Agents page into `AgentList` + `AgentDetail` components. The parent `Agents.tsx` becomes a thin layout shell. New tab groups become their own components. This keeps each file focused without premature abstraction.

For Missions, changes are lighter — adding tabs to MissionDetail and a wizard step — so those stay as modifications to existing files with extracted section components.

## Design

### 1. Agents Page — Master-Detail Split

**Layout:** Horizontal split — top ~25% is the agent table, bottom ~75% is the detail panel for the selected agent. No route change; `/agents` shows the table with no selection, `/agents/:name` shows table + detail.

#### Top Panel — Agent Table (AgentList.tsx)

Compact table with columns:

| Column | Content |
|--------|---------|
| Name | Agent name, clickable |
| Status | Dot indicator + label |
| Team | Team name |
| Mission | Current mission name |
| Activity | Sparkline — recent activity from agent audit log, backfilled on load via `api.agents.logs()`, supplemented by live WebSocket signals. If audit log is unavailable, fall back to a dot-density indicator showing signal count in the current session. |
| Budget | Thin inline progress bar (red >95%, amber >80%, primary otherwise) |
| Last Active | Relative timestamp |

- Selected row gets `bg-primary/5` highlight
- Clicking a row navigates to `/agents/:name` and populates the detail panel
- Table scrolls independently

#### Bottom Panel — Grouped Tabs (AgentDetail.tsx)

Four tab groups in a primary tab bar:

**Overview** — Agent summary grid (mode, enforcer, role, model, preset, trust, team, type, mission, build, last_active), budget detail bars, status.

**Activity** group (AgentActivityGroup.tsx):
- Activity feed — existing signals + new signal type renderers (see Section 3)
- Memory — procedures table, episodes table, trajectory status (see Section 2)

Sub-sections within a group render as secondary tabs (smaller, below the primary tab bar). For Activity: "Feed" and "Memory" secondary tabs. For Operations: "Channels", "Knowledge", "Meeseeks" secondary tabs. For System: "Config" and "Logs" secondary tabs. Overview has no secondary tabs. Each group defaults to its most-used secondary tab on first click (Activity→Feed, Operations→Channels, System→Config) so the first click always shows content, never an empty state.

**Operations** group:
- Channels, Knowledge, Meeseeks (existing content, moved into group)

**System** group:
- Config, Logs (existing content, moved into group)

#### Component Decomposition

- `Agents.tsx` — thin shell, manages split layout + selected agent state
- `AgentList.tsx` — compact table with sparklines/indicators
- `AgentDetail.tsx` — grouped tab panel
- `AgentActivityGroup.tsx` — activity feed + memory sub-sections
- `AgentMemoryPanel.tsx` — procedures, episodes, trajectory

### 2. Agent Memory Panel (AgentMemoryPanel.tsx)

Three sub-sections within the Activity group's Memory view.

#### Procedures Table

- **Columns:** Timestamp | Mission | Outcome (badge: green/amber/red for success/partial/failed) | Duration | Approach (truncated)
- **Expanded row:** Full approach text, tools_used as badges, lessons as bullet list
- **Data:** `GET /agents/{name}/procedures`
- **Filter:** Outcome dropdown (all/success/partial/failed) using API query param

#### Episodes Table

- **Columns:** Timestamp | Mission | Outcome (badge, includes "escalated" as fourth state) | Tone (badge: muted=routine, amber=notable, red=problematic) | Summary | Events (count badge, e.g. "3 events" — draws attention to rows worth expanding)
- **Inline preview:** First notable event shown truncated in the table row itself, so the highest-value data is visible without expanding.
- **Expanded row:** Full notable events as Alert-styled list (amber background, each event a line item). Entities mentioned as small badges (type: name). Tags as muted badges.
- **Data:** `GET /agents/{name}/episodes`
- **Notable events are the highest-value field** — visible inline, not hidden behind expand.

#### Trajectory Status

Live status card (not a table):

- **Header:** "Trajectory Monitor" with enabled/disabled badge, window size, current entry count
- **Detector list:** Each detector as a row — name, status badge, last_fired timestamp
- **Active anomalies:** Alert components — warning=amber, critical=red with glow effect (`shadow-[0_0_6px_hsl(0,72%,55%,0.4)]`) and a subtle pulse animation (not ping — that's reserved for connection states). Critical anomalies should feel like a cockpit warning. Each shows: detector, detail text, severity badge, first_detected timestamp
- **Refresh:** Polling every 30s or via WebSocket `trajectory_anomaly` signals
- **Data:** `GET /agents/{name}/trajectory`

All three lazy-loaded when the Memory sub-section becomes active.

### 3. Agent Activity Feed — New Signal Types

New cases in the existing signal renderer:

**`reflection_cycle`**
- Display: "Reflection round {round}: {verdict}"
- Issues array shown as indented bullet list below
- Style: muted/informational when verdict="approved", amber when "revision-needed"

**`fallback_activated`**
- Display: "Fallback: {trigger} on {tool}"
- Policy steps as compact action badges (retry, degrade, escalate, etc.)
- Style: amber warning

**`trajectory_anomaly`**
- Display: "Trajectory: {detail}"
- Severity drives style: amber=warning, red=critical
- Detector name as muted label

**`task_complete` enrichments** (extends existing renderer):
- `reflection_rounds > 0`: append "(reflected {n}x)". If `reflection_forced`: small "forced" badge
- `evaluation.passed === false`: "(evaluation: {n} criteria failed)" in amber. If passed: "(evaluation: passed)" in green
- `tier`: small badge (minimal/standard/full)

### 4. Mission Detail — Tabbed Layout

Current page becomes three tabs.

#### Overview Tab (existing content + minor additions)

- Name, description, instructions (inline-editable, unchanged)
- Assigned to, triggers, requirements, budget (unchanged)
- **Cost mode badge** added prominently near the top, next to status badge. Tooltip explains each mode.
- Min task tier shown as small badge near budget
- Collapsible YAML preview stays here

#### Quality Tab (new — tuning knobs)

All read-only. Editing via existing YAML update flow. Two-column grid layout to avoid a wall of text.

- **Cost Mode** — prominent header card at the top, full-width. Badge + description of what this mode enables. This leads the tab visually.
- **Reflection** + **Memory Config** — side by side in two-column grid (lighter sections). Reflection: enabled/disabled badge, max_rounds, criteria as bullet list. Memory: procedural + episodic settings as key-value grid (capture, retrieve, max_retrieved, tool_enabled).
- **Success Criteria** — full-width below. Checklist items as list with required/optional badges. Evaluation config below (mode, on_failure, max_retries).
- **Fallback Policies** — full-width. Collapsible list. Each policy: trigger badge → strategy chain shown as flow (retry → degrade → escalate). Default policy shown separately at bottom.

#### Health Tab (new — observability)

- **Evaluation pass rate** — Recharts stacked area chart (green=passed, red=failed) from `GET /missions/{name}/evaluations`. Area charts communicate volume better than lines for pass/fail data. Use chart color tokens: chart-3 (green) for passed, chart-5 (red) for failed.
- **Recent evaluations** — table: task_id, passed (green/red badge), mode, per-criterion results expandable
- **Task tier distribution** — horizontal stacked bar chart showing minimal/standard/full counts (derived from evaluation data). Use chart color tokens: chart-2 (amber) for minimal, chart-1 (cyan) for standard, chart-3 (green) for full. Horizontal bar reads better than donut with only 3 segments.
- **Mission procedures** — same table pattern as agent memory, from `GET /missions/{name}/procedures`
- **Mission episodes** — same pattern, from `GET /missions/{name}/episodes`

### 5. Mission Wizard — Cost & Quality Step

New step inserted between Requirements and Review (step 5 of 6).

#### Primary Control: Cost Mode Selector

Three horizontal option cards (like existing template picker in StepBasics):

**Frugal:**
- Reflection: off
- Evaluation: off
- Memory: episodic only
- Task tier: minimal

**Balanced:**
- Reflection: on (2 rounds)
- Evaluation: checklist
- Memory: both
- Task tier: standard

**Thorough:**
- Reflection: on (5 rounds)
- Evaluation: LLM
- Memory: both + tool
- Task tier: full

Selected card gets `border-primary` highlight.

Each card's visual weight communicates the cost/quality tradeoff before reading the text:
- **Frugal:** Thin border, muted background, minimal styling — visually "light"
- **Balanced:** Standard card weight, default border
- **Thorough:** Richer background (`bg-primary/5`), subtle glow or accent border — visually "heavier"

#### Expandable Advanced Section (collapsed by default)

- **Reflection:** toggle + max_rounds number input + criteria tag input
- **Success Criteria:** checklist item editor (add/remove, toggle required) + evaluation mode dropdown + on_failure dropdown
- **Fallback:** trigger type dropdown + strategy dropdown per policy, add/remove
- **Memory:** toggles for procedural/episodic capture/retrieve/tool_enabled, max_retrieved inputs

#### Behavior

- Selecting cost_mode pre-fills all Advanced fields with preset defaults
- Modifying Advanced fields shows "Custom" indicator on cost_mode selector without resetting changes
- Switching cost_mode after customizing shows confirm: "Reset to {mode} defaults?"

#### WizardState Additions

```typescript
cost_mode: 'frugal' | 'balanced' | 'thorough'
reflection: { enabled: boolean; max_rounds: number; criteria: string[] }
success_criteria: {
  checklist: { id: string; description: string; required: boolean }[];
  evaluation: { enabled: boolean; mode: string; on_failure: string }
}
fallback: { policies: FallbackPolicy[] }
procedural_memory: { capture: boolean; retrieve: boolean; max_retrieved: number }
episodic_memory: { capture: boolean; retrieve: boolean; max_retrieved: number; tool_enabled: boolean }
```

### 6. Mission List — Cost Mode Badge

Mission list cards get a cost_mode badge next to the existing status badge:

- **Frugal:** muted/secondary (low-cost default, shouldn't draw attention)
- **Balanced:** blue/primary
- **Thorough:** amber (signals higher cost, worth noticing)

Missions without cost_mode (legacy) show no badge.

## API Integration

### New API Client Methods (lib/api.ts)

```typescript
agents: {
  procedures: (name: string, params?: { mission?: string; outcome?: string }) => req(...)
  episodes: (name: string, params?: { mission?: string; from?: string; to?: string; outcome?: string; tag?: string }) => req(...)
  trajectory: (name: string) => req(...)
}
missions: {
  procedures: (name: string, params?: { agent?: string }) => req(...)
  episodes: (name: string) => req(...)
  evaluations: (name: string, params?: { limit?: number }) => req(...)
}
```

### New TypeScript Types (types.ts)

Types derived from OpenAPI schemas:

- `ProcedureRecord` — task_id, agent, mission_id, mission_name, task_type, timestamp, approach, tools_used, outcome, duration_minutes, lessons
- `EpisodeRecord` — task_id, agent, mission_name, timestamp, duration_minutes, outcome, summary, notable_events, entities_mentioned, operational_tone, tags
- `TrajectoryState` — agent, enabled, window_size, current_entries, active_anomalies[], detectors{}
- `EvaluationResult` — task_id, passed, evaluation_mode, model_used, criteria_results[], evaluated_at
- `MissionReflection` — enabled, max_rounds, criteria[]
- `MissionSuccessCriteria` — checklist[], evaluation{}
- `MissionFallback` — policies[], default_policy

### 7. Text Scale Control

Global text size control for accessibility and mobile usability. The app already uses a CSS variable `--font-size` (base 15px) in `theme.css`. This feature exposes that as a user preference.

#### Implementation

- **CSS variable approach:** All type sizes derive from the `--font-size` base. Changing this one variable scales the entire UI proportionally.
- **Control:** A scale slider or step buttons (A- / A+) in the sidebar footer or a settings popover. Steps: 12px, 13px, 15px (default), 17px, 19px, 22px. Labels like "XS, S, M, L, XL, XXL" are friendlier than pixel values.
- **Persistence:** Save to `localStorage` (key: `agency-text-scale`). Apply on page load before first render to avoid flash of wrong size.
- **Mobile default:** On viewports below 640px, default to one step larger (17px) unless the user has explicitly set a preference. Small screens held at arm's length need larger text.
- **Scope:** Applies to all text — body, labels, badges, table cells, monospace. Does not scale icons, spacing, or layout breakpoints (those stay fixed so the layout doesn't break at larger sizes).

#### Where to Surface

- Sidebar footer: small "Aa" button that opens a popover with the scale slider
- Respects `prefers-reduced-motion` — if the user has that OS setting, skip the transition when changing scale. Otherwise, a brief `transition: font-size 150ms` on `<html>` for a smooth resize.

## ASK Framework Compliance

**Authorization scoping (Tenets 4, 24):** The UI renders whatever the gateway API returns — the gateway is responsible for filtering data based on the authenticated principal's authorization scope. The UI does not implement its own auth layer. All new endpoints (procedures, episodes, trajectory, evaluations) must handle 403 responses gracefully: display "You don't have access to this agent's memory" or similar contextual message rather than a generic error.

**Anomaly→halt visual linking (Tenets 8, 9):** When a `trajectory_anomaly` signal with critical severity is immediately followed by a halt event in the activity feed, render them as a visually connected sequence (e.g., shared left border or grouping) so operators see the causal chain: anomaly detected → halt triggered. This supports halt auditability.

**Data provenance (Tenets 2, 25):** Memory records (procedures, episodes) displayed in the UI are sourced from the gateway's mediation layer, not from agent self-reporting. The gateway writes these records as part of its enforcement audit trail. The UI is a consumer of immutable audit data. This distinction matters: agents cannot fabricate or modify their own memory records.

## Design Notes

- All new UI is read-only. Editing mission config goes through existing YAML update flow.
- Use existing shadcn/ui components: Badge, Card, Table patterns, Alert for anomalies.
- Recharts for evaluation pass rate and tier distribution charts.
- Lazy-load tab/section data on activation (existing pattern).
- WebSocket subscriptions for real-time signal updates (existing pattern).
- Frontend-design skill should be used during implementation for design review.
