import { useState, useEffect, type ReactNode } from 'react';
import { Bot, MessageSquare, Play, Pause, Loader2 } from 'lucide-react';
import { Agent } from '../../types';
import { type RawCapability } from '../../lib/api';
import { useAgentDetailData } from './useAgentDetailData';
import { AgentOverviewTab } from './AgentOverviewTab';
import { AgentActivityTab } from './AgentActivityTab';
import { AgentOperationsTab } from './AgentOperationsTab';
import { AgentSystemTab } from './AgentSystemTab';

type PrimaryTab = 'overview' | 'activity' | 'memory' | 'operations' | 'system';
type OperationsSubTab = 'channels' | 'knowledge' | 'meeseeks' | 'economics';
type SystemSubTab = 'config' | 'logs';

interface Props {
  agent: Agent;
  capabilities: RawCapability[];
  onAction: (name: string, action: string) => Promise<void>;
  actionLoading: string | null;
  onRefreshAgents: () => void;
  onRequestDelete: (name: string) => void;
}

const STATUS_DOT: Record<string, string> = {
  running: 'var(--teal)',
  active: 'var(--teal)',
  halted: 'var(--amber)',
  paused: 'var(--amber)',
  stopped: 'var(--red)',
  unhealthy: 'var(--red)',
  idle: 'var(--ink-faint)',
};

function StatusDot({ status, label = false, pulse = false, size = 8 }: { status: string; label?: boolean; pulse?: boolean; size?: number }) {
  const color = STATUS_DOT[status] ?? 'var(--ink-faint)';
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, whiteSpace: 'nowrap' }}>
      <span style={{ position: 'relative', width: size, height: size, borderRadius: '50%', background: color, flexShrink: 0 }}>
        {pulse && <span style={{ position: 'absolute', inset: 0, borderRadius: '50%', background: color, animation: 'agencyPulse 1.8s ease-out infinite' }} />}
      </span>
      {label && <span className="font-mono" style={{ fontSize: 11, color: 'var(--ink-mid)', textTransform: 'lowercase' }}>{status}</span>}
    </span>
  );
}

function Btn({ children, icon, variant = 'default', disabled = false, onClick, ariaLabel }: {
  children?: ReactNode;
  icon?: ReactNode;
  variant?: 'default' | 'primary' | 'ghost';
  disabled?: boolean;
  onClick?: () => void;
  ariaLabel?: string;
}) {
  const variants = {
    default: { bg: 'var(--warm)', color: 'var(--ink)', border: '0.5px solid var(--ink-hairline-strong)' },
    primary: { bg: 'var(--ink)', color: 'var(--warm)', border: '0.5px solid var(--ink)' },
    ghost: { bg: 'transparent', color: 'var(--ink-mid)', border: '0.5px solid transparent' },
  }[variant];
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      aria-label={ariaLabel}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        padding: children ? '5px 10px' : 5,
        minWidth: children ? undefined : 30,
        minHeight: 30,
        justifyContent: 'center',
        fontSize: 12,
        fontWeight: 400,
        fontFamily: 'var(--font-sans)',
        cursor: disabled ? 'default' : 'pointer',
        background: variants.bg,
        color: variants.color,
        border: variants.border,
        borderRadius: 999,
        opacity: disabled ? 0.5 : 1,
      }}
    >
      {icon}
      {children}
    </button>
  );
}

function Badge({ children }: { children: ReactNode }) {
  return (
    <span className="font-mono" style={{ display: 'inline-flex', alignItems: 'center', padding: '2px 8px', fontSize: 10, letterSpacing: '0.08em', textTransform: 'uppercase', background: 'var(--warm-3)', color: 'var(--ink-mid)', border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 4 }}>
      {children}
    </span>
  );
}

function StatCell({ label, value, sub, accent = false }: { label: string; value: ReactNode; sub?: ReactNode; accent?: boolean }) {
  return (
    <div style={{ padding: '14px 18px', borderRight: '0.5px solid var(--ink-hairline)', background: accent ? 'var(--red-tint)' : 'transparent', minWidth: 0 }}>
      <div className="eyebrow" style={{ fontSize: 9, marginBottom: 6 }}>{label}</div>
      <div style={{ display: 'flex', alignItems: 'baseline', flexWrap: 'wrap' }}>{value}</div>
      {sub && <div>{sub}</div>}
    </div>
  );
}

export function AgentDetail({ agent, capabilities: initialCapabilities, onAction, actionLoading, onRefreshAgents, onRequestDelete }: Props) {
  const [primaryTab, setPrimaryTab] = useState<PrimaryTab>('overview');
  const [opsSubTab, setOpsSubTab] = useState<OperationsSubTab>('channels');
  const [sysSubTab, setSysSubTab] = useState<SystemSubTab>('config');

  useEffect(() => {
    setPrimaryTab('overview');
    setOpsSubTab('channels');
    setSysSubTab('config');
  }, [agent.name]);

  const effectiveDataTab = primaryTab === 'memory' ? 'knowledge'
    : primaryTab === 'operations' ? opsSubTab
    : primaryTab === 'system' ? sysSubTab
    : primaryTab;

  const data = useAgentDetailData(agent.name, initialCapabilities, effectiveDataTab, onRefreshAgents);
  const dailyUsed = data.budget?.daily_used ?? 0;
  const dailyLimit = data.budget?.daily_limit ?? 0;
  const dailyPct = dailyLimit > 0 ? Math.min(100, (dailyUsed / dailyLimit) * 100) : 0;
  const budgetColor = dailyPct > 90 ? 'var(--red)' : dailyPct > 75 ? 'var(--amber)' : 'var(--teal)';
  const visibleCapabilities = Array.from(new Set(agent.grantedCapabilities || [])).slice(0, 5);
  const roleLine = [agent.role || agent.preset || agent.type, agent.mission].filter(Boolean).join(' · ');
  const displayName = agent.name.length > 1 ? `${agent.name[0]}\u200b${agent.name.slice(1)}` : agent.name;

  return (
    <div className="scrollbar-none" style={{ height: '100%', overflowY: 'auto', overflowX: 'hidden', padding: '24px 32px', minWidth: 0, minHeight: 0, background: 'var(--warm)' }}>
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 16, marginBottom: 20 }}>
        <div style={{ width: 56, height: 56, borderRadius: 12, background: 'var(--warm-3)', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}>
          <Bot size={26} style={{ color: 'var(--ink-mid)' }} aria-hidden="true" />
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, flexWrap: 'wrap' }}>
            <h2 style={{ margin: 0, fontFamily: 'var(--font-display)', fontSize: 32, fontWeight: 300, lineHeight: 1.05, letterSpacing: '-0.02em', color: 'var(--ink)' }}>{displayName}</h2>
            <StatusDot status={agent.status} label pulse={agent.status === 'running'} />
          </div>
          <p style={{ margin: '4px 0 0', color: 'var(--ink-mid)', fontSize: 13 }}>
            {roleLine || 'no mission'}
          </p>
        </div>

        {agent.status !== 'running' && agent.status !== 'halted' && agent.status !== 'unhealthy' && (
          <Btn
            disabled={!!actionLoading}
            onClick={() => onAction(agent.name, 'start')}
            icon={actionLoading === `${agent.name}-start` ? <Loader2 size={13} className="animate-spin" /> : <Play size={13} />}
          >
            {actionLoading === `${agent.name}-start` ? 'Starting...' : 'Start'}
          </Btn>
        )}
        {agent.status === 'halted' && (
          <Btn
            disabled={!!actionLoading}
            onClick={() => onAction(agent.name, 'resume')}
            icon={actionLoading === `${agent.name}-resume` ? <Loader2 size={13} className="animate-spin" /> : <Play size={13} />}
          >
            {actionLoading === `${agent.name}-resume` ? 'Resuming...' : 'Resume'}
          </Btn>
        )}
        {(agent.status === 'running' || agent.status === 'unhealthy') && (
          <Btn
            disabled={!!actionLoading}
            onClick={() => onAction(agent.name, 'pause')}
            icon={actionLoading === `${agent.name}-pause` ? <Loader2 size={13} className="animate-spin" /> : <Pause size={13} />}
          >
            {actionLoading === `${agent.name}-pause` ? 'Pausing...' : 'Pause'}
          </Btn>
        )}
        <Btn variant="primary" icon={<MessageSquare size={13} />} onClick={() => void data.handleOpenDM(agent.name)}>Open DM</Btn>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(150px, 1fr) minmax(190px, 1.15fr) minmax(210px, 1.5fr)', gap: 0, background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 10, marginBottom: 24, overflow: 'hidden' }}>
        <StatCell
          label="Budget (24h)"
          accent={dailyPct > 90}
          value={<><span style={{ fontFamily: 'var(--font-display)', fontSize: 22, fontWeight: 300, letterSpacing: '-0.02em', color: dailyPct > 90 ? 'var(--red)' : 'var(--ink)' }}>${dailyUsed.toFixed(2)}</span>{dailyLimit > 0 && <span className="font-mono" style={{ fontSize: 11, color: 'var(--ink-faint)', marginLeft: 6 }}>/ ${dailyLimit.toFixed(2)}</span>}</>}
          sub={<div style={{ marginTop: 6, display: 'flex', alignItems: 'center', gap: 8 }}><div style={{ flex: 1, height: 3, background: 'var(--warm-3)', borderRadius: 2, overflow: 'hidden' }}><div style={{ width: `${dailyPct}%`, height: '100%', background: budgetColor, borderRadius: 2 }} /></div><span className="font-mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>{dailyPct.toFixed(0)}%</span></div>}
        />
        <StatCell
          label="Model"
          value={<span className="font-mono" style={{ fontSize: 13, color: 'var(--ink)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{agent.model || 'default provider'}</span>}
          sub={<span className="font-mono" style={{ fontSize: 10, color: 'var(--ink-faint)', whiteSpace: 'nowrap' }}>via provider · trust: {agent.trustLevel ? agent.trustLevel : 'full'}</span>}
        />
        <StatCell
          label="Capabilities"
          value={<div style={{ display: 'flex', flexWrap: 'wrap', gap: 3, marginTop: 2 }}>{(visibleCapabilities.length > 0 ? visibleCapabilities : ['fs.read', 'http.get']).map((cap) => <Badge key={cap}>{cap}</Badge>)}</div>}
        />
      </div>

      <div role="tablist" style={{ display: 'flex', gap: 22, borderBottom: '0.5px solid var(--ink-hairline)', marginBottom: 20, overflowX: 'auto' }}>
        {([
          { id: 'overview' as PrimaryTab, label: 'Overview' },
          { id: 'activity' as PrimaryTab, label: 'Activity' },
          { id: 'memory' as PrimaryTab, label: 'Memory' },
          { id: 'operations' as PrimaryTab, label: 'Operations' },
          { id: 'system' as PrimaryTab, label: 'System' },
        ]).map((tab) => (
          <button
            key={tab.id}
            role="tab"
            aria-selected={primaryTab === tab.id}
            aria-controls={`panel-${tab.id}`}
            onClick={() => setPrimaryTab(tab.id)}
            style={{
              background: 'transparent',
              border: 0,
              padding: '10px 0',
              fontFamily: 'var(--font-sans)',
              fontSize: 13,
              cursor: 'pointer',
              color: primaryTab === tab.id ? 'var(--ink)' : 'var(--ink-mid)',
              borderBottom: primaryTab === tab.id ? '1.5px solid var(--teal)' : '1.5px solid transparent',
              marginBottom: -0.5,
              whiteSpace: 'nowrap',
            }}
          >
            {tab.label}
          </button>
        ))}
      </div>

      <div role="tabpanel" id={`panel-${primaryTab}`}>
        {primaryTab === 'overview' && <AgentOverviewTab agent={agent} logs={data.logs} />}
        {primaryTab === 'activity' && (
          <AgentActivityTab
            agentName={agent.name}
            logs={data.logs}
            refreshingLogs={data.refreshingLogs}
            refreshLogs={data.refreshLogs}
            handleSendDM={data.handleSendDM}
          />
        )}
        {primaryTab === 'memory' && <AgentMemoryTab knowledge={data.knowledge} />}
        {primaryTab === 'operations' && (
          <AgentOperationsTab
            agentName={agent.name}
            channels={data.channels}
            knowledge={data.knowledge}
            meeseeksList={data.meeseeksList}
            economics={data.economics}
            refreshMeeseeks={data.refreshMeeseeks}
            handleKillMeeseeks={data.handleKillMeeseeks}
            handleKillAllMeeseeks={data.handleKillAllMeeseeks}
            handleClearCache={data.handleClearCache}
            subTab={opsSubTab}
            onSubTabChange={setOpsSubTab}
          />
        )}
        {primaryTab === 'system' && (
          <AgentSystemTab
            agent={agent}
            agentConfig={data.agentConfig}
            capabilities={data.capabilities}
            policy={data.policy}
            capLoading={data.capLoading}
            logs={data.logs}
            refreshingLogs={data.refreshingLogs}
            refreshLogs={data.refreshLogs}
            handleGrant={data.handleGrant}
            handleRevoke={data.handleRevoke}
            handleSaveConfig={data.handleSaveConfig}
            onRequestDelete={onRequestDelete}
            subTab={sysSubTab}
            onSubTabChange={setSysSubTab}
          />
        )}
      </div>
    </div>
  );
}

function AgentMemoryTab({ knowledge }: { knowledge: Record<string, any>[] }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div style={{ background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 10, padding: 20 }}>
        <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12 }}>
          <div className="eyebrow">Memory</div>
          <span className="font-mono" style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--ink-faint)' }}>{knowledge.length} nodes</span>
        </div>
        <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 8, overflow: 'hidden', background: 'var(--warm)' }}>
          {knowledge.length === 0 ? (
            <div style={{ padding: 16, fontSize: 13, color: 'var(--ink-faint)' }}>No memory records reported for this agent yet.</div>
          ) : (
            knowledge.slice(0, 8).map((node: any, index) => (
              <div key={node.id || index} style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) auto', gap: 12, padding: 14, borderTop: index === 0 ? 0 : '0.5px solid var(--ink-hairline)', alignItems: 'center' }}>
                <div style={{ minWidth: 0 }}>
                  <div className="font-mono" style={{ fontSize: 12, color: 'var(--ink)' }}>{node.label || node.topic || node.id || 'knowledge node'}</div>
                  {(node.summary || node.content) && <div style={{ marginTop: 4, fontSize: 12, color: 'var(--ink-mid)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{node.summary || node.content}</div>}
                </div>
                {node.confidence != null && <div className="font-mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>{Math.round(node.confidence * 100)}%</div>}
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
