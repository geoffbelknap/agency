import { useState, useEffect, useCallback } from 'react';
import { Link } from 'react-router';
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
        fix: c.fix || '',
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
  const platformIssues = groups.find((group) => group.name === '(platform)' && !group.allPass) ?? null;
  const firstAgentIssue = groups.find((group) => group.name !== '(platform)' && !group.allPass) ?? null;

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
    <div className="space-y-6">
      <div className="rounded-2xl border border-border bg-card px-4 py-4 md:px-5">
        <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
          <div className="space-y-1">
            <div className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">Runtime doctor</div>
            <p className="text-sm text-muted-foreground">Run operator-safe health checks across the shared platform and active agents.</p>
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              {!loading && totalAgents > 0 && (
                <span
                  className={`inline-block h-2 w-2 rounded-full ${issueCount === 0 ? 'bg-emerald-500' : 'bg-amber-500'}`}
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
          </div>
          <Button size="sm" onClick={runDoctor} disabled={loading} className="h-9 self-start md:self-auto">
            {loading ? 'Running...' : 'Run Doctor'}
          </Button>
        </div>
      </div>

      {error && (
        <div className="text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
          {error}
        </div>
      )}

      {!loading && !error && issueCount > 0 && (
        <div className="space-y-3 rounded-2xl border border-border bg-card p-4">
          <div className="flex items-center gap-2 text-sm text-amber-400">
            <AlertTriangle className="w-4 h-4" />
            <span>{issueCount === 1 ? '1 issue needs attention' : `${issueCount} issues need attention`}</span>
          </div>
          <div className="text-xs text-muted-foreground">
            Start with the shared platform if infrastructure checks are failing. If the issue is scoped to one agent, jump straight into that agent’s detail view.
          </div>
          <div className="flex flex-wrap gap-2">
            {platformIssues && (
              <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                <Link to="/admin/infrastructure">Open Infrastructure</Link>
              </Button>
            )}
            {firstAgentIssue && (
              <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                <Link to={`/agents/${encodeURIComponent(firstAgentIssue.name)}`}>Open Agent: {firstAgentIssue.name}</Link>
              </Button>
            )}
            <Button size="sm" className="h-8 text-xs" onClick={runDoctor} disabled={loading}>
              Re-run checks
            </Button>
          </div>
        </div>
      )}

      {loading ? (
        <div className="text-sm text-muted-foreground text-center py-12">
          Running doctor checks...
        </div>
      ) : groups.length === 0 ? (
        <div className="text-sm text-muted-foreground text-center py-12">No checks returned</div>
      ) : (
        <div className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-4">
          {groups.map((group) => (
            <div key={group.name} className="contents">
              <button
                onClick={() => handleCardClick(group.name)}
                className={`cursor-pointer rounded-2xl border bg-card p-4 text-left transition-colors hover:bg-secondary/40 ${
                  group.allPass ? 'border-emerald-500/40' : 'border-amber-500/40'
                } ${expandedAgent === group.name ? 'ring-1 ring-border' : ''}`}
              >
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">Checks</div>
                    <div
                      className={`mt-1 text-2xl font-semibold ${group.allPass ? 'text-emerald-400' : 'text-amber-400'}`}
                    >
                      {group.passed}/{group.total}
                    </div>
                  </div>
                  <div className={`mt-1 h-2.5 w-2.5 rounded-full ${group.allPass ? 'bg-emerald-500' : 'bg-amber-500'}`} />
                </div>
                <div className="mt-3 truncate font-mono text-xs text-muted-foreground">
                  {group.name}
                </div>
              </button>

              {expandedAgent === group.name && (
                <div className="col-span-2 space-y-2 rounded-2xl border border-border bg-card p-4 md:col-span-3 lg:col-span-4">
                  <h3 className="mb-3 text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                    <code className="normal-case">{group.name}</code> — checks
                  </h3>
                  {group.checks.map((check) => (
                    <div
                      key={check.id}
                      className="flex items-start gap-3 rounded-2xl bg-background p-3 text-sm"
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
                        {check.fix && check.status !== 'pass' && (
                          <div className="mt-1 text-xs text-foreground/70">
                            Fix: <span className="text-muted-foreground">{check.fix}</span>
                          </div>
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
