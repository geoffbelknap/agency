import type { CanvasNode } from './canvasTypes';
import { getNodeDef } from './nodeRegistry';
import { CATEGORY_COLORS } from './canvasTypes';

interface PropertyPanelProps {
  node: CanvasNode | null;
  onChange: (nodeId: string, config: Record<string, unknown>) => void;
}

export function PropertyPanel({ node, onChange }: PropertyPanelProps) {
  if (!node) {
    return (
      <div className="w-64 border-l border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-950 flex items-center justify-center">
        <p className="text-xs text-zinc-400">Select a node to edit</p>
      </div>
    );
  }

  const def = getNodeDef(node.data.typeId);
  if (!def) return null;

  const config = node.data.config || {};
  const color = CATEGORY_COLORS[def.category];

  const updateField = (key: string, value: unknown) => {
    onChange(node.id, { ...config, [key]: value });
  };

  return (
    <div className="w-64 border-l border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-950 overflow-y-auto">
      <div className="px-3 py-2 border-b border-zinc-200 dark:border-zinc-800">
        <div className="flex items-center gap-2">
          <div className="w-3 h-3 rounded-full" style={{ backgroundColor: color }} />
          <span className="text-sm font-medium dark:text-zinc-100">{def.label}</span>
        </div>
        <div className="text-[10px] text-zinc-400 mt-0.5">{def.typeId}</div>
      </div>

      <div className="p-3 space-y-3">
        {def.configSchema.map(field => (
          <div key={field.key}>
            <label className="block text-xs font-medium text-zinc-600 dark:text-zinc-300 mb-1">
              {field.label}
              {field.required && <span className="text-red-400 ml-0.5">*</span>}
            </label>

            {field.type === 'text' || field.type === 'cron' ? (
              <input
                type="text"
                value={(config[field.key] as string) || ''}
                onChange={e => updateField(field.key, e.target.value)}
                placeholder={field.placeholder}
                className="w-full px-2 py-1.5 text-xs rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900"
              />
            ) : field.type === 'textarea' ? (
              <textarea
                value={(config[field.key] as string) || ''}
                onChange={e => updateField(field.key, e.target.value)}
                placeholder={field.placeholder}
                rows={4}
                className="w-full px-2 py-1.5 text-xs rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900 resize-y"
              />
            ) : field.type === 'number' ? (
              <input
                type="number"
                value={(config[field.key] as number) ?? field.defaultValue ?? ''}
                onChange={e => updateField(field.key, e.target.value ? Number(e.target.value) : null)}
                placeholder={field.placeholder}
                className="w-full px-2 py-1.5 text-xs rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900"
              />
            ) : field.type === 'select' ? (
              <select
                value={String(config[field.key] ?? field.defaultValue ?? '')}
                onChange={e => updateField(field.key, e.target.value)}
                className="w-full px-2 py-1.5 text-xs rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900"
              >
                <option value="">Select...</option>
                {field.options?.map(opt => (
                  <option key={opt.value} value={opt.value}>{opt.label}</option>
                ))}
              </select>
            ) : field.type === 'checkbox' ? (
              <label className="flex items-center gap-2 text-xs">
                <input
                  type="checkbox"
                  checked={!!(config[field.key])}
                  onChange={e => updateField(field.key, e.target.checked)}
                  className="rounded border-zinc-300"
                />
                Enabled
              </label>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  );
}
