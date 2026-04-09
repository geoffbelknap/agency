import { useState, useEffect, useCallback } from 'react';
import { Button } from '../components/ui/button';
import { Calendar } from '../components/ui/calendar';
import { Popover, PopoverContent, PopoverTrigger } from '../components/ui/popover';
import { RefreshCw, CalendarIcon, Check, X } from 'lucide-react';
import type { DateRange } from 'react-day-picker';
import { toast } from 'sonner';
import { api, type RawRoutingSuggestion } from '../lib/api';

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
  recent_errors?: RecentError[];
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
  const [suggestionsLoading, setSuggestionsLoading] = useState(true);
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
      toast.error(err.message || 'Failed to load routing suggestions');
    } finally {
      setSuggestionsLoading(false);
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
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <p className="text-sm text-muted-foreground">LLM usage and estimated spend</p>
          {metrics?.period && (
            <p className="text-[10px] text-muted-foreground/70 mt-0.5">
              {new Date(metrics.period.since).toLocaleDateString()} — {new Date(metrics.period.until).toLocaleDateString()}
            </p>
          )}
        </div>
        <div className="flex items-center gap-1.5">
          {RANGE_PRESETS.filter(p => p.key !== 'custom').map(p => (
            <Button
              key={p.key}
              variant={preset === p.key ? 'default' : 'outline'}
              size="sm"
              className="h-7 px-2.5 text-xs"
              onClick={() => handlePreset(p.key)}
            >
              {p.label}
            </Button>
          ))}
          <Popover open={calendarOpen} onOpenChange={setCalendarOpen}>
            <PopoverTrigger asChild>
              <Button
                variant={preset === 'custom' ? 'default' : 'outline'}
                size="sm"
                className="h-7 px-2.5 text-xs gap-1"
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
          <div className="w-px h-5 bg-border mx-0.5" />
          <Button variant="outline" size="sm" className="h-7 px-2" onClick={() => load()} disabled={refreshing}>
            <RefreshCw className={`w-3 h-3 ${refreshing ? 'animate-spin' : ''}`} />
          </Button>
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
          {/* Summary cards */}
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-2 md:gap-3">
            <div className="bg-card border border-border rounded p-3 md:p-4">
              <div className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Requests</div>
              <div className="text-lg md:text-xl font-semibold text-foreground">{t.requests}</div>
              {t.errors > 0 && <div className="text-[10px] text-red-400">{t.errors} errors</div>}
            </div>
            <div className="bg-card border border-border rounded p-3 md:p-4">
              <div className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Input Tokens</div>
              <div className="text-lg md:text-xl font-semibold text-foreground">{formatTokens(t.input_tokens)}</div>
            </div>
            <div className="bg-card border border-border rounded p-3 md:p-4">
              <div className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Output Tokens</div>
              <div className="text-lg md:text-xl font-semibold text-foreground">{formatTokens(t.output_tokens)}</div>
            </div>
            <div className="bg-card border border-border rounded p-3 md:p-4">
              <div className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Total Tokens</div>
              <div className="text-lg md:text-xl font-semibold text-foreground">{formatTokens(t.total_tokens)}</div>
            </div>
            <div className="bg-card border border-border rounded p-3 md:p-4">
              <div className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Avg Latency</div>
              <div className="text-lg md:text-xl font-semibold text-foreground">{(t.avg_latency_ms / 1000).toFixed(1)}s</div>
              {t.p95_latency_ms != null && <div className="text-[10px] text-muted-foreground">p95: {(t.p95_latency_ms / 1000).toFixed(1)}s</div>}
            </div>
            <div className="bg-card border border-border rounded p-3 md:p-4">
              <div className="text-[10px] text-muted-foreground uppercase tracking-wide mb-1">Est. Cost</div>
              <div className="text-lg md:text-xl font-semibold text-green-400">{displayCost(t)}</div>
            </div>
          </div>

          <div className="bg-card border border-border rounded overflow-hidden">
            <div className="px-4 py-3 border-b border-border flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Routing Suggestions</h3>
                <p className="text-[10px] text-muted-foreground/70 mt-0.5">Pending optimizer recommendations for lower-cost model routing</p>
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

          {/* Per-agent */}
          {byAgent.length > 0 && (
            <div className="bg-card border border-border rounded overflow-hidden">
              <div className="px-4 py-3 border-b border-border">
                <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Per Agent</h3>
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
                <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Per Model</h3>
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
                <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Per Provider</h3>
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
                <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Per Source</h3>
                <p className="text-[10px] text-muted-foreground/70 mt-0.5">System vs agent LLM usage by caller</p>
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
          {metrics?.recent_errors && metrics.recent_errors.length > 0 && (
            <div className="bg-card border border-red-900/30 rounded overflow-hidden">
              <div className="px-4 py-3 border-b border-border">
                <h3 className="text-xs font-medium text-red-400 uppercase tracking-wide">Recent Errors</h3>
              </div>
              <div className="divide-y divide-border">
                {metrics.recent_errors.map((e, i) => (
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
