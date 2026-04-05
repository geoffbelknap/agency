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
