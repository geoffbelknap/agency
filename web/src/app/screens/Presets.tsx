import { useState, useEffect, useCallback } from 'react';
import { Link } from 'react-router';
import { toast } from 'sonner';
import { api } from '../lib/api';
import { Button } from '../components/ui/button';
import { AlertTriangle, Copy, FileText, Plus, Save, Trash2, X } from 'lucide-react';
import { ConfirmDialog } from '../components/ConfirmDialog';

interface PresetSummary {
  name: string;
  description: string;
  type: string;
  source: string;
}

interface PresetDetail {
  name: string;
  description: string;
  type: string;
  source: string;
  tools?: string[];
  capabilities?: string[];
  model_tier?: string;
  hard_limits?: { rule: string; reason: string }[];
  escalation?: {
    always_escalate?: string[];
    flag_before_proceeding?: string[];
  };
  identity?: {
    purpose?: string;
    body?: string;
  };
}

const EMPTY_PRESET: PresetDetail = {
  name: '',
  description: '',
  type: 'standard',
  source: 'user',
  tools: ['python3'],
  hard_limits: [
    { rule: 'never expose credentials, tokens, or secrets in any output', reason: 'credential exposure is a critical security risk' },
    { rule: 'never send data to external services without explicit approval', reason: 'data exfiltration risk' },
    { rule: 'never delete files without explicit confirmation', reason: 'deletions are irreversible' },
  ],
  escalation: {
    always_escalate: ['authentication and authorization changes'],
    flag_before_proceeding: ['irreversible actions', 'unexpected findings during autonomous operation'],
  },
  identity: { purpose: '', body: '' },
};

export function Presets() {
  const [presets, setPresets] = useState<PresetSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<PresetDetail | null>(null);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<PresetDetail>(EMPTY_PRESET);
  const [saving, setSaving] = useState(false);
  const [isNew, setIsNew] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

  const loadPresets = useCallback(async () => {
    try {
      setLoading(true);
      const data = await api.presets.list();
      setPresets(data ?? []);
    } catch {
      setPresets([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { loadPresets(); }, [loadPresets]);

  const selectPreset = async (p: PresetSummary) => {
    if (p.source === 'user') {
      try {
        const detail = await api.presets.show(p.name);
        setSelected(detail as unknown as PresetDetail);
        setDraft(detail as unknown as PresetDetail);
        setEditing(false);
        setIsNew(false);
      } catch {
        // Fall back to summary
        setSelected({ ...p, tools: [], hard_limits: [], identity: { purpose: p.description, body: '' } });
        setDraft({ ...p, tools: [], hard_limits: [], identity: { purpose: p.description, body: '' } });
        setEditing(false);
        setIsNew(false);
      }
    } else {
      // Built-in — show summary only (detail endpoint not available)
      setSelected({ ...p, tools: [], hard_limits: [], identity: { purpose: p.description, body: '' } });
      setEditing(false);
      setIsNew(false);
    }
  };

  const startNew = () => {
    setSelected(null);
    setDraft({ ...EMPTY_PRESET });
    setEditing(true);
    setIsNew(true);
  };

  const duplicatePreset = (p: PresetSummary) => {
    const base = selected || { ...p, tools: [], hard_limits: [...EMPTY_PRESET.hard_limits!], escalation: { ...EMPTY_PRESET.escalation }, identity: { purpose: p.description, body: '' } };
    setDraft({ ...base, name: `${p.name}-copy`, source: 'user' } as PresetDetail);
    setEditing(true);
    setIsNew(true);
    setSelected(null);
  };

  const handleSave = async () => {
    if (!draft.name.trim()) { toast.error('Name is required'); return; }
    setSaving(true);
    try {
      if (isNew) {
        await api.presets.create(draft as unknown as Record<string, unknown>);
        toast.success(`Preset "${draft.name}" created`);
      } else {
        await api.presets.update(draft.name, draft as unknown as Record<string, unknown>);
        toast.success(`Preset "${draft.name}" updated`);
      }
      await loadPresets();
      const detail = await api.presets.show(draft.name).catch(() => draft as unknown as Record<string, unknown>);
      setSelected(detail as unknown as PresetDetail);
      setDraft(detail as unknown as PresetDetail);
      setEditing(false);
      setIsNew(false);
    } catch (e: any) {
      toast.error(e.message || 'Failed to save preset');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await api.presets.delete(deleteTarget);
      toast.success(`Preset "${deleteTarget}" deleted`);
      if (selected?.name === deleteTarget) { setSelected(null); setEditing(false); }
      setDeleteTarget(null);
      await loadPresets();
    } catch (e: any) {
      toast.error(e.message || 'Failed to delete');
      setDeleteTarget(null);
    }
  };

  // Helpers for editing arrays
  const updateDraftField = (path: string, value: any) => {
    setDraft((prev) => {
      const next = { ...prev };
      const keys = path.split('.');
      let obj: any = next;
      for (let i = 0; i < keys.length - 1; i++) {
        if (!obj[keys[i]]) obj[keys[i]] = {};
        obj[keys[i]] = { ...obj[keys[i]] };
        obj = obj[keys[i]];
      }
      obj[keys[keys.length - 1]] = value;
      return next;
    });
  };

  const userPresets = presets.filter((p) => p.source === 'user');
  const builtinPresets = presets.filter((p) => p.source === 'built-in');

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">Agent preset templates</p>
        <Button size="sm" onClick={startNew}>
          <Plus className="w-4 h-4 mr-1" />
          New Preset
        </Button>
      </div>

      <div className="rounded-lg border border-border bg-card p-4">
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <div className="space-y-1">
            <div className="text-sm font-medium text-foreground">Use presets to standardize agent behavior before enabling tools</div>
            <p className="text-xs text-muted-foreground">
              Start from a built-in preset when the role already exists, then duplicate it only when your team needs a custom identity, limits, or escalation rules.
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button asChild variant="outline" size="sm" className="h-8 text-xs">
              <Link to="/admin/capabilities">Open Capabilities</Link>
            </Button>
            <Button asChild variant="outline" size="sm" className="h-8 text-xs">
              <Link to="/agents">Open Agents</Link>
            </Button>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* Preset list */}
        <div className="space-y-3">
          {userPresets.length > 0 && (
            <div>
              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1.5">Custom</div>
              <div className="space-y-1">
                {userPresets.map((p) => (
                  <button
                    key={p.name}
                    onClick={() => selectPreset(p)}
                    className={`w-full text-left px-3 py-2 rounded text-sm transition-colors ${
                      selected?.name === p.name ? 'bg-primary/10 text-primary' : 'bg-card border border-border hover:bg-secondary'
                    }`}
                  >
                    <div className="font-medium">{p.name}</div>
                    <div className="text-[10px] text-muted-foreground">{p.description} · {p.type}</div>
                  </button>
                ))}
              </div>
            </div>
          )}
          <div>
            <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1.5">Built-in</div>
            <div className="space-y-1">
              {builtinPresets.map((p) => (
                <button
                  key={p.name}
                  onClick={() => selectPreset(p)}
                  className={`w-full text-left px-3 py-2 rounded text-sm transition-colors ${
                    selected?.name === p.name ? 'bg-primary/10 text-primary' : 'bg-card border border-border hover:bg-secondary'
                  }`}
                >
                  <div className="flex items-center justify-between">
                    <span className="font-medium">{p.name}</span>
                    <span className="text-[9px] text-muted-foreground/60 uppercase">read-only</span>
                  </div>
                  <div className="text-[10px] text-muted-foreground">{p.description} · {p.type}</div>
                </button>
              ))}
            </div>
          </div>
        </div>

        {/* Detail / Editor panel */}
        <div className="lg:col-span-2">
          {!editing && !selected ? (
            <div className="bg-card border border-border rounded p-8 text-center">
              <div className="flex items-center justify-center gap-2 text-sm font-medium text-amber-300">
                <AlertTriangle className="h-4 w-4" />
                Select a preset to review before creating a custom one
              </div>
              <p className="mt-2 text-sm text-muted-foreground">
                Built-in presets are the fastest path for alpha users. Create a custom preset only when the built-ins no longer match the role you need.
              </p>
            </div>
          ) : editing ? (
            /* Editor form */
            <div className="bg-card border border-border rounded p-4 space-y-4">
              <div className="flex items-center justify-between">
                <h3 className="text-sm font-semibold text-foreground">{isNew ? 'New Preset' : `Edit: ${draft.name}`}</h3>
                <div className="flex gap-1">
                  <Button size="sm" onClick={handleSave} disabled={saving}>
                    <Save className="w-3 h-3 mr-1" />
                    {saving ? 'Saving...' : 'Save'}
                  </Button>
                  <Button variant="outline" size="sm" onClick={() => { setEditing(false); if (isNew) setSelected(null); }}>
                    Cancel
                  </Button>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="text-[10px] text-muted-foreground uppercase tracking-wide">Name</label>
                  <input
                    value={draft.name}
                    onChange={(e) => updateDraftField('name', e.target.value)}
                    disabled={!isNew}
                    className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-sm text-foreground disabled:opacity-50"
                  />
                </div>
                <div>
                  <label className="text-[10px] text-muted-foreground uppercase tracking-wide">Type</label>
                  <select
                    value={draft.type}
                    onChange={(e) => updateDraftField('type', e.target.value)}
                    className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-sm text-foreground"
                  >
                    <option value="standard">standard</option>
                    <option value="function">function</option>
                    <option value="coordinator">coordinator</option>
                  </select>
                </div>
              </div>

              <div>
                <label className="text-[10px] text-muted-foreground uppercase tracking-wide">Description</label>
                <input
                  value={draft.description}
                  onChange={(e) => updateDraftField('description', e.target.value)}
                  className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-sm text-foreground"
                />
              </div>

              <div>
                <label className="text-[10px] text-muted-foreground uppercase tracking-wide">Purpose</label>
                <input
                  value={draft.identity?.purpose || ''}
                  onChange={(e) => updateDraftField('identity.purpose', e.target.value)}
                  placeholder="One-line purpose statement"
                  className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-sm text-foreground placeholder:text-muted-foreground/50"
                />
              </div>

              <div>
                <label className="text-[10px] text-muted-foreground uppercase tracking-wide">Identity / Personality (Markdown)</label>
                <textarea
                  value={draft.identity?.body || ''}
                  onChange={(e) => updateDraftField('identity.body', e.target.value)}
                  placeholder="Agent personality prompt..."
                  className="w-full h-40 bg-background border border-border rounded px-2.5 py-1.5 text-xs font-mono text-foreground resize-y placeholder:text-muted-foreground/50"
                />
              </div>

              <div>
                <label className="text-[10px] text-muted-foreground uppercase tracking-wide">Tools (comma-separated)</label>
                <input
                  value={(draft.tools || []).join(', ')}
                  onChange={(e) => updateDraftField('tools', e.target.value.split(',').map((s) => s.trim()).filter(Boolean))}
                  placeholder="python3, git, curl"
                  className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-sm text-foreground placeholder:text-muted-foreground/50"
                />
              </div>

              <div>
                <div className="flex items-center justify-between mb-1">
                  <label className="text-[10px] text-muted-foreground uppercase tracking-wide">Hard Limits</label>
                  <button
                    onClick={() => updateDraftField('hard_limits', [...(draft.hard_limits || []), { rule: '', reason: '' }])}
                    className="text-[10px] text-primary hover:underline"
                  >
                    + Add
                  </button>
                </div>
                <div className="space-y-1.5">
                  {(draft.hard_limits || []).map((h, i) => (
                    <div key={i} className="flex gap-2 items-start">
                      <div className="flex-1 space-y-1">
                        <input
                          value={h.rule}
                          onChange={(e) => {
                            const updated = [...(draft.hard_limits || [])];
                            updated[i] = { ...updated[i], rule: e.target.value };
                            updateDraftField('hard_limits', updated);
                          }}
                          placeholder="Rule"
                          className="w-full bg-background border border-border rounded px-2 py-1 text-xs text-foreground"
                        />
                        <input
                          value={h.reason}
                          onChange={(e) => {
                            const updated = [...(draft.hard_limits || [])];
                            updated[i] = { ...updated[i], reason: e.target.value };
                            updateDraftField('hard_limits', updated);
                          }}
                          placeholder="Reason"
                          className="w-full bg-background border border-border rounded px-2 py-1 text-xs text-muted-foreground"
                        />
                      </div>
                      <button
                        onClick={() => updateDraftField('hard_limits', (draft.hard_limits || []).filter((_, j) => j !== i))}
                        className="text-muted-foreground hover:text-destructive p-1 mt-1"
                      >
                        <X className="w-3 h-3" />
                      </button>
                    </div>
                  ))}
                </div>
              </div>

              <div>
                <label className="text-[10px] text-muted-foreground uppercase tracking-wide">Always Escalate (one per line)</label>
                <textarea
                  value={(draft.escalation?.always_escalate || []).join('\n')}
                  onChange={(e) => updateDraftField('escalation.always_escalate', e.target.value.split('\n').filter(Boolean))}
                  rows={3}
                  className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-xs text-foreground resize-y"
                />
              </div>

              <div>
                <label className="text-[10px] text-muted-foreground uppercase tracking-wide">Flag Before Proceeding (one per line)</label>
                <textarea
                  value={(draft.escalation?.flag_before_proceeding || []).join('\n')}
                  onChange={(e) => updateDraftField('escalation.flag_before_proceeding', e.target.value.split('\n').filter(Boolean))}
                  rows={3}
                  className="w-full bg-background border border-border rounded px-2.5 py-1.5 text-xs text-foreground resize-y"
                />
              </div>
            </div>
          ) : selected ? (
            /* Read-only detail view */
            <div className="bg-card border border-border rounded p-4 space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="text-sm font-semibold text-foreground">{selected.name}</h3>
                  <div className="text-xs text-muted-foreground">{selected.description} · {selected.type} · {selected.source}</div>
                </div>
                <div className="flex gap-1">
                  {selected.source === 'user' && (
                    <>
                      <Button variant="outline" size="sm" onClick={() => { setDraft({ ...selected }); setEditing(true); setIsNew(false); }}>
                        Edit
                      </Button>
                      <Button variant="outline" size="sm" onClick={() => setDeleteTarget(selected.name)}>
                        <Trash2 className="w-3 h-3" />
                      </Button>
                    </>
                  )}
                  <Button variant="outline" size="sm" onClick={() => duplicatePreset(selected as PresetSummary)}>
                    <Copy className="w-3 h-3 mr-1" />
                    Duplicate
                  </Button>
                </div>
              </div>

              {selected.identity?.purpose && (
                <div>
                  <div className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Purpose</div>
                  <div className="text-xs text-foreground/80">{selected.identity.purpose}</div>
                </div>
              )}

              {selected.identity?.body && (
                <div>
                  <div className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Identity / Personality</div>
                  <div className="bg-background rounded p-3 text-xs font-mono text-foreground/80 whitespace-pre-wrap max-h-48 overflow-y-auto">
                    {selected.identity.body}
                  </div>
                </div>
              )}

              {selected.tools && selected.tools.length > 0 && (
                <div>
                  <div className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Tools</div>
                  <div className="flex flex-wrap gap-1">
                    {selected.tools.map((t) => (
                      <span key={t} className="text-xs bg-secondary text-foreground/80 px-2 py-0.5 rounded">{t}</span>
                    ))}
                  </div>
                </div>
              )}

              {selected.hard_limits && selected.hard_limits.length > 0 && (
                <div>
                  <div className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Hard Limits</div>
                  <div className="space-y-1">
                    {selected.hard_limits.map((h, i) => (
                      <div key={i} className="text-xs text-amber-700 dark:text-amber-400/80 bg-amber-50 dark:bg-amber-950/20 border border-amber-200 dark:border-amber-900/30 rounded px-2.5 py-1.5">
                        {h.rule}{h.reason && <span className="text-muted-foreground ml-1">— {h.reason}</span>}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {selected.escalation && (
                <div>
                  <div className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Escalation</div>
                  <div className="space-y-1">
                    {(selected.escalation.always_escalate || []).map((e, i) => (
                      <div key={`a-${i}`} className="text-xs text-red-700 dark:text-red-400/80 bg-red-50 dark:bg-red-950/20 border border-red-200 dark:border-red-900/30 rounded px-2.5 py-1.5">
                        Always escalate: {e}
                      </div>
                    ))}
                    {(selected.escalation.flag_before_proceeding || []).map((e, i) => (
                      <div key={`f-${i}`} className="text-xs text-blue-700 dark:text-blue-400/80 bg-blue-50 dark:bg-blue-950/20 border border-blue-200 dark:border-blue-900/30 rounded px-2.5 py-1.5">
                        Flag: {e}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {selected.source === 'built-in' && (
                <div className="text-xs text-muted-foreground/70 italic pt-2">
                  Built-in presets are read-only. Use Duplicate to create an editable copy.
                </div>
              )}
            </div>
          ) : null}
        </div>
      </div>

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => { if (!open) setDeleteTarget(null); }}
        title="Delete Preset"
        description={`Are you sure you want to delete "${deleteTarget}"? This cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={handleDelete}
      />
    </div>
  );
}
