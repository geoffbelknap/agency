import { useState, useEffect, useRef, useCallback } from 'react';
import cytoscape from 'cytoscape';
import { KnowledgeNode } from './types';
import { KIND_COLORS, MAX_GRAPH_NODES } from './constants';

// ── Layout ──

type LayoutMode = 'radial' | 'force' | 'timeline' | 'grid';

const LAYOUT_LABELS: Record<LayoutMode, string> = {
  radial: 'Radial (clusters)',
  force: 'Force-directed',
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
      'color': '#ccc',
      'text-valign': 'bottom',
      'text-margin-y': 4,
      'width': 'data(size)',
      'height': 'data(size)',
      'border-width': 0,
    },
  },
  {
    selector: 'node:selected',
    style: {
      'border-width': 3,
      'border-color': '#38bdf8',
    },
  },
  {
    selector: 'edge',
    style: {
      'width': 1,
      'line-color': '#334155',
      'curve-style': 'bezier',
      'opacity': 0.4,
    },
  },
  {
    selector: 'edge.highlighted',
    style: {
      'line-color': '#38bdf8',
      'width': 2,
      'opacity': 1,
    },
  },
  {
    selector: 'node.highlighted',
    style: {
      'border-width': 2,
      'border-color': '#38bdf8',
    },
  },
];

// ── Layout config ──

function getCyLayout(mode: LayoutMode): cytoscape.LayoutOptions {
  switch (mode) {
    case 'radial':
      return { name: 'concentric', concentric: (n: any) => n.degree(), levelWidth: () => 2, spacingFactor: 1.5 } as any;
    case 'force':
      return { name: 'cose', animate: false, nodeRepulsion: () => 8000, idealEdgeLength: () => 100, gravity: 0.3 } as any;
    case 'grid':
      return { name: 'grid', spacingFactor: 1.2 } as any;
    case 'timeline':
      return { name: 'grid', sort: (a: any, b: any) => {
        const ta = a.data('created_at') || '';
        const tb = b.data('created_at') || '';
        return ta.localeCompare(tb);
      }} as any;
    default:
      return { name: 'cose', animate: false } as any;
  }
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

  // Build a map from label to node for fast lookup
  const nodeMapRef = useRef<Map<string, KnowledgeNode>>(new Map());
  useEffect(() => {
    const m = new Map<string, KnowledgeNode>();
    for (const n of nodes) m.set(n.label, n);
    nodeMapRef.current = m;
  }, [nodes]);

  // Count connections per node for sizing
  const connectionCounts = useRef<Map<string, number>>(new Map());
  useEffect(() => {
    const counts = new Map<string, number>();
    if (realEdges) {
      for (const e of realEdges) {
        counts.set(e.source, (counts.get(e.source) || 0) + 1);
        counts.set(e.target, (counts.get(e.target) || 0) + 1);
      }
    }
    connectionCounts.current = counts;
  }, [realEdges]);

  // Build Cytoscape elements from nodes and edges (ignoring hiddenKinds — visibility is
  // handled separately via .hide()/.show() to avoid layout thrash).
  const buildElements = useCallback(() => {
    const graphNodes = nodes.length > MAX_GRAPH_NODES ? nodes.slice(0, MAX_GRAPH_NODES) : nodes;
    const nodeLabels = new Set(graphNodes.map((n) => n.label));

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
        if (nodeLabels.has(e.source) && nodeLabels.has(e.target)) {
          const key = `${e.source}->${e.target}`;
          if (!edgeSet.has(key)) {
            edgeSet.add(key);
            cyEdges.push({ data: { source: e.source, target: e.target, relation: e.relation } });
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

    // Fit after layout finishes
    cy.on('layoutstop', () => {
      cy.fit(undefined, 50);
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
      cy.layout(getCyLayout(layout)).run();
    }
  }, [nodes, realEdges, buildElements, hiddenKinds, layout]);

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
  }, [hiddenKinds]);

  // Re-run layout when layout mode changes
  useEffect(() => {
    const cy = cyRef.current;
    if (!cy || cy.elements().length === 0) return;
    lastLayoutCountRef.current = cy.elements().length;
    cy.layout(getCyLayout(layout)).run();
  }, [layout]);

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
    cyRef.current?.fit(undefined, 50);
  }, []);

  // Kind counts for the toolbar
  const kindCounts: Record<string, number> = {};
  for (const n of nodes) {
    kindCounts[n.kind] = (kindCounts[n.kind] || 0) + 1;
  }

  return (
    <div className="w-full h-full relative flex flex-col">
      {nodes.length > MAX_GRAPH_NODES && (
        <div className="text-xs text-amber-400 px-4 py-1 absolute top-0 right-0 z-20 pointer-events-none">
          Showing {MAX_GRAPH_NODES} of {nodes.length} nodes
        </div>
      )}
      {/* Combined toolbar: layout buttons + kind filters */}
      <div className="overflow-x-auto border-b border-border flex-shrink-0">
        <div className="flex items-center gap-2 px-4 py-2 min-w-max">
          {/* Layout buttons */}
          <div className="flex items-center gap-1 bg-card border border-border rounded-md p-1 flex-shrink-0">
            {(Object.keys(LAYOUT_LABELS) as LayoutMode[]).map((mode) => (
              <button
                key={mode}
                onClick={() => setLayout(mode)}
                className={`px-2.5 py-1 text-[10px] font-medium rounded transition-colors ${
                  layout === mode
                    ? 'bg-primary text-primary-foreground'
                    : 'text-muted-foreground hover:text-foreground hover:bg-secondary'
                }`}
              >
                {LAYOUT_LABELS[mode]}
              </button>
            ))}
            <button
              onClick={handleFit}
              className="px-2.5 py-1 text-[10px] font-medium rounded transition-colors text-muted-foreground hover:text-foreground hover:bg-secondary"
              title="Fit to viewport"
            >
              Fit
            </button>
          </div>

          <span className="w-px h-5 bg-border flex-shrink-0" />

          {/* Kind filters */}
          <span className="text-[10px] text-muted-foreground/50 flex-shrink-0">{nodes.length} nodes</span>
          {Object.entries(kindCounts).map(([kind, count]) => {
            const hidden = hiddenKinds.has(kind);
            const color = KIND_COLORS[kind] || KIND_COLORS.unknown;
            return (
              <button
                key={kind}
                onClick={() => {
                  const next = new Set(hiddenKinds);
                  hidden ? next.delete(kind) : next.add(kind);
                  setHiddenKinds(next);
                }}
                className={`flex items-center gap-1.5 px-2 py-1 rounded text-[11px] transition-colors flex-shrink-0 ${
                  hidden ? 'opacity-40 bg-transparent border border-border' : 'bg-secondary'
                }`}
              >
                <span className="w-2 h-2 rounded-full flex-shrink-0" style={{ backgroundColor: color }} />
                {kind} ({count})
              </button>
            );
          })}
        </div>
      </div>
      {/* Cytoscape container */}
      <div ref={containerRef} className="flex-1 bg-background" role="img" aria-label="Knowledge graph visualization — use the Browser view for keyboard navigation" />
    </div>
  );
}
