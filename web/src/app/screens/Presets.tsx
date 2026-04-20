import { useState, useEffect, useCallback, useMemo } from 'react';
import { Link } from 'react-router';
import { toast } from 'sonner';
import { Copy, Package, Pencil, Plus, Save, UserPlus } from 'lucide-react';
import { api, type RawAgent } from '../lib/api';
import { ConfirmDialog } from '../components/ConfirmDialog';

type BadgeTone = 'teal' | 'amber' | 'red' | 'neutral';

interface PresetSummary {
  name: string;
  description: string;
  type: string;
  source: string;
}

interface PresetDetail extends PresetSummary {
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
  capabilities: [],
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

function Badge({ children, tone = 'neutral' }: { children: React.ReactNode; tone?: BadgeTone }) {
  const tones = {
    teal: { bg: 'var(--teal-tint)', color: 'var(--teal-dark)', border: 'var(--teal-border)' },
    amber: { bg: 'var(--amber-tint)', color: 'var(--amber-foreground)', border: 'var(--amber)' },
    red: { bg: 'var(--red-tint)', color: 'var(--red)', border: 'var(--red)' },
    neutral: { bg: 'var(--warm-3)', color: 'var(--ink-mid)', border: 'var(--ink-hairline-strong)' },
  }[tone];
  return (
    <span className="mono" style={{ display: 'inline-flex', alignItems: 'center', gap: 4, padding: '2px 8px', fontSize: 10, letterSpacing: '0.08em', textTransform: 'uppercase', background: tones.bg, color: tones.color, border: `0.5px solid ${tones.border}`, borderRadius: 4, whiteSpace: 'nowrap' }}>
      {children}
    </span>
  );
}

function actionStyle(variant: 'default' | 'primary' | 'ghost' | 'danger' = 'default', disabled = false): React.CSSProperties {
  const variants = {
    default: { bg: 'var(--warm)', color: 'var(--ink)', border: '0.5px solid var(--ink-hairline-strong)' },
    primary: { bg: 'var(--ink)', color: 'var(--warm)', border: '0.5px solid var(--ink)' },
    ghost: { bg: 'transparent', color: 'var(--ink-mid)', border: '0.5px solid transparent' },
    danger: { bg: 'transparent', color: 'var(--red)', border: '0.5px solid transparent' },
  }[variant];
  return { display: 'inline-flex', alignItems: 'center', gap: 6, padding: '5px 10px', fontSize: 12, fontWeight: 400, fontFamily: 'var(--sans)', cursor: disabled ? 'default' : 'pointer', background: variants.bg, color: variants.color, border: variants.border, borderRadius: 999, opacity: disabled ? 0.5 : 1, whiteSpace: 'nowrap', textDecoration: 'none' };
}

function DesignButton({ children, icon, variant = 'default', disabled = false, onClick }: { children: React.ReactNode; icon?: React.ReactNode; variant?: 'default' | 'primary' | 'ghost' | 'danger'; disabled?: boolean; onClick?: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      style={actionStyle(variant, disabled)}
    >
      {icon}
      {children}
    </button>
  );
}

function DesignLink({ children, icon, to, variant = 'default' }: { children: React.ReactNode; icon?: React.ReactNode; to: string; variant?: 'default' | 'primary' | 'ghost' | 'danger' }) {
  return (
    <Link to={to} style={actionStyle(variant)}>
      {icon}
      {children}
    </Link>
  );
}

function MetaStat({ label, value, tone }: { label: string; value: string | number; tone?: string }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      <span className="eyebrow" style={{ fontSize: 9 }}>{label}</span>
      <span className="mono" style={{ fontSize: 14, color: tone || 'var(--ink)' }}>{value}</span>
    </div>
  );
}

function Card({ children }: { children: React.ReactNode }) {
  return (
    <div style={{ background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderLeft: '2px solid transparent', borderRadius: 10, padding: 18, display: 'flex', flexDirection: 'column', gap: 12, minHeight: 210 }}>
      {children}
    </div>
  );
}

function presetCaps(preset?: PresetDetail | PresetSummary | null) {
  if (!preset) return [];
  const detail = preset as PresetDetail;
  return [...(detail.capabilities || []), ...(detail.tools || [])].filter(Boolean).slice(0, 6);
}

function normalizeDetail(summary: PresetSummary, detail?: Record<string, unknown>): PresetDetail {
  return {
    ...summary,
    ...(detail || {}),
    name: String(detail?.name || summary.name),
    description: String(detail?.description || summary.description || ''),
    type: String(detail?.type || summary.type || 'standard'),
    source: String(detail?.source || summary.source || 'built-in'),
  } as PresetDetail;
}

export function Presets() {
  const [presets, setPresets] = useState<PresetSummary[]>([]);
  const [agents, setAgents] = useState<RawAgent[]>([]);
  const [details, setDetails] = useState<Record<string, PresetDetail>>({});
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<PresetDetail>(EMPTY_PRESET);
  const [saving, setSaving] = useState(false);
  const [isNew, setIsNew] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

  const loadPresets = useCallback(async () => {
    try {
      setLoading(true);
      const [presetData, agentData] = await Promise.all([
        api.presets.list(),
        api.agents.list().catch(() => [] as RawAgent[]),
      ]);
      setPresets(presetData ?? []);
      setAgents(agentData ?? []);
    } catch {
      setPresets([]);
      setAgents([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { loadPresets(); }, [loadPresets]);

  const agentCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const agent of agents) {
      if (!agent.preset) continue;
      counts[agent.preset] = (counts[agent.preset] || 0) + 1;
    }
    return counts;
  }, [agents]);

  const userPresets = presets.filter((preset) => preset.source === 'user');
  const builtinPresets = presets.filter((preset) => preset.source !== 'user');
  const assignedCount = Object.values(agentCounts).reduce((sum, count) => sum + count, 0);

  const getPresetDetail = async (preset: PresetSummary | PresetDetail) => {
    if (details[preset.name]) return details[preset.name];
    try {
      const detail = await api.presets.show(preset.name);
      const normalized = normalizeDetail(preset, detail);
      setDetails((prev) => ({ ...prev, [preset.name]: normalized }));
      return normalized;
    } catch {
      const normalized = normalizeDetail(preset);
      setDetails((prev) => ({ ...prev, [preset.name]: normalized }));
      return normalized;
    }
  };

  const startNew = () => {
    setDraft({ ...EMPTY_PRESET, hard_limits: [...(EMPTY_PRESET.hard_limits || [])], escalation: { ...EMPTY_PRESET.escalation }, identity: { ...EMPTY_PRESET.identity } });
    setEditing(true);
    setIsNew(true);
  };

  const duplicatePreset = async (preset: PresetSummary | PresetDetail) => {
    const source = await getPresetDetail(preset);
    setDraft({
      ...EMPTY_PRESET,
      ...source,
      name: `${source.name}-copy`,
      source: 'user',
      hard_limits: [...(source.hard_limits || EMPTY_PRESET.hard_limits || [])],
      escalation: { ...(source.escalation || EMPTY_PRESET.escalation) },
      identity: { ...(source.identity || { purpose: source.description, body: '' }) },
    });
    setEditing(true);
    setIsNew(true);
  };

  const editPreset = async (preset: PresetSummary | PresetDetail) => {
    const source = await getPresetDetail(preset);
    if (source.source === 'user') {
      setDraft({
        ...EMPTY_PRESET,
        ...source,
        hard_limits: [...(source.hard_limits || [])],
        escalation: { ...(source.escalation || EMPTY_PRESET.escalation) },
        identity: { ...(source.identity || { purpose: source.description, body: '' }) },
      });
      setIsNew(false);
    } else {
      setDraft({
        ...EMPTY_PRESET,
        ...source,
        name: `${source.name}-custom`,
        source: 'user',
        hard_limits: [...(source.hard_limits || EMPTY_PRESET.hard_limits || [])],
        escalation: { ...(source.escalation || EMPTY_PRESET.escalation) },
        identity: { ...(source.identity || { purpose: source.description, body: '' }) },
      });
      setIsNew(true);
    }
    setEditing(true);
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
      const saved = normalizeDetail(draft, detail);
      setDetails((prev) => ({ ...prev, [saved.name]: saved }));
      setDraft(saved);
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
      setDeleteTarget(null);
      setDetails((prev) => {
        const next = { ...prev };
        delete next[deleteTarget];
        return next;
      });
      await loadPresets();
    } catch (e: any) {
      toast.error(e.message || 'Failed to delete');
      setDeleteTarget(null);
    }
  };

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

  const renderPresetCard = (preset: PresetSummary) => {
    const detail = details[preset.name] || preset;
    const caps = presetCaps(detail);
    const count = agentCounts[preset.name] || 0;
    return (
      <Card key={preset.name}>
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Package size={16} style={{ color: 'var(--ink-mid)' }} />
            <span className="mono" style={{ fontSize: 14, color: 'var(--ink)' }}>{preset.name}</span>
            <span className="mono" style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--ink-faint)' }}>
              {count} agent{count === 1 ? '' : 's'}
            </span>
          </div>
          <p style={{ margin: '7px 0 0', fontSize: 12, color: 'var(--ink-mid)', lineHeight: 1.5 }}>{preset.description || 'No description provided.'}</p>
        </div>
        <div>
          <div className="eyebrow" style={{ fontSize: 9, marginBottom: 6 }}>Access shape</div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
            <Badge tone={preset.source === 'user' ? 'teal' : 'neutral'}>{preset.source === 'user' ? 'custom' : 'built-in'}</Badge>
            <Badge tone="neutral">{preset.type || 'standard'}</Badge>
            {caps.length === 0 && <Badge tone="neutral">declared on open</Badge>}
            {caps.map((cap) => <Badge key={cap} tone="neutral">{cap}</Badge>)}
          </div>
        </div>
        <div style={{ display: 'flex', gap: 6, marginTop: 'auto', paddingTop: 8, borderTop: '0.5px solid var(--ink-hairline)' }}>
          <DesignButton variant="ghost" icon={<Pencil size={12} />} onClick={() => editPreset(details[preset.name] || preset)}>{preset.source === 'user' ? 'Edit' : 'Edit copy'}</DesignButton>
          <DesignButton variant="ghost" icon={<Copy size={12} />} onClick={() => duplicatePreset(details[preset.name] || preset)}>Duplicate</DesignButton>
          <div style={{ flex: 1 }} />
          <DesignLink to="/agents" variant="ghost" icon={<UserPlus size={12} />}>Assign</DesignLink>
        </div>
      </Card>
    );
  };

  return (
    <>
      <div style={{ marginBottom: 20 }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
          <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
            <MetaStat label="Total" value={presets.length} />
            <MetaStat label="Built-in" value={builtinPresets.length} />
            <MetaStat label="Custom" value={userPresets.length} tone="var(--teal-dark)" />
            <MetaStat label="Assigned" value={assignedCount} tone="var(--teal-dark)" />
          </div>
          <DesignButton variant="primary" icon={<Plus size={13} />} onClick={startNew}>New preset</DesignButton>
        </div>
      </div>

      {loading ? (
        <div style={{ padding: 32, border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm-2)', color: 'var(--ink-mid)', fontSize: 13, textAlign: 'center' }}>Loading presets...</div>
      ) : editing ? (
        <section style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', padding: 20 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center', marginBottom: 16 }}>
            <div>
              <div className="display" style={{ fontSize: 22, color: 'var(--ink)' }}>{isNew ? 'New preset' : `Edit ${draft.name}`}</div>
              <div style={{ fontSize: 12, color: 'var(--ink-mid)', marginTop: 4 }}>Presets stay external to agents; runtime enforcement still depends on explicit grants and policy.</div>
            </div>
            <div style={{ display: 'flex', gap: 8 }}>
              <DesignButton variant="primary" icon={<Save size={13} />} disabled={saving} onClick={handleSave}>{saving ? 'Saving...' : 'Save'}</DesignButton>
              <DesignButton onClick={() => setEditing(false)}>Cancel</DesignButton>
            </div>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))', gap: 12 }}>
            <label style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
              <span className="eyebrow" style={{ fontSize: 9 }}>Name</span>
              <input id="preset-name" name="preset-name" value={draft.name} disabled={!isNew} onChange={(e) => updateDraftField('name', e.target.value)} style={{ height: 38, border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm)', color: 'var(--ink)', padding: '0 12px', opacity: !isNew ? 0.55 : 1 }} />
            </label>
            <label style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
              <span className="eyebrow" style={{ fontSize: 9 }}>Type</span>
              <select id="preset-type" name="preset-type" value={draft.type} onChange={(e) => updateDraftField('type', e.target.value)} style={{ height: 38, border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm)', color: 'var(--ink)', padding: '0 12px' }}>
                <option value="standard">standard</option>
                <option value="function">function</option>
                <option value="coordinator">coordinator</option>
              </select>
            </label>
          </div>

          <label style={{ display: 'flex', flexDirection: 'column', gap: 5, marginTop: 12 }}>
            <span className="eyebrow" style={{ fontSize: 9 }}>Description</span>
            <input id="preset-description" name="preset-description" value={draft.description} onChange={(e) => updateDraftField('description', e.target.value)} style={{ height: 38, border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm)', color: 'var(--ink)', padding: '0 12px' }} />
          </label>

          <label style={{ display: 'flex', flexDirection: 'column', gap: 5, marginTop: 12 }}>
            <span className="eyebrow" style={{ fontSize: 9 }}>Purpose</span>
            <input id="preset-purpose" name="preset-purpose" value={draft.identity?.purpose || ''} onChange={(e) => updateDraftField('identity.purpose', e.target.value)} placeholder="One-line purpose statement" style={{ height: 38, border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm)', color: 'var(--ink)', padding: '0 12px' }} />
          </label>

          <label style={{ display: 'flex', flexDirection: 'column', gap: 5, marginTop: 12 }}>
            <span className="eyebrow" style={{ fontSize: 9 }}>Identity prompt</span>
            <textarea id="preset-identity" name="preset-identity" value={draft.identity?.body || ''} onChange={(e) => updateDraftField('identity.body', e.target.value)} placeholder="Agent personality prompt..." style={{ minHeight: 140, border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm)', color: 'var(--ink)', padding: 12, fontFamily: 'var(--mono)', fontSize: 12, resize: 'vertical' }} />
          </label>

          <label style={{ display: 'flex', flexDirection: 'column', gap: 5, marginTop: 12 }}>
            <span className="eyebrow" style={{ fontSize: 9 }}>Tools</span>
            <input id="preset-tools" name="preset-tools" value={(draft.tools || []).join(', ')} onChange={(e) => updateDraftField('tools', e.target.value.split(',').map((item) => item.trim()).filter(Boolean))} placeholder="python3, git, curl" style={{ height: 38, border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm)', color: 'var(--ink)', padding: '0 12px' }} />
          </label>
        </section>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: 12 }}>
          {presets.map(renderPresetCard)}
        </div>
      )}

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => { if (!open) setDeleteTarget(null); }}
        title="Delete Preset"
        description={`Are you sure you want to delete "${deleteTarget}"? This cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={handleDelete}
      />
    </>
  );
}
