import { useState, useRef, useEffect } from 'react';

interface InlineEditFieldProps {
  label: string;
  value: string;
  onSave: (value: string) => Promise<void> | void;
  multiline?: boolean;
  mono?: boolean;
}

export function InlineEditField({ label, value, onSave, multiline = false, mono = false }: InlineEditFieldProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const [saving, setSaving] = useState(false);
  const ref = useRef<HTMLInputElement | HTMLTextAreaElement>(null);

  useEffect(() => { setDraft(value); }, [value]);
  useEffect(() => { if (editing) ref.current?.focus(); }, [editing]);

  const handleSave = async () => {
    if (draft === value) { setEditing(false); return; }
    setSaving(true);
    try {
      await onSave(draft);
      setEditing(false);
    } catch {
      // error handled by caller via toast
    } finally {
      setSaving(false);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') { setDraft(value); setEditing(false); }
    if (e.key === 'Enter' && !multiline) handleSave();
    if (e.key === 'Enter' && e.metaKey && multiline) handleSave();
  };

  const inputClasses = [
    'w-full bg-background border border-border rounded px-3 py-2 text-sm text-foreground',
    'focus:outline-none focus:ring-1 focus:ring-primary focus:border-primary',
    mono ? 'font-mono' : '',
    saving ? 'opacity-50 pointer-events-none' : '',
  ].filter(Boolean).join(' ');

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between">
        <span className="text-[10px] uppercase tracking-wide text-muted-foreground">{label}</span>
        {!editing && (
          <span
            className="text-[10px] text-primary cursor-pointer hover:text-primary/80"
            onClick={() => setEditing(true)}
          >
            edit
          </span>
        )}
      </div>

      {editing ? (
        multiline ? (
          <textarea
            ref={ref as React.RefObject<HTMLTextAreaElement>}
            className={`${inputClasses} resize-y`}
            style={{ minHeight: 80 }}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={handleSave}
            onKeyDown={handleKeyDown}
            disabled={saving}
          />
        ) : (
          <input
            ref={ref as React.RefObject<HTMLInputElement>}
            className={inputClasses}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={handleSave}
            onKeyDown={handleKeyDown}
            disabled={saving}
          />
        )
      ) : (
        <div
          className={[
            'text-sm text-foreground',
            mono ? 'font-mono' : '',
            multiline ? 'max-h-24 overflow-hidden' : 'truncate',
          ].filter(Boolean).join(' ')}
        >
          {value || <span className="text-muted-foreground italic">—</span>}
        </div>
      )}
    </div>
  );
}
