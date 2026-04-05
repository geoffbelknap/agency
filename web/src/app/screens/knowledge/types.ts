export interface KnowledgeNode {
  type?: string;
  label: string;
  kind: string;
  summary?: string;
  properties?: Record<string, unknown>;
  source_type?: string;
  contributed_by?: string;
  created_at?: string;
  updated_at?: string;
  [key: string]: unknown;
}

export type ViewMode = 'browser' | 'graph' | 'search';
