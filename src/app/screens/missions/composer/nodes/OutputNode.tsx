import { Handle, Position, type NodeProps } from '@xyflow/react';
import { Hash, Send, AlertTriangle } from 'lucide-react';
import type { CanvasNode } from '../canvasTypes';
import { getNodeDef } from '../nodeRegistry';
import { CATEGORY_COLORS } from '../canvasTypes';

const ICONS: Record<string, React.ElementType> = { Hash, Send, AlertTriangle };

export function OutputNode({ data, selected }: NodeProps<CanvasNode>) {
  const def = getNodeDef(data.typeId);
  if (!def) return null;

  const Icon = ICONS[def.icon] || Hash;
  const color = CATEGORY_COLORS.output;
  const config = data.config || {};
  const summary = def.configSchema.map(f => config[f.key]).filter(Boolean).join(' · ') || 'Not configured';

  return (
    <div
      className={`rounded-lg shadow-md bg-white dark:bg-zinc-900 border-2 min-w-[180px] ${selected ? 'ring-2 ring-orange-400' : ''}`}
      style={{ borderColor: color }}
    >
      {def.ports.inputs.map(port => (
        <Handle key={port.id} type="target" position={Position.Left} id={port.id}
          style={{ background: color, width: 10, height: 10 }} />
      ))}
      <div className="flex items-center gap-2 px-3 py-1.5 rounded-t-md text-white text-sm font-medium" style={{ backgroundColor: color }}>
        <Icon size={14} />
        {def.label}
      </div>
      <div className="px-3 py-2 text-xs text-zinc-500 dark:text-zinc-400 truncate max-w-[200px]">{summary}</div>
    </div>
  );
}
