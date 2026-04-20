import { useEffect, useMemo, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { api } from '../../lib/api';
import { Button } from '../../components/ui/button';

interface CapabilityItem {
  name: string;
  state: string;
}

interface CapabilityTier {
  capabilities: string[];
}

interface SetupConfig {
  capability_tiers: Record<string, CapabilityTier>;
}

interface CapabilitiesStepProps {
  onUpdate: (capabilities: string[]) => void;
  onNext: () => void;
  onBack: () => void;
}

const PROVIDER_DEFAULTS = ['provider-web-fetch', 'provider-web-search'];

export function CapabilitiesStep({ onUpdate, onNext, onBack }: CapabilitiesStepProps) {
  const [available, setAvailable] = useState<CapabilityItem[]>([]);
  const [setupConfig, setSetupConfig] = useState<SetupConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    Promise.all([
      api.capabilities.list(),
      api.setup.config(),
    ]).then(([caps, config]) => {
      setAvailable((caps || []).map((c: any) => ({
        name: c.name,
        state: c.state || 'disabled',
      })));
      setSetupConfig(config);
      setLoading(false);
    }).catch((err: any) => {
      setError(err.message || 'Failed to load capabilities');
      setLoading(false);
    });
  }, []);

  const defaultCapabilities = useMemo(() => {
    const availableNames = new Set(available.map((cap) => cap.name));
    const standard = setupConfig?.capability_tiers?.standard?.capabilities || [];
    return Array.from(new Set([...standard, ...PROVIDER_DEFAULTS])).filter((name) => availableNames.has(name));
  }, [available, setupConfig]);

  const missingDefaults = PROVIDER_DEFAULTS.filter((name) => !available.some((cap) => cap.name === name));

  const handleApply = async () => {
    setApplying(true);
    setError('');
    try {
      for (const name of defaultCapabilities) {
        const cap = available.find((item) => item.name === name);
        const isEnabled = cap?.state === 'enabled' || cap?.state === 'available' || cap?.state === 'restricted';
        if (!isEnabled) {
          await api.capabilities.enable(name);
        }
      }
      onUpdate(defaultCapabilities);
      onNext();
    } catch (e: any) {
      setError(e.message || 'Failed to apply default capabilities');
    } finally {
      setApplying(false);
    }
  };

  if (loading) {
    return (
      <div style={{ display: 'grid', gap: 14, justifyItems: 'center', padding: '26px 0' }}>
        <div className="eyebrow">Capabilities</div>
        <Loader2 className="h-5 w-5 animate-spin" style={{ color: 'var(--ink-faint)' }} />
      </div>
    );
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 22 }}>
      <div>
        <div className="eyebrow" style={{ marginBottom: 8 }}>Default capability set</div>
        <p style={{ margin: 0, maxWidth: 650, color: 'var(--ink-mid)', fontSize: 13, lineHeight: 1.55 }}>
          Setup applies the standard capability tier automatically. New agents get provider web fetch and provider web search when those tools are available from the configured model provider.
        </p>
      </div>

      <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 10, overflow: 'hidden', background: 'var(--warm-2)' }}>
        <div style={{ padding: '12px 16px', borderBottom: '0.5px solid var(--ink-hairline)', background: 'var(--warm)' }}>
          <span className="mono" style={{ fontSize: 10, color: 'var(--teal-dark)', letterSpacing: '0.16em', textTransform: 'uppercase' }}>
            Standard
          </span>
        </div>
        {defaultCapabilities.length === 0 ? (
          <div style={{ padding: 16, color: 'var(--ink-mid)', fontSize: 13 }}>No standard capabilities are available from the current registry.</div>
        ) : defaultCapabilities.map((name) => (
          <div key={name} style={{ display: 'grid', gridTemplateColumns: 'minmax(180px, 1fr) auto', gap: 16, alignItems: 'center', padding: '14px 16px', borderBottom: '0.5px solid var(--ink-hairline)' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
              <span style={{ width: 8, height: 8, borderRadius: 999, background: 'var(--teal)' }} />
              <span className="mono" style={{ color: 'var(--ink)', fontSize: 14 }}>{name}</span>
            </div>
            <span className="mono" style={{ border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 999, padding: '4px 8px', color: 'var(--teal-dark)', background: 'var(--teal-tint)', fontSize: 10, letterSpacing: '0.1em', textTransform: 'uppercase' }}>
              default
            </span>
          </div>
        ))}
      </div>

      {missingDefaults.length > 0 && (
        <p style={{ margin: 0, color: 'var(--ink-faint)', fontSize: 12 }}>
          Not available in this registry: {missingDefaults.join(', ')}.
        </p>
      )}

      {error && <p style={{ margin: 0, color: 'var(--red)', fontSize: 13 }}>{error}</p>}

      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', paddingTop: 4 }}>
        <button type="button" onClick={onBack} style={{ border: 0, background: 'transparent', color: 'var(--ink-mid)', fontSize: 13, cursor: 'pointer' }}>Back</button>
        <Button onClick={handleApply} disabled={applying}>
          {applying ? <><Loader2 className="w-3 h-3 mr-1.5 animate-spin" /> Applying...</> : 'Continue'}
        </Button>
      </div>
    </div>
  );
}
