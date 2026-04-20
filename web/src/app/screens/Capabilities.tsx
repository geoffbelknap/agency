import { useState, useEffect, useCallback, useMemo } from 'react';
import { toast } from 'sonner';
import { Search } from 'lucide-react';
import { api, type RawProviderToolCapability } from '../lib/api';
import { Agent, Capability, CapabilityState } from '../types';
import { Button } from '../components/ui/button';

type BadgeTone = 'teal' | 'amber' | 'red' | 'neutral';
type CapabilityView = 'all' | 'provider' | 'local';

const TABLE_WIDTHS = 'minmax(180px, 1.2fr) 120px 90px minmax(160px, 1.4fr) 80px 70px';

function isActive(state: string) {
  return state === 'enabled' || state === 'available' || state === 'restricted';
}

function riskFromCapability(capability: Capability) {
  if (typeof capability.spec?.risk === 'string') return capability.spec.risk;
  const name = capability.name.toLowerCase();
  if (name.includes('delete') || name.includes('shell') || name.includes('send') || name.includes('write') || name.includes('exec')) return 'high';
  if (name.includes('post') || name.includes('global') || name.includes('memory') || capability.kind === 'integration' || capability.kind === 'provider-tool') return 'medium';
  return 'low';
}

function scopeFromCapability(capability: Capability) {
  if (typeof capability.spec?.scope === 'string') return capability.spec.scope;
  if (capability.scopedAgents.length > 0) return capability.scopedAgents.join(', ');
  if (capability.kind === 'provider-tool') {
    const execution = typeof capability.spec?.execution === 'string' ? capability.spec.execution : 'provider hosted';
    return execution.replace(/_/g, ' ');
  }
  if (capability.state === 'disabled') return 'not granted';
  return 'platform-wide';
}

function packageFromCapability(capability: Capability) {
  if (capability.kind === 'provider-tool') return 'provider';
  if (capability.kind === 'mcp-server') return 'mcp-server';
  if (capability.kind === 'skill') return 'skill';
  if (capability.name.includes('.')) return capability.name.split('.')[0];
  if (capability.kind === 'tool') return 'local-tool';
  if (capability.kind === 'integration') return 'integration';
  return 'service';
}

function toneForRisk(risk: string): BadgeTone {
  if (risk === 'critical' || risk === 'high') return 'red';
  if (risk === 'medium') return 'amber';
  return 'teal';
}

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

function Btn({ children, icon, variant = 'default', disabled = false, onClick }: { children: React.ReactNode; icon?: React.ReactNode; variant?: 'default' | 'primary'; disabled?: boolean; onClick?: () => void }) {
  const variants = {
    default: { bg: 'var(--warm)', color: 'var(--ink)', border: '0.5px solid var(--ink-hairline-strong)' },
    primary: { bg: 'var(--ink)', color: 'var(--warm)', border: '0.5px solid var(--ink)' },
  }[variant];
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '5px 10px', fontSize: 12, fontWeight: 400, fontFamily: 'var(--sans)', cursor: disabled ? 'default' : 'pointer', background: variants.bg, color: variants.color, border: variants.border, borderRadius: 999, opacity: disabled ? 0.5 : 1, whiteSpace: 'nowrap' }}
    >
      {icon}
      {children}
    </button>
  );
}

function Card({ children, padded = false }: { children: React.ReactNode; padded?: boolean }) {
  return (
    <div style={{ background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 10, padding: padded ? 20 : 0, overflow: 'hidden' }}>
      {children}
    </div>
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

function TableHeader({ cols, widths }: { cols: React.ReactNode[]; widths: string }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: widths, gap: 16, padding: '10px 18px', background: 'var(--warm-2)' }}>
      {cols.map((col, index) => <div key={index} className="eyebrow" style={{ fontSize: 9 }}>{col}</div>)}
    </div>
  );
}

function TableRow({ cols, widths, accent }: { cols: React.ReactNode[]; widths: string; accent?: string }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: widths, gap: 16, padding: '12px 18px', alignItems: 'center', borderTop: '0.5px solid var(--ink-hairline)', borderLeft: accent ? `2px solid ${accent}` : '2px solid transparent' }}>
      {cols.map((col, index) => <div key={index} style={{ minWidth: 0 }}>{col}</div>)}
    </div>
  );
}

function Toggle({ on, onChange, label }: { on: boolean; onChange: () => void; label: string }) {
  return (
    <button
      type="button"
      aria-label={label}
      onClick={onChange}
      style={{ position: 'relative', width: 28, height: 16, borderRadius: 999, background: on ? 'var(--teal)' : 'var(--warm-3)', border: `0.5px solid ${on ? 'var(--teal)' : 'var(--ink-hairline-strong)'}`, cursor: 'pointer', padding: 0 }}
    >
      <span style={{ position: 'absolute', top: 1, left: on ? 13 : 2, width: 12, height: 12, borderRadius: '50%', background: 'var(--warm)', boxShadow: '0 1px 2px rgba(0,0,0,0.2)', transition: 'left 0.15s' }} />
    </button>
  );
}

function Segment({ active, children, onClick }: { active: boolean; children: React.ReactNode; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      style={{
        padding: '6px 12px',
        borderRadius: 999,
        border: '0',
        background: active ? 'var(--ink)' : 'transparent',
        color: active ? 'var(--warm)' : 'var(--ink-mid)',
        fontSize: 12,
        fontFamily: 'var(--sans)',
        cursor: 'pointer',
        whiteSpace: 'nowrap',
      }}
    >
      {children}
    </button>
  );
}

function capabilitySort(a: Capability, b: Capability) {
  const riskRank: Record<string, number> = { high: 0, medium: 1, low: 2 };
  const activeDiff = Number(isActive(b.state)) - Number(isActive(a.state));
  if (activeDiff !== 0) return activeDiff;
  const riskDiff = (riskRank[riskFromCapability(a)] ?? 9) - (riskRank[riskFromCapability(b)] ?? 9);
  if (riskDiff !== 0) return riskDiff;
  return a.name.localeCompare(b.name);
}

export function Capabilities() {
  const [capabilities, setCapabilities] = useState<Capability[]>([]);
  const [providerTools, setProviderTools] = useState<Record<string, RawProviderToolCapability>>({});
  const [localStates, setLocalStates] = useState<Record<string, { state: CapabilityState; agents: string[] }>>({});
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [view, setView] = useState<CapabilityView>('all');
  const [enableTarget, setEnableTarget] = useState<Capability | null>(null);
  const [enableKey, setEnableKey] = useState('');
  const [enableAgents, setEnableAgents] = useState<string[]>([]);
  const [enabling, setEnabling] = useState(false);

  const loadCapabilities = useCallback(async (showLoading = true) => {
    try {
      if (showLoading) setLoading(true);
      setError(null);
      const raw = await api.capabilities.list();
      api.providers.tools()
        .then((inventory) => setProviderTools(inventory.capabilities ?? {}))
        .catch(() => setProviderTools({}));
      const mapped = (raw ?? []).map((c: any) => ({
        id: c.name,
        name: c.name,
        kind: c.kind || 'service',
        state: c.state || 'disabled',
        scopedAgents: c.agents || [],
        description: c.description || '',
        spec: c.spec || {},
      }));
      setCapabilities(mapped);
      setLocalStates((prev) => {
        const next = { ...prev };
        for (const capability of mapped) {
          delete next[capability.name];
        }
        return next;
      });
    } catch (e: any) {
      setError(e.message || 'Failed to load capabilities');
    } finally {
      setLoading(false);
    }
  }, []);

  const loadAgents = useCallback(async () => {
    try {
      const raw = await api.agents.list();
      setAgents((raw ?? []).map((a: any) => ({
        id: a.name,
        name: a.name,
        status: a.status || 'stopped',
        mode: a.mode || '',
        type: a.type || '',
        preset: a.preset || '',
        team: a.team || '',
        enforcerState: a.enforcer || '',
      })));
    } catch {
      setAgents([]);
    }
  }, []);

  useEffect(() => {
    loadCapabilities();
  }, [loadCapabilities]);

  const mergedCapabilities = useMemo(() => {
    const byName = new Map<string, Capability>();
    for (const capability of capabilities) {
      byName.set(capability.name, capability);
    }
    for (const [name, tool] of Object.entries(providerTools)) {
      if (byName.has(name)) continue;
      const local = localStates[name];
      byName.set(name, {
        id: name,
        name,
        kind: 'provider-tool',
        state: local?.state ?? (tool.default_grant ? 'available' : 'disabled'),
        scopedAgents: local?.agents ?? [],
        description: tool.description || tool.title || '',
        spec: {
          risk: tool.risk,
          execution: tool.execution,
          default_grant: tool.default_grant,
          providers: tool.providers,
          title: tool.title,
        },
      });
    }
    return Array.from(byName.values());
  }, [capabilities, providerTools, localStates]);

  const rows = useMemo(() => mergedCapabilities
    .filter((capability) => {
      if (view === 'provider') return capability.kind === 'provider-tool';
      if (view === 'local') return capability.kind !== 'provider-tool';
      return true;
    })
    .sort(capabilitySort), [mergedCapabilities, view]);
  const stats = useMemo(() => {
    const enabled = mergedCapabilities.filter((capability) => isActive(capability.state)).length;
    const highRisk = mergedCapabilities.filter((capability) => riskFromCapability(capability) === 'high').length;
    return {
      total: mergedCapabilities.length,
      enabled,
      highRisk,
      auditCoverage: mergedCapabilities.length ? '100%' : '0%',
    };
  }, [mergedCapabilities]);
  const viewCounts = useMemo(() => {
    const provider = mergedCapabilities.filter((capability) => capability.kind === 'provider-tool').length;
    return {
      all: mergedCapabilities.length,
      provider,
      local: mergedCapabilities.length - provider,
    };
  }, [mergedCapabilities]);

  const openEnableDialog = (capability: Capability) => {
    setEnableTarget(capability);
    setEnableKey('');
    setEnableAgents(capability.scopedAgents || []);
    loadAgents();
  };

  const handleEnable = async () => {
    if (!enableTarget) return;
    try {
      setEnabling(true);
      setError(null);
      const agents = enableAgents.length > 0 ? enableAgents : [];
      await api.capabilities.enable(enableTarget.name, enableKey || undefined, agents.length > 0 ? agents : undefined);
      setLocalStates((prev) => ({
        ...prev,
        [enableTarget.name]: {
          state: agents.length > 0 ? 'restricted' : 'available',
          agents,
        },
      }));
      await loadCapabilities(false);
      toast.success(`Capability "${enableTarget.name}" enabled`);
      setEnableTarget(null);
      setEnableKey('');
      setEnableAgents([]);
    } catch (e: any) {
      setError(e.message || 'Failed to enable capability');
    } finally {
      setEnabling(false);
    }
  };

  const handleDisable = async (capability: Capability) => {
    try {
      setError(null);
      await api.capabilities.disable(capability.name);
      setLocalStates((prev) => ({
        ...prev,
        [capability.name]: {
          state: 'disabled',
          agents: capability.scopedAgents,
        },
      }));
      await loadCapabilities(false);
      toast.success(`Capability "${capability.name}" disabled`);
    } catch (e: any) {
      setError(e.message || 'Failed to disable capability');
    }
  };

  const toggleAgent = (name: string) => {
    setEnableAgents((prev) => prev.includes(name) ? prev.filter((agent) => agent !== name) : [...prev, name]);
  };

  return (
    <>
      <div style={{ marginBottom: 20 }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
          <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
            <MetaStat label="Total" value={stats.total} />
            <MetaStat label="Enabled" value={stats.enabled} tone="var(--teal-dark)" />
            <MetaStat label="High-risk" value={stats.highRisk} tone="var(--red)" />
            <MetaStat label="Audit coverage" value={stats.auditCoverage} tone="var(--teal-dark)" />
          </div>
          <Btn icon={<Search size={13} />}>Explain selected</Btn>
        </div>
      </div>

      {error && <div style={{ marginBottom: 14, padding: '10px 12px', border: '0.5px solid var(--red)', background: 'var(--red-tint)', color: 'var(--red)', borderRadius: 8, fontSize: 12 }}>{error}</div>}

      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12, flexWrap: 'wrap' }}>
        <span className="eyebrow" style={{ fontSize: 9 }}>View</span>
        <div style={{ display: 'inline-flex', alignItems: 'center', gap: 2, padding: 3, border: '0.5px solid var(--ink-hairline)', borderRadius: 999, background: 'var(--warm-2)' }}>
          <Segment active={view === 'all'} onClick={() => setView('all')}>All <span className="mono">{viewCounts.all}</span></Segment>
          <Segment active={view === 'provider'} onClick={() => setView('provider')}>Provider tools <span className="mono">{viewCounts.provider}</span></Segment>
          <Segment active={view === 'local'} onClick={() => setView('local')}>Local <span className="mono">{viewCounts.local}</span></Segment>
        </div>
      </div>

      <Card>
        <TableHeader widths={TABLE_WIDTHS} cols={['Action', 'From', 'Risk', 'Scope', 'Used by', '']} />
        {loading ? (
          <div style={{ padding: 32, textAlign: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>Loading registry...</div>
        ) : rows.length === 0 ? (
          <div style={{ padding: 32, textAlign: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>{view === 'provider' ? 'No provider tools found.' : 'No entries found.'}</div>
        ) : rows.map((capability) => {
          const active = isActive(capability.state);
          const risk = riskFromCapability(capability);
          return (
            <TableRow
              key={capability.name}
              widths={TABLE_WIDTHS}
              accent={risk === 'high' && active ? 'var(--red)' : risk === 'medium' && active ? 'var(--amber)' : undefined}
              cols={[
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <span className="mono" style={{ fontSize: 13, color: active ? 'var(--ink)' : 'var(--ink-faint)' }}>{capability.name}</span>
                </div>,
                <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)' }}>{packageFromCapability(capability)}</span>,
                <Badge tone={toneForRisk(risk)}>{risk}</Badge>,
                <span style={{ fontSize: 12, color: 'var(--ink-mid)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{scopeFromCapability(capability)}</span>,
                <span className="mono" style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{capability.scopedAgents.length || (active ? 'all' : 0)} agents</span>,
                <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                  <Toggle
                    on={active}
                    label={`${active ? 'Disable' : 'Enable'} ${capability.name}`}
                    onChange={() => active ? handleDisable(capability) : openEnableDialog(capability)}
                  />
                </div>,
              ]}
            />
          );
        })}
      </Card>

      {enableTarget && (() => {
        const isConfiguring = isActive(enableTarget.state);
        return (
          <div className="fixed inset-0 z-50 flex items-center justify-center">
            <div className="fixed inset-0 bg-black/60" onClick={() => setEnableTarget(null)} />
            <div className="relative bg-card border border-border rounded-lg p-4 md:p-6 w-full max-w-md mx-4 space-y-4 shadow-xl">
              <h3 className="text-lg font-semibold text-foreground">
                {isConfiguring ? 'Configure' : 'Enable'} {enableTarget.name}
              </h3>
              {enableTarget.description && <p className="text-sm text-muted-foreground">{enableTarget.description}</p>}
              <div className="space-y-2">
                <label htmlFor="capability-credential" className="text-xs text-muted-foreground uppercase tracking-wide">
                  API Key / Credential {isConfiguring ? '(leave blank to keep current)' : '(optional)'}
                </label>
                <input
                  id="capability-credential"
                  name="capability-credential"
                  type="password"
                  value={enableKey}
                  onChange={(e) => setEnableKey(e.target.value)}
                  placeholder={isConfiguring ? 'Enter new key to replace...' : 'Enter API key if required...'}
                  className="w-full bg-background border border-border text-foreground rounded px-3 py-2 text-sm placeholder:text-muted-foreground/70"
                />
              </div>
              <div className="space-y-2">
                <label className="text-xs text-muted-foreground uppercase tracking-wide">Agent access</label>
                <div className="flex flex-wrap gap-2">
                  {agents.map((agent) => (
                    <button
                      key={agent.name}
                      type="button"
                      onClick={() => toggleAgent(agent.name)}
                      className={`text-xs px-2.5 py-1 rounded border transition-colors ${
                        enableAgents.includes(agent.name)
                          ? 'bg-primary border-primary/80 text-white'
                          : 'bg-secondary border-border text-muted-foreground hover:border-border'
                      }`}
                    >
                      {agent.name}
                    </button>
                  ))}
                  {agents.length === 0 && <span className="text-xs text-muted-foreground">No agents found</span>}
                </div>
                {enableAgents.length === 0 && agents.length > 0 && <p className="text-xs text-muted-foreground">No agents selected - all agents have access</p>}
              </div>
              <div className="flex justify-end gap-2 pt-2">
                <Button variant="outline" size="sm" onClick={() => setEnableTarget(null)}>Cancel</Button>
                <Button size="sm" onClick={handleEnable} disabled={enabling}>{enabling ? 'Saving...' : isConfiguring ? 'Save' : 'Enable'}</Button>
              </div>
            </div>
          </div>
        );
      })()}
    </>
  );
}
