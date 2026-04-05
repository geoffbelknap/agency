import { useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../components/ui/select';
import { Input } from '../../components/ui/input';
import { formatDateTimeShort } from '../../lib/time';
import { KnowledgeNode } from './types';
import { KindBadge, SourceBadge } from './badges';

export function NodeBrowser({
  nodes,
  selectedNode,
  onSelectNode,
}: {
  nodes: KnowledgeNode[];
  selectedNode: KnowledgeNode | null;
  onSelectNode: (n: KnowledgeNode | null) => void;
}) {
  const [kindFilter, setKindFilter] = useState('all');
  const [sourceFilter, setSourceFilter] = useState('all');
  const [searchText, setSearchText] = useState('');

  const sourceTypes = [...new Set(nodes.map((n) => n.source_type).filter(Boolean))] as string[];
  const kinds = [...new Set(nodes.map((n) => n.kind).filter(Boolean))] as string[];

  const filtered = nodes.filter((n) => {
    if (kindFilter !== 'all' && n.kind !== kindFilter) return false;
    if (sourceFilter !== 'all' && n.source_type !== sourceFilter) return false;
    if (searchText) {
      const q = searchText.toLowerCase();
      const matchLabel = n.label?.toLowerCase().includes(q);
      const matchSummary = n.summary?.toLowerCase().includes(q);
      if (!matchLabel && !matchSummary) return false;
    }
    return true;
  });

  // Group by kind
  const grouped: Record<string, KnowledgeNode[]> = {};
  for (const n of filtered) {
    const k = n.kind || 'unknown';
    if (!grouped[k]) grouped[k] = [];
    grouped[k].push(n);
  }

  return (
    <div className="space-y-4">
      {/* Filters */}
      <div className="flex items-center gap-3 flex-wrap">
        <Select value={kindFilter} onValueChange={setKindFilter}>
          <SelectTrigger className="w-36 h-8 text-xs">
            <SelectValue placeholder="Kind" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All kinds</SelectItem>
            {kinds.map((k) => (
              <SelectItem key={k} value={k}>{k}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={sourceFilter} onValueChange={setSourceFilter}>
          <SelectTrigger className="w-36 h-8 text-xs">
            <SelectValue placeholder="Source" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All sources</SelectItem>
            {sourceTypes.map((s) => (
              <SelectItem key={s} value={s}>{s}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          value={searchText}
          onChange={(e) => setSearchText(e.target.value)}
          placeholder="Filter by label or summary..."
          className="h-8 text-xs flex-1 min-w-48 bg-background border-border text-foreground placeholder:text-muted-foreground/70"
        />
        <span className="text-xs text-muted-foreground">{filtered.length} nodes</span>
      </div>

      {/* Grouped list */}
      {Object.entries(grouped).map(([kind, kindNodes]) => (
        <NodeGroup
          key={kind}
          kind={kind}
          nodes={kindNodes}
          selectedNode={selectedNode}
          onSelectNode={onSelectNode}
        />
      ))}
      {filtered.length === 0 && (
        <div className="text-sm text-muted-foreground text-center py-8">No nodes match the current filters</div>
      )}
    </div>
  );
}

function NodeGroup({
  kind,
  nodes,
  selectedNode,
  onSelectNode,
}: {
  kind: string;
  nodes: KnowledgeNode[];
  selectedNode: KnowledgeNode | null;
  onSelectNode: (n: KnowledgeNode | null) => void;
}) {
  const [expanded, setExpanded] = useState(true);

  return (
    <div>
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-2 mb-2 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
      >
        {expanded ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
        <KindBadge kind={kind} />
        <span>{nodes.length} node{nodes.length !== 1 ? 's' : ''}</span>
      </button>
      {expanded && (
        <div className="space-y-2 ml-5">
          {nodes.map((node) => (
            <NodeCard
              key={node.label}
              node={node}
              isSelected={selectedNode?.label === node.label}
              onSelect={() => onSelectNode(selectedNode?.label === node.label ? null : node)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function NodeCard({
  node,
  isSelected,
  onSelect,
}: {
  node: KnowledgeNode;
  isSelected: boolean;
  onSelect: () => void;
}) {
  return (
    <div
      onClick={onSelect}
      className={`bg-card border rounded p-3 cursor-pointer transition-colors ${
        isSelected ? 'border-primary/50 bg-primary/5' : 'border-border hover:border-border hover:bg-secondary/30'
      }`}
    >
      <div className="flex items-start justify-between gap-2 mb-1">
        <span className="text-sm font-medium text-foreground break-all">{node.label}</span>
        <div className="flex items-center gap-1.5 flex-shrink-0">
          <KindBadge kind={node.kind} />
          {node.source_type && <SourceBadge source={node.source_type} />}
        </div>
      </div>
      {node.summary && (
        <p className="text-xs text-muted-foreground line-clamp-2">{node.summary}</p>
      )}
      <div className="flex items-center gap-2 mt-1.5 text-[10px] text-muted-foreground/70">
        {node.contributed_by && (
          <span className="bg-secondary text-muted-foreground px-1.5 py-0.5 rounded">
            {String(node.contributed_by)}
          </span>
        )}
        {node.created_at && <span>{formatDateTimeShort(node.created_at)}</span>}
      </div>
    </div>
  );
}
