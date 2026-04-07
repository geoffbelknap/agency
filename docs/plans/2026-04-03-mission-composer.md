# Mission Composer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Visual canvas editor in agency-web for composing missions using React Flow, with an extensible node registry and gateway canvas API.

**Architecture:** Two repos — agency-web gets the React Flow canvas, node registry, property panel, and canvas serialization. Agency gateway gets 4 new endpoints for canvas CRUD and mission generation from canvas. The canvas.json is stored alongside mission YAML. The existing MissionWizard stays as the simple path.

**Tech Stack:** React 19, React Flow (@xyflow/react), TypeScript, Tailwind CSS, agency gateway (Go/chi)

**Spec:** `docs/specs/mission-composer.md`

**Repos:**
- `agency-web/` — UI (Tasks 1–8)
- `agency/` — Gateway API (Task 9)

---

## File Structure

### agency-web (new files)

```
src/app/screens/missions/composer/
├── MissionComposer.tsx          — Main canvas screen (React Flow + panels)
├── ComposerToolbar.tsx          — Top bar: save, validate, deploy, back
├── NodePalette.tsx              — Left sidebar: draggable node categories
├── PropertyPanel.tsx            — Right panel: selected node config form
├── nodeRegistry.ts              — Node type registry (definitions + lookup)
├── nodeTypes.ts                 — React Flow nodeTypes map (renders all categories)
├── canvasTypes.ts               — TypeScript types for canvas, nodes, edges, ports
├── canvasSerializer.ts          — Canvas JSON ↔ mission YAML generation
├── canvasValidator.ts           — Client-side canvas validation
├── useCanvasApi.ts              — Hook for canvas CRUD API calls
├── nodes/
│   ├── TriggerNode.tsx          — Render component for all trigger node types
│   ├── AgentNode.tsx            — Render component for agent node
│   ├── OutputNode.tsx           — Render component for all output node types
│   ├── ModifierNode.tsx         — Render component for all modifier node types
│   └── HubNode.tsx              — Render component for hub component nodes
└── config/
    ├── triggerConfigs.ts        — Config schemas for trigger node types
    ├── agentConfig.ts           — Config schema for agent node
    ├── outputConfigs.ts         — Config schemas for output node types
    ├── modifierConfigs.ts       — Config schemas for modifier node types
    └── hubConfigs.ts            — Config schemas for hub node types
```

### agency-web (modified files)

```
src/app/lib/api.ts               — Add canvas CRUD methods to missions namespace
src/app/screens/MissionWizard.tsx — Add "Visual Editor" toggle button
src/app/screens/MissionDetail.tsx — Add "Edit in Visual Editor" button
src/app/screens/MissionList.tsx   — Show canvas icon for missions with canvas.json
src/app/routes.tsx                — Add /missions/:name/composer route
package.json                      — Add @xyflow/react dependency
```

### agency gateway (new/modified files)

```
internal/api/handlers_canvas.go   — Canvas CRUD + generation endpoints (new)
internal/api/routes.go            — Register canvas routes (modify)
internal/orchestrate/missions.go  — Canvas file management helpers (modify)
```

---

### Task 1: Install React Flow and create canvas types

**Files:**
- Modify: `agency-web/package.json`
- Create: `agency-web/src/app/screens/missions/composer/canvasTypes.ts`

- [ ] **Step 1: Install React Flow**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
npm install @xyflow/react
```

- [ ] **Step 2: Create canvas types**

Create `src/app/screens/missions/composer/canvasTypes.ts`:

```typescript
import type { Node, Edge } from '@xyflow/react';

// --- Port and Node Definition Types ---

export type PortType = 'trigger' | 'agent' | 'output' | 'modifier' | 'data';
export type NodeCategory = 'trigger' | 'agent' | 'output' | 'modifier' | 'hub';

export interface PortDef {
  id: string;
  type: PortType;
  label?: string;
  multiple?: boolean;
}

export interface ConfigField {
  key: string;
  label: string;
  type: 'text' | 'textarea' | 'number' | 'select' | 'checkbox' | 'cron' | 'tags';
  placeholder?: string;
  required?: boolean;
  options?: { value: string; label: string }[];
  defaultValue?: string | number | boolean;
}

export interface ValidationError {
  nodeId?: string;
  field?: string;
  message: string;
}

export interface MissionFragment {
  triggers?: Record<string, unknown>[];
  requires?: { capabilities?: string[]; channels?: string[] };
  budget?: Record<string, number | null>;
  health?: Record<string, unknown>;
  fallback?: Record<string, unknown>;
  success_criteria?: Record<string, unknown>;
  reflection?: Record<string, unknown>;
  procedural_memory?: Record<string, unknown>;
  episodic_memory?: Record<string, unknown>;
  meeseeks?: boolean;
  meeseeks_limit?: number;
  meeseeks_model?: string;
  meeseeks_budget?: number;
  cost_mode?: string;
  instructions?: string;
  name?: string;
  description?: string;
  preset?: string;
  model?: string;
}

export interface NodeDefinition {
  typeId: string;
  category: NodeCategory;
  label: string;
  icon: string;
  ports: {
    inputs: PortDef[];
    outputs: PortDef[];
  };
  configSchema: ConfigField[];
  serialize: (data: Record<string, unknown>) => MissionFragment;
  validate: (data: Record<string, unknown>, connections: Edge[]) => ValidationError[];
}

// --- Canvas Persistence Types ---

export interface CanvasNodeData {
  typeId: string;
  config: Record<string, unknown>;
  [key: string]: unknown;
}

export type CanvasNode = Node<CanvasNodeData>;
export type CanvasEdge = Edge;

export interface CanvasDocument {
  version: 1;
  nodes: Array<{
    id: string;
    type: string;
    position: { x: number; y: number };
    data: CanvasNodeData;
  }>;
  edges: Array<{
    id: string;
    source: string;
    sourceHandle?: string;
    target: string;
    targetHandle?: string;
  }>;
}

// --- Category Colors ---

export const CATEGORY_COLORS: Record<NodeCategory, string> = {
  trigger: '#3b82f6',   // blue
  agent: '#22c55e',     // green
  output: '#f97316',    // orange
  modifier: '#a855f7',  // purple
  hub: '#14b8a6',       // teal
};
```

- [ ] **Step 3: Verify build**

```bash
npm run build 2>&1 | tail -5
```
Expected: builds clean (types only, no runtime usage yet)

- [ ] **Step 4: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add package.json package-lock.json src/app/screens/missions/composer/canvasTypes.ts
git commit -m "feat: install React Flow, add canvas type definitions"
```

---

### Task 2: Node registry and initial node definitions

**Files:**
- Create: `agency-web/src/app/screens/missions/composer/nodeRegistry.ts`
- Create: `agency-web/src/app/screens/missions/composer/config/triggerConfigs.ts`
- Create: `agency-web/src/app/screens/missions/composer/config/agentConfig.ts`
- Create: `agency-web/src/app/screens/missions/composer/config/outputConfigs.ts`
- Create: `agency-web/src/app/screens/missions/composer/config/modifierConfigs.ts`
- Create: `agency-web/src/app/screens/missions/composer/config/hubConfigs.ts`

- [ ] **Step 1: Create the node registry**

Create `src/app/screens/missions/composer/nodeRegistry.ts`:

```typescript
import type { NodeDefinition, NodeCategory } from './canvasTypes';

const registry = new Map<string, NodeDefinition>();

export function registerNode(def: NodeDefinition): void {
  registry.set(def.typeId, def);
}

export function getNodeDef(typeId: string): NodeDefinition | undefined {
  return registry.get(typeId);
}

export function getNodesByCategory(category: NodeCategory): NodeDefinition[] {
  return Array.from(registry.values()).filter(d => d.category === category);
}

export function getAllCategories(): NodeCategory[] {
  const cats = new Set<NodeCategory>();
  for (const def of registry.values()) cats.add(def.category);
  return Array.from(cats);
}

export function getAllNodes(): NodeDefinition[] {
  return Array.from(registry.values());
}
```

- [ ] **Step 2: Create trigger node configs**

Create `src/app/screens/missions/composer/config/triggerConfigs.ts`:

```typescript
import type { NodeDefinition } from '../canvasTypes';
import { registerNode } from '../nodeRegistry';

const scheduleNode: NodeDefinition = {
  typeId: 'trigger/schedule',
  category: 'trigger',
  label: 'Schedule',
  icon: 'Clock',
  ports: {
    inputs: [],
    outputs: [{ id: 'trigger-out', type: 'trigger', label: 'On Schedule' }],
  },
  configSchema: [
    { key: 'cron', label: 'Cron Expression', type: 'cron', required: true, placeholder: '0 9 * * 1-5' },
    { key: 'timezone', label: 'Timezone', type: 'text', placeholder: 'America/Los_Angeles' },
  ],
  serialize: (data) => ({
    triggers: [{ source: 'schedule', cron: data.cron, ...(data.timezone ? { timezone: data.timezone } : {}) }],
  }),
  validate: (data) => {
    const errors = [];
    if (!data.cron) errors.push({ field: 'cron', message: 'Cron expression is required' });
    return errors;
  },
};

const webhookNode: NodeDefinition = {
  typeId: 'trigger/webhook',
  category: 'trigger',
  label: 'Webhook',
  icon: 'Link',
  ports: {
    inputs: [],
    outputs: [{ id: 'trigger-out', type: 'trigger', label: 'On Webhook' }],
  },
  configSchema: [
    { key: 'name', label: 'Webhook Name', type: 'text', required: true, placeholder: 'my-webhook' },
    { key: 'event_type', label: 'Event Type Filter', type: 'text', placeholder: 'Optional' },
  ],
  serialize: (data) => ({
    triggers: [{ source: 'webhook', name: data.name, ...(data.event_type ? { event_type: data.event_type } : {}) }],
  }),
  validate: (data) => {
    const errors = [];
    if (!data.name) errors.push({ field: 'name', message: 'Webhook name is required' });
    return errors;
  },
};

const connectorEventNode: NodeDefinition = {
  typeId: 'trigger/connector-event',
  category: 'trigger',
  label: 'Connector Event',
  icon: 'Plug',
  ports: {
    inputs: [],
    outputs: [{ id: 'trigger-out', type: 'trigger', label: 'On Event' }],
  },
  configSchema: [
    { key: 'connector', label: 'Connector', type: 'text', required: true, placeholder: 'limacharlie' },
    { key: 'event_type', label: 'Event Type', type: 'text', required: true, placeholder: 'alert_created' },
    { key: 'match', label: 'Match Pattern', type: 'text', placeholder: 'Optional glob' },
  ],
  serialize: (data) => ({
    triggers: [{
      source: 'connector',
      connector: data.connector,
      event_type: data.event_type,
      ...(data.match ? { match: data.match } : {}),
    }],
  }),
  validate: (data) => {
    const errors = [];
    if (!data.connector) errors.push({ field: 'connector', message: 'Connector is required' });
    if (!data.event_type) errors.push({ field: 'event_type', message: 'Event type is required' });
    return errors;
  },
};

const channelPatternNode: NodeDefinition = {
  typeId: 'trigger/channel-pattern',
  category: 'trigger',
  label: 'Channel Message',
  icon: 'MessageSquare',
  ports: {
    inputs: [],
    outputs: [{ id: 'trigger-out', type: 'trigger', label: 'On Message' }],
  },
  configSchema: [
    { key: 'channel', label: 'Channel', type: 'text', required: true, placeholder: 'security-ops' },
    { key: 'match', label: 'Pattern', type: 'text', placeholder: 'hunt:*' },
  ],
  serialize: (data) => ({
    triggers: [{
      source: 'channel',
      channel: data.channel,
      ...(data.match ? { match: data.match } : {}),
    }],
  }),
  validate: (data) => {
    const errors = [];
    if (!data.channel) errors.push({ field: 'channel', message: 'Channel is required' });
    return errors;
  },
};

const platformEventNode: NodeDefinition = {
  typeId: 'trigger/platform-event',
  category: 'trigger',
  label: 'Platform Event',
  icon: 'Zap',
  ports: {
    inputs: [],
    outputs: [{ id: 'trigger-out', type: 'trigger', label: 'On Event' }],
  },
  configSchema: [
    { key: 'name', label: 'Event Name', type: 'text', required: true, placeholder: 'daily-digest' },
  ],
  serialize: (data) => ({
    triggers: [{ source: 'platform', name: data.name }],
  }),
  validate: (data) => {
    const errors = [];
    if (!data.name) errors.push({ field: 'name', message: 'Event name is required' });
    return errors;
  },
};

export function registerTriggerNodes(): void {
  registerNode(scheduleNode);
  registerNode(webhookNode);
  registerNode(connectorEventNode);
  registerNode(channelPatternNode);
  registerNode(platformEventNode);
}
```

- [ ] **Step 3: Create agent node config**

Create `src/app/screens/missions/composer/config/agentConfig.ts`:

```typescript
import type { NodeDefinition } from '../canvasTypes';
import { registerNode } from '../nodeRegistry';

const agentNode: NodeDefinition = {
  typeId: 'agent',
  category: 'agent',
  label: 'Agent',
  icon: 'Bot',
  ports: {
    inputs: [
      { id: 'trigger-in', type: 'trigger', label: 'Triggers', multiple: true },
      { id: 'modifier-in', type: 'modifier', label: 'Modifiers', multiple: true },
    ],
    outputs: [
      { id: 'output-out', type: 'output', label: 'Output', multiple: true },
    ],
  },
  configSchema: [
    { key: 'name', label: 'Mission Name', type: 'text', required: true, placeholder: 'threat-hunter' },
    { key: 'description', label: 'Description', type: 'text', required: true, placeholder: 'What this mission does' },
    { key: 'preset', label: 'Agent Preset', type: 'text', placeholder: 'generalist' },
    { key: 'model', label: 'Model', type: 'select', options: [
      { value: 'sonnet', label: 'Sonnet' },
      { value: 'haiku', label: 'Haiku' },
      { value: 'opus', label: 'Opus' },
    ]},
    { key: 'instructions', label: 'Instructions', type: 'textarea', required: true, placeholder: 'What the agent should do...' },
    { key: 'cost_mode', label: 'Cost Mode', type: 'select', options: [
      { value: 'frugal', label: 'Frugal' },
      { value: 'balanced', label: 'Balanced' },
      { value: 'thorough', label: 'Thorough' },
    ]},
    { key: 'meeseeks', label: 'Enable Meeseeks', type: 'checkbox' },
    { key: 'meeseeks_limit', label: 'Max Meeseeks', type: 'number', defaultValue: 3 },
    { key: 'meeseeks_model', label: 'Meeseeks Model', type: 'select', options: [
      { value: 'haiku', label: 'Haiku' },
      { value: 'sonnet', label: 'Sonnet' },
    ]},
    { key: 'meeseeks_budget', label: 'Meeseeks Budget ($)', type: 'number', defaultValue: 0.5 },
  ],
  serialize: (data) => ({
    name: data.name as string,
    description: data.description as string,
    instructions: data.instructions as string,
    ...(data.preset ? { preset: data.preset as string } : {}),
    ...(data.model ? { model: data.model as string } : {}),
    ...(data.cost_mode ? { cost_mode: data.cost_mode as string } : {}),
    ...(data.meeseeks ? {
      meeseeks: true,
      meeseeks_limit: (data.meeseeks_limit as number) || 3,
      meeseeks_model: (data.meeseeks_model as string) || 'haiku',
      meeseeks_budget: (data.meeseeks_budget as number) || 0.5,
    } : {}),
  }),
  validate: (data) => {
    const errors = [];
    if (!data.name) errors.push({ field: 'name', message: 'Mission name is required' });
    if (!data.description) errors.push({ field: 'description', message: 'Description is required' });
    if (!data.instructions) errors.push({ field: 'instructions', message: 'Instructions are required' });
    return errors;
  },
};

export function registerAgentNode(): void {
  registerNode(agentNode);
}
```

- [ ] **Step 4: Create output node configs**

Create `src/app/screens/missions/composer/config/outputConfigs.ts`:

```typescript
import type { NodeDefinition } from '../canvasTypes';
import { registerNode } from '../nodeRegistry';

const channelPostNode: NodeDefinition = {
  typeId: 'output/channel-post',
  category: 'output',
  label: 'Channel Post',
  icon: 'Hash',
  ports: {
    inputs: [{ id: 'output-in', type: 'output', label: 'Results' }],
    outputs: [],
  },
  configSchema: [
    { key: 'channel', label: 'Channel', type: 'text', required: true, placeholder: 'security-findings' },
  ],
  serialize: (data) => ({
    requires: { channels: [data.channel as string] },
  }),
  validate: (data) => {
    if (!data.channel) return [{ field: 'channel', message: 'Channel is required' }];
    return [];
  },
};

const webhookCallNode: NodeDefinition = {
  typeId: 'output/webhook-call',
  category: 'output',
  label: 'Webhook Call',
  icon: 'Send',
  ports: {
    inputs: [{ id: 'output-in', type: 'output', label: 'Results' }],
    outputs: [],
  },
  configSchema: [
    { key: 'url', label: 'URL', type: 'text', required: true, placeholder: 'https://hooks.example.com/...' },
  ],
  serialize: () => ({}), // Webhook outputs are informational for the instructions context
  validate: (data) => {
    if (!data.url) return [{ field: 'url', message: 'URL is required' }];
    return [];
  },
};

const escalationNode: NodeDefinition = {
  typeId: 'output/escalation',
  category: 'output',
  label: 'Escalation',
  icon: 'AlertTriangle',
  ports: {
    inputs: [{ id: 'output-in', type: 'output', label: 'Escalation' }],
    outputs: [],
  },
  configSchema: [
    { key: 'severity', label: 'Severity', type: 'select', required: true, options: [
      { value: 'info', label: 'Info' },
      { value: 'warn', label: 'Warning' },
      { value: 'error', label: 'Error' },
      { value: 'critical', label: 'Critical' },
    ]},
    { key: 'message', label: 'Message Template', type: 'textarea', placeholder: 'Escalation reason...' },
  ],
  serialize: () => ({}), // Escalation outputs inform fallback config
  validate: (data) => {
    if (!data.severity) return [{ field: 'severity', message: 'Severity is required' }];
    return [];
  },
};

export function registerOutputNodes(): void {
  registerNode(channelPostNode);
  registerNode(webhookCallNode);
  registerNode(escalationNode);
}
```

- [ ] **Step 5: Create modifier node configs**

Create `src/app/screens/missions/composer/config/modifierConfigs.ts`:

```typescript
import type { NodeDefinition } from '../canvasTypes';
import { registerNode } from '../nodeRegistry';

const fallbackPolicyNode: NodeDefinition = {
  typeId: 'modifier/fallback-policy',
  category: 'modifier',
  label: 'Fallback Policy',
  icon: 'LifeBuoy',
  ports: {
    inputs: [],
    outputs: [{ id: 'modifier-out', type: 'modifier', label: 'Policy' }],
  },
  configSchema: [
    { key: 'trigger', label: 'Trigger', type: 'select', required: true, options: [
      { value: 'tool_error', label: 'Tool Error' },
      { value: 'capability_unavailable', label: 'Capability Unavailable' },
      { value: 'budget_warning', label: 'Budget Warning' },
      { value: 'consecutive_errors', label: 'Consecutive Errors' },
      { value: 'timeout', label: 'Timeout' },
      { value: 'no_progress', label: 'No Progress' },
    ]},
    { key: 'action', label: 'First Action', type: 'select', required: true, options: [
      { value: 'retry', label: 'Retry' },
      { value: 'alternative_tool', label: 'Alternative Tool' },
      { value: 'degrade', label: 'Degrade' },
      { value: 'simplify', label: 'Simplify' },
      { value: 'complete_partial', label: 'Complete Partial' },
      { value: 'pause_and_assess', label: 'Pause & Assess' },
      { value: 'escalate', label: 'Escalate' },
    ]},
    { key: 'hint', label: 'Hint', type: 'text', placeholder: 'Guidance for the agent' },
  ],
  serialize: (data) => ({
    fallback: {
      policies: [{
        trigger: data.trigger,
        strategy: [{ action: data.action, ...(data.hint ? { hint: data.hint } : {}) }],
      }],
    },
  }),
  validate: (data) => {
    const errors = [];
    if (!data.trigger) errors.push({ field: 'trigger', message: 'Trigger is required' });
    if (!data.action) errors.push({ field: 'action', message: 'Action is required' });
    return errors;
  },
};

const successCriteriaNode: NodeDefinition = {
  typeId: 'modifier/success-criteria',
  category: 'modifier',
  label: 'Success Criteria',
  icon: 'CheckCircle',
  ports: {
    inputs: [],
    outputs: [{ id: 'modifier-out', type: 'modifier', label: 'Criteria' }],
  },
  configSchema: [
    { key: 'checklist', label: 'Checklist Items (one per line)', type: 'textarea', required: true, placeholder: 'All findings documented\nRecommendations included' },
    { key: 'mode', label: 'Evaluation Mode', type: 'select', options: [
      { value: 'checklist_only', label: 'Checklist Only (free)' },
      { value: 'llm', label: 'LLM Evaluation' },
    ], defaultValue: 'checklist_only' },
    { key: 'on_failure', label: 'On Failure', type: 'select', options: [
      { value: 'flag', label: 'Flag (accept + tag)' },
      { value: 'retry', label: 'Retry' },
      { value: 'block', label: 'Block' },
    ], defaultValue: 'flag' },
  ],
  serialize: (data) => {
    const lines = (data.checklist as string || '').split('\n').filter(Boolean);
    return {
      success_criteria: {
        checklist: lines.map((desc, i) => ({
          id: `criteria-${i + 1}`,
          description: desc.trim(),
          required: true,
        })),
        evaluation: {
          enabled: true,
          mode: data.mode || 'checklist_only',
          on_failure: data.on_failure || 'flag',
        },
      },
    };
  },
  validate: (data) => {
    if (!data.checklist) return [{ field: 'checklist', message: 'At least one criterion is required' }];
    return [];
  },
};

const reflectionNode: NodeDefinition = {
  typeId: 'modifier/reflection',
  category: 'modifier',
  label: 'Reflection',
  icon: 'RotateCcw',
  ports: {
    inputs: [],
    outputs: [{ id: 'modifier-out', type: 'modifier', label: 'Reflection' }],
  },
  configSchema: [
    { key: 'max_rounds', label: 'Max Rounds', type: 'number', defaultValue: 3, placeholder: '1-10' },
    { key: 'criteria', label: 'Reflection Criteria (one per line)', type: 'textarea', placeholder: 'Output addresses all requirements\nRecommendations are actionable' },
  ],
  serialize: (data) => {
    const criteria = (data.criteria as string || '').split('\n').filter(Boolean);
    return {
      reflection: {
        enabled: true,
        max_rounds: (data.max_rounds as number) || 3,
        ...(criteria.length > 0 ? { criteria } : {}),
      },
    };
  },
  validate: () => [],
};

const budgetLimitsNode: NodeDefinition = {
  typeId: 'modifier/budget-limits',
  category: 'modifier',
  label: 'Budget Limits',
  icon: 'DollarSign',
  ports: {
    inputs: [],
    outputs: [{ id: 'modifier-out', type: 'modifier', label: 'Budget' }],
  },
  configSchema: [
    { key: 'daily', label: 'Daily Limit ($)', type: 'number', placeholder: '5.00' },
    { key: 'monthly', label: 'Monthly Limit ($)', type: 'number', placeholder: '100.00' },
    { key: 'per_task', label: 'Per Task Limit ($)', type: 'number', placeholder: '3.00' },
  ],
  serialize: (data) => ({
    budget: {
      daily: (data.daily as number) || null,
      monthly: (data.monthly as number) || null,
      per_task: (data.per_task as number) || null,
    },
  }),
  validate: () => [],
};

export function registerModifierNodes(): void {
  registerNode(fallbackPolicyNode);
  registerNode(successCriteriaNode);
  registerNode(reflectionNode);
  registerNode(budgetLimitsNode);
}
```

- [ ] **Step 6: Create hub node configs**

Create `src/app/screens/missions/composer/config/hubConfigs.ts`:

```typescript
import type { NodeDefinition } from '../canvasTypes';
import { registerNode } from '../nodeRegistry';

const hubConnectorNode: NodeDefinition = {
  typeId: 'hub/connector',
  category: 'hub',
  label: 'Connector',
  icon: 'Plug2',
  ports: {
    inputs: [],
    outputs: [{ id: 'trigger-out', type: 'trigger', label: 'Events' }],
  },
  configSchema: [
    { key: 'instance', label: 'Connector Instance', type: 'text', required: true, placeholder: 'limacharlie' },
    { key: 'event_type', label: 'Event Type Filter', type: 'text', placeholder: 'alert_created' },
  ],
  serialize: (data) => ({
    triggers: [{
      source: 'connector',
      connector: data.instance as string,
      ...(data.event_type ? { event_type: data.event_type } : {}),
    }],
  }),
  validate: (data) => {
    if (!data.instance) return [{ field: 'instance', message: 'Connector instance is required' }];
    return [];
  },
};

const hubSkillNode: NodeDefinition = {
  typeId: 'hub/skill',
  category: 'hub',
  label: 'Skill',
  icon: 'Wrench',
  ports: {
    inputs: [],
    outputs: [{ id: 'modifier-out', type: 'modifier', label: 'Capability' }],
  },
  configSchema: [
    { key: 'skill', label: 'Skill Name', type: 'text', required: true, placeholder: 'code-review' },
  ],
  serialize: (data) => ({
    requires: { capabilities: [data.skill as string] },
  }),
  validate: (data) => {
    if (!data.skill) return [{ field: 'skill', message: 'Skill name is required' }];
    return [];
  },
};

export function registerHubNodes(): void {
  registerNode(hubConnectorNode);
  registerNode(hubSkillNode);
}
```

- [ ] **Step 7: Verify build**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
npm run build 2>&1 | tail -5
```

- [ ] **Step 8: Commit**

```bash
git add src/app/screens/missions/composer/
git commit -m "feat: node registry with trigger, agent, output, modifier, hub definitions"
```

---

### Task 3: Node render components

**Files:**
- Create: `agency-web/src/app/screens/missions/composer/nodes/TriggerNode.tsx`
- Create: `agency-web/src/app/screens/missions/composer/nodes/AgentNode.tsx`
- Create: `agency-web/src/app/screens/missions/composer/nodes/OutputNode.tsx`
- Create: `agency-web/src/app/screens/missions/composer/nodes/ModifierNode.tsx`
- Create: `agency-web/src/app/screens/missions/composer/nodes/HubNode.tsx`
- Create: `agency-web/src/app/screens/missions/composer/nodeTypes.ts`

Each node component renders a React Flow custom node with:
- Category-colored header bar
- Icon + label
- Config summary (one-line preview of key settings)
- Typed handles (input/output ports) with color-matched connection points

- [ ] **Step 1: Create base node render component pattern**

Create `src/app/screens/missions/composer/nodes/TriggerNode.tsx`:

```tsx
import { Handle, Position, type NodeProps } from '@xyflow/react';
import { Clock, Link, Plug, MessageSquare, Zap } from 'lucide-react';
import type { CanvasNodeData } from '../canvasTypes';
import { getNodeDef } from '../nodeRegistry';
import { CATEGORY_COLORS } from '../canvasTypes';

const ICONS: Record<string, React.ElementType> = {
  Clock, Link, Plug, MessageSquare, Zap,
};

export function TriggerNode({ data, selected }: NodeProps<CanvasNodeData>) {
  const def = getNodeDef(data.typeId);
  if (!def) return null;

  const Icon = ICONS[def.icon] || Zap;
  const color = CATEGORY_COLORS.trigger;
  const config = data.config || {};

  // Build summary from first non-empty config value
  const summary = def.configSchema
    .map(f => config[f.key])
    .filter(Boolean)
    .join(' · ') || 'Not configured';

  return (
    <div
      className={`rounded-lg shadow-md bg-white dark:bg-zinc-900 border-2 min-w-[180px] ${selected ? 'ring-2 ring-blue-400' : ''}`}
      style={{ borderColor: color }}
    >
      <div className="flex items-center gap-2 px-3 py-1.5 rounded-t-md text-white text-sm font-medium" style={{ backgroundColor: color }}>
        <Icon size={14} />
        {def.label}
      </div>
      <div className="px-3 py-2 text-xs text-zinc-500 dark:text-zinc-400 truncate max-w-[200px]">
        {summary}
      </div>
      {def.ports.outputs.map(port => (
        <Handle
          key={port.id}
          type="source"
          position={Position.Right}
          id={port.id}
          style={{ background: color, width: 10, height: 10 }}
        />
      ))}
    </div>
  );
}
```

- [ ] **Step 2: Create AgentNode**

Create `src/app/screens/missions/composer/nodes/AgentNode.tsx`:

```tsx
import { Handle, Position, type NodeProps } from '@xyflow/react';
import { Bot } from 'lucide-react';
import type { CanvasNodeData } from '../canvasTypes';
import { getNodeDef } from '../nodeRegistry';
import { CATEGORY_COLORS } from '../canvasTypes';

export function AgentNode({ data, selected }: NodeProps<CanvasNodeData>) {
  const def = getNodeDef(data.typeId);
  if (!def) return null;

  const color = CATEGORY_COLORS.agent;
  const config = data.config || {};
  const name = (config.name as string) || 'Unnamed Mission';
  const preset = (config.preset as string) || '';

  return (
    <div
      className={`rounded-lg shadow-md bg-white dark:bg-zinc-900 border-2 min-w-[220px] ${selected ? 'ring-2 ring-green-400' : ''}`}
      style={{ borderColor: color }}
    >
      {def.ports.inputs.map(port => (
        <Handle
          key={port.id}
          type="target"
          position={Position.Left}
          id={port.id}
          style={{
            background: CATEGORY_COLORS[port.type === 'modifier' ? 'modifier' : 'trigger'],
            width: 10,
            height: 10,
            top: port.type === 'modifier' ? '70%' : '30%',
          }}
        />
      ))}
      <div className="flex items-center gap-2 px-3 py-1.5 rounded-t-md text-white text-sm font-medium" style={{ backgroundColor: color }}>
        <Bot size={14} />
        Agent
      </div>
      <div className="px-3 py-2">
        <div className="text-sm font-medium dark:text-zinc-100">{name}</div>
        {preset && <div className="text-xs text-zinc-500 dark:text-zinc-400">Preset: {preset}</div>}
        {config.cost_mode && <div className="text-xs text-zinc-500 dark:text-zinc-400">Cost: {config.cost_mode as string}</div>}
      </div>
      {def.ports.outputs.map(port => (
        <Handle
          key={port.id}
          type="source"
          position={Position.Right}
          id={port.id}
          style={{ background: CATEGORY_COLORS.output, width: 10, height: 10 }}
        />
      ))}
    </div>
  );
}
```

- [ ] **Step 3: Create OutputNode, ModifierNode, HubNode**

Create `src/app/screens/missions/composer/nodes/OutputNode.tsx`:

```tsx
import { Handle, Position, type NodeProps } from '@xyflow/react';
import { Hash, Send, AlertTriangle } from 'lucide-react';
import type { CanvasNodeData } from '../canvasTypes';
import { getNodeDef } from '../nodeRegistry';
import { CATEGORY_COLORS } from '../canvasTypes';

const ICONS: Record<string, React.ElementType> = { Hash, Send, AlertTriangle };

export function OutputNode({ data, selected }: NodeProps<CanvasNodeData>) {
  const def = getNodeDef(data.typeId);
  if (!def) return null;

  const Icon = ICONS[def.icon] || Hash;
  const color = CATEGORY_COLORS.output;
  const config = data.config || {};
  const summary = def.configSchema.map(f => config[f.key]).filter(Boolean).join(' · ') || 'Not configured';

  return (
    <div
      className={`rounded-lg shadow-md bg-white dark:bg-zinc-900 border-2 min-w-[180px] ${selected ? 'ring-2 ring-orange-400' : ''}`}
      style={{ borderColor: color }}
    >
      {def.ports.inputs.map(port => (
        <Handle key={port.id} type="target" position={Position.Left} id={port.id}
          style={{ background: color, width: 10, height: 10 }} />
      ))}
      <div className="flex items-center gap-2 px-3 py-1.5 rounded-t-md text-white text-sm font-medium" style={{ backgroundColor: color }}>
        <Icon size={14} />
        {def.label}
      </div>
      <div className="px-3 py-2 text-xs text-zinc-500 dark:text-zinc-400 truncate max-w-[200px]">{summary}</div>
    </div>
  );
}
```

Create `src/app/screens/missions/composer/nodes/ModifierNode.tsx`:

```tsx
import { Handle, Position, type NodeProps } from '@xyflow/react';
import { LifeBuoy, CheckCircle, RotateCcw, DollarSign } from 'lucide-react';
import type { CanvasNodeData } from '../canvasTypes';
import { getNodeDef } from '../nodeRegistry';
import { CATEGORY_COLORS } from '../canvasTypes';

const ICONS: Record<string, React.ElementType> = { LifeBuoy, CheckCircle, RotateCcw, DollarSign };

export function ModifierNode({ data, selected }: NodeProps<CanvasNodeData>) {
  const def = getNodeDef(data.typeId);
  if (!def) return null;

  const Icon = ICONS[def.icon] || LifeBuoy;
  const color = CATEGORY_COLORS.modifier;
  const config = data.config || {};
  const summary = def.configSchema.map(f => config[f.key]).filter(Boolean).join(' · ') || 'Not configured';

  return (
    <div
      className={`rounded-lg shadow-md bg-white dark:bg-zinc-900 border-2 min-w-[180px] ${selected ? 'ring-2 ring-purple-400' : ''}`}
      style={{ borderColor: color }}
    >
      <div className="flex items-center gap-2 px-3 py-1.5 rounded-t-md text-white text-sm font-medium" style={{ backgroundColor: color }}>
        <Icon size={14} />
        {def.label}
      </div>
      <div className="px-3 py-2 text-xs text-zinc-500 dark:text-zinc-400 truncate max-w-[200px]">{summary}</div>
      {def.ports.outputs.map(port => (
        <Handle key={port.id} type="source" position={Position.Right} id={port.id}
          style={{ background: color, width: 10, height: 10 }} />
      ))}
    </div>
  );
}
```

Create `src/app/screens/missions/composer/nodes/HubNode.tsx`:

```tsx
import { Handle, Position, type NodeProps } from '@xyflow/react';
import { Plug2, Wrench } from 'lucide-react';
import type { CanvasNodeData } from '../canvasTypes';
import { getNodeDef } from '../nodeRegistry';
import { CATEGORY_COLORS } from '../canvasTypes';

const ICONS: Record<string, React.ElementType> = { Plug2, Wrench };

export function HubNode({ data, selected }: NodeProps<CanvasNodeData>) {
  const def = getNodeDef(data.typeId);
  if (!def) return null;

  const Icon = ICONS[def.icon] || Plug2;
  const color = CATEGORY_COLORS.hub;
  const config = data.config || {};
  const summary = def.configSchema.map(f => config[f.key]).filter(Boolean).join(' · ') || 'Not configured';

  return (
    <div
      className={`rounded-lg shadow-md bg-white dark:bg-zinc-900 border-2 min-w-[180px] ${selected ? 'ring-2 ring-teal-400' : ''}`}
      style={{ borderColor: color }}
    >
      <div className="flex items-center gap-2 px-3 py-1.5 rounded-t-md text-white text-sm font-medium" style={{ backgroundColor: color }}>
        <Icon size={14} />
        {def.label}
      </div>
      <div className="px-3 py-2 text-xs text-zinc-500 dark:text-zinc-400 truncate max-w-[200px]">{summary}</div>
      {def.ports.outputs.map(port => (
        <Handle key={port.id} type="source" position={Position.Right} id={port.id}
          style={{ background: CATEGORY_COLORS[port.type as keyof typeof CATEGORY_COLORS] || color, width: 10, height: 10 }} />
      ))}
    </div>
  );
}
```

- [ ] **Step 4: Create nodeTypes map**

Create `src/app/screens/missions/composer/nodeTypes.ts`:

```typescript
import type { NodeTypes } from '@xyflow/react';
import { TriggerNode } from './nodes/TriggerNode';
import { AgentNode } from './nodes/AgentNode';
import { OutputNode } from './nodes/OutputNode';
import { ModifierNode } from './nodes/ModifierNode';
import { HubNode } from './nodes/HubNode';

// Register all node definitions on import
import { registerTriggerNodes } from './config/triggerConfigs';
import { registerAgentNode } from './config/agentConfig';
import { registerOutputNodes } from './config/outputConfigs';
import { registerModifierNodes } from './config/modifierConfigs';
import { registerHubNodes } from './config/hubConfigs';

registerTriggerNodes();
registerAgentNode();
registerOutputNodes();
registerModifierNodes();
registerHubNodes();

// Map category prefixes to render components
export const composerNodeTypes: NodeTypes = {
  'trigger/schedule': TriggerNode,
  'trigger/webhook': TriggerNode,
  'trigger/connector-event': TriggerNode,
  'trigger/channel-pattern': TriggerNode,
  'trigger/platform-event': TriggerNode,
  'agent': AgentNode,
  'output/channel-post': OutputNode,
  'output/webhook-call': OutputNode,
  'output/escalation': OutputNode,
  'modifier/fallback-policy': ModifierNode,
  'modifier/success-criteria': ModifierNode,
  'modifier/reflection': ModifierNode,
  'modifier/budget-limits': ModifierNode,
  'hub/connector': HubNode,
  'hub/skill': HubNode,
};
```

- [ ] **Step 5: Verify build**

```bash
npm run build 2>&1 | tail -5
```

- [ ] **Step 6: Commit**

```bash
git add src/app/screens/missions/composer/nodes/ src/app/screens/missions/composer/nodeTypes.ts
git commit -m "feat: React Flow node render components for all categories"
```

---

### Task 4: Canvas serializer and validator

**Files:**
- Create: `agency-web/src/app/screens/missions/composer/canvasSerializer.ts`
- Create: `agency-web/src/app/screens/missions/composer/canvasValidator.ts`

- [ ] **Step 1: Create canvas serializer**

Create `src/app/screens/missions/composer/canvasSerializer.ts`:

```typescript
import type { CanvasDocument, CanvasNode, CanvasEdge, MissionFragment } from './canvasTypes';
import { getNodeDef } from './nodeRegistry';
import type { WizardState } from '../serialize';

/**
 * Convert a canvas document into a WizardState that can be serialized to YAML
 * using the existing serialize.ts infrastructure.
 */
export function canvasToWizardState(doc: CanvasDocument): WizardState {
  // Find the agent node (exactly one required)
  const agentNode = doc.nodes.find(n => n.type === 'agent');
  if (!agentNode) {
    throw new Error('Canvas must contain exactly one Agent node');
  }

  const agentConfig = agentNode.data.config || {};
  const edges = doc.edges;

  // Collect all trigger fragments
  const triggers: Record<string, unknown>[] = [];
  const requires: { capabilities: string[]; channels: string[] } = { capabilities: [], channels: [] };
  let budget: { daily: number | null; monthly: number | null; per_task: number | null } = { daily: null, monthly: null, per_task: null };
  let fallback: Record<string, unknown> | undefined;
  let success_criteria: Record<string, unknown> | undefined;
  let reflection: Record<string, unknown> | undefined;

  // Walk all nodes connected to the agent
  for (const node of doc.nodes) {
    if (node.id === agentNode.id) continue;

    const def = getNodeDef(node.type);
    if (!def) continue;

    // Check if this node connects to the agent
    const connected = edges.some(e =>
      (e.source === node.id && e.target === agentNode.id) ||
      (e.target === node.id && e.source === agentNode.id)
    );
    if (!connected) continue;

    const fragment = def.serialize(node.data.config || {});

    // Merge fragment into state
    if (fragment.triggers) triggers.push(...fragment.triggers);
    if (fragment.requires?.capabilities) requires.capabilities.push(...fragment.requires.capabilities);
    if (fragment.requires?.channels) requires.channels.push(...fragment.requires.channels);
    if (fragment.budget) budget = { ...budget, ...fragment.budget };
    if (fragment.fallback) {
      if (!fallback) fallback = fragment.fallback;
      else {
        // Merge fallback policies
        const existing = (fallback as { policies?: unknown[] }).policies || [];
        const incoming = (fragment.fallback as { policies?: unknown[] }).policies || [];
        (fallback as { policies: unknown[] }).policies = [...existing, ...incoming];
      }
    }
    if (fragment.success_criteria) success_criteria = fragment.success_criteria;
    if (fragment.reflection) reflection = fragment.reflection;
  }

  // Assemble WizardState
  return {
    name: (agentConfig.name as string) || '',
    description: (agentConfig.description as string) || '',
    instructions: (agentConfig.instructions as string) || '',
    triggers: triggers.map(t => ({
      source: t.source as string,
      connector: t.connector as string | undefined,
      channel: t.channel as string | undefined,
      event_type: t.event_type as string | undefined,
      match: t.match as string | undefined,
      name: t.name as string | undefined,
      cron: t.cron as string | undefined,
    })),
    requires,
    budget,
    health: { indicators: [], business_hours: '' },
    meeseeks: !!(agentConfig.meeseeks),
    meeseeksLimit: (agentConfig.meeseeks_limit as number) || 3,
    meeseeksModel: (agentConfig.meeseeks_model as 'haiku' | 'sonnet') || 'haiku',
    meeseeksBudget: (agentConfig.meeseeks_budget as number) || 0.5,
    assignTarget: '',
    assignType: 'agent',
    cost_mode: agentConfig.cost_mode as string | undefined,
    reflection: reflection as WizardState['reflection'],
    success_criteria: success_criteria as WizardState['success_criteria'],
    fallback: fallback as WizardState['fallback'],
    procedural_memory: undefined,
    episodic_memory: undefined,
  };
}

/**
 * Convert React Flow state to a persistable CanvasDocument.
 */
export function toCanvasDocument(nodes: CanvasNode[], edges: CanvasEdge[]): CanvasDocument {
  return {
    version: 1,
    nodes: nodes.map(n => ({
      id: n.id,
      type: n.type || '',
      position: n.position,
      data: n.data,
    })),
    edges: edges.map(e => ({
      id: e.id,
      source: e.source,
      sourceHandle: e.sourceHandle || undefined,
      target: e.target,
      targetHandle: e.targetHandle || undefined,
    })),
  };
}

/**
 * Convert a CanvasDocument back to React Flow nodes/edges.
 */
export function fromCanvasDocument(doc: CanvasDocument): { nodes: CanvasNode[]; edges: CanvasEdge[] } {
  return {
    nodes: doc.nodes.map(n => ({
      id: n.id,
      type: n.type,
      position: n.position,
      data: n.data,
    })),
    edges: doc.edges.map(e => ({
      id: e.id,
      source: e.source,
      sourceHandle: e.sourceHandle,
      target: e.target,
      targetHandle: e.targetHandle,
    })),
  };
}
```

- [ ] **Step 2: Create canvas validator**

Create `src/app/screens/missions/composer/canvasValidator.ts`:

```typescript
import type { CanvasNode, CanvasEdge, ValidationError } from './canvasTypes';
import { getNodeDef } from './nodeRegistry';

/**
 * Validate the canvas for structural correctness.
 * Returns an array of validation errors (empty = valid).
 */
export function validateCanvas(nodes: CanvasNode[], edges: CanvasEdge[]): ValidationError[] {
  const errors: ValidationError[] = [];

  // Must have exactly one agent node
  const agentNodes = nodes.filter(n => n.type === 'agent');
  if (agentNodes.length === 0) {
    errors.push({ message: 'Canvas must contain an Agent node' });
  } else if (agentNodes.length > 1) {
    errors.push({ message: 'Canvas must contain exactly one Agent node' });
  }

  const agentId = agentNodes[0]?.id;

  // Agent must have at least one trigger connected
  if (agentId) {
    const triggerEdges = edges.filter(e =>
      e.target === agentId && e.targetHandle === 'trigger-in'
    );
    if (triggerEdges.length === 0) {
      errors.push({ nodeId: agentId, message: 'Agent must have at least one trigger connected' });
    }
  }

  // Validate each node's config
  for (const node of nodes) {
    const def = getNodeDef(node.type || '');
    if (!def) continue;

    const nodeErrors = def.validate(node.data.config || {}, edges);
    for (const err of nodeErrors) {
      errors.push({ nodeId: node.id, field: err.field, message: `${def.label}: ${err.message}` });
    }
  }

  // Warn about disconnected nodes (not errors)
  for (const node of nodes) {
    if (node.id === agentId) continue;
    const connected = edges.some(e => e.source === node.id || e.target === node.id);
    if (!connected) {
      const def = getNodeDef(node.type || '');
      errors.push({ nodeId: node.id, message: `${def?.label || 'Node'} is not connected` });
    }
  }

  return errors;
}

/**
 * Check if a proposed connection is valid based on port types.
 */
export function isValidConnection(
  sourceType: string | undefined,
  sourceHandle: string | undefined,
  targetType: string | undefined,
  targetHandle: string | undefined,
): boolean {
  if (!sourceHandle || !targetHandle) return false;

  // Extract port types from handle IDs (e.g., "trigger-out" → "trigger")
  const sourcePort = sourceHandle.replace('-out', '');
  const targetPort = targetHandle.replace('-in', '');

  return sourcePort === targetPort;
}
```

- [ ] **Step 3: Verify build**

```bash
npm run build 2>&1 | tail -5
```

- [ ] **Step 4: Commit**

```bash
git add src/app/screens/missions/composer/canvasSerializer.ts src/app/screens/missions/composer/canvasValidator.ts
git commit -m "feat: canvas serializer (canvas → YAML) and validator"
```

---

### Task 5: Node Palette and Property Panel

**Files:**
- Create: `agency-web/src/app/screens/missions/composer/NodePalette.tsx`
- Create: `agency-web/src/app/screens/missions/composer/PropertyPanel.tsx`

- [ ] **Step 1: Create NodePalette**

Create `src/app/screens/missions/composer/NodePalette.tsx`:

```tsx
import { useState } from 'react';
import { Search } from 'lucide-react';
import { getAllNodes, getNodesByCategory } from './nodeRegistry';
import type { NodeCategory } from './canvasTypes';
import { CATEGORY_COLORS } from './canvasTypes';

const CATEGORY_ORDER: NodeCategory[] = ['trigger', 'agent', 'output', 'modifier', 'hub'];
const CATEGORY_LABELS: Record<NodeCategory, string> = {
  trigger: 'Triggers',
  agent: 'Agent',
  output: 'Outputs',
  modifier: 'Modifiers',
  hub: 'Hub Components',
};

export function NodePalette() {
  const [search, setSearch] = useState('');

  const onDragStart = (event: React.DragEvent, typeId: string) => {
    event.dataTransfer.setData('application/reactflow-type', typeId);
    event.dataTransfer.effectAllowed = 'move';
  };

  const allNodes = getAllNodes();
  const filtered = search
    ? allNodes.filter(n => n.label.toLowerCase().includes(search.toLowerCase()) || n.typeId.includes(search.toLowerCase()))
    : null;

  return (
    <div className="w-52 border-r border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-950 overflow-y-auto">
      <div className="p-2">
        <div className="relative">
          <Search size={14} className="absolute left-2 top-2.5 text-zinc-400" />
          <input
            type="text"
            value={search}
            onChange={e => setSearch(e.target.value)}
            placeholder="Search nodes..."
            className="w-full pl-7 pr-2 py-1.5 text-xs rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900"
          />
        </div>
      </div>

      {filtered ? (
        <div className="px-2 pb-2">
          {filtered.map(def => (
            <div
              key={def.typeId}
              draggable
              onDragStart={e => onDragStart(e, def.typeId)}
              className="flex items-center gap-2 px-2 py-1.5 text-xs rounded cursor-grab hover:bg-zinc-100 dark:hover:bg-zinc-800 mb-0.5"
            >
              <div className="w-2 h-2 rounded-full" style={{ backgroundColor: CATEGORY_COLORS[def.category] }} />
              {def.label}
            </div>
          ))}
          {filtered.length === 0 && <div className="text-xs text-zinc-400 px-2 py-4">No matching nodes</div>}
        </div>
      ) : (
        CATEGORY_ORDER.map(cat => {
          const nodes = getNodesByCategory(cat);
          if (nodes.length === 0) return null;
          return (
            <div key={cat} className="mb-2">
              <div className="px-3 py-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-400">
                {CATEGORY_LABELS[cat]}
              </div>
              {nodes.map(def => (
                <div
                  key={def.typeId}
                  draggable
                  onDragStart={e => onDragStart(e, def.typeId)}
                  className="flex items-center gap-2 px-3 py-1.5 text-xs rounded cursor-grab hover:bg-zinc-100 dark:hover:bg-zinc-800 mx-1 mb-0.5"
                >
                  <div className="w-2 h-2 rounded-full" style={{ backgroundColor: CATEGORY_COLORS[cat] }} />
                  {def.label}
                </div>
              ))}
            </div>
          );
        })
      )}
    </div>
  );
}
```

- [ ] **Step 2: Create PropertyPanel**

Create `src/app/screens/missions/composer/PropertyPanel.tsx`:

```tsx
import type { CanvasNode } from './canvasTypes';
import { getNodeDef } from './nodeRegistry';
import { CATEGORY_COLORS } from './canvasTypes';

interface PropertyPanelProps {
  node: CanvasNode | null;
  onChange: (nodeId: string, config: Record<string, unknown>) => void;
}

export function PropertyPanel({ node, onChange }: PropertyPanelProps) {
  if (!node) {
    return (
      <div className="w-64 border-l border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-950 flex items-center justify-center">
        <p className="text-xs text-zinc-400">Select a node to edit</p>
      </div>
    );
  }

  const def = getNodeDef(node.data.typeId);
  if (!def) return null;

  const config = node.data.config || {};
  const color = CATEGORY_COLORS[def.category];

  const updateField = (key: string, value: unknown) => {
    onChange(node.id, { ...config, [key]: value });
  };

  return (
    <div className="w-64 border-l border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-950 overflow-y-auto">
      <div className="px-3 py-2 border-b border-zinc-200 dark:border-zinc-800">
        <div className="flex items-center gap-2">
          <div className="w-3 h-3 rounded-full" style={{ backgroundColor: color }} />
          <span className="text-sm font-medium dark:text-zinc-100">{def.label}</span>
        </div>
        <div className="text-[10px] text-zinc-400 mt-0.5">{def.typeId}</div>
      </div>

      <div className="p-3 space-y-3">
        {def.configSchema.map(field => (
          <div key={field.key}>
            <label className="block text-xs font-medium text-zinc-600 dark:text-zinc-300 mb-1">
              {field.label}
              {field.required && <span className="text-red-400 ml-0.5">*</span>}
            </label>

            {field.type === 'text' || field.type === 'cron' ? (
              <input
                type="text"
                value={(config[field.key] as string) || ''}
                onChange={e => updateField(field.key, e.target.value)}
                placeholder={field.placeholder}
                className="w-full px-2 py-1.5 text-xs rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900"
              />
            ) : field.type === 'textarea' ? (
              <textarea
                value={(config[field.key] as string) || ''}
                onChange={e => updateField(field.key, e.target.value)}
                placeholder={field.placeholder}
                rows={4}
                className="w-full px-2 py-1.5 text-xs rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900 resize-y"
              />
            ) : field.type === 'number' ? (
              <input
                type="number"
                value={(config[field.key] as number) ?? field.defaultValue ?? ''}
                onChange={e => updateField(field.key, e.target.value ? Number(e.target.value) : null)}
                placeholder={field.placeholder}
                className="w-full px-2 py-1.5 text-xs rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900"
              />
            ) : field.type === 'select' ? (
              <select
                value={(config[field.key] as string) || field.defaultValue || ''}
                onChange={e => updateField(field.key, e.target.value)}
                className="w-full px-2 py-1.5 text-xs rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900"
              >
                <option value="">Select...</option>
                {field.options?.map(opt => (
                  <option key={opt.value} value={opt.value}>{opt.label}</option>
                ))}
              </select>
            ) : field.type === 'checkbox' ? (
              <label className="flex items-center gap-2 text-xs">
                <input
                  type="checkbox"
                  checked={!!(config[field.key])}
                  onChange={e => updateField(field.key, e.target.checked)}
                  className="rounded border-zinc-300"
                />
                Enabled
              </label>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Verify build**

```bash
npm run build 2>&1 | tail -5
```

- [ ] **Step 4: Commit**

```bash
git add src/app/screens/missions/composer/NodePalette.tsx src/app/screens/missions/composer/PropertyPanel.tsx
git commit -m "feat: NodePalette sidebar and PropertyPanel config editor"
```

---

### Task 6: Main MissionComposer canvas component

**Files:**
- Create: `agency-web/src/app/screens/missions/composer/ComposerToolbar.tsx`
- Create: `agency-web/src/app/screens/missions/composer/MissionComposer.tsx`

- [ ] **Step 1: Create ComposerToolbar**

Create `src/app/screens/missions/composer/ComposerToolbar.tsx`:

```tsx
import { ArrowLeft, Save, CheckCircle, Rocket } from 'lucide-react';
import { useNavigate } from 'react-router';
import { Button } from '@/app/components/ui/button';
import type { ValidationError } from './canvasTypes';

interface ComposerToolbarProps {
  missionName: string;
  onSave: () => void;
  onValidate: () => ValidationError[];
  onDeploy: () => void;
  saving: boolean;
  dirty: boolean;
}

export function ComposerToolbar({ missionName, onSave, onValidate, onDeploy, saving, dirty }: ComposerToolbarProps) {
  const navigate = useNavigate();

  const handleValidate = () => {
    const errors = onValidate();
    if (errors.length === 0) {
      // Toast success handled by caller
    }
  };

  return (
    <div className="flex items-center justify-between px-4 py-2 border-b border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-950">
      <div className="flex items-center gap-3">
        <button onClick={() => navigate(missionName ? `/missions/${missionName}` : '/missions')} className="text-zinc-400 hover:text-zinc-600">
          <ArrowLeft size={18} />
        </button>
        <span className="text-sm font-medium dark:text-zinc-100">
          {missionName ? `Mission: ${missionName}` : 'New Mission'}
        </span>
        {dirty && <span className="text-[10px] text-zinc-400">Unsaved changes</span>}
      </div>
      <div className="flex items-center gap-2">
        <Button variant="outline" size="sm" onClick={onSave} disabled={saving}>
          <Save size={14} className="mr-1" />
          {saving ? 'Saving...' : 'Save'}
        </Button>
        <Button variant="outline" size="sm" onClick={handleValidate}>
          <CheckCircle size={14} className="mr-1" />
          Validate
        </Button>
        <Button size="sm" onClick={onDeploy}>
          <Rocket size={14} className="mr-1" />
          Deploy
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Create MissionComposer**

Create `src/app/screens/missions/composer/MissionComposer.tsx`:

```tsx
import { useCallback, useRef, useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router';
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  addEdge,
  useNodesState,
  useEdgesState,
  type Connection,
  type OnConnect,
  ReactFlowProvider,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { toast } from 'sonner';

import { composerNodeTypes } from './nodeTypes';
import { NodePalette } from './NodePalette';
import { PropertyPanel } from './PropertyPanel';
import { ComposerToolbar } from './ComposerToolbar';
import { isValidConnection } from './canvasValidator';
import { validateCanvas } from './canvasValidator';
import { toCanvasDocument, fromCanvasDocument, canvasToWizardState } from './canvasSerializer';
import { serializeToYaml } from '../serialize';
import { api } from '@/app/lib/api';
import type { CanvasNode, CanvasEdge, CanvasNodeData } from './canvasTypes';
import { getNodeDef } from './nodeRegistry';

function ComposerInner() {
  const { name } = useParams<{ name: string }>();
  const navigate = useNavigate();
  const reactFlowWrapper = useRef<HTMLDivElement>(null);
  const [nodes, setNodes, onNodesChange] = useNodesState<CanvasNodeData>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<CanvasEdge>([]);
  const [selectedNode, setSelectedNode] = useState<CanvasNode | null>(null);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [reactFlowInstance, setReactFlowInstance] = useState<any>(null);

  // Load existing canvas
  useEffect(() => {
    if (!name) return;
    api.missions.canvas(name).then(doc => {
      if (doc) {
        const { nodes: n, edges: e } = fromCanvasDocument(doc);
        setNodes(n);
        setEdges(e);
      }
    }).catch(() => {
      // No canvas yet — that's fine
    });
  }, [name]);

  const onConnect: OnConnect = useCallback((connection: Connection) => {
    const sourceNode = nodes.find(n => n.id === connection.source);
    const targetNode = nodes.find(n => n.id === connection.target);
    if (!isValidConnection(sourceNode?.type, connection.sourceHandle ?? undefined, targetNode?.type, connection.targetHandle ?? undefined)) {
      toast.error('Invalid connection — port types must match');
      return;
    }
    setEdges(eds => addEdge(connection, eds));
    setDirty(true);
  }, [nodes, setEdges]);

  const onDragOver = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  }, []);

  const onDrop = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    const typeId = event.dataTransfer.getData('application/reactflow-type');
    if (!typeId || !reactFlowInstance || !reactFlowWrapper.current) return;

    const def = getNodeDef(typeId);
    if (!def) return;

    // Only allow one agent node
    if (def.category === 'agent' && nodes.some(n => n.type === 'agent')) {
      toast.error('Canvas can only have one Agent node');
      return;
    }

    const bounds = reactFlowWrapper.current.getBoundingClientRect();
    const position = reactFlowInstance.screenToFlowPosition({
      x: event.clientX - bounds.left,
      y: event.clientY - bounds.top,
    });

    const newNode: CanvasNode = {
      id: `n-${crypto.randomUUID().slice(0, 8)}`,
      type: typeId,
      position,
      data: { typeId, config: {} },
    };

    setNodes(nds => [...nds, newNode]);
    setDirty(true);
  }, [reactFlowInstance, nodes, setNodes]);

  const onNodeClick = useCallback((_: React.MouseEvent, node: CanvasNode) => {
    setSelectedNode(node);
  }, []);

  const onPaneClick = useCallback(() => {
    setSelectedNode(null);
  }, []);

  const handleConfigChange = useCallback((nodeId: string, config: Record<string, unknown>) => {
    setNodes(nds => nds.map(n =>
      n.id === nodeId ? { ...n, data: { ...n.data, config } } : n
    ));
    setSelectedNode(prev => prev?.id === nodeId ? { ...prev, data: { ...prev.data, config } } : prev);
    setDirty(true);
  }, [setNodes]);

  const handleSave = useCallback(async () => {
    if (!name) return;
    setSaving(true);
    try {
      const doc = toCanvasDocument(nodes, edges);
      await api.missions.saveCanvas(name, doc);
      setDirty(false);
      toast.success('Canvas saved');
    } catch (err) {
      toast.error(`Save failed: ${err}`);
    } finally {
      setSaving(false);
    }
  }, [name, nodes, edges]);

  const handleValidate = useCallback(() => {
    const errors = validateCanvas(nodes as CanvasNode[], edges);
    if (errors.length === 0) {
      toast.success('Canvas is valid');
    } else {
      errors.forEach(e => toast.error(e.message));
    }
    return errors;
  }, [nodes, edges]);

  const handleDeploy = useCallback(async () => {
    const errors = validateCanvas(nodes as CanvasNode[], edges);
    if (errors.length > 0) {
      errors.forEach(e => toast.error(e.message));
      return;
    }

    try {
      const doc = toCanvasDocument(nodes, edges);
      const state = canvasToWizardState(doc);
      const yaml = serializeToYaml(state);

      // Save canvas + generate mission YAML
      if (name) {
        await api.missions.saveCanvas(name, doc);
        await api.missions.update(name, yaml);
        toast.success('Mission deployed');
        navigate(`/missions/${name}`);
      } else {
        await api.missions.create(yaml);
        const missionName = state.name;
        await api.missions.saveCanvas(missionName, doc);
        toast.success('Mission created');
        navigate(`/missions/${missionName}`);
      }
    } catch (err) {
      toast.error(`Deploy failed: ${err}`);
    }
  }, [name, nodes, edges, navigate]);

  return (
    <div className="h-screen flex flex-col">
      <ComposerToolbar
        missionName={name || ''}
        onSave={handleSave}
        onValidate={handleValidate}
        onDeploy={handleDeploy}
        saving={saving}
        dirty={dirty}
      />
      <div className="flex flex-1 overflow-hidden">
        <NodePalette />
        <div ref={reactFlowWrapper} className="flex-1">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onDragOver={onDragOver}
            onDrop={onDrop}
            onNodeClick={onNodeClick}
            onPaneClick={onPaneClick}
            onInit={setReactFlowInstance}
            nodeTypes={composerNodeTypes}
            fitView
            snapToGrid
            snapGrid={[16, 16]}
            deleteKeyCode={['Backspace', 'Delete']}
          >
            <Background gap={16} size={1} />
            <Controls />
            <MiniMap
              nodeStrokeWidth={3}
              pannable
              zoomable
            />
          </ReactFlow>
        </div>
        <PropertyPanel node={selectedNode} onChange={handleConfigChange} />
      </div>
    </div>
  );
}

export function MissionComposer() {
  return (
    <ReactFlowProvider>
      <ComposerInner />
    </ReactFlowProvider>
  );
}
```

- [ ] **Step 3: Verify build**

```bash
npm run build 2>&1 | tail -10
```
Expected: may fail due to missing `api.missions.canvas` and `api.missions.saveCanvas` methods (added in Task 7). That's OK.

- [ ] **Step 4: Commit**

```bash
git add src/app/screens/missions/composer/ComposerToolbar.tsx src/app/screens/missions/composer/MissionComposer.tsx
git commit -m "feat: MissionComposer canvas with React Flow, drag-drop, save/deploy"
```

---

### Task 7: API client methods and routing

**Files:**
- Modify: `agency-web/src/app/lib/api.ts` — add canvas CRUD methods
- Modify: `agency-web/src/app/routes.tsx` — add composer route
- Modify: `agency-web/src/app/screens/MissionDetail.tsx` — add "Visual Editor" button
- Modify: `agency-web/src/app/screens/MissionWizard.tsx` — add "Visual Editor" button
- Modify: `agency-web/src/app/screens/MissionList.tsx` — show canvas icon

- [ ] **Step 1: Add canvas API methods**

In `src/app/lib/api.ts`, add to the `missions` namespace (after the existing `evaluations` method):

```typescript
async canvas(name: string) {
  const resp = await authenticatedFetch(`/api/v1/missions/${encodeURIComponent(name)}/canvas`);
  if (resp.status === 404) return null;
  if (!resp.ok) throw new Error(`GET /api/v1/missions/${name}/canvas: ${resp.status}`);
  return resp.json();
},
async saveCanvas(name: string, doc: unknown) {
  const resp = await authenticatedFetch(`/api/v1/missions/${encodeURIComponent(name)}/canvas`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(doc),
  });
  if (!resp.ok) throw new Error(`PUT /api/v1/missions/${name}/canvas: ${resp.status}`);
  return resp.json();
},
```

Also add `has_canvas?: boolean` to the `RawMission` type in api.ts.

- [ ] **Step 2: Add composer route**

In `src/app/routes.tsx`, add inside the Layout route children:

```typescript
{ path: 'missions/:name/composer', lazy: () => import('./screens/missions/composer/MissionComposer').then(m => ({ Component: m.MissionComposer })), ErrorBoundary: RouteErrorBoundary },
```

- [ ] **Step 3: Add "Visual Editor" button to MissionDetail**

In `src/app/screens/MissionDetail.tsx`, find the actions area (edit/pause/resume buttons) and add:

```tsx
<Button variant="outline" size="sm" onClick={() => navigate(`/missions/${name}/composer`)}>
  Visual Editor
</Button>
```

- [ ] **Step 4: Add "Visual Editor" button to MissionWizard**

In `src/app/screens/MissionWizard.tsx`, add a button near the top of the wizard dialog that navigates to the composer for new missions.

- [ ] **Step 5: Show canvas icon in MissionList**

In `src/app/screens/MissionList.tsx`, when a mission has `has_canvas: true`, show a small canvas icon (e.g., `<Workflow size={12} />` from lucide) next to the mission name.

- [ ] **Step 6: Verify build**

```bash
npm run build 2>&1 | tail -10
```

- [ ] **Step 7: Commit**

```bash
git add src/app/lib/api.ts src/app/routes.tsx src/app/screens/MissionDetail.tsx src/app/screens/MissionWizard.tsx src/app/screens/MissionList.tsx
git commit -m "feat: canvas API client, routing, and entry points from wizard/detail/list"
```

---

### Task 8: Build and visual test agency-web

**Files:** None new — this is a build + visual verification task.

- [ ] **Step 1: Full build**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
npm run build
```
Expected: clean build with no errors

- [ ] **Step 2: Docker image build**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
make web
```
Expected: agency-web image builds successfully

- [ ] **Step 3: Commit agency-web**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add -A
git commit -m "feat: Mission Composer — visual canvas editor for missions"
git push origin main
```

---

### Task 9: Gateway canvas API endpoints

**Files:**
- Create: `agency/internal/api/handlers_canvas.go`
- Modify: `agency/internal/api/routes.go` — register canvas routes

- [ ] **Step 1: Create canvas handlers**

Create `internal/api/handlers_canvas.go`:

```go
package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
)

// getCanvas handles GET /api/v1/missions/{name}/canvas
func (h *handler) getCanvas(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	canvasPath := filepath.Join(h.cfg.Home, "missions", name+".canvas.json")

	data, err := os.ReadFile(canvasPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, 404, map[string]string{"error": "no canvas for this mission"})
			return
		}
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// putCanvas handles PUT /api/v1/missions/{name}/canvas
func (h *handler) putCanvas(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	// Verify mission exists
	missionPath := filepath.Join(h.cfg.Home, "missions", name+".yaml")
	if _, err := os.Stat(missionPath); os.IsNotExist(err) {
		writeJSON(w, 404, map[string]string{"error": "mission not found"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "failed to read body"})
		return
	}

	// Validate it's valid JSON
	var doc map[string]interface{}
	if json.Unmarshal(body, &doc) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}

	canvasPath := filepath.Join(h.cfg.Home, "missions", name+".canvas.json")
	if err := os.WriteFile(canvasPath, body, 0644); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, 200, map[string]string{"status": "saved"})
}

// deleteCanvas handles DELETE /api/v1/missions/{name}/canvas
func (h *handler) deleteCanvas(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	canvasPath := filepath.Join(h.cfg.Home, "missions", name+".canvas.json")
	os.Remove(canvasPath) // ignore errors — may not exist
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}
```

- [ ] **Step 2: Register canvas routes**

In `internal/api/routes.go`, find the missions route group and add:

```go
r.Get("/missions/{name}/canvas", h.getCanvas)
r.Put("/missions/{name}/canvas", h.putCanvas)
r.Delete("/missions/{name}/canvas", h.deleteCanvas)
```

- [ ] **Step 3: Add has_canvas to mission show response**

In the mission show handler, check if `.canvas.json` exists and include `has_canvas: true` in the response.

- [ ] **Step 4: Clean up canvas on mission delete**

In the mission delete handler, add:
```go
os.Remove(filepath.Join(h.cfg.Home, "missions", name+".canvas.json"))
```

- [ ] **Step 5: Build and test**

```bash
go build ./cmd/gateway/
go test ./internal/api/ -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers_canvas.go internal/api/routes.go
git commit -m "feat: canvas CRUD API endpoints for mission composer"
```

---

### Task 10: Integration test and deploy

- [ ] **Step 1: Run full Go test suite**

```bash
go test ./...
```

- [ ] **Step 2: Rebuild and deploy**

```bash
make install && make web && agency infra up
```

- [ ] **Step 3: Smoke test — create mission via composer**

Open `https://127.0.0.1:8280/missions`, click a mission, click "Visual Editor". Verify:
- Canvas loads with React Flow
- Nodes can be dragged from palette
- Nodes can be connected
- Property panel shows config for selected node
- Save persists canvas.json
- Deploy generates mission YAML

- [ ] **Step 4: Final commit and push**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git add -A
git commit -m "feat: Mission Composer — visual canvas editor (Phase 1 complete)"
git push origin main
```
