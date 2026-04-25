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
      { value: 'standard', label: 'Standard' },
      { value: 'fast', label: 'Fast' },
      { value: 'frontier', label: 'Frontier' },
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
      { value: 'fast', label: 'Fast' },
      { value: 'standard', label: 'Standard' },
      { value: 'frontier', label: 'Frontier' },
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
      meeseeks_model: (data.meeseeks_model as string) || 'fast',
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
