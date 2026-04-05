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
