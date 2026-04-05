import { useState, useEffect } from 'react';
import { api } from '../lib/api';
import { formatDateTimeShort } from '../lib/time';
import { Input } from '../components/ui/input';
import { Button } from '../components/ui/button';
import { Search, Database, Sparkles, Check, X } from 'lucide-react';
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

  const [ontologyCandidates, setOntologyCandidates] = useState<any[]>([]);
  const [ontologyLoading, setOntologyLoading] = useState(false);

  const loadOntologyCandidates = async () => {
    try {
      setOntologyLoading(true);
      const data = await api.knowledge.ontologyCandidates();
      setOntologyCandidates(data.candidates || []);
    } catch {
      // Ontology may not be available — ignore
    } finally {
      setOntologyLoading(false);
    }
  };

  const handlePromote = async (value: string) => {
    try {
      await api.knowledge.ontologyPromote(value);
      toast.success(`Promoted "${value}" to ontology`);
      setOntologyCandidates((prev) => prev.filter((c) => (c.value || c.label || c.name) !== value));
    } catch (e: any) {
      toast.error(e.message || 'Promote failed');
    }
  };

  const handleReject = async (value: string) => {
    try {
      await api.knowledge.ontologyReject(value);
      toast.success(`Rejected "${value}"`);
      setOntologyCandidates((prev) => prev.filter((c) => (c.value || c.label || c.name) !== value));
    } catch (e: any) {
      toast.error(e.message || 'Reject failed');
    }
  };

  useEffect(() => {
    loadOntologyCandidates();
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
      {ontologyCandidates.length > 0 && (
        <div className="bg-card border border-border rounded p-4 md:p-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-sm font-semibold text-foreground/80 flex items-center gap-2">
              <Sparkles className="w-4 h-4" />
              Ontology Candidates
            </h2>
            <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={loadOntologyCandidates} disabled={ontologyLoading}>
              {ontologyLoading ? '...' : 'Refresh'}
            </Button>
          </div>
          <p className="text-xs text-muted-foreground mb-3">
            Emerged concepts from the knowledge graph. Promote to add to the base ontology, or reject.
          </p>
          <div className="space-y-2 max-h-80 overflow-y-auto">
            {ontologyCandidates.map((candidate, idx) => {
              const val = candidate.value || candidate.label || candidate.name || `candidate_${idx}`;
              return (
                <div key={val} className="flex items-center justify-between bg-background border border-border rounded px-3 py-2">
                  <div className="flex-1 min-w-0">
                    <span className="text-sm text-foreground font-mono">{val}</span>
                    {candidate.count != null && (
                      <span className="text-xs text-muted-foreground ml-2">({candidate.count} occurrences)</span>
                    )}
                    {candidate.source && (
                      <span className="text-xs text-muted-foreground/70 ml-2">from {candidate.source}</span>
                    )}
                  </div>
                  <div className="flex gap-1 ml-2 shrink-0">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 w-7 p-0 text-green-500 hover:text-green-400 hover:bg-green-950/30"
                      onClick={() => handlePromote(val)}
                      title="Promote to ontology"
                    >
                      <Check className="w-4 h-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 w-7 p-0 text-red-500 hover:text-red-400 hover:bg-red-950/30"
                      onClick={() => handleReject(val)}
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
    </div>
  );
}
