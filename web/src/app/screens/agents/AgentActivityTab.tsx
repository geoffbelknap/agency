import { useState, useMemo, type ReactNode } from 'react';
import { Link } from 'react-router';
import { Send, RefreshCw, ChevronDown, ChevronRight } from 'lucide-react';
import { type RawAuditEntry } from '../../lib/api';

interface Props {
  agentName: string;
  logs: RawAuditEntry[];
  refreshingLogs: boolean;
  refreshLogs: (name: string) => Promise<void>;
  handleSendDM: (agentName: string, dmText: string) => Promise<boolean>;
}

function Card({ children }: { children: ReactNode }) {
  return (
    <div style={{ background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 10, padding: 20 }}>
      {children}
    </div>
  );
}

function SmallButton({ children, onClick, disabled = false, primary = false, ariaLabel }: { children: ReactNode; onClick?: () => void; disabled?: boolean; primary?: boolean; ariaLabel?: string }) {
  return (
    <button
      type="button"
      aria-label={ariaLabel}
      disabled={disabled}
      onClick={onClick}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        border: primary ? '0.5px solid var(--ink)' : '0.5px solid var(--ink-hairline-strong)',
        background: primary ? 'var(--ink)' : 'var(--warm)',
        color: primary ? 'var(--warm)' : 'var(--ink)',
        fontFamily: 'var(--font-sans)',
        fontSize: 12,
        padding: '5px 10px',
        borderRadius: 999,
        cursor: disabled ? 'default' : 'pointer',
        opacity: disabled ? 0.5 : 1,
      }}
    >
      {children}
    </button>
  );
}

function DmSection({ agentName, handleSendDM }: { agentName: string; handleSendDM: (name: string, text: string) => Promise<boolean> }) {
  const [dmText, setDmText] = useState('');

  return (
    <Card>
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12 }}>
        <div className="eyebrow">Send task via DM</div>
        <Link to={`/channels/dm-${agentName}`} className="font-mono" style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--teal-dark)', textDecoration: 'none' }}>
          open conversation
        </Link>
      </div>
      <textarea
        value={dmText}
        onChange={(e) => setDmText(e.target.value)}
        placeholder="Describe the task..."
        style={{
          width: '100%',
          minHeight: 112,
          resize: 'vertical',
          border: '0.5px solid var(--ink-hairline)',
          borderRadius: 8,
          background: 'var(--warm)',
          color: 'var(--ink)',
          outline: 0,
          padding: 12,
          fontFamily: 'var(--font-sans)',
          fontSize: 13,
          lineHeight: 1.5,
        }}
      />
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 12 }}>
        <SmallButton primary disabled={!dmText.trim()} onClick={async () => { const ok = await handleSendDM(agentName, dmText); if (ok) setDmText(''); }}>
          <Send size={13} />
          Send to DM
        </SmallButton>
        <span style={{ fontSize: 12, color: 'var(--ink-faint)' }}>Routes through the agent DM channel.</span>
      </div>
    </Card>
  );
}

function eventDetail(e: RawAuditEntry): string {
  return e.task_content || e.reason || e.capability || e.error || e.phase_name || e.detail || e.source || '';
}

function LogsSection({ agentName, logs, refreshingLogs, refreshLogs }: {
  agentName: string;
  logs: RawAuditEntry[];
  refreshingLogs: boolean;
  refreshLogs: (name: string) => Promise<void>;
}) {
  const [expandedLog, setExpandedLog] = useState<number | null>(null);
  const reversedLogs = useMemo(() => logs.slice().reverse(), [logs]);

  return (
    <Card>
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12 }}>
        <div className="eyebrow">Audit log</div>
        <span className="font-mono" style={{ marginLeft: 'auto', marginRight: 8, fontSize: 10, color: 'var(--ink-faint)' }}>{logs.length} events</span>
        <SmallButton ariaLabel={refreshingLogs ? 'Refreshing logs' : 'Refresh logs'} disabled={refreshingLogs} onClick={() => void refreshLogs(agentName)}>
          <RefreshCw size={13} className={refreshingLogs ? 'animate-spin' : ''} />
          Refresh
        </SmallButton>
      </div>
      <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 8, overflow: 'hidden', background: 'var(--warm)' }}>
        {logs.length === 0 ? (
          <div style={{ padding: 16, fontSize: 13, color: 'var(--ink-faint)' }}>No audit logs yet.</div>
        ) : (
          reversedLogs.map((e, i) => {
            const isExpanded = expandedLog === i;
            const detail = eventDetail(e);
            return (
              <div key={i} style={{ borderTop: i === 0 ? 0 : '0.5px solid var(--ink-hairline)' }}>
                <button
                  type="button"
                  onClick={() => detail && setExpandedLog(isExpanded ? null : i)}
                  style={{
                    display: 'grid',
                    gridTemplateColumns: '18px 76px 132px minmax(0, 1fr)',
                    gap: 12,
                    alignItems: 'baseline',
                    width: '100%',
                    border: 0,
                    background: 'transparent',
                    padding: '10px 12px',
                    textAlign: 'left',
                    cursor: detail ? 'pointer' : 'default',
                    fontFamily: 'var(--font-mono)',
                    fontSize: 11,
                    color: 'var(--ink-mid)',
                  }}
                >
                  {detail ? (isExpanded ? <ChevronDown size={13} /> : <ChevronRight size={13} />) : <span />}
                  <span style={{ color: 'var(--ink-faint)' }}>{(e.timestamp || e.ts || '').slice(11, 19)}</span>
                  <span style={{ color: 'var(--ink)' }}>{e.event || e.type || 'event'}</span>
                  <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{detail}</span>
                </button>
                {isExpanded && detail && (
                  <div style={{ padding: '0 12px 12px 118px', fontSize: 12, color: e.error ? 'var(--red)' : 'var(--ink-mid)', lineHeight: 1.5 }}>
                    {detail}
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>
    </Card>
  );
}

export function AgentActivityTab({ agentName, logs, refreshingLogs, refreshLogs, handleSendDM }: Props) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <DmSection agentName={agentName} handleSendDM={handleSendDM} />
      <LogsSection agentName={agentName} logs={logs} refreshingLogs={refreshingLogs} refreshLogs={refreshLogs} />
    </div>
  );
}

export { LogsSection };
