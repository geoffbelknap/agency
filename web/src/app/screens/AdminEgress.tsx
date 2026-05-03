import { useState, useEffect, useCallback, useMemo } from 'react';
import { Plus, RefreshCw, ShieldCheck, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { api, type RawAgent, type RawEgress } from '../lib/api';
import { formatDateTime } from '../lib/time';

type BadgeTone = 'teal' | 'amber' | 'red' | 'neutral';

type EgressSource = {
  type?: string;
  name?: string;
  added_at?: string;
};

type EgressDomain = {
  domain: string;
  sources?: EgressSource[];
  auto_managed?: boolean;
};

type HostRow = {
  host: string;
  purpose: string;
  verdict: 'allow' | 'deny';
  managed: string;
  sources: EgressSource[];
  agentScoped: boolean;
};

type EgressMode = 'denylist' | 'allowlist' | 'supervised-strict' | 'supervised-permissive';

const EGRESS_MODES: EgressMode[] = ['denylist', 'allowlist', 'supervised-strict', 'supervised-permissive'];

function Badge({ children, tone = 'neutral' }: { children: React.ReactNode; tone?: BadgeTone }) {
  const tones = {
    teal: { bg: 'var(--teal-tint)', color: 'var(--teal-dark)', border: 'var(--teal-border)' },
    amber: { bg: 'var(--amber-tint)', color: 'var(--amber-foreground)', border: 'var(--amber)' },
    red: { bg: 'var(--red-tint)', color: 'var(--red)', border: 'var(--red)' },
    neutral: { bg: 'var(--warm-3)', color: 'var(--ink-mid)', border: 'var(--ink-hairline-strong)' },
  }[tone];
  return (
    <span className="mono" style={{ display: 'inline-flex', alignItems: 'center', padding: '2px 8px', fontSize: 10, letterSpacing: '0.08em', textTransform: 'uppercase', background: tones.bg, color: tones.color, border: `0.5px solid ${tones.border}`, borderRadius: 4, whiteSpace: 'nowrap' }}>
      {children}
    </span>
  );
}

function ActionButton({ children, icon, disabled = false, onClick }: { children: React.ReactNode; icon?: React.ReactNode; disabled?: boolean; onClick?: () => void }) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '5px 10px', border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 999, background: 'var(--warm)', color: 'var(--ink)', fontSize: 12, fontFamily: 'var(--sans)', cursor: disabled ? 'default' : 'pointer', opacity: disabled ? 0.55 : 1, whiteSpace: 'nowrap' }}
    >
      {icon}
      {children}
    </button>
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

function SourceChips({ sources }: { sources: EgressSource[] }) {
  if (sources.length === 0) {
    return <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)' }}>no provenance</span>;
  }
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
      {sources.slice(0, 3).map((source, index) => (
        <span key={`${source.type || 'source'}-${source.name || index}`} className="mono" title={source.added_at ? formatDateTime(source.added_at) : undefined} style={{ padding: '2px 6px', border: '0.5px solid var(--ink-hairline)', borderRadius: 4, color: 'var(--ink-mid)', background: 'var(--warm)', fontSize: 10 }}>
          {source.type || 'source'}{source.name ? `:${source.name}` : ''}
        </span>
      ))}
      {sources.length > 3 && <span className="mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>+{sources.length - 3}</span>}
    </div>
  );
}

function DangerButton({ children, icon, disabled = false, onClick, ariaLabel }: { children: React.ReactNode; icon?: React.ReactNode; disabled?: boolean; onClick?: () => void; ariaLabel?: string }) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      aria-label={ariaLabel}
      style={{ display: 'inline-flex', alignItems: 'center', gap: 5, padding: '4px 8px', border: '0.5px solid transparent', borderRadius: 999, background: 'transparent', color: 'var(--red)', fontSize: 11, fontFamily: 'var(--sans)', cursor: disabled ? 'default' : 'pointer', opacity: disabled ? 0.45 : 1, whiteSpace: 'nowrap' }}
    >
      {icon}
      {children}
    </button>
  );
}

function HostTable({ rows, onRevoke, revokingHost }: { rows: HostRow[]; onRevoke: (host: string) => void; revokingHost?: string }) {
  if (rows.length === 0) {
    return (
      <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', padding: 28, textAlign: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>
        No managed egress hosts are active. Unlisted hosts remain denied by default.
      </div>
    );
  }

  return (
    <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', overflow: 'hidden' }}>
      <div style={{ overflowX: 'auto' }}>
        <div style={{ minWidth: 940 }}>
          <div style={{ display: 'grid', gridTemplateColumns: 'minmax(220px, 1.5fr) minmax(180px, 1fr) 92px 110px minmax(180px, 1.1fr) 84px', gap: 12, padding: '10px 16px', borderBottom: '0.5px solid var(--ink-hairline)', background: 'var(--warm-3)' }}>
            {['Host', 'Purpose', 'Verdict', 'Managed', 'Sources', ''].map((label) => (
              <span key={label || 'actions'} className="eyebrow" style={{ fontSize: 9 }}>{label}</span>
            ))}
          </div>
          {rows.map((row) => (
            <div key={`${row.host}-${row.agentScoped ? 'agent' : 'managed'}`} style={{ display: 'grid', gridTemplateColumns: 'minmax(220px, 1.5fr) minmax(180px, 1fr) 92px 110px minmax(180px, 1.1fr) 84px', gap: 12, alignItems: 'center', padding: '13px 16px', borderBottom: '0.5px solid var(--ink-hairline)', borderLeft: row.verdict === 'deny' ? '2px solid var(--red)' : '2px solid transparent' }}>
              <span className="mono" style={{ fontSize: 13, color: 'var(--ink)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{row.host}</span>
              <span style={{ fontSize: 12, color: 'var(--ink-mid)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{row.purpose}</span>
              <Badge tone={row.verdict === 'allow' ? 'teal' : 'red'}>{row.verdict}</Badge>
              <span className="mono" style={{ fontSize: 11, color: 'var(--ink-mid)' }}>{row.managed}</span>
              <SourceChips sources={row.sources} />
              <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                {row.agentScoped ? (
                  <DangerButton icon={<Trash2 size={12} />} disabled={revokingHost === row.host} onClick={() => onRevoke(row.host)} ariaLabel={`Revoke ${row.host}`}>
                    {revokingHost === row.host ? 'Revoking...' : 'Revoke'}
                  </DangerButton>
                ) : (
                  <span className="mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>managed</span>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function agentDomains(data: RawEgress | null) {
  if (!data) return [];
  const domains = data.allowed_domains || data.domains || [];
  return domains
    .map((entry) => {
      if (typeof entry === 'string') return entry;
      if (entry && typeof entry === 'object' && 'domain' in entry) return String((entry as { domain?: unknown }).domain || '');
      return '';
    })
    .filter(Boolean);
}

function egressMode(data: RawEgress | null): EgressMode {
  const mode = typeof data?.mode === 'string' ? data.mode : 'allowlist';
  return EGRESS_MODES.includes(mode as EgressMode) ? mode as EgressMode : 'allowlist';
}

export function AdminEgress() {
  const [agents, setAgents] = useState<RawAgent[]>([]);
  const [domains, setDomains] = useState<EgressDomain[]>([]);
  const [selectedAgent, setSelectedAgent] = useState('');
  const [egressData, setEgressData] = useState<RawEgress | null>(null);
  const [loading, setLoading] = useState(true);
  const [agentLoading, setAgentLoading] = useState(false);
  const [mutating, setMutating] = useState<string | null>(null);
  const [newDomain, setNewDomain] = useState('');
  const [newReason, setNewReason] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [agentError, setAgentError] = useState<string | null>(null);

  const loadAgentEgress = useCallback(async (agent: string) => {
    if (!agent) {
      setEgressData(null);
      return;
    }
    try {
      setAgentLoading(true);
      setAgentError(null);
      setEgressData(await api.admin.egress(agent));
    } catch (e: any) {
      setEgressData(null);
      setAgentError(e.message || 'Failed to load agent egress');
    } finally {
      setAgentLoading(false);
    }
  }, []);

  const loadBase = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const [agentData, domainData] = await Promise.all([
        api.agents.list().catch(() => [] as RawAgent[]),
        api.admin.egressDomains().catch(() => [] as EgressDomain[]),
      ]);
      const nextAgents = (agentData || []).filter((agent) => agent.name);
      setAgents(nextAgents);
      setDomains((domainData || []).filter((entry: any) => entry?.domain));
    } catch (e: any) {
      setError(e.message || 'Failed to load egress policy');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { loadBase(); }, [loadBase]);

  useEffect(() => {
    if (!selectedAgent && agents.length > 0) setSelectedAgent(agents[0].name);
  }, [agents, selectedAgent]);

  useEffect(() => {
    if (selectedAgent) loadAgentEgress(selectedAgent);
  }, [selectedAgent, loadAgentEgress]);

  const selectedAgentDomains = useMemo(() => agentDomains(egressData), [egressData]);
  const currentMode = useMemo(() => egressMode(egressData), [egressData]);

  const rows = useMemo(() => {
    const managedRows: HostRow[] = domains.map((entry) => ({
      host: entry.domain,
      purpose: entry.sources?.length ? 'managed allowlist' : 'allowlist entry',
      verdict: 'allow',
      managed: entry.auto_managed ? 'auto' : 'manual',
      sources: entry.sources || [],
      agentScoped: false,
    }));
    const managedHosts = new Set(managedRows.map((row) => row.host));
    const agentRows: HostRow[] = selectedAgentDomains
      .filter((domain) => !managedHosts.has(domain))
      .map((domain) => ({
        host: domain,
        purpose: selectedAgent ? `${selectedAgent} allowlist` : 'agent allowlist',
        verdict: 'allow',
        managed: 'agent',
        sources: selectedAgent ? [{ type: 'agent', name: selectedAgent }] : [],
        agentScoped: true,
      }));
    return [...managedRows, ...agentRows].sort((a, b) => a.host.localeCompare(b.host));
  }, [domains, selectedAgent, selectedAgentDomains]);

  const autoManagedCount = domains.filter((entry) => entry.auto_managed).length;

  const approveDomain = async () => {
    if (!selectedAgent || !newDomain.trim()) return;
    try {
      setMutating('approve');
      const next = await api.admin.approveEgressDomain(selectedAgent, newDomain, newReason);
      setEgressData(next.egress);
      setNewDomain('');
      setNewReason('');
      toast.success(`Allowed ${newDomain.trim()} for ${selectedAgent}`);
    } catch (e: any) {
      toast.error(e.message || 'Failed to allow host');
    } finally {
      setMutating(null);
    }
  };

  const revokeDomain = async (domain: string) => {
    if (!selectedAgent) return;
    try {
      setMutating(`revoke:${domain}`);
      const next = await api.admin.revokeEgressDomain(selectedAgent, domain);
      setEgressData(next.egress);
      toast.success(`Revoked ${domain} from ${selectedAgent}`);
    } catch (e: any) {
      toast.error(e.message || 'Failed to revoke host');
    } finally {
      setMutating(null);
    }
  };

  const updateMode = async (mode: EgressMode) => {
    if (!selectedAgent || mode === currentMode) return;
    try {
      setMutating('mode');
      const next = await api.admin.updateEgressMode(selectedAgent, mode);
      setEgressData(next.egress);
      toast.success(`Set ${selectedAgent} egress mode to ${mode}`);
    } catch (e: any) {
      toast.error(e.message || 'Failed to update egress mode');
    } finally {
      setMutating(null);
    }
  };

  return (
    <>
      <div style={{ marginBottom: 20 }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
          <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
            <MetaStat label="Mode" value={currentMode} tone="var(--teal-dark)" />
            <MetaStat label="Managed hosts" value={domains.length} />
            <MetaStat label="Auto-managed" value={autoManagedCount} />
            <MetaStat label="Agent hosts" value={selectedAgentDomains.length} tone="var(--teal-dark)" />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
            <select
              id="egress-agent"
              name="egress-agent"
              value={selectedAgent}
              onChange={(event) => setSelectedAgent(event.target.value)}
              style={{ height: 32, minWidth: 160, border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 999, background: 'var(--warm)', color: 'var(--ink)', padding: '0 12px', fontFamily: 'var(--sans)', fontSize: 12 }}
            >
              <option value="">Select agent...</option>
              {agents.map((agent) => <option key={agent.name} value={agent.name}>{agent.name}</option>)}
            </select>
            <ActionButton icon={<RefreshCw size={13} />} disabled={loading || agentLoading} onClick={() => { loadBase(); if (selectedAgent) loadAgentEgress(selectedAgent); }}>
              {loading || agentLoading ? 'Refreshing...' : 'Refresh'}
            </ActionButton>
          </div>
        </div>
      </div>

      {(error || agentError) && (
        <div style={{ marginBottom: 14, border: '0.5px solid var(--red)', borderRadius: 10, background: 'var(--red-tint)', color: 'var(--red)', padding: '10px 12px', fontSize: 12 }}>
          {error || agentError}
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) 260px', gap: 14, alignItems: 'start' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12, minWidth: 0 }}>
          <section style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', overflow: 'hidden' }}>
            <div style={{ display: 'grid', gridTemplateColumns: 'minmax(180px, 0.75fr) minmax(360px, 1.5fr)', gap: 0 }}>
              <div style={{ padding: 16, borderRight: '0.5px solid var(--ink-hairline)', background: 'var(--warm-3)' }}>
                <div className="eyebrow" style={{ fontSize: 9, marginBottom: 8 }}>Policy changes</div>
                <div className="display" style={{ color: 'var(--ink)', fontSize: 20, lineHeight: 1.1 }}>Edit {selectedAgent || 'agent'} egress</div>
                <p style={{ color: 'var(--ink-mid)', fontSize: 12, lineHeight: 1.45, margin: '8px 0 0' }}>
                  Changes write through the gateway policy API and stay outside the agent boundary.
                </p>
              </div>
              <div style={{ padding: 16, display: 'grid', gridTemplateColumns: '160px minmax(180px, 1fr) minmax(160px, 0.8fr) auto', gap: 10, alignItems: 'end' }}>
                <label style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                  <span className="mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>Mode</span>
                  <select
                    id="egress-mode"
                    name="egress-mode"
                    aria-label="Mode"
                    value={currentMode}
                    disabled={!selectedAgent || mutating === 'mode'}
                    onChange={(event) => updateMode(event.target.value as EgressMode)}
                    style={{ height: 34, border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 8, background: 'var(--warm)', color: 'var(--ink)', padding: '0 10px', fontFamily: 'var(--mono)', fontSize: 11, opacity: !selectedAgent ? 0.55 : 1 }}
                  >
                    {EGRESS_MODES.map((mode) => <option key={mode} value={mode}>{mode}</option>)}
                  </select>
                </label>
                <label style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                  <span className="mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>Host</span>
                  <input
                    id="egress-new-domain"
                    name="egress-new-domain"
                    value={newDomain}
                    onChange={(event) => setNewDomain(event.target.value)}
                    placeholder="api.example.com"
                    style={{ height: 34, border: '0.5px solid var(--ink-hairline)', borderRadius: 8, background: 'var(--warm)', color: 'var(--ink)', padding: '0 10px', fontFamily: 'var(--mono)', fontSize: 12 }}
                  />
                </label>
                <label style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                  <span className="mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>Reason</span>
                  <input
                    id="egress-new-reason"
                    name="egress-new-reason"
                    value={newReason}
                    onChange={(event) => setNewReason(event.target.value)}
                    placeholder="operator approval"
                    style={{ height: 34, border: '0.5px solid var(--ink-hairline)', borderRadius: 8, background: 'var(--warm)', color: 'var(--ink)', padding: '0 10px', fontSize: 12 }}
                  />
                </label>
                <ActionButton icon={<Plus size={13} />} disabled={!selectedAgent || !newDomain.trim() || mutating === 'approve'} onClick={approveDomain}>
                  {mutating === 'approve' ? 'Allowing...' : 'Allow host'}
                </ActionButton>
              </div>
            </div>
          </section>

          <HostTable rows={rows} onRevoke={revokeDomain} revokingHost={mutating?.startsWith('revoke:') ? mutating.slice('revoke:'.length) : undefined} />
        </div>

        <aside style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', padding: 16, display: 'flex', flexDirection: 'column', gap: 14 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <ShieldCheck size={16} style={{ color: 'var(--teal-dark)' }} />
            <span className="display" style={{ fontSize: 18, color: 'var(--ink)' }}>Boundary</span>
          </div>
          <p style={{ margin: 0, color: 'var(--ink-mid)', fontSize: 12, lineHeight: 1.5 }}>
            Egress remains mediated outside the agent boundary. Changes below write through the gateway policy API and audit trail.
          </p>
          <div style={{ borderTop: '0.5px solid var(--ink-hairline)', paddingTop: 12, display: 'flex', flexDirection: 'column', gap: 8 }}>
            <div>
              <div className="eyebrow" style={{ fontSize: 9, marginBottom: 5 }}>Selected agent</div>
              <div className="mono" style={{ color: 'var(--ink)', fontSize: 13 }}>{selectedAgent || 'none'}</div>
            </div>
            <div>
              <div className="eyebrow" style={{ fontSize: 9, marginBottom: 5 }}>Mode</div>
              <div className="mono" style={{ color: 'var(--ink)', fontSize: 13 }}>{currentMode}</div>
            </div>
            {selectedAgentDomains.length > 0 && (
              <div>
                <div className="eyebrow" style={{ fontSize: 9, marginBottom: 6 }}>Agent allowlist</div>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                  {selectedAgentDomains.slice(0, 6).map((domain) => (
                    <span key={domain} className="mono" style={{ fontSize: 11, color: 'var(--ink-mid)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{domain}</span>
                  ))}
                  {selectedAgentDomains.length > 6 && <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)' }}>+{selectedAgentDomains.length - 6} more</span>}
                </div>
              </div>
            )}
          </div>
        </aside>
      </div>
    </>
  );
}
