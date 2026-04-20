import { useState, useEffect, useCallback } from 'react';
import { Calendar } from '../components/ui/calendar';
import { Popover, PopoverContent, PopoverTrigger } from '../components/ui/popover';
import { CalendarIcon, Check, RefreshCw, X } from 'lucide-react';
import type { DateRange } from 'react-day-picker';
import { toast } from 'sonner';
import { api, type RawRoutingStat, type RawRoutingSuggestion } from '../lib/api';
import { featureEnabled } from '../lib/features';

// Approximate pricing per million tokens (USD)
const PRICING: Record<string, { input: number; output: number }> = {
  'claude-sonnet': { input: 3, output: 15 },
  'claude-haiku': { input: 0.25, output: 1.25 },
  'claude-opus': { input: 15, output: 75 },
  'gpt-4o': { input: 2.5, output: 10 },
  'text-embedding-3': { input: 0.13, output: 0 },
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
  retry_cost_usd?: number;
  cached_tokens?: number;
  cached_cost_usd?: number;
}

interface RecentError {
  ts: string;
  agent: string;
  model: string;
  status: number;
  error: string;
}

interface UsageCall {
  ts: string;
  agent: string;
  source?: string;
  model: string;
  provider_model?: string;
  status: number;
  error?: string;
  duration_ms?: number;
  input_tokens: number;
  output_tokens: number;
  cached_tokens?: number;
  cache_creation_input_tokens?: number;
  cache_read_input_tokens?: number;
  total_tokens: number;
  est_cost_usd: number;
  token_cost_usd?: number;
  cached_cost_usd?: number;
  provider_tool_calls?: number;
  provider_tool_cost_usd?: number;
  provider_tool_unpriced_calls?: number;
  provider_tool_capabilities?: string;
  retry?: boolean;
}

interface HourlyUsageBucket {
  hour: string;
  totals: MetricsBucket;
  by_model?: Record<string, MetricsBucket>;
}

interface RoutingMetrics {
  period: { since: string; until: string };
  totals: MetricsBucket;
  by_agent: Record<string, MetricsBucket>;
  by_model: Record<string, MetricsBucket>;
  by_provider: Record<string, MetricsBucket>;
  by_source?: Record<string, MetricsBucket>;
  by_provider_tool?: Record<string, MetricsBucket>;
  by_hour?: HourlyUsageBucket[];
  recent_calls?: UsageCall[];
  recent_errors?: RecentError[];
}

function estimateCost(model: string, input: number, output: number): number {
  const p = pricingForModel(model);
  return (input * p.input + output * p.output) / 1_000_000;
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

function formatMoneyShort(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '$0.00';
  if (value < 0.01) return '<$0.01';
  return `$${value.toFixed(value >= 1 ? 2 : 3)}`;
}

function formatCostCell(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '$0.0000';
  if (value < 0.01) return '<$0.01';
  return `$${value.toFixed(4)}`;
}

function formatRate(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '-';
  return `$${value.toFixed(value >= 1 ? 2 : 2)}`;
}

function formatLatency(ms?: number): string {
  if (!ms) return '-';
  return `${Math.round(ms).toLocaleString()}ms`;
}

type RangePreset = '24h' | '7d' | '30d' | 'mtd' | 'custom';
type UsageView = 'usage' | 'optimizer';

const RANGE_PRESETS: { key: RangePreset; label: string }[] = [
  { key: '24h', label: '24h' },
  { key: '7d', label: '7d' },
  { key: '30d', label: '30d' },
  { key: 'mtd', label: 'MTD' },
];

function presetToSince(preset: RangePreset): string {
  const now = new Date();
  switch (preset) {
    case '24h': return new Date(now.getTime() - 24 * 60 * 60 * 1000).toISOString();
    case '7d': return new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000).toISOString();
    case '30d': return new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000).toISOString();
    case 'mtd': return new Date(now.getFullYear(), now.getMonth(), 1).toISOString();
    default: return '';
  }
}

function formatDateShort(d: Date): string {
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

function rangeLabelForPreset(preset: RangePreset): string {
  if (preset === 'mtd') return 'month to date';
  if (preset === 'custom') return 'custom range';
  return preset;
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

function MetaStat({ label, value, detail, tone }: { label: string; value: string | number; detail?: string; tone?: string }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 130 }}>
      <span className="eyebrow" style={{ fontSize: 10 }}>{label}</span>
      <span className="mono" style={{ fontSize: 18, lineHeight: 1, color: tone || 'var(--ink)' }}>{value}</span>
      {detail && <span style={{ color: 'var(--ink-faint)', fontSize: 12, whiteSpace: 'nowrap' }}>{detail}</span>}
    </div>
  );
}

const MODEL_COLORS = [
  '#0FA77F',
  '#087A61',
  '#B8E8D8',
  '#E6A300',
  '#B879F2',
  '#3B82F6',
  '#EF7B45',
  '#64748B',
  '#2DB4A0',
  '#C76B98',
  '#7A9A01',
  '#8B6F47',
];
const OTHER_MODEL_COLOR = '#B8B4AC';
const MAX_CHART_MODEL_SERIES = 8;

function stableHash(value: string): number {
  let hash = 2166136261;
  for (let i = 0; i < value.length; i += 1) {
    hash ^= value.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
}

function colorForModel(model: string): string {
  const key = model.trim().toLowerCase() || 'unknown';
  return MODEL_COLORS[stableHash(key) % MODEL_COLORS.length];
}

function pricingForModel(model: string): { input: number; output: number } {
  const lower = model.toLowerCase();
  if (PRICING[model]) return PRICING[model];
  if (lower.includes('sonnet')) return PRICING['claude-sonnet'];
  if (lower.includes('haiku')) return PRICING['claude-haiku'];
  if (lower.includes('opus')) return PRICING['claude-opus'];
  if (lower.includes('gpt-4o')) return PRICING['gpt-4o'];
  if (lower.includes('embedding')) return PRICING['text-embedding-3'];
  return DEFAULT_PRICING;
}

function providerForModel(model: string, providerModel?: string): string {
  const value = `${model} ${providerModel ?? ''}`.toLowerCase();
  if (value.includes('claude')) return 'ANTHROPIC';
  if (value.includes('gpt') || value.includes('openai') || value.includes('embedding')) return 'OPENAI';
  if (value.includes('gemini')) return 'GOOGLE';
  if (value.includes('mistral') || value.includes('codestral')) return 'MISTRAL';
  if (value.includes('deepseek')) return 'DEEPSEEK';
  return 'PROVIDER';
}

function Panel({ children, padded = false }: { children: React.ReactNode; padded?: boolean }) {
  return (
    <section style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', overflow: 'hidden', padding: padded ? 18 : 0 }}>
      {children}
    </section>
  );
}

function TableHeader({ cols, widths }: { cols: React.ReactNode[]; widths: string }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: widths, gap: 12, padding: '10px 16px', borderBottom: '0.5px solid var(--ink-hairline)', alignItems: 'center' }}>
      {cols.map((col, index) => (
        <div key={index} className="mono" style={{ color: 'var(--teal-dark)', fontSize: 10, letterSpacing: '0.16em', textTransform: 'uppercase', minWidth: 0 }}>
          {col}
        </div>
      ))}
    </div>
  );
}

function TableRow({ cols, widths, accent }: { cols: React.ReactNode[]; widths: string; accent?: string }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: widths, gap: 12, padding: '13px 16px', borderBottom: '0.5px solid var(--ink-hairline)', alignItems: 'center', boxShadow: accent ? `inset 2px 0 0 ${accent}` : undefined }}>
      {cols.map((col, index) => (
        <div key={index} style={{ minWidth: 0 }}>
          {col}
        </div>
      ))}
    </div>
  );
}

interface BreakdownRow {
  key: string;
  primary: string;
  secondary?: string;
  requests: number;
  tokens: string;
  latency: string;
  errors: number;
  cost: string;
}

function BreakdownGroup({ title, rows }: { title: string; rows: BreakdownRow[] }) {
  return (
    <div style={{ minWidth: 0 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 8, padding: '11px 14px 8px', borderBottom: '0.5px solid var(--ink-hairline)' }}>
        <span className="eyebrow" style={{ fontSize: 9 }}>{title}</span>
        <span className="mono" style={{ color: 'var(--ink-faint)', fontSize: 11 }}>{rows.length}</span>
      </div>
      {rows.length === 0 ? (
        <div style={{ padding: 14, color: 'var(--ink-mid)', fontSize: 12 }}>No data</div>
      ) : rows.slice(0, 5).map((row) => (
        <div key={row.key} style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) 52px 62px 46px 72px', gap: 10, alignItems: 'center', padding: '10px 14px', borderBottom: '0.5px solid var(--ink-hairline)' }}>
          <div style={{ minWidth: 0 }}>
            <div className="mono" style={{ color: 'var(--ink)', fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{row.primary}</div>
            {row.secondary && <div style={{ marginTop: 2, color: 'var(--ink-mid)', fontSize: 11, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{row.secondary}</div>}
          </div>
          <span className="mono" style={{ color: 'var(--ink)', fontSize: 11, textAlign: 'right' }}>{row.requests}</span>
          <span className="mono" style={{ color: 'var(--ink-mid)', fontSize: 11, textAlign: 'right' }}>{row.tokens}</span>
          <span className="mono" style={{ color: row.errors > 0 ? 'var(--red)' : 'var(--ink-faint)', fontSize: 11, textAlign: 'right' }}>{row.errors}</span>
          <span className="mono" style={{ color: 'var(--teal-dark)', fontSize: 11, textAlign: 'right' }}>{row.cost}</span>
        </div>
      ))}
      {rows.length > 5 && <div className="mono" style={{ padding: '8px 14px', color: 'var(--ink-faint)', fontSize: 11 }}>+ {rows.length - 5} more</div>}
    </div>
  );
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
  const routingOptimizerEnabled = featureEnabled('routing_optimizer');
  const [metrics, setMetrics] = useState<RoutingMetrics | null>(null);
  const [suggestions, setSuggestions] = useState<RawRoutingSuggestion[]>([]);
  const [routingStats, setRoutingStats] = useState<RawRoutingStat[]>([]);
  const [suggestionsLoading, setSuggestionsLoading] = useState(routingOptimizerEnabled);
  const [statsLoading, setStatsLoading] = useState(routingOptimizerEnabled);
  const [suggestionAction, setSuggestionAction] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [preset, setPreset] = useState<RangePreset>('24h');
  const [dateRange, setDateRange] = useState<DateRange | undefined>();
  const [calendarOpen, setCalendarOpen] = useState(false);
  const [view, setView] = useState<UsageView>('usage');
  const [breakout, setBreakout] = useState<'model' | 'agent'>('model');
  const [chartMetric, setChartMetric] = useState<'cost' | 'tokens'>('cost');
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
    if (!routingOptimizerEnabled) {
      setSuggestions([]);
      setSuggestionsLoading(false);
      return;
    }
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
  }, [routingOptimizerEnabled]);

  const loadRoutingStats = useCallback(async () => {
    if (!routingOptimizerEnabled) {
      setRoutingStats([]);
      setStatsLoading(false);
      return;
    }
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
  }, [routingOptimizerEnabled]);

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
    if (routingOptimizerEnabled) {
      loadSuggestions();
      loadRoutingStats();
    }
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
  const byHour = metrics?.by_hour ?? [];
  const recentCalls = metrics?.recent_calls ?? [];
  const recentErrors = metrics?.recent_errors ?? [];
  const providerToolCost = t?.provider_tool_cost_usd ?? 0;
  const providerToolCalls = t?.provider_tool_calls ?? 0;
  const unpricedProviderToolCalls = t?.provider_tool_unpriced_calls ?? 0;
  const tokenCost = t ? Math.max(0, (t.est_cost_usd || 0) - providerToolCost - (t.cached_cost_usd ?? 0)) : 0;
  const knownProviderToolCalls = Math.max(0, providerToolCalls - unpricedProviderToolCalls);
  const retryCost = t?.retry_cost_usd ?? 0;
  const cachedTokens = t?.cached_tokens ?? 0;
  const cachedCost = t?.cached_cost_usd ?? 0;
  const periodDays = metrics?.period
    ? Math.max(1, (new Date(metrics.period.until).getTime() - new Date(metrics.period.since).getTime()) / (24 * 60 * 60 * 1000))
    : preset === '7d' ? 7 : preset === '30d' ? 30 : 1;
  const projectedMonth = t ? (t.est_cost_usd || 0) * (30 / periodDays) : 0;
  const monthlyBudget = 400;
  const budgetUsedPercent = monthlyBudget > 0 ? Math.min(100, (projectedMonth / monthlyBudget) * 100) : 0;
  const summaryMetrics = t ? [
    {
      label: 'Spend',
      value: displayCost(t),
      detail: rangeLabelForPreset(preset),
      tone: 'var(--teal-dark)',
    },
    {
      label: 'Projected month',
      value: `$${projectedMonth.toFixed(projectedMonth >= 10 ? 0 : 2)}`,
      detail: 'at current pace',
    },
    {
      label: 'Budget used',
      value: `${Math.round(budgetUsedPercent)}%`,
      detail: `$${monthlyBudget.toLocaleString()} monthly cap`,
      tone: budgetUsedPercent >= 90 ? 'var(--red)' : budgetUsedPercent >= 60 ? 'var(--amber)' : 'var(--teal-dark)',
    },
    {
      label: 'Tokens',
      value: formatTokens(t.total_tokens),
      detail: `${formatTokens(t.input_tokens)} in / ${formatTokens(t.output_tokens)} out`,
    },
    {
      label: 'Calls',
      value: t.requests.toLocaleString(),
      detail: `${t.errors} errors`,
      tone: t.errors > 0 ? 'var(--amber)' : undefined,
    },
    {
      label: 'Avg $ / call',
      value: t.requests > 0 ? `$${((t.est_cost_usd || 0) / t.requests).toFixed(4)}` : '$0.0000',
      detail: t.p95_latency_ms != null ? `p95 ${(t.p95_latency_ms / 1000).toFixed(1)}s` : `${(t.avg_latency_ms / 1000).toFixed(1)}s avg`,
    },
  ] : [];
  const costComponents = t ? [
    {
      label: 'Token spend',
      value: tokenCost,
      meta: `${formatTokens(t.input_tokens)} input / ${formatTokens(t.output_tokens)} output`,
      tone: 'var(--teal)',
      included: true,
    },
    {
      label: 'Cached input',
      value: cachedCost,
      meta: cachedTokens > 0 ? `${formatTokens(cachedTokens)} cached tokens` : 'not reported by gateway yet',
      tone: '#B8E8D8',
      included: cachedCost > 0,
    },
    {
      label: 'Provider tools',
      value: providerToolCost,
      meta: `${knownProviderToolCalls.toLocaleString()} priced / ${providerToolCalls.toLocaleString()} total calls`,
      tone: 'var(--amber)',
      included: providerToolCost > 0,
    },
    {
      label: 'Retry waste',
      value: retryCost,
      meta: retryCost > 0 ? 'included in estimated spend' : 'no retry waste reported',
      tone: 'var(--red)',
      included: retryCost > 0,
    },
    {
      label: 'Unpriced tools',
      value: 0,
      meta: `${unpricedProviderToolCalls.toLocaleString()} calls missing pricing metadata`,
      tone: 'var(--ink-faint)',
      included: false,
    },
  ].filter((component) => component.value > 0 || component.label === 'Token spend' || component.label === 'Cached input' || component.label === 'Unpriced tools' && unpricedProviderToolCalls > 0) : [];
  const pricedComponentTotal = costComponents.reduce((sum, component) => sum + (component.included ? component.value : 0), 0);

  // Use gateway cost if available, otherwise estimate client-side
  function displayCost(bucket: MetricsBucket, model?: string): string {
    if (bucket.est_cost_usd > 0) return `$${bucket.est_cost_usd.toFixed(4)}`;
    if (model) return `$${estimateCost(model, bucket.input_tokens, bucket.output_tokens).toFixed(4)}`;
    // Sum across models for total
    const totalEst = byModel.reduce((sum, [m, b]) => sum + estimateCost(m, b.input_tokens, b.output_tokens), 0);
    return `~$${totalEst.toFixed(4)}`;
  }

  const rangeLabel = metrics?.period
    ? `${new Date(metrics.period.since).toLocaleDateString()} - ${new Date(metrics.period.until).toLocaleDateString()}`
    : preset === 'custom' && dateRange?.from
      ? `${formatDateShort(dateRange.from)}${dateRange.to ? ` - ${formatDateShort(dateRange.to)}` : ''}`
      : preset;

  const topAgent = byAgent.slice().sort((a, b) => b[1].requests - a[1].requests)[0];
  const topModel = byModel.slice().sort((a, b) => b[1].requests - a[1].requests)[0];
  const topProvider = byProvider.slice().sort((a, b) => b[1].requests - a[1].requests)[0];
  const totalKnownSpend = t ? displayCost(t) : '$0.0000';
  const costValue = (bucket: MetricsBucket, model?: string) => bucket.est_cost_usd > 0 ? bucket.est_cost_usd : model ? estimateCost(model, bucket.input_tokens, bucket.output_tokens) : 0;
  const chartValue = (bucket: MetricsBucket, model?: string) => chartMetric === 'tokens' ? Math.max(0, bucket.total_tokens || bucket.input_tokens + bucket.output_tokens) : costValue(bucket, model);
  const chartLegendValue = (bucket: MetricsBucket, model?: string) => chartMetric === 'tokens' ? formatTokens(bucket.total_tokens || bucket.input_tokens + bucket.output_tokens) : formatMoneyShort(costValue(bucket, model));
  const modelRows = byModel
    .map(([model, bucket]) => ({ model, bucket, color: colorForModel(model), cost: costValue(bucket, model), chartValue: chartValue(bucket, model) }))
    .sort((a, b) => b.chartValue - a.chartValue || b.bucket.requests - a.bucket.requests);
  const fallbackModelRows = modelRows;
  const visibleChartModelRows = fallbackModelRows.slice(0, MAX_CHART_MODEL_SERIES);
  const overflowModelRows = fallbackModelRows.slice(MAX_CHART_MODEL_SERIES);
  const chartModelRows = overflowModelRows.length > 0
    ? [
      ...visibleChartModelRows,
      {
        model: 'other models',
        bucket: overflowModelRows.reduce<MetricsBucket>((bucket, row) => ({
          requests: bucket.requests + row.bucket.requests,
          input_tokens: bucket.input_tokens + row.bucket.input_tokens,
          output_tokens: bucket.output_tokens + row.bucket.output_tokens,
          total_tokens: bucket.total_tokens + (row.bucket.total_tokens || row.bucket.input_tokens + row.bucket.output_tokens),
          est_cost_usd: bucket.est_cost_usd + row.cost,
          errors: bucket.errors + row.bucket.errors,
          avg_latency_ms: 0,
        }), { requests: 0, input_tokens: 0, output_tokens: 0, total_tokens: 0, est_cost_usd: 0, errors: 0, avg_latency_ms: 0 }),
        color: OTHER_MODEL_COLOR,
        cost: overflowModelRows.reduce((sum, row) => sum + row.cost, 0),
        chartValue: overflowModelRows.reduce((sum, row) => sum + row.chartValue, 0),
      },
    ]
    : visibleChartModelRows;
  const chartModelSet = new Set(visibleChartModelRows.map((row) => row.model));
  const modelColorByName = fallbackModelRows.reduce<Record<string, string>>((colors, row) => {
    colors[row.model] = row.color || colorForModel(row.model);
    return colors;
  }, {});
  modelColorByName['other models'] = OTHER_MODEL_COLOR;
  const totalModelCost = fallbackModelRows.reduce((sum, row) => sum + row.cost, 0);
  const totalAgentCost = byAgent.reduce((sum, [, bucket]) => sum + costValue(bucket, byModel.length === 1 ? byModel[0][0] : undefined), 0);
  const chartStacks = byHour.length > 0 && chartModelRows.length > 0
    ? byHour.slice(-24).map((bucket, hourIndex) => {
      const modelEntries = Object.entries(bucket.by_model ?? {});
      const seriesValues = modelEntries
        .reduce<Record<string, number>>((series, [model, bucket]) => {
          const key = chartModelSet.has(model) || fallbackModelRows.length <= MAX_CHART_MODEL_SERIES ? model : 'other models';
          series[key] = (series[key] ?? 0) + chartValue(bucket, model);
          return series;
        }, {});
      const segments = chartModelRows
        .map((row) => ({ id: row.model, color: modelColorByName[row.model] ?? row.color, value: Math.max(0, seriesValues[row.model] ?? 0) }))
        .filter((segment) => segment.value > 0);
      return { hour: hourIndex, segments, total: segments.reduce((sum, segment) => sum + segment.value, 0) };
    })
    : [];
  const maxChartStack = Math.max(...chartStacks.map((stack) => stack.total), 0);
  const recentRows = recentCalls.length > 0
    ? recentCalls.slice(-10).reverse().map((entry) => ({
      id: `${entry.ts}:${entry.agent}:${entry.model}:${entry.status}`,
      time: new Date(entry.ts).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }),
      agent: entry.agent || 'platform',
      model: entry.model || 'unknown',
      color: colorForModel(entry.model || 'unknown'),
      input: formatTokens(entry.input_tokens),
      output: formatTokens(entry.output_tokens),
      latency: formatLatency(entry.duration_ms),
      cost: formatCostCell(entry.est_cost_usd || 0),
      status: entry.error ? `${entry.status || 'error'}` : 'ok',
      error: entry.error || '',
    }))
    : recentErrors.length > 0
    ? recentErrors.slice(0, 8).map((entry) => ({
      id: `${entry.ts}:${entry.agent}:${entry.model}:${entry.error}`,
      time: new Date(entry.ts).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }),
      agent: entry.agent || 'platform',
      model: entry.model || 'unknown',
      color: colorForModel(entry.model || 'unknown'),
      input: '-',
      output: '-',
      latency: '-',
      cost: '-',
      status: entry.status ? `${entry.status}` : 'error',
      error: entry.error,
    }))
    : fallbackModelRows.slice(0, 8).map((row) => ({
      id: `aggregate:${row.model}`,
      time: 'window',
      agent: topAgent?.[0] ?? 'all',
      model: row.model,
      color: row.color,
      input: formatTokens(row.bucket.input_tokens),
      output: formatTokens(row.bucket.output_tokens),
      latency: formatLatency(row.bucket.avg_latency_ms),
      cost: displayCost(row.bucket, row.model),
      status: row.bucket.errors > 0 ? `${row.bucket.errors} errors` : 'ok',
      error: '',
    }));

  const surfaceStyle = { display: 'flex', flexDirection: 'column' as const, gap: 20 };
  const panelStyle = { border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm-2)', overflow: 'hidden' };
  const sectionHeaderStyle = { display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 14, padding: '12px 16px', borderBottom: '0.5px solid var(--ink-hairline)' };
  const buttonBase = { border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 999, padding: '5px 10px', background: 'var(--warm)', color: 'var(--ink)', fontFamily: 'var(--sans)', fontSize: 12, cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 6, whiteSpace: 'nowrap' as const };
  const activeButton = { ...buttonBase, background: 'var(--ink)', color: 'var(--warm)' };
  const mutedButton = { ...buttonBase, color: 'var(--ink-mid)' };
  const miniLabel = { color: 'var(--teal-dark)', fontFamily: 'var(--mono)', fontSize: 9, letterSpacing: '0.16em', textTransform: 'uppercase' as const };
  const mutedText = { color: 'var(--ink-mid)' };
  const ledgerHeaderStyle = { color: 'var(--teal-dark)', fontFamily: 'var(--mono)', fontSize: 10, letterSpacing: '0.18em', textTransform: 'uppercase' as const };
  const ledgerCellStyle = { fontFamily: 'var(--mono)', fontSize: 15, color: 'var(--ink-mid)', minWidth: 0 };
  const ledgerStrongStyle = { fontFamily: 'var(--mono)', fontSize: 15, color: 'var(--ink)' };

  const RangeButton = ({ item }: { item: { key: RangePreset; label: string } }) => (
    <button type="button" onClick={() => handlePreset(item.key)} style={preset === item.key ? activeButton : mutedButton}>
      {item.label}
    </button>
  );

  function exportCsv() {
    const rows = [
      ['type', 'name', 'requests', 'input_tokens', 'output_tokens', 'total_tokens', 'errors', 'avg_latency_ms', 'estimated_cost_usd'],
      ...byAgent.map(([name, bucket]) => ['agent', name, bucket.requests, bucket.input_tokens, bucket.output_tokens, bucket.total_tokens, bucket.errors, bucket.avg_latency_ms, costValue(bucket, byModel.length === 1 ? byModel[0][0] : undefined).toFixed(6)]),
      ...byModel.map(([name, bucket]) => ['model', name, bucket.requests, bucket.input_tokens, bucket.output_tokens, bucket.total_tokens, bucket.errors, bucket.avg_latency_ms, costValue(bucket, name).toFixed(6)]),
      ...byProvider.map(([name, bucket]) => ['provider', name, bucket.requests, bucket.input_tokens, bucket.output_tokens, bucket.total_tokens, bucket.errors, bucket.avg_latency_ms, costValue(bucket).toFixed(6)]),
      ...bySource.map(([name, bucket]) => ['source', name, bucket.requests, bucket.input_tokens, bucket.output_tokens, bucket.total_tokens, bucket.errors, bucket.avg_latency_ms, costValue(bucket).toFixed(6)]),
    ];
    const csv = rows.map((row) => row.map((cell) => `"${String(cell).replace(/"/g, '""')}"`).join(',')).join('\n');
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = `agency-usage-${preset}.csv`;
    anchor.click();
    URL.revokeObjectURL(url);
  }

  return (
    <div style={surfaceStyle}>
      <section style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 18, flexWrap: 'wrap' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
          <div style={{ display: 'inline-flex', background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 999, padding: 2 }}>
            {RANGE_PRESETS.map((item) => <RangeButton key={item.key} item={item} />)}
          </div>
          <Popover open={calendarOpen} onOpenChange={setCalendarOpen}>
            <PopoverTrigger asChild>
              <button type="button" onClick={() => { setPreset('custom'); setCalendarOpen(true); }} style={preset === 'custom' ? activeButton : mutedButton}>
                <CalendarIcon size={14} />
                {preset === 'custom' && dateRange?.from ? `${formatDateShort(dateRange.from)}${dateRange.to ? ` - ${formatDateShort(dateRange.to)}` : ''}` : 'Custom'}
              </button>
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
          <button type="button" onClick={exportCsv} style={mutedButton}>
            Export CSV
          </button>
          <button type="button" onClick={() => load()} disabled={refreshing} style={{ ...mutedButton, opacity: refreshing ? 0.65 : 1 }} aria-label="Refresh usage">
            <RefreshCw size={14} className={refreshing ? 'animate-spin' : ''} />
            Refresh
          </button>
        </div>
      </section>

      {error && (
        <div style={{ border: '0.5px solid var(--red)', borderRadius: 12, background: 'var(--red-tint)', color: 'var(--red)', padding: '12px 14px', fontSize: 13 }}>{error}</div>
      )}

      {loading ? (
        <div style={{ ...panelStyle, padding: 40, textAlign: 'center', color: 'var(--ink-mid)' }}>Loading usage data...</div>
      ) : !t ? (
        <div style={{ ...panelStyle, padding: 40, textAlign: 'center', color: 'var(--ink-mid)' }}>No metrics available</div>
      ) : (
        <>
          <section style={{ padding: '4px 0 8px' }}>
            <div style={{ ...miniLabel, marginBottom: 16 }}>Usage & cost</div>
            <div style={{ display: 'flex', gap: 38, flexWrap: 'wrap', alignItems: 'flex-start' }}>
              {summaryMetrics.map((metric) => (
                <MetaStat key={metric.label} label={metric.label} value={metric.value} detail={metric.detail} tone={metric.tone} />
              ))}
            </div>
          </section>

          <Panel padded>
            <div style={{ display: 'flex', alignItems: 'baseline', gap: 14, marginBottom: 10, flexWrap: 'wrap' }}>
              <div style={miniLabel}>Monthly budget</div>
              <span className="mono" style={{ fontSize: 11, color: 'var(--ink-mid)' }}>
                ${projectedMonth.toFixed(projectedMonth >= 10 ? 0 : 2)} projected of ${monthlyBudget}
              </span>
              <span className="mono" style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--ink-faint)' }}>{rangeLabel}</span>
            </div>
            <div style={{ position: 'relative', height: 10, background: 'var(--warm-3)', borderRadius: 2, overflow: 'hidden' }}>
              <div style={{ position: 'absolute', left: 0, top: 0, bottom: 0, width: `${budgetUsedPercent}%`, background: budgetUsedPercent >= 90 ? 'var(--red)' : budgetUsedPercent >= 60 ? 'var(--amber)' : 'var(--teal)' }} />
              <div style={{ position: 'absolute', left: '60%', top: -2, bottom: -2, width: 1, background: 'var(--amber)' }} />
              <div style={{ position: 'absolute', left: '90%', top: -2, bottom: -2, width: 1, background: 'var(--red)' }} />
            </div>
            <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 6 }}>
              <span className="mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>$0</span>
              <span className="mono" style={{ fontSize: 10, color: 'var(--amber)' }}>$240 soft</span>
              <span className="mono" style={{ fontSize: 10, color: 'var(--red)' }}>$360 hard</span>
              <span className="mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>${monthlyBudget}</span>
            </div>
          </Panel>

          {routingOptimizerEnabled && (
            <section style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 10, flexWrap: 'wrap' }}>
              <div style={miniLabel}>Mode</div>
              <div style={{ display: 'inline-flex', border: '0.5px solid var(--ink-hairline)', borderRadius: 6, padding: 2, background: 'var(--warm-2)' }}>
                {([
                  ['usage', 'Usage'],
                  ['optimizer', 'Optimizer'],
                ] as const).map(([value, label]) => (
                  <button
                    key={value}
                    type="button"
                    onClick={() => setView(value)}
                    style={view === value ? { ...activeButton, borderRadius: 4, padding: '5px 12px' } : { ...mutedButton, border: 0, borderRadius: 4, padding: '5px 12px' }}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </section>
          )}

          {view === 'usage' && (
            <>
              <Panel>
                <div style={sectionHeaderStyle}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
                    <div style={miniLabel}>Spend by model</div>
                    <div style={{ display: 'inline-flex', border: '0.5px solid var(--ink-hairline)', borderRadius: 6, padding: 2, background: 'var(--warm)' }}>
                      <button type="button" onClick={() => setChartMetric('cost')} style={chartMetric === 'cost' ? { ...activeButton, borderRadius: 4, padding: '5px 11px' } : { ...mutedButton, border: 0, borderRadius: 4, padding: '5px 11px' }} aria-pressed={chartMetric === 'cost'}>$</button>
                      <button type="button" onClick={() => setChartMetric('tokens')} style={chartMetric === 'tokens' ? { ...activeButton, borderRadius: 4, padding: '5px 11px' } : { ...mutedButton, border: 0, borderRadius: 4, padding: '5px 11px' }} aria-pressed={chartMetric === 'tokens'}>tokens</button>
                    </div>
                  </div>
                  <span className="mono" style={{ color: 'var(--ink-faint)', fontSize: 11 }}>{rangeLabel}</span>
                </div>
                <div style={{ padding: '16px 20px 18px' }}>
                  {chartModelRows.length === 0 ? (
                    <div style={{ height: 160, display: 'grid', placeItems: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>
                      No per-model usage recorded in the selected period.
                    </div>
                  ) : (
                    <>
                      <div style={{ display: 'flex', gap: 18, flexWrap: 'wrap', marginBottom: 18 }}>
                        {chartModelRows.map((row) => (
                          <div key={row.model} style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
                            <span style={{ width: 10, height: 10, borderRadius: 2, background: row.color }} />
                            <span className="mono" style={{ fontSize: 11, color: 'var(--ink)' }}>{row.model}</span>
                            <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)' }}>{chartLegendValue(row.bucket, row.model)}</span>
                          </div>
                        ))}
                      </div>
                      {chartStacks.length === 0 || maxChartStack <= 0 ? (
                        <div style={{ height: 160, display: 'grid', placeItems: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>
                          No hourly model buckets recorded yet.
                        </div>
                      ) : (
                        <div style={{ display: 'flex', alignItems: 'flex-end', gap: 3, height: 160 }}>
                          {chartStacks.map((stack) => (
                            <div key={stack.hour} title={`bucket ${stack.hour + 1}`} style={{ flex: 1, display: 'flex', flexDirection: 'column-reverse', minWidth: 4, height: stack.total > 0 ? `${Math.max(2, (stack.total / maxChartStack) * 100)}%` : 0 }}>
                              {stack.segments.map((segment) => (
                                <div key={segment.id} style={{ height: stack.total > 0 ? `${(segment.value / stack.total) * 100}%` : 0, background: segment.color }} />
                              ))}
                            </div>
                          ))}
                        </div>
                      )}
                      <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 8 }}>
                        {['-24h', '-18h', '-12h', '-6h'].map((label) => (
                          <span key={label} className="mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>{label}</span>
                        ))}
                      </div>
                    </>
                  )}
                </div>
              </Panel>

              <section>
                <div style={{ display: 'flex', alignItems: 'baseline', gap: 12, marginBottom: 10 }}>
                  <div style={miniLabel}>Break out by</div>
                  <div style={{ display: 'inline-flex', border: '0.5px solid var(--ink-hairline)', borderRadius: 6, padding: 2, background: 'var(--warm)' }}>
                    <button type="button" onClick={() => setBreakout('model')} style={breakout === 'model' ? { ...activeButton, borderRadius: 4, padding: '6px 13px' } : { ...mutedButton, border: 0, borderRadius: 4, padding: '6px 13px' }}>Model</button>
                    <button type="button" onClick={() => setBreakout('agent')} style={breakout === 'agent' ? { ...activeButton, borderRadius: 4, padding: '6px 13px' } : { ...mutedButton, border: 0, borderRadius: 4, padding: '6px 13px' }}>Agent</button>
                  </div>
                </div>
                {breakout === 'model' ? (
                  <Panel>
                    <div style={{ display: 'grid', gridTemplateColumns: 'minmax(270px, 1.6fr) 90px 120px 120px 130px 130px 170px 100px', gap: 16, padding: '14px 20px', borderBottom: '0.5px solid var(--ink-hairline)', alignItems: 'center' }}>
                      {['Model', 'Calls', 'Input', 'Output', '$ / 1M in', '$ / 1M out', 'Share', 'Cost'].map((label) => (
                        <div key={label} style={ledgerHeaderStyle}>{label}</div>
                      ))}
                    </div>
                    {fallbackModelRows.length === 0 ? (
                      <div style={{ padding: 20, color: 'var(--ink-mid)' }}>No per-model usage recorded in the selected period.</div>
                    ) : fallbackModelRows.map((row) => {
                      const share = totalModelCost > 0 ? (row.cost / totalModelCost) * 100 : 0;
                      const pricing = pricingForModel(row.model);
                      return (
                        <div key={row.model} style={{ display: 'grid', gridTemplateColumns: 'minmax(270px, 1.6fr) 90px 120px 120px 130px 130px 170px 100px', gap: 16, padding: '20px', borderBottom: '0.5px solid var(--ink-hairline)', alignItems: 'center' }}>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 13, minWidth: 0 }}>
                            <span style={{ width: 11, height: 11, borderRadius: 3, background: row.color, flexShrink: 0 }} />
                            <span style={{ ...ledgerStrongStyle, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{row.model}</span>
                            <span style={{ ...ledgerHeaderStyle, color: 'var(--ink-faint)', letterSpacing: '0.16em', whiteSpace: 'nowrap' }}>{providerForModel(row.model)}</span>
                          </div>
                          <div style={ledgerCellStyle}>{row.bucket.requests.toLocaleString()}</div>
                          <div style={ledgerCellStyle}>{formatTokens(row.bucket.input_tokens)}</div>
                          <div style={ledgerCellStyle}>{row.bucket.output_tokens > 0 ? formatTokens(row.bucket.output_tokens) : '-'}</div>
                          <div style={{ ...ledgerCellStyle, color: 'var(--ink-faint)' }}>{formatRate(pricing.input)}</div>
                          <div style={{ ...ledgerCellStyle, color: 'var(--ink-faint)' }}>{formatRate(pricing.output)}</div>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 12, minWidth: 0 }}>
                            <div aria-label={`${row.model} share ${share.toFixed(0)}%`} style={{ width: 96, height: 6, background: 'var(--warm-3)', borderRadius: 3, overflow: 'hidden' }}>
                              <div style={{ width: `${share}%`, height: '100%', background: row.color }} />
                            </div>
                            <span style={{ ...ledgerCellStyle, color: 'var(--ink-faint)', fontSize: 13 }}>{share.toFixed(0)}%</span>
                          </div>
                          <div style={{ ...ledgerStrongStyle, textAlign: 'left' }}>{formatCostCell(row.cost)}</div>
                        </div>
                      );
                    })}
                  </Panel>
                ) : (
                  <Panel>
                    <div style={{ display: 'grid', gridTemplateColumns: 'minmax(220px, 1.6fr) 90px 120px 120px 170px 100px', gap: 16, padding: '14px 20px', borderBottom: '0.5px solid var(--ink-hairline)', alignItems: 'center' }}>
                      {['Agent', 'Calls', 'Input', 'Output', 'Share', 'Cost'].map((label) => (
                        <div key={label} style={ledgerHeaderStyle}>{label}</div>
                      ))}
                    </div>
                    {byAgent.length === 0 ? (
                      <div style={{ padding: 20, color: 'var(--ink-mid)' }}>No per-agent usage recorded in the selected period.</div>
                    ) : byAgent.slice().sort((a, b) => b[1].requests - a[1].requests).map(([agent, bucket], index) => {
                      const cost = costValue(bucket, byModel.length === 1 ? byModel[0][0] : undefined);
                      const share = totalAgentCost > 0 ? (cost / totalAgentCost) * 100 : 0;
                      const color = colorForModel(agent);
                      return (
                        <div key={agent} style={{ display: 'grid', gridTemplateColumns: 'minmax(220px, 1.6fr) 90px 120px 120px 170px 100px', gap: 16, padding: '20px', borderBottom: '0.5px solid var(--ink-hairline)', alignItems: 'center' }}>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 13, minWidth: 0 }}>
                            <span style={{ width: 11, height: 11, borderRadius: 3, background: color, flexShrink: 0 }} />
                            <span style={{ ...ledgerStrongStyle, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{agent}</span>
                          </div>
                          <div style={ledgerCellStyle}>{bucket.requests.toLocaleString()}</div>
                          <div style={ledgerCellStyle}>{formatTokens(bucket.input_tokens)}</div>
                          <div style={ledgerCellStyle}>{bucket.output_tokens > 0 ? formatTokens(bucket.output_tokens) : '-'}</div>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 12, minWidth: 0 }}>
                            <div aria-label={`${agent} share ${share.toFixed(0)}%`} style={{ width: 96, height: 6, background: 'var(--warm-3)', borderRadius: 3, overflow: 'hidden' }}>
                              <div style={{ width: `${share}%`, height: '100%', background: color }} />
                            </div>
                            <span style={{ ...ledgerCellStyle, color: 'var(--ink-faint)', fontSize: 13 }}>{share.toFixed(0)}%</span>
                          </div>
                          <div style={ledgerStrongStyle}>{formatCostCell(cost)}</div>
                        </div>
                      );
                    })}
                  </Panel>
                )}
              </section>

              <section>
                <div style={{ ...miniLabel, marginBottom: 10 }}>Recent calls</div>
                <Panel>
                  <div style={{ display: 'grid', gridTemplateColumns: '120px 180px minmax(260px, 1.4fr) 90px 90px 120px 110px 34px', gap: 16, padding: '14px 20px', borderBottom: '0.5px solid var(--ink-hairline)', alignItems: 'center' }}>
                    {['Time', 'Agent', 'Model', 'In', 'Out', 'Latency', 'Cost', ''].map((label) => (
                      <div key={label || 'action'} style={ledgerHeaderStyle}>{label}</div>
                    ))}
                  </div>
                  {recentRows.map((row) => (
                    <div key={row.id} style={{ display: 'grid', gridTemplateColumns: '120px 180px minmax(260px, 1.4fr) 90px 90px 120px 110px 34px', gap: 16, padding: '18px 20px', borderBottom: '0.5px solid var(--ink-hairline)', alignItems: 'center', boxShadow: row.error ? 'inset 2px 0 0 var(--red)' : undefined }}>
                      <div style={{ ...ledgerCellStyle, color: 'var(--ink-faint)' }}>{row.time}</div>
                      <div style={ledgerStrongStyle}>{row.agent}</div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
                        <span style={{ width: 9, height: 9, borderRadius: 3, background: row.color, flexShrink: 0 }} />
                        <span style={{ ...ledgerStrongStyle, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{row.model}</span>
                      </div>
                      <div style={ledgerCellStyle}>{row.input}</div>
                      <div style={ledgerCellStyle}>{row.output}</div>
                      <div style={{ ...ledgerCellStyle, color: row.error ? 'var(--red)' : 'var(--amber)' }}>{row.latency}</div>
                      <div style={ledgerStrongStyle}>{row.cost}</div>
                      <div style={{ ...ledgerCellStyle, color: row.error ? 'var(--red)' : 'var(--ink-faint)' }}>{row.error ? row.status : '→'}</div>
                    </div>
                  ))}
                </Panel>
              </section>

              <Panel>
                <div style={sectionHeaderStyle}>
                  <div>
                    <div style={miniLabel}>Cost components</div>
                    <p style={{ margin: '6px 0 0', ...mutedText }}>The same spend, separated by pricing source.</p>
                  </div>
                  <span className="mono" style={{ color: 'var(--ink-faint)', fontSize: 11 }}>{pricedComponentTotal > 0 ? `$${pricedComponentTotal.toFixed(4)} priced` : 'no priced spend'}</span>
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(210px, 1fr))' }}>
                  {costComponents.map((component, index) => (
                    <div key={component.label} style={{ padding: 16, borderTop: index === 0 ? 0 : undefined, borderLeft: index > 0 ? '0.5px solid var(--ink-hairline)' : 0 }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <span style={{ width: 8, height: 8, borderRadius: 2, background: component.tone }} />
                        <div style={miniLabel}>{component.label}</div>
                      </div>
                      <div className="mono" style={{ marginTop: 10, color: component.included ? 'var(--ink)' : 'var(--ink-faint)', fontSize: 20 }}>
                        {component.value > 0 ? `$${component.value.toFixed(4)}` : '—'}
                      </div>
                      <div style={{ marginTop: 5, color: component.label === 'Unpriced tools' ? 'var(--amber)' : 'var(--ink-mid)', fontSize: 12 }}>{component.meta}</div>
                    </div>
                  ))}
                </div>
                {byProviderTool.length > 0 && (
                  <div style={{ borderTop: '0.5px solid var(--ink-hairline)' }}>
                    <TableHeader widths="minmax(220px, 1fr) 100px 120px 90px" cols={['Provider tool', 'Calls', 'Confidence', 'Cost']} />
                    {byProviderTool.slice(0, 6).map(([capability, bucket]) => (
                      <TableRow key={capability} widths="minmax(220px, 1fr) 100px 120px 90px" cols={[
                        <code style={{ color: 'var(--ink)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{capability}</code>,
                        <span className="mono" style={{ color: 'var(--ink-mid)' }}>{(bucket.provider_tool_calls ?? bucket.requests).toLocaleString()}</span>,
                        <span className="mono" style={{ color: (bucket.provider_tool_unpriced_calls ?? 0) > 0 ? 'var(--amber)' : 'var(--ink-faint)' }}>{formatProviderToolMeta(bucket.provider_tool_cost_confidence)}</span>,
                        <span className="mono" style={{ color: 'var(--ink)' }}>${(bucket.provider_tool_cost_usd ?? bucket.est_cost_usd ?? 0).toFixed(4)}</span>,
                      ]} />
                    ))}
                  </div>
                )}
              </Panel>
            </>
          )}

          {view === 'optimizer' && routingOptimizerEnabled && (
            <section style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))', gap: 16 }}>
              <div style={panelStyle}>
                <div style={sectionHeaderStyle}><div><div style={miniLabel}>Routing Suggestions</div><p style={{ margin: '6px 0 0', ...mutedText }}>Pending optimizer recommendations for lower-cost model routing.</p></div><button type="button" onClick={loadSuggestions} disabled={suggestionsLoading} style={mutedButton}><RefreshCw size={14} className={suggestionsLoading ? 'animate-spin' : ''} />Refresh</button></div>
                {suggestionsLoading ? <div style={{ padding: 24, color: 'var(--ink-mid)' }}>Loading routing suggestions...</div> : suggestions.length === 0 ? <div style={{ padding: 24, color: 'var(--ink-mid)' }}>No pending routing suggestions</div> : suggestions.map((s) => (
                  <div key={s.id} style={{ padding: '16px 20px', borderTop: '0.5px solid var(--ink-hairline)', display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 16 }}>
                    <div>
                      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}><code>{s.task_type || 'unknown-task'}</code><span style={mutedText}>route</span><code>{s.current_model || 'current'}</code><span style={mutedText}>to</span><code style={{ color: 'var(--teal-dark)' }}>{s.suggested_model || 'suggested'}</code></div>
                      <div style={{ marginTop: 6, color: 'var(--ink-mid)', fontSize: 13 }}>{s.reason || 'No reason supplied.'}</div>
                      <div style={{ marginTop: 8, display: 'flex', gap: 8, flexWrap: 'wrap', color: 'var(--teal-dark)', fontSize: 12 }}><span>{formatSavingsPercent(s.savings_percent)} savings</span><span>{formatSavingsUSD(s.savings_usd_per_1k)} / 1K calls</span></div>
                    </div>
                    {s.status === 'pending' && <div style={{ display: 'flex', gap: 8 }}><button type="button" onClick={() => handleSuggestionAction(s.id, 'reject')} disabled={suggestionAction !== null} style={mutedButton}><X size={13} />Reject</button><button type="button" onClick={() => handleSuggestionAction(s.id, 'approve')} disabled={suggestionAction !== null} style={activeButton}><Check size={13} />Approve</button></div>}
                  </div>
                ))}
              </div>
              <div style={panelStyle}>
                <div style={sectionHeaderStyle}><div><div style={miniLabel}>Routing Model Stats</div><p style={{ margin: '6px 0 0', ...mutedText }}>Per-model optimizer telemetry by task type.</p></div><button type="button" onClick={loadRoutingStats} disabled={statsLoading} style={mutedButton}><RefreshCw size={14} className={statsLoading ? 'animate-spin' : ''} />Refresh</button></div>
                {statsLoading ? <div style={{ padding: 24, color: 'var(--ink-mid)' }}>Loading routing stats...</div> : routingStats.length === 0 ? <div style={{ padding: 24, color: 'var(--ink-mid)' }}>No routing stats available</div> : routingStats.map((s) => (
                  <div key={`${s.task_type}:${s.model}`} style={{ padding: '14px 20px', borderTop: '0.5px solid var(--ink-hairline)', display: 'grid', gridTemplateColumns: '1fr repeat(5, auto)', gap: 12, alignItems: 'center' }}>
                    <div><div style={{ color: 'var(--ink)' }}>{s.task_type || 'unknown'}</div><div style={{ marginTop: 3, color: 'var(--ink-mid)', fontSize: 12 }}>{s.model || 'unknown'}</div></div>
                    <span>{s.total_calls}</span><span>{s.retries} retries</span><span style={{ color: 'var(--teal-dark)' }}>{Math.round((s.success_rate || 0) * 100)}%</span><span>{((s.avg_latency_ms || 0) / 1000).toFixed(1)}s</span><span style={{ color: 'var(--teal-dark)' }}>${(s.cost_per_1k || 0).toFixed(4)}</span>
                  </div>
                ))}
              </div>
            </section>
          )}

        </>
      )}
    </div>
  );
}
