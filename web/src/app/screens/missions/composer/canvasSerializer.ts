import type { CanvasDocument, CanvasNode, CanvasEdge, MissionFragment } from './canvasTypes';
import { getNodeDef } from './nodeRegistry';
import type { WizardState, WizardTrigger } from '../serialize';

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

    const fragment = def.serialize(node.data.config || {}) as MissionFragment;

    // Merge fragment into state
    if (fragment.triggers) triggers.push(...fragment.triggers);
    if (fragment.requires?.capabilities) requires.capabilities.push(...fragment.requires.capabilities);
    if (fragment.requires?.channels) requires.channels.push(...fragment.requires.channels);
    if (fragment.budget) budget = { ...budget, ...fragment.budget };
    if (fragment.fallback) {
      if (!fallback) fallback = fragment.fallback as Record<string, unknown>;
      else {
        // Merge fallback policies
        const existing = (fallback as { policies?: unknown[] }).policies || [];
        const incoming = (fragment.fallback as { policies?: unknown[] }).policies || [];
        (fallback as { policies: unknown[] }).policies = [...existing, ...incoming];
      }
    }
    if (fragment.success_criteria) success_criteria = fragment.success_criteria as Record<string, unknown>;
    if (fragment.reflection) reflection = fragment.reflection as Record<string, unknown>;
  }

  // Assemble WizardState
  return {
    name: (agentConfig.name as string) || '',
    description: (agentConfig.description as string) || '',
    instructions: (agentConfig.instructions as string) || '',
    triggers: triggers.map(t => ({
      source: t.source as WizardTrigger['source'],
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
    meeseeksModel: (agentConfig.meeseeks_model as 'fast' | 'standard' | 'frontier') || 'fast',
    meeseeksBudget: (agentConfig.meeseeks_budget as number) || 0.5,
    assignTarget: '',
    assignType: 'agent',
    cost_mode: agentConfig.cost_mode as 'frugal' | 'balanced' | 'thorough' | undefined,
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
