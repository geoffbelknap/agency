import { useState, useEffect } from 'react';
import { Check, ExternalLink, Loader2, X, ChevronDown } from 'lucide-react';
import { api } from '../../lib/api';
import { Input } from '../../components/ui/input';
import { Button } from '../../components/ui/button';

type TierStrategy = 'strict' | 'best_effort' | 'catch_all';

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
  tierStrategy: TierStrategy;
  onProviderUpdate: (name: string, configured: boolean, validated: boolean) => void;
  onTierStrategyUpdate: (strategy: TierStrategy) => void;
  onNext: () => void;
  onBack: () => void;
}

export function ProvidersStep({
  providers: configuredProviders,
  tierStrategy,
  onProviderUpdate,
  onTierStrategyUpdate,
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
    const key = keyInputs[provider.name] || '';

    if (credName && !key && !provider.credential_configured) return;

    setTesting(provider.name);
    setTestError((prev) => ({ ...prev, [provider.name]: '' }));

    try {
      if (credName && key) {
        await api.credentials.store(credName, key, { kind: 'provider', scope: 'global', protocol: 'api-key' });
      }

      if (credName) {
        const result = await api.credentials.test(credName);
        if (!result.ok) {
          setTestError((prev) => ({ ...prev, [provider.name]: result.message || 'Verification failed' }));
          onProviderUpdate(provider.name, true, false);
          return;
        }
      }

      if (!provider.installed) {
        try {
          await api.hub.install(provider.name, 'provider');
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
    cloud: 'Cloud Providers',
    local: 'Local',
    compatible: 'OpenAI-Compatible',
  };

  if (loading) {
    return (
      <div className="text-center space-y-4">
        <h2 className="text-2xl font-semibold text-foreground">LLM Providers</h2>
        <Loader2 className="w-5 h-5 animate-spin mx-auto text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <div className="text-center space-y-2">
        <h2 className="text-2xl font-semibold text-foreground">LLM Providers</h2>
        <p className="text-muted-foreground text-sm">Connect at least one provider to power your agents.</p>
      </div>

      <div className="space-y-6">
        {(['cloud', 'local', 'compatible'] as const).map((cat) => {
          const items = grouped[cat];
          if (items.length === 0) return null;
          return (
            <div key={cat} className="space-y-2">
              <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">{categoryLabels[cat]}</h3>
              <div className="space-y-2">
                {items.map((provider) => {
                  const status = configuredProviders[provider.name];
                  const isExpanded = expanded === provider.name;
                  const isValidated = status?.validated || provider.credential_configured;

                  return (
                    <div key={provider.name} className="border border-border rounded-lg bg-card overflow-hidden">
                      <button
                        className="w-full flex items-center justify-between px-4 py-3 text-left hover:bg-secondary/30 transition-colors"
                        onClick={() => setExpanded(isExpanded ? null : provider.name)}
                      >
                        <div className="flex items-center gap-3">
                          <span className="text-sm font-medium text-foreground">{provider.display_name}</span>
                          {isValidated && <Check className="w-4 h-4 text-emerald-500" />}
                        </div>
                        <ChevronDown className={`w-4 h-4 text-muted-foreground transition-transform ${isExpanded ? 'rotate-180' : ''}`} />
                      </button>

                      {isExpanded && (
                        <div className="px-4 pb-4 space-y-3 border-t border-border pt-3">
                          <p className="text-xs text-muted-foreground">{provider.description}</p>

                          {provider.credential_name && (
                            <div className="space-y-1.5">
                              <label className="text-xs text-muted-foreground">{provider.credential_label || 'API Key'}</label>
                              <Input
                                type="password"
                                value={keyInputs[provider.name] || ''}
                                onChange={(e) => setKeyInputs((prev) => ({ ...prev, [provider.name]: e.target.value }))}
                                placeholder={isValidated ? '••••••••' : 'Enter your API key'}
                                className="text-sm bg-background"
                              />
                              {provider.api_key_url && (
                                <a href={provider.api_key_url} target="_blank" rel="noopener noreferrer"
                                  className="text-xs text-blue-400 hover:text-blue-300 inline-flex items-center gap-1">
                                  Get an API key <ExternalLink className="w-3 h-3" />
                                </a>
                              )}
                            </div>
                          )}

                          {provider.api_base_configurable && (
                            <div className="space-y-1.5">
                              <label className="text-xs text-muted-foreground">API Base URL</label>
                              <Input
                                value={baseInputs[provider.name] || ''}
                                onChange={(e) => setBaseInputs((prev) => ({ ...prev, [provider.name]: e.target.value }))}
                                placeholder="http://localhost:11434/v1"
                                className="text-sm bg-background"
                              />
                            </div>
                          )}

                          {testError[provider.name] && (
                            <p className="text-xs text-red-400 flex items-center gap-1">
                              <X className="w-3 h-3" /> {testError[provider.name]}
                            </p>
                          )}

                          <Button size="sm" onClick={() => handleVerify(provider)} disabled={testing === provider.name} className="w-full">
                            {testing === provider.name ? (
                              <><Loader2 className="w-3 h-3 mr-1.5 animate-spin" /> Verifying...</>
                            ) : 'Verify & Save'}
                          </Button>
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

      {hasValidProvider && (
        <div className="space-y-3 pt-4 border-t border-border">
          <h3 className="text-sm font-medium text-foreground">Model Routing Strategy</h3>
          <p className="text-xs text-muted-foreground">How should the platform handle model tier requests?</p>
          <div className="space-y-2">
            {([
              { value: 'best_effort' as const, label: 'Best Effort', desc: 'Use the nearest available model when the requested tier is unmapped.' },
              { value: 'strict' as const, label: 'Strict', desc: 'Only use exact tier matches. Fail if no model is mapped to the requested tier.' },
              { value: 'catch_all' as const, label: 'Catch-all', desc: 'Route all tiers to whatever model is available. Best for single-model setups.' },
            ]).map((opt) => (
              <button
                key={opt.value}
                className={`w-full text-left px-3 py-2.5 rounded border transition-colors ${
                  tierStrategy === opt.value ? 'border-foreground/30 bg-secondary/50' : 'border-border hover:border-border/80'
                }`}
                onClick={() => onTierStrategyUpdate(opt.value)}
              >
                <div className="text-sm font-medium text-foreground">{opt.label}</div>
                <div className="text-xs text-muted-foreground">{opt.desc}</div>
              </button>
            ))}
          </div>
        </div>
      )}

      <div className="flex items-center justify-between pt-4">
        <button onClick={onBack} className="text-sm text-muted-foreground hover:text-foreground transition-colors">Back</button>
        <Button onClick={onNext} disabled={!hasValidProvider}>Continue</Button>
      </div>
    </div>
  );
}
