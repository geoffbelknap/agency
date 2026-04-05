import { Handle, Position, type NodeProps } from '@xyflow/react';
import { Plug2, Wrench } from 'lucide-react';
import type { CanvasNode } from '../canvasTypes';
import { getNodeDef } from '../nodeRegistry';
import { CATEGORY_COLORS } from '../canvasTypes';

const ICONS: Record<string, React.ElementType> = { Plug2, Wrench };

export function HubNode({ data, selected }: NodeProps<CanvasNode>) {
  const def = getNodeDef(data.typeId);
  if (!def) return null;

  const Icon = ICONS[def.icon] || Plug2;
  const color = CATEGORY_COLORS.hub;
  const config = data.config || {};
  const summary = def.configSchema.map(f => config[f.key]).filter(Boolean).join(' · ') || 'Not configured';

  return (
    <div
      className={`rounded-lg shadow-md bg-white dark:bg-zinc-900 border-2 min-w-[180px] ${selected ? 'ring-2 ring-teal-400' : ''}`}
      style={{ borderColor: color }}
    >
      <div className="flex items-center gap-2 px-3 py-1.5 rounded-t-md text-white text-sm font-medium" style={{ backgroundColor: color }}>
        <Icon size={14} />
        {def.label}
      </div>
      <div className="px-3 py-2 text-xs text-zinc-500 dark:text-zinc-400 truncate max-w-[200px]">{summary}</div>
      {def.ports.outputs.map(port => (
        <Handle key={port.id} type="source" position={Position.Right} id={port.id}
          style={{ background: CATEGORY_COLORS[port.type as keyof typeof CATEGORY_COLORS] || color, width: 10, height: 10 }} />
      ))}
    </div>
  );
}
