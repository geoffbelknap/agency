import { StatusIndicator } from '../../components/StatusIndicator';
import { Agent } from '../../types';
import { type RawBudgetResponse } from '../../lib/api';
import { formatDateTimeShort } from '../../lib/time';

interface Props {
  agent: Agent;
  budget: RawBudgetResponse | null;
}

export function AgentOverviewTab({ agent, budget }: Props) {
  return (
    <div className="space-y-4 p-4">
      {/* Current Task */}
      {agent.currentTask && (
        <div className="bg-accent border border-primary/20 rounded p-3">
          <div className="text-xs uppercase tracking-wide text-primary mb-1.5">Current Task</div>
          <div className="text-sm text-foreground">{agent.currentTask.content}</div>
          <div className="text-[10px] text-muted-foreground mt-1">
            {agent.currentTask.task_id} · {formatDateTimeShort(agent.currentTask.timestamp)}
          </div>
        </div>
      )}

      {/* Status bar */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <StatusIndicator status={agent.status} />
          <span className="text-sm text-foreground capitalize font-medium">{agent.status}</span>
        </div>
        {agent.uptime && (
          <span className="text-xs text-muted-foreground">Uptime: {agent.uptime}</span>
        )}
      </div>

      {/* Properties grid */}
      <div className="grid grid-cols-2 md:grid-cols-3 gap-2 text-sm">
        {[
          ['Mode', agent.mode],
          ['Enforcer', agent.enforcerState],
          ['Role', agent.role],
          ['Model', agent.model],
          ['Preset', agent.preset],
          ['Trust', agent.trustLevel != null && agent.trustLevel > 0 ? `${agent.trustLevel}/5` : undefined],
          ['Team', agent.team],
          ['Type', agent.type],
          ['Mission', agent.mission],
          ['Mission Status', agent.missionStatus],
          ['Build', agent.buildId],
          ['Last Active', agent.lastActive ? formatDateTimeShort(agent.lastActive) : undefined],
        ].filter(([, v]) => v).map(([k, v]) => (
          <div key={k as string} className="bg-secondary rounded px-2.5 py-1.5">
            <div className="text-[10px] text-muted-foreground">{k}</div>
            <div className="text-foreground text-xs">{v}</div>
          </div>
        ))}
      </div>

      {/* Granted Capabilities */}
      {agent.grantedCapabilities && agent.grantedCapabilities.length > 0 && (
        <div>
          <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1.5">Capabilities</div>
          <div className="flex flex-wrap gap-1.5">
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
        <div>
          <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1.5">Budget</div>
          <div className="space-y-2">
            {budget.daily_limit > 0 && (
              <div>
                <div className="flex items-center justify-between text-[10px] mb-1">
                  <span className="text-muted-foreground">Daily</span>
                  <span className="text-foreground/80">${budget.daily_used.toFixed(2)} / ${budget.daily_limit.toFixed(2)}</span>
                </div>
                <div className="h-1.5 bg-secondary rounded-full overflow-hidden">
                  <div
                    className={`h-full rounded-full transition-all ${
                      budget.daily_used / budget.daily_limit > 0.95 ? 'bg-red-500' :
                      budget.daily_used / budget.daily_limit > 0.8 ? 'bg-amber-500' : 'bg-primary'
                    }`}
                    style={{ width: `${Math.min(100, (budget.daily_used / budget.daily_limit) * 100)}%` }}
                  />
                </div>
              </div>
            )}
            {budget.monthly_limit > 0 && (
              <div>
                <div className="flex items-center justify-between text-[10px] mb-1">
                  <span className="text-muted-foreground">Monthly</span>
                  <span className="text-foreground/80">${budget.monthly_used.toFixed(2)} / ${budget.monthly_limit.toFixed(2)}</span>
                </div>
                <div className="h-1.5 bg-secondary rounded-full overflow-hidden">
                  <div
                    className={`h-full rounded-full transition-all ${
                      budget.monthly_used / budget.monthly_limit > 0.95 ? 'bg-red-500' :
                      budget.monthly_used / budget.monthly_limit > 0.8 ? 'bg-amber-500' : 'bg-primary'
                    }`}
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
