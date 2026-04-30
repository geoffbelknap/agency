import { useState, useEffect, useRef } from 'react';
import { ChevronDown, ChevronRight, AlertTriangle, AlertCircle, CheckCircle2, Activity } from 'lucide-react';
import { Badge } from '@/app/components/ui/badge';
import { api } from '@/app/lib/api';
import type {
  ProcedureRecord,
  EpisodeRecord,
  TrajectoryState,
  MemoryOutcome,
  EpisodeOutcome,
  AnomalySeverity,
} from '@/app/types';

// ── Color helpers ──

const outcomeColors: Record<MemoryOutcome | EpisodeOutcome, string> = {
  success: 'bg-green-500/15 text-green-400 border-green-500/30',
  partial: 'bg-amber-500/15 text-amber-400 border-amber-500/30',
  failed: 'bg-red-500/15 text-red-400 border-red-500/30',
  escalated: 'bg-purple-500/15 text-purple-400 border-purple-500/30',
};

const toneColors: Record<string, string> = {
  routine: 'text-muted-foreground',
  notable: 'text-amber-400',
  problematic: 'text-red-400',
};

// ── ProceduresView ──

function ProceduresView({ agentName }: { agentName: string }) {
  const [procedures, setProcedures] = useState<ProcedureRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [outcomeFilter, setOutcomeFilter] = useState<string>('all');
  const [expanded, setExpanded] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    const params: { outcome?: string } = {};
    if (outcomeFilter !== 'all') params.outcome = outcomeFilter;
    api.agents.procedures(agentName, params)
      .then(r => setProcedures(Array.isArray(r?.procedures) ? r.procedures : []))
      .catch(() => setProcedures([]))
      .finally(() => setLoading(false));
  }, [agentName, outcomeFilter]);

  const filtered = procedures;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <select
          value={outcomeFilter}
          onChange={e => setOutcomeFilter(e.target.value)}
          aria-label="Filter by outcome"
          className="text-xs border rounded px-2 py-1 bg-background"
        >
          <option value="all">all</option>
          <option value="success">Success</option>
          <option value="partial">Partial</option>
          <option value="failed">Failed</option>
        </select>
        <span className="text-xs text-muted-foreground">{filtered.length} procedure{filtered.length !== 1 ? 's' : ''}</span>
      </div>

      {loading && <div className="text-xs text-muted-foreground py-4 text-center">Loading…</div>}

      {!loading && filtered.length === 0 && (
        <div className="text-xs text-muted-foreground py-4 text-center">No procedures found.</div>
      )}

      {!loading && filtered.map(p => {
        const isOpen = expanded === p.task_id;
        return (
          <div key={p.task_id} className="border rounded-md overflow-hidden">
            <button
              className="w-full flex items-center gap-2 px-3 py-2 hover:bg-muted/40 text-left text-sm"
              onClick={() => setExpanded(isOpen ? null : p.task_id)}
            >
              {isOpen ? <ChevronDown className="w-3.5 h-3.5 shrink-0" /> : <ChevronRight className="w-3.5 h-3.5 shrink-0" />}
              <span className="font-medium flex-1">{p.mission_name}</span>
              <span className="text-xs text-muted-foreground">{p.task_type}</span>
              <span className={`text-xs px-1.5 py-0.5 rounded border ${outcomeColors[p.outcome as MemoryOutcome]}`}>
                {p.outcome}
              </span>
              <span className="text-xs text-muted-foreground">{p.duration_minutes}m</span>
            </button>

            {isOpen && (
              <div className="px-3 pb-3 pt-1 flex flex-col gap-2 border-t bg-muted/20">
                <div>
                  <div className="text-xs font-medium text-muted-foreground mb-1">Approach</div>
                  <div className="text-sm">{p.approach}</div>
                </div>
                {(p.tools_used || []).length > 0 && (
                  <div>
                    <div className="text-xs font-medium text-muted-foreground mb-1">Tools used</div>
                    <div className="flex flex-wrap gap-1">
                      {(p.tools_used || []).map(t => (
                        <Badge key={t} variant="secondary" className="text-xs">{t}</Badge>
                      ))}
                    </div>
                  </div>
                )}
                {(p.lessons || []).length > 0 && (
                  <div>
                    <div className="text-xs font-medium text-muted-foreground mb-1">Lessons</div>
                    <ul className="list-disc list-inside text-sm space-y-0.5">
                      {(p.lessons || []).map((l, i) => <li key={i}>{l}</li>)}
                    </ul>
                  </div>
                )}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

// ── EpisodesView ──

function EpisodesView({ agentName }: { agentName: string }) {
  const [episodes, setEpisodes] = useState<EpisodeRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [expanded, setExpanded] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    api.agents.episodes(agentName)
      .then(r => setEpisodes(Array.isArray(r?.episodes) ? r.episodes : []))
      .catch(() => setEpisodes([]))
      .finally(() => setLoading(false));
  }, [agentName]);

  return (
    <div className="flex flex-col gap-3">
      <div className="text-xs text-muted-foreground">
        {episodes.length} episode{episodes.length !== 1 ? 's' : ''}
      </div>

      {loading && <div className="text-xs text-muted-foreground py-4 text-center">Loading…</div>}

      {!loading && episodes.length === 0 && (
        <div className="text-xs text-muted-foreground py-4 text-center">No episodes found.</div>
      )}

      {!loading && episodes.map(ep => {
        const isOpen = expanded === ep.task_id;
        const notableEvents = ep.notable_events || [];
        const entities = ep.entities_mentioned || [];
        const tags = ep.tags || [];
        const firstEvent = notableEvents[0];
        return (
          <div key={ep.task_id} className="border rounded-md overflow-hidden">
            <button
              className="w-full flex flex-col items-start gap-1 px-3 py-2 hover:bg-muted/40 text-left"
              onClick={() => setExpanded(isOpen ? null : ep.task_id)}
            >
              <div className="flex items-center gap-2 w-full">
                {isOpen ? <ChevronDown className="w-3.5 h-3.5 shrink-0" /> : <ChevronRight className="w-3.5 h-3.5 shrink-0" />}
                <span className="text-sm font-medium flex-1">{ep.summary}</span>
                <span className={`text-xs px-1.5 py-0.5 rounded border ${outcomeColors[ep.outcome]}`}>
                  {ep.outcome}
                </span>
                <span className="text-xs text-muted-foreground">{ep.duration_minutes}m</span>
              </div>
              {firstEvent && (
                <div className={`ml-5 text-xs ${toneColors[ep.operational_tone] || 'text-muted-foreground'}`}>
                  {firstEvent}
                </div>
              )}
            </button>

            {isOpen && (
              <div className="px-3 pb-3 pt-1 flex flex-col gap-2 border-t bg-muted/20">
                {notableEvents.length > 0 && (
                  <div>
                    <div className="text-xs font-medium text-muted-foreground mb-1">Notable events</div>
                    <div className="flex flex-col gap-1">
                      {notableEvents.map((ev, i) => (
                        <div
                          key={i}
                          className="text-xs px-2 py-1 rounded bg-amber-500/10 border border-amber-500/30 text-amber-400"
                        >
                          {ev}
                        </div>
                      ))}
                    </div>
                  </div>
                )}
                {entities.length > 0 && (
                  <div>
                    <div className="text-xs font-medium text-muted-foreground mb-1">Entities</div>
                    <div className="flex flex-wrap gap-1">
                      {entities.map((ent, i) => (
                        <Badge key={i} variant="outline" className="text-xs">{ent.type}: {ent.name}</Badge>
                      ))}
                    </div>
                  </div>
                )}
                {tags.length > 0 && (
                  <div className="flex flex-wrap gap-1">
                    {tags.map(tag => (
                      <Badge key={tag} variant="secondary" className="text-xs">#{tag}</Badge>
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

// ── TrajectoryView ──

const severityStyles: Record<AnomalySeverity, string> = {
  warning: 'border-amber-500/40 bg-amber-500/10 text-amber-400',
  critical: 'border-red-500/40 bg-red-500/10 text-red-400 shadow-[0_0_6px_hsl(0,72%,55%,0.4)] animate-pulse-warning',
};

const severityIcon = (s: AnomalySeverity) =>
  s === 'critical'
    ? <AlertCircle className="w-4 h-4 shrink-0" />
    : <AlertTriangle className="w-4 h-4 shrink-0" />;

function TrajectoryView({ agentName }: { agentName: string }) {
  const [state, setState] = useState<TrajectoryState | null>(null);
  const [loading, setLoading] = useState(true);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const load = () => {
    api.agents.trajectory(agentName)
      .then(r => setState(r))
      .catch(() => setState(null))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    intervalRef.current = setInterval(load, 30_000);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [agentName]);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <Activity className="w-4 h-4 text-muted-foreground" />
        <span className="font-medium text-sm">Trajectory Monitor</span>
        {state && (
          <Badge variant={state.enabled ? 'default' : 'secondary'} className="ml-auto text-xs">
            {state.enabled ? 'enabled' : 'disabled'}
          </Badge>
        )}
      </div>

      {loading && <div className="text-xs text-muted-foreground py-4 text-center">Loading…</div>}

      {!loading && state && (
        <>
          <div className="grid grid-cols-2 gap-2 text-xs">
            <div className="border rounded p-2">
              <div className="text-muted-foreground">Window size</div>
              <div className="font-medium">{state.window_size}</div>
            </div>
            <div className="border rounded p-2">
              <div className="text-muted-foreground">Current entries</div>
              <div className="font-medium">{state.current_entries}</div>
            </div>
          </div>

          {Object.keys(state.detectors || {}).length > 0 && (
            <div>
              <div className="text-xs font-medium text-muted-foreground mb-2">Detectors</div>
              <div className="flex flex-col gap-1">
                {Object.entries(state.detectors || {}).map(([name, det]) => (
                  <div key={name} className="flex items-center gap-2 text-xs border rounded px-2 py-1">
                    <CheckCircle2 className={`w-3 h-3 ${det.status === 'active' ? 'text-green-400' : 'text-muted-foreground'}`} />
                    <span className="font-mono text-muted-foreground">{name} — {det.status}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {(state.active_anomalies || []).length > 0 && (
            <div>
              <div className="text-xs font-medium text-muted-foreground mb-2">
                Active anomalies ({(state.active_anomalies || []).length})
              </div>
              <div className="flex flex-col gap-2">
                {(state.active_anomalies || []).map((a, i) => (
                  <div key={i} className={`flex items-start gap-2 border rounded p-2 text-xs ${severityStyles[a.severity]}`}>
                    {severityIcon(a.severity)}
                    <div className="flex flex-col gap-0.5 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="font-mono font-medium">{a.detector}</span>
                        <span className="text-xs opacity-70">{a.severity}</span>
                      </div>
                      <div>{a.detail}</div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {(state.active_anomalies || []).length === 0 && (
            <div className="text-xs text-muted-foreground flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-green-400" />
              No active anomalies
            </div>
          )}
        </>
      )}

      {!loading && !state && (
        <div className="text-xs text-muted-foreground py-4 text-center">Trajectory data unavailable.</div>
      )}
    </div>
  );
}

// ── AgentMemoryPanel ──

type MemoryTab = 'procedures' | 'episodes' | 'trajectory';

const TABS: { id: MemoryTab; label: string }[] = [
  { id: 'procedures', label: 'Procedures' },
  { id: 'episodes', label: 'Episodes' },
  { id: 'trajectory', label: 'Trajectory' },
];

export interface AgentMemoryPanelProps {
  agentName: string;
}

export function AgentMemoryPanel({ agentName }: AgentMemoryPanelProps) {
  const [activeTab, setActiveTab] = useState<MemoryTab>('procedures');

  return (
    <div className="flex flex-col gap-4">
      <div className="flex gap-1 border-b pb-2">
        {TABS.map(tab => (
          <button
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            className={`px-3 py-1.5 text-sm rounded-t transition-colors focus-visible:ring-2 focus-visible:ring-primary/50 ${
              activeTab === tab.id
                ? 'bg-muted font-medium text-foreground'
                : 'text-muted-foreground hover:text-foreground hover:bg-muted/50'
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      <div>
        {activeTab === 'procedures' && <ProceduresView agentName={agentName} />}
        {activeTab === 'episodes' && <EpisodesView agentName={agentName} />}
        {activeTab === 'trajectory' && <TrajectoryView agentName={agentName} />}
      </div>
    </div>
  );
}
