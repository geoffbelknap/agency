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
