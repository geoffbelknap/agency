import { Handle, Position, type NodeProps } from '@xyflow/react';
import { Bot } from 'lucide-react';
import type { CanvasNode } from '../canvasTypes';
import { getNodeDef } from '../nodeRegistry';
import { CATEGORY_COLORS } from '../canvasTypes';

export function AgentNode({ data, selected }: NodeProps<CanvasNode>) {
  const def = getNodeDef(data.typeId);
  if (!def) return null;

  const color = CATEGORY_COLORS.agent;
  const config = data.config || {};
  const name = (config.name as string) || 'Unnamed Mission';
  const preset = (config.preset as string) || '';

  return (
    <div
      className={`rounded-lg shadow-md bg-white dark:bg-zinc-900 border-2 min-w-[220px] ${selected ? 'ring-2 ring-green-400' : ''}`}
      style={{ borderColor: color }}
    >
      {def.ports.inputs.map(port => (
        <Handle
          key={port.id}
          type="target"
          position={Position.Left}
          id={port.id}
          style={{
            background: CATEGORY_COLORS[port.type === 'modifier' ? 'modifier' : 'trigger'],
            width: 10,
            height: 10,
            top: port.type === 'modifier' ? '70%' : '30%',
          }}
        />
      ))}
      <div className="flex items-center gap-2 px-3 py-1.5 rounded-t-md text-white text-sm font-medium" style={{ backgroundColor: color }}>
        <Bot size={14} />
        Agent
      </div>
      <div className="px-3 py-2">
        <div className="text-sm font-medium dark:text-zinc-100">{name}</div>
        {preset && <div className="text-xs text-zinc-500 dark:text-zinc-400">Preset: {preset}</div>}
        {config.cost_mode ? <div className="text-xs text-zinc-500 dark:text-zinc-400">Cost: {String(config.cost_mode)}</div> : null}
      </div>
      {def.ports.outputs.map(port => (
        <Handle
          key={port.id}
          type="source"
          position={Position.Right}
          id={port.id}
          style={{ background: CATEGORY_COLORS.output, width: 10, height: 10 }}
        />
      ))}
    </div>
  );
}
