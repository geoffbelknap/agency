import type { RawMission } from '../../lib/api';

export interface WizardTrigger {
  source: 'channel' | 'schedule' | 'webhook' | 'connector' | 'platform';
  connector?: string;
  channel?: string;
  event_type?: string;
  match?: string;
  name?: string;
  cron?: string;
}

export interface WizardState {
  name: string;
  description: string;
  templateId?: string;
  instructions: string;
  triggers: WizardTrigger[];
  requires: { capabilities: string[]; channels: string[] };
  budget: { daily: number | null; monthly: number | null; per_task: number | null };
  health: { indicators: string[]; business_hours: string };
  meeseeks: boolean;
  meeseeksLimit: number | null;
  meeseeksModel: 'fast' | 'standard' | 'frontier' | null;
  meeseeksBudget: number | null;
  assignTarget: string;
  assignType: 'agent' | 'team';
  cost_mode?: 'frugal' | 'balanced' | 'thorough';
  reflection?: { enabled: boolean; max_rounds: number; criteria: string[] };
  success_criteria?: {
    checklist: { id: string; description: string; required: boolean }[];
    evaluation: { enabled: boolean; mode: string; on_failure: string };
  };
  fallback?: { policies: any[] };
  procedural_memory?: { capture: boolean; retrieve: boolean; max_retrieved: number };
  episodic_memory?: { capture: boolean; retrieve: boolean; max_retrieved: number; tool_enabled: boolean };
}

export function emptyWizardState(): WizardState {
  return {
    name: '',
    description: '',
    instructions: '',
    triggers: [],
    requires: { capabilities: [], channels: [] },
    budget: { daily: null, monthly: null, per_task: null },
    health: { indicators: [], business_hours: '' },
    meeseeks: false,
    meeseeksLimit: null,
    meeseeksModel: null,
    meeseeksBudget: null,
    assignTarget: '',
    assignType: 'agent',
  };
}

const YAML_BARE_UNSAFE = /[:#\[\]{}&*!|>'"%@`,?\n\r]/;
const YAML_RESERVED_WORDS = new Set([
  'true', 'false', 'yes', 'no', 'on', 'off', 'null', 'True', 'False',
  'Yes', 'No', 'On', 'Off', 'Null', 'TRUE', 'FALSE', 'YES', 'NO',
  'ON', 'OFF', 'NULL', '~',
]);

function yamlScalar(value: string): string {
  if (value === '') return "''";
  if (
    YAML_BARE_UNSAFE.test(value) ||
    value.trim() !== value ||
    value.startsWith('-') ||
    value.startsWith('.') ||
    YAML_RESERVED_WORDS.has(value) ||
    /^\d/.test(value)
  ) {
    return JSON.stringify(value);
  }
  return value;
}

function yamlBlock(text: string): string {
  const lines = text.split('\n');
  return '|\n' + lines.map((l) => '  ' + l).join('\n');
}

export function serializeToYaml(state: WizardState): string {
  const lines: string[] = [];

  lines.push(`name: ${yamlScalar(state.name)}`);
  lines.push(`description: ${yamlScalar(state.description)}`);

  if (state.instructions.includes('\n')) {
    lines.push(`instructions: ${yamlBlock(state.instructions)}`);
  } else {
    lines.push(`instructions: ${yamlScalar(state.instructions)}`);
  }

  // Triggers
  if (state.triggers.length > 0) {
    lines.push('triggers:');
    for (const t of state.triggers) {
      lines.push(`  - source: ${yamlScalar(t.source)}`);
      if (t.connector) lines.push(`    connector: ${yamlScalar(t.connector)}`);
      if (t.channel) lines.push(`    channel: ${yamlScalar(t.channel)}`);
      if (t.event_type) lines.push(`    event_type: ${yamlScalar(t.event_type)}`);
      if (t.match) lines.push(`    match: ${yamlScalar(t.match)}`);
      if (t.name) lines.push(`    name: ${yamlScalar(t.name)}`);
      if (t.cron) lines.push(`    cron: ${yamlScalar(t.cron)}`);
    }
  }

  // Requires
  const hasCaps = state.requires.capabilities.length > 0;
  const hasChans = state.requires.channels.length > 0;
  if (hasCaps || hasChans) {
    lines.push('requires:');
    if (hasCaps) {
      lines.push('  capabilities:');
      for (const c of state.requires.capabilities) {
        lines.push(`    - ${yamlScalar(c)}`);
      }
    }
    if (hasChans) {
      lines.push('  channels:');
      for (const c of state.requires.channels) {
        lines.push(`    - ${yamlScalar(c)}`);
      }
    }
  }

  // Budget
  const { daily, monthly, per_task } = state.budget;
  if (daily !== null || monthly !== null || per_task !== null) {
    lines.push('budget:');
    if (daily !== null) lines.push(`  daily: ${daily}`);
    if (monthly !== null) lines.push(`  monthly: ${monthly}`);
    if (per_task !== null) lines.push(`  per_task: ${per_task}`);
  }

  // Health
  const hasIndicators = state.health.indicators.length > 0;
  const hasHours = state.health.business_hours !== '';
  if (hasIndicators || hasHours) {
    lines.push('health:');
    if (hasIndicators) {
      lines.push('  indicators:');
      for (const i of state.health.indicators) {
        lines.push(`    - ${yamlScalar(i)}`);
      }
    }
    if (hasHours) {
      lines.push(`  business_hours: ${yamlScalar(state.health.business_hours)}`);
    }
  }

  // Meeseeks
  if (state.meeseeks) {
    lines.push('meeseeks: true');
    if (state.meeseeksLimit !== null) lines.push(`meeseeks_limit: ${state.meeseeksLimit}`);
    if (state.meeseeksModel !== null) lines.push(`meeseeks_model: ${yamlScalar(state.meeseeksModel)}`);
    if (state.meeseeksBudget !== null) lines.push(`meeseeks_budget: ${state.meeseeksBudget}`);
  }

  // Cost mode
  if (state.cost_mode) {
    lines.push(`cost_mode: ${state.cost_mode}`);
  }

  // Reflection
  if (state.reflection) {
    lines.push('reflection:');
    lines.push(`  enabled: ${state.reflection.enabled}`);
    lines.push(`  max_rounds: ${state.reflection.max_rounds}`);
    if (state.reflection.criteria.length > 0) {
      lines.push('  criteria:');
      for (const c of state.reflection.criteria) {
        lines.push(`    - ${yamlScalar(c)}`);
      }
    }
  }

  // Success criteria
  if (state.success_criteria) {
    lines.push('success_criteria:');
    if (state.success_criteria.checklist.length > 0) {
      lines.push('  checklist:');
      for (const item of state.success_criteria.checklist) {
        lines.push(`    - id: ${yamlScalar(item.id)}`);
        lines.push(`      description: ${yamlScalar(item.description)}`);
        lines.push(`      required: ${item.required}`);
      }
    }
    const ev = state.success_criteria.evaluation;
    lines.push('  evaluation:');
    lines.push(`    enabled: ${ev.enabled}`);
    lines.push(`    mode: ${yamlScalar(ev.mode)}`);
    lines.push(`    on_failure: ${yamlScalar(ev.on_failure)}`);
  }

  // Procedural memory
  if (state.procedural_memory) {
    lines.push('procedural_memory:');
    lines.push(`  capture: ${state.procedural_memory.capture}`);
    lines.push(`  retrieve: ${state.procedural_memory.retrieve}`);
    lines.push(`  max_retrieved: ${state.procedural_memory.max_retrieved}`);
  }

  // Episodic memory
  if (state.episodic_memory) {
    lines.push('episodic_memory:');
    lines.push(`  capture: ${state.episodic_memory.capture}`);
    lines.push(`  retrieve: ${state.episodic_memory.retrieve}`);
    lines.push(`  max_retrieved: ${state.episodic_memory.max_retrieved}`);
    lines.push(`  tool_enabled: ${state.episodic_memory.tool_enabled}`);
  }

  return lines.join('\n') + '\n';
}

export function parseFromRaw(raw: RawMission): WizardState {
  return {
    name: raw.name ?? '',
    description: raw.description ?? '',
    instructions: raw.instructions ?? '',
    triggers: (raw.triggers ?? []).map((t) => ({
      source: t.source as WizardTrigger['source'],
      ...(t.connector ? { connector: t.connector } : {}),
      ...(t.channel ? { channel: t.channel } : {}),
      ...(t.event_type ? { event_type: t.event_type } : {}),
      ...(t.match ? { match: t.match } : {}),
      ...(t.name ? { name: t.name } : {}),
      ...(t.cron ? { cron: t.cron } : {}),
    })),
    requires: {
      capabilities: raw.requires?.capabilities ?? [],
      channels: raw.requires?.channels ?? [],
    },
    budget: {
      daily: raw.budget?.daily ?? null,
      monthly: raw.budget?.monthly ?? null,
      per_task: raw.budget?.per_task ?? null,
    },
    health: {
      indicators: raw.health?.indicators ?? [],
      business_hours: raw.health?.business_hours ?? '',
    },
    meeseeks: raw.meeseeks ?? false,
    meeseeksLimit: raw.meeseeks_limit ?? null,
    meeseeksModel: (raw.meeseeks_model as WizardState['meeseeksModel']) ?? null,
    meeseeksBudget: raw.meeseeks_budget ?? null,
    assignTarget: '',
    assignType: 'agent',
    cost_mode: (raw.cost_mode as WizardState['cost_mode']) ?? undefined,
    reflection: raw.reflection ?? undefined,
    success_criteria: raw.success_criteria ?? undefined,
    fallback: raw.fallback ?? undefined,
    procedural_memory: raw.procedural_memory ?? undefined,
    episodic_memory: raw.episodic_memory ?? undefined,
  };
}
