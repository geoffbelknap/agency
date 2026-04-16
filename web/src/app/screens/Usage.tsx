import { useState, useEffect, useCallback, useMemo } from 'react';
import { Link } from 'react-router';
import { Button } from '../components/ui/button';
import { Calendar } from '../components/ui/calendar';
import { Popover, PopoverContent, PopoverTrigger } from '../components/ui/popover';
import { AlertTriangle, ArrowRightLeft, CalendarIcon, Check, Coins, Layers3, LineChart, RefreshCw, TrendingUp, Wrench, X } from 'lucide-react';
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
type UsageView = 'breakdowns' | 'tools' | 'economics' | 'optimizer' | 'errors';
type BreakdownView = 'agents' | 'models' | 'providers' | 'sources';

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

function sectionButton(active: boolean): string {
  return active
    ? 'border-primary/30 bg-primary/10 text-primary'
    : 'border-border bg-background text-muted-foreground hover:border-border hover:text-foreground';
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
  const [view, setView] = useState<UsageView>('breakdowns');
  const [breakdown, setBreakdown] = useState<BreakdownView>('agents');

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
    {
      label: 'Requests',
      value: t.requests.toLocaleString(),
      detail: `${formatTokens(t.total_tokens)} total tokens`,
      icon: LineChart,
      tone: 'text-foreground',
    },
    {
      label: 'Estimated spend',
      value: displayCost(t),
      detail: `$${tokenCost.toFixed(4)} tokens + $${providerToolCost.toFixed(4)} tools`,
      icon: Coins,
      tone: 'text-primary',
    },
    {
      label: 'Latency',
      value: `${(t.avg_latency_ms / 1000).toFixed(1)}s`,
      detail: t.p95_latency_ms != null ? `p95 ${(t.p95_latency_ms / 1000).toFixed(1)}s` : 'p95 unavailable',
      icon: TrendingUp,
      tone: 'text-foreground',
    },
    {
      label: 'Open issues',
      value: `${t.errors + (unpricedProviderToolCalls > 0 ? 1 : 0)}`,
      detail: `${t.errors} errors, ${unpricedProviderToolCalls.toLocaleString()} unpriced tool calls`,
      icon: AlertTriangle,
      tone: t.errors > 0 || unpricedProviderToolCalls > 0 ? 'text-amber-300' : 'text-foreground',
    },
  ] : [];
  const activeBreakdownRows = useMemo(() => {
    switch (breakdown) {
      case 'agents':
        return byAgent.map(([name, b]) => ({
          key: name,
          primary: name,
          secondary: undefined,
          requests: b.requests,
          input: formatTokens(b.input_tokens),
          output: formatTokens(b.output_tokens),
          tokens: formatTokens(b.total_tokens),
          latency: `${(b.avg_latency_ms / 1000).toFixed(1)}s`,
          p95: b.p95_latency_ms != null ? `${(b.p95_latency_ms / 1000).toFixed(1)}s` : '—',
          errors: b.errors,
          cost: displayCost(b, byModel.length === 1 ? byModel[0][0] : undefined),
        }));
      case 'models':
        return byModel.map(([model, b]) => ({
          key: model,
          primary: model,
          secondary: undefined,
          requests: b.requests,
          input: formatTokens(b.input_tokens),
          output: formatTokens(b.output_tokens),
          tokens: formatTokens(b.total_tokens),
          latency: `${(b.avg_latency_ms / 1000).toFixed(1)}s`,
          p95: b.p95_latency_ms != null ? `${(b.p95_latency_ms / 1000).toFixed(1)}s` : '—',
          errors: b.errors,
          cost: displayCost(b, model),
        }));
      case 'providers':
        return byProvider.map(([provider, b]) => ({
          key: provider,
          primary: provider,
          secondary: undefined,
          requests: b.requests,
          input: formatTokens(b.input_tokens),
          output: formatTokens(b.output_tokens),
          tokens: formatTokens(b.total_tokens),
          latency: `${(b.avg_latency_ms / 1000).toFixed(1)}s`,
          p95: b.p95_latency_ms != null ? `${(b.p95_latency_ms / 1000).toFixed(1)}s` : '—',
          errors: b.errors,
          cost: displayCost(b),
        }));
      case 'sources':
        return bySource.map(([source, b]) => ({
          key: source,
          primary: source,
          secondary: source === 'agent' ? 'runtime requests' : source === 'system' ? 'setup + admin' : undefined,
          requests: b.requests,
          input: formatTokens(b.input_tokens),
          output: formatTokens(b.output_tokens),
          tokens: formatTokens(b.total_tokens),
          latency: `${(b.avg_latency_ms / 1000).toFixed(1)}s`,
          p95: b.p95_latency_ms != null ? `${(b.p95_latency_ms / 1000).toFixed(1)}s` : '—',
          errors: b.errors,
          cost: displayCost(b),
        }));
      default:
        return [];
    }
  }, [breakdown, byAgent, byModel, byProvider, bySource]);
  const missingPricingRows = useMemo(
    () => byProviderTool.filter(([, b]) => (b.provider_tool_unpriced_calls ?? 0) > 0),
    [byProviderTool],
  );

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
          <div className="overflow-hidden rounded-2xl border border-border bg-card">
            <div className="grid gap-0 lg:grid-cols-4">
              {summaryMetrics.map((metric, index) => {
                const Icon = metric.icon;
                return (
                  <div
                    key={metric.label}
                    className={`px-4 py-4 md:px-5 ${index < summaryMetrics.length - 1 ? 'border-b border-border lg:border-b-0 lg:border-r' : ''}`}
                  >
                    <div className="flex items-center justify-between gap-3">
                      <div className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">{metric.label}</div>
                      <Icon className="h-4 w-4 text-muted-foreground" />
                    </div>
                    <div className={`mt-2 text-2xl font-semibold tracking-tight ${metric.tone}`}>{metric.value}</div>
                    <div className="mt-1 text-xs text-muted-foreground">{metric.detail}</div>
                  </div>
                );
              })}
            </div>
            {(recentErrors.length > 0 || unpricedProviderToolCalls > 0) && (
              <div className="border-t border-border bg-secondary/20 px-4 py-3 md:px-5">
                <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                  <div className="space-y-1">
                    {recentErrors.length > 0 && (
                      <div className="flex items-center gap-2 text-sm font-medium text-red-300">
                        <AlertTriangle className="h-4 w-4" />
                        {recentErrors.length} recent routing error{recentErrors.length !== 1 ? 's' : ''} need attention
                      </div>
                    )}
                    {unpricedProviderToolCalls > 0 && (
                      <div className="text-sm text-amber-300">
                        {unpricedProviderToolCalls} provider-tool call{unpricedProviderToolCalls === 1 ? '' : 's'} are missing pricing metadata and are not fully reflected in estimated spend.
                      </div>
                    )}
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {primaryErroredAgent && (
                      <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                        <Link to={`/agents/${primaryErroredAgent}`}>Open Agent: {primaryErroredAgent}</Link>
                      </Button>
                    )}
                    {unpricedProviderToolCalls > 0 && (
                      <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                        <Link to="/admin/provider-tools">Open Provider Tools</Link>
                      </Button>
                    )}
                    <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                      <Link to="/admin/doctor">Open Doctor</Link>
                    </Button>
                  </div>
                </div>
              </div>
            )}
            <div className="flex flex-col gap-3 border-t border-border px-4 py-3 md:px-5 lg:flex-row lg:items-center lg:justify-between">
              <div className="flex flex-wrap gap-2">
                {[
                  { value: 'breakdowns', label: 'Breakdowns', icon: Layers3 },
                  { value: 'tools', label: 'Tool Usage', icon: Wrench },
                  { value: 'economics', label: 'Economics', icon: Coins },
                  { value: 'optimizer', label: 'Optimizer', icon: ArrowRightLeft },
                  { value: 'errors', label: 'Errors', icon: AlertTriangle },
                ].map((item) => {
                  const Icon = item.icon;
                  return (
                    <button
                      key={item.value}
                      type="button"
                      onClick={() => setView(item.value as UsageView)}
                      className={`inline-flex h-9 items-center gap-2 rounded-full border px-3 text-sm transition-colors ${sectionButton(view === item.value)}`}
                    >
                      <Icon className="h-3.5 w-3.5" />
                      <span>{item.label}</span>
                    </button>
                  );
                })}
              </div>
              <div className="text-sm text-muted-foreground">Operational detail is split by task instead of stacked into one long report.</div>
            </div>
          </div>

          {view === 'breakdowns' && (
            <section className="overflow-hidden rounded-2xl border border-border bg-card">
              <div className="flex flex-col gap-3 border-b border-border px-4 py-4 md:px-5 lg:flex-row lg:items-center lg:justify-between">
                <div>
                  <h3 className="text-sm font-medium text-foreground">Primary Breakdown</h3>
                  <p className="mt-1 text-sm text-muted-foreground">One table at a time. Operators swap the lens instead of scrolling through multiple repeated report sections.</p>
                </div>
                <div className="flex flex-wrap gap-2">
                  {[
                    { value: 'agents', label: 'Agents' },
                    { value: 'models', label: 'Models' },
                    { value: 'providers', label: 'Providers' },
                    { value: 'sources', label: 'Sources' },
                  ]
                    .filter((item) => item.value !== 'sources' || bySource.length > 0)
                    .map((item) => (
                      <button
                        key={item.value}
                        type="button"
                        onClick={() => setBreakdown(item.value as BreakdownView)}
                        className={`inline-flex h-8 items-center rounded-full border px-3 text-sm transition-colors ${sectionButton(breakdown === item.value)}`}
                      >
                        {item.label}
                      </button>
                    ))}
                </div>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full min-w-[840px] text-sm">
                  <thead>
                    <tr className="border-b border-border text-xs uppercase tracking-[0.12em] text-muted-foreground">
                      <th className="p-3 text-left font-medium">{breakdown.slice(0, -1)}</th>
                      <th className="p-3 text-right font-medium">Requests</th>
                      <th className="p-3 text-right font-medium">Input</th>
                      <th className="p-3 text-right font-medium">Output</th>
                      <th className="p-3 text-right font-medium">Tokens</th>
                      <th className="p-3 text-right font-medium">Avg latency</th>
                      <th className="p-3 text-right font-medium">p95</th>
                      <th className="p-3 text-right font-medium">Errors</th>
                      <th className="p-3 text-right font-medium">Est. cost</th>
                    </tr>
                  </thead>
                  <tbody>
                    {activeBreakdownRows.map((row) => (
                      <tr key={row.key} className="border-b border-border/70 last:border-0 hover:bg-secondary/35">
                        <td className="p-3">
                          <div className="font-medium text-foreground">{row.primary}</div>
                          {row.secondary && <div className="mt-0.5 text-xs text-muted-foreground">{row.secondary}</div>}
                        </td>
                        <td className="p-3 text-right text-foreground/80">{row.requests}</td>
                        <td className="p-3 text-right text-foreground/80">{row.input}</td>
                        <td className="p-3 text-right text-foreground/80">{row.output}</td>
                        <td className="p-3 text-right text-foreground/80">{row.tokens}</td>
                        <td className="p-3 text-right text-muted-foreground">{row.latency}</td>
                        <td className="p-3 text-right text-muted-foreground">{row.p95}</td>
                        <td className="p-3 text-right">
                          {row.errors > 0 ? <span className="text-red-400">{row.errors}</span> : <span className="text-muted-foreground/70">0</span>}
                        </td>
                        <td className="p-3 text-right text-green-400">{row.cost}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          )}

          {view === 'tools' && (
            <section className="grid gap-6 xl:grid-cols-[0.95fr_1.05fr]">
              <div className="overflow-hidden rounded-2xl border border-border bg-card">
                <div className="border-b border-border px-4 py-4 md:px-5">
                  <h3 className="text-sm font-medium text-foreground">Tool Activity Mix</h3>
                  <p className="mt-1 text-sm text-muted-foreground">This screen currently tracks provider-side tool activity. Agency-native tool usage is not yet part of the routing metrics contract.</p>
                </div>
                <div className="divide-y divide-border">
                  <div className="px-4 py-4 md:px-5">
                    <div className="flex items-start justify-between gap-4">
                      <div>
                        <div className="text-sm font-medium text-foreground">Provider-side tools</div>
                        <div className="mt-1 text-sm text-muted-foreground">Search, file search, URL context, and other provider-defined tools visible to routing metrics.</div>
                      </div>
                      <div className="text-right">
                        <div className="text-lg font-semibold text-foreground">{providerToolCalls.toLocaleString()}</div>
                        <div className="text-xs text-muted-foreground">total tool calls</div>
                      </div>
                    </div>
                  </div>
                  <div className="px-4 py-4 md:px-5">
                    <div className="flex items-start justify-between gap-4">
                      <div>
                        <div className="text-sm font-medium text-foreground">Priced provider tools</div>
                        <div className="mt-1 text-sm text-muted-foreground">Calls with pricing metadata available and included in estimated spend.</div>
                      </div>
                      <div className="text-right">
                        <div className="text-lg font-semibold text-foreground">{knownProviderToolCalls.toLocaleString()}</div>
                        <div className="text-xs text-muted-foreground">known-price calls</div>
                      </div>
                    </div>
                  </div>
                  <div className="px-4 py-4 md:px-5">
                    <div className="flex items-start justify-between gap-4">
                      <div>
                        <div className="text-sm font-medium text-foreground">Missing pricing metadata</div>
                        <div className="mt-1 text-sm text-muted-foreground">These calls are visible, but they are not fully reflected in cost estimates until pricing data is added.</div>
                      </div>
                      <div className="text-right">
                        <div className="text-lg font-semibold text-amber-300">{unpricedProviderToolCalls.toLocaleString()}</div>
                        <div className="text-xs text-muted-foreground">unpriced calls</div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              <div className="overflow-hidden rounded-2xl border border-border bg-card">
                <div className="border-b border-border px-4 py-4 md:px-5">
                  <h3 className="text-sm font-medium text-foreground">Most Used Provider Tool Surfaces</h3>
                  <p className="mt-1 text-sm text-muted-foreground">This shows which provider-side tools are active, how often they are called, and where pricing confidence is incomplete.</p>
                </div>
                <div className="divide-y divide-border">
                  {byProviderTool.length === 0 ? (
                    <div className="px-4 py-6 text-sm text-muted-foreground">No provider-side tool activity recorded in the selected period.</div>
                  ) : (
                    byProviderTool.map(([capability, b]) => (
                      <div key={capability} className="px-4 py-4 md:px-5">
                        <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                          <div className="min-w-0 space-y-2">
                            <div className="flex flex-wrap items-center gap-2">
                              <code className="break-all text-sm text-foreground">{capability}</code>
                              <span className="rounded-full border border-border bg-background px-2 py-0.5 text-[10px] uppercase tracking-[0.14em] text-muted-foreground">
                                provider side
                              </span>
                            </div>
                            <div className="text-sm text-muted-foreground">
                              pricing confidence: <span className={(b.provider_tool_unpriced_calls ?? 0) > 0 ? 'text-amber-300' : 'text-foreground/80'}>{formatProviderToolMeta(b.provider_tool_cost_confidence)}</span>
                            </div>
                            <div className="text-xs text-muted-foreground">
                              pricing source: {formatProviderToolMeta(b.provider_tool_cost_source)}
                            </div>
                          </div>
                          <div className="grid min-w-0 grid-cols-2 gap-3 sm:min-w-[260px]">
                            <div className="rounded-xl border border-border bg-secondary/35 px-3 py-2">
                              <div className="text-[10px] uppercase tracking-[0.14em] text-muted-foreground">Calls</div>
                              <div className="mt-1 text-base font-semibold text-foreground">{(b.provider_tool_calls ?? 0).toLocaleString()}</div>
                            </div>
                            <div className="rounded-xl border border-border bg-secondary/35 px-3 py-2">
                              <div className="text-[10px] uppercase tracking-[0.14em] text-muted-foreground">Requests</div>
                              <div className="mt-1 text-base font-semibold text-foreground">{b.requests.toLocaleString()}</div>
                            </div>
                            <div className="rounded-xl border border-border bg-secondary/35 px-3 py-2">
                              <div className="text-[10px] uppercase tracking-[0.14em] text-muted-foreground">Known tool cost</div>
                              <div className="mt-1 text-base font-semibold text-green-400">${(b.provider_tool_cost_usd ?? 0).toFixed(4)}</div>
                            </div>
                            <div className="rounded-xl border border-border bg-secondary/35 px-3 py-2">
                              <div className="text-[10px] uppercase tracking-[0.14em] text-muted-foreground">Unpriced</div>
                              <div className={`mt-1 text-base font-semibold ${(b.provider_tool_unpriced_calls ?? 0) > 0 ? 'text-amber-300' : 'text-muted-foreground/80'}`}>
                                {(b.provider_tool_unpriced_calls ?? 0).toLocaleString()}
                              </div>
                            </div>
                          </div>
                        </div>
                      </div>
                    ))
                  )}
                </div>
              </div>
            </section>
          )}

          {view === 'economics' && (
            <section className="grid gap-6 xl:grid-cols-[1.15fr_1fr]">
              <div className="overflow-hidden rounded-2xl border border-border bg-card">
                <div className="border-b border-border px-4 py-4 md:px-5">
                  <h3 className="text-sm font-medium text-foreground">Spend Composition</h3>
                  <p className="mt-1 text-sm text-muted-foreground">This shows what spend is known, what is included in the estimate, and where the estimate is incomplete because pricing metadata is missing.</p>
                </div>
                <div className="grid gap-0 sm:grid-cols-3">
                  <div className="border-b border-border px-4 py-4 sm:border-b-0 sm:border-r">
                    <div className="text-[11px] uppercase tracking-[0.14em] text-muted-foreground">Token spend</div>
                    <div className="mt-2 text-2xl font-semibold text-foreground">${tokenCost.toFixed(4)}</div>
                    <div className="mt-1 text-xs text-muted-foreground">Included in estimated total</div>
                  </div>
                  <div className="border-b border-border px-4 py-4 sm:border-b-0 sm:border-r">
                    <div className="text-[11px] uppercase tracking-[0.14em] text-muted-foreground">Priced tool spend</div>
                    <div className="mt-2 text-2xl font-semibold text-foreground">${providerToolCost.toFixed(4)}</div>
                    <div className="mt-1 text-xs text-muted-foreground">Included in estimated total</div>
                  </div>
                  <div className="px-4 py-4">
                    <div className="text-[11px] uppercase tracking-[0.14em] text-amber-500">Calls without pricing metadata</div>
                    <div className="mt-2 text-2xl font-semibold text-amber-300">{unpricedProviderToolCalls.toLocaleString()} calls</div>
                    <div className="mt-1 text-xs text-amber-200/80">Not included in estimated total</div>
                  </div>
                </div>
                <div className="border-t border-border px-4 py-4 md:px-5">
                  <div className="rounded-xl border border-amber-900/40 bg-amber-950/20 px-4 py-3">
                    <div className="text-sm font-medium text-amber-200">What this means</div>
                    <div className="mt-1 text-sm text-muted-foreground">
                      Agency can see these tool calls happened, but the current routing metrics contract only exposes missing pricing at the capability level. Add pricing metadata for the flagged capabilities to make the spend estimate complete.
                    </div>
                  </div>
                </div>
              </div>

              <div className="space-y-6">
                <div className="overflow-hidden rounded-2xl border border-border bg-card">
                  <div className="border-b border-border px-4 py-4 md:px-5">
                    <h3 className="text-sm font-medium text-foreground">Missing Pricing Metadata</h3>
                    <p className="mt-1 text-sm text-muted-foreground">These are the exact provider-tool capabilities currently showing unpriced usage in the selected period.</p>
                  </div>
                  <div className="divide-y divide-border">
                    {missingPricingRows.length === 0 ? (
                      <div className="px-4 py-6 text-sm text-muted-foreground">No provider tool calls are currently missing pricing metadata.</div>
                    ) : (
                      missingPricingRows.map(([capability, b]) => (
                        <div key={capability} className="flex flex-col gap-3 px-4 py-4 md:px-5">
                          <div className="flex flex-wrap items-center gap-2 text-sm">
                            <code className="text-foreground">{capability}</code>
                            <span className="text-muted-foreground">pricing confidence</span>
                            <code className="text-muted-foreground">{formatProviderToolMeta(b.provider_tool_cost_confidence)}</code>
                          </div>
                          <div className="text-sm text-muted-foreground">
                            {(b.provider_tool_unpriced_calls ?? 0).toLocaleString()} calls missing a complete price mapping
                          </div>
                        </div>
                      ))
                    )}
                  </div>
                  <div className="border-t border-border bg-secondary/30 px-4 py-4 md:px-5">
                    <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                      <div className="text-sm text-muted-foreground">
                        Update provider tool pricing metadata in routing configuration so these calls are included in spend estimates.
                      </div>
                      <div className="flex flex-wrap gap-2">
                        <Button asChild variant="outline" size="sm" className="h-8 rounded-xl px-3 text-xs">
                          <Link to="/admin/provider-tools">Open Provider Tools</Link>
                        </Button>
                        <Button asChild variant="outline" size="sm" className="h-8 rounded-xl px-3 text-xs">
                          <Link to="/admin/presets">Open Presets</Link>
                        </Button>
                      </div>
                    </div>
                  </div>
                </div>

                {byProviderTool.length > 0 && (
                  <div className="overflow-hidden rounded-2xl border border-border bg-card">
                    <div className="border-b border-border px-4 py-4 md:px-5">
                      <h3 className="text-sm font-medium text-foreground">Provider Tool Breakdown</h3>
                      <p className="mt-1 text-sm text-muted-foreground">Once pricing metadata exists, this is the durable audit of provider-side tool spend.</p>
                    </div>
                    <div className="divide-y divide-border">
                      {byProviderTool.map(([capability, b]) => (
                        <div key={capability} className="grid gap-3 px-4 py-3 md:grid-cols-[1.4fr_repeat(4,minmax(0,1fr))] md:items-center md:px-5">
                          <div>
                            <div className="font-mono text-sm text-foreground">{capability}</div>
                            <div className="mt-1 text-xs text-muted-foreground">{b.requests} requests / {b.provider_tool_calls ?? 0} tool calls</div>
                          </div>
                          <div className="text-sm text-muted-foreground md:text-right">
                            <div className="text-[10px] uppercase tracking-[0.14em]">Known</div>
                            <div className="mt-1 text-foreground">${(b.provider_tool_cost_usd ?? 0).toFixed(4)}</div>
                          </div>
                          <div className="text-sm text-muted-foreground md:text-right">
                            <div className="text-[10px] uppercase tracking-[0.14em]">Confidence</div>
                            <div className={`mt-1 ${(b.provider_tool_unpriced_calls ?? 0) > 0 ? 'text-amber-300' : 'text-foreground'}`}>
                              {formatProviderToolMeta(b.provider_tool_cost_confidence)}
                            </div>
                          </div>
                          <div className="text-sm text-muted-foreground md:text-right">
                            <div className="text-[10px] uppercase tracking-[0.14em]">Unpriced</div>
                            <div className="mt-1 text-amber-300">{(b.provider_tool_unpriced_calls ?? 0).toLocaleString()}</div>
                          </div>
                          <div className="text-sm text-muted-foreground md:text-right">
                            <div className="text-[10px] uppercase tracking-[0.14em]">Total est.</div>
                            <div className="mt-1 text-green-400">{displayCost(b)}</div>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            </section>
          )}

          {view === 'optimizer' && (
            <section className="grid gap-6 xl:grid-cols-[1.1fr_0.9fr]">
              <div className="overflow-hidden rounded-2xl border border-border bg-card">
                <div className="border-b border-border px-4 py-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between md:px-5">
                  <div>
                    <h3 className="text-sm font-medium text-foreground">Routing Suggestions</h3>
                    <p className="mt-0.5 text-sm text-muted-foreground">Pending optimizer recommendations for lower-cost model routing.</p>
                  </div>
                  <Button variant="outline" size="sm" className="h-8 rounded-xl px-3 text-xs gap-1" onClick={loadSuggestions} disabled={suggestionsLoading}>
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
                      <div key={s.id} className="px-4 py-3 flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between md:px-5">
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

              <div className="overflow-hidden rounded-2xl border border-border bg-card">
                <div className="border-b border-border px-4 py-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between md:px-5">
                  <div>
                    <h3 className="text-sm font-medium text-foreground">Routing Model Stats</h3>
                    <p className="mt-0.5 text-sm text-muted-foreground">Per-model optimizer telemetry by task type.</p>
                  </div>
                  <Button variant="outline" size="sm" className="h-8 rounded-xl px-3 text-xs gap-1" onClick={loadRoutingStats} disabled={statsLoading}>
                    <RefreshCw className={`w-3 h-3 ${statsLoading ? 'animate-spin' : ''}`} />
                    Refresh
                  </Button>
                </div>
                {statsLoading ? (
                  <div className="px-4 py-6 text-sm text-muted-foreground text-center">Loading routing stats...</div>
                ) : routingStats.length === 0 ? (
                  <div className="px-4 py-6 text-sm text-muted-foreground text-center">No routing stats available</div>
                ) : (
                  <div className="divide-y divide-border">
                    {routingStats.map((s) => (
                      <div key={`${s.task_type}:${s.model}`} className="grid gap-2 px-4 py-3 md:grid-cols-[1.3fr_1fr_repeat(4,minmax(0,0.7fr))] md:items-center md:px-5">
                        <div>
                          <div className="text-sm font-medium text-foreground">{s.task_type || 'unknown'}</div>
                          <div className="mt-1 text-xs text-muted-foreground">{s.model || 'unknown'}</div>
                        </div>
                        <div className="text-sm text-muted-foreground md:text-right">{s.total_calls}</div>
                        <div className="text-sm text-muted-foreground md:text-right">{s.retries} retries</div>
                        <div className="text-sm text-green-400 md:text-right">{Math.round((s.success_rate || 0) * 100)}%</div>
                        <div className="text-sm text-muted-foreground md:text-right">{((s.avg_latency_ms || 0) / 1000).toFixed(1)}s</div>
                        <div className="text-sm text-muted-foreground md:text-right">{formatTokens((s.avg_input_tokens || 0) + (s.avg_output_tokens || 0))}</div>
                        <div className="text-sm text-green-400 md:text-right">${(s.cost_per_1k || 0).toFixed(4)}</div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </section>
          )}

          {view === 'errors' && (
            <section className="overflow-hidden rounded-2xl border border-red-900/30 bg-card">
              <div className="border-b border-red-900/30 bg-red-950/20 px-4 py-4 md:px-5">
                <div className="flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
                  <div>
                    <h3 className="text-sm font-medium text-red-300">Recent Routing Failures</h3>
                    <p className="mt-1 text-sm text-muted-foreground">Error detail lives in its own view instead of being duplicated in both the summary and the bottom of the page.</p>
                  </div>
                  <Button asChild variant="outline" size="sm" className="h-8 rounded-xl px-3 text-xs">
                    <Link to="/admin/doctor">Open Doctor</Link>
                  </Button>
                </div>
              </div>
              {recentErrors.length === 0 ? (
                <div className="px-4 py-6 text-sm text-muted-foreground">No recent routing errors in the selected period.</div>
              ) : (
                <div className="divide-y divide-border">
                  {recentErrors.map((e, i) => (
                    <div key={i} className="px-4 py-4 md:px-5">
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
              )}
            </section>
          )}
        </>
      )}
    </div>
  );
}
