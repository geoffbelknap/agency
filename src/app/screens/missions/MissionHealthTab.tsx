import { useState, useEffect, useMemo } from 'react';
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  BarChart,
  Bar,
  Cell,
  Legend,
} from 'recharts';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { Badge } from '@/app/components/ui/badge';
import { api } from '@/app/lib/api';
import type { EvaluationResult, ProcedureRecord, EpisodeRecord } from '@/app/types';

const PASSED_COLOR = 'hsl(155, 70%, 50%)';
const FAILED_COLOR = 'hsl(350, 80%, 60%)';

const TIER_COLORS: Record<string, string> = {
  minimal: 'hsl(38, 92%, 55%)',
  standard: 'hsl(192, 85%, 55%)',
  full: 'hsl(155, 70%, 50%)',
};

const TOOLTIP_STYLE = {
  backgroundColor: 'hsl(var(--card))',
  border: '1px solid hsl(var(--border))',
  borderRadius: '6px',
  fontSize: '12px',
};

function outcomeBadgeClass(passed: boolean | null, partial?: boolean): string {
  if (partial) return 'bg-amber-900/30 text-amber-400';
  if (passed) return 'bg-green-900/30 text-green-400';
  return 'bg-red-900/30 text-red-400';
}

function buildPassRateData(evaluations: EvaluationResult[]) {
  // Group by date (YYYY-MM-DD) and compute pass/fail counts
  const byDate: Record<string, { passed: number; failed: number }> = {};
  for (const ev of evaluations) {
    const day = ev.evaluated_at.slice(0, 10);
    if (!byDate[day]) byDate[day] = { passed: 0, failed: 0 };
    if (ev.passed) byDate[day].passed++;
    else byDate[day].failed++;
  }
  return Object.entries(byDate)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([date, counts]) => ({ date, ...counts }));
}

function buildTierData(evaluations: EvaluationResult[]) {
  // Derive tier from evaluation_mode as a proxy — show mode distribution
  const counts: Record<string, number> = { minimal: 0, standard: 0, full: 0 };
  for (const ev of evaluations) {
    if (ev.evaluation_mode === 'checklist_only') counts.minimal++;
    else if (ev.evaluation_mode === 'checklist_only_fallback') counts.standard++;
    else counts.full++;
  }
  return [
    { name: 'minimal', value: counts.minimal },
    { name: 'standard', value: counts.standard },
    { name: 'full', value: counts.full },
  ];
}

function EvaluationRow({ evaluation }: { evaluation: EvaluationResult }) {
  const [expanded, setExpanded] = useState(false);
  const passed = evaluation.passed;

  return (
    <>
      <tr
        className="border-b border-border hover:bg-secondary/50 cursor-pointer"
        onClick={() => setExpanded(!expanded)}
        tabIndex={0}
        role="button"
        onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setExpanded(!expanded); } }}
      >
        <td className="p-3 text-xs font-mono text-muted-foreground truncate max-w-[180px]">
          {evaluation.task_id}
        </td>
        <td className="p-3">
          <Badge className={outcomeBadgeClass(passed)}>
            {passed ? 'passed' : 'failed'}
          </Badge>
        </td>
        <td className="p-3 text-xs text-muted-foreground">
          {evaluation.evaluation_mode}
        </td>
        <td className="p-3 text-xs text-muted-foreground">
          {evaluation.criteria_results.length} criteria
        </td>
        <td className="p-3 text-xs text-muted-foreground">
          {new Date(evaluation.evaluated_at).toLocaleString()}
        </td>
        <td className="p-3 text-muted-foreground">
          {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
        </td>
      </tr>
      {expanded && (
        <tr className="border-b border-border bg-secondary/20">
          <td colSpan={6} className="p-4">
            <div className="space-y-2">
              {evaluation.criteria_results.map((cr) => (
                <div key={cr.id} className="flex items-start gap-3">
                  <Badge className={outcomeBadgeClass(cr.passed)} style={{ minWidth: '52px', justifyContent: 'center' }}>
                    {cr.passed ? 'pass' : 'fail'}
                  </Badge>
                  <div className="flex-1">
                    <span className="text-xs font-mono text-foreground/80">{cr.id}</span>
                    {cr.required && (
                      <span className="ml-2 text-[10px] text-amber-400">(required)</span>
                    )}
                    {cr.reasoning && (
                      <p className="text-xs text-muted-foreground mt-0.5">{cr.reasoning}</p>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

function InfraHealthPanel({ missionName }: { missionName: string }) {
  const [health, setHealth] = useState<any>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.missions.health(missionName)
      .then((data) => setHealth(data))
      .catch(() => setHealth(null))
      .finally(() => setLoading(false));
  }, [missionName]);

  if (loading) return null;
  if (!health || !health.checks || health.checks.length === 0) return null;

  const statusColor = health.status === 'healthy' ? 'text-emerald-400' : health.status === 'degraded' ? 'text-amber-400' : 'text-red-400';
  const borderColor = health.status === 'healthy' ? 'border-emerald-900/50' : health.status === 'degraded' ? 'border-amber-900/50' : 'border-red-900/50';

  return (
    <div className={`bg-card border ${borderColor} rounded-lg p-4 mb-6`}>
      <div className="flex items-center gap-2 mb-3">
        <span className={`text-sm font-medium ${statusColor}`}>
          Infrastructure: {health.status}
        </span>
        {health.summary && health.status !== 'healthy' && (
          <span className="text-xs text-muted-foreground">— {health.summary}</span>
        )}
      </div>
      <div className="space-y-1.5">
        {health.checks.map((check: any, i: number) => (
          <div key={i} className="flex items-start gap-2 text-xs">
            <span className={`flex-shrink-0 mt-0.5 ${check.status === 'pass' ? 'text-emerald-500' : check.status === 'warn' ? 'text-amber-500' : 'text-red-500'}`}>
              {check.status === 'pass' ? '✓' : check.status === 'warn' ? '!' : '✗'}
            </span>
            <span className="text-foreground font-medium w-[160px] flex-shrink-0">{check.name}</span>
            <span className="text-muted-foreground">{check.detail}</span>
            {check.fix && check.status === 'fail' && (
              <span className="ml-auto text-muted-foreground/60 font-mono text-[10px] truncate max-w-[300px]" title={check.fix}>
                {check.fix}
              </span>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

export function MissionHealthTab({ missionName }: { missionName: string }) {
  const [evaluations, setEvaluations] = useState<EvaluationResult[]>([]);
  const [procedures, setProcedures] = useState<ProcedureRecord[]>([]);
  const [episodes, setEpisodes] = useState<EpisodeRecord[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    Promise.allSettled([
      api.missions.evaluations(missionName, { limit: 50 }),
      api.missions.procedures(missionName),
      api.missions.episodes(missionName),
    ]).then(([evRes, procRes, epRes]) => {
      if (evRes.status === 'fulfilled') {
        setEvaluations(evRes.value.evaluations ?? []);
      }
      if (procRes.status === 'fulfilled') {
        setProcedures(procRes.value.procedures ?? []);
      }
      if (epRes.status === 'fulfilled') {
        setEpisodes(epRes.value.episodes ?? []);
      }
      setLoading(false);
    });
  }, [missionName]);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-40 text-muted-foreground text-sm">
        Loading health data…
      </div>
    );
  }

  if (evaluations.length === 0 && procedures.length === 0 && episodes.length === 0) {
    return (
      <div>
        <InfraHealthPanel missionName={missionName} />
        <div className="text-sm text-muted-foreground py-6 text-center">
          No evaluation, procedure, or episode data yet. Quality metrics will appear here as tasks are completed.
        </div>
      </div>
    );
  }

  const { passRateData, tierData, totalPassed, totalFailed, passRate } = useMemo(() => {
    const passRateData = buildPassRateData(evaluations);
    const tierData = buildTierData(evaluations);
    const totalPassed = evaluations.filter((e) => e.passed).length;
    const totalFailed = evaluations.length - totalPassed;
    const passRate = evaluations.length > 0
      ? Math.round((totalPassed / evaluations.length) * 100)
      : null;
    return { passRateData, tierData, totalPassed, totalFailed, passRate };
  }, [evaluations]);

  return (
    <div className="space-y-6 p-4">
      {/* Summary stats */}
      {evaluations.length > 0 && (
        <div className="grid grid-cols-3 gap-3">
          <div className="border border-border rounded-lg p-4 bg-card">
            <div className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Pass Rate</div>
            <div className="text-2xl font-semibold text-green-400" style={{ fontVariantNumeric: 'tabular-nums' }}>{passRate}%</div>
          </div>
          <div className="border border-border rounded-lg p-4 bg-card">
            <div className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Passed</div>
            <div className="text-2xl font-semibold text-green-400" style={{ fontVariantNumeric: 'tabular-nums' }}>{totalPassed}</div>
          </div>
          <div className="border border-border rounded-lg p-4 bg-card">
            <div className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Failed</div>
            <div className="text-2xl font-semibold text-red-400" style={{ fontVariantNumeric: 'tabular-nums' }}>{totalFailed}</div>
          </div>
        </div>
      )}

      {/* Evaluation pass rate chart */}
      {passRateData.length > 0 && (
        <div className="border border-border rounded-lg p-4 bg-card">
          <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-4">
            Evaluation Pass Rate
          </h3>
          <ResponsiveContainer width="100%" height={200}>
            <AreaChart data={passRateData} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
              <XAxis dataKey="date" tick={{ fontSize: 10 }} stroke="hsl(var(--muted-foreground))" />
              <YAxis tick={{ fontSize: 10 }} stroke="hsl(var(--muted-foreground))" allowDecimals={false} />
              <Tooltip contentStyle={TOOLTIP_STYLE} />
              <Area
                type="monotone"
                dataKey="passed"
                stackId="1"
                stroke={PASSED_COLOR}
                fill={PASSED_COLOR}
                fillOpacity={0.4}
                name="Passed"
              />
              <Area
                type="monotone"
                dataKey="failed"
                stackId="1"
                stroke={FAILED_COLOR}
                fill={FAILED_COLOR}
                fillOpacity={0.4}
                name="Failed"
              />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      )}

      {/* Task tier distribution */}
      {evaluations.length > 0 && (
        <div className="border border-border rounded-lg p-4 bg-card">
          <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-4">
            Evaluation Mode Distribution
          </h3>
          <ResponsiveContainer width="100%" height={120}>
            <BarChart
              data={tierData}
              layout="vertical"
              margin={{ top: 0, right: 16, bottom: 0, left: 16 }}
            >
              <XAxis type="number" tick={{ fontSize: 10 }} stroke="hsl(var(--muted-foreground))" allowDecimals={false} />
              <YAxis type="category" dataKey="name" tick={{ fontSize: 10 }} stroke="hsl(var(--muted-foreground))" width={64} />
              <Tooltip contentStyle={TOOLTIP_STYLE} />
              <Legend wrapperStyle={{ fontSize: '10px' }} />
              <Bar dataKey="value" name="Count" radius={[0, 3, 3, 0]}>
                {tierData.map((entry) => (
                  <Cell key={entry.name} fill={TIER_COLORS[entry.name] ?? PASSED_COLOR} />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}

      {/* Recent evaluations table */}
      <div className="border border-border rounded-lg bg-card overflow-hidden">
        <div className="px-4 py-3 border-b border-border">
          <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
            Recent Evaluations
          </h3>
        </div>
        {evaluations.length === 0 ? (
          <div className="px-4 py-6 text-sm text-muted-foreground italic text-center">
            No evaluations recorded yet
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm min-w-[520px]">
              <thead>
                <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
                  <th className="text-left p-3 font-medium">Task ID</th>
                  <th className="text-left p-3 font-medium">Outcome</th>
                  <th className="text-left p-3 font-medium">Mode</th>
                  <th className="text-left p-3 font-medium">Criteria</th>
                  <th className="text-left p-3 font-medium">Evaluated At</th>
                  <th className="p-3" />
                </tr>
              </thead>
              <tbody>
                {evaluations.map((ev) => (
                  <EvaluationRow key={ev.task_id} evaluation={ev} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Mission procedures */}
      <div className="border border-border rounded-lg p-4 bg-card">
        <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-3">
          Procedures ({procedures.length})
        </h3>
        {procedures.length === 0 ? (
          <p className="text-sm text-muted-foreground italic">No procedures recorded</p>
        ) : (
          <div className="space-y-2">
            {procedures.map((proc) => (
              <div
                key={proc.task_id}
                className="flex items-start gap-3 py-2 border-b border-border last:border-0"
              >
                <Badge className={outcomeBadgeClass(proc.outcome === 'success', proc.outcome === 'partial')}>
                  {proc.outcome}
                </Badge>
                <div className="flex-1 min-w-0">
                  <div className="text-xs font-mono text-foreground/80 truncate">{proc.task_type}</div>
                  <div className="text-xs text-muted-foreground truncate">{proc.approach}</div>
                  {proc.lessons.length > 0 && (
                    <div className="text-[10px] text-muted-foreground/70 mt-0.5">
                      {proc.lessons.join(' · ')}
                    </div>
                  )}
                </div>
                <div className="text-[10px] text-muted-foreground whitespace-nowrap">
                  {proc.duration_minutes}m
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Mission episodes */}
      <div className="border border-border rounded-lg p-4 bg-card">
        <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-3">
          Episodes ({episodes.length})
        </h3>
        {episodes.length === 0 ? (
          <p className="text-sm text-muted-foreground italic">No episodes recorded</p>
        ) : (
          <div className="space-y-2">
            {episodes.map((ep) => (
              <div
                key={ep.task_id}
                className="flex items-start gap-3 py-2 border-b border-border last:border-0"
              >
                <Badge
                  className={outcomeBadgeClass(
                    ep.outcome === 'success',
                    ep.outcome === 'partial',
                  )}
                >
                  {ep.outcome}
                </Badge>
                <div className="flex-1 min-w-0">
                  <div className="text-xs text-foreground/80 line-clamp-2">{ep.summary}</div>
                  {ep.tags.length > 0 && (
                    <div className="flex flex-wrap gap-1 mt-1">
                      {ep.tags.map((tag) => (
                        <span
                          key={tag}
                          className="inline-flex items-center rounded px-1 py-0.5 text-[10px] bg-secondary text-muted-foreground"
                        >
                          {tag}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
                <div className="text-[10px] text-muted-foreground whitespace-nowrap">
                  {ep.duration_minutes}m
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
