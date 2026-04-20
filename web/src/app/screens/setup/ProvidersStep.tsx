import { useState, useEffect } from 'react';
import { ExternalLink, Loader2, X, ChevronDown } from 'lucide-react';
import { api } from '../../lib/api';
import { Input } from '../../components/ui/input';
import { Button } from '../../components/ui/button';

interface ProviderData {
  name: string;
  display_name: string;
  description: string;
  category: string;
  installed: boolean;
  credential_name?: string;
  credential_label?: string;
  api_key_url?: string;
  api_base_configurable?: boolean;
  credential_configured: boolean;
}

interface ProvidersStepProps {
  providers: Record<string, { configured: boolean; validated: boolean }>;
  onProviderUpdate: (name: string, configured: boolean, validated: boolean) => void;
  onNext: () => void;
  onBack: () => void;
}

export function ProvidersStep({
  providers: configuredProviders,
  onProviderUpdate,
  onNext,
  onBack,
}: ProvidersStepProps) {
  const [available, setAvailable] = useState<ProviderData[]>([]);
  const [loading, setLoading] = useState(true);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [keyInputs, setKeyInputs] = useState<Record<string, string>>({});
  const [baseInputs, setBaseInputs] = useState<Record<string, string>>({});
  const [testing, setTesting] = useState<string | null>(null);
  const [testError, setTestError] = useState<Record<string, string>>({});

  const hasValidProvider = Object.values(configuredProviders).some((p) => p.validated);

  useEffect(() => {
    api.providers.list().then((data) => {
      setAvailable(data || []);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  const handleVerify = async (provider: ProviderData) => {
    const credName = provider.credential_name;
    const key = (keyInputs[provider.name] || '').trim();
    const baseURL = (baseInputs[provider.name] || '').trim();

    if (provider.name === 'openai-compatible') {
      setTestError((prev) => ({
        ...prev,
        [provider.name]: 'OpenAI-compatible endpoints need a custom base URL and model mapping. This setup step cannot save that mapping yet.',
      }));
      return;
    }

    if (provider.api_base_configurable && provider.category !== 'local' && !baseURL) {
      setTestError((prev) => ({ ...prev, [provider.name]: 'Enter the provider base URL before saving.' }));
      return;
    }

    if (credName && !key && !provider.credential_configured) {
      setTestError((prev) => ({ ...prev, [provider.name]: `Enter ${provider.credential_label || 'an API key'} before verifying.` }));
      return;
    }

    setTesting(provider.name);
    setTestError((prev) => ({ ...prev, [provider.name]: '' }));

    try {
      if (credName && key) {
        await api.credentials.store(credName, key, { kind: 'provider', scope: 'global', protocol: 'api-key' });
      }

      if (credName && key) {
        const result = await api.credentials.test(credName);
        if (!result.ok) {
          setTestError((prev) => ({ ...prev, [provider.name]: result.message || 'Verification failed' }));
          onProviderUpdate(provider.name, true, false);
          return;
        }
      } else if (credName && provider.credential_configured) {
        // The backend may report a provider as configured through an alias such
        // as ANTHROPIC_API_KEY. Do not re-test the catalog credential name here,
        // because that can produce a false "credential not found" error.
        onProviderUpdate(provider.name, true, true);
      }

      if (!provider.installed) {
        try {
          await api.providers.install(provider.name);
        } catch (installErr: any) {
          if (!(installErr.message || '').includes('already exists')) {
            throw installErr;
          }
        }
      }

      onProviderUpdate(provider.name, true, true);
      setExpanded(null);
    } catch (e: any) {
      setTestError((prev) => ({ ...prev, [provider.name]: e.message || 'Failed' }));
      onProviderUpdate(provider.name, true, false);
    } finally {
      setTesting(null);
    }
  };

  const grouped = {
    cloud: available.filter((p) => p.category === 'cloud'),
    local: available.filter((p) => p.category === 'local'),
    compatible: available.filter((p) => p.category === 'compatible'),
  };

  const categoryLabels: Record<string, string> = {
    cloud: 'Cloud providers',
    local: 'Local',
    compatible: 'OpenAI-compatible',
  };

  if (loading) {
    return (
      <div style={{ display: 'grid', gap: 14, justifyItems: 'center', padding: '26px 0' }}>
        <div className="eyebrow">Providers</div>
        <Loader2 className="h-5 w-5 animate-spin" style={{ color: 'var(--ink-faint)' }} />
      </div>
    );
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
      <div>
        <div className="eyebrow" style={{ marginBottom: 8 }}>Provider readiness</div>
        <p style={{ margin: 0, maxWidth: 650, color: 'var(--ink-mid)', fontSize: 13, lineHeight: 1.55 }}>
          Verify one model provider before creating the first agent. Credentials stay mediated through the gateway credential store.
        </p>
      </div>

      <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 10, overflow: 'hidden', background: 'var(--warm-2)' }}>
        {(['cloud', 'local', 'compatible'] as const).map((cat) => {
          const items = grouped[cat];
          if (items.length === 0) return null;
          return (
            <div key={cat}>
              <div style={{ padding: '12px 16px', borderBottom: '0.5px solid var(--ink-hairline)', background: 'var(--warm)' }}>
                <span className="mono" style={{ fontSize: 10, color: 'var(--teal-dark)', letterSpacing: '0.16em', textTransform: 'uppercase' }}>
                  {categoryLabels[cat]}
                </span>
              </div>
              <div>
                {items.map((provider) => {
                  const status = configuredProviders[provider.name];
                  const isExpanded = expanded === provider.name;
                  const isValidated = status?.validated || provider.credential_configured;
                  const isConfigured = status?.configured || provider.installed || provider.credential_configured;
                  const verifyLabel = provider.name === 'openai-compatible'
                    ? 'Setup info'
                    : isValidated && !keyInputs[provider.name]
                      ? 'Use Existing Credential'
                      : 'Verify & Save';

                  return (
                    <div key={provider.name} style={{ borderBottom: '0.5px solid var(--ink-hairline)', background: isExpanded ? 'var(--warm)' : 'transparent' }}>
                      <button
                        type="button"
                        style={{ width: '100%', display: 'grid', gridTemplateColumns: 'minmax(160px, 0.75fr) minmax(220px, 1fr) auto auto', gap: 16, alignItems: 'center', padding: '14px 16px', border: 0, background: 'transparent', textAlign: 'left', cursor: 'pointer' }}
                        onClick={() => setExpanded(isExpanded ? null : provider.name)}
                      >
                        <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
                          <span style={{ width: 8, height: 8, borderRadius: 999, background: isValidated ? 'var(--teal)' : isConfigured ? 'var(--amber)' : 'var(--ink-hairline-strong)', flexShrink: 0 }} />
                          <span className="mono" style={{ color: 'var(--ink)', fontSize: 14, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{provider.display_name}</span>
                        </div>
                        <span style={{ color: 'var(--ink-mid)', fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{provider.description}</span>
                        <span className="mono" style={{ border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 999, padding: '4px 8px', color: isValidated ? 'var(--teal-dark)' : 'var(--ink-faint)', background: isValidated ? 'var(--teal-tint)' : 'var(--warm)', fontSize: 10, letterSpacing: '0.1em', textTransform: 'uppercase', whiteSpace: 'nowrap' }}>
                          {isValidated ? 'verified' : isConfigured ? 'configured' : 'missing'}
                        </span>
                        <ChevronDown className={`h-4 w-4 transition-transform ${isExpanded ? 'rotate-180' : ''}`} style={{ color: 'var(--ink-faint)' }} />
                      </button>

                      {isExpanded && (
                        <div style={{ display: 'grid', gap: 14, borderTop: '0.5px solid var(--ink-hairline)', padding: '16px', background: 'var(--warm)' }}>
                          <p style={{ margin: 0, color: 'var(--ink-mid)', fontSize: 13, lineHeight: 1.55 }}>{provider.description}</p>

                          {provider.credential_name && (
                            <div style={{ display: 'grid', gap: 8 }}>
                              <label className="eyebrow" style={{ fontSize: 9 }}>{provider.credential_label || 'API Key'}</label>
                              <Input
                                id={`provider-key-${provider.name}`}
                                name={`provider-key-${provider.name}`}
                                type="password"
                                value={keyInputs[provider.name] || ''}
                                onChange={(e) => setKeyInputs((prev) => ({ ...prev, [provider.name]: e.target.value }))}
                                placeholder={isValidated ? '••••••••' : 'Enter your API key'}
                                className="border-border bg-card text-sm"
                              />
                              {provider.api_key_url && (
                                <a href={provider.api_key_url} target="_blank" rel="noopener noreferrer"
                                  className="inline-flex items-center gap-1 text-xs hover:opacity-80" style={{ color: 'var(--teal-dark)' }}>
                                  Get an API key <ExternalLink className="w-3 h-3" />
                                </a>
                              )}
                            </div>
                          )}

                          {provider.api_base_configurable && (
                            <div style={{ display: 'grid', gap: 8 }}>
                              <label className="eyebrow" style={{ fontSize: 9 }}>API Base URL</label>
                              <Input
                                id={`provider-base-${provider.name}`}
                                name={`provider-base-${provider.name}`}
                                value={baseInputs[provider.name] || ''}
                                onChange={(e) => setBaseInputs((prev) => ({ ...prev, [provider.name]: e.target.value }))}
                                placeholder="http://localhost:11434/v1"
                                className="border-border bg-card text-sm"
                              />
                            </div>
                          )}

                          {testError[provider.name] && (
                            <p className="flex items-center gap-1 text-sm" style={{ color: 'var(--red)' }}>
                              <X className="w-3 h-3" /> {testError[provider.name]}
                            </p>
                          )}

                          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                            <Button size="sm" onClick={() => handleVerify(provider)} disabled={testing === provider.name}>
                              {testing === provider.name ? (
                                <><Loader2 className="w-3 h-3 mr-1.5 animate-spin" /> Verifying...</>
                              ) : verifyLabel}
                            </Button>
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', paddingTop: 4 }}>
        <button type="button" onClick={onBack} style={{ border: 0, background: 'transparent', color: 'var(--ink-mid)', fontSize: 13, cursor: 'pointer' }}>Back</button>
        <Button onClick={onNext} disabled={!hasValidProvider}>Continue</Button>
      </div>
    </div>
  );
}
