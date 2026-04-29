import { useEffect, useState } from 'react';
import { Shuffle, Loader2 } from 'lucide-react';
import { api } from '../../lib/api';
import { Input } from '../../components/ui/input';
import { Button } from '../../components/ui/button';
import { randomAgentName } from '../../data/agent-names';

interface AgentStepProps {
  agentName: string;
  onUpdate: (name: string, preset: string) => void;
  onNext: () => void;
  onBack: () => void;
}

const NAME_PATTERN = /^[a-z0-9][a-z0-9-]*[a-z0-9]$/;
const RESERVED = new Set(['infra-egress', 'agency', 'enforcer', 'gateway', 'workspace']);
const DEFAULT_PRESET = 'platform-expert';
const PROVIDER_DEFAULTS = ['provider-web-fetch', 'provider-web-search'];

export function AgentStep({
  agentName,
  onUpdate,
  onNext,
  onBack,
}: AgentStepProps) {
  const [name, setName] = useState(agentName);
  const [creating, setCreating] = useState(false);
  const [checkingExisting, setCheckingExisting] = useState(false);
  const [existingAgents, setExistingAgents] = useState<Record<string, string>>({});
  const [phase, setPhase] = useState<'idle' | 'creating' | 'capabilities' | 'starting'>('idle');
  const [error, setError] = useState('');

  const sanitize = (input: string) => {
    return input.toLowerCase().replace(/[^a-z0-9-]/g, '').replace(/--+/g, '-');
  };

  const isValid = name.length >= 2 && name.length <= 64 && NAME_PATTERN.test(name) && !RESERVED.has(name);
  const existingStatus = existingAgents[name] || null;

  useEffect(() => {
    let cancelled = false;
    setCheckingExisting(true);
    api.agents.list()
      .then((agents) => {
        if (cancelled) return;
        setExistingAgents(Object.fromEntries((agents || []).map((agent: any) => [agent.name, agent.status || 'existing'])));
      })
      .catch(() => {
        if (!cancelled) setExistingAgents({});
      })
      .finally(() => {
        if (!cancelled) setCheckingExisting(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const grantDefaultCapabilities = async (agent: string) => {
    try {
      const [caps, config] = await Promise.all([
        api.capabilities.list(),
        api.setup.config(),
      ]);
      const available = new Set((caps || []).map((cap: any) => cap.name).filter(Boolean));
      const standard = config?.capability_tiers?.standard?.capabilities || [];
      const defaults = Array.from(new Set([...standard, ...PROVIDER_DEFAULTS])).filter((capability) => available.has(capability));
      for (const capability of defaults) {
        await api.agents.grant(agent, capability).catch(() => api.capabilities.enable(capability, undefined, [agent]));
      }
    } catch {
      // Capability grants should not block first-run setup. Admin capabilities
      // remains the source of truth for inspecting or adjusting the result.
    }
  };

  const handleCreate = async () => {
    if (!isValid) return;
    setCreating(true);
    setPhase('creating');
    setError('');
    try {
      if (!existingStatus) {
        await api.agents.create(name, DEFAULT_PRESET, 'assisted');
      }
      setPhase('capabilities');
      await grantDefaultCapabilities(name);
      setPhase('starting');
      onUpdate(name, DEFAULT_PRESET);
      onNext();
    } catch (e: any) {
      if ((e.message || '').toLowerCase().includes('already exists')) {
        setPhase('capabilities');
        await grantDefaultCapabilities(name);
        setPhase('starting');
        onUpdate(name, DEFAULT_PRESET);
        onNext();
      } else if ((e.message || '').toLowerCase().includes('backend')) {
        setError('The selected runtime backend is not ready. Run agency admin doctor, fix the reported host checks, and try again.');
        setCreating(false);
      } else {
        setError(e.message || 'Failed to create agent');
        setCreating(false);
      }
    }
  };

  const handleShuffle = () => {
    let newName = randomAgentName();
    while (newName === name) {
      newName = randomAgentName();
    }
    setName(newName);
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 28 }}>
      <div style={{ display: 'grid', gap: 8 }}>
        <div className="eyebrow">Agent identity</div>
        <p style={{ margin: 0, color: 'var(--ink-mid)', fontSize: 13, lineHeight: 1.55, maxWidth: 640 }}>
          Name your agent. If that agent already exists, setup will reuse it and continue.
        </p>
      </div>

      <div className="space-y-5 max-w-md">
        <div className="space-y-2">
          <label className="eyebrow" style={{ fontSize: 9 }} htmlFor="setup-agent-name">Agent name</label>
          <div className="flex gap-2">
            <Input
              id="setup-agent-name"
              name="setup-agent-name"
              value={name}
              onChange={(e) => setName(sanitize(e.target.value))}
              placeholder="henry"
              className="flex-1 bg-card border-border"
              maxLength={64}
            />
            <Button variant="outline" size="icon" onClick={handleShuffle} title="Random name">
              <Shuffle className="w-4 h-4" />
            </Button>
          </div>
          {checkingExisting && (
            <p className="mono" style={{ margin: '8px 0 0', color: 'var(--ink-faint)', fontSize: 11 }}>checking for existing agent...</p>
          )}
          {!checkingExisting && existingStatus && (
            <p className="mono" style={{ margin: '8px 0 0', color: 'var(--teal-dark)', fontSize: 11 }}>
              @{name} already exists · {existingStatus}. Setup will reuse it.
            </p>
          )}
        </div>

        {error && (
          <p className="text-xs text-red-400 bg-red-950/30 border border-red-900/50 rounded px-3 py-2">{error}</p>
        )}
      </div>

      <div className="flex items-center justify-between pt-4">
        <button onClick={onBack} className="text-sm text-muted-foreground hover:text-foreground transition-colors">Back</button>
        <Button onClick={handleCreate} disabled={!isValid || creating}>
          {creating ? (
            <><Loader2 className="w-3 h-3 mr-1.5 animate-spin" /> {phase === 'capabilities' ? 'Applying defaults...' : phase === 'starting' ? 'Starting agent...' : 'Creating agent...'}</>
          ) : existingStatus ? `Continue with ${name}` : 'Create agent'}
        </Button>
      </div>
    </div>
  );
}
