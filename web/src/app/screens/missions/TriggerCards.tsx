import { useState } from 'react';
import { Button } from '../../components/ui/button';
import type { WizardTrigger } from './serialize';

interface TriggerCardsProps {
  triggers: WizardTrigger[];
  onChange: (triggers: WizardTrigger[]) => void;
}

const TRIGGER_TYPES = [
  { source: 'channel' as const, icon: '\u{1F4AC}', label: 'Channel message', description: 'Trigger when a message arrives in a channel' },
  { source: 'schedule' as const, icon: '\u23F0', label: 'Schedule', description: 'Run on a recurring cron schedule' },
  { source: 'webhook' as const, icon: '\u{1F517}', label: 'Webhook', description: 'Trigger from an external webhook event' },
  { source: 'connector' as const, icon: '\u{1F50C}', label: 'Connector event', description: 'Trigger from an inbound connector' },
  { source: 'platform' as const, icon: '\u26A1', label: 'Platform event', description: 'Trigger on internal platform events' },
];

const ICON_BG: Record<WizardTrigger['source'], string> = {
  channel: 'bg-primary/10',
  schedule: 'bg-amber-500/10',
  webhook: 'bg-cyan-500/10',
  connector: 'bg-purple-500/10',
  platform: 'bg-emerald-500/10',
};

function triggerSummary(t: WizardTrigger): string {
  const parts: string[] = [];
  if (t.channel) parts.push(`channel: #${t.channel}`);
  if (t.connector) parts.push(`connector: ${t.connector}`);
  if (t.cron) parts.push(`cron: ${t.cron}`);
  if (t.name) parts.push(`name: ${t.name}`);
  if (t.event_type) parts.push(`event: ${t.event_type}`);
  if (t.match) parts.push(`match: ${t.match}`);
  return parts.join(' \u00B7 ');
}

function emptyDraft(source: WizardTrigger['source']): WizardTrigger {
  return { source };
}

const inputClass = 'bg-background border border-border text-foreground rounded px-3 py-1.5 text-sm w-full';

export default function TriggerCards({ triggers, onChange }: TriggerCardsProps) {
  const [expandedType, setExpandedType] = useState<WizardTrigger['source'] | null>(null);
  const [editingIndex, setEditingIndex] = useState<number | null>(null);
  const [draft, setDraft] = useState<WizardTrigger | null>(null);

  function openNew(source: WizardTrigger['source']) {
    setExpandedType(source);
    setEditingIndex(null);
    setDraft(emptyDraft(source));
  }

  function openEdit(index: number) {
    const t = triggers[index];
    setExpandedType(t.source);
    setEditingIndex(index);
    setDraft({ ...t });
  }

  function cancel() {
    setExpandedType(null);
    setEditingIndex(null);
    setDraft(null);
  }

  function addOrUpdate() {
    if (!draft) return;
    const next = [...triggers];
    if (editingIndex !== null) {
      next[editingIndex] = draft;
    } else {
      next.push(draft);
    }
    onChange(next);
    cancel();
  }

  function remove(index: number) {
    onChange(triggers.filter((_, i) => i !== index));
    if (editingIndex === index) cancel();
  }

  function updateDraft(field: keyof WizardTrigger, value: string) {
    if (!draft) return;
    setDraft({ ...draft, [field]: value });
  }

  function renderFormFields() {
    if (!draft) return null;
    switch (draft.source) {
      case 'channel':
        return (
          <>
            <label className="block space-y-1">
              <span className="text-xs text-muted-foreground">Channel name</span>
              <input className={inputClass} value={draft.channel ?? ''} onChange={e => updateDraft('channel', e.target.value)} placeholder="#general" />
            </label>
            <label className="block space-y-1">
              <span className="text-xs text-muted-foreground">Match pattern</span>
              <input className={inputClass} value={draft.match ?? ''} onChange={e => updateDraft('match', e.target.value)} placeholder="deploy*" />
            </label>
            <label className="block space-y-1">
              <span className="text-xs text-muted-foreground">Event type</span>
              <input className={inputClass} value={draft.event_type ?? ''} onChange={e => updateDraft('event_type', e.target.value)} placeholder="message" />
            </label>
          </>
        );
      case 'schedule':
        return (
          <>
            <label className="block space-y-1">
              <span className="text-xs text-muted-foreground">Cron expression</span>
              <input className={inputClass} value={draft.cron ?? ''} onChange={e => updateDraft('cron', e.target.value)} placeholder="0 9 * * 1-5" />
            </label>
            <label className="block space-y-1">
              <span className="text-xs text-muted-foreground">Name</span>
              <input className={inputClass} value={draft.name ?? ''} onChange={e => updateDraft('name', e.target.value)} placeholder="weekday-morning" />
            </label>
          </>
        );
      case 'webhook':
        return (
          <>
            <label className="block space-y-1">
              <span className="text-xs text-muted-foreground">Name</span>
              <input className={inputClass} value={draft.name ?? ''} onChange={e => updateDraft('name', e.target.value)} placeholder="github-push" />
            </label>
            <label className="block space-y-1">
              <span className="text-xs text-muted-foreground">Event type</span>
              <input className={inputClass} value={draft.event_type ?? ''} onChange={e => updateDraft('event_type', e.target.value)} placeholder="push" />
            </label>
          </>
        );
      case 'connector':
        return (
          <>
            <label className="block space-y-1">
              <span className="text-xs text-muted-foreground">Connector name</span>
              <input className={inputClass} value={draft.connector ?? ''} onChange={e => updateDraft('connector', e.target.value)} placeholder="slack" />
            </label>
            <label className="block space-y-1">
              <span className="text-xs text-muted-foreground">Event type</span>
              <input className={inputClass} value={draft.event_type ?? ''} onChange={e => updateDraft('event_type', e.target.value)} placeholder="message" />
            </label>
            <label className="block space-y-1">
              <span className="text-xs text-muted-foreground">Match pattern</span>
              <input className={inputClass} value={draft.match ?? ''} onChange={e => updateDraft('match', e.target.value)} placeholder="*" />
            </label>
          </>
        );
      case 'platform':
        return (
          <label className="block space-y-1">
            <span className="text-xs text-muted-foreground">Event type</span>
            <input className={inputClass} value={draft.event_type ?? ''} onChange={e => updateDraft('event_type', e.target.value)} placeholder="agent.started" />
          </label>
        );
      default:
        return null;
    }
  }

  const typeInfo = (source: WizardTrigger['source']) => TRIGGER_TYPES.find(t => t.source === source);

  return (
    <div className="space-y-4">
      {/* Active triggers */}
      {triggers.length > 0 && (
        <div className="space-y-2">
          <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Active Triggers</h4>
          {triggers.map((t, i) => {
            const info = typeInfo(t.source);
            return (
              <div key={i} className="border border-green-500/20 bg-green-500/5 rounded-lg p-3 flex items-center justify-between gap-2">
                <div className="flex items-center gap-2 min-w-0">
                  <span className={`w-7 h-7 rounded-md flex items-center justify-center text-sm shrink-0 ${ICON_BG[t.source]}`}>
                    {info?.icon}
                  </span>
                  <div className="min-w-0">
                    <span className="text-sm font-medium">{info?.label}</span>
                    {triggerSummary(t) && (
                      <span className="text-xs text-muted-foreground ml-2">{triggerSummary(t)}</span>
                    )}
                  </div>
                </div>
                <div className="flex items-center gap-1 shrink-0">
                  <Button variant="ghost" size="sm" onClick={() => openEdit(i)}>Edit</Button>
                  <Button variant="ghost" size="sm" className="text-red-400 hover:text-red-300" onClick={() => remove(i)}>Remove</Button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Add a trigger */}
      <div className="space-y-2">
        <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Add a trigger</h4>
        <div className="grid grid-cols-2 gap-2">
          {TRIGGER_TYPES.map(tt => (
            <div
              key={tt.source}
              className="border border-border rounded-lg p-3.5 cursor-pointer hover:border-primary/50 hover:bg-primary/5 transition-colors"
              onClick={() => openNew(tt.source)}
            >
              <div className="flex items-start gap-2.5">
                <span className={`w-7 h-7 rounded-md flex items-center justify-center text-sm shrink-0 ${ICON_BG[tt.source]}`}>
                  {tt.icon}
                </span>
                <div>
                  <div className="text-sm font-medium">{tt.label}</div>
                  <div className="text-xs text-muted-foreground mt-0.5">{tt.description}</div>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Inline config form */}
      {expandedType && draft && (
        <div className="bg-background border border-border rounded-lg p-4 space-y-3">
          <h4 className="text-sm font-medium flex items-center gap-2">
            <span className={`w-7 h-7 rounded-md flex items-center justify-center text-sm ${ICON_BG[expandedType]}`}>
              {typeInfo(expandedType)?.icon}
            </span>
            {editingIndex !== null ? 'Edit' : 'Configure'} {typeInfo(expandedType)?.label}
          </h4>
          {renderFormFields()}
          <div className="flex items-center gap-2 pt-1">
            <Button size="sm" onClick={addOrUpdate}>
              {editingIndex !== null ? 'Update' : 'Add Trigger'}
            </Button>
            <Button variant="ghost" size="sm" onClick={cancel}>Cancel</Button>
          </div>
        </div>
      )}
    </div>
  );
}
