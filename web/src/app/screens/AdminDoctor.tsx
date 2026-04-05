import { useState, useEffect, useCallback } from 'react';
import { api } from '../lib/api';
import { DoctorCheck } from '../types';
import { Button } from '../components/ui/button';
import { CheckCircle, AlertTriangle, XCircle } from 'lucide-react';

interface AgentGroup {
  name: string;
  checks: DoctorCheck[];
  passed: number;
  total: number;
  allPass: boolean;
}

export function AdminDoctor() {
  const [checks, setChecks] = useState<DoctorCheck[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastRun, setLastRun] = useState<string | null>(null);
  const [expandedAgent, setExpandedAgent] = useState<string | null>(null);

  const runDoctor = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await api.admin.doctor();
      const mapped: DoctorCheck[] = (data.checks || []).map((c: any) => ({
        id: c.name + (c.agent || ''),
        agentName: c.agent,
        name: c.name,
        status: c.status,
        message: c.detail || '',
      }));
      setChecks(mapped);
      setLastRun(new Date().toLocaleTimeString());
    } catch (e: any) {
      setError(e.message || 'Doctor check failed');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    runDoctor();
  }, [runDoctor]);

  // Group checks by agent name
  const groups: AgentGroup[] = Object.entries(
    checks.reduce((acc, check) => {
      const key = check.agentName || '(platform)';
      if (!acc[key]) acc[key] = [];
      acc[key].push(check);
      return acc;
    }, {} as Record<string, DoctorCheck[]>)
  ).map(([name, agentChecks]) => {
    const passed = agentChecks.filter((c) => c.status === 'pass').length;
    return {
      name,
      checks: agentChecks,
      passed,
      total: agentChecks.length,
      allPass: passed === agentChecks.length,
    };
  });

  const totalAgents = groups.length;
  const issueCount = groups.filter((g) => !g.allPass).length;

  const summaryText =
    totalAgents === 0
      ? 'No checks returned'
      : issueCount === 0
        ? `${totalAgents} ${totalAgents === 1 ? 'agent' : 'agents'} · all checks passing`
        : `${totalAgents} ${totalAgents === 1 ? 'agent' : 'agents'} · ${issueCount} ${issueCount === 1 ? 'issue' : 'issues'}`;

  const handleCardClick = (agentName: string) => {
    setExpandedAgent(expandedAgent === agentName ? null : agentName);
  };

  return (
    <div className="space-y-4">
      {/* Header row */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          {!loading && totalAgents > 0 && (
            <span
              className={`inline-block w-2 h-2 rounded-full ${issueCount === 0 ? 'bg-emerald-500' : 'bg-amber-500'}`}
            />
          )}
          <span>
            {loading
              ? 'Running checks...'
              : lastRun
                ? `${summaryText} · last run ${lastRun}`
                : summaryText}
          </span>
        </div>
        <Button size="sm" onClick={runDoctor} disabled={loading}>
          {loading ? 'Running...' : 'Run Doctor'}
        </Button>
      </div>

      {/* Error */}
      {error && (
        <div className="text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
          {error}
        </div>
      )}

      {/* Loading */}
      {loading ? (
        <div className="text-sm text-muted-foreground text-center py-12">
          Running doctor checks...
        </div>
      ) : groups.length === 0 ? (
        <div className="text-sm text-muted-foreground text-center py-12">No checks returned</div>
      ) : (
        <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-3">
          {groups.map((group) => (
            <div key={group.name} className="contents">
              {/* Summary card */}
              <button
                onClick={() => handleCardClick(group.name)}
                className={`bg-card border rounded-lg p-4 text-center cursor-pointer hover:bg-secondary/40 transition-colors border-l-[3px] ${
                  group.allPass ? 'border-l-emerald-500' : 'border-l-amber-500'
                } ${expandedAgent === group.name ? 'ring-1 ring-border' : ''}`}
              >
                <div
                  className={`text-2xl font-bold ${group.allPass ? 'text-emerald-400' : 'text-amber-400'}`}
                >
                  {group.passed}/{group.total}
                </div>
                <div className="text-xs text-muted-foreground font-mono mt-1 truncate">
                  {group.name}
                </div>
              </button>

              {/* Expanded check list — spans full row when open */}
              {expandedAgent === group.name && (
                <div className="col-span-2 md:col-span-3 lg:col-span-4 bg-card border border-border rounded-lg p-4 space-y-2">
                  <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-3">
                    <code className="normal-case">{group.name}</code> — checks
                  </h3>
                  {group.checks.map((check) => (
                    <div
                      key={check.id}
                      className="flex items-start gap-3 text-sm bg-background rounded p-3"
                    >
                      <div className="pt-0.5 shrink-0">
                        {check.status === 'pass' ? (
                          <CheckCircle className="w-4 h-4 text-emerald-500" />
                        ) : check.status === 'warn' ? (
                          <AlertTriangle className="w-4 h-4 text-amber-500" />
                        ) : (
                          <XCircle className="w-4 h-4 text-red-500" />
                        )}
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="text-foreground/80">{check.name}</div>
                        {check.message && (
                          <div className="text-xs text-muted-foreground mt-0.5">{check.message}</div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
