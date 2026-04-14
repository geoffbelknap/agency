import { useIsMobile } from '../../components/ui/use-mobile';
import { featureEnabled } from '../../lib/features';

interface AgentRow {
  name: string;
  status: string;
  team: string;
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
  running: 'bg-green-500',
  stopped: 'bg-red-500',
  paused: 'bg-amber-500',
  halted: 'bg-amber-500',
  idle: 'bg-gray-400',
};

function statusDotColor(status: string): string {
  return STATUS_DOT_COLOR[status] ?? 'bg-gray-400';
}

function budgetColor(used: number, limit: number): string {
  const pct = limit > 0 ? used / limit : 0;
  if (pct > 0.95) return 'bg-red-500';
  if (pct > 0.8) return 'bg-amber-500';
  return 'bg-primary';
}

function relativeTime(iso: string): string {
  if (!iso) return '—';
  const diff = Date.now() - new Date(iso).getTime();
  if (Number.isNaN(diff)) return '—';
  const seconds = Math.floor(diff / 1000);
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

export function AgentList({ agents, selectedAgent, onSelect }: Props) {
  const isMobile = useIsMobile();
  const showTeams = featureEnabled('teams');
  const showMissions = featureEnabled('missions');

  return isMobile ? (
    <div className="space-y-2">
      {agents.map((agent) => {
        const isSelected = agent.name === selectedAgent;
        return (
          <button
            key={agent.name}
            type="button"
            onClick={() => onSelect(agent.name)}
            className={`w-full rounded-xl border px-3 py-3 text-left transition-colors ${
              isSelected
                ? 'border-primary/40 bg-primary/5'
                : 'border-border bg-background hover:bg-muted/40'
            }`}
          >
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="truncate font-mono text-sm text-foreground">{agent.name}</div>
                <div className="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground">
                  <span className={`inline-block h-2 w-2 rounded-full ${statusDotColor(agent.status)}`} />
                  <span className="capitalize">{agent.status}</span>
                  {showTeams && agent.team && (
                    <>
                      <span className="text-muted-foreground/60">•</span>
                      <span>{agent.team}</span>
                    </>
                  )}
                </div>
              </div>
              <div className="shrink-0 text-right text-[11px] text-muted-foreground" style={{ fontVariantNumeric: 'tabular-nums' }}>
                {relativeTime(agent.lastActive)}
              </div>
            </div>
            <div className="mt-3 flex items-center justify-between gap-3">
              <div className="min-w-0 text-xs text-muted-foreground">
                {showMissions ? (
                  <>
                    <span className="text-muted-foreground/70">Mission:</span>{' '}
                    <span className="truncate">{agent.mission ?? '—'}</span>
                  </>
                ) : (
                  <>
                    <span className="text-muted-foreground/70">Recent activity:</span>{' '}
                    <span className="truncate">{relativeTime(agent.lastActive)}</span>
                  </>
                )}
              </div>
              {agent.budget ? (
                <div className="h-1.5 w-20 shrink-0 overflow-hidden rounded-full bg-muted">
                  <div
                    data-budget-bar
                    className={`h-full rounded-full ${budgetColor(agent.budget.daily_used, agent.budget.daily_limit)}`}
                    style={{
                      width: `${Math.min(100, (agent.budget.daily_used / agent.budget.daily_limit) * 100)}%`,
                    }}
                  />
                </div>
              ) : (
                <div className="text-xs text-muted-foreground">—</div>
              )}
            </div>
          </button>
        );
      })}
    </div>
  ) : (
    <table className="w-full text-sm">
      <thead>
        <tr className="border-b text-left text-muted-foreground">
          <th className="pb-2 pr-4 font-medium">Name</th>
          <th className="pb-2 pr-4 font-medium">Status</th>
          {showTeams && <th className="pb-2 pr-4 font-medium">Team</th>}
          {showMissions && <th className="pb-2 pr-4 font-medium">Mission</th>}
          <th className="pb-2 pr-4 font-medium">Budget</th>
          <th className="pb-2 font-medium">Last Active</th>
        </tr>
      </thead>
      <tbody>
        {agents.map((agent) => {
          const isSelected = agent.name === selectedAgent;
          const rowClass = [
            'cursor-pointer transition-colors hover:bg-muted/50',
            isSelected ? 'bg-primary/5' : '',
          ]
            .filter(Boolean)
            .join(' ');

          return (
            <tr
              key={agent.name}
              className={rowClass}
              onClick={() => onSelect(agent.name)}
              tabIndex={0}
              role="button"
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onSelect(agent.name);
                }
              }}
            >
              <td className="py-2 pr-4 font-mono">{agent.name}</td>
              <td className="py-2 pr-4">
                <span className="flex items-center gap-1.5">
                  <span className={`inline-block h-2 w-2 rounded-full ${statusDotColor(agent.status)}`} />
                  {agent.status}
                </span>
              </td>
              {showTeams && <td className="py-2 pr-4">{agent.team || '—'}</td>}
              {showMissions && <td className="py-2 pr-4">{agent.mission ?? '—'}</td>}
              <td className="py-2 pr-4">
                {agent.budget ? (
                  <div className="h-1.5 w-20 overflow-hidden rounded-full bg-muted">
                    <div
                      data-budget-bar
                      className={`h-full rounded-full ${budgetColor(agent.budget.daily_used, agent.budget.daily_limit)}`}
                      style={{
                        width: `${Math.min(100, (agent.budget.daily_used / agent.budget.daily_limit) * 100)}%`,
                      }}
                    />
                  </div>
                ) : (
                  '—'
                )}
              </td>
              <td className="py-2" style={{ fontVariantNumeric: 'tabular-nums' }}>
                {relativeTime(agent.lastActive)}
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
