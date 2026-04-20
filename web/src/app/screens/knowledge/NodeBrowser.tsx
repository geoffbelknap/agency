import { useState } from 'react';
import { ChevronDown, ChevronRight, Search } from 'lucide-react';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../components/ui/select';
import { Input } from '../../components/ui/input';
import { formatDateTimeShort } from '../../lib/time';
import { cn } from '../../components/ui/utils';
import { KnowledgeNode } from './types';
import { KIND_COLORS } from './constants';
import { SourceBadge } from './badges';

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
    <div className="space-y-5">
      <div className="flex flex-col gap-3 border-b border-border pb-4 xl:flex-row xl:items-center">
        <div className="grid gap-2 sm:grid-cols-2">
          <Select value={kindFilter} onValueChange={setKindFilter}>
            <SelectTrigger className="h-9 w-full border-border bg-secondary text-xs sm:w-40">
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
            <SelectTrigger className="h-9 w-full border-border bg-secondary text-xs sm:w-40">
              <SelectValue placeholder="Source" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All sources</SelectItem>
              {sourceTypes.map((s) => (
                <SelectItem key={s} value={s}>{s}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <label className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={searchText}
            onChange={(e) => setSearchText(e.target.value)}
            placeholder="Filter by label or summary..."
            className="h-9 border-border bg-secondary pl-9 text-xs text-foreground placeholder:text-muted-foreground/70"
          />
        </label>
        <span className="font-mono text-xs text-muted-foreground">{filtered.length.toLocaleString()} nodes</span>
      </div>

      <div className="space-y-5">
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
          <div className="border border-dashed border-border bg-secondary py-12 text-center text-sm text-muted-foreground">
            No nodes match the current filters
          </div>
        )}
      </div>
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
  const color = KIND_COLORS[kind] || KIND_COLORS.unknown;

  return (
    <section>
      <button
        onClick={() => setExpanded(!expanded)}
        className="mb-2 flex w-full items-center gap-2 text-left text-xs text-muted-foreground transition-colors hover:text-foreground"
      >
        {expanded ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
        <span className="h-2 w-2 rounded-[2px]" style={{ backgroundColor: color }} />
        <span className="uppercase tracking-[0.16em]">{kind}</span>
        <span className="font-mono text-muted-foreground/70">{nodes.length} node{nodes.length !== 1 ? 's' : ''}</span>
      </button>
      {expanded && (
        <div className="border-y border-border">
          {nodes.map((node, index) => (
            <NodeRow
              key={node.label}
              node={node}
              index={index}
              isSelected={selectedNode?.label === node.label}
              onSelect={() => onSelectNode(selectedNode?.label === node.label ? null : node)}
            />
          ))}
        </div>
      )}
    </section>
  );
}

function NodeRow({
  node,
  index,
  isSelected,
  onSelect,
}: {
  node: KnowledgeNode;
  index: number;
  isSelected: boolean;
  onSelect: () => void;
}) {
  const color = KIND_COLORS[node.kind] || KIND_COLORS.unknown;
  return (
    <button
      onClick={onSelect}
      className={cn(
        'grid w-full grid-cols-[2.5rem_minmax(0,1fr)] gap-3 border-b border-border px-0 py-3 text-left transition-colors last:border-b-0 md:grid-cols-[2.5rem_minmax(0,1fr)_auto]',
        isSelected ? 'bg-primary/5' : 'hover:bg-secondary/70',
      )}
    >
      <div className="flex items-start gap-2 pt-0.5">
        <span className="font-mono text-[11px] text-muted-foreground/60">{String(index + 1).padStart(2, '0')}</span>
        <span className="mt-1 h-2 w-2 rounded-full" style={{ backgroundColor: color }} />
      </div>
      <div className="min-w-0">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <span className="break-all font-mono text-sm text-foreground">{node.label}</span>
          {node.source_type && <SourceBadge source={node.source_type} />}
        </div>
        {node.summary && (
          <p className="mt-1 line-clamp-2 text-xs leading-5 text-muted-foreground">{node.summary}</p>
        )}
        <div className="mt-2 flex flex-wrap items-center gap-2 text-[10px] text-muted-foreground/70">
          <span className="rounded-full border border-border bg-secondary px-2 py-0.5">{node.kind}</span>
          {node.contributed_by && <span>{String(node.contributed_by)}</span>}
          {node.created_at && <span>{formatDateTimeShort(node.created_at)}</span>}
        </div>
      </div>
      <div className="hidden items-center pr-3 text-[10px] uppercase tracking-[0.14em] text-muted-foreground md:flex">
        {isSelected ? 'Open' : 'Inspect'}
      </div>
    </button>
  );
}
