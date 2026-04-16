import { useCallback, useEffect, useMemo, useState } from 'react';
import { RefreshCw, Shield, Search } from 'lucide-react';
import { api, type RawProviderToolCapability, type RawProviderToolProvider } from '../lib/api';
import { Button } from '../components/ui/button';

const PROVIDERS = ['openai', 'anthropic', 'google'] as const;

type ProviderName = typeof PROVIDERS[number];

interface ProviderToolRow {
  name: string;
  tool: RawProviderToolCapability;
}

function statusTone(status: string) {
  switch (status) {
    case 'supported':
      return 'border-green-200 bg-green-50 text-green-700 dark:border-green-900/40 dark:bg-green-950/20 dark:text-green-400';
    case 'partial':
    case 'beta':
    case 'inventoried':
      return 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900/40 dark:bg-amber-950/20 dark:text-amber-400';
    case 'harness_required':
    case 'agency_native':
      return 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900/40 dark:bg-blue-950/20 dark:text-blue-400';
    case 'harness_unavailable':
      return 'border-red-200 bg-red-50 text-red-700 dark:border-red-900/40 dark:bg-red-950/20 dark:text-red-400';
    case 'no_equivalent':
      return 'border-border bg-secondary text-muted-foreground';
    default:
      return 'border-border bg-background text-muted-foreground';
  }
}

function riskTone(risk: string) {
  switch (risk) {
    case 'critical':
      return 'text-red-500';
    case 'high':
      return 'text-amber-500';
    case 'medium':
      return 'text-blue-500';
    default:
      return 'text-muted-foreground';
  }
}

function pricingLabel(provider?: RawProviderToolProvider) {
  const pricing = provider?.pricing || {};
  const confidence = typeof pricing.confidence === 'string' ? pricing.confidence : 'unknown';
  const unit = typeof pricing.unit === 'string' ? pricing.unit : '';
  const price = typeof pricing.usd_per_unit === 'number' ? `$${pricing.usd_per_unit.toFixed(4)}` : '';
  return [confidence, price, unit].filter(Boolean).join(' · ');
}

function testCoverage(provider?: RawProviderToolProvider) {
  const tests = provider?.tests || [];
  if (tests.length === 0) return 'none';
  if (tests.includes('inventory_only')) return 'inventory only';
  return `${tests.length} checks`;
}

function statusLabel(status: string) {
  return status.replace(/_/g, ' ');
}

function sortRows(rows: ProviderToolRow[]) {
  const riskOrder: Record<string, number> = { critical: 0, high: 1, medium: 2, low: 3 };
  return [...rows].sort((a, b) => {
    const riskDiff = (riskOrder[a.tool.risk] ?? 9) - (riskOrder[b.tool.risk] ?? 9);
    if (riskDiff !== 0) return riskDiff;
    return a.name.localeCompare(b.name);
  });
}

export function AdminProviderTools() {
  const [tools, setTools] = useState<Record<string, RawProviderToolCapability>>({});
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState('');
  const [query, setQuery] = useState('');

  const load = useCallback(async () => {
    setRefreshing(true);
    try {
      const inventory = await api.providers.tools();
      setTools(inventory.capabilities || {});
      setError('');
    } catch (err) {
      setTools({});
      setError(err instanceof Error ? err.message : 'Provider tool inventory unavailable.');
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    const all = sortRows(Object.entries(tools).map(([name, tool]) => ({ name, tool })));
    if (!q) return all;
    return all.filter(({ name, tool }) => {
      const providerText = Object.entries(tool.providers || {})
        .map(([provider, meta]) => `${provider} ${meta.status} ${(meta.request_tools || []).join(' ')}`)
        .join(' ');
      return [
        name,
        tool.title,
        tool.description,
        tool.risk,
        tool.execution,
        providerText,
      ].join(' ').toLowerCase().includes(q);
    });
  }, [query, tools]);

  const total = Object.keys(tools).length;
  const defaultCount = Object.values(tools).filter((tool) => tool.default_grant).length;
  const harnessedCount = Object.values(tools).filter((tool) => tool.execution === 'agency_harnessed').length;
  const exactPricingCount = Object.values(tools).filter((tool) =>
    Object.values(tool.providers || {}).some((provider) => provider.pricing?.confidence === 'exact'),
  ).length;
  const unknownPricingCount = Object.values(tools).filter((tool) =>
    Object.values(tool.providers || {}).some((provider) => {
      const status = provider.status || '';
      return status !== 'no_equivalent' && provider.pricing?.confidence === 'unknown';
    }),
  ).length;

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div className="grid grid-cols-2 gap-2 md:grid-cols-5">
          <div className="rounded-2xl border border-border bg-card p-3">
            <div className="text-[10px] uppercase tracking-[0.14em] text-muted-foreground">Capabilities</div>
            <div className="mt-1 text-xl text-foreground">{total}</div>
          </div>
          <div className="rounded-2xl border border-border bg-card p-3">
            <div className="text-[10px] uppercase tracking-[0.14em] text-muted-foreground">Default</div>
            <div className="mt-1 text-xl text-green-400">{defaultCount}</div>
          </div>
          <div className="rounded-2xl border border-border bg-card p-3">
            <div className="text-[10px] uppercase tracking-[0.14em] text-muted-foreground">Harnessed</div>
            <div className="mt-1 text-xl text-blue-400">{harnessedCount}</div>
          </div>
          <div className="rounded-2xl border border-border bg-card p-3">
            <div className="text-[10px] uppercase tracking-[0.14em] text-muted-foreground">Exact pricing</div>
            <div className="mt-1 text-xl text-foreground">{exactPricingCount}</div>
          </div>
          <div className="rounded-2xl border border-border bg-card p-3">
            <div className="text-[10px] uppercase tracking-[0.14em] text-muted-foreground">Unknown pricing</div>
            <div className="mt-1 text-xl text-amber-400">{unknownPricingCount}</div>
          </div>
        </div>
        <div className="flex flex-col gap-2 sm:flex-row">
          <label className="relative block">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Filter tools..."
              className="h-9 w-full rounded-xl border border-border bg-card pl-9 pr-3 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-primary sm:w-64"
            />
          </label>
          <Button variant="outline" size="sm" onClick={load} disabled={refreshing}>
            <RefreshCw className={`mr-1 h-3 w-3 ${refreshing ? 'animate-spin' : ''}`} />
            {refreshing ? 'Refreshing...' : 'Refresh'}
          </Button>
        </div>
      </div>

      {error && (
        <div className="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800 dark:border-amber-900/40 dark:bg-amber-950/20 dark:text-amber-300">
          {error}
        </div>
      )}

      {loading ? (
        <div className="py-12 text-center text-sm text-muted-foreground">Loading provider tools...</div>
      ) : rows.length === 0 ? (
        <div className="rounded-2xl border border-border bg-card p-8 text-center text-sm text-muted-foreground">
          No provider tools match the current filter.
        </div>
      ) : (
        <div className="overflow-x-auto rounded-2xl border border-border bg-card">
          <table className="w-full min-w-[1060px] text-sm">
            <thead className="border-b border-border bg-secondary/50 text-xs uppercase tracking-[0.12em] text-muted-foreground">
              <tr>
                <th className="p-3 text-left font-medium">Capability</th>
                <th className="p-3 text-left font-medium">Boundary</th>
                {PROVIDERS.map((provider) => (
                  <th key={provider} className="p-3 text-left font-medium">{provider}</th>
                ))}
                <th className="p-3 text-left font-medium">Coverage</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(({ name, tool }) => (
                <tr key={name} className="border-b border-border/70 last:border-0 align-top">
                  <td className="p-3">
                    <div className="flex items-start gap-2">
                      <Shield className={`mt-0.5 h-4 w-4 flex-shrink-0 ${riskTone(tool.risk)}`} />
                      <div>
                        <div className="font-medium text-foreground">{tool.title || name}</div>
                        <div className="mt-0.5 font-mono text-xs text-muted-foreground">{name}</div>
                        <div className="mt-1 max-w-xs text-xs text-muted-foreground">{tool.description}</div>
                      </div>
                    </div>
                  </td>
                  <td className="p-3">
                    <div className={`text-xs font-medium ${riskTone(tool.risk)}`}>{tool.risk}</div>
                    <div className="mt-1 text-xs text-foreground/80">{tool.execution.replace(/_/g, ' ')}</div>
                    {tool.default_grant && (
                      <div className="mt-2 inline-flex rounded-full border border-green-200 bg-green-50 px-2 py-0.5 text-[10px] font-medium text-green-700 dark:border-green-900/40 dark:bg-green-950/20 dark:text-green-400">
                        default grant
                      </div>
                    )}
                  </td>
                  {PROVIDERS.map((providerName: ProviderName) => {
                    const provider = tool.providers?.[providerName];
                    const status = provider?.status || 'unknown';
                    return (
                      <td key={providerName} className="p-3">
                        <div className={`inline-flex rounded-full border px-2 py-0.5 text-[10px] font-medium ${statusTone(status)}`}>
                          {statusLabel(status)}
                        </div>
                        <div className="mt-2 text-xs text-muted-foreground">{pricingLabel(provider) || 'pricing n/a'}</div>
                        {(provider?.request_tools?.length || 0) > 0 && (
                          <div className="mt-1 max-w-[14rem] truncate font-mono text-[10px] text-muted-foreground">
                            {provider?.request_tools?.join(', ')}
                          </div>
                        )}
                      </td>
                    );
                  })}
                  <td className="p-3">
                    <div className="space-y-1">
                      {PROVIDERS.map((providerName) => (
                        <div key={providerName} className="text-xs text-muted-foreground">
                          <span className="font-medium text-foreground/80">{providerName}</span>: {testCoverage(tool.providers?.[providerName])}
                        </div>
                      ))}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
