import { useState } from 'react';
import { Bot, Search } from 'lucide-react';

interface AgentRow {
  name: string;
  status: string;
  role?: string;
  mission?: string;
  lastActive: string;
  budget?: { daily_used: number; daily_limit: number };
}

interface Props {
  agents: AgentRow[];
  selectedAgent: string | null;
  onSelect: (name: string) => void;
}

const STATUS_DOT_COLOR: Record<string, string> = {
  running: 'var(--teal)',
  stopped: 'var(--red)',
  paused: 'var(--amber)',
  halted: 'var(--amber)',
  idle: 'var(--ink-faint)',
  unhealthy: 'var(--red)',
};

function statusDotColor(status: string): string {
  return STATUS_DOT_COLOR[status] ?? 'var(--ink-faint)';
}

function budgetColor(used: number, limit: number): string {
  if (!limit) return 'var(--ink-faint)';
  const pct = used / limit;
  if (pct >= 0.9) return 'var(--red)';
  if (pct >= 0.75) return 'var(--amber)';
  return 'var(--teal)';
}

function relativeTime(iso: string): string {
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

export function AgentList({ agents, selectedAgent, onSelect }: Props) {
  const [query, setQuery] = useState('');
  const filteredAgents = agents.filter((agent) => {
    const q = query.trim().toLowerCase();
    if (!q) return true;
    return [agent.name, agent.status, agent.role, agent.mission]
      .filter(Boolean)
      .some((value) => String(value).toLowerCase().includes(q));
  });

  return (
    <div>
      <div style={{ padding: '12px 20px', borderBottom: '0.5px solid var(--ink-hairline)', display: 'flex', alignItems: 'center', gap: 10 }}>
        <Search size={13} style={{ color: 'var(--ink-faint)' }} aria-hidden="true" />
        <input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Filter agents..."
          style={{ flex: 1, background: 'transparent', border: 0, outline: 0, fontFamily: 'var(--font-sans)', fontSize: 13, color: 'var(--ink)' }}
        />
        <span className="font-mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>{filteredAgents.length}</span>
      </div>

      {filteredAgents.map((agent) => {
        const selected = agent.name === selectedAgent;
        const budgetPct = agent.budget && agent.budget.daily_limit > 0
          ? Math.min(100, (agent.budget.daily_used / agent.budget.daily_limit) * 100)
          : 0;
        const role = agent.role || agent.mission || 'no mission';
        return (
          <button
            key={agent.name}
            type="button"
            onClick={() => onSelect(agent.name)}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 12,
              padding: '14px 20px',
              width: '100%',
              border: 0,
              borderBottom: '0.5px solid var(--ink-hairline)',
              background: selected ? 'var(--warm)' : 'transparent',
              borderLeft: selected ? '2px solid var(--teal)' : '2px solid transparent',
              cursor: 'pointer',
              textAlign: 'left',
              fontFamily: 'var(--font-sans)',
            }}
          >
            <div style={{ position: 'relative', flexShrink: 0 }}>
              <div style={{ width: 36, height: 36, borderRadius: 8, background: 'var(--warm-3)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                <Bot size={16} style={{ color: 'var(--ink-mid)' }} aria-hidden="true" />
              </div>
              <span
                aria-hidden="true"
                style={{
                  position: 'absolute',
                  bottom: -2,
                  right: -2,
                  width: 10,
                  height: 10,
                  borderRadius: '50%',
                  background: statusDotColor(agent.status),
                  border: '2px solid var(--warm-2)',
                }}
              />
            </div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ display: 'flex', alignItems: 'baseline', gap: 6 }}>
                <span className="font-mono" style={{ fontSize: 13, color: 'var(--ink)' }}>{agent.name}</span>
                <span className="font-mono" style={{ fontSize: 11, color: 'var(--ink-faint)', marginLeft: 'auto' }}>{relativeTime(agent.lastActive)}</span>
              </div>
              <div style={{ fontSize: 12, color: 'var(--ink-mid)', marginTop: 2, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{role}</div>
              <div style={{ marginTop: 6, display: 'flex', alignItems: 'center', gap: 8 }}>
                <div style={{ flex: 1, height: 3, background: 'var(--warm-3)', borderRadius: 2, overflow: 'hidden' }}>
                  <div
                    data-budget-bar
                    style={{ width: `${budgetPct}%`, height: '100%', background: agent.budget ? budgetColor(agent.budget.daily_used, agent.budget.daily_limit) : 'var(--ink-faint)' }}
                  />
                </div>
                <span className="font-mono" style={{ fontSize: 9, color: 'var(--ink-faint)' }}>
                  {agent.budget ? `$${agent.budget.daily_used.toFixed(2)}/$${agent.budget.daily_limit.toFixed(2)}` : '-'}
                </span>
              </div>
            </div>
          </button>
        );
      })}
      {filteredAgents.length === 0 && (
        <div style={{ padding: '32px 20px', textAlign: 'center', fontSize: 13, color: 'var(--ink-faint)' }}>
          No agents match this filter.
        </div>
      )}
    </div>
  );
}
