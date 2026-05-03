import { useState, useEffect, useCallback, useMemo } from 'react';
import { useParams, useNavigate } from 'react-router';
import { api } from '../lib/api';
import { Database, Plus, RefreshCw, Search } from 'lucide-react';
import { KnowledgeNode } from './knowledge/types';
import { KIND_COLORS } from './knowledge/constants';
import { GraphView } from './knowledge/GraphView';
import { formatDateTimeShort } from '../lib/time';

type KnowledgeEdge = { source: string; target: string; relation: string };
type KnowledgeView = 'graph' | 'browser' | 'search';

const VIEW_ALIASES: Record<string, KnowledgeView> = {
  graph: 'graph',
  browse: 'browser',
  browser: 'browser',
  search: 'search',
};

const VIEW_LABELS: Array<[KnowledgeView, string]> = [
  ['graph', 'Graph'],
  ['browser', 'Browse'],
  ['search', 'Search'],
];

function normalizeNode(raw: any): KnowledgeNode {
  return {
    ...raw,
    label: raw.label || raw.source || raw.id || 'unknown',
    kind: raw.kind || 'unknown',
    contributed_by: raw.contributed_by || raw.properties?.contributed_by,
  };
}

function nodeKey(node: KnowledgeNode): string {
  return String((node as any).id || node.label);
}

function nodeScore(node: KnowledgeNode, edges: KnowledgeEdge[]): number {
  const key = nodeKey(node);
  const edgeCount = edges.filter((edge) => edge.source === key || edge.target === key || edge.source === node.label || edge.target === node.label).length;
  return Number((node as any).connections || 0) + edgeCount + (node.summary ? 1 : 0);
}

function kindColor(kind: string): string {
  return KIND_COLORS[kind] || KIND_COLORS[kind?.toLowerCase?.()] || KIND_COLORS.unknown;
}

function asArray(value: unknown): any[] {
  return Array.isArray(value) ? value : [];
}

function resultNodes(value: unknown): KnowledgeNode[] {
  const payload = value as any;
  const items = Array.isArray(payload) ? payload : asArray(payload?.results || payload?.nodes || payload?.matches);
  return items
    .filter((item) => item && (item.label || item.id || item.source))
    .map(normalizeNode);
}

function relativeIndexedTime(nodes: KnowledgeNode[]): string {
  const latest = nodes
    .map((node) => Date.parse(String(node.updated_at || node.created_at || '')))
    .filter((time) => Number.isFinite(time))
    .sort((a, b) => b - a)[0];
  if (!latest) return 'last indexed unknown';
  const minutes = Math.max(0, Math.round((Date.now() - latest) / 60_000));
  if (minutes < 1) return 'last indexed just now';
  if (minutes < 60) return `last indexed ${minutes} min ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 48) return `last indexed ${hours}h ago`;
  return `last indexed ${Math.round(hours / 24)}d ago`;
}

function KnowledgeHeader({
  nodes,
  edges,
  view,
  loading,
  onViewChange,
  onRefresh,
  onIngest,
}: {
  nodes: KnowledgeNode[];
  edges: KnowledgeEdge[];
  view: KnowledgeView;
  loading: boolean;
  onViewChange: (view: KnowledgeView) => void;
  onRefresh: () => void;
  onIngest: () => void;
}) {
  return (
    <div className="knowledge-header" style={{ minHeight: 58, padding: '8px 16px', borderBottom: '0.5px solid var(--ink-hairline)', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 14, flexWrap: 'wrap', background: 'var(--warm)' }}>
      <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap', minWidth: 0 }}>
        <div className="eyebrow" style={{ fontSize: 9 }}>Knowledge</div>
        <div style={{ display: 'inline-flex', background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 999, padding: 2, gap: 2 }}>
          {VIEW_LABELS.map(([id, label]) => (
            <button key={id} type="button" onClick={() => onViewChange(id)} style={{ padding: '5px 13px', border: 0, background: view === id ? 'var(--ink)' : 'transparent', color: view === id ? 'var(--warm)' : 'var(--ink-mid)', fontFamily: 'var(--sans)', fontSize: 12, borderRadius: 999, cursor: 'pointer' }}>
              {label}
            </button>
          ))}
        </div>
        <div className="mono" style={{ fontSize: 10, color: 'var(--ink-faint)', whiteSpace: 'nowrap' }}>
          {nodes.length.toLocaleString()} nodes · {edges.length.toLocaleString()} edges · {relativeIndexedTime(nodes)}
        </div>
      </div>
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
        <button type="button" onClick={onRefresh} disabled={loading} style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '5px 10px', fontSize: 12, fontFamily: 'var(--sans)', cursor: loading ? 'default' : 'pointer', background: 'var(--warm)', color: 'var(--ink)', border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 999, opacity: loading ? 0.55 : 1 }}>
          <RefreshCw size={13} className={loading ? 'animate-spin' : ''} />
          Refresh
        </button>
        <button type="button" onClick={onIngest} style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '5px 10px', fontSize: 12, fontFamily: 'var(--sans)', background: 'var(--ink)', color: 'var(--warm)', border: '0.5px solid var(--ink)', borderRadius: 999, cursor: 'pointer' }}>
          <Plus size={13} />
          Ingest
        </button>
      </div>
    </div>
  );
}

function KnowledgeBrowseSurface({ nodes, selectedNode, onSelectNode }: { nodes: KnowledgeNode[]; selectedNode: KnowledgeNode | null; onSelectNode: (node: KnowledgeNode) => void }) {
  const [query, setQuery] = useState('');
  const [kind, setKind] = useState('all');
  const kinds = useMemo(() => [...new Set(nodes.map((node) => node.kind).filter(Boolean))].sort(), [nodes]);
  const filtered = nodes.filter((node) => {
    if (kind !== 'all' && node.kind !== kind) return false;
    if (!query.trim()) return true;
    const needle = query.toLowerCase();
    return node.label.toLowerCase().includes(needle) || String(node.summary || '').toLowerCase().includes(needle);
  });

  return (
    <div style={{ padding: 28, background: 'var(--warm)', overflow: 'auto', height: '100%' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 22, flexWrap: 'wrap' }}>
        <label style={{ flex: '1 1 260px', position: 'relative' }}>
          <Search size={14} style={{ position: 'absolute', left: 12, top: '50%', transform: 'translateY(-50%)', color: 'var(--ink-faint)' }} />
          <input id="knowledge-node-filter" name="knowledge-node-filter" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Filter nodes..." style={{ width: '100%', height: 38, border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm-2)', color: 'var(--ink)', outline: 0, padding: '0 12px 0 34px', fontSize: 13 }} />
        </label>
        <select id="knowledge-kind-filter" name="knowledge-kind-filter" value={kind} onChange={(event) => setKind(event.target.value)} style={{ height: 38, border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm-2)', color: 'var(--ink)', outline: 0, padding: '0 12px', fontSize: 12 }}>
          <option value="all">All kinds</option>
          {kinds.map((item) => <option key={item} value={item}>{item}</option>)}
        </select>
        <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)' }}>{filtered.length.toLocaleString()} nodes</span>
      </div>
      <div style={{ borderTop: '0.5px solid var(--ink-hairline)' }}>
        {filtered.slice(0, 120).map((node, index) => {
          const active = selectedNode?.label === node.label && selectedNode?.kind === node.kind;
          return (
            <button key={`${node.kind}-${node.label}-${index}`} type="button" onClick={() => onSelectNode(node)} style={{ width: '100%', display: 'grid', gridTemplateColumns: '44px minmax(0, 1fr) auto', gap: 12, alignItems: 'start', textAlign: 'left', padding: '14px 0', border: 0, borderBottom: '0.5px solid var(--ink-hairline)', background: active ? 'var(--teal-tint)' : 'transparent', cursor: 'pointer' }}>
              <span className="mono" style={{ color: 'var(--ink-faint)', fontSize: 11, paddingLeft: 10 }}>{String(index + 1).padStart(2, '0')}</span>
              <span style={{ minWidth: 0 }}>
                <span className="mono" style={{ display: 'block', color: 'var(--ink)', fontSize: 13, overflowWrap: 'anywhere' }}>{node.label}</span>
                {node.summary && <span style={{ display: 'block', color: 'var(--ink-mid)', fontSize: 12, marginTop: 4, lineHeight: 1.45 }}>{node.summary}</span>}
              </span>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 7, color: 'var(--ink-mid)', fontSize: 11, paddingRight: 10 }}>
                <span style={{ width: 8, height: 8, borderRadius: 2, background: kindColor(node.kind) }} />
                {node.kind}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

function KnowledgeSearchSurface({ nodes, onSelectNode }: { nodes: KnowledgeNode[]; onSelectNode: (node: KnowledgeNode) => void }) {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<KnowledgeNode[]>([]);
  const [whoKnows, setWhoKnows] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const localMatches = query.trim() ? nodes.filter((node) => `${node.label} ${node.kind} ${node.summary || ''}`.toLowerCase().includes(query.toLowerCase())).slice(0, 24) : [];
  const visibleResults = results.length > 0 ? results : localMatches;

  const runSearch = useCallback(async () => {
    if (!query.trim()) return;
    try {
      setLoading(true);
      setError(null);
      const [queryData, whoKnowsData] = await Promise.all([
        api.knowledge.query(query.trim()),
        api.knowledge.whoKnows(query.trim()).catch(() => null),
      ]);
      setResults(resultNodes(queryData));
      setWhoKnows(asArray((whoKnowsData as any)?.agents));
    } catch (event: any) {
      setError(event.message || 'Knowledge query failed');
      setResults([]);
      setWhoKnows([]);
    } finally {
      setLoading(false);
    }
  }, [query]);

  return (
    <div style={{ padding: 32, background: 'var(--warm)', overflow: 'auto', height: '100%' }}>
      <div style={{ maxWidth: 720 }}>
        <div className="eyebrow" style={{ marginBottom: 10 }}>Search</div>
        <h2 className="display" style={{ margin: 0, fontSize: 30, fontWeight: 300, color: 'var(--ink)' }}>Query graph memory</h2>
        <p style={{ margin: '8px 0 20px', color: 'var(--ink-mid)', fontSize: 13 }}>Runs the gateway knowledge query and who-knows surfaces, with local export matches as an immediate fallback.</p>
        <div style={{ display: 'flex', gap: 8 }}>
          <input id="knowledge-search-query" name="knowledge-search-query" autoFocus value={query} onChange={(event) => { setQuery(event.target.value); setResults([]); }} onKeyDown={(event) => { if (event.key === 'Enter') runSearch(); }} placeholder="governance, build, field notes..." style={{ flex: 1, height: 48, border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 12, background: 'var(--warm-2)', color: 'var(--ink)', outline: 0, padding: '0 16px', fontSize: 15 }} />
          <button type="button" onClick={runSearch} disabled={loading || !query.trim()} style={{ height: 48, border: '0.5px solid var(--ink)', borderRadius: 12, background: 'var(--ink)', color: 'var(--warm)', padding: '0 18px', opacity: loading || !query.trim() ? 0.5 : 1, cursor: loading || !query.trim() ? 'default' : 'pointer' }}>{loading ? 'Searching...' : 'Search'}</button>
        </div>
      </div>
      {error && <div style={{ marginTop: 14, maxWidth: 720, padding: '9px 12px', border: '0.5px solid var(--amber)', borderRadius: 8, background: 'var(--warm-2)', color: 'var(--ink)', fontSize: 12 }}>{error.includes('502') || error.includes('503') ? 'Knowledge service is starting up. Try again in a moment.' : error}</div>}
      {whoKnows.length > 0 && (
        <div style={{ marginTop: 18, maxWidth: 860 }}>
          <div className="eyebrow" style={{ marginBottom: 8 }}>Who knows</div>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            {whoKnows.slice(0, 8).map((agent) => (
              <div key={agent.name || agent.agent || JSON.stringify(agent)} style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 999, background: 'var(--warm-2)', color: 'var(--ink)', padding: '5px 10px', fontSize: 12 }}>
                {agent.name || agent.agent || 'agent'} {agent.confidence != null ? <span style={{ color: 'var(--ink-faint)' }}>{Math.round(Number(agent.confidence) * 100)}%</span> : null}
              </div>
            ))}
          </div>
        </div>
      )}
      <div style={{ marginTop: 24, maxWidth: 860, borderTop: query ? '0.5px solid var(--ink-hairline)' : 0 }}>
        {visibleResults.map((node, index) => (
          <button key={`${node.kind}-${node.label}-${index}`} type="button" onClick={() => onSelectNode(node)} style={{ width: '100%', textAlign: 'left', border: 0, borderBottom: '0.5px solid var(--ink-hairline)', background: 'transparent', padding: '15px 0', cursor: 'pointer' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span style={{ width: 8, height: 8, borderRadius: 2, background: kindColor(node.kind) }} />
              <span className="mono" style={{ color: 'var(--ink)', fontSize: 13 }}>{node.label}</span>
              <span style={{ color: 'var(--ink-faint)', fontSize: 11 }}>{node.kind}</span>
            </div>
            {node.summary && <div style={{ marginTop: 6, color: 'var(--ink-mid)', fontSize: 12, lineHeight: 1.45 }}>{node.summary}</div>}
          </button>
        ))}
        {query && visibleResults.length === 0 && !loading && <div style={{ padding: '24px 0', color: 'var(--ink-mid)', fontSize: 13 }}>No matching nodes.</div>}
      </div>
    </div>
  );
}

function KnowledgeRail({ nodes, edges, selectedNode, onSelectNode }: { nodes: KnowledgeNode[]; edges: KnowledgeEdge[]; selectedNode: KnowledgeNode | null; onSelectNode: (node: KnowledgeNode) => void }) {
  const [neighbors, setNeighbors] = useState<any[]>([]);
  const [context, setContext] = useState<any | null>(null);
  const kindCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    nodes.forEach((node) => { counts[node.kind || 'unknown'] = (counts[node.kind || 'unknown'] || 0) + 1; });
    return Object.entries(counts).sort((a, b) => b[1] - a[1]);
  }, [nodes]);
  const topNodes = useMemo(() => [...nodes].sort((a, b) => nodeScore(b, edges) - nodeScore(a, edges) || a.label.localeCompare(b.label)).slice(0, 8), [nodes, edges]);

  useEffect(() => {
    let cancelled = false;
    setNeighbors([]);
    setContext(null);
    if (!selectedNode) return;
    Promise.all([
      api.knowledge.neighbors(selectedNode.label).catch(() => null),
      api.knowledge.context(selectedNode.label).catch(() => null),
    ]).then(([neighborData, contextData]) => {
      if (cancelled) return;
      setNeighbors(asArray((neighborData as any)?.neighbors || (neighborData as any)?.nodes || (neighborData as any)?.results).slice(0, 6));
      setContext(contextData);
    });
    return () => { cancelled = true; };
  }, [selectedNode]);

  return (
    <aside className="knowledge-rail" style={{ borderLeft: '0.5px solid var(--ink-hairline)', padding: 24, background: 'var(--warm-2)', overflowY: 'auto' }}>
      {selectedNode && (
        <div style={{ marginBottom: 22, paddingBottom: 20, borderBottom: '0.5px solid var(--ink-hairline)' }}>
          <div className="eyebrow" style={{ marginBottom: 10 }}>Selected</div>
          <div className="display" style={{ fontSize: 24, fontWeight: 300, letterSpacing: '-0.02em', color: 'var(--ink)', overflowWrap: 'anywhere' }}>{selectedNode.label}</div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 10, flexWrap: 'wrap' }}>
            <span style={{ width: 8, height: 8, borderRadius: 2, background: kindColor(selectedNode.kind) }} />
            <span className="mono" style={{ color: 'var(--ink-mid)', fontSize: 11 }}>{selectedNode.kind}</span>
            {selectedNode.updated_at && <span className="mono" style={{ color: 'var(--ink-faint)', fontSize: 10 }}>{formatDateTimeShort(selectedNode.updated_at)}</span>}
          </div>
          {selectedNode.summary && <p style={{ margin: '12px 0 0', color: 'var(--ink-mid)', fontSize: 12, lineHeight: 1.5 }}>{selectedNode.summary}</p>}
          {(neighbors.length > 0 || context) && (
            <div style={{ marginTop: 14, display: 'flex', flexDirection: 'column', gap: 8 }}>
              {neighbors.length > 0 && (
                <div>
                  <div className="eyebrow" style={{ marginBottom: 6 }}>Neighbors</div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                    {neighbors.map((neighbor, index) => {
                      const label = String(neighbor.label || neighbor.id || neighbor.target || neighbor.source || `neighbor-${index}`);
                      return <div key={`${label}-${index}`} className="mono" style={{ fontSize: 11, color: 'var(--ink-mid)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{label}</div>;
                    })}
                  </div>
                </div>
              )}
              {context && (
                <div style={{ padding: 10, border: '0.5px solid var(--ink-hairline)', borderRadius: 8, background: 'var(--warm)' }}>
                  <div className="eyebrow" style={{ marginBottom: 5 }}>Context</div>
                  <div style={{ fontSize: 11, color: 'var(--ink-mid)', lineHeight: 1.45 }}>
                    {String((context as any).summary || (context as any).context || (context as any).text || JSON.stringify(context)).slice(0, 220)}
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      )}
      <div className="eyebrow" style={{ marginBottom: 10 }}>Kinds</div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        {kindCounts.slice(0, 8).map(([kind, count]) => (
          <div key={kind} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '8px 10px', background: 'var(--warm)', border: '0.5px solid var(--ink-hairline)', borderRadius: 8 }}>
            <span style={{ width: 8, height: 8, borderRadius: 2, background: kindColor(kind) }} />
            <span style={{ flex: 1, fontSize: 12, color: 'var(--ink)' }}>{kind}</span>
            <span className="mono" style={{ fontSize: 11, color: 'var(--ink-mid)' }}>{count}</span>
          </div>
        ))}
        {kindCounts.length === 0 && <div style={{ color: 'var(--ink-mid)', fontSize: 12 }}>No kinds indexed.</div>}
      </div>
      <div className="eyebrow" style={{ marginTop: 20, marginBottom: 10 }}>Top entities</div>
      <div style={{ fontSize: 12, display: 'flex', flexDirection: 'column', gap: 8 }}>
        {topNodes.map((node, index) => (
          <button key={`${node.kind}-${node.label}`} type="button" onClick={() => onSelectNode(node)} style={{ display: 'flex', gap: 8, border: 0, padding: 0, background: 'transparent', textAlign: 'left', cursor: 'pointer' }}>
            <span className="mono" style={{ color: 'var(--ink-faint)', width: 20 }}>{String(index + 1).padStart(2, '0')}</span>
            <span className="mono" style={{ color: 'var(--ink)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{node.label}</span>
            <span className="mono" style={{ color: 'var(--ink-faint)' }}>{nodeScore(node, edges)}</span>
          </button>
        ))}
        {topNodes.length === 0 && <div style={{ color: 'var(--ink-mid)', fontSize: 12 }}>No entities loaded.</div>}
      </div>
    </aside>
  );
}

function IngestDialog({
  open,
  loading,
  error,
  onClose,
  onSubmit,
}: {
  open: boolean;
  loading: boolean;
  error: string | null;
  onClose: () => void;
  onSubmit: (input: { content: string; filename: string; content_type: string }) => void;
}) {
  const [content, setContent] = useState('');
  const [filename, setFilename] = useState('operator-note.md');
  const [contentType, setContentType] = useState('text/markdown');

  useEffect(() => {
    if (!open) return;
    setContent('');
    setFilename('operator-note.md');
    setContentType('text/markdown');
  }, [open]);

  if (!open) return null;
  return (
    <div style={{ position: 'fixed', inset: 0, zIndex: 60, display: 'grid', placeItems: 'center', background: 'rgba(26, 23, 20, 0.28)' }}>
      <div style={{ width: 'min(680px, calc(100vw - 32px))', border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 18, background: 'var(--warm)', color: 'var(--ink)', padding: 22 }}>
        <div className="eyebrow" style={{ marginBottom: 10 }}>Ingest</div>
        <h2 className="display" style={{ margin: 0, fontSize: 30, fontWeight: 300 }}>Add graph source</h2>
        <p style={{ margin: '8px 0 18px', color: 'var(--ink-mid)', fontSize: 13 }}>Operator-supplied content is sent to the gateway ingest contract and then the graph is refreshed.</p>
        <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) 180px', gap: 10, marginBottom: 10 }}>
          <input id="knowledge-ingest-filename" name="knowledge-ingest-filename" value={filename} onChange={(event) => setFilename(event.target.value)} placeholder="filename.md" style={{ height: 38, border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm-2)', color: 'var(--ink)', outline: 0, padding: '0 12px', fontSize: 13 }} />
          <select id="knowledge-ingest-content-type" name="knowledge-ingest-content-type" value={contentType} onChange={(event) => setContentType(event.target.value)} style={{ height: 38, border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm-2)', color: 'var(--ink)', outline: 0, padding: '0 12px', fontSize: 12 }}>
            <option value="text/markdown">Markdown</option>
            <option value="text/plain">Plain text</option>
            <option value="application/json">JSON</option>
          </select>
        </div>
        <textarea id="knowledge-ingest-content" name="knowledge-ingest-content" value={content} onChange={(event) => setContent(event.target.value)} placeholder="Paste notes, source text, or structured knowledge..." style={{ width: '100%', minHeight: 220, border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', color: 'var(--ink)', outline: 0, padding: 14, fontSize: 13, lineHeight: 1.5, resize: 'vertical' }} />
        {error && <div style={{ marginTop: 10, padding: '9px 12px', border: '0.5px solid var(--amber)', borderRadius: 8, background: 'var(--warm-2)', color: 'var(--ink)', fontSize: 12 }}>{error}</div>}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 16 }}>
          <button type="button" onClick={onClose} disabled={loading} style={{ border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 999, background: 'var(--warm)', color: 'var(--ink)', padding: '7px 13px', cursor: loading ? 'default' : 'pointer' }}>Cancel</button>
          <button type="button" onClick={() => onSubmit({ content, filename, content_type: contentType })} disabled={loading || !content.trim()} style={{ border: '0.5px solid var(--ink)', borderRadius: 999, background: 'var(--ink)', color: 'var(--warm)', padding: '7px 13px', opacity: loading || !content.trim() ? 0.5 : 1, cursor: loading || !content.trim() ? 'default' : 'pointer' }}>{loading ? 'Ingesting...' : 'Ingest source'}</button>
        </div>
      </div>
    </div>
  );
}

function EmptyState({ loading }: { loading: boolean }) {
  return (
    <div style={{ display: 'grid', placeItems: 'center', minHeight: 360, background: 'var(--warm)' }}>
      <div style={{ textAlign: 'center', color: 'var(--ink-mid)' }}>
        <Database style={{ margin: '0 auto 10px' }} size={28} />
        <div style={{ color: 'var(--ink)', fontSize: 16 }}>{loading ? 'Loading knowledge graph...' : 'Knowledge graph is empty'}</div>
        {!loading && <div style={{ marginTop: 6, fontSize: 12 }}>Add agents or content to populate the graph.</div>}
      </div>
    </div>
  );
}

export function KnowledgeExplorer() {
  const { view: urlView } = useParams<{ view?: string }>();
  const navigate = useNavigate();
  const initialView = VIEW_ALIASES[urlView || ''] || 'graph';
  const [view, setViewState] = useState<KnowledgeView>(initialView);
  const [nodes, setNodes] = useState<KnowledgeNode[]>([]);
  const [edges, setEdges] = useState<KnowledgeEdge[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedNode, setSelectedNode] = useState<KnowledgeNode | null>(null);
  const [ingestOpen, setIngestOpen] = useState(false);
  const [ingestLoading, setIngestLoading] = useState(false);
  const [ingestError, setIngestError] = useState<string | null>(null);

  const setView = useCallback((next: KnowledgeView) => {
    setViewState(next);
    navigate(`/knowledge/${next}`, { replace: true });
  }, [navigate]);

  useEffect(() => {
    const next = VIEW_ALIASES[urlView || ''];
    if (next && next !== view) setViewState(next);
  }, [urlView]); // eslint-disable-line react-hooks/exhaustive-deps

  const loadNodes = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const raw = (await api.knowledge.export()) || [];
      const nextNodes = (raw as any[]).filter((item) => item.type === 'node' || (item.label && item.kind)).map(normalizeNode);
      const nextEdges = (raw as any[]).filter((item) => item.type === 'edge' && item.source && item.target).map((edge) => ({ source: String(edge.source), target: String(edge.target), relation: String(edge.relation || '') }));
      setNodes(nextNodes);
      setEdges(nextEdges);
      setSelectedNode((current) => {
        if (!current) return nextNodes[0] || null;
        return nextNodes.find((node) => node.label === current.label && node.kind === current.kind) || nextNodes[0] || null;
      });
    } catch (event: any) {
      setError(event.message || 'Failed to load knowledge graph');
      setNodes([]);
      setEdges([]);
      setSelectedNode(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { loadNodes(); }, [loadNodes]);

  const handleIngest = useCallback(async (input: { content: string; filename: string; content_type: string }) => {
    try {
      setIngestLoading(true);
      setIngestError(null);
      await api.knowledge.ingest(input);
      setIngestOpen(false);
      await loadNodes();
    } catch (event: any) {
      setIngestError(event.message || 'Knowledge ingest failed');
    } finally {
      setIngestLoading(false);
    }
  }, [loadNodes]);

  return (
    <div className="knowledge-page" style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0, background: 'var(--warm)' }}>
      <KnowledgeHeader nodes={nodes} edges={edges} view={view} loading={loading} onViewChange={setView} onRefresh={loadNodes} onIngest={() => setIngestOpen(true)} />
      {error && <div style={{ margin: '12px 40px 0', padding: '9px 12px', border: '0.5px solid var(--amber)', borderRadius: 8, background: 'var(--warm-2)', color: 'var(--ink)', fontSize: 12 }}>{error.includes('502') || error.includes('503') ? 'Knowledge service is starting up. Try again in a moment.' : error}</div>}
      <div className="knowledge-shell-grid" style={{ flex: 1, display: 'grid', minHeight: 0 }}>
        <div className="knowledge-main-pane" style={{ minHeight: 0, overflow: 'hidden' }}>
          {loading || nodes.length === 0 ? (
            <EmptyState loading={loading} />
          ) : view === 'graph' ? (
            <div className="knowledge-graph-host">
              <GraphView nodes={nodes} realEdges={edges} selectedNode={selectedNode} onSelectNode={setSelectedNode} />
            </div>
          ) : view === 'browser' ? (
            <KnowledgeBrowseSurface nodes={nodes} selectedNode={selectedNode} onSelectNode={setSelectedNode} />
          ) : (
            <KnowledgeSearchSurface nodes={nodes} onSelectNode={(node) => { setSelectedNode(node); setView('browser'); }} />
          )}
        </div>
        <KnowledgeRail nodes={nodes} edges={edges} selectedNode={selectedNode} onSelectNode={setSelectedNode} />
      </div>
      <IngestDialog open={ingestOpen} loading={ingestLoading} error={ingestError} onClose={() => setIngestOpen(false)} onSubmit={handleIngest} />
    </div>
  );
}
