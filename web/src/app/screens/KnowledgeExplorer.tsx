import { useState, useEffect, useCallback, lazy, Suspense } from 'react';
import { useParams, useNavigate } from 'react-router';
import { api } from '../lib/api';
import { Button } from '../components/ui/button';
import { RefreshCw, Network, List, Search, Database } from 'lucide-react';
import { KnowledgeNode, ViewMode } from './knowledge/types';
import { KIND_COLORS } from './knowledge/constants';
import { GraphView } from './knowledge/GraphView';
import { NodeBrowser } from './knowledge/NodeBrowser';
import { NodeDetail } from './knowledge/NodeDetail';

const KnowledgeSearch = lazy(() => import('./Knowledge').then((m) => ({ default: m.Knowledge })));

export function KnowledgeExplorer() {
  const { view: urlView } = useParams<{ view?: string }>();
  const navigate = useNavigate();
  const validViews: ViewMode[] = ['browser', 'graph', 'search'];
  const initialView = validViews.includes(urlView as ViewMode) ? (urlView as ViewMode) : 'browser';
  const [view, setViewState] = useState<ViewMode>(initialView);

  const setView = useCallback((v: ViewMode) => {
    setViewState(v);
    navigate(`/knowledge/${v}`, { replace: true });
  }, [navigate]);

  // Sync if URL changes externally
  useEffect(() => {
    if (urlView && validViews.includes(urlView as ViewMode) && urlView !== view) {
      setViewState(urlView as ViewMode);
    }
  }, [urlView]); // eslint-disable-line react-hooks/exhaustive-deps

  const [nodes, setNodes] = useState<KnowledgeNode[]>([]);
  const [graphEdges, setGraphEdges] = useState<Array<{ source: string; target: string; relation: string }>>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedNode, setSelectedNode] = useState<KnowledgeNode | null>(null);

  const loadNodes = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const raw = (await api.knowledge.export()) || [];
      // Filter to nodes only (export may include edges), ensure required fields
      const data = (raw as any[])
        .filter((n) => n.type === 'node' || (n.label && n.kind))
        .map((n) => ({
          ...n,
          label: n.label || n.source || 'unknown',
          kind: n.kind || 'unknown',
          contributed_by: n.contributed_by || (n.properties as any)?.contributed_by,
        }));
      setNodes(data as KnowledgeNode[]);
      // Extract real edges from export
      const edges = (raw as any[])
        .filter((n) => n.type === 'edge' && n.source && n.target)
        .map((e) => ({ source: e.source as string, target: e.target as string, relation: (e.relation || '') as string }));
      setGraphEdges(edges);
    } catch (e: any) {
      setError(e.message || 'Failed to load knowledge graph');
      setNodes([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadNodes();
  }, [loadNodes]);

  // Stats
  const kindCounts: Record<string, number> = {};
  const sourceCounts: Record<string, number> = {};
  for (const n of nodes) {
    kindCounts[n.kind] = (kindCounts[n.kind] || 0) + 1;
    if (n.source_type) sourceCounts[n.source_type] = (sourceCounts[n.source_type] || 0) + 1;
  }

  return (
    <div className="h-full flex flex-col">
      {/* Header */}
      <div className="border-b border-border px-4 md:px-8 py-4 flex items-center justify-between">
        <div>
          <h1 className="text-xl text-foreground">Knowledge</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Browse and explore the organizational knowledge graph
          </p>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex rounded-md border border-border overflow-hidden">
            <button
              onClick={() => setView('browser')}
              className={`px-3 py-1.5 text-xs font-medium flex items-center gap-1.5 transition-colors ${
                view === 'browser'
                  ? 'bg-secondary text-foreground'
                  : 'text-muted-foreground hover:text-foreground hover:bg-secondary/50'
              }`}
            >
              <List className="w-3.5 h-3.5" />
              Browser
            </button>
            <button
              onClick={() => setView('graph')}
              className={`px-3 py-1.5 text-xs font-medium flex items-center gap-1.5 transition-colors border-l border-border ${
                view === 'graph'
                  ? 'bg-secondary text-foreground'
                  : 'text-muted-foreground hover:text-foreground hover:bg-secondary/50'
              }`}
            >
              <Network className="w-3.5 h-3.5" />
              Graph
            </button>
            <button
              onClick={() => setView('search')}
              className={`px-3 py-1.5 text-xs font-medium flex items-center gap-1.5 transition-colors border-l border-border ${
                view === 'search'
                  ? 'bg-secondary text-foreground'
                  : 'text-muted-foreground hover:text-foreground hover:bg-secondary/50'
              }`}
            >
              <Search className="w-3.5 h-3.5" />
              Search
            </button>
          </div>
          <Button
            size="sm"
            variant="outline"
            onClick={loadNodes}
            disabled={loading}
            className="ml-2"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
          </Button>
        </div>
      </div>

      {/* Stats Bar — hidden on graph view since kind filter pills serve as legend */}
      {view === 'browser' && (
        <div className="border-b border-border px-4 md:px-8 py-2 flex flex-wrap items-center gap-x-6 gap-y-1 text-xs text-muted-foreground overflow-hidden">
          <span>
            <span className="font-medium text-foreground">{nodes.length}</span> nodes
          </span>
          <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span className="text-[10px] uppercase tracking-widest text-muted-foreground/60">Kind</span>
            {Object.entries(kindCounts).map(([kind, count]) => (
              <span key={kind} className="flex items-center gap-1">
                <span className="w-2 h-2 rounded-full" style={{ backgroundColor: KIND_COLORS[kind] || KIND_COLORS.unknown }} />
                {count} {kind}{count !== 1 ? 's' : ''}
              </span>
            ))}
          </span>
          <span className="w-px h-4 bg-border" />
          <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span className="text-[10px] uppercase tracking-widest text-muted-foreground/60">Source</span>
            {Object.entries(sourceCounts).map(([src, count]) => (
              <span key={src} className="flex items-center gap-1">
                <span className={`w-2 h-2 rounded-full ${
                  src === 'agent' ? 'bg-green-500' : src === 'llm' ? 'bg-cyan-500' : src === 'rule' ? 'bg-amber-500' : 'bg-gray-500'
                }`} />
                {count} {src}
              </span>
            ))}
          </span>
        </div>
      )}

      {/* Error */}
      {error && (
        <div className="mx-4 md:mx-8 mt-4 text-xs text-amber-700 dark:text-amber-400 bg-amber-50 dark:bg-amber-950/20 border border-amber-200 dark:border-amber-900/50 rounded px-3 py-2">
          {error.includes('502') || error.includes('503')
            ? 'Knowledge service is starting up. Try again in a moment.'
            : error}
        </div>
      )}

      {/* Content */}
      <div className="flex-1 overflow-hidden flex">
        {view === 'search' ? (
          <div className="flex-1 p-4 md:p-8 overflow-auto">
            <Suspense fallback={<div className="text-sm text-muted-foreground text-center py-16">Loading search...</div>}>
              <KnowledgeSearch onSelectResult={(label, kind) => {
                const match = nodes.find((n) => n.label === label && n.kind === kind);
                if (match) {
                  setSelectedNode(match);
                  setView('browser');
                }
              }} />
            </Suspense>
          </div>
        ) : view === 'browser' ? (
          <div className="flex-1 flex overflow-hidden">
            <div className={`flex-1 overflow-auto p-4 md:p-8 ${selectedNode ? 'border-r border-border' : ''}`}>
              {loading ? (
                <div className="text-sm text-muted-foreground text-center py-16">Loading knowledge graph...</div>
              ) : nodes.length === 0 ? (
                <div className="text-center py-16">
                  <Database className="w-8 h-8 text-muted-foreground/70 mx-auto mb-2" />
                  <p className="text-sm text-muted-foreground mb-1">Knowledge graph is empty</p>
                  <p className="text-xs text-muted-foreground/70">Add agents or content to populate the knowledge graph.</p>
                </div>
              ) : (
                <NodeBrowser nodes={nodes} selectedNode={selectedNode} onSelectNode={setSelectedNode} />
              )}
            </div>
            {selectedNode && (
              <div className="w-96 overflow-auto p-4 bg-card">
                <NodeDetail node={selectedNode} allNodes={nodes} onClose={() => setSelectedNode(null)} onNavigate={setSelectedNode} />
              </div>
            )}
          </div>
        ) : (
          <div className="flex-1 flex overflow-hidden">
            <div className={`flex-1 overflow-hidden ${selectedNode ? 'border-r border-border' : ''}`}>
              {loading ? (
                <div className="text-sm text-muted-foreground text-center py-16">Loading knowledge graph...</div>
              ) : nodes.length === 0 ? (
                <div className="text-center py-16">
                  <Database className="w-8 h-8 text-muted-foreground/70 mx-auto mb-2" />
                  <p className="text-sm text-muted-foreground mb-1">Knowledge graph is empty</p>
                  <p className="text-xs text-muted-foreground/70">Add agents or content to populate the knowledge graph.</p>
                </div>
              ) : (
                <GraphView nodes={nodes} realEdges={graphEdges} selectedNode={selectedNode} onSelectNode={setSelectedNode} />
              )}
            </div>
            {selectedNode && (
              <div className="w-96 overflow-auto p-4 bg-card">
                <NodeDetail node={selectedNode} allNodes={nodes} onClose={() => setSelectedNode(null)} onNavigate={setSelectedNode} />
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
