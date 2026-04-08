import { StatusIndicator } from '../../components/StatusIndicator';
import { Agent } from '../../types';
import { type RawBudgetResponse } from '../../lib/api';
import { formatDateTimeShort } from '../../lib/time';

interface Props {
  agent: Agent;
  budget: RawBudgetResponse | null;
}

function usageTone(used: number, limit: number): string {
  if (limit <= 0) return 'text-foreground';
  const ratio = used / limit;
  if (ratio > 0.95) return 'text-red-500';
  if (ratio > 0.8) return 'text-amber-500';
  return 'text-foreground';
}

function progressTone(used: number, limit: number): string {
  if (limit <= 0) return 'bg-primary';
  const ratio = used / limit;
  if (ratio > 0.95) return 'bg-red-500';
  if (ratio > 0.8) return 'bg-amber-500';
  return 'bg-primary';
}

export function AgentOverviewTab({ agent, budget }: Props) {
  const summaryItems = [
    {
      label: 'Status',
      value: agent.status,
      detail: agent.mode ? `Mode: ${agent.mode}` : undefined,
    },
    {
      label: 'Mission',
      value: agent.mission || 'No mission assigned',
      detail: agent.missionStatus ? `State: ${agent.missionStatus}` : undefined,
    },
    {
      label: 'Last active',
      value: agent.lastActive ? formatDateTimeShort(agent.lastActive) : 'No recent activity',
      detail: agent.uptime ? `Uptime: ${agent.uptime}` : undefined,
    },
    {
      label: 'Daily budget',
      value: budget && budget.daily_limit > 0
        ? `$${budget.daily_used.toFixed(2)} / $${budget.daily_limit.toFixed(2)}`
        : 'No daily budget',
      detail: budget && budget.daily_limit > 0
        ? `${Math.round((budget.daily_used / budget.daily_limit) * 100)}% used`
        : undefined,
      valueClassName: budget && budget.daily_limit > 0
        ? usageTone(budget.daily_used, budget.daily_limit)
        : undefined,
    },
  ];

  const identityItems = [
    ['Preset', agent.preset],
    ['Role', agent.role],
    ['Model', agent.model],
    ['Team', agent.team],
    ['Type', agent.type],
    ['Trust', agent.trustLevel != null && agent.trustLevel > 0 ? `${agent.trustLevel}/5` : undefined],
    ['Enforcer', agent.enforcerState],
    ['Build', agent.buildId],
  ].filter(([, value]) => value);

  return (
    <div className="space-y-4 p-4">
      {/* Current Task */}
      {agent.currentTask && (
        <div className="rounded-2xl border border-primary/20 bg-accent p-4">
          <div className="text-xs uppercase tracking-wide text-primary mb-1.5">Current Task</div>
          <div className="text-sm text-foreground">{agent.currentTask.content}</div>
          <div className="text-[10px] text-muted-foreground mt-1">
            {agent.currentTask.task_id} · {formatDateTimeShort(agent.currentTask.timestamp)}
          </div>
        </div>
      )}

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        {summaryItems.map((item) => (
          <div key={item.label} className="rounded-2xl border border-border bg-background/60 p-4">
            <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
              {item.label}
            </div>
            <div className={`mt-2 text-sm font-medium ${item.valueClassName ?? 'text-foreground'}`}>
              {item.value}
            </div>
            {item.detail && (
              <div className="mt-1 text-xs text-muted-foreground">{item.detail}</div>
            )}
          </div>
        ))}
      </div>

      <div className="rounded-2xl border border-border bg-background/60 p-4">
        <div className="flex items-center gap-3">
          <StatusIndicator status={agent.status} />
          <div>
            <div className="text-sm font-medium capitalize text-foreground">{agent.status}</div>
            <div className="text-xs text-muted-foreground">
              {agent.currentTask ? 'Currently executing work.' : 'No active task reported.'}
            </div>
          </div>
        </div>
      </div>

      <div className="rounded-2xl border border-border bg-background/60 p-4">
        <div className="text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">Identity</div>
        <div className="mt-3 grid grid-cols-2 gap-2 text-sm md:grid-cols-3">
          {identityItems.map(([label, value]) => (
            <div key={label as string} className="rounded-xl bg-secondary px-3 py-2">
              <div className="text-[10px] text-muted-foreground">{label}</div>
              <div className="mt-1 text-xs text-foreground">{value}</div>
            </div>
          ))}
        </div>
      </div>

      {/* Granted Capabilities */}
      {agent.grantedCapabilities && agent.grantedCapabilities.length > 0 && (
        <div className="rounded-2xl border border-border bg-background/60 p-4">
          <div className="text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">Capabilities</div>
          <div className="mt-3 flex flex-wrap gap-1.5">
            {agent.grantedCapabilities.map((c) => (
              <span key={c} className="text-xs bg-blue-50 dark:bg-blue-950/40 text-blue-700 dark:text-blue-300 border border-blue-200 dark:border-blue-900/40 rounded px-2 py-0.5">
                {c}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Budget */}
      {budget && (budget.daily_limit > 0 || budget.monthly_limit > 0) && (
        <div className="rounded-2xl border border-border bg-background/60 p-4">
          <div className="text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">Budget</div>
          <div className="mt-3 space-y-3">
            {budget.daily_limit > 0 && (
              <div>
                <div className="flex items-center justify-between text-[10px] mb-1">
                  <span className="text-muted-foreground">Daily</span>
                  <span className={usageTone(budget.daily_used, budget.daily_limit)}>${budget.daily_used.toFixed(2)} / ${budget.daily_limit.toFixed(2)}</span>
                </div>
                <div className="h-1.5 bg-secondary rounded-full overflow-hidden">
                  <div
                    className={`h-full rounded-full transition-all ${progressTone(budget.daily_used, budget.daily_limit)}`}
                    style={{ width: `${Math.min(100, (budget.daily_used / budget.daily_limit) * 100)}%` }}
                  />
                </div>
              </div>
            )}
            {budget.monthly_limit > 0 && (
              <div>
                <div className="flex items-center justify-between text-[10px] mb-1">
                  <span className="text-muted-foreground">Monthly</span>
                  <span className={usageTone(budget.monthly_used, budget.monthly_limit)}>${budget.monthly_used.toFixed(2)} / ${budget.monthly_limit.toFixed(2)}</span>
                </div>
                <div className="h-1.5 bg-secondary rounded-full overflow-hidden">
                  <div
                    className={`h-full rounded-full transition-all ${progressTone(budget.monthly_used, budget.monthly_limit)}`}
                    style={{ width: `${Math.min(100, (budget.monthly_used / budget.monthly_limit) * 100)}%` }}
                  />
                </div>
              </div>
            )}
            <div className="flex gap-3 text-[10px] text-muted-foreground">
              <span>LLM calls: <span className="text-foreground/80">{budget.today_llm_calls}</span></span>
              <span>In: <span className="text-foreground/80">{(budget.today_input_tokens / 1000).toFixed(1)}K</span></span>
              <span>Out: <span className="text-foreground/80">{(budget.today_output_tokens / 1000).toFixed(1)}K</span></span>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
