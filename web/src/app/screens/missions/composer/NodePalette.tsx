import { useState } from 'react';
import { Search } from 'lucide-react';
import { getAllNodes, getNodesByCategory } from './nodeRegistry';
import type { NodeCategory } from './canvasTypes';
import { CATEGORY_COLORS } from './canvasTypes';

const CATEGORY_ORDER: NodeCategory[] = ['trigger', 'agent', 'output', 'modifier', 'hub'];
const CATEGORY_LABELS: Record<NodeCategory, string> = {
  trigger: 'Triggers',
  agent: 'Agent',
  output: 'Outputs',
  modifier: 'Modifiers',
  hub: 'Hub Components',
};

export function NodePalette() {
  const [search, setSearch] = useState('');

  const onDragStart = (event: React.DragEvent, typeId: string) => {
    event.dataTransfer.setData('application/reactflow-type', typeId);
    event.dataTransfer.effectAllowed = 'move';
  };

  const allNodes = getAllNodes();
  const filtered = search
    ? allNodes.filter(n => n.label.toLowerCase().includes(search.toLowerCase()) || n.typeId.includes(search.toLowerCase()))
    : null;

  return (
    <div className="w-52 border-r border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-950 overflow-y-auto">
      <div className="p-2">
        <div className="relative">
          <Search size={14} className="absolute left-2 top-2.5 text-zinc-400" />
          <input
            type="text"
            value={search}
            onChange={e => setSearch(e.target.value)}
            placeholder="Search nodes..."
            className="w-full pl-7 pr-2 py-1.5 text-xs rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900"
          />
        </div>
      </div>

      {filtered ? (
        <div className="px-2 pb-2">
          {filtered.map(def => (
            <div
              key={def.typeId}
              draggable
              onDragStart={e => onDragStart(e, def.typeId)}
              className="flex items-center gap-2 px-2 py-1.5 text-xs rounded cursor-grab hover:bg-zinc-100 dark:hover:bg-zinc-800 mb-0.5"
            >
              <div className="w-2 h-2 rounded-full" style={{ backgroundColor: CATEGORY_COLORS[def.category] }} />
              {def.label}
            </div>
          ))}
          {filtered.length === 0 && <div className="text-xs text-zinc-400 px-2 py-4">No matching nodes</div>}
        </div>
      ) : (
        CATEGORY_ORDER.map(cat => {
          const nodes = getNodesByCategory(cat);
          if (nodes.length === 0) return null;
          return (
            <div key={cat} className="mb-2">
              <div className="px-3 py-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-400">
                {CATEGORY_LABELS[cat]}
              </div>
              {nodes.map(def => (
                <div
                  key={def.typeId}
                  draggable
                  onDragStart={e => onDragStart(e, def.typeId)}
                  className="flex items-center gap-2 px-3 py-1.5 text-xs rounded cursor-grab hover:bg-zinc-100 dark:hover:bg-zinc-800 mx-1 mb-0.5"
                >
                  <div className="w-2 h-2 rounded-full" style={{ backgroundColor: CATEGORY_COLORS[cat] }} />
                  {def.label}
                </div>
              ))}
            </div>
          );
        })
      )}
    </div>
  );
}
