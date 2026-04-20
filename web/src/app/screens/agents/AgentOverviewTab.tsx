import { type ReactNode } from 'react';
import { Agent } from '../../types';
import { type RawAuditEntry } from '../../lib/api';

interface Props {
  agent: Agent;
  logs: RawAuditEntry[];
}

function Card({ children }: { children: ReactNode }) {
  return (
    <div style={{ background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 10, padding: 20 }}>
      {children}
    </div>
  );
}

function eventTimestamp(entry: RawAuditEntry): string {
  const raw = entry.timestamp || entry.ts || '';
  if (!raw) return '--:--:--';
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return raw.slice(11, 19) || raw;
  return date.toLocaleTimeString([], { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function eventDetail(entry: RawAuditEntry): string {
  return entry.detail
    || entry.task_content
    || entry.capability
    || entry.provider_tool_capability
    || entry.error
    || entry.reason
    || entry.source
    || '';
}

export function AgentOverviewTab({ agent, logs }: Props) {
  const recentEvents = logs
    .slice(-6)
    .reverse()
    .map((entry) => ({
      ts: eventTimestamp(entry),
      event: entry.event || entry.type || 'event',
      detail: eventDetail(entry),
    }));

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Card>
        <div style={{ display: 'flex', alignItems: 'center', marginBottom: 10 }}>
          <div className="eyebrow">Current task</div>
          {agent.currentTask && (
            <span className="font-mono" style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--teal-dark)', display: 'inline-flex', alignItems: 'center', gap: 6 }}>
              <span style={{ position: 'relative', width: 6, height: 6, borderRadius: '50%', background: 'var(--teal)' }}>
                <span style={{ position: 'absolute', inset: 0, borderRadius: '50%', background: 'var(--teal)', animation: 'agencyPulse 1.8s ease-out infinite' }} />
              </span>
              in progress
            </span>
          )}
        </div>
        <div style={{ fontSize: 15, color: 'var(--ink)', lineHeight: 1.55, maxWidth: 680 }}>
          {agent.currentTask ? agent.currentTask.content : 'No active task reported.'}
        </div>
      </Card>

      <Card>
        <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12 }}>
          <div className="eyebrow">Recent events</div>
          <span className="font-mono" style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--ink-faint)' }}>last 15 min</span>
        </div>
        <div style={{ fontSize: 12, display: 'flex', flexDirection: 'column' }}>
          {recentEvents.length === 0 && (
            <div style={{ fontSize: 13, color: 'var(--ink-faint)', padding: '4px 0' }}>
              No recent events reported.
            </div>
          )}
          {recentEvents.map((event, index) => (
            <div key={`${event.ts}-${event.event}-${index}`} style={{ display: 'grid', gridTemplateColumns: '72px 110px 1fr', gap: 12, padding: '8px 0', borderTop: index === 0 ? 'none' : '0.5px solid var(--ink-hairline)', alignItems: 'baseline' }}>
              <span className="font-mono" style={{ fontSize: 11, color: 'var(--ink-faint)' }}>{event.ts}</span>
              <span className="font-mono" style={{ fontSize: 11, color: 'var(--ink-mid)' }}>{event.event}</span>
              <span style={{ fontSize: 13, color: 'var(--ink)', minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{event.detail}</span>
            </div>
          ))}
        </div>
      </Card>
    </div>
  );
}
