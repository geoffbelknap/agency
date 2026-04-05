import { useState, useEffect } from 'react';
import { Search, X } from 'lucide-react';
import { Button } from '../../components/ui/button';
import { formatDateTimeShort } from '../../lib/time';
import { api } from '../../lib/api';
import { KnowledgeNode } from './types';
import { KIND_COLORS } from './constants';
import { KindBadge, SourceBadge } from './badges';

export function NodeDetail({
  node,
  allNodes,
  onClose,
  onNavigate,
}: {
  node: KnowledgeNode;
  allNodes: KnowledgeNode[];
  onClose: () => void;
  onNavigate: (n: KnowledgeNode) => void;
}) {
  const [searchAroundResults, setSearchAroundResults] = useState<KnowledgeNode[]>([]);
  const [searchAroundLoading, setSearchAroundLoading] = useState(false);
  const [showSearchAround, setShowSearchAround] = useState(false);
  const [searchAroundEdges, setSearchAroundEdges] = useState<Array<{ relation: string; label: string }>>([]);

  // Reset search around state when selected node changes
  useEffect(() => {
    setShowSearchAround(false);
    setSearchAroundResults([]);
    setSearchAroundEdges([]);
  }, [node.label]);

  // Find connected nodes (same contributor, or referenced in properties)
  const contributor = node.contributed_by ? String(node.contributed_by) : '';
  const connected = allNodes.filter(
    (n) => n.label !== node.label && contributor && String(n.contributed_by) === contributor,
  );

  const handleSearchAround = async () => {
    setShowSearchAround(true);
    setSearchAroundLoading(true);
    try {
      // Try graph neighbors first (follows actual edges)
      const data = await api.knowledge.neighbors((node as any).id || node.label);
      const neighbors = ((data as any).neighbors || []) as KnowledgeNode[];
      const edges = ((data as any).edges || []) as any[];
      if (neighbors.length > 0) {
        setSearchAroundResults(neighbors.filter((n: any) => n.label !== node.label));
        // Build edge labels for each neighbor
        setSearchAroundEdges(neighbors
          .filter((n: any) => n.label !== node.label)
          .map((n: any) => {
            const edge = edges.find((e: any) =>
              (e.source_id === (node as any).id && e.target_id === n.id) ||
              (e.target_id === (node as any).id && e.source_id === n.id)
            );
            return { relation: edge?.relation || '', label: n.label };
          }));
      } else {
        // Orphan node — fall back to text search
        const qData = await api.knowledge.query(node.label);
        const results = ((qData as any).results || []) as KnowledgeNode[];
        setSearchAroundResults(results.filter((r: any) => r.label !== node.label));
        setSearchAroundEdges([]);
      }
    } catch {
      setSearchAroundResults([]);
      setSearchAroundEdges([]);
    } finally {
      setSearchAroundLoading(false);
    }
  };

  return (
    <div className="space-y-0">
      {/* Entity header */}
      <div className="sticky top-0 bg-card z-10 pb-3 border-b border-border mb-3">
        <div className="flex items-start justify-between gap-2 mb-2">
          <div className="flex items-center gap-2">
            <div className="w-3 h-3 rounded-full flex-shrink-0" style={{ backgroundColor: KIND_COLORS[node.kind] || KIND_COLORS.unknown }} />
            <h3 className="text-sm font-semibold text-foreground break-all leading-tight">{node.label}</h3>
          </div>
          <button onClick={onClose} className="text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors flex-shrink-0 rounded-md w-7 h-7 flex items-center justify-center" title="Close">
            <X className="w-4 h-4" />
          </button>
        </div>
        <div className="flex items-center gap-1.5 flex-wrap">
          <KindBadge kind={node.kind} />
          {node.source_type && <SourceBadge source={node.source_type} />}
          {contributor && (
            <span className="text-[10px] bg-green-500/10 text-green-600 dark:text-green-400 px-1.5 py-0.5 rounded">
              {contributor}
            </span>
          )}
        </div>
      </div>

      {/* Summary */}
      {node.summary && (
        <div className="mb-3">
          <p className="text-xs text-foreground/80 leading-relaxed">{node.summary}</p>
        </div>
      )}

      {/* Metadata strip */}
      <div className="flex gap-3 text-[10px] text-muted-foreground mb-3">
        {node.created_at && <span>Created {formatDateTimeShort(node.created_at)}</span>}
        {node.updated_at && node.updated_at !== node.created_at && <span>Updated {formatDateTimeShort(node.updated_at)}</span>}
      </div>

      {/* Properties */}
      {node.properties && Object.keys(node.properties).length > 0 && (
        <div className="mb-3">
          <div className="text-[10px] text-muted-foreground/70 uppercase tracking-widest font-medium mb-1.5">Properties</div>
          <div className="bg-background rounded border border-border p-2">
            {Object.entries(node.properties).map(([key, value]) => (
              <div key={key} className="flex gap-2 py-1 border-b border-border/50 last:border-0">
                <span className="text-[10px] text-muted-foreground font-mono w-28 flex-shrink-0 truncate" title={key}>{key}</span>
                <span className="text-[10px] text-foreground/80 break-all">
                  {typeof value === 'string' ? value : JSON.stringify(value)}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Connected nodes */}
      {connected.length > 0 && (
        <div className="mb-3">
          <div className="text-[10px] text-muted-foreground/70 uppercase tracking-widest font-medium mb-1.5">
            Connected ({connected.length})
          </div>
          <div className="space-y-1">
            {connected.slice(0, 8).map((n) => (
              <button
                key={n.label}
                onClick={() => onNavigate(n)}
                className="w-full text-left bg-background rounded border border-border px-2.5 py-1.5 hover:border-primary/50 hover:bg-primary/5 transition-colors"
              >
                <div className="flex items-center gap-1.5">
                  <div className="w-2 h-2 rounded-full flex-shrink-0" style={{ backgroundColor: KIND_COLORS[n.kind] || KIND_COLORS.unknown }} />
                  <span className="text-[11px] text-foreground truncate">{n.label}</span>
                </div>
              </button>
            ))}
            {connected.length > 8 && (
              <div className="text-[10px] text-muted-foreground px-2.5">+{connected.length - 8} more</div>
            )}
          </div>
        </div>
      )}

      {/* Search Around */}
      <div className="border-t border-border pt-3">
        {!showSearchAround ? (
          <Button size="sm" variant="outline" className="w-full text-xs" onClick={handleSearchAround}>
            <Search className="w-3 h-3 mr-1.5" />
            Search Around
          </Button>
        ) : (
          <div>
            <div className="text-[10px] text-muted-foreground/70 uppercase tracking-widest font-medium mb-1.5">
              Search Around Results
            </div>
            {searchAroundLoading ? (
              <div className="text-xs text-muted-foreground py-2">Searching...</div>
            ) : searchAroundResults.length === 0 ? (
              <div className="text-xs text-muted-foreground py-2">No related nodes found</div>
            ) : (
              <div className="space-y-1">
                {searchAroundResults.map((r: any) => {
                  const edgeInfo = searchAroundEdges.find((e) => e.label === r.label);
                  return (
                    <button
                      key={r.label}
                      onClick={() => {
                        const match = allNodes.find((n) => n.label === r.label);
                        if (match) onNavigate(match);
                      }}
                      className="w-full text-left bg-background rounded border border-border px-2.5 py-1.5 hover:border-primary/50 hover:bg-primary/5 transition-colors"
                    >
                      <div className="flex items-center gap-1.5">
                        <div className="text-[11px] text-foreground truncate">{r.label}</div>
                        <span className="text-[9px] font-mono text-muted-foreground/70 bg-secondary px-1 py-0.5 rounded flex-shrink-0">{r.kind}</span>
                      </div>
                      {edgeInfo?.relation && (
                        <div className="text-[10px] text-muted-foreground/70 mt-0.5">{edgeInfo.relation}</div>
                      )}
                      {r.summary && <div className="text-[10px] text-muted-foreground truncate mt-0.5">{r.summary}</div>}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
