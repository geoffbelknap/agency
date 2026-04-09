import { useState, useEffect } from 'react';
import { api } from '../lib/api';
import { formatDateTimeShort } from '../lib/time';
import { Input } from '../components/ui/input';
import { Button } from '../components/ui/button';
import { Search, Database, Sparkles, Check, X, RotateCcw } from 'lucide-react';
import { toast } from 'sonner';

interface QueryResult {
  label: string;
  kind: string;
  summary: string;
  source_type: string;
  updated_at: string;
  connections: number;
}

interface KnowledgeStats {
  node_count: number;
  edge_count: number;
}

interface OntologyCandidate {
  id: string;
  value?: string;
  label?: string;
  name?: string;
  count?: number;
  source?: string;
  status?: string;
  candidate_type?: string;
  properties?: {
    value?: string;
    occurrence_count?: number;
    source_count?: number;
    status?: string;
  };
}

interface OntologyDecision {
  id: string;
  nodeId: string;
  value: string;
  action: 'promote' | 'reject' | 'restore' | 'unknown';
  timestamp?: string;
}

function candidateValue(candidate: OntologyCandidate, fallback = 'candidate') {
  return (
    candidate.value ||
    candidate.properties?.value ||
    candidate.label ||
    candidate.name ||
    fallback
  );
}

function candidateStatus(candidate: OntologyCandidate) {
  return candidate.status || candidate.properties?.status || 'candidate';
}

function parseOntologyDecisions(raw: unknown): OntologyDecision[] {
  const entries = Array.isArray(raw)
    ? raw
    : Array.isArray((raw as { entries?: unknown[] } | null)?.entries)
      ? (raw as { entries: unknown[] }).entries
      : [];

  return entries
    .map((entry, index) => {
      const record = (entry ?? {}) as Record<string, unknown>;
      const data = (record.data ?? {}) as Record<string, unknown>;
      const actionText = String(record.action ?? record.event ?? record.type ?? '');
      const normalizedAction = actionText.toLowerCase();
      const action: OntologyDecision['action'] =
        normalizedAction.includes('promote') ? 'promote' :
        normalizedAction.includes('reject') ? 'reject' :
        normalizedAction.includes('restore') ? 'restore' :
        'unknown';

      if (action === 'unknown') {
        return null;
      }

      const nodeId = String(
        record.node_id ??
        record.nodeId ??
        data.node_id ??
        data.nodeId ??
        record.id ??
        ''
      );
      const value = String(
        record.value ??
        data.value ??
        record.label ??
        data.label ??
        data.subject ??
        record.subject ??
        nodeId
      );

      if (!nodeId || !value) {
        return null;
      }

      const timestamp = typeof record.timestamp === 'string'
        ? record.timestamp
        : typeof record.ts === 'string'
          ? record.ts
          : undefined;

      return {
        id: String(record.id ?? `${action}-${nodeId}-${timestamp ?? index}`),
        nodeId,
        value,
        action,
        timestamp,
      };
    })
    .filter((entry): entry is OntologyDecision => entry !== null);
}

export function Knowledge({ onSelectResult }: { onSelectResult?: (label: string, kind: string) => void }) {
  const [queryText, setQueryText] = useState('');
  const [queryResults, setQueryResults] = useState<QueryResult[]>([]);
  const [queryLoading, setQueryLoading] = useState(false);
  const [queryError, setQueryError] = useState<string | null>(null);

  const [whoKnowsText, setWhoKnowsText] = useState('');
  const [whoKnowsResults, setWhoKnowsResults] = useState<any>(null);
  const [whoKnowsLoading, setWhoKnowsLoading] = useState(false);
  const [whoKnowsError, setWhoKnowsError] = useState<string | null>(null);

  const [stats, setStats] = useState<KnowledgeStats | null>(null);
  const [statsLoading, setStatsLoading] = useState(true);

  const [ontologyCandidates, setOntologyCandidates] = useState<OntologyCandidate[]>([]);
  const [ontologyLoading, setOntologyLoading] = useState(false);
  const [ontologyDecisions, setOntologyDecisions] = useState<OntologyDecision[]>([]);
  const [ontologyActionLoading, setOntologyActionLoading] = useState<string | null>(null);

  const loadOntologyReviewData = async () => {
    try {
      setOntologyLoading(true);
      const [candidateData, curationLog] = await Promise.all([
        api.knowledge.ontologyCandidates(),
        api.knowledge.curationLog().catch(() => null),
      ]);
      setOntologyCandidates(candidateData.candidates || []);
      setOntologyDecisions(parseOntologyDecisions(curationLog));
    } catch {
      // Ontology may not be available — ignore
    } finally {
      setOntologyLoading(false);
    }
  };

  const handlePromote = async (candidate: OntologyCandidate) => {
    const value = candidateValue(candidate, candidate.id);
    try {
      setOntologyActionLoading(candidate.id);
      await api.knowledge.ontologyPromote(candidate.id, value);
      toast.success(`Promoted "${value}" to ontology`);
      await loadOntologyReviewData();
    } catch (e: any) {
      toast.error(e.message || 'Promote failed');
    } finally {
      setOntologyActionLoading(null);
    }
  };

  const handleReject = async (candidate: OntologyCandidate) => {
    const value = candidateValue(candidate, candidate.id);
    try {
      setOntologyActionLoading(candidate.id);
      await api.knowledge.ontologyReject(candidate.id, value);
      toast.success(`Rejected "${value}"`);
      await loadOntologyReviewData();
    } catch (e: any) {
      toast.error(e.message || 'Reject failed');
    } finally {
      setOntologyActionLoading(null);
    }
  };

  const handleRestore = async (nodeId: string, value: string) => {
    try {
      setOntologyActionLoading(nodeId);
      await api.knowledge.ontologyRestore(nodeId, value);
      toast.success(`Restored "${value}" to ontology review`);
      await loadOntologyReviewData();
    } catch (e: any) {
      toast.error(e.message || 'Restore failed');
    } finally {
      setOntologyActionLoading(null);
    }
  };

  useEffect(() => {
    loadOntologyReviewData();
    const loadStats = async () => {
      try {
        setStatsLoading(true);
        const data = await api.knowledge.stats();
        const d = data as any;
        setStats({ node_count: d.nodes ?? d.node_count ?? 0, edge_count: d.edges ?? d.edge_count ?? 0 });
      } catch {
        setStats(null);
      } finally {
        setStatsLoading(false);
      }
    };
    loadStats();
  }, []);

  const handleQuery = async () => {
    if (!queryText.trim()) return;
    try {
      setQueryLoading(true);
      setQueryError(null);
      const data = await api.knowledge.query(queryText.trim());
      setQueryResults((data as any).results || []);
    } catch (e: any) {
      setQueryError(e.message || 'Query failed');
      setQueryResults([]);
    } finally {
      setQueryLoading(false);
    }
  };

  const handleWhoKnows = async () => {
    if (!whoKnowsText.trim()) return;
    try {
      setWhoKnowsLoading(true);
      setWhoKnowsError(null);
      const data = await api.knowledge.whoKnows(whoKnowsText.trim());
      setWhoKnowsResults(data);
    } catch (e: any) {
      setWhoKnowsError(e.message || 'Who Knows query failed');
      setWhoKnowsResults(null);
    } finally {
      setWhoKnowsLoading(false);
    }
  };

  return (
    <div className="space-y-6">
      {/* Stats */}
      {!stats && !statsLoading ? (
        <div className="bg-card border border-border rounded p-4 md:p-6 text-center">
          <Database className="w-8 h-8 text-muted-foreground/70 mx-auto mb-2" />
          <p className="text-sm text-muted-foreground mb-1">Knowledge graph is empty</p>
          <p className="text-xs text-muted-foreground/70">Add agents or content to populate the knowledge graph.</p>
        </div>
      ) : (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          <div className="bg-card border border-border rounded p-4">
            <div className="text-xs text-muted-foreground uppercase tracking-wide mb-1">Nodes</div>
            <div className="text-2xl font-semibold text-foreground">
              {statsLoading ? '—' : stats ? stats.node_count.toLocaleString() : '—'}
            </div>
          </div>
          <div className="bg-card border border-border rounded p-4">
            <div className="text-xs text-muted-foreground uppercase tracking-wide mb-1">Edges</div>
            <div className="text-2xl font-semibold text-foreground">
              {statsLoading ? '—' : stats ? stats.edge_count.toLocaleString() : '—'}
            </div>
          </div>
          <div className="bg-card border border-border rounded p-4">
            <div className="text-xs text-muted-foreground uppercase tracking-wide mb-1">
              Query Results
            </div>
            <div className="text-2xl font-semibold text-foreground">{queryResults.length}</div>
          </div>
          <div className="bg-card border border-border rounded p-4">
            <div className="text-xs text-muted-foreground uppercase tracking-wide mb-1">
              Who Knows
            </div>
            <div className="text-sm text-muted-foreground">
              {whoKnowsResults ? 'Results loaded' : 'No query yet'}
            </div>
          </div>
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 md:gap-6">
        {/* Query */}
        <div className="bg-card border border-border rounded p-4 md:p-6">
          <h2 className="text-sm font-semibold text-foreground/80 mb-4 flex items-center gap-2">
            <Search className="w-4 h-4" />
            Query Knowledge
          </h2>
          <div className="flex gap-2 mb-4">
            <Input
              value={queryText}
              onChange={(e) => setQueryText(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleQuery()}
              placeholder="Search topics and content..."
              className="bg-background border-border text-foreground placeholder:text-muted-foreground/70"
            />
            <Button size="sm" onClick={handleQuery} disabled={queryLoading}>
              {queryLoading ? '...' : 'Search'}
            </Button>
          </div>
          {queryError && (
            <div className="text-xs text-amber-700 dark:text-amber-400 bg-amber-50 dark:bg-amber-950/20 border border-amber-200 dark:border-amber-900/50 rounded px-3 py-2 mb-3">
              {queryError.includes('502') || queryError.includes('503')
                ? 'Knowledge service is starting up. Try again in a moment.'
                : queryError}
            </div>
          )}
          <div className="space-y-2 max-h-80 overflow-y-auto">
            {queryResults.length === 0 && !queryLoading ? (
              <div className="text-sm text-muted-foreground text-center py-8">
                Enter a query to search the knowledge graph
              </div>
            ) : (
              queryResults.map((node, idx) => (
                <div
                  key={idx}
                  onClick={() => onSelectResult?.(node.label, node.kind)}
                  className="bg-background border border-border rounded p-3 hover:border-border transition-colors cursor-pointer"
                >
                  <div className="flex items-start justify-between mb-1">
                    <h3 className="text-sm font-medium text-foreground">{node.label}</h3>
                    <span className="text-[10px] font-mono text-muted-foreground bg-secondary px-1.5 py-0.5 rounded">
                      {node.kind}
                    </span>
                  </div>
                  {node.summary && (
                    <p className="text-xs text-muted-foreground mb-2">{node.summary}</p>
                  )}
                  <div className="flex items-center gap-2 text-[10px] text-muted-foreground/70">
                    <code>{node.source_type}</code>
                    <span>·</span>
                    <span>{formatDateTimeShort(node.updated_at)}</span>
                    {node.connections > 0 && (
                      <>
                        <span>·</span>
                        <span>{node.connections} connections</span>
                      </>
                    )}
                  </div>
                </div>
              ))
            )}
          </div>
        </div>

        {/* Who Knows */}
        <div className="bg-card border border-border rounded p-4 md:p-6">
          <h2 className="text-sm font-semibold text-foreground/80 mb-4 flex items-center gap-2">
            <Database className="w-4 h-4" />
            Who Knows
          </h2>

          <div className="flex gap-2 mb-4">
            <Input
              value={whoKnowsText}
              onChange={(e) => setWhoKnowsText(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleWhoKnows()}
              placeholder="Enter a topic..."
              className="bg-background border-border text-foreground placeholder:text-muted-foreground/70"
            />
            <Button size="sm" onClick={handleWhoKnows} disabled={whoKnowsLoading}>
              {whoKnowsLoading ? '...' : 'Find'}
            </Button>
          </div>
          {whoKnowsError && (
            <div className="text-xs text-amber-700 dark:text-amber-400 bg-amber-50 dark:bg-amber-950/20 border border-amber-200 dark:border-amber-900/50 rounded px-3 py-2 mb-3">
              {whoKnowsError.includes('502') || whoKnowsError.includes('503')
                ? 'Knowledge service is starting up. Try again in a moment.'
                : whoKnowsError}
            </div>
          )}
          <div className="space-y-2 max-h-80 overflow-y-auto">
            {whoKnowsResults === null ? (
              <div className="text-sm text-muted-foreground text-center py-8">
                Enter a topic to find agents with expertise
              </div>
            ) : (
              <div className="space-y-2">
                {(whoKnowsResults.agents || []).length === 0 ? (
                  <div className="text-sm text-muted-foreground text-center py-4">No agents found for this topic</div>
                ) : (
                  (whoKnowsResults.agents || []).map((agent: any) => (
                    <div key={agent.name} className="bg-background border border-border rounded p-3">
                      <div className="flex items-center justify-between mb-1">
                        <code className="text-sm text-foreground">{agent.name}</code>
                        <span className="text-xs text-muted-foreground">{Math.round((agent.confidence || 0) * 100)}%</span>
                      </div>
                      {agent.topics && (
                        <div className="flex flex-wrap gap-1 mt-1">
                          {agent.topics.map((t: string) => (
                            <span key={t} className="text-[10px] bg-secondary text-muted-foreground px-1.5 py-0.5 rounded">{t}</span>
                          ))}
                        </div>
                      )}
                    </div>
                  ))
                )}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Ontology Candidates */}
      {(ontologyCandidates.length > 0 || ontologyDecisions.length > 0) && (
        <div className="bg-card border border-border rounded p-4 md:p-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-sm font-semibold text-foreground/80 flex items-center gap-2">
              <Sparkles className="w-4 h-4" />
              Ontology Review
            </h2>
            <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={loadOntologyReviewData} disabled={ontologyLoading}>
              {ontologyLoading ? '...' : 'Refresh'}
            </Button>
          </div>
          <p className="text-xs text-muted-foreground mb-3">
            Emerged concepts from the knowledge graph. Promote or reject candidates, and restore recent ontology decisions when a judgment needs to be revisited.
          </p>
          {ontologyCandidates.length > 0 && (
            <div className="mb-5">
              <div className="mb-2 text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                Pending Candidates
              </div>
              <div className="space-y-2 max-h-72 overflow-y-auto">
                {ontologyCandidates.map((candidate, idx) => {
                  const val = candidateValue(candidate, `candidate_${idx}`);
                  const count = candidate.count ?? candidate.properties?.occurrence_count;
                  const source = candidate.source ?? (candidate.properties?.source_count ? 'knowledge' : undefined);
                  const status = candidateStatus(candidate);
                  return (
                    <div key={candidate.id || val} className="flex items-center justify-between bg-background border border-border rounded px-3 py-2">
                      <div className="flex-1 min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-sm text-foreground font-mono">{val}</span>
                          <span className="rounded-full bg-secondary px-2 py-0.5 text-[10px] uppercase tracking-[0.14em] text-muted-foreground">
                            {status}
                          </span>
                        </div>
                        <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                          {count != null && <span>{count} occurrences</span>}
                          {source && <span>from {source}</span>}
                          {candidate.candidate_type && <span>type: {candidate.candidate_type}</span>}
                        </div>
                      </div>
                      <div className="flex gap-1 ml-2 shrink-0">
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 w-7 p-0 text-green-500 hover:text-green-400 hover:bg-green-950/30"
                          onClick={() => handlePromote(candidate)}
                          disabled={ontologyActionLoading === candidate.id}
                          title="Promote to ontology"
                        >
                          <Check className="w-4 h-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 w-7 p-0 text-red-500 hover:text-red-400 hover:bg-red-950/30"
                          onClick={() => handleReject(candidate)}
                          disabled={ontologyActionLoading === candidate.id}
                          title="Reject candidate"
                        >
                          <X className="w-4 h-4" />
                        </Button>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {ontologyDecisions.length > 0 && (
            <div>
              <div className="mb-2 text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                Recent Decisions
              </div>
              <div className="space-y-2 max-h-72 overflow-y-auto">
                {ontologyDecisions.map((decision) => (
                  <div key={decision.id} className="flex items-center justify-between bg-background border border-border rounded px-3 py-2">
                    <div className="flex-1 min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-sm text-foreground font-mono">{decision.value}</span>
                        <span className="rounded-full bg-secondary px-2 py-0.5 text-[10px] uppercase tracking-[0.14em] text-muted-foreground">
                          {decision.action}
                        </span>
                      </div>
                      {decision.timestamp && (
                        <div className="mt-1 text-xs text-muted-foreground">
                          {formatDateTimeShort(decision.timestamp)}
                        </div>
                      )}
                    </div>
                    {decision.action !== 'restore' && (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-7 px-2 text-xs text-amber-600 hover:text-amber-500 hover:bg-amber-950/20"
                        onClick={() => handleRestore(decision.nodeId, decision.value)}
                        disabled={ontologyActionLoading === decision.nodeId}
                        title="Restore to ontology review"
                      >
                        <RotateCcw className="mr-1 h-3.5 w-3.5" />
                        Restore
                      </Button>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          {ontologyCandidates.length === 0 && ontologyDecisions.length === 0 && (
            <div className="text-sm text-muted-foreground text-center py-8">
              No ontology candidates or recent decisions
            </div>
          )}
          </div>
      )}
    </div>
  );
}
