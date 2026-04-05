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
