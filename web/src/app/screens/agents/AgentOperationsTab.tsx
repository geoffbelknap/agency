import { useNavigate } from 'react-router';
import { Hash, Brain, DatabaseZap } from 'lucide-react';
import { Button } from '../../components/ui/button';
import { type RawChannel, type RawEconomicsResponse } from '../../lib/api';
import { formatDateTimeShort } from '../../lib/time';

type OperationsSubTab = 'channels' | 'knowledge' | 'economics';

const ALL_OPERATIONS_TABS: { id: OperationsSubTab; label: string }[] = [
  { id: 'channels', label: 'Channels' },
  { id: 'knowledge', label: 'Knowledge' },
  { id: 'economics', label: 'Economics' },
];

interface Props {
  agentName: string;
  channels: RawChannel[];
  knowledge: Record<string, any>[];
  economics: RawEconomicsResponse | null;
  handleClearCache: (agentName: string) => Promise<void>;
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

function KnowledgeContent({ agentName, knowledge, handleClearCache }: {
  agentName: string;
  knowledge: Record<string, any>[];
  handleClearCache: (agentName: string) => Promise<void>;
}) {
  return (
    <div className="space-y-2 p-4">
      <div className="flex items-center justify-between gap-3 mb-2">
        <div className="text-xs uppercase tracking-wide text-muted-foreground">
          Knowledge Contributions
        </div>
        <Button
          variant="outline"
          size="sm"
          className="h-7 text-xs"
          onClick={() => handleClearCache(agentName)}
        >
          <DatabaseZap className="w-3 h-3 mr-1" />
          Clear cache
        </Button>
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

function fmtCurrency(value?: number) {
  return `$${(value ?? 0).toFixed(4)}`;
}

function fmtNumber(value?: number) {
  return (value ?? 0).toLocaleString();
}

function fmtPercent(value?: number) {
  return `${Math.round((value ?? 0) * 100)}%`;
}

function MetricCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded bg-secondary p-3">
      <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">{label}</div>
      <div className="text-sm font-medium text-foreground">{value}</div>
    </div>
  );
}

function EconomicsContent({ economics }: { economics: RawEconomicsResponse | null }) {
  if (!economics) {
    return (
      <div className="p-4 text-xs text-muted-foreground/70">
        No economics metrics available for this agent yet.
      </div>
    );
  }

  return (
    <div className="space-y-4 p-4">
      <div>
        <div className="text-xs uppercase tracking-wide text-muted-foreground mb-1">Today</div>
        <div className="text-xs text-muted-foreground/70">{economics.period ?? 'Current UTC day'}</div>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <MetricCard label="Cost" value={fmtCurrency(economics.total_cost_usd)} />
        <MetricCard label="Requests" value={fmtNumber(economics.requests)} />
        <MetricCard label="Input Tokens" value={fmtNumber(economics.input_tokens)} />
        <MetricCard label="Output Tokens" value={fmtNumber(economics.output_tokens)} />
        <MetricCard label="Cache Hits" value={fmtNumber(economics.cache_hits)} />
        <MetricCard label="Cache Hit Rate" value={fmtPercent(economics.cache_hit_rate)} />
        <MetricCard label="Retry Waste" value={fmtCurrency(economics.retry_waste_usd)} />
        <MetricCard label="Tool Hallucination Rate" value={fmtPercent(economics.tool_hallucination_rate)} />
      </div>
      {economics.by_model && Object.keys(economics.by_model).length > 0 && (
        <div>
          <div className="text-xs uppercase tracking-wide text-muted-foreground mb-2">By Model</div>
          <pre className="font-mono text-[11px] bg-secondary rounded p-3 overflow-x-auto whitespace-pre-wrap break-all">
            {JSON.stringify(economics.by_model, null, 2)}
          </pre>
        </div>
      )}
    </div>
  );
}

export function AgentOperationsTab({ agentName, channels, knowledge, economics, handleClearCache, subTab, onSubTabChange }: Props) {
  return (
    <div className="flex flex-col h-full">
      <div role="tablist" className="flex flex-wrap gap-2 px-3 py-2 border-b border-border">
        {ALL_OPERATIONS_TABS.map((t) => (
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
        {subTab === 'knowledge' && <KnowledgeContent agentName={agentName} knowledge={knowledge} handleClearCache={handleClearCache} />}
        {subTab === 'economics' && <EconomicsContent economics={economics} />}
      </div>
    </div>
  );
}
