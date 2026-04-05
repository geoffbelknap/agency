import { useState, useMemo } from 'react';
import { Link } from 'react-router';
import { Send, RefreshCw, ChevronDown, ChevronRight } from 'lucide-react';
import { Button } from '../../components/ui/button';
import { type RawAuditEntry } from '../../lib/api';
import { AgentActivityGroup } from './AgentActivityGroup';

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

  return (
    <div className="space-y-1 p-4">
      <div className="flex items-center justify-between mb-2">
        <div className="text-xs uppercase tracking-wide text-muted-foreground">
          Audit Log ({logs.length} events)
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
          <div className="p-3 text-xs text-muted-foreground/70">No audit logs yet.</div>
        ) : (
          reversedLogs.map((e, i) => {
            const isExpanded = expandedLog === i;
            const hasDetail = e.task_content || e.reason || e.capability || e.error || e.phase_name;
            return (
              <div key={i}>
                <div
                  className={`flex items-center gap-2 md:gap-3 px-3 py-1.5 font-mono text-xs ${hasDetail ? 'cursor-pointer hover:bg-card/50' : ''}`}
                  onClick={() => hasDetail && setExpandedLog(isExpanded ? null : i)}
                >
                  {hasDetail ? (
                    isExpanded ? <ChevronDown className="w-3 h-3 text-muted-foreground/70 shrink-0" /> : <ChevronRight className="w-3 h-3 text-muted-foreground/70 shrink-0" />
                  ) : (
                    <span className="w-3 shrink-0" />
                  )}
                  <span className="text-muted-foreground/70 shrink-0">{(e.timestamp || e.ts || '').slice(11, 19)}</span>
                  <span className="text-primary shrink-0">{e.event || e.type}</span>
                  <span className="text-muted-foreground truncate">{e.source || ''}</span>
                </div>
                {isExpanded && (
                  <div className="px-3 pb-2 pl-9 space-y-1">
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
  const activityContent = <DmSection agentName={agentName} handleSendDM={handleSendDM} />;
  return (
    <AgentActivityGroup agentName={agentName} activityContent={activityContent} />
  );
}

export { LogsSection };
