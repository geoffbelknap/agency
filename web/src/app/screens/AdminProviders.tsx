import { useCallback, useEffect, useMemo, useState } from 'react';
import { ExternalLink, KeyRound, Loader2, RefreshCw } from 'lucide-react';
import { toast } from 'sonner';
import { api, type RawRoutingConfig } from '../lib/api';
import type { Provider } from '../types';
import { Input } from '../components/ui/input';
import { Button } from '../components/ui/button';

function ProviderBadge({ children, tone = 'neutral' }: { children: React.ReactNode; tone?: 'neutral' | 'ok' | 'warn' }) {
  const styles = {
    neutral: { background: 'var(--warm-3)', color: 'var(--ink-mid)' },
    ok: { background: 'var(--teal-tint)', color: 'var(--teal-dark)' },
    warn: { background: 'var(--amber-tint)', color: '#8B5A00' },
  }[tone];
  return (
    <span className="mono" style={{ display: 'inline-flex', alignItems: 'center', borderRadius: 999, padding: '3px 8px', fontSize: 10, letterSpacing: '0.08em', textTransform: 'uppercase', ...styles }}>
      {children}
    </span>
  );
}

function providerStatus(provider: Provider) {
  if (provider.credential_configured) return { label: 'credential ready', tone: 'ok' as const };
  if (provider.installed) return { label: 'routing installed', tone: 'warn' as const };
  return { label: 'available', tone: 'neutral' as const };
}

function providerModels(config: RawRoutingConfig | null, providerName: string) {
  return Object.entries(config?.models || {})
    .filter(([, model]) => model.provider === providerName)
    .map(([name, model]) => ({ name, ...model }))
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function AdminProviders() {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [routingConfig, setRoutingConfig] = useState<RawRoutingConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [busyProvider, setBusyProvider] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [keyInputs, setKeyInputs] = useState<Record<string, string>>({});
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    setRefreshing(true);
    try {
      const [providerList, routing] = await Promise.all([
        api.providers.list(),
        api.providers.routingConfig().catch(() => null),
      ]);
      setProviders(providerList || []);
      setRoutingConfig(routing);
      setError('');
    } catch (err) {
      setProviders([]);
      setRoutingConfig(null);
      setError(err instanceof Error ? err.message : 'Provider state unavailable.');
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const configuredCount = providers.filter((provider) => provider.credential_configured).length;
  const installedCount = providers.filter((provider) => provider.installed).length;
  const modelCount = Object.keys(routingConfig?.models || {}).length;
  const providerCount = Object.keys(routingConfig?.providers || {}).length;
  const tierRows = useMemo(() => Object.entries(routingConfig?.tiers || {}), [routingConfig]);

  async function handleSaveProvider(provider: Provider) {
    const credentialName = provider.credential_name;
    const key = (keyInputs[provider.name] || '').trim();

    if (credentialName && !key && !provider.credential_configured) {
      toast.error(`Enter ${provider.credential_label || 'an API key'} before saving ${provider.display_name}`);
      return;
    }

    setBusyProvider(provider.name);
    try {
      if (credentialName && key) {
        await api.credentials.store(credentialName, key, { kind: 'provider', scope: 'global', protocol: 'api-key' });
        const result = await api.credentials.test(credentialName);
        if (!result.ok) {
          throw new Error(result.message || 'Credential test failed');
        }
      }

      if (!provider.installed) {
        await api.providers.install(provider.name);
      }

      toast.success(`${provider.display_name} provider ready`);
      setKeyInputs((current) => ({ ...current, [provider.name]: '' }));
      await load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : `Failed to configure ${provider.display_name}`);
    } finally {
      setBusyProvider(null);
    }
  }

  return (
    <div style={{ display: 'grid', gap: 18 }}>
      <section style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 16, flexWrap: 'wrap' }}>
        <div>
          <div className="eyebrow" style={{ marginBottom: 8 }}>Providers</div>
          <h3 className="display" style={{ margin: 0, fontSize: 32, fontWeight: 300, lineHeight: 1.1, color: 'var(--ink)' }}>Model provider operations</h3>
          <p style={{ margin: '8px 0 0', maxWidth: 680, color: 'var(--ink-mid)', fontSize: 13, lineHeight: 1.55 }}>
            Install bundled routing definitions, update provider credentials, and inspect the active model and tier map.
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={load} disabled={refreshing}>
          <RefreshCw className={`mr-1 h-3.5 w-3.5 ${refreshing ? 'animate-spin' : ''}`} />
          {refreshing ? 'Refreshing...' : 'Refresh'}
        </Button>
      </section>

      <section style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(170px, 1fr))', gap: 10 }}>
        {[
          ['Credentialed', configuredCount],
          ['Installed', installedCount],
          ['Routing providers', providerCount],
          ['Models', modelCount],
        ].map(([label, value]) => (
          <div key={label} style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm-2)', padding: 14 }}>
            <div className="eyebrow" style={{ fontSize: 9 }}>{label}</div>
            <div className="mono" style={{ marginTop: 8, fontSize: 24, color: 'var(--ink)' }}>{value}</div>
          </div>
        ))}
      </section>

      {error && <div style={{ border: '0.5px solid var(--red)', borderRadius: 10, background: 'var(--red-tint)', color: 'var(--red)', padding: 12, fontSize: 13 }}>{error}</div>}

      {loading ? (
        <div style={{ padding: 40, textAlign: 'center', color: 'var(--ink-mid)' }}>Loading providers...</div>
      ) : (
        <section style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm-2)', overflow: 'hidden' }}>
          {providers.length === 0 ? (
            <div style={{ padding: 22, color: 'var(--ink-mid)' }}>No bundled providers available.</div>
          ) : providers.map((provider, index) => {
            const status = providerStatus(provider);
            const isExpanded = expanded === provider.name;
            const models = providerModels(routingConfig, provider.name);
            const busy = busyProvider === provider.name;
            return (
              <div key={provider.name} style={{ borderTop: index === 0 ? 0 : '0.5px solid var(--ink-hairline)' }}>
                <button
                  type="button"
                  onClick={() => setExpanded(isExpanded ? null : provider.name)}
                  style={{ width: '100%', display: 'grid', gridTemplateColumns: 'minmax(180px, 0.75fr) minmax(240px, 1fr) auto auto', gap: 14, alignItems: 'center', border: 0, background: isExpanded ? 'var(--warm)' : 'transparent', padding: '14px 16px', textAlign: 'left', cursor: 'pointer' }}
                >
                  <div style={{ minWidth: 0 }}>
                    <div className="mono" style={{ color: 'var(--ink)', fontSize: 14, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{provider.display_name}</div>
                    <div style={{ marginTop: 3, color: 'var(--ink-faint)', fontSize: 11 }}>{provider.category}</div>
                  </div>
                  <div style={{ color: 'var(--ink-mid)', fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{provider.description}</div>
                  <ProviderBadge tone={status.tone}>{status.label}</ProviderBadge>
                  <ProviderBadge tone={models.length > 0 ? 'ok' : 'neutral'}>{models.length} models</ProviderBadge>
                </button>

                {isExpanded && (
                  <div style={{ display: 'grid', gap: 14, borderTop: '0.5px solid var(--ink-hairline)', background: 'var(--warm)', padding: 16 }}>
                    <div style={{ display: 'grid', gridTemplateColumns: 'minmax(220px, 1fr) auto', gap: 14, alignItems: 'end' }}>
                      {provider.credential_name ? (
                        <label style={{ display: 'grid', gap: 7 }}>
                          <span className="eyebrow" style={{ fontSize: 9 }}>{provider.credential_label || provider.credential_name}</span>
                          <Input
                            type="password"
                            value={keyInputs[provider.name] || ''}
                            onChange={(event) => setKeyInputs((current) => ({ ...current, [provider.name]: event.target.value }))}
                            placeholder={provider.credential_configured ? 'Stored credential present' : 'Paste API key'}
                            className="border-border bg-card text-sm"
                          />
                          {provider.api_key_url && (
                            <a href={provider.api_key_url} target="_blank" rel="noopener noreferrer" style={{ display: 'inline-flex', alignItems: 'center', gap: 4, color: 'var(--teal-dark)', fontSize: 12, textDecoration: 'none' }}>
                              Get an API key <ExternalLink size={12} />
                            </a>
                          )}
                        </label>
                      ) : (
                        <div style={{ color: 'var(--ink-mid)', fontSize: 13 }}>No credential required for this provider.</div>
                      )}
                      <Button size="sm" onClick={() => handleSaveProvider(provider)} disabled={busy}>
                        {busy ? <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" /> : <KeyRound className="mr-1 h-3.5 w-3.5" />}
                        {provider.installed ? 'Save & Test' : 'Install Provider'}
                      </Button>
                    </div>

                    <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 8, overflow: 'hidden' }}>
                      <div style={{ padding: '9px 12px', borderBottom: '0.5px solid var(--ink-hairline)', background: 'var(--warm-2)' }}>
                        <span className="eyebrow" style={{ fontSize: 9 }}>Configured models</span>
                      </div>
                      {models.length === 0 ? (
                        <div style={{ padding: 12, color: 'var(--ink-mid)', fontSize: 12 }}>No models from this provider are installed in routing config.</div>
                      ) : models.map((model) => (
                        <div key={model.name} style={{ display: 'grid', gridTemplateColumns: 'minmax(170px, 0.7fr) minmax(220px, 1fr) 120px 120px', gap: 12, padding: '10px 12px', borderTop: '0.5px solid var(--ink-hairline)', alignItems: 'center' }}>
                          <code style={{ color: 'var(--ink)' }}>{model.name}</code>
                          <span style={{ color: 'var(--ink-mid)', fontSize: 12 }}>{model.provider_model}</span>
                          <span className="mono" style={{ color: 'var(--ink-faint)', fontSize: 11 }}>${(model.cost_per_mtok_in ?? 0).toFixed(2)} / MTok in</span>
                          <span className="mono" style={{ color: 'var(--ink-faint)', fontSize: 11 }}>${(model.cost_per_mtok_out ?? 0).toFixed(2)} / MTok out</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </section>
      )}

      <section style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm-2)', overflow: 'hidden' }}>
        <div style={{ padding: '12px 16px', borderBottom: '0.5px solid var(--ink-hairline)' }}>
          <span className="eyebrow" style={{ fontSize: 9 }}>Routing tiers</span>
        </div>
        {tierRows.length === 0 ? (
          <div style={{ padding: 16, color: 'var(--ink-mid)', fontSize: 13 }}>
            No explicit tier map is configured. Known default model names will be inferred at startup when possible.
          </div>
        ) : tierRows.map(([tier, entries]) => (
          <div key={tier} style={{ display: 'grid', gridTemplateColumns: '140px minmax(0, 1fr)', gap: 12, padding: '11px 16px', borderTop: '0.5px solid var(--ink-hairline)', alignItems: 'center' }}>
            <span className="mono" style={{ color: 'var(--ink)', fontSize: 12 }}>{tier}</span>
            <span style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {(entries || []).map((entry) => <ProviderBadge key={`${tier}:${entry.model}`}>{entry.model}</ProviderBadge>)}
            </span>
          </div>
        ))}
      </section>
    </div>
  );
}
