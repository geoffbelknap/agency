import { describe, it, expect } from 'vitest';
import type { RawMission } from '../../lib/api';
import { emptyWizardState, serializeToYaml, parseFromRaw } from './serialize';

describe('emptyWizardState', () => {
  it('returns valid defaults', () => {
    const state = emptyWizardState();
    expect(state.name).toBe('');
    expect(state.description).toBe('');
    expect(state.instructions).toBe('');
    expect(state.triggers).toEqual([]);
    expect(state.requires).toEqual({ capabilities: [], channels: [] });
    expect(state.budget).toEqual({ daily: null, monthly: null, per_task: null });
    expect(state.health).toEqual({ indicators: [], business_hours: '' });
    expect(state.meeseeks).toBe(false);
    expect(state.meeseeksLimit).toBeNull();
    expect(state.meeseeksModel).toBeNull();
    expect(state.meeseeksBudget).toBeNull();
    expect(state.assignTarget).toBe('');
    expect(state.assignType).toBe('agent');
  });
});

describe('serializeToYaml', () => {
  it('with minimal state outputs only name, description, instructions', () => {
    const state = emptyWizardState();
    state.name = 'test-mission';
    state.description = 'A test';
    state.instructions = 'Do the thing';
    const yaml = serializeToYaml(state);
    expect(yaml).toContain('name: test-mission');
    expect(yaml).toContain('description: A test');
    expect(yaml).toContain('instructions: Do the thing');
    expect(yaml).not.toContain('triggers:');
    expect(yaml).not.toContain('requires:');
    expect(yaml).not.toContain('budget:');
    expect(yaml).not.toContain('health:');
    expect(yaml).not.toContain('meeseeks');
  });

  it('with full state includes all sections', () => {
    const state = emptyWizardState();
    state.name = 'full-mission';
    state.description = 'Full test';
    state.instructions = 'Step 1\nStep 2';
    state.triggers = [
      { source: 'channel', channel: '#ops', match: 'deploy*' },
      { source: 'schedule', cron: '0 9 * * 1' },
    ];
    state.requires = { capabilities: ['web-search'], channels: ['#ops'] };
    state.budget = { daily: 10, monthly: 200, per_task: 5 };
    state.health = { indicators: ['response_time'], business_hours: '09:00-17:00' };
    state.meeseeks = true;
    state.meeseeksLimit = 3;
    state.meeseeksModel = 'haiku';
    state.meeseeksBudget = 1.5;

    const yaml = serializeToYaml(state);
    expect(yaml).toContain('name: full-mission');
    expect(yaml).toContain('instructions: |');
    expect(yaml).toContain('  Step 1');
    expect(yaml).toContain('  Step 2');
    expect(yaml).toContain('triggers:');
    expect(yaml).toContain('  - source: channel');
    expect(yaml).toContain('    channel: \"#ops\"');
    expect(yaml).toContain('    match: \"deploy*\"');
    expect(yaml).toContain('  - source: schedule');
    expect(yaml).toContain('    cron: \"0 9 * * 1\"');
    expect(yaml).toContain('requires:');
    expect(yaml).toContain('    - web-search');
    expect(yaml).toContain('    - \"#ops\"');
    expect(yaml).toContain('budget:');
    expect(yaml).toContain('  daily: 10');
    expect(yaml).toContain('  monthly: 200');
    expect(yaml).toContain('  per_task: 5');
    expect(yaml).toContain('health:');
    expect(yaml).toContain('    - response_time');
    expect(yaml).toContain('  business_hours: \"09:00-17:00\"');
    expect(yaml).toContain('meeseeks: true');
    expect(yaml).toContain('meeseeks_limit: 3');
    expect(yaml).toContain('meeseeks_model: haiku');
    expect(yaml).toContain('meeseeks_budget: 1.5');
    // Should not include assignment fields
    expect(yaml).not.toContain('assignTarget');
    expect(yaml).not.toContain('assignType');
  });

  it('omits empty optional fields', () => {
    const state = emptyWizardState();
    state.name = 'sparse';
    state.description = 'Sparse mission';
    state.instructions = 'Just basics';
    const yaml = serializeToYaml(state);
    expect(yaml).not.toContain('triggers:');
    expect(yaml).not.toContain('requires:');
    expect(yaml).not.toContain('budget:');
    expect(yaml).not.toContain('health:');
    expect(yaml).not.toContain('meeseeks');
  });
});

describe('yamlScalar edge cases', () => {
  it('quotes strings containing newlines', () => {
    const state = emptyWizardState();
    state.name = 'test';
    state.description = 'line1\nline2';
    state.instructions = 'single line';
    const yaml = serializeToYaml(state);
    // Description with newline must be quoted (JSON-style) not bare
    expect(yaml).toContain('description: "line1\\nline2"');
  });

  it('quotes strings starting with a hyphen', () => {
    const state = emptyWizardState();
    state.name = '- injected';
    state.description = 'ok';
    state.instructions = 'ok';
    const yaml = serializeToYaml(state);
    expect(yaml).toContain('name: "- injected"');
  });

  it('quotes strings that look like booleans or numbers', () => {
    const state = emptyWizardState();
    state.name = 'true';
    state.description = 'null';
    state.instructions = 'ok';
    const yaml = serializeToYaml(state);
    expect(yaml).toContain('name: "true"');
    expect(yaml).toContain('description: "null"');
  });
});

describe('parseFromRaw', () => {
  it('maps all fields', () => {
    const raw: RawMission = {
      id: 'mission-1',
      name: 'ops-watch',
      description: 'Watches ops',
      version: 2,
      status: 'active',
      assigned_to: 'agent-1',
      assigned_type: 'agent',
      instructions: 'Monitor and report',
      triggers: [
        { source: 'channel', channel: '#ops', match: 'alert*' },
        { source: 'schedule', cron: '*/5 * * * *' },
      ],
      requires: { capabilities: ['web-search', 'slack'], channels: ['#ops', '#alerts'] },
      budget: { daily: 10, monthly: 250, per_task: 2 },
      health: { indicators: ['uptime', 'latency'], business_hours: '08:00-18:00' },
      meeseeks: true,
      meeseeks_limit: 5,
      meeseeks_model: 'sonnet',
      meeseeks_budget: 3.0,
    };

    const state = parseFromRaw(raw);
    expect(state.name).toBe('ops-watch');
    expect(state.description).toBe('Watches ops');
    expect(state.instructions).toBe('Monitor and report');
    expect(state.triggers).toHaveLength(2);
    expect(state.triggers[0]).toEqual({ source: 'channel', channel: '#ops', match: 'alert*' });
    expect(state.triggers[1]).toEqual({ source: 'schedule', cron: '*/5 * * * *' });
    expect(state.requires.capabilities).toEqual(['web-search', 'slack']);
    expect(state.requires.channels).toEqual(['#ops', '#alerts']);
    expect(state.budget).toEqual({ daily: 10, monthly: 250, per_task: 2 });
    expect(state.health.indicators).toEqual(['uptime', 'latency']);
    expect(state.health.business_hours).toBe('08:00-18:00');
    expect(state.meeseeks).toBe(true);
    expect(state.meeseeksLimit).toBe(5);
    expect(state.meeseeksModel).toBe('sonnet');
    expect(state.meeseeksBudget).toBe(3.0);
    expect(state.assignTarget).toBe('');
    expect(state.assignType).toBe('agent');
  });

  it('with minimal input uses defaults for missing fields', () => {
    const raw: RawMission = { name: 'bare', status: 'draft' };
    const state = parseFromRaw(raw);
    expect(state.name).toBe('bare');
    expect(state.description).toBe('');
    expect(state.instructions).toBe('');
    expect(state.triggers).toEqual([]);
    expect(state.requires).toEqual({ capabilities: [], channels: [] });
    expect(state.budget).toEqual({ daily: null, monthly: null, per_task: null });
    expect(state.health).toEqual({ indicators: [], business_hours: '' });
    expect(state.meeseeks).toBe(false);
    expect(state.meeseeksLimit).toBeNull();
    expect(state.meeseeksModel).toBeNull();
    expect(state.meeseeksBudget).toBeNull();
  });
});

describe('cost_mode serialization', () => {
  it('serializes cost_mode and new fields to YAML', () => {
    const state = {
      ...emptyWizardState(),
      name: 'test',
      cost_mode: 'balanced' as const,
      reflection: { enabled: true, max_rounds: 2, criteria: ['correctness'] },
      success_criteria: {
        checklist: [{ id: 'c1', description: 'Tests pass', required: true }],
        evaluation: { enabled: true, mode: 'checklist_only' as const, on_failure: 'flag' as const },
      },
      procedural_memory: { capture: true, retrieve: true, max_retrieved: 5 },
      episodic_memory: { capture: true, retrieve: true, max_retrieved: 10, tool_enabled: false },
    };
    const yaml = serializeToYaml(state);
    expect(yaml).toContain('cost_mode: balanced');
    expect(yaml).toContain('reflection:');
    expect(yaml).toContain('max_rounds: 2');
    expect(yaml).toContain('success_criteria:');
    expect(yaml).toContain('procedural_memory:');
    expect(yaml).toContain('episodic_memory:');
  });

  it('parses new fields from raw mission', () => {
    const raw = {
      name: 'test',
      cost_mode: 'thorough',
      reflection: { enabled: true, max_rounds: 5, criteria: ['accuracy'] },
      success_criteria: {
        checklist: [{ id: 'c1', description: 'Done', required: false }],
        evaluation: { enabled: true, mode: 'llm', on_failure: 'retry' },
      },
      procedural_memory: { capture: true, retrieve: false, max_retrieved: 3 },
      episodic_memory: { capture: true, retrieve: true, max_retrieved: 8, tool_enabled: true },
    };
    const state = parseFromRaw(raw as any);
    expect(state.cost_mode).toBe('thorough');
    expect(state.reflection?.max_rounds).toBe(5);
    expect(state.success_criteria?.checklist).toHaveLength(1);
    expect(state.episodic_memory?.tool_enabled).toBe(true);
  });

  it('omits cost_mode fields when not set', () => {
    const state = emptyWizardState();
    state.name = 'basic';
    const yaml = serializeToYaml(state);
    expect(yaml).not.toContain('cost_mode');
    expect(yaml).not.toContain('reflection');
    expect(yaml).not.toContain('success_criteria');
  });
});

describe('round-trip', () => {
  it('parseFromRaw then serializeToYaml preserves fields', () => {
    const raw: RawMission = {
      name: 'roundtrip',
      description: 'Round trip test',
      status: 'active',
      instructions: 'Line one\nLine two',
      triggers: [{ source: 'webhook', name: 'deploy-hook' }],
      requires: { capabilities: ['docker'] },
      budget: { daily: 5 },
      health: { indicators: ['cpu'], business_hours: '09:00-17:00' },
      meeseeks: true,
      meeseeks_limit: 2,
      meeseeks_model: 'haiku',
      meeseeks_budget: 1.0,
    };

    const state = parseFromRaw(raw);
    const yaml = serializeToYaml(state);

    expect(yaml).toContain('name: roundtrip');
    expect(yaml).toContain('description: Round trip test');
    expect(yaml).toContain('instructions: |');
    expect(yaml).toContain('  Line one');
    expect(yaml).toContain('  Line two');
    expect(yaml).toContain('  - source: webhook');
    expect(yaml).toContain('    name: deploy-hook');
    expect(yaml).toContain('    - docker');
    expect(yaml).toContain('  daily: 5');
    expect(yaml).toContain('    - cpu');
    expect(yaml).toContain('meeseeks: true');
    expect(yaml).toContain('meeseeks_limit: 2');
    expect(yaml).toContain('meeseeks_model: haiku');
    expect(yaml).toContain('meeseeks_budget: 1');
  });
});
