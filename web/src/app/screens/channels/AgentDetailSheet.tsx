// src/app/screens/channels/AgentDetailSheet.tsx
import { X } from 'lucide-react';
import { Sheet, SheetContent } from '../../components/ui/sheet';
import { Button } from '../../components/ui/button';
import { StatusIndicator } from '../../components/StatusIndicator';
import { type RawAgent, type RawBudgetResponse } from '../../lib/api';
import { formatDateTimeShort } from '../../lib/time';
import type { AgentStatus } from '../../types';

interface AgentDetailSheetProps {
  agentName: string | null;
  agent: RawAgent | null;
  budget: RawBudgetResponse | null;
  onClose: () => void;
  onMessageAgent: (dmChannelName: string) => void;
}

export function AgentDetailSheet({ agentName, agent, budget, onClose, onMessageAgent }: AgentDetailSheetProps) {
  return (
    <Sheet open={!!agentName} onOpenChange={(open) => { if (!open) onClose(); }}>
      <SheetContent side="right" hideClose className="p-0 w-full sm:max-w-[480px] bg-card border-border flex flex-col overflow-hidden">
        {agent ? (
          <>
            <div className="shrink-0 border-b border-border p-4">
              <div className="flex items-start justify-between mb-3">
                <div>
                  <code className="text-lg text-foreground">{agent.name}</code>
                  <div className="text-xs text-muted-foreground mt-1">
                    {[agent.type, agent.role, agent.team && `team: ${agent.team}`].filter(Boolean).join(' · ')}
                  </div>
                </div>
                <Button size="sm" variant="ghost" onClick={onClose} className="h-8 w-8 p-0">
                  <X className="w-4 h-4" />
                  <span className="sr-only">Close</span>
                </Button>
              </div>
              <div className="flex items-center gap-3">
                <StatusIndicator status={(agent.status || 'stopped') as AgentStatus} />
                <span className="text-sm text-foreground capitalize font-medium">{agent.status || 'stopped'}</span>
                {agent.uptime && <span className="text-xs text-muted-foreground">Uptime: {agent.uptime}</span>}
                <Button size="sm" variant="outline" className="ml-auto" onClick={() => {
                  onMessageAgent('dm-' + agent.name);
                  onClose();
                }}>
                  Message
                </Button>
              </div>
            </div>
            <div className="flex-1 overflow-auto p-4 space-y-4">
              {/* Current Task */}
              {agent.current_task && (
                <div className="bg-accent border border-primary/20 rounded p-3">
                  <div className="text-xs uppercase tracking-wide text-primary mb-1.5">Current Task</div>
                  <div className="text-sm text-foreground">{agent.current_task.content}</div>
                  <div className="text-[10px] text-muted-foreground mt-1">
                    {agent.current_task.task_id} · {formatDateTimeShort(agent.current_task.timestamp)}
                  </div>
                </div>
              )}

              {/* Properties */}
              <div className="grid grid-cols-2 gap-2 text-sm">
                {[
                  ['Mode', agent.mode],
                  ['Enforcer', agent.enforcer],
                  ['Role', agent.role],
                  ['Model', agent.model],
                  ['Preset', agent.preset],
                  ['Trust', agent.trust_level != null && agent.trust_level > 0 ? `${agent.trust_level}/5` : undefined],
                  ['Team', agent.team],
                  ['Mission', agent.mission],
                  ['Mission Status', agent.mission_status],
                  ['Build', agent.build_id],
                  ['Last Active', agent.last_active ? formatDateTimeShort(agent.last_active) : undefined],
                ].filter(([, v]) => v).map(([k, v]) => (
                  <div key={k as string} className="bg-secondary rounded px-2.5 py-1.5">
                    <div className="text-[10px] text-muted-foreground">{k}</div>
                    <div className="text-foreground text-xs">{v}</div>
                  </div>
                ))}
              </div>

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
                            className={`h-full rounded-full ${
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
                            className={`h-full rounded-full ${
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

              {/* Capabilities */}
              {agent.granted_capabilities && agent.granted_capabilities.length > 0 && (
                <div>
                  <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1.5">Capabilities</div>
                  <div className="flex flex-wrap gap-1.5">
                    {agent.granted_capabilities.map((c) => (
                      <span key={c} className="text-xs bg-blue-50 dark:bg-blue-950/40 text-blue-700 dark:text-blue-300 border border-blue-200 dark:border-blue-900/40 rounded px-2 py-0.5">
                        {c}
                      </span>
                    ))}
                  </div>
                </div>
              )}

            </div>
          </>
        ) : (
          <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
            Loading agent detail...
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}
