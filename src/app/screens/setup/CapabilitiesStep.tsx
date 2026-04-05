import { useState, useEffect } from 'react';
import { Check, Loader2, Key, ChevronDown } from 'lucide-react';
import { api } from '../../lib/api';
import { Input } from '../../components/ui/input';
import { Button } from '../../components/ui/button';

interface CapabilityItem {
  name: string;
  kind: string;
  state: string;
  description: string;
}

interface CapabilityTier {
  display_name: string;
  description: string;
  capabilities: string[];
}

interface SetupConfig {
  capability_tiers: Record<string, CapabilityTier>;
}

type TierChoice = 'minimal' | 'standard' | 'custom';

interface CapabilitiesStepProps {
  capabilities: string[];
  onUpdate: (capabilities: string[]) => void;
  onNext: () => void;
  onBack: () => void;
}

export function CapabilitiesStep({ capabilities, onUpdate, onNext, onBack }: CapabilitiesStepProps) {
  const [available, setAvailable] = useState<CapabilityItem[]>([]);
  const [setupConfig, setSetupConfig] = useState<SetupConfig | null>(null);
  const [tier, setTier] = useState<TierChoice>('standard');
  const [selected, setSelected] = useState<Set<string>>(new Set(capabilities));
  const [loading, setLoading] = useState(true);
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState('');
  const [expandedCap, setExpandedCap] = useState<string | null>(null);
  const [credInputs, setCredInputs] = useState<Record<string, string>>({});
  const [credSaved, setCredSaved] = useState<Set<string>>(new Set());

  useEffect(() => {
    Promise.all([
      api.capabilities.list(),
      api.setup.config(),
    ]).then(([caps, config]) => {
      const mapped: CapabilityItem[] = (caps || []).map((c: any) => ({
        name: c.name,
        kind: c.kind || 'service',
        state: c.state || 'disabled',
        description: c.description || '',
      }));
      setAvailable(mapped);
      setSetupConfig(config);

      const standardCaps = config?.capability_tiers?.standard?.capabilities || [];
      setSelected(new Set(standardCaps));
      // Mark already-enabled capabilities as having credentials
      const alreadyEnabled = new Set(mapped.filter(c => c.state === 'enabled' || c.state === 'available').map(c => c.name));
      setCredSaved(alreadyEnabled);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  const handleTierChange = (newTier: TierChoice) => {
    setTier(newTier);
    if (newTier === 'minimal') {
      setSelected(new Set());
      setExpandedCap(null);
    } else if (newTier === 'standard') {
      const caps = setupConfig?.capability_tiers?.standard?.capabilities || [];
      setSelected(new Set(caps));
      setExpandedCap(null);
    }
  };

  const toggleCap = (cap: CapabilityItem) => {
    setTier('custom');
    const wasSelected = selected.has(cap.name);
    setSelected((prev) => {
      const next = new Set(prev);
      if (wasSelected) {
        next.delete(cap.name);
      } else {
        next.add(cap.name);
      }
      return next;
    });

    // If selecting a service capability, expand it for credential input
    if (!wasSelected && cap.kind === 'service' && !credSaved.has(cap.name)) {
      setExpandedCap(cap.name);
    } else if (wasSelected) {
      if (expandedCap === cap.name) setExpandedCap(null);
    }
  };

  const handleSaveCredential = async (capName: string) => {
    const key = credInputs[capName]?.trim();
    if (!key) return;

    try {
      await api.credentials.store(`${capName}-api-key`, key, {
        kind: 'capability',
        scope: 'global',
        protocol: 'api-key',
        service: capName,
      });
      setCredSaved((prev) => new Set(prev).add(capName));
      setExpandedCap(null);
    } catch (e: any) {
      setError(e.message || `Failed to save credential for ${capName}`);
    }
  };

  const handleApply = async () => {
    // Check if any selected service capabilities still need credentials
    const needsCred = available.filter(
      (cap) => selected.has(cap.name) && cap.kind === 'service' && !credSaved.has(cap.name)
    );
    if (needsCred.length > 0) {
      setExpandedCap(needsCred[0].name);
      setError(`${needsCred[0].name} requires an API key before continuing.`);
      return;
    }

    setApplying(true);
    setError('');
    try {
      for (const cap of available) {
        const shouldEnable = selected.has(cap.name);
        const isEnabled = cap.state === 'enabled' || cap.state === 'available';
        if (shouldEnable && !isEnabled) {
          const credKey = credInputs[cap.name]?.trim() || undefined;
          await api.capabilities.enable(cap.name, credKey);
        } else if (!shouldEnable && isEnabled) {
          await api.capabilities.disable(cap.name);
        }
      }
      onUpdate(Array.from(selected));
      onNext();
    } catch (e: any) {
      setError(e.message || 'Failed to apply capabilities');
    } finally {
      setApplying(false);
    }
  };

  const tierCards = [
    {
      key: 'minimal' as const,
      label: setupConfig?.capability_tiers?.minimal?.display_name || 'Minimal',
      desc: setupConfig?.capability_tiers?.minimal?.description || 'LLM access only. No external services.',
    },
    {
      key: 'standard' as const,
      label: setupConfig?.capability_tiers?.standard?.display_name || 'Standard',
      desc: setupConfig?.capability_tiers?.standard?.description || 'Recommended defaults. No API keys needed.',
    },
    {
      key: 'custom' as const,
      label: 'Custom',
      desc: 'Pick capabilities individually. Some require API keys.',
    },
  ];

  if (loading) {
    return (
      <div className="text-center space-y-4">
        <h2 className="text-2xl font-semibold text-foreground">Capabilities</h2>
        <Loader2 className="w-5 h-5 animate-spin mx-auto text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="text-center space-y-2">
        <h2 className="text-2xl font-semibold text-foreground">What should your agents be able to do?</h2>
        <p className="text-muted-foreground text-sm">You can always change these later in Admin.</p>
      </div>

      <div className="grid grid-cols-3 gap-2">
        {tierCards.map((tc) => (
          <button
            key={tc.key}
            className={`text-left px-3 py-3 rounded border transition-colors ${
              tier === tc.key ? 'border-foreground/30 bg-secondary/50' : 'border-border hover:border-border/80'
            }`}
            onClick={() => handleTierChange(tc.key)}
          >
            <div className="text-sm font-medium text-foreground">{tc.label}</div>
            <div className="text-xs text-muted-foreground mt-0.5">{tc.desc}</div>
          </button>
        ))}
      </div>

      {tier !== 'minimal' && available.length > 0 && (
        <div className="space-y-1 max-h-72 overflow-y-auto">
          {available.map((cap) => {
            const isSelected = selected.has(cap.name);
            const isExpanded = expandedCap === cap.name;
            const hasCred = credSaved.has(cap.name);
            const needsKey = cap.kind === 'service';

            return (
              <div key={cap.name} className="rounded border border-transparent hover:border-border/50">
                <button
                  className={`w-full flex items-center gap-3 px-3 py-2 rounded text-left transition-colors ${
                    isSelected ? 'bg-secondary/50' : 'hover:bg-secondary/20'
                  }`}
                  onClick={() => toggleCap(cap)}
                >
                  <div className={`w-4 h-4 rounded border flex items-center justify-center flex-shrink-0 ${
                    isSelected ? 'bg-emerald-600 border-emerald-600' : 'border-muted-foreground/40'
                  }`}>
                    {isSelected && <Check className="w-3 h-3 text-white" />}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="text-sm text-foreground flex items-center gap-1.5">
                      {cap.name}
                      {needsKey && !hasCred && isSelected && (
                        <Key className="w-3 h-3 text-amber-400" />
                      )}
                      {needsKey && hasCred && isSelected && (
                        <Key className="w-3 h-3 text-emerald-500" />
                      )}
                    </div>
                    {cap.description && <div className="text-xs text-muted-foreground truncate">{cap.description}</div>}
                  </div>
                  {needsKey && isSelected && (
                    <span
                      role="button"
                      onClick={(e) => { e.stopPropagation(); setExpandedCap(isExpanded ? null : cap.name); }}
                      className="p-0.5 rounded hover:bg-secondary/50"
                    >
                      <ChevronDown className={`w-3.5 h-3.5 text-muted-foreground transition-transform ${isExpanded ? 'rotate-180' : ''}`} />
                    </span>
                  )}
                </button>

                {isExpanded && needsKey && (
                  <div className="px-10 pb-3 space-y-2">
                    {hasCred ? (
                      <p className="text-xs text-emerald-500 flex items-center gap-1">
                        <Check className="w-3 h-3" /> API key configured
                      </p>
                    ) : (
                      <>
                        <Input
                          type="password"
                          value={credInputs[cap.name] || ''}
                          onChange={(e) => setCredInputs((prev) => ({ ...prev, [cap.name]: e.target.value }))}
                          placeholder={`API key for ${cap.name}`}
                          className="text-sm bg-background h-8"
                        />
                        <Button
                          size="sm"
                          variant="outline"
                          className="h-7 text-xs"
                          onClick={(e) => { e.stopPropagation(); handleSaveCredential(cap.name); }}
                          disabled={!credInputs[cap.name]?.trim()}
                        >
                          Save Key
                        </Button>
                      </>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {error && <p className="text-xs text-red-400">{error}</p>}

      <div className="flex items-center justify-between pt-4">
        <button onClick={onBack} className="text-sm text-muted-foreground hover:text-foreground transition-colors">Back</button>
        <Button onClick={handleApply} disabled={applying}>
          {applying ? <><Loader2 className="w-3 h-3 mr-1.5 animate-spin" /> Applying...</> : 'Continue'}
        </Button>
      </div>
    </div>
  );
}
