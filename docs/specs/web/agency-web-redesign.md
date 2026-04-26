---
description: "Comprehensive UI improvements across channel sidebar, Admin tabs, and knowledge graph. All designs follow the existin..."
status: "Implemented"
---

# Agency Web UI Redesign

**Date:** 2026-03-29
**Status:** 7/8 Implemented — Section 8 (Egress domain merge) pending backend API support

Comprehensive UI improvements across channel sidebar, Admin tabs, and knowledge graph. All designs follow the existing "Mission Control" theme: deep slate-blue backgrounds, cyan primary accents, DM Sans + JetBrains Mono typography, instrument-panel label patterns.

---

## 1. Channel Sidebar — Slack-style Groups

Replace the flat channel list with grouped sections.

### Structure

```
Channels                               +
  # general
  # operator
  # security-findings          3
  + Add channel

Direct Messages
  ● alert-triage              AGENT
  ● henrybot9000              AGENT
  ● security-explorer         AGENT

▶ Internal (collapsed)
  # _knowledge-updates
```

### Behavior

- **Channels section**: all non-DM, non-internal channels. `#` prefix. Unread badge (count) on right using `--primary` cyan, not Discord purple.
- **Direct Messages section**: DM channels (`dm-*` prefix stripped). Status dot using `--chart-3` green for running, `--destructive` red for stopped. `AGENT` badge in `text-[10px] bg-secondary`.
- **Internal section**: channels starting with `_`. Collapsed by default. Header in `text-muted-foreground/50`.
- **Section headers**: `text-[10px] uppercase tracking-widest text-muted-foreground/60` — instrument-panel labels, matching existing patterns.
- **Sections are collapsible** via clicking the header.
- "+ Add channel"** link at bottom of Channels section — opens channel create dialog. No separate Browse/Compass button.
- Channel grouping logic: `type === 'dm'` or name starts with `dm-` → DMs. Name starts with `_` → Internal. Everything else → Channels.

### Data Source

`GET /channels` returns all channels (no membership filter). The sidebar groups them client-side.

### Files

- Modify: `src/app/components/chat/ChannelSidebar.tsx`
- Modify: `src/app/screens/Channels.tsx` (channel list loading)

---

## 2. Audit — Log Viewer

Replace the sparse audit table with a monospace log viewer. This should feel like reading `journalctl` with color coding.

### Layout

Sticky filter bar at top. Scrollable log below.

**Filters**: agent dropdown, entry type dropdown (LLM_DIRECT, TOOL_CALL, MEDIATION, etc.).

**Default**: today's audit entries from all agents, newest first.

### Entry Format

Each line in `JetBrains Mono` at `text-xs` (12px):

```
HH:MM:SS  agent-name  TYPE  detail
```

Detail is type-specific:
- `LLM_DIRECT`: model, input tokens, output tokens, estimated cost (cost in `text-muted-foreground` — secondary to token count)
- `TOOL_CALL`: tool name, arguments summary
- `MEDIATION`: endpoint, method, status code

### Color Scheme

Use existing chart/theme colors for entry types:
- `--chart-1` cyan for `LLM_DIRECT`
- `--chart-3` green for `TOOL_CALL`
- `--chart-2` amber for `MEDIATION`
- `--destructive` red for errors

### API

Current `GET /admin/audit` may need to be extended to return parsed JSONL entries. Check if the gateway already parses audit logs or if the web UI needs to fetch raw JSONL and parse client-side.

### Files

- Modify: `src/app/screens/Admin.tsx` (Audit section)
- Possibly modify: `agency-gateway/internal/api/` (if audit log endpoint needs enrichment)

---

## 3. Intake — Connector Detail + Work Items

### Connectors Tab

Replace the flat table with expandable rows.

**Collapsed row**: status dot, name, kind badge, poll interval + last poll time (relative, `font-mono`), version badge (`font-mono text-[10px] bg-secondary`), expand chevron.

**Expanded row** (below the collapsed row, `bg-background` within `bg-card`):
- Grid layout with `text-[10px] uppercase tracking-wide` labels above values — same pattern as Usage stats cards
- Source type and interval
- Graph ingest: rule count and match summary
- Routes: target type and name per route
- Action buttons: Setup, Deactivate

### Work Items Tab

**Stats bar** at top: Total, Routed, Relayed, Unrouted — as large number cards matching the Usage page pattern.

**Work item rows**: connector name, status badge (routed/relayed/unrouted), route target (e.g., "→ alert-triage"), relative timestamp, payload preview (one-line truncated).

**Click to expand**: shows full JSON payload in a `<pre>` block using `JetBrains Mono` in `bg-background`. Syntax-highlight if feasible (keys in `text-muted-foreground`, string values in `text-foreground`).

### Data Sources

- Connectors: `GET /hub/instances?kind=connector` for state/version, connector YAML for routes/graph_ingest (via `/connectors/{name}/requirements` or `/hub/{nameOrID}` info)
- Work items: `GET /events/intake/items` for work item list, `GET /events/intake/stats` for totals

### Files

- Modify: `src/app/screens/Intake.tsx`

---

## 4. Hub Tab — Homebrew-style Update/Upgrade

### Changes

- "Update Sources" button** stays — calls `POST /hub/update`.
- "Upgrade All" button** added next to it — calls `POST /hub/upgrade`.
- **Upgrade banner** appears after Update Sources when `report.available.length > 0`. Styled with `bg-green-950/30 border-green-900/50` — subtle, not loud. Shows count and summary. "Upgrade" button in the banner calls `/hub/upgrade`.
- **Browse tab**: installed components show green checkmark + version badge (`font-mono text-[10px] bg-secondary`) instead of "Install" button. Uninstalled show "Install" button. Wait for installed data to load before rendering browse results. Browse is discovery, Installed is management — different views of different data, no duplication.
- **Installed tab**: show version, state (active/installed), kind badge, source. From `/hub/instances`.

### API Endpoints

- `POST /hub/update` → `UpdateReport` (sources, available upgrades, warnings)
- `POST /hub/upgrade` → `UpgradeReport` (files, components, warnings)
- `GET /hub/outdated` → `[]AvailableUpgrade`
- `GET /hub/instances` → installed instances with version, state, kind

### Files

- Modify: `src/app/screens/Hub.tsx`
- Modify: `src/app/lib/api.ts` (add update/upgrade/outdated response types)

---

## 5. Events — Simplified Feed

Replace the raw event table with a clean feed that feels alive.

**Each row**: colored status dot (by severity — green=normal, amber=warning, red=error), agent/source name (bold), event type, brief detail string, relative timestamp. One line per event.

**Filters**: dropdown for event type (all, agent_signal, connector, platform), dropdown for agent.

**Remove**: raw JSON "DATA" column. Event detail is the one-line summary, not a JSON blob.

**Detail on click**: expand to show full event payload (like work items).

### Files

- Modify: `src/app/screens/Admin.tsx` (Events section)

---

## 6. Knowledge Graph — Filtered Force-Directed

### Changes

- **Default view**: Force-directed (not radial).
- **Kind filter toggles**: small pills with colored dot matching node color. Active = filled background, inactive = outline only. Gives instant visual mapping between legend and filter state.
- **Max nodes cap**: input field (default 100) — limits nodes rendered. Prevents browser freeze on large graphs.
- **Fit button**: re-centers and auto-zooms to fit visible nodes.
- **Initial load animation**: subtle zoom from slightly zoomed-in to fit view (0.5s ease-out).
- **Node sizing**: encode connection count — more connected nodes are larger.
- **Labels on hover**: show tooltip card (not just text) with label, kind, connection count. Kind's color accent on the card border.
- Keep radial, timeline, grid as alternative views.

### Files

- Modify: `src/app/screens/KnowledgeExplorer.tsx` (graph view section)

---

## 7. Doctor — Summary Cards

Replace the vertical list of all checks per agent with a status board.

**Summary view** (default):
- Overall status: single line at top — `"3 agents · all checks passing"` in `text-muted-foreground` with green dot. Not a large banner.
- Grid of agent cards with `border-l-[3px]` in status color (green/amber/red). Shows `passed/total` as large number with agent name below.
- Cards are clickable — expands to show the full check list for that agent.

### Files

- Modify: `src/app/screens/Admin.tsx` (Doctor section)

---

## 8. Egress — Show Platform Domains

The Domain Provenance table works but shows empty when no domains are auto-managed. The egress policy (`egress/policy.yaml`) has manually-configured domains that should also appear.

**Change**: merge domain-provenance (auto-managed) with egress policy domains. Show all domains with a "managed by" indicator: "auto" (from connector activation) or "policy" (from egress/policy.yaml).

### Files

- Modify: `src/app/screens/Admin.tsx` (Egress section)
- Possibly modify: `agency-gateway/internal/api/` (egress domains endpoint may need to merge policy.yaml)

---

## 9. Capabilities — No Changes

Currently working correctly. `limacharlie-api` shows as `available`, others as `disabled`. No changes needed.

---

## Priority Order

1. Channel sidebar (highest user impact)
2. Audit log viewer (most broken, clearest design)
3. Intake connectors + work items
4. Hub update/upgrade UX
5. Events feed simplification
6. Knowledge graph controls
7. Doctor summary cards
8. Egress domain merge

---

## Design Principles

All components follow existing patterns:
- **Labels**: `text-[10px] uppercase tracking-widest text-muted-foreground/60`
- **Data readouts**: `font-mono text-xs` for values, IDs, timestamps
- **Version badges**: `font-mono text-[10px] bg-secondary px-1.5 py-0.5 rounded`
- **Status dots**: 6-8px circles using theme status colors
- **Cards**: `bg-card border border-border rounded` with optional `border-l-[3px]` for status accent
- **Expanded detail**: `bg-background` (darker than card) for nested content
- **Colors**: use `--chart-1` through `--chart-5` and status colors, not arbitrary hex values

---

## Scope

**In scope**: all items listed above (sections 1-8).

**Not in scope**:
- Agent "Last Active" field — requires gateway-side change to track and populate
- Agent detail view redesign
- Mission detail view
- New pages or navigation changes beyond channel sidebar
