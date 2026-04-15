import { useState, useMemo } from 'react';
import { Link } from 'react-router';
import { Send, RefreshCw, ChevronDown, ChevronRight } from 'lucide-react';
import { Button } from '../../components/ui/button';
import { type RawAuditEntry } from '../../lib/api';

interface Props {
  agentName: string;
  logs: RawAuditEntry[];
  refreshingLogs: boolean;
  refreshLogs: (name: string) => Promise<void>;
  handleSendDM: (agentName: string, dmText: string) => Promise<boolean>;
}

function DmSection({ agentName, handleSendDM }: { agentName: string; handleSendDM: (name: string, text: string) => Promise<boolean> }) {
  const [dmText, setDmText] = useState('');

  return (
    <div className="space-y-4 p-4">
      <div className="space-y-2">
        <div className="text-xs uppercase tracking-wide text-muted-foreground">Send Task via DM</div>
        <textarea
          value={dmText}
          onChange={(e) => setDmText(e.target.value)}
          placeholder="Describe the task..."
          className="w-full h-28 bg-secondary border border-border rounded p-3 text-sm text-foreground placeholder-muted-foreground/70 resize-none focus:outline-none focus:border-primary"
        />
        <div className="flex items-center gap-3">
          <Button size="sm" onClick={async () => { const ok = await handleSendDM(agentName, dmText); if (ok) setDmText(''); }} disabled={!dmText.trim()}>
            <Send className="w-3 h-3 mr-1" />Send to DM
          </Button>
          <Link to={`/channels/dm-${agentName}`} className="text-xs text-primary hover:text-primary/80">
            View conversation →
          </Link>
        </div>
      </div>
    </div>
  );
}

function LogsSection({ agentName, logs, refreshingLogs, refreshLogs }: {
  agentName: string;
  logs: RawAuditEntry[];
  refreshingLogs: boolean;
  refreshLogs: (name: string) => Promise<void>;
}) {
  const [expandedLog, setExpandedLog] = useState<number | null>(null);
  const reversedLogs = useMemo(() => logs.slice().reverse(), [logs]);

  function formatTimestamp(entry: RawAuditEntry): string {
    const value = entry.timestamp || entry.ts;
    if (!value) return 'Unknown time';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString([], {
      month: 'short',
      day: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
    });
  }

  function summarizeEvent(entry: RawAuditEntry): string {
    if (entry.error) return entry.error;
    if (entry.detail) return entry.detail;
    if (entry.reason) return entry.reason;
    if (entry.task_content) return entry.task_content;
    if (entry.tool) return `Tool call: ${entry.tool}`;
    if (entry.capability) return `Capability: ${entry.capability}`;
    if (entry.phase_name) return `Phase: ${entry.phase_name}`;
    if (entry.method || entry.path || entry.url) {
      return [entry.method, entry.path || entry.url].filter(Boolean).join(' ');
    }
    return 'No additional details recorded.';
  }

  return (
    <div className="space-y-1 p-4">
      <div className="flex items-center justify-between mb-2">
        <div className="text-xs uppercase tracking-wide text-muted-foreground">
          Recent Runtime Events ({logs.length})
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 w-6 p-0"
          onClick={() => refreshLogs(agentName)}
          disabled={refreshingLogs}
          aria-label={refreshingLogs ? 'Refreshing logs' : 'Refresh logs'}
        >
          <RefreshCw className={`w-3 h-3 ${refreshingLogs ? 'animate-spin' : ''}`} />
        </Button>
      </div>
      <div className="bg-background border border-border rounded divide-y divide-border/50 max-h-[600px] overflow-auto">
        {logs.length === 0 ? (
          <div className="p-3 text-xs text-muted-foreground/70">No runtime events yet.</div>
        ) : (
          reversedLogs.map((e, i) => {
            const isExpanded = expandedLog === i;
            const hasDetail = Boolean(
              e.task_content || e.reason || e.capability || e.error || e.phase_name || e.detail || e.tool || e.path || e.url,
            );
            return (
              <div key={i}>
                <div
                  className={`flex items-start gap-2 px-3 py-2 text-xs ${hasDetail ? 'cursor-pointer hover:bg-card/50' : ''}`}
                  onClick={() => hasDetail && setExpandedLog(isExpanded ? null : i)}
                >
                  {hasDetail ? (
                    isExpanded ? <ChevronDown className="mt-0.5 w-3 h-3 text-muted-foreground/70 shrink-0" /> : <ChevronRight className="mt-0.5 w-3 h-3 text-muted-foreground/70 shrink-0" />
                  ) : (
                    <span className="w-3 shrink-0" />
                  )}
                  <div className="min-w-0 flex-1 space-y-1">
                    <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                      <span className="shrink-0 text-muted-foreground/70">{formatTimestamp(e)}</span>
                      <span className="shrink-0 rounded-full bg-secondary px-2 py-0.5 font-medium text-foreground">
                        {e.event || e.type || 'event'}
                      </span>
                      {(e.source || e.agent_name || e.agent) && (
                        <span className="shrink-0 text-muted-foreground">
                          {e.source || e.agent_name || e.agent}
                        </span>
                      )}
                    </div>
                    <div className="min-w-0 break-words text-muted-foreground">
                      {summarizeEvent(e)}
                    </div>
                  </div>
                </div>
                {isExpanded && (
                  <div className="px-3 pb-3 pl-8 space-y-1.5">
                    {e.task_content && (
                      <div>
                        <span className="text-[10px] text-muted-foreground">Task: </span>
                        <span className="text-xs text-foreground/80">{e.task_content}</span>
                      </div>
                    )}
                    {e.delivered_by && (
                      <div>
                        <span className="text-[10px] text-muted-foreground">Delivered by: </span>
                        <span className="text-xs text-foreground/80">{e.delivered_by}</span>
                      </div>
                    )}
                    {e.task_id && (
                      <div>
                        <span className="text-[10px] text-muted-foreground">Task ID: </span>
                        <code className="text-xs text-muted-foreground">{e.task_id}</code>
                      </div>
                    )}
                    {e.mode && (
                      <div>
                        <span className="text-[10px] text-muted-foreground">Mode: </span>
                        <span className="text-xs text-foreground/80">{e.mode}</span>
                      </div>
                    )}
                    {e.capability && (
                      <div>
                        <span className="text-[10px] text-muted-foreground">Capability: </span>
                        <span className="text-xs text-foreground/80">{e.capability}</span>
                      </div>
                    )}
                    {e.reason && (
                      <div>
                        <span className="text-[10px] text-muted-foreground">Reason: </span>
                        <span className="text-xs text-foreground/80">{e.reason}</span>
                      </div>
                    )}
                    {e.phase_name && (
                      <div>
                        <span className="text-[10px] text-muted-foreground">Phase: </span>
                        <span className="text-xs text-foreground/80">{e.phase_name} (phase {e.phase})</span>
                      </div>
                    )}
                    {e.error && (
                      <div>
                        <span className="text-[10px] text-muted-foreground">Error: </span>
                        <span className="text-xs text-red-400">{e.error}</span>
                      </div>
                    )}
                    {e.model && (
                      <div>
                        <span className="text-[10px] text-muted-foreground">Model: </span>
                        <span className="text-xs text-foreground/80">{e.model}</span>
                      </div>
                    )}
                    {typeof e.duration_ms === 'number' && (
                      <div>
                        <span className="text-[10px] text-muted-foreground">Duration: </span>
                        <span className="text-xs text-foreground/80">{e.duration_ms} ms</span>
                      </div>
                    )}
                    {typeof e.status === 'number' && (
                      <div>
                        <span className="text-[10px] text-muted-foreground">Status: </span>
                        <span className="text-xs text-foreground/80">{e.status}</span>
                      </div>
                    )}
                    {e.initiator && (
                      <div>
                        <span className="text-[10px] text-muted-foreground">Initiator: </span>
                        <span className="text-xs text-foreground/80">{e.initiator}</span>
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}

export function AgentActivityTab({ agentName, logs, refreshingLogs, refreshLogs, handleSendDM }: Props) {
  return (
    <div className="flex h-full flex-col">
      <DmSection agentName={agentName} handleSendDM={handleSendDM} />
      <div className="border-t border-border" />
      <LogsSection
        agentName={agentName}
        logs={logs}
        refreshingLogs={refreshingLogs}
        refreshLogs={refreshLogs}
      />
    </div>
  );
}

export { LogsSection };
