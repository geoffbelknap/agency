import { useState, useEffect, useCallback } from 'react';
import { Link } from 'react-router';
import { Button } from '../components/ui/button';
import { Calendar } from '../components/ui/calendar';
import { Popover, PopoverContent, PopoverTrigger } from '../components/ui/popover';
import { AlertTriangle, CalendarIcon, Check, RefreshCw, X } from 'lucide-react';
import type { DateRange } from 'react-day-picker';
import { toast } from 'sonner';
import { api, type RawRoutingStat, type RawRoutingSuggestion } from '../lib/api';

// Approximate pricing per million tokens (USD)
const PRICING: Record<string, { input: number; output: number }> = {
  'claude-sonnet': { input: 3, output: 15 },
  'claude-haiku': { input: 0.25, output: 1.25 },
  'claude-opus': { input: 15, output: 75 },
};
const DEFAULT_PRICING = { input: 3, output: 15 };

interface MetricsBucket {
  requests: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  est_cost_usd: number;
  errors: number;
  avg_latency_ms: number;
  p95_latency_ms?: number;
  provider_tool_calls?: number;
  provider_tool_cost_usd?: number;
  provider_tool_unpriced_calls?: number;
  provider_tool_cost_confidence?: string;
  provider_tool_cost_source?: string;
}

interface RecentError {
  ts: string;
  agent: string;
  model: string;
  status: number;
  error: string;
}

interface RoutingMetrics {
  period: { since: string; until: string };
  totals: MetricsBucket;
  by_agent: Record<string, MetricsBucket>;
  by_model: Record<string, MetricsBucket>;
  by_provider: Record<string, MetricsBucket>;
  by_source?: Record<string, MetricsBucket>;
  by_provider_tool?: Record<string, MetricsBucket>;
  recent_errors?: RecentError[];
}

function routingErrorHint(error: RecentError): string {
  const text = `${error.status} ${error.error}`.toLowerCase();
  if (error.status === 429 || text.includes('rate limit')) {
    return 'Looks like a provider or rate-limit issue. Check the affected agent first, then review model/provider usage and retry pressure.';
  }
  if (error.status === 401 || error.status === 403 || text.includes('auth')) {
    return 'Looks like an authentication or permission issue. Check credentials and provider setup before changing routing policy.';
  }
  return 'Start with the affected agent path, then use Doctor if the failure looks broader than one model or task route.';
}

function estimateCost(model: string, input: number, output: number): number {
  const p = PRICING[model] || DEFAULT_PRICING;
  return (input * p.input + output * p.output) / 1_000_000;
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

type RangePreset = '24h' | '7d' | '30d' | 'custom';

const RANGE_PRESETS: { key: RangePreset; label: string }[] = [
  { key: '24h', label: '24h' },
  { key: '7d', label: '7d' },
  { key: '30d', label: '30d' },
  { key: 'custom', label: 'Custom' },
];

function presetToSince(preset: RangePreset): string {
  const now = new Date();
  switch (preset) {
    case '24h': return new Date(now.getTime() - 24 * 60 * 60 * 1000).toISOString();
    case '7d': return new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000).toISOString();
    case '30d': return new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000).toISOString();
    default: return '';
  }
}

function formatDateShort(d: Date): string {
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

function formatSavingsPercent(value: number): string {
  if (!Number.isFinite(value)) return '0%';
  return `${Math.round(value * 100)}%`;
}

function formatSavingsUSD(value: number): string {
  if (!Number.isFinite(value)) return '$0.0000';
  return `$${value.toFixed(4)}`;
}

function formatProviderToolMeta(value?: string): string {
  if (!value) return 'unknown';
  return value.split(',').map((part) => part.trim()).filter(Boolean).join(', ') || 'unknown';
}

function isNotFoundError(err: unknown): boolean {
  return err instanceof Error && (err.message.includes('404') || err.message.toLowerCase().includes('not found'));
}

async function fetchMetrics(since?: string, until?: string): Promise<RoutingMetrics> {
  const configRes = await fetch('/__agency/config');
  let base = '/api/v1';
  let token = '';
  if (configRes.ok) {
    const cfg = await configRes.json();
    if (cfg.token) token = cfg.token;
    if (cfg.gateway) base = `${cfg.gateway}/api/v1`;
  }
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (token) headers['Authorization'] = `Bearer ${token}`;
  const params = new URLSearchParams();
  if (since) params.set('since', since);
  if (until) params.set('until', until);
  const qs = params.toString();
  const res = await fetch(`${base}/infra/routing/metrics${qs ? `?${qs}` : ''}`, { headers });
  if (!res.ok) throw new Error(`metrics: ${res.status}`);
  return res.json();
}

export function Usage() {
  const [metrics, setMetrics] = useState<RoutingMetrics | null>(null);
  const [suggestions, setSuggestions] = useState<RawRoutingSuggestion[]>([]);
  const [routingStats, setRoutingStats] = useState<RawRoutingStat[]>([]);
  const [suggestionsLoading, setSuggestionsLoading] = useState(true);
  const [statsLoading, setStatsLoading] = useState(true);
  const [suggestionAction, setSuggestionAction] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [preset, setPreset] = useState<RangePreset>('24h');
  const [dateRange, setDateRange] = useState<DateRange | undefined>();
  const [calendarOpen, setCalendarOpen] = useState(false);

  const load = useCallback(async (p?: RangePreset, range?: DateRange) => {
    const activePreset = p ?? preset;
    const activeRange = range ?? dateRange;
    setRefreshing(true);
    setError(null);
    try {
      let since: string | undefined;
      let until: string | undefined;
      if (activePreset === 'custom' && activeRange?.from) {
        since = activeRange.from.toISOString();
        if (activeRange.to) {
          // End of the selected day
          const end = new Date(activeRange.to);
          end.setHours(23, 59, 59, 999);
          until = end.toISOString();
        }
      } else if (activePreset !== 'custom') {
        since = presetToSince(activePreset);
      }
      const data = await fetchMetrics(since, until);
      setMetrics(data);
    } catch (err: any) {
      setError(err.message || 'Failed to load metrics');
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, [preset, dateRange]);

  const loadSuggestions = useCallback(async () => {
    setSuggestionsLoading(true);
    try {
      setSuggestions(await api.routing.suggestions('pending'));
    } catch (err: any) {
      if (isNotFoundError(err)) {
        setSuggestions([]);
        return;
      }
      toast.error(err.message || 'Failed to load routing suggestions');
    } finally {
      setSuggestionsLoading(false);
    }
  }, []);

  const loadRoutingStats = useCallback(async () => {
    setStatsLoading(true);
    try {
      setRoutingStats(await api.routing.stats());
    } catch (err: any) {
      if (isNotFoundError(err)) {
        setRoutingStats([]);
        return;
      }
      toast.error(err.message || 'Failed to load routing stats');
    } finally {
      setStatsLoading(false);
    }
  }, []);

  async function handleSuggestionAction(id: string, action: 'approve' | 'reject') {
    setSuggestionAction(`${action}:${id}`);
    try {
      if (action === 'approve') {
        await api.routing.approveSuggestion(id);
        toast.success('Routing suggestion approved');
      } else {
        await api.routing.rejectSuggestion(id);
        toast.success('Routing suggestion rejected');
      }
      await loadSuggestions();
    } catch (err: any) {
      toast.error(err.message || `Failed to ${action} suggestion`);
    } finally {
      setSuggestionAction(null);
    }
  }

  useEffect(() => {
    load();
    loadSuggestions();
    loadRoutingStats();
  }, []);

  function handlePreset(p: RangePreset) {
    setPreset(p);
    if (p !== 'custom') {
      setDateRange(undefined);
      load(p);
    }
  }

  function handleDateRangeSelect(range: DateRange | undefined) {
    setDateRange(range);
    if (range?.from && range?.to) {
      setCalendarOpen(false);
      load('custom', range);
    }
  }

  const t = metrics?.totals;
  const byAgent = metrics?.by_agent ? Object.entries(metrics.by_agent) : [];
  const byModel = metrics?.by_model ? Object.entries(metrics.by_model) : [];
  const byProvider = metrics?.by_provider ? Object.entries(metrics.by_provider) : [];
  const bySource = metrics?.by_source ? Object.entries(metrics.by_source) : [];
  const byProviderTool = metrics?.by_provider_tool ? Object.entries(metrics.by_provider_tool) : [];
  const recentErrors = metrics?.recent_errors ?? [];
  const primaryErroredAgent = recentErrors.find((entry) => entry.agent)?.agent;
  const providerToolCost = t?.provider_tool_cost_usd ?? 0;
  const providerToolCalls = t?.provider_tool_calls ?? 0;
  const unpricedProviderToolCalls = t?.provider_tool_unpriced_calls ?? 0;
  const tokenCost = t ? Math.max(0, (t.est_cost_usd || 0) - providerToolCost) : 0;
  const knownProviderToolCalls = Math.max(0, providerToolCalls - unpricedProviderToolCalls);
  const summaryMetrics = t ? [
    { label: 'Requests', value: t.requests.toLocaleString(), tone: 'text-foreground' },
    { label: 'Total tokens', value: formatTokens(t.total_tokens), tone: 'text-foreground' },
    { label: 'Estimated spend', value: displayCost(t), tone: 'text-primary' },
    { label: 'Token spend', value: `$${tokenCost.toFixed(4)}`, tone: 'text-muted-foreground-strong' },
    { label: 'Provider tools', value: `$${providerToolCost.toFixed(4)}`, tone: providerToolCalls > 0 ? 'text-primary' : 'text-muted-foreground-strong', detail: `${knownProviderToolCalls.toLocaleString()} priced / ${unpricedProviderToolCalls.toLocaleString()} unpriced` },
    { label: 'Avg latency', value: `${(t.avg_latency_ms / 1000).toFixed(1)}s`, tone: 'text-muted-foreground-strong', detail: t.p95_latency_ms != null ? `p95 ${(t.p95_latency_ms / 1000).toFixed(1)}s` : undefined },
  ] : [];

  // Use gateway cost if available, otherwise estimate client-side
  function displayCost(bucket: MetricsBucket, model?: string): string {
    if (bucket.est_cost_usd > 0) return `$${bucket.est_cost_usd.toFixed(4)}`;
    if (model) return `$${estimateCost(model, bucket.input_tokens, bucket.output_tokens).toFixed(4)}`;
    // Sum across models for total
    const totalEst = byModel.reduce((sum, [m, b]) => sum + estimateCost(m, b.input_tokens, b.output_tokens), 0);
    return `~$${totalEst.toFixed(4)}`;
  }

  return (
    <div className="space-y-6">
      <div className="rounded-2xl border border-border bg-card px-4 py-4 md:px-5">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-1">
            <div className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
              Usage overview
            </div>
            <p className="text-sm text-muted-foreground">Model traffic, spend, and routing quality for the selected period.</p>
            {metrics?.period && (
              <p className="text-[11px] text-muted-foreground/70">
                {new Date(metrics.period.since).toLocaleDateString()} — {new Date(metrics.period.until).toLocaleDateString()}
              </p>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <div className="inline-flex flex-wrap items-center gap-1 rounded-2xl border border-border/80 bg-secondary/55 p-1">
              {RANGE_PRESETS.filter(p => p.key !== 'custom').map(p => (
                <Button
                  key={p.key}
                  variant={preset === p.key ? 'default' : 'ghost'}
                  size="sm"
                  className="h-8 rounded-xl px-3 text-xs"
                  onClick={() => handlePreset(p.key)}
                >
                  {p.label}
                </Button>
              ))}
              <Popover open={calendarOpen} onOpenChange={setCalendarOpen}>
                <PopoverTrigger asChild>
                  <Button
                    variant={preset === 'custom' ? 'default' : 'ghost'}
                    size="sm"
                    className="h-8 gap-1 rounded-xl px-3 text-xs"
                    onClick={() => { setPreset('custom'); setCalendarOpen(true); }}
                  >
                    <CalendarIcon className="w-3 h-3" />
                    {preset === 'custom' && dateRange?.from
                      ? `${formatDateShort(dateRange.from)}${dateRange.to ? ` – ${formatDateShort(dateRange.to)}` : ''}`
                      : 'Custom'}
                  </Button>
                </PopoverTrigger>
                <PopoverContent className="w-auto p-0" align="end">
                  <Calendar
                    mode="range"
                    selected={dateRange}
                    onSelect={handleDateRangeSelect}
                    numberOfMonths={2}
                    disabled={{ after: new Date() }}
                    defaultMonth={dateRange?.from ?? new Date(Date.now() - 30 * 24 * 60 * 60 * 1000)}
                  />
                </PopoverContent>
              </Popover>
            </div>
            <Button variant="outline" size="sm" className="h-8 rounded-xl px-3" onClick={() => load()} disabled={refreshing}>
              <RefreshCw className={`w-3 h-3 ${refreshing ? 'animate-spin' : ''}`} />
            </Button>
          </div>
        </div>
      </div>

      {error && (
        <div className="text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">{error}</div>
      )}

      {loading ? (
        <div className="text-sm text-muted-foreground text-center py-12">Loading usage data...</div>
      ) : !t ? (
        <div className="text-sm text-muted-foreground text-center py-12">No metrics available</div>
      ) : (
        <>
          {recentErrors.length > 0 && (
            <div className="rounded-lg border border-red-900/40 bg-red-950/20 p-4">
              <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                <div className="space-y-1">
                  <div className="flex items-center gap-2 text-sm font-medium text-red-300">
                    <AlertTriangle className="h-4 w-4" />
                    {recentErrors.length} recent routing error{recentErrors.length !== 1 ? 's' : ''} need attention
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Review the affected agent path first, then use Doctor if the failure is broader than one route or model.
                  </p>
                </div>
                <div className="flex flex-wrap gap-2">
                  {primaryErroredAgent && (
                    <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                      <Link to={`/agents/${primaryErroredAgent}`}>Open Agent: {primaryErroredAgent}</Link>
                    </Button>
                  )}
                  <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                    <Link to="/admin/doctor">Open Doctor</Link>
                  </Button>
                </div>
              </div>
            </div>
          )}

          <div className="overflow-hidden rounded-2xl border border-border bg-card">
            <div className="grid gap-0 lg:grid-cols-[1.5fr_1fr]">
              <div className="border-b border-border px-4 py-4 lg:border-b-0 lg:border-r lg:px-5">
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                  {summaryMetrics.slice(0, 3).map((metric) => (
                    <div key={metric.label} className="space-y-1.5">
                      <div className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                        {metric.label}
                      </div>
                      <div className={`text-2xl font-semibold tracking-tight ${metric.tone}`}>
                        {metric.value}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
              <div className="grid grid-cols-1 gap-0 divide-y divide-border sm:grid-cols-3 sm:divide-x sm:divide-y-0">
                {summaryMetrics.slice(3).map((metric) => (
                  <div key={metric.label} className="px-4 py-4">
                    <div className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                      {metric.label}
                    </div>
                    <div className={`mt-1 text-lg font-semibold ${metric.tone}`}>
                      {metric.value}
                    </div>
                    {metric.detail && (
                      <div className="mt-1 text-xs text-muted-foreground">{metric.detail}</div>
                    )}
                  </div>
                ))}
              </div>
            </div>
            {t.errors > 0 && (
              <div className="border-t border-border bg-red-50/50 px-4 py-2 text-sm text-red-700 dark:bg-red-950/20 dark:text-red-300">
                {t.errors} request errors recorded in the selected period.
              </div>
            )}
            {unpricedProviderToolCalls > 0 && (
              <div className="border-t border-border bg-amber-50/70 px-4 py-2 text-sm text-amber-800 dark:bg-amber-950/25 dark:text-amber-300">
                {unpricedProviderToolCalls} provider-tool call{unpricedProviderToolCalls === 1 ? '' : 's'} had unknown pricing and may not be fully reflected in estimated spend.
              </div>
            )}
          </div>

          {byProviderTool.length > 0 && (
            <div className="bg-card border border-border rounded overflow-hidden">
              <div className="px-4 py-3 border-b border-border">
                <h3 className="text-sm font-medium text-foreground">Provider tool economics</h3>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  Provider-side tool calls separated from token spend. Known cost is included in estimated spend; unpriced calls are budget exposure.
                </p>
                <div className="mt-3 grid gap-2 sm:grid-cols-3">
                  <div className="rounded-xl border border-border bg-secondary/45 px-3 py-2">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-muted-foreground">Known provider-tool spend</div>
                    <div className="mt-1 text-lg font-semibold text-foreground">${providerToolCost.toFixed(4)}</div>
                  </div>
                  <div className="rounded-xl border border-border bg-secondary/45 px-3 py-2">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-muted-foreground">Priced calls</div>
                    <div className="mt-1 text-lg font-semibold text-foreground">{knownProviderToolCalls.toLocaleString()}</div>
                  </div>
                  <div className="rounded-xl border border-amber-200 bg-amber-50/70 px-3 py-2 dark:border-amber-900/40 dark:bg-amber-950/20">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-amber-700 dark:text-amber-400">Unpriced exposure</div>
                    <div className="mt-1 text-lg font-semibold text-amber-700 dark:text-amber-300">{unpricedProviderToolCalls.toLocaleString()}</div>
                  </div>
                </div>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full text-sm min-w-[820px]">
                  <thead>
                    <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
                      <th className="text-left p-3 font-medium">Capability</th>
                      <th className="text-right p-3 font-medium">Requests</th>
                      <th className="text-right p-3 font-medium">Tool Calls</th>
                      <th className="text-left p-3 font-medium">Pricing</th>
                      <th className="text-right p-3 font-medium">Known Tool Cost</th>
                      <th className="text-right p-3 font-medium">Unpriced</th>
                      <th className="text-right p-3 font-medium">Total Est.</th>
                    </tr>
                  </thead>
                  <tbody>
                    {byProviderTool.map(([capability, b]) => (
                      <tr key={capability} className="border-b border-border hover:bg-secondary/50">
                        <td className="p-3"><code className="text-foreground">{capability}</code></td>
                        <td className="p-3 text-right text-foreground/80">{b.requests}</td>
                        <td className="p-3 text-right text-foreground/80">{b.provider_tool_calls ?? 0}</td>
                        <td className="p-3 text-left text-xs text-muted-foreground">
                          <div>confidence: <span className={(b.provider_tool_unpriced_calls ?? 0) > 0 ? 'text-amber-300' : 'text-foreground/80'}>{formatProviderToolMeta(b.provider_tool_cost_confidence)}</span></div>
                          <div>source: <span className="text-foreground/80">{formatProviderToolMeta(b.provider_tool_cost_source)}</span></div>
                        </td>
                        <td className="p-3 text-right text-green-400">${(b.provider_tool_cost_usd ?? 0).toFixed(4)}</td>
                        <td className="p-3 text-right">
                          {(b.provider_tool_unpriced_calls ?? 0) > 0
                            ? <span className="text-amber-300">{b.provider_tool_unpriced_calls}</span>
                            : <span className="text-muted-foreground/70">0</span>}
                        </td>
                        <td className="p-3 text-right text-green-400">{displayCost(b)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          <div className="bg-card border border-border rounded overflow-hidden">
            <div className="px-4 py-3 border-b border-border flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <h3 className="text-sm font-medium text-foreground">Routing suggestions</h3>
                <p className="mt-0.5 text-xs text-muted-foreground">Pending optimizer recommendations for lower-cost model routing.</p>
              </div>
              <Button variant="outline" size="sm" className="h-7 px-2 text-xs gap-1" onClick={loadSuggestions} disabled={suggestionsLoading}>
                <RefreshCw className={`w-3 h-3 ${suggestionsLoading ? 'animate-spin' : ''}`} />
                Refresh
              </Button>
            </div>
            {suggestionsLoading ? (
              <div className="px-4 py-6 text-sm text-muted-foreground text-center">Loading routing suggestions...</div>
            ) : suggestions.length === 0 ? (
              <div className="px-4 py-6 text-sm text-muted-foreground text-center">No pending routing suggestions</div>
            ) : (
              <div className="divide-y divide-border">
                {suggestions.map((s) => (
                  <div key={s.id} className="px-4 py-3 flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2 text-sm">
                        <code className="text-foreground">{s.task_type || 'unknown-task'}</code>
                        <span className="text-muted-foreground/70">route</span>
                        <code className="text-muted-foreground">{s.current_model || 'current'}</code>
                        <span className="text-muted-foreground/70">to</span>
                        <code className="text-green-400">{s.suggested_model || 'suggested'}</code>
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground break-words">{s.reason || 'No reason supplied.'}</div>
                      <div className="mt-2 flex flex-wrap gap-2 text-[10px] uppercase tracking-wide">
                        <span className="rounded bg-green-950/40 text-green-300 border border-green-900/40 px-2 py-0.5">
                          {formatSavingsPercent(s.savings_percent)} savings
                        </span>
                        <span className="rounded bg-secondary text-muted-foreground px-2 py-0.5">
                          {formatSavingsUSD(s.savings_usd_per_1k)} / 1K calls
                        </span>
                        <span className="rounded bg-secondary text-muted-foreground px-2 py-0.5">{s.status}</span>
                      </div>
                    </div>
                    {s.status === 'pending' && (
                      <div className="flex gap-2 shrink-0">
                        <Button
                          variant="outline"
                          size="sm"
                          className="h-8 gap-1"
                          onClick={() => handleSuggestionAction(s.id, 'reject')}
                          disabled={suggestionAction !== null}
                        >
                          <X className="w-3 h-3" />
                          Reject
                        </Button>
                        <Button
                          size="sm"
                          className="h-8 gap-1"
                          onClick={() => handleSuggestionAction(s.id, 'approve')}
                          disabled={suggestionAction !== null}
                        >
                          <Check className="w-3 h-3" />
                          Approve
                        </Button>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="bg-card border border-border rounded overflow-hidden">
            <div className="px-4 py-3 border-b border-border flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <h3 className="text-sm font-medium text-foreground">Routing model stats</h3>
                <p className="mt-0.5 text-xs text-muted-foreground">Per-model optimizer telemetry by task type.</p>
              </div>
              <Button variant="outline" size="sm" className="h-7 px-2 text-xs gap-1" onClick={loadRoutingStats} disabled={statsLoading}>
                <RefreshCw className={`w-3 h-3 ${statsLoading ? 'animate-spin' : ''}`} />
                Refresh
              </Button>
            </div>
            {statsLoading ? (
              <div className="px-4 py-6 text-sm text-muted-foreground text-center">Loading routing stats...</div>
            ) : routingStats.length === 0 ? (
              <div className="px-4 py-6 text-sm text-muted-foreground text-center">No routing stats available</div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm min-w-[720px]">
                  <thead>
                    <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
                      <th className="text-left p-3 font-medium">Task</th>
                      <th className="text-left p-3 font-medium">Model</th>
                      <th className="text-right p-3 font-medium">Calls</th>
                      <th className="text-right p-3 font-medium">Retries</th>
                      <th className="text-right p-3 font-medium">Success</th>
                      <th className="text-right p-3 font-medium">Avg Latency</th>
                      <th className="text-right p-3 font-medium">Avg Tokens</th>
                      <th className="text-right p-3 font-medium">Cost / 1K</th>
                    </tr>
                  </thead>
                  <tbody>
                    {routingStats.map((s) => (
                      <tr key={`${s.task_type}:${s.model}`} className="border-b border-border hover:bg-secondary/50">
                        <td className="p-3"><code className="text-foreground">{s.task_type || 'unknown'}</code></td>
                        <td className="p-3"><code className="text-muted-foreground">{s.model || 'unknown'}</code></td>
                        <td className="p-3 text-right text-foreground/80">{s.total_calls}</td>
                        <td className="p-3 text-right text-foreground/80">{s.retries}</td>
                        <td className="p-3 text-right text-green-400">{Math.round((s.success_rate || 0) * 100)}%</td>
                        <td className="p-3 text-right text-muted-foreground">{((s.avg_latency_ms || 0) / 1000).toFixed(1)}s</td>
                        <td className="p-3 text-right text-muted-foreground">{formatTokens((s.avg_input_tokens || 0) + (s.avg_output_tokens || 0))}</td>
                        <td className="p-3 text-right text-green-400">${(s.cost_per_1k || 0).toFixed(4)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          {/* Per-agent */}
          {byAgent.length > 0 && (
            <div className="bg-card border border-border rounded overflow-hidden">
              <div className="px-4 py-3 border-b border-border">
                <h3 className="text-sm font-medium text-foreground">Per agent</h3>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full text-sm min-w-[640px]">
                  <thead>
                    <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
                      <th className="text-left p-3 font-medium">Agent</th>
                      <th className="text-right p-3 font-medium">Requests</th>
                      <th className="text-right p-3 font-medium">Input</th>
                      <th className="text-right p-3 font-medium">Output</th>
                      <th className="text-right p-3 font-medium">Avg Latency</th>
                      <th className="text-right p-3 font-medium">p95</th>
                      <th className="text-right p-3 font-medium">Errors</th>
                      <th className="text-right p-3 font-medium">Est. Cost</th>
                    </tr>
                  </thead>
                  <tbody>
                    {byAgent.map(([name, b]) => (
                      <tr key={name} className="border-b border-border hover:bg-secondary/50">
                        <td className="p-3"><code className="text-foreground">{name}</code></td>
                        <td className="p-3 text-right text-foreground/80">{b.requests}</td>
                        <td className="p-3 text-right text-foreground/80">{formatTokens(b.input_tokens)}</td>
                        <td className="p-3 text-right text-foreground/80">{formatTokens(b.output_tokens)}</td>
                        <td className="p-3 text-right text-muted-foreground">{(b.avg_latency_ms / 1000).toFixed(1)}s</td>
                        <td className="p-3 text-right text-muted-foreground">{b.p95_latency_ms != null ? `${(b.p95_latency_ms / 1000).toFixed(1)}s` : '—'}</td>
                        <td className="p-3 text-right">{b.errors > 0 ? <span className="text-red-400">{b.errors}</span> : <span className="text-muted-foreground/70">0</span>}</td>
                        <td className="p-3 text-right text-green-400">{displayCost(b, byModel.length === 1 ? byModel[0][0] : undefined)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* Per-model */}
          {byModel.length > 0 && (
            <div className="bg-card border border-border rounded overflow-hidden">
              <div className="px-4 py-3 border-b border-border">
                <h3 className="text-sm font-medium text-foreground">Per model</h3>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full text-sm min-w-[520px]">
                  <thead>
                    <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
                      <th className="text-left p-3 font-medium">Model</th>
                      <th className="text-right p-3 font-medium">Requests</th>
                      <th className="text-right p-3 font-medium">Input</th>
                      <th className="text-right p-3 font-medium">Output</th>
                      <th className="text-right p-3 font-medium">Avg Latency</th>
                      <th className="text-right p-3 font-medium">Est. Cost</th>
                    </tr>
                  </thead>
                  <tbody>
                    {byModel.map(([model, b]) => (
                      <tr key={model} className="border-b border-border hover:bg-secondary/50">
                        <td className="p-3"><code className="text-foreground">{model}</code></td>
                        <td className="p-3 text-right text-foreground/80">{b.requests}</td>
                        <td className="p-3 text-right text-foreground/80">{formatTokens(b.input_tokens)}</td>
                        <td className="p-3 text-right text-foreground/80">{formatTokens(b.output_tokens)}</td>
                        <td className="p-3 text-right text-muted-foreground">{(b.avg_latency_ms / 1000).toFixed(1)}s</td>
                        <td className="p-3 text-right text-green-400">{displayCost(b, model)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* Per-provider */}
          {byProvider.length > 0 && (
            <div className="bg-card border border-border rounded overflow-hidden">
              <div className="px-4 py-3 border-b border-border">
                <h3 className="text-sm font-medium text-foreground">Per provider</h3>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full text-sm min-w-[440px]">
                  <thead>
                    <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
                      <th className="text-left p-3 font-medium">Provider</th>
                      <th className="text-right p-3 font-medium">Requests</th>
                      <th className="text-right p-3 font-medium">Tokens</th>
                      <th className="text-right p-3 font-medium">Errors</th>
                      <th className="text-right p-3 font-medium">Avg Latency</th>
                    </tr>
                  </thead>
                  <tbody>
                    {byProvider.map(([provider, b]) => (
                      <tr key={provider} className="border-b border-border hover:bg-secondary/50">
                        <td className="p-3"><code className="text-foreground">{provider}</code></td>
                        <td className="p-3 text-right text-foreground/80">{b.requests}</td>
                        <td className="p-3 text-right text-foreground/80">{formatTokens(b.total_tokens)}</td>
                        <td className="p-3 text-right">{b.errors > 0 ? <span className="text-red-400">{b.errors}</span> : <span className="text-muted-foreground/70">0</span>}</td>
                        <td className="p-3 text-right text-muted-foreground">{(b.avg_latency_ms / 1000).toFixed(1)}s</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* Per-source */}
          {bySource.length > 0 && (
            <div className="bg-card border border-border rounded overflow-hidden">
              <div className="px-4 py-3 border-b border-border">
                <h3 className="text-sm font-medium text-foreground">Per source</h3>
                <p className="mt-0.5 text-xs text-muted-foreground">System vs agent LLM usage by caller.</p>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full text-sm min-w-[520px]">
                  <thead>
                    <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
                      <th className="text-left p-3 font-medium">Source</th>
                      <th className="text-right p-3 font-medium">Requests</th>
                      <th className="text-right p-3 font-medium">Input</th>
                      <th className="text-right p-3 font-medium">Output</th>
                      <th className="text-right p-3 font-medium">Avg Latency</th>
                      <th className="text-right p-3 font-medium">Errors</th>
                      <th className="text-right p-3 font-medium">Est. Cost</th>
                    </tr>
                  </thead>
                  <tbody>
                    {bySource.map(([source, b]) => (
                      <tr key={source} className="border-b border-border hover:bg-secondary/50">
                        <td className="p-3"><code className="text-foreground">{source}</code></td>
                        <td className="p-3 text-right text-foreground/80">{b.requests}</td>
                        <td className="p-3 text-right text-foreground/80">{formatTokens(b.input_tokens)}</td>
                        <td className="p-3 text-right text-foreground/80">{formatTokens(b.output_tokens)}</td>
                        <td className="p-3 text-right text-muted-foreground">{(b.avg_latency_ms / 1000).toFixed(1)}s</td>
                        <td className="p-3 text-right">{b.errors > 0 ? <span className="text-red-400">{b.errors}</span> : <span className="text-muted-foreground/70">0</span>}</td>
                        <td className="p-3 text-right text-green-400">{displayCost(b)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* Recent Errors */}
          {recentErrors.length > 0 && (
            <div className="bg-card border border-red-900/30 rounded overflow-hidden">
              <div className="px-4 py-3 border-b border-border">
                <h3 className="text-sm font-medium text-red-400">Recent errors</h3>
              </div>
              <div className="divide-y divide-border">
                {recentErrors.map((e, i) => (
                  <div key={i} className="px-4 py-3">
                    <div className="flex items-center gap-3 text-sm">
                      <span className="text-muted-foreground text-xs whitespace-nowrap">
                        {new Date(e.ts).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}
                      </span>
                      <code className="text-foreground">{e.agent}</code>
                      <span className="text-muted-foreground/70">{e.model}</span>
                      {e.status > 0 && (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-red-950 text-red-400 font-mono">{e.status}</span>
                      )}
                    </div>
                    <div className="mt-1 text-xs text-red-400/80 font-mono break-all">{e.error}</div>
                    <div className="mt-2 rounded border border-red-900/30 bg-red-950/20 px-3 py-2 text-xs">
                      <div className="font-medium text-red-300">Likely next step</div>
                      <div className="mt-1 text-muted-foreground">{routingErrorHint(e)}</div>
                      <div className="mt-2 flex flex-wrap gap-2">
                        <Button asChild variant="outline" size="sm" className="h-7 text-xs">
                          <Link to={`/agents/${e.agent}`}>Open Agent: {e.agent}</Link>
                        </Button>
                        <Button asChild variant="outline" size="sm" className="h-7 text-xs">
                          <Link to="/admin/doctor">Open Doctor</Link>
                        </Button>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}
