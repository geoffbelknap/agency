import { type ReactNode } from 'react';
import { useNavigate } from 'react-router';
import { Brain, DatabaseZap, Hash, RefreshCw } from 'lucide-react';
import { type RawChannel, type RawEconomicsResponse, type RawMeeseeks } from '../../lib/api';
import { formatDateTimeShort } from '../../lib/time';
import { featureEnabled } from '../../lib/features';

type OperationsSubTab = 'channels' | 'knowledge' | 'meeseeks' | 'economics';

const ALL_OPERATIONS_TABS: { id: OperationsSubTab; label: string; feature?: 'meeseeks' }[] = [
  { id: 'channels', label: 'Channels' },
  { id: 'knowledge', label: 'Knowledge' },
  { id: 'meeseeks', label: 'Meeseeks', feature: 'meeseeks' },
  { id: 'economics', label: 'Economics' },
];

interface Props {
  agentName: string;
  channels: RawChannel[];
  knowledge: Record<string, any>[];
  meeseeksList: RawMeeseeks[];
  economics: RawEconomicsResponse | null;
  refreshMeeseeks: (agentName: string) => Promise<void>;
  handleKillMeeseeks: (agentName: string, meeseeksId: string) => Promise<void>;
  handleKillAllMeeseeks: (agentName: string) => Promise<void>;
  handleClearCache: (agentName: string) => Promise<void>;
  subTab: OperationsSubTab;
  onSubTabChange: (tab: OperationsSubTab) => void;
}

const cardStyle = { background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 10, padding: 20 } as const;
const rowStyle = { display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) auto', gap: 12, padding: 14, borderTop: '0.5px solid var(--ink-hairline)', alignItems: 'center' } as const;

function SmallButton({ children, onClick, disabled = false, danger = false }: { children: ReactNode; onClick?: () => void; disabled?: boolean; danger?: boolean }) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      style={{ display: 'inline-flex', alignItems: 'center', gap: 6, border: '0.5px solid var(--ink-hairline-strong)', background: 'var(--warm)', color: danger ? 'var(--red)' : 'var(--ink)', fontFamily: 'var(--font-sans)', fontSize: 12, padding: '5px 10px', borderRadius: 999, cursor: disabled ? 'default' : 'pointer', opacity: disabled ? 0.5 : 1 }}
    >
      {children}
    </button>
  );
}

function StateBadge({ children, tone = 'neutral' }: { children: ReactNode; tone?: 'neutral' | 'active' | 'warn' | 'danger' }) {
  const colors = {
    neutral: { bg: 'var(--warm-3)', color: 'var(--ink-mid)' },
    active: { bg: 'var(--teal-tint)', color: 'var(--teal-dark)' },
    warn: { bg: 'var(--amber-tint)', color: '#8B5A00' },
    danger: { bg: 'var(--red-tint)', color: 'var(--red)' },
  }[tone];
  return <span className="font-mono" style={{ fontSize: 10, padding: '2px 7px', borderRadius: 4, background: colors.bg, color: colors.color }}>{children}</span>;
}

function PanelHeader({ title, meta, action }: { title: string; meta?: string; action?: ReactNode }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12, gap: 10 }}>
      <div className="eyebrow">{title}</div>
      {meta && <span className="font-mono" style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--ink-faint)' }}>{meta}</span>}
      {action}
    </div>
  );
}

function ChannelsContent({ channels }: { channels: RawChannel[] }) {
  const navigate = useNavigate();
  return (
    <div style={cardStyle}>
      <PanelHeader title="Channels" meta={`${channels.length} joined`} />
      <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 8, overflow: 'hidden', background: 'var(--warm)' }}>
        {channels.length === 0 ? (
          <div style={{ padding: 16, fontSize: 13, color: 'var(--ink-faint)' }}>Not a member of any channels.</div>
        ) : channels.map((ch: any, index) => (
          <button key={ch.name} type="button" onClick={() => navigate(`/channels/${ch.name}`)} style={{ ...rowStyle, borderTop: index === 0 ? 0 : rowStyle.borderTop, width: '100%', borderLeft: 0, borderRight: 0, borderBottom: 0, background: 'transparent', textAlign: 'left', cursor: 'pointer', fontFamily: 'var(--font-sans)' }}>
            <span style={{ minWidth: 0 }}>
              <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <Hash size={13} style={{ color: 'var(--ink-mid)' }} />
                <span className="font-mono" style={{ fontSize: 13, color: 'var(--ink)' }}>{ch.name}</span>
                <StateBadge tone={ch.state === 'active' || !ch.state ? 'active' : 'neutral'}>{ch.state || 'active'}</StateBadge>
              </span>
              {ch.topic && <span style={{ display: 'block', marginTop: 4, fontSize: 12, color: 'var(--ink-mid)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{ch.topic}</span>}
            </span>
            <span className="font-mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>{(ch.members || []).length} members · {ch.type || 'standard'}</span>
          </button>
        ))}
      </div>
    </div>
  );
}

function KnowledgeContent({ agentName, knowledge, handleClearCache }: { agentName: string; knowledge: Record<string, any>[]; handleClearCache: (agentName: string) => Promise<void> }) {
  return (
    <div style={cardStyle}>
      <PanelHeader title="Knowledge contributions" meta={`${knowledge.length} nodes`} action={<SmallButton onClick={() => void handleClearCache(agentName)}><DatabaseZap size={13} />Clear cache</SmallButton>} />
      <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 8, overflow: 'hidden', background: 'var(--warm)' }}>
        {knowledge.length === 0 ? (
          <div style={{ padding: 16, fontSize: 13, color: 'var(--ink-faint)' }}>No knowledge contributions found.</div>
        ) : knowledge.slice(0, 20).map((node: any, index: number) => (
          <div key={node.id || index} style={{ ...rowStyle, borderTop: index === 0 ? 0 : rowStyle.borderTop }}>
            <div style={{ minWidth: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <Brain size={13} style={{ color: 'var(--teal-dark)' }} />
                <span style={{ fontSize: 13, color: 'var(--ink)' }}>{node.label || node.topic || node.id || 'knowledge node'}</span>
                {node.confidence != null && <StateBadge>{Math.round(node.confidence * 100)}%</StateBadge>}
              </div>
              {(node.summary || node.content) && <div style={{ marginTop: 4, fontSize: 12, color: 'var(--ink-mid)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{node.summary || node.content}</div>}
            </div>
            {node.timestamp && <span className="font-mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>{formatDateTimeShort(node.timestamp)}</span>}
          </div>
        ))}
      </div>
    </div>
  );
}

function fmtCurrency(value?: number) { return `$${(value ?? 0).toFixed(4)}`; }
function fmtNumber(value?: number) { return (value ?? 0).toLocaleString(); }
function fmtPercent(value?: number) { return `${Math.round((value ?? 0) * 100)}%`; }

function MetricCard({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ background: 'var(--warm)', border: '0.5px solid var(--ink-hairline)', borderRadius: 8, padding: 12 }}>
      <div className="eyebrow" style={{ fontSize: 9, marginBottom: 6 }}>{label}</div>
      <div className="font-mono" style={{ fontSize: 13, color: 'var(--ink)' }}>{value}</div>
    </div>
  );
}

function EconomicsContent({ economics }: { economics: RawEconomicsResponse | null }) {
  if (!economics) {
    return <div style={cardStyle}><PanelHeader title="Economics" /><div style={{ fontSize: 13, color: 'var(--ink-faint)' }}>No economics metrics available for this agent yet.</div></div>;
  }
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div style={cardStyle}>
        <PanelHeader title="Today" meta={economics.period ?? 'Current UTC day'} />
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))', gap: 10 }}>
          <MetricCard label="Cost" value={fmtCurrency(economics.total_cost_usd)} />
          <MetricCard label="Requests" value={fmtNumber(economics.requests)} />
          <MetricCard label="Input tokens" value={fmtNumber(economics.input_tokens)} />
          <MetricCard label="Output tokens" value={fmtNumber(economics.output_tokens)} />
          <MetricCard label="Cache hits" value={fmtNumber(economics.cache_hits)} />
          <MetricCard label="Cache hit rate" value={fmtPercent(economics.cache_hit_rate)} />
          <MetricCard label="Retry waste" value={fmtCurrency(economics.retry_waste_usd)} />
          <MetricCard label="Tool hallucination" value={fmtPercent(economics.tool_hallucination_rate)} />
        </div>
      </div>
      {economics.by_model && Object.keys(economics.by_model).length > 0 && (
        <div style={cardStyle}>
          <PanelHeader title="By model" />
          <pre style={{ background: 'var(--warm)', border: '0.5px solid var(--ink-hairline)', borderRadius: 8, color: 'var(--ink-mid)', fontSize: 11, padding: 12, overflowX: 'auto', whiteSpace: 'pre-wrap' }}>{JSON.stringify(economics.by_model, null, 2)}</pre>
        </div>
      )}
    </div>
  );
}

function MeeseeksContent({ agentName, meeseeksList, refreshMeeseeks, handleKillMeeseeks, handleKillAllMeeseeks }: { agentName: string; meeseeksList: RawMeeseeks[]; refreshMeeseeks: (agentName: string) => Promise<void>; handleKillMeeseeks: (agentName: string, meeseeksId: string) => Promise<void>; handleKillAllMeeseeks: (agentName: string) => Promise<void> }) {
  return (
    <div style={cardStyle}>
      <PanelHeader title="Meeseeks" meta={`${meeseeksList.length} active`} action={<><SmallButton onClick={() => void refreshMeeseeks(agentName)}><RefreshCw size={13} />Refresh</SmallButton><SmallButton danger disabled={meeseeksList.length === 0} onClick={() => void handleKillAllMeeseeks(agentName)}>Kill all</SmallButton></>} />
      <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 8, overflow: 'hidden', background: 'var(--warm)' }}>
        {meeseeksList.length === 0 ? (
          <div style={{ padding: 16, fontSize: 13, color: 'var(--ink-faint)' }}>No active meeseeks.</div>
        ) : meeseeksList.map((m, index) => (
          <div key={m.id} style={{ ...rowStyle, borderTop: index === 0 ? 0 : rowStyle.borderTop }}>
            <div style={{ minWidth: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span className="font-mono" style={{ fontSize: 12, color: 'var(--ink)' }}>{m.id}</span>
                <StateBadge tone={m.status === 'distressed' ? 'danger' : m.status === 'working' ? 'active' : 'neutral'}>{m.status}</StateBadge>
                {m.orphaned && <StateBadge tone="warn">orphaned</StateBadge>}
              </div>
              <div style={{ marginTop: 4, fontSize: 12, color: 'var(--ink-mid)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{m.task}</div>
            </div>
            {m.status !== 'completed' && m.status !== 'terminated' && <SmallButton danger onClick={() => void handleKillMeeseeks(agentName, m.id)}>Kill</SmallButton>}
          </div>
        ))}
      </div>
    </div>
  );
}

export function AgentOperationsTab({ agentName, channels, knowledge, meeseeksList, economics, refreshMeeseeks, handleKillMeeseeks, handleKillAllMeeseeks, handleClearCache, subTab, onSubTabChange }: Props) {
  const operationsTabs = ALL_OPERATIONS_TABS.filter((tab) => !tab.feature || featureEnabled(tab.feature));

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div role="tablist" style={{ display: 'flex', gap: 18, borderBottom: '0.5px solid var(--ink-hairline)' }}>
        {operationsTabs.map((tab) => (
          <button key={tab.id} type="button" role="tab" aria-selected={subTab === tab.id} aria-controls={`ops-panel-${tab.id}`} onClick={() => onSubTabChange(tab.id)} style={{ background: 'transparent', border: 0, borderBottom: subTab === tab.id ? '1.5px solid var(--teal)' : '1.5px solid transparent', color: subTab === tab.id ? 'var(--ink)' : 'var(--ink-mid)', cursor: 'pointer', fontFamily: 'var(--font-sans)', fontSize: 12, marginBottom: -0.5, padding: '8px 0' }}>
            {tab.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`ops-panel-${subTab}`}>
        {subTab === 'channels' && <ChannelsContent channels={channels} />}
        {subTab === 'knowledge' && <KnowledgeContent agentName={agentName} knowledge={knowledge} handleClearCache={handleClearCache} />}
        {featureEnabled('meeseeks') && subTab === 'meeseeks' && <MeeseeksContent agentName={agentName} meeseeksList={meeseeksList} refreshMeeseeks={refreshMeeseeks} handleKillMeeseeks={handleKillMeeseeks} handleKillAllMeeseeks={handleKillAllMeeseeks} />}
        {subTab === 'economics' && <EconomicsContent economics={economics} />}
      </div>
    </div>
  );
}
