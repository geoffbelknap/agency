import { useState, useEffect, useCallback, useMemo } from 'react';
import { useParams, useNavigate } from 'react-router';
import { Bot, MessageSquare, MoreHorizontal, Pause, Play, Plus, RefreshCw, Settings } from 'lucide-react';
import { toast } from 'sonner';
import { Agent } from '../types';
import { api, type RawAgent, type RawCapability } from '../lib/api';
import { socket } from '../lib/ws';
import { useIsMobile } from '../components/ui/use-mobile';
import { CreateAgentDialog } from '../components/CreateAgentDialog';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { AgentList } from './agents/AgentList';
import { AgentDetail } from './agents/AgentDetail';

function mapAgent(a: RawAgent): Agent {
  return {
    id: a.name,
    name: a.name,
    status: (a.status || 'stopped') as Agent['status'],
    mode: (a.mode || 'assisted') as Agent['mode'],
    type: a.type || 'agent',
    preset: a.preset || '',
    team: a.team || '',
    enforcerState: a.enforcer || '',
    model: a.model,
    role: a.role,
    uptime: a.uptime,
    lastActive: a.last_active,
    trustLevel: a.trust_level,
    restrictions: a.restrictions,
    grantedCapabilities: a.granted_capabilities,
    currentTask: a.current_task,
    mission: a.mission,
    missionStatus: a.mission_status,
    buildId: a.build_id,
  };
}

const AGENTS_CACHE_KEY = 'agency.agents.lastList';
const AGENTS_CACHE_ENABLED = import.meta.env.MODE !== 'test';
const AGENTS_VARIANT_KEY = 'agency.agents.variant';

type AgentViewVariant = 'split' | 'roster' | 'dispatch' | 'timeline';
type AgentBudget = { daily_used: number; daily_limit: number };

const AGENT_VIEW_VARIANTS: Array<{ id: AgentViewVariant; label: string; hint: string }> = [
  { id: 'split', label: 'Split view', hint: 'List and detail' },
  { id: 'roster', label: 'Roster', hint: 'Rich cards grid' },
  { id: 'dispatch', label: 'Dispatch', hint: 'Columns by status' },
  { id: 'timeline', label: 'Timeline', hint: 'Activity over time' },
];

const STATUS_DOT_COLOR: Record<string, string> = {
  running: 'var(--teal)',
  stopped: 'var(--red)',
  paused: 'var(--amber)',
  halted: 'var(--amber)',
  unhealthy: 'var(--red)',
  idle: 'var(--ink-faint)',
};

function statusDotColor(status: string): string {
  return STATUS_DOT_COLOR[status] ?? 'var(--ink-faint)';
}

function readVariant(): AgentViewVariant {
  if (typeof window === 'undefined') return 'split';
  try {
    const stored = window.localStorage.getItem(AGENTS_VARIANT_KEY);
    return AGENT_VIEW_VARIANTS.some((variant) => variant.id === stored) ? stored as AgentViewVariant : 'split';
  } catch {
    return 'split';
  }
}

function writeVariant(variant: AgentViewVariant) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(AGENTS_VARIANT_KEY, variant);
  } catch {
    // View preference is optional.
  }
}

function budgetPct(budget?: AgentBudget): number {
  return budget && budget.daily_limit > 0 ? Math.min(100, (budget.daily_used / budget.daily_limit) * 100) : 0;
}

function budgetColor(budget?: AgentBudget): string {
  if (!budget || budget.daily_limit <= 0) return 'var(--ink-faint)';
  const pct = budget.daily_used / budget.daily_limit;
  if (pct >= 0.9) return 'var(--red)';
  if (pct >= 0.75) return 'var(--amber)';
  return 'var(--teal)';
}

function relativeTime(iso?: string): string {
  if (!iso) return 'just now';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  const diff = Date.now() - date.getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

function AgentIcon({ size = 44, iconSize = 20, status }: { size?: number; iconSize?: number; status?: string }) {
  return (
    <div style={{ position: 'relative', flexShrink: 0 }}>
      <div style={{ width: size, height: size, borderRadius: Math.max(6, size / 4.4), background: 'var(--warm-3)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Bot size={iconSize} style={{ color: 'var(--ink-mid)' }} aria-hidden="true" />
      </div>
      {status && (
        <span style={{ position: 'absolute', bottom: -2, right: -2, width: Math.max(8, size / 3.6), height: Math.max(8, size / 3.6), borderRadius: '50%', background: statusDotColor(status), border: '2px solid var(--warm)' }} />
      )}
    </div>
  );
}

function SparkBar({ value, height = 24 }: { value: number; height?: number }) {
  const bars = Array.from({ length: 14 }, (_, index) => Math.max(2, Math.round(((value + index * 13) % 37) / 2)));
  return (
    <div style={{ display: 'flex', alignItems: 'end', gap: 2, height, minWidth: 86 }}>
      {bars.map((bar, index) => (
        <span key={index} style={{ width: 3, height: `${Math.min(100, 18 + bar * 3)}%`, borderRadius: 2, background: index > 10 ? 'var(--teal)' : 'var(--warm-3)' }} />
      ))}
    </div>
  );
}

function RosterView({
  agents,
  budgets,
  onSelect,
  onCreate,
  onOpenDM,
  onAction,
  actionLoading,
}: {
  agents: Agent[];
  budgets: Record<string, AgentBudget>;
  onSelect: (name: string) => void;
  onCreate: () => void;
  onOpenDM: (name: string) => void;
  onAction: (name: string, action: string) => void;
  actionLoading: string | null;
}) {
  return (
    <div style={{ flex: 1, overflowY: 'auto', padding: '28px 40px', minHeight: 0 }}>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: 16 }}>
        {agents.map((agent) => {
          const budget = budgets[agent.name];
          const pct = budgetPct(budget);
          return (
            <div key={agent.name} style={{ background: 'var(--warm)', border: '0.5px solid var(--ink-hairline)', borderRadius: 12, padding: 20, display: 'flex', flexDirection: 'column', gap: 14 }}>
              <button type="button" onClick={() => onSelect(agent.name)} style={{ display: 'flex', alignItems: 'flex-start', gap: 12, border: 0, padding: 0, background: 'transparent', textAlign: 'left', cursor: 'pointer', fontFamily: 'var(--font-sans)' }}>
                <AgentIcon status={agent.status} />
                <span style={{ flex: 1, minWidth: 0 }}>
                  <span style={{ display: 'block', fontFamily: 'var(--font-display)', fontSize: 20, fontWeight: 400, letterSpacing: '-0.01em', color: 'var(--ink)' }}>{agent.name}</span>
                  <span style={{ display: 'block', fontSize: 12, color: 'var(--ink-mid)', marginTop: 2 }}>{agent.role || agent.preset || agent.type}</span>
                </span>
                <MoreHorizontal size={14} style={{ color: 'var(--ink-faint)', marginTop: 4 }} aria-hidden="true" />
              </button>
              <div style={{ padding: '10px 12px', background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 8, fontSize: 12, color: 'var(--ink)', lineHeight: 1.4, minHeight: 56, display: 'flex', alignItems: 'center' }}>
                {agent.mission || <em style={{ color: 'var(--ink-faint)', fontStyle: 'italic' }}>No mission assigned</em>}
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                <SparkBar value={pct} />
                <div style={{ flex: 1 }} />
                <span className="font-mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>{relativeTime(agent.lastActive)}</span>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span className="font-mono" style={{ fontSize: 10, color: 'var(--ink-faint)', width: 48 }}>budget</span>
                <div style={{ flex: 1, height: 4, background: 'var(--warm-3)', borderRadius: 2, overflow: 'hidden' }}>
                  <div style={{ width: `${pct}%`, height: '100%', background: budgetColor(budget) }} />
                </div>
                <span className="font-mono" style={{ fontSize: 10, color: 'var(--ink-mid)' }}>{budget ? `$${budget.daily_used.toFixed(2)}/$${budget.daily_limit.toFixed(2)}` : '-'}</span>
              </div>
              <div style={{ display: 'flex', gap: 6, paddingTop: 6, borderTop: '0.5px solid var(--ink-hairline)' }}>
                <button type="button" onClick={() => onOpenDM(agent.name)} style={smallGhostButtonStyle}><MessageSquare size={13} />DM</button>
                <button type="button" disabled={!!actionLoading} onClick={() => onAction(agent.name, agent.status === 'running' ? 'pause' : 'start')} style={smallGhostButtonStyle}>
                  {agent.status === 'running' ? <Pause size={13} /> : <Play size={13} />}
                  {agent.status === 'running' ? 'Pause' : 'Start'}
                </button>
                <div style={{ flex: 1 }} />
                <button type="button" onClick={() => onSelect(agent.name)} aria-label={`Settings for ${agent.name}`} style={smallGhostButtonStyle}><Settings size={13} /></button>
              </div>
            </div>
          );
        })}
        <button type="button" onClick={onCreate} style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 10, minHeight: 220, padding: 24, background: 'transparent', border: '1.5px dashed var(--ink-hairline-strong)', borderRadius: 12, cursor: 'pointer', color: 'var(--ink-mid)', fontFamily: 'var(--font-sans)' }}>
          <span style={{ width: 40, height: 40, borderRadius: '50%', border: '1px solid var(--ink-hairline-strong)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <Plus size={18} />
          </span>
          <span style={{ fontSize: 13 }}>New agent</span>
          <span style={{ fontSize: 11, color: 'var(--ink-faint)' }}>Pick a preset or start blank</span>
        </button>
      </div>
    </div>
  );
}

function DispatchView({ agents, budgets, onSelect }: { agents: Agent[]; budgets: Record<string, AgentBudget>; onSelect: (name: string) => void }) {
  const columns = [
    { key: 'running', label: 'Running', dot: 'var(--teal)', items: agents.filter((agent) => agent.status === 'running') },
    { key: 'attention', label: 'Needs attention', dot: 'var(--red)', items: agents.filter((agent) => agent.status === 'unhealthy') },
    { key: 'paused', label: 'Paused', dot: 'var(--amber)', items: agents.filter((agent) => agent.status === 'paused' || agent.status === 'halted') },
    { key: 'stopped', label: 'Stopped', dot: 'var(--ink-faint)', items: agents.filter((agent) => agent.status === 'stopped') },
  ];
  return (
    <div style={{ flex: 1, overflow: 'auto', padding: '24px 32px', minHeight: 0 }}>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, minmax(220px, 1fr))', gap: 16, minWidth: 900 }}>
        {columns.map((column) => (
          <div key={column.key} style={{ background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 12, padding: 12, display: 'flex', flexDirection: 'column', gap: 10 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '4px 6px' }}>
              <span style={{ width: 8, height: 8, borderRadius: '50%', background: column.dot }} />
              <span className="font-mono" style={{ fontSize: 11, color: 'var(--ink)', textTransform: 'uppercase', letterSpacing: '0.1em', fontWeight: 700 }}>{column.label}</span>
              <span className="font-mono" style={{ fontSize: 10, color: 'var(--ink-faint)', marginLeft: 'auto' }}>{column.items.length}</span>
            </div>
            {column.items.length === 0 && <div style={{ padding: '20px 12px', textAlign: 'center', fontSize: 11, color: 'var(--ink-faint)', border: '1px dashed var(--ink-hairline)', borderRadius: 8 }}>No agents here.</div>}
            {column.items.map((agent) => {
              const budget = budgets[agent.name];
              const pct = budgetPct(budget);
              return (
                <button key={agent.name} type="button" onClick={() => onSelect(agent.name)} style={{ background: 'var(--warm)', border: '0.5px solid var(--ink-hairline)', borderRadius: 10, padding: 12, cursor: 'pointer', display: 'flex', flexDirection: 'column', gap: 8, textAlign: 'left', fontFamily: 'var(--font-sans)' }}>
                  <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <AgentIcon size={26} iconSize={13} status={agent.status} />
                    <span className="font-mono" style={{ fontSize: 12, color: 'var(--ink)' }}>{agent.name}</span>
                    <span className="font-mono" style={{ fontSize: 10, color: 'var(--ink-faint)', marginLeft: 'auto' }}>{relativeTime(agent.lastActive)}</span>
                  </span>
                  <span style={{ fontSize: 11, color: 'var(--ink-mid)', lineHeight: 1.4, minHeight: 30 }}>{agent.mission || <span style={{ color: 'var(--ink-faint)', fontStyle: 'italic' }}>No mission</span>}</span>
                  <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <span style={{ flex: 1, height: 3, background: 'var(--warm-3)', borderRadius: 2, overflow: 'hidden' }}>
                      <span style={{ display: 'block', width: `${pct}%`, height: '100%', background: budgetColor(budget) }} />
                    </span>
                    <span className="font-mono" style={{ fontSize: 9, color: pct > 90 ? 'var(--red)' : 'var(--ink-faint)' }}>{pct.toFixed(0)}%</span>
                  </span>
                </button>
              );
            })}
          </div>
        ))}
      </div>
    </div>
  );
}

function TimelineView({ agents, budgets, onSelect }: { agents: Agent[]; budgets: Record<string, AgentBudget>; onSelect: (name: string) => void }) {
  const markers = [0, 15, 30, 45, 60];
  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
      <div style={{ padding: '14px 32px', borderBottom: '0.5px solid var(--ink-hairline)', display: 'flex', alignItems: 'center', gap: 14 }}>
        <span className="eyebrow" style={{ fontSize: 9 }}>Window</span>
        <div style={{ display: 'inline-flex', padding: 2, gap: 2, background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 999 }}>
          {['15m', '1h', '6h', '24h'].map((window, index) => (
            <button key={window} type="button" style={{ padding: '4px 10px', border: 0, borderRadius: 999, background: index === 1 ? 'var(--ink)' : 'transparent', color: index === 1 ? 'var(--warm)' : 'var(--ink-mid)', fontFamily: 'var(--font-sans)', fontSize: 11, cursor: 'pointer' }}>{window}</button>
          ))}
        </div>
        <div style={{ flex: 1 }} />
        <LegendDot color="var(--teal)" label="Activity" />
        <LegendDot color="var(--amber)" label="Reply" />
        <LegendDot color="var(--red)" label="Alert" />
      </div>
      <div style={{ flex: 1, overflow: 'auto', padding: '20px 0', minHeight: 0 }}>
        <div style={{ position: 'relative', minWidth: 760 }}>
          <div style={{ position: 'sticky', top: 0, zIndex: 2, background: 'var(--warm)', display: 'grid', gridTemplateColumns: '200px 1fr 140px', borderBottom: '0.5px solid var(--ink-hairline)', padding: '8px 0' }}>
            <div style={{ padding: '0 20px' }}><span className="eyebrow" style={{ fontSize: 9 }}>Agent</span></div>
            <div style={{ position: 'relative', height: 20 }}>{markers.map((marker) => <span key={marker} className="font-mono" style={{ position: 'absolute', left: `${(marker / 60) * 100}%`, top: 0, transform: 'translateX(-50%)', fontSize: 10, color: 'var(--ink-faint)' }}>-{marker}m</span>)}</div>
            <div style={{ padding: '0 20px', textAlign: 'right' }}><span className="eyebrow" style={{ fontSize: 9 }}>Now</span></div>
          </div>
          {agents.map((agent, row) => {
            const pct = budgetPct(budgets[agent.name]);
            return (
              <button key={agent.name} type="button" onClick={() => onSelect(agent.name)} style={{ display: 'grid', gridTemplateColumns: '200px 1fr 140px', alignItems: 'center', height: 64, width: '100%', border: 0, borderBottom: '0.5px solid var(--ink-hairline)', background: row % 2 === 0 ? 'transparent' : 'var(--warm-2)', cursor: 'pointer', textAlign: 'left', fontFamily: 'var(--font-sans)' }}>
                <span style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '0 20px', minWidth: 0 }}>
                  <AgentIcon size={28} iconSize={13} status={agent.status} />
                  <span style={{ minWidth: 0 }}>
                    <span className="font-mono" style={{ display: 'block', fontSize: 12, color: 'var(--ink)' }}>{agent.name}</span>
                    <span style={{ display: 'block', fontSize: 10, color: 'var(--ink-faint)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{agent.role || agent.preset || agent.type}</span>
                  </span>
                </span>
                <span style={{ position: 'relative', height: 48 }}>
                  {markers.map((marker) => <span key={marker} style={{ position: 'absolute', left: `${(marker / 60) * 100}%`, top: 0, bottom: 0, borderLeft: '0.5px dashed var(--ink-hairline)' }} />)}
                  <span style={{ position: 'absolute', left: `${Math.max(8, 100 - pct)}%`, top: '50%', transform: 'translateY(-50%)', width: `${Math.max(8, pct / 2)}%`, maxWidth: '42%', height: 10, background: statusDotColor(agent.status), borderRadius: 3, opacity: 0.85 }} />
                  <span style={{ position: 'absolute', right: 0, top: 0, bottom: 0, width: 2, background: 'var(--teal)' }} />
                </span>
                <span style={{ padding: '0 20px', display: 'flex', flexDirection: 'column', gap: 3, alignItems: 'flex-end' }}>
                  <SparkBar value={pct} height={22} />
                  <span className="font-mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>{relativeTime(agent.lastActive)}</span>
                </span>
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}

function LegendDot({ color, label }: { color: string; label: string }) {
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
      <span style={{ width: 8, height: 8, borderRadius: 2, background: color }} />
      <span className="font-mono" style={{ fontSize: 10, color: 'var(--ink-mid)' }}>{label}</span>
    </span>
  );
}

const smallGhostButtonStyle = {
  display: 'inline-flex',
  alignItems: 'center',
  gap: 5,
  border: 0,
  background: 'transparent',
  color: 'var(--ink-mid)',
  fontFamily: 'var(--font-sans)',
  fontSize: 12,
  padding: '5px 8px',
  borderRadius: 999,
  cursor: 'pointer',
} as const;

function readCachedAgents(): Agent[] {
  if (!AGENTS_CACHE_ENABLED || typeof window === 'undefined') return [];
  try {
    const raw = window.sessionStorage.getItem(AGENTS_CACHE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as RawAgent[];
    return Array.isArray(parsed) ? parsed.map(mapAgent) : [];
  } catch {
    return [];
  }
}

function writeCachedAgents(agents: RawAgent[]) {
  if (!AGENTS_CACHE_ENABLED || typeof window === 'undefined') return;
  try {
    window.sessionStorage.setItem(AGENTS_CACHE_KEY, JSON.stringify(agents));
  } catch {
    // Cache is an optimization only.
  }
}

export function Agents() {
  const { name: urlAgentName } = useParams<{ name: string }>();
  const navigate = useNavigate();
  const isMobile = useIsMobile();
  const [agents, setAgents] = useState<Agent[]>(() => readCachedAgents());
  const [capabilities, setCapabilities] = useState<RawCapability[]>([]);
  const [loading, setLoading] = useState(() => readCachedAgents().length === 0);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [refreshingAgents, setRefreshingAgents] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const [budgets, setBudgets] = useState<Record<string, { daily_used: number; daily_limit: number }>>({});
  const [variant, setVariant] = useState<AgentViewVariant>(() => readVariant());

  const routeSelectedAgent = urlAgentName ? agents.find((a) => a.name === urlAgentName) ?? null : null;

  const load = useCallback(async () => {
    setRefreshingAgents(true);
    try {
      const data = await api.agents.list();
      const mapped = (data ?? []).map(mapAgent);
      setAgents(mapped);
      writeCachedAgents(data ?? []);
      void api.capabilities.list()
        .then(setCapabilities)
        .catch(() => setCapabilities([] as RawCapability[]));
      Promise.allSettled(mapped.map((a) =>
        api.agents.budget(a.name).then((b) => ({ name: a.name, daily_used: b.daily_used, daily_limit: b.daily_limit })),
      )).then((results) => {
        const bmap: Record<string, { daily_used: number; daily_limit: number }> = {};
        for (const r of results) {
          if (r.status === 'fulfilled' && r.value.daily_limit > 0) {
            bmap[r.value.name] = { daily_used: r.value.daily_used, daily_limit: r.value.daily_limit };
          }
        }
        setBudgets(bmap);
      });
    } catch (e) {
      console.error(e);
    } finally {
      setRefreshingAgents(false);
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    const unsub = socket.on('agent_status', () => load());
    return () => { unsub(); };
  }, [load]);

  const handleAction = async (name: string, action: string) => {
    setActionLoading(`${name}-${action}`);
    try {
      if (action === 'start') { await api.agents.start(name); toast.success(`Agent "${name}" started`); }
      else if (action === 'pause') { await api.agents.halt(name, 'supervised'); toast.success(`Agent "${name}" paused`); }
      else if (action === 'resume') { await api.agents.resume(name); toast.success(`Agent "${name}" resumed`); }
      else if (action === 'restart') { await api.agents.restart(name); toast.success(`Agent "${name}" restarted`); }
      await load();
    } catch (e: any) {
      toast.error(e.message || 'Action failed');
    } finally {
      setActionLoading(null);
    }
  };

  const handleSelect = (name: string) => {
    navigate(`/agents/${name}`, { replace: true });
  };

  const handleVariantChange = (nextVariant: AgentViewVariant) => {
    setVariant(nextVariant);
    writeVariant(nextVariant);
    if (nextVariant !== 'split' && urlAgentName) {
      navigate('/agents', { replace: true });
    }
  };

  const handleOpenDM = async (name: string) => {
    try {
      const result = await api.agents.ensureDM(name);
      navigate(`/channels/${encodeURIComponent(result.channel || `dm-${name}`)}`);
    } catch (e: any) {
      toast.error(e.message || 'Failed to open DM');
    }
  };

  const handleDeleteAgent = async () => {
    if (!deleteTarget) return;
    const name = deleteTarget;
    try {
      await api.agents.delete(name);
      toast.success(`Agent "${name}" deleted`);
      setDeleteTarget(null);
      if (urlAgentName === name) {
        navigate('/agents', { replace: true });
      }
      await load();
    } catch (e: any) {
      toast.error(e.message || `Failed to delete agent "${name}"`);
      throw e;
    }
  };

  const listRows = useMemo(
    () => agents.map((a) => ({
      name: a.name,
      status: a.status,
      role: a.role || a.preset || a.type,
      mission: a.mission,
      lastActive: a.lastActive || '',
      budget: budgets[a.name],
    })),
    [agents, budgets],
  );

  const selectedAgent = routeSelectedAgent ?? (!urlAgentName && !isMobile ? agents[0] ?? null : null);
  const activeVariant = isMobile ? 'split' : variant;
  const pillButtonStyle = {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 6,
    padding: '5px 10px',
    fontSize: 12,
    fontWeight: 400,
    fontFamily: 'var(--font-sans)',
    cursor: 'pointer',
    borderRadius: 999,
  } as const;

  return (
    <div className="h-full min-h-0" style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0, background: 'var(--warm)' }}>
      <div style={{ minHeight: 58, padding: '8px 16px', borderBottom: '0.5px solid var(--ink-hairline)', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 14, flexWrap: 'wrap', background: 'var(--warm)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
          <div className="eyebrow" style={{ fontSize: 9 }}>Agents</div>
          <div style={{ display: 'inline-flex', padding: 2, gap: 2, background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 999 }}>
            {AGENT_VIEW_VARIANTS.map((item) => (
              <button
                key={item.id}
                type="button"
                title={item.hint}
                onClick={() => handleVariantChange(item.id)}
                style={{ padding: '5px 11px', border: 0, borderRadius: 999, background: activeVariant === item.id ? 'var(--ink)' : 'transparent', color: activeVariant === item.id ? 'var(--warm)' : 'var(--ink-mid)', fontFamily: 'var(--font-sans)', fontSize: 12, cursor: 'pointer' }}
              >
                {item.label}
              </button>
            ))}
          </div>
        </div>
        <div className="sr-only">{agents.length} total agents</div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <button
            type="button"
            onClick={load}
            disabled={refreshingAgents}
            aria-label={refreshingAgents ? 'Refreshing agents' : 'Refresh agents'}
            style={{ ...pillButtonStyle, background: 'var(--warm)', color: 'var(--ink)', border: '0.5px solid var(--ink-hairline-strong)', opacity: refreshingAgents ? 0.5 : 1 }}
          >
            <RefreshCw size={13} className={refreshingAgents ? 'animate-spin' : ''} aria-hidden="true" />
            Refresh
          </button>
          <button
            type="button"
            aria-label="Create new agent"
            onClick={() => setCreateOpen(true)}
            style={{ ...pillButtonStyle, background: 'var(--ink)', color: 'var(--warm)', border: '0.5px solid var(--ink)' }}
          >
            <Plus size={13} aria-hidden="true" />
            New agent
          </button>
        </div>
      </div>

      {activeVariant === 'split' && <div style={{ flex: 1, display: 'grid', gridTemplateColumns: 'minmax(320px, 360px) minmax(0, 1fr)', minHeight: 0 }}>
        <section className={`${routeSelectedAgent ? 'hidden lg:flex' : 'flex'} min-h-0 flex-col`} style={{ borderRight: '0.5px solid var(--ink-hairline)', overflowY: 'auto', overflowX: 'hidden', minWidth: 0 }}>
          <div style={{ minHeight: 0, flex: 1 }}>
            {loading ? (
              <div style={{ padding: 20, fontSize: 13, color: 'var(--ink-faint)' }}>Loading agents...</div>
            ) : agents.length === 0 ? (
              <div style={{ margin: 20, border: '1px dashed var(--ink-hairline-strong)', borderRadius: 10, padding: '28px 16px', fontSize: 13, color: 'var(--ink-faint)' }}>No agents. Create one to get started.</div>
            ) : (
              <AgentList
                agents={listRows}
                selectedAgent={selectedAgent?.name ?? null}
                onSelect={handleSelect}
              />
            )}
          </div>
        </section>

        <section className={`${!selectedAgent && isMobile ? 'hidden' : 'block'}`} style={{ minHeight: 0, overflow: 'hidden', background: 'var(--warm)' }}>
          {selectedAgent ? (
            <AgentDetail
              agent={selectedAgent}
              capabilities={capabilities}
              onAction={handleAction}
              actionLoading={actionLoading}
              onRefreshAgents={load}
              onRequestDelete={setDeleteTarget}
            />
          ) : (
            <div className="flex h-full min-h-[320px] items-center justify-center px-6 text-center">
              <div className="space-y-2">
                <div style={{ fontSize: 13, color: 'var(--ink)' }}>No agent selected</div>
                <div style={{ fontSize: 13, color: 'var(--ink-mid)' }}>
                  Choose an agent from the fleet panel to inspect activity, controls, and system state.
                </div>
              </div>
            </div>
          )}
        </section>
      </div>}

      {activeVariant === 'roster' && (
        <RosterView
          agents={agents}
          budgets={budgets}
          onSelect={handleSelect}
          onCreate={() => setCreateOpen(true)}
          onOpenDM={(name) => void handleOpenDM(name)}
          onAction={(name, action) => void handleAction(name, action)}
          actionLoading={actionLoading}
        />
      )}

      {activeVariant === 'dispatch' && <DispatchView agents={agents} budgets={budgets} onSelect={handleSelect} />}
      {activeVariant === 'timeline' && <TimelineView agents={agents} budgets={budgets} onSelect={handleSelect} />}

      <CreateAgentDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={({ name, started, dmChannel }) => {
          void load();
          if (dmChannel) {
            navigate(`/channels/${encodeURIComponent(dmChannel)}`);
            return;
          }
          if (started) {
            navigate(`/agents/${encodeURIComponent(name)}`);
          }
        }}
      />
      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => { if (!open) setDeleteTarget(null); }}
        title={deleteTarget ? `Delete agent "${deleteTarget}"?` : 'Delete agent?'}
        description="This deletes the agent runtime, workspace state, local agent files, and comms membership. Audit logs are archived and preserved."
        confirmLabel="Delete agent"
        variant="destructive"
        onConfirm={handleDeleteAgent}
      />
    </div>
  );
}
