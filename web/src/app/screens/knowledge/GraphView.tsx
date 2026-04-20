import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import cytoscape from 'cytoscape';
import { KnowledgeNode } from './types';
import { KIND_COLORS, MAX_GRAPH_NODES } from './constants';

// ── Layout ──

type LayoutMode = 'radial' | 'force' | 'timeline' | 'grid';

const LAYOUT_LABELS: Record<LayoutMode, string> = {
  force: 'Force-directed',
  radial: 'Radial (clusters)',
  timeline: 'Timeline',
  grid: 'Grid',
};

// ── Cytoscape Stylesheet ──

const CY_STYLE: cytoscape.StylesheetStyle[] = [
  {
    selector: 'node',
    style: {
      'background-color': 'data(color)',
      'label': 'data(shortLabel)',
      'font-size': '10px',
      'font-family': 'Space Mono, monospace',
      'color': '#1A1714',
      'text-valign': 'bottom',
      'text-margin-y': 4,
      'width': 'data(size)',
      'height': 'data(size)',
      'border-width': 2,
      'border-color': '#FDFAF5',
    },
  },
  {
    selector: 'node:selected',
    style: {
      'border-width': 3,
      'border-color': '#00A882',
    },
  },
  {
    selector: 'edge',
    style: {
      'width': 1,
      'line-color': '#D4CEC8',
      'curve-style': 'bezier',
      'opacity': 0.65,
    },
  },
  {
    selector: 'edge.highlighted',
    style: {
      'line-color': '#00A882',
      'width': 2,
      'opacity': 1,
    },
  },
  {
    selector: 'node.highlighted',
    style: {
      'border-width': 2,
      'border-color': '#00A882',
    },
  },
];

// ── Layout config ──

function getVisibleElements(cy: cytoscape.Core): cytoscape.CollectionReturnValue {
  return cy.elements().filter((element) => element.visible());
}

function getCyLayout(mode: LayoutMode, cy?: cytoscape.Core): cytoscape.LayoutOptions {
  switch (mode) {
    case 'radial':
      return { name: 'concentric', concentric: (n: any) => n.degree(), levelWidth: () => 2, spacingFactor: 1.5, fit: false } as any;
    case 'force':
      return { name: 'cose', animate: false, nodeRepulsion: () => 8000, idealEdgeLength: () => 100, gravity: 0.3, fit: false } as any;
    case 'grid':
      return { name: 'grid', spacingFactor: 1.2, fit: false } as any;
    case 'timeline':
      if (!cy) return { name: 'grid', spacingFactor: 1.2, fit: false } as any;
      {
        const visibleNodes = cy.nodes().filter((node) => node.visible());
        const timestamps = visibleNodes
          .map((node) => Date.parse(String(node.data('created_at') || '')))
          .filter((time) => Number.isFinite(time));
        const min = timestamps.length ? Math.min(...timestamps) : 0;
        const max = timestamps.length ? Math.max(...timestamps) : min + 1;
        const span = Math.max(1, max - min);
        const kinds = [...new Set(visibleNodes.map((node) => String(node.data('kind') || 'unknown')))].sort();
        const laneByKind = new Map(kinds.map((kind, index) => [kind, index]));
        const width = Math.max(900, visibleNodes.length * 18);
        const laneHeight = 82;
        const padX = 80;
        const padY = 70;
        return {
          name: 'preset',
          fit: false,
          positions: (node: any) => {
            const rawTime = Date.parse(String(node.data('created_at') || ''));
            const time = Number.isFinite(rawTime) ? rawTime : min;
            const lane = laneByKind.get(String(node.data('kind') || 'unknown')) || 0;
            const jitter = (Array.from(String(node.id())).reduce((sum, char) => sum + char.charCodeAt(0), 0) % 17) - 8;
            return {
              x: padX + ((time - min) / span) * width,
              y: padY + lane * laneHeight + jitter,
            };
          },
        } as any;
      }
    default:
      return { name: 'cose', animate: false, fit: false } as any;
  }
}

function nodeAliases(node: KnowledgeNode): string[] {
  const props = node.properties || {};
  return [...new Set([
    node.label,
    node.id,
    node.source,
    props.id,
    props.label,
    props.source,
  ].filter(Boolean).map(String))];
}

function formatTimelineDate(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return 'unknown';
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' }).format(new Date(value));
}

// ── GraphView Component ──

export function GraphView({
  nodes,
  realEdges,
  selectedNode,
  onSelectNode,
}: {
  nodes: KnowledgeNode[];
  realEdges?: Array<{ source: string; target: string; relation: string }>;
  selectedNode: KnowledgeNode | null;
  onSelectNode: (n: KnowledgeNode | null) => void;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const cyRef = useRef<cytoscape.Core | null>(null);
  const [layout, setLayout] = useState<LayoutMode>('force');
  const [hiddenKinds, setHiddenKinds] = useState<Set<string>>(new Set());

  const fitVisible = useCallback((padding = 56) => {
    const cy = cyRef.current;
    if (!cy) return;
    requestAnimationFrame(() => {
      cy.resize();
      const visible = getVisibleElements(cy);
      if (visible.length > 0) cy.fit(visible, padding);
    });
  }, []);

  const runLayout = useCallback((mode: LayoutMode, padding = 56) => {
    const cy = cyRef.current;
    if (!cy || cy.elements().length === 0) return;
    cy.resize();
    const layoutInstance = cy.layout(getCyLayout(mode, cy));
    layoutInstance.one('layoutstop', () => fitVisible(padding));
    layoutInstance.run();
  }, [fitVisible]);

  // Build a map from label to node for fast lookup
  const nodeMapRef = useRef<Map<string, KnowledgeNode>>(new Map());
  useEffect(() => {
    const m = new Map<string, KnowledgeNode>();
    for (const n of nodes) {
      for (const alias of nodeAliases(n)) m.set(alias, n);
    }
    nodeMapRef.current = m;
  }, [nodes]);

  // Count connections per node for sizing
  const connectionCounts = useRef<Map<string, number>>(new Map());
  useEffect(() => {
    const counts = new Map<string, number>();
    const aliasToLabel = new Map<string, string>();
    for (const n of nodes) {
      for (const alias of nodeAliases(n)) aliasToLabel.set(alias, n.label);
    }
    if (realEdges) {
      for (const e of realEdges) {
        const source = aliasToLabel.get(e.source);
        const target = aliasToLabel.get(e.target);
        if (!source || !target) continue;
        counts.set(source, (counts.get(source) || 0) + 1);
        counts.set(target, (counts.get(target) || 0) + 1);
      }
    }
    connectionCounts.current = counts;
  }, [nodes, realEdges]);

  // Build Cytoscape elements from nodes and edges (ignoring hiddenKinds — visibility is
  // handled separately via .hide()/.show() to avoid layout thrash).
  const buildElements = useCallback(() => {
    const graphNodes = nodes.length > MAX_GRAPH_NODES ? nodes.slice(0, MAX_GRAPH_NODES) : nodes;
    const nodeLabels = new Set(graphNodes.map((n) => n.label));
    const aliasToLabel = new Map<string, string>();
    for (const n of graphNodes) {
      for (const alias of nodeAliases(n)) aliasToLabel.set(alias, n.label);
    }

    const cyNodes: cytoscape.ElementDefinition[] = graphNodes.map((n) => {
      const conns = connectionCounts.current.get(n.label) || 0;
      const size = Math.max(16, Math.min(50, 16 + conns * 4));
      const shortLabel = n.label.length > 15 ? n.label.slice(0, 12) + '...' : n.label;
      return {
        data: {
          id: n.label,
          shortLabel,
          fullLabel: n.label,
          kind: n.kind,
          color: KIND_COLORS[n.kind] || KIND_COLORS.unknown,
          size,
          created_at: n.created_at || '',
        },
      };
    });

    const edgeSet = new Set<string>();
    const cyEdges: cytoscape.ElementDefinition[] = [];

    // Real edges from the graph export
    if (realEdges) {
      for (const e of realEdges) {
        const source = aliasToLabel.get(e.source);
        const target = aliasToLabel.get(e.target);
        if (source && target && source !== target && nodeLabels.has(source) && nodeLabels.has(target)) {
          const key = `${source}->${target}`;
          if (!edgeSet.has(key)) {
            edgeSet.add(key);
            cyEdges.push({ data: { source, target, relation: e.relation } });
          }
        }
      }
    }

    // Synthetic edges for clustering by contributor
    const byContributor: Record<string, string[]> = {};
    for (const n of graphNodes) {
      const c = n.contributed_by ? String(n.contributed_by) : '';
      if (c) (byContributor[c] ??= []).push(n.label);
    }
    for (const labels of Object.values(byContributor)) {
      for (let i = 0; i < labels.length - 1; i++) {
        const key = `${labels[i]}->${labels[i + 1]}`;
        if (!edgeSet.has(key)) {
          edgeSet.add(key);
          cyEdges.push({ data: { source: labels[i], target: labels[i + 1], relation: '' } });
        }
      }
    }

    return [...cyNodes, ...cyEdges];
  }, [nodes, realEdges]);

  // Track the element count from the last layout run so we can decide whether
  // a delta warrants a fresh layout (>10% change threshold).
  const lastLayoutCountRef = useRef<number>(0);

  // Initialize Cytoscape
  useEffect(() => {
    if (!containerRef.current) return;

    const initialElements = buildElements();
    const cy = cytoscape({
      container: containerRef.current,
      elements: initialElements,
      style: CY_STYLE,
      layout: getCyLayout(layout),
      userZoomingEnabled: true,
      userPanningEnabled: true,
      boxSelectionEnabled: false,
    });

    lastLayoutCountRef.current = initialElements.length;
    cyRef.current = cy;
    requestAnimationFrame(() => {
      cy.resize();
      const visible = getVisibleElements(cy);
      if (visible.length > 0) cy.fit(visible, 56);
    });

    // Node click → select
    cy.on('tap', 'node', (evt) => {
      const nodeId = evt.target.id();
      const knowledgeNode = nodeMapRef.current.get(nodeId);
      if (knowledgeNode) {
        onSelectNode(selectedNode?.label === nodeId ? null : knowledgeNode);
      }
    });

    // Background click → deselect
    cy.on('tap', (evt) => {
      if (evt.target === cy) {
        onSelectNode(null);
      }
    });

    // Hover: highlight connected edges
    cy.on('mouseover', 'node', (evt) => {
      const node = evt.target;
      node.connectedEdges().addClass('highlighted');
      node.neighborhood('node').addClass('highlighted');
    });
    cy.on('mouseout', 'node', (evt) => {
      const node = evt.target;
      node.connectedEdges().removeClass('highlighted');
      node.neighborhood('node').removeClass('highlighted');
    });

    return () => {
      cy.destroy();
      cyRef.current = null;
    };
  // We intentionally only run this on mount/unmount. Element and layout updates
  // are handled by the subsequent effects.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Diff-based element update when nodes or edges change.
  // Avoids full remove/re-add; re-runs layout only when element count shifts >10%.
  useEffect(() => {
    const cy = cyRef.current;
    if (!cy) return;

    const newElements = buildElements();
    const newIds = new Set(newElements.map((el) => el.data.id as string));

    cy.batch(() => {
      // Remove elements that are no longer present
      cy.elements().forEach((el) => {
        if (!newIds.has(el.id())) {
          el.remove();
        }
      });

      // Add elements that don't exist yet
      const existingIds = new Set(cy.elements().map((el) => el.id()));
      const toAdd = newElements.filter((el) => !existingIds.has(el.data.id as string));
      if (toAdd.length > 0) {
        cy.add(toAdd);
      }
    });

    // Apply current hiddenKinds visibility to any newly added nodes
    cy.nodes().forEach((cyNode) => {
      const kind = cyNode.data('kind') as string;
      if (hiddenKinds.has(kind)) {
        (cyNode as any).hide();
      } else {
        (cyNode as any).show();
      }
    });

    // Re-run layout only when element count changes significantly (>10% delta)
    const currentCount = newElements.length;
    const lastCount = lastLayoutCountRef.current;
    const delta = lastCount === 0 ? 1 : Math.abs(currentCount - lastCount) / lastCount;
    if (delta > 0.1) {
      lastLayoutCountRef.current = currentCount;
      runLayout(layout);
    }
  }, [nodes, realEdges, buildElements, hiddenKinds, layout, runLayout]);

  // For hiddenKinds-only changes: show/hide nodes without touching layout.
  useEffect(() => {
    const cy = cyRef.current;
    if (!cy) return;

    cy.nodes().forEach((cyNode) => {
      const kind = cyNode.data('kind') as string;
      if (hiddenKinds.has(kind)) {
        (cyNode as any).hide();
      } else {
        (cyNode as any).show();
      }
    });
    fitVisible();
  }, [hiddenKinds, fitVisible]);

  // Re-run layout when layout mode changes
  useEffect(() => {
    const cy = cyRef.current;
    if (!cy || cy.elements().length === 0) return;
    lastLayoutCountRef.current = cy.elements().length;
    runLayout(layout);
  }, [layout, runLayout]);

  // Highlight the selected node and its edges
  useEffect(() => {
    const cy = cyRef.current;
    if (!cy) return;

    // Clear all previous selection highlights
    cy.nodes().unselect();
    cy.edges().removeClass('highlighted');
    cy.nodes().removeClass('highlighted');

    if (selectedNode) {
      const cyNode = cy.getElementById(selectedNode.label);
      if (cyNode.length > 0) {
        cyNode.select();
        cyNode.connectedEdges().addClass('highlighted');
        cyNode.neighborhood('node').addClass('highlighted');
      }
    }
  }, [selectedNode]);

  // Fit viewport helper
  const handleFit = useCallback(() => {
    fitVisible(72);
  }, [fitVisible]);

  // Kind counts for the toolbar
  const kindCounts: Record<string, number> = {};
  for (const n of nodes) {
    kindCounts[n.kind] = (kindCounts[n.kind] || 0) + 1;
  }
  const sortedKindCounts = Object.entries(kindCounts).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
  const activeKindCount = sortedKindCounts.filter(([kind]) => !hiddenKinds.has(kind)).length;
  const timelineMeta = useMemo(() => {
    const visible = nodes.filter((node) => !hiddenKinds.has(node.kind));
    const timestamps = visible.map((node) => Date.parse(String(node.created_at || ''))).filter(Number.isFinite);
    return {
      start: timestamps.length ? Math.min(...timestamps) : 0,
      end: timestamps.length ? Math.max(...timestamps) : 0,
      lanes: [...new Set(visible.map((node) => node.kind || 'unknown'))].sort().slice(0, 7),
    };
  }, [nodes, hiddenKinds]);

  return (
    <div className="knowledge-cytoscape relative flex h-full w-full flex-col" style={{ background: 'var(--warm)' }}>
      {nodes.length > MAX_GRAPH_NODES && (
        <div className="pointer-events-none absolute right-4 top-3 z-20 rounded-full px-3 py-1 text-xs" style={{ border: '0.5px solid var(--ink-hairline)', background: 'var(--warm)', color: 'var(--ink-mid)' }}>
          Showing {MAX_GRAPH_NODES} of {nodes.length} nodes
        </div>
      )}
      <div className="flex h-[58px] flex-shrink-0 items-center border-b border-border" style={{ background: 'var(--warm-2)' }}>
        <div className="flex min-w-0 items-center gap-2 px-4">
          <div className="flex flex-shrink-0 items-center gap-1 rounded-full p-1" style={{ border: '0.5px solid var(--ink-hairline)', background: 'var(--warm)' }}>
            {(Object.keys(LAYOUT_LABELS) as LayoutMode[]).map((mode) => (
              <button
                key={mode}
                onClick={() => setLayout(mode)}
                className="rounded-full px-2.5 py-1 text-[10px] transition-colors"
                style={{ background: layout === mode ? 'var(--ink)' : 'transparent', color: layout === mode ? 'var(--warm)' : 'var(--ink-mid)' }}
              >
                {LAYOUT_LABELS[mode]}
              </button>
            ))}
            <button
              onClick={handleFit}
              className="rounded-full px-2.5 py-1 text-[10px] transition-colors"
              style={{ color: 'var(--ink-mid)' }}
              title="Fit to viewport"
            >
              Fit
            </button>
          </div>

          <span className="w-px h-5 flex-shrink-0" style={{ background: 'var(--ink-hairline)' }} />

          <span className="flex-shrink-0 font-mono text-[10px]" style={{ color: 'var(--ink-faint)' }}>{nodes.length} nodes</span>
          <details className="knowledge-kind-menu relative flex-shrink-0">
            <summary className="flex cursor-pointer list-none items-center gap-1.5 rounded-full px-2.5 py-1 text-[11px]" style={{ border: '0.5px solid var(--ink-hairline)', background: 'var(--warm)', color: 'var(--ink)' }}>
              Kinds {activeKindCount}/{sortedKindCounts.length}
            </summary>
            <div className="knowledge-kind-menu-panel absolute left-0 top-8 z-30 w-[300px] rounded-xl p-2" style={{ border: '0.5px solid var(--ink-hairline)', background: 'var(--warm)', color: 'var(--ink)' }}>
              <div className="mb-2 flex items-center justify-between gap-2 px-1">
                <span className="font-mono text-[10px] uppercase tracking-[0.16em]" style={{ color: 'var(--teal)' }}>Filter kinds</span>
                <button type="button" onClick={() => setHiddenKinds(new Set())} className="rounded-full px-2 py-0.5 text-[10px]" style={{ border: '0.5px solid var(--ink-hairline)', color: 'var(--ink-mid)' }}>
                  Show all
                </button>
              </div>
              <div className="grid max-h-[280px] grid-cols-1 gap-1 overflow-auto">
                {sortedKindCounts.map(([kind, count]) => {
                  const hidden = hiddenKinds.has(kind);
                  const color = KIND_COLORS[kind] || KIND_COLORS.unknown;
                  return (
                    <button
                      key={kind}
                      type="button"
                      onClick={() => {
                        const next = new Set(hiddenKinds);
                        hidden ? next.delete(kind) : next.add(kind);
                        setHiddenKinds(next);
                      }}
                      className="flex items-center gap-2 rounded-lg px-2 py-1.5 text-left text-[11px] transition-colors"
                      style={{ background: hidden ? 'transparent' : 'var(--warm-2)', color: 'var(--ink)', opacity: hidden ? 0.45 : 1 }}
                    >
                      <span className="h-2 w-2 flex-shrink-0 rounded-full" style={{ backgroundColor: color }} />
                      <span className="min-w-0 flex-1 truncate">{kind}</span>
                      <span className="font-mono" style={{ color: 'var(--ink-faint)' }}>{count}</span>
                    </button>
                  );
                })}
              </div>
            </div>
          </details>
        </div>
      </div>
      <div className="pointer-events-none absolute left-5 top-20 z-10 max-w-[280px]">
        <div className="text-[10px] uppercase tracking-[0.18em]" style={{ color: 'var(--teal)' }}>Graph canvas</div>
        <div className="mt-1 font-mono text-lg" style={{ color: 'var(--ink)' }}>{selectedNode?.label || 'Select a node'}</div>
        <div className="mt-1 text-xs leading-5" style={{ color: 'var(--ink-mid)' }}>
          {selectedNode ? `${selectedNode.kind} · ${selectedNode.source_type || 'unknown source'}` : 'Drag to pan, scroll to zoom, and use the rail for details.'}
        </div>
      </div>
      {layout === 'timeline' && (
        <div className="pointer-events-none absolute bottom-4 left-5 right-5 z-10" style={{ color: 'var(--ink-faint)' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', fontFamily: 'var(--font-mono)', fontSize: 10, borderTop: '0.5px solid var(--ink-hairline)', paddingTop: 7 }}>
            <span>{formatTimelineDate(timelineMeta.start)}</span>
            <span className="eyebrow" style={{ color: 'var(--ink-faint)' }}>created_at timeline</span>
            <span>{formatTimelineDate(timelineMeta.end)}</span>
          </div>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginTop: 7 }}>
            {timelineMeta.lanes.map((kind) => (
              <span key={kind} style={{ display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 10 }}>
                <span style={{ width: 7, height: 7, borderRadius: 2, background: KIND_COLORS[kind] || KIND_COLORS.unknown }} />
                {kind}
              </span>
            ))}
          </div>
        </div>
      )}
      <div ref={containerRef} className="flex-1" style={{ background: 'var(--warm)' }} role="img" aria-label="Knowledge graph visualization - use the Browser view for keyboard navigation" />
    </div>
  );
}
