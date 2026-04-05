import { useNavigate } from 'react-router';
import { Hash, Brain } from 'lucide-react';
import { Button } from '../../components/ui/button';
import { type RawChannel, type RawMeeseeks } from '../../lib/api';
import { formatDateTimeShort } from '../../lib/time';

type OperationsSubTab = 'channels' | 'knowledge' | 'meeseeks';

const OPERATIONS_TABS: { id: OperationsSubTab; label: string }[] = [
  { id: 'channels', label: 'Channels' },
  { id: 'knowledge', label: 'Knowledge' },
  { id: 'meeseeks', label: 'Meeseeks' },
];

interface Props {
  agentName: string;
  channels: RawChannel[];
  knowledge: Record<string, any>[];
  meeseeksList: RawMeeseeks[];
  handleKillMeeseeks: (agentName: string, meeseeksId: string) => Promise<void>;
  subTab: OperationsSubTab;
  onSubTabChange: (tab: OperationsSubTab) => void;
}

function ChannelsContent({ channels }: { channels: RawChannel[] }) {
  const navigate = useNavigate();
  return (
    <div className="space-y-2 p-4">
      <div className="text-xs uppercase tracking-wide text-muted-foreground mb-2">
        Channels ({channels.length})
      </div>
      {channels.length === 0 ? (
        <div className="text-xs text-muted-foreground/70">Not a member of any channels.</div>
      ) : (
        channels.map((ch: any) => (
          <div
            key={ch.name}
            className="bg-secondary rounded p-3 cursor-pointer hover:bg-accent hover:border-primary border border-transparent transition-colors"
            onClick={() => navigate(`/channels/${ch.name}`)}
          >
            <div className="flex items-center gap-2 mb-1">
              <Hash className="w-3 h-3 text-muted-foreground" />
              <code className="text-sm text-foreground">{ch.name}</code>
              <span className={`text-[10px] px-1.5 py-0.5 rounded ${ch.state === 'active' ? 'bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-400' : 'bg-border text-muted-foreground'}`}>
                {ch.state || 'active'}
              </span>
            </div>
            {ch.topic && <div className="text-xs text-muted-foreground ml-5">{ch.topic}</div>}
            <div className="text-[10px] text-muted-foreground/70 ml-5 mt-1">
              {(ch.members || []).length} members · {ch.type || 'standard'} · <span className="text-primary">click to join</span>
            </div>
          </div>
        ))
      )}
    </div>
  );
}

function KnowledgeContent({ knowledge }: { knowledge: Record<string, any>[] }) {
  return (
    <div className="space-y-2 p-4">
      <div className="text-xs uppercase tracking-wide text-muted-foreground mb-2">
        Knowledge Contributions
      </div>
      {knowledge.length === 0 ? (
        <div className="text-xs text-muted-foreground/70">No knowledge contributions found.</div>
      ) : (
        (Array.isArray(knowledge) ? knowledge : []).slice(0, 20).map((node: any, i: number) => (
          <div key={node.id || i} className="bg-secondary rounded p-3">
            <div className="flex items-center gap-2 mb-1">
              <Brain className="w-3 h-3 text-purple-400" />
              <span className="text-xs font-medium text-foreground">{node.label || node.topic || node.id}</span>
              {node.confidence != null && (
                <span className="text-[10px] text-muted-foreground">{Math.round(node.confidence * 100)}%</span>
              )}
            </div>
            {(node.summary || node.content) && (
              <div className="text-xs text-muted-foreground ml-5 line-clamp-2">{node.summary || node.content}</div>
            )}
            {node.timestamp && (
              <div className="text-[10px] text-muted-foreground ml-5 mt-1">{formatDateTimeShort(node.timestamp)}</div>
            )}
          </div>
        ))
      )}
    </div>
  );
}

function MeeseeksContent({ agentName, meeseeksList, handleKillMeeseeks }: {
  agentName: string;
  meeseeksList: RawMeeseeks[];
  handleKillMeeseeks: (agentName: string, meeseeksId: string) => Promise<void>;
}) {
  return (
    <div className="space-y-2 p-4">
      <div className="text-xs uppercase tracking-wide text-muted-foreground mb-2">
        Meeseeks ({meeseeksList.length})
      </div>
      {meeseeksList.length === 0 ? (
        <div className="text-xs text-muted-foreground/70">No active meeseeks.</div>
      ) : (
        meeseeksList.map((m) => {
          const statusColor = m.status === 'working' ? 'bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-400'
            : m.status === 'spawned' ? 'bg-blue-50 dark:bg-blue-950 text-blue-700 dark:text-blue-400'
            : m.status === 'distressed' ? 'bg-red-50 dark:bg-red-950 text-red-700 dark:text-red-400'
            : 'bg-border text-muted-foreground';
          return (
            <div key={m.id} className="bg-secondary rounded p-3">
              <div className="flex items-center justify-between mb-1">
                <div className="flex items-center gap-2">
                  <code className="text-xs text-foreground">{m.id}</code>
                  <span className={`text-[10px] px-1.5 py-0.5 rounded ${statusColor}`}>{m.status}</span>
                  {m.orphaned && (
                    <span className="text-[10px] bg-amber-50 dark:bg-amber-950 text-amber-700 dark:text-amber-400 px-1.5 py-0.5 rounded">orphaned</span>
                  )}
                </div>
                {m.status !== 'completed' && m.status !== 'terminated' && (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 text-[10px] text-red-400 hover:text-red-300"
                    onClick={() => handleKillMeeseeks(agentName, m.id)}
                  >
                    Kill
                  </Button>
                )}
              </div>
              <div className="text-xs text-foreground/80 line-clamp-2">{m.task}</div>
              <div className="flex items-center gap-3 mt-1 text-[10px] text-muted-foreground">
                {m.model && <span>Model: {m.model}</span>}
                {m.budget != null && <span>Budget: ${(m.budget_used || 0).toFixed(4)} / ${m.budget.toFixed(4)}</span>}
                {m.spawned_at && <span>{new Date(m.spawned_at).toLocaleTimeString()}</span>}
              </div>
            </div>
          );
        })
      )}
    </div>
  );
}

export function AgentOperationsTab({ agentName, channels, knowledge, meeseeksList, handleKillMeeseeks, subTab, onSubTabChange }: Props) {
  return (
    <div className="flex flex-col h-full">
      <div role="tablist" className="flex gap-2 px-2 py-1 border-b border-border">
        {OPERATIONS_TABS.map((t) => (
          <button key={t.id} role="tab" aria-selected={subTab === t.id} aria-controls={`ops-panel-${t.id}`} onClick={() => onSubTabChange(t.id)}
            className={`text-xs px-2 py-1 rounded transition-colors ${
              subTab === t.id ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:text-foreground'
            }`}>
            {t.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`ops-panel-${subTab}`} className="flex-1 overflow-auto">
        {subTab === 'channels' && <ChannelsContent channels={channels} />}
        {subTab === 'knowledge' && <KnowledgeContent knowledge={knowledge} />}
        {subTab === 'meeseeks' && <MeeseeksContent agentName={agentName} meeseeksList={meeseeksList} handleKillMeeseeks={handleKillMeeseeks} />}
      </div>
    </div>
  );
}
