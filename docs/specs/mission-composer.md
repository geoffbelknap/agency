# Mission Composer — Visual Mission Builder

## Status: Approved

## Summary

A visual canvas editor in agency-web for composing missions. Available as an "advanced mode" toggle from the existing MissionWizard. Uses React Flow for the node graph. An extensible node registry supports trigger, agent, output, modifier, and hub component nodes. Canvas layout is stored as `.canvas.json` alongside the mission YAML. The mission YAML remains the runtime artifact — generated from the canvas definition.

Phase 1 of a two-phase effort. Phase 2 (Workflow Engine) adds multi-step pipelines with chained agent tasks, conditionals, and data flow between steps.

## Node Registry

Each node type is a registered definition:

```typescript
interface NodeDefinition {
  typeId: string;           // e.g., "trigger/schedule", "hub/connector"
  category: NodeCategory;   // trigger | agent | output | modifier | hub
  label: string;            // display name
  icon: string;             // icon identifier
  ports: {
    inputs: PortDef[];      // connection points accepting edges
    outputs: PortDef[];     // connection points emitting edges
  };
  configSchema: ConfigField[];  // fields rendered in property panel
  serialize: (data: NodeData) => MissionFragment;  // contributes to mission YAML
  validate: (data: NodeData, connections: Edge[]) => ValidationError[];
}

interface PortDef {
  id: string;
  type: PortType;           // "trigger" | "agent" | "output" | "modifier" | "data"
  label?: string;
  multiple?: boolean;       // accept multiple connections (default false)
}

type NodeCategory = "trigger" | "agent" | "output" | "modifier" | "hub";
```

Ports have typed compatibility — triggers connect to agents, agents connect to outputs, modifiers attach to agents. Invalid connections are rejected visually (React Flow `isValidConnection`).

### Initial Node Types

| Category | Type ID | Ports In | Ports Out | Config Fields |
|----------|---------|----------|-----------|---------------|
| **Trigger** | `trigger/schedule` | — | trigger | cron expression, timezone |
| **Trigger** | `trigger/webhook` | — | trigger | name (auto-generates URL + secret) |
| **Trigger** | `trigger/connector-event` | — | trigger | connector instance, event_type filter |
| **Trigger** | `trigger/channel-pattern` | — | trigger | channel name, glob pattern |
| **Trigger** | `trigger/platform-event` | — | trigger | event name |
| **Agent** | `agent` | trigger, modifier | output | preset, model, instructions, budget (daily/monthly/per_task), cost_mode, meeseeks config |
| **Output** | `output/channel-post` | output | — | channel name |
| **Output** | `output/webhook-call` | output | — | URL, headers |
| **Output** | `output/escalation` | output | — | severity, message template |
| **Modifier** | `modifier/fallback-policy` | — | modifier | trigger type, strategy chain |
| **Modifier** | `modifier/success-criteria` | — | modifier | checklist items, evaluation mode, on_failure |
| **Modifier** | `modifier/reflection` | — | modifier | enabled, max_rounds, criteria |
| **Modifier** | `modifier/budget-limits` | — | modifier | daily, monthly, per_task |
| **Hub** | `hub/connector` | — | trigger | instance selector (from installed connectors), event_type filter |
| **Hub** | `hub/skill` | — | modifier | skill selector (from installed skills) |

New node types are added by registering a `NodeDefinition` in the registry. No canvas core changes needed.

## Canvas Data Model

### Storage

```
~/.agency/missions/{name}.yaml          ← runtime mission YAML (generated from canvas)
~/.agency/missions/{name}.canvas.json   ← visual layout (canvas source of truth)
```

Both files are managed by the gateway. The canvas.json is optional — missions created via CLI/YAML don't have one.

### Canvas JSON Schema

```json
{
  "version": 1,
  "nodes": [
    {
      "id": "n-abc123",
      "type": "trigger/schedule",
      "position": { "x": 100, "y": 200 },
      "data": { "cron": "0 9 * * 3", "timezone": "America/Los_Angeles" }
    },
    {
      "id": "n-def456",
      "type": "agent",
      "position": { "x": 400, "y": 200 },
      "data": {
        "preset": "threat-hunter",
        "model": "sonnet",
        "instructions": "Conduct proactive threat hunts...",
        "budget": { "daily": 5.0, "per_task": 3.0 },
        "cost_mode": "balanced"
      }
    },
    {
      "id": "n-ghi789",
      "type": "output/channel-post",
      "position": { "x": 700, "y": 200 },
      "data": { "channel": "security-findings" }
    }
  ],
  "edges": [
    { "id": "e-1", "source": "n-abc123", "sourceHandle": "trigger-out", "target": "n-def456", "targetHandle": "trigger-in" },
    { "id": "e-2", "source": "n-def456", "sourceHandle": "output-out", "target": "n-ghi789", "targetHandle": "output-in" }
  ]
}
```

### Generation

Canvas → mission YAML generation:
1. Find the agent node (exactly one required per canvas)
2. Collect all trigger nodes connected to the agent → `triggers[]`
3. Collect modifier nodes connected to the agent → fallback, success_criteria, reflection, budget fields
4. Collect output nodes connected to the agent → inform instructions context
5. Collect hub nodes → connector triggers, skill capabilities in `requires`
6. Assemble mission YAML from fragments using each node's `serialize()` function
7. Validate the assembled mission against the existing `MissionConfig.Validate()` model

### Reverse Generation

Existing missions without canvas.json can be converted:
1. Parse mission YAML
2. Create agent node from core fields
3. Create trigger nodes from `triggers[]`
4. Create modifier nodes from fallback, success_criteria, reflection
5. Auto-layout nodes left-to-right (triggers → agent → outputs)
6. Save as canvas.json

This is best-effort — hand-edited YAML may have fields that don't map cleanly to nodes.

## UI Design

### Entry Points

1. **MissionWizard** — new "Visual Editor" button alongside the existing step-by-step form
2. **MissionDetail** — "Edit in Visual Editor" button opens the canvas for an existing mission
3. **MissionList** — missions with canvas.json show a canvas icon

### Canvas Layout

```
┌─────────────────────────────────────────────────────────┐
│ ← Back to Mission   │  Mission: threat-hunter   │ Save  │ Validate │ Deploy │
├──────────┬──────────────────────────────────┬───────────┤
│ Palette  │                                  │ Properties│
│          │                                  │           │
│ Triggers │        React Flow Canvas         │ [Selected │
│  ● Sched │                                  │  Node     │
│  ● Webhk │   [Schedule] ──→ [Agent] ──→     │  Config]  │
│  ● Conn  │                    ↑       [Ch]  │           │
│          │              [Fallback]           │  Cron:    │
│ Agent    │                                  │  ________  │
│  ● Agent │                                  │           │
│          │                                  │  Timezone: │
│ Outputs  │                                  │  ________  │
│  ● Chan  │                                  │           │
│  ● Hook  │                                  │           │
│          │              ┌─────────┐         │           │
│ Modifier │              │ Minimap │         │           │
│  ● Fallb │              └─────────┘         │           │
│  ● Succ  │                                  │           │
│          │                                  │           │
│ Hub      │                                  │           │
│  ● Conns │                                  │           │
│  ● Skill │                                  │           │
└──────────┴──────────────────────────────────┴───────────┘
```

- **Left sidebar** — node palette grouped by category, draggable, searchable
- **Center** — React Flow canvas (zoom, pan, snap-to-grid, minimap)
- **Right panel** — property editor for selected node, context-sensitive form
- **Top bar** — mission name, save, validate, deploy actions

### Node Visual Design

Each node rendered as a card:
- Category color-coded (triggers: blue, agent: green, outputs: orange, modifiers: purple, hub: teal)
- Icon + label + summary of config (e.g., "Every Wed 9am")
- Connection handles on edges, color-matched to port type

### Validation

Client-side validation on save:
- Exactly one agent node required
- Agent must have at least one trigger connected
- No disconnected nodes (warning, not error)
- Modifier nodes must connect to the agent
- Hub connector nodes must reference installed instances
- Cron expressions validated for syntax

Server-side validation on deploy:
- Generated mission YAML passes `MissionConfig.Validate()`
- Referenced hub instances exist and are active
- Required capabilities are available

## API Changes

### New Endpoints

```
GET    /api/v1/missions/{name}/canvas     → canvas.json or 404
PUT    /api/v1/missions/{name}/canvas     → save canvas.json
POST   /api/v1/missions/{name}/canvas/generate  → generate mission YAML from canvas, return both
DELETE /api/v1/missions/{name}/canvas     → remove canvas.json (mission YAML unaffected)
```

### Modified Endpoints

- `GET /api/v1/missions/{name}` — add `has_canvas: bool` field to response
- `DELETE /api/v1/missions/{name}` — also deletes canvas.json if present

### Generation Endpoint

`POST /api/v1/missions/{name}/canvas/generate`:
- Request body: canvas.json content
- Saves canvas.json
- Generates mission YAML from canvas
- Validates generated mission
- Returns: `{ mission: <yaml>, canvas: <json>, validation: { valid: bool, errors: [] } }`
- Does NOT assign or deploy — that's a separate step

## What Doesn't Change

- **Mission model** — no schema changes to MissionConfig
- **Mission lifecycle** — create/assign/pause/resume/complete unchanged
- **Event framework** — triggers work the same way
- **CLI** — `agency mission create/assign` still works with YAML directly
- **Hub** — components referenced by name, resolved at generation time
- **Existing MissionWizard** — stays as the simple/quick path

## Dependencies

- **React Flow** (`@xyflow/react`) — MIT licensed, ~50KB gzipped
- Existing agency-web stack (React, Vite, Tailwind)
- Existing gateway mission CRUD endpoints

## Phase 2 Preview

Phase 2 (Workflow Engine) extends this by:
- Adding a `Workflow` model to the gateway (sequence of steps with data flow)
- New step node types: conditional branch, meeseeks spawn, delay/wait, HTTP call
- Data passing between steps (output of step N → input of step N+1)
- Workflow execution engine in the gateway
- Canvas supports multi-agent flows (multiple agent nodes connected in sequence)

The node registry and canvas infrastructure from Phase 1 are reused — Phase 2 adds new node types and a backend execution runtime.
