import { useState, useEffect } from 'react';
import { Shuffle, Loader2 } from 'lucide-react';
import { api } from '../../lib/api';
import { Input } from '../../components/ui/input';
import { Button } from '../../components/ui/button';
import { randomAgentName } from '../../data/agent-names';

interface AgentStepProps {
  agentName: string;
  agentPreset: string;
  platformExpert: boolean;
  onUpdate: (name: string, preset: string) => void;
  onPlatformExpertToggle: (enabled: boolean) => void;
  onNext: () => void;
  onBack: () => void;
}

const NAME_PATTERN = /^[a-z0-9][a-z0-9-]*[a-z0-9]$/;
const RESERVED = new Set(['infra-egress', 'agency', 'enforcer', 'gateway', 'workspace']);

interface Preset {
  name: string;
  description?: string;
  type?: string;
  source?: string;
}

export function AgentStep({
  agentName,
  agentPreset,
  platformExpert,
  onUpdate,
  onPlatformExpertToggle,
  onNext,
  onBack,
}: AgentStepProps) {
  const [name, setName] = useState(agentName);
  const [preset, setPreset] = useState(agentPreset);
  const [expert, setExpert] = useState(platformExpert);
  const [presets, setPresets] = useState<Preset[]>([]);
  const [creating, setCreating] = useState(false);
  const [phase, setPhase] = useState<'idle' | 'creating' | 'starting'>('idle');
  const [error, setError] = useState('');

  useEffect(() => {
    api.presets.list().then((data: Preset[]) => {
      setPresets(data || []);
    }).catch(() => {});
  }, []);

  const sanitize = (input: string) => {
    return input.toLowerCase().replace(/[^a-z0-9-]/g, '').replace(/--+/g, '-');
  };

  const isValid = name.length >= 2 && name.length <= 64 && NAME_PATTERN.test(name) && !RESERVED.has(name);

  const handleCreate = async () => {
    if (!isValid) return;
    setCreating(true);
    setPhase('creating');
    setError('');
    const selectedPreset = expert ? 'platform-expert' : preset;
    try {
      await api.agents.create(name, selectedPreset, 'assisted');
      setPhase('starting');
      // Start can take longer than the Web proxy timeout on first-run image
      // builds. Kick it off and let ChatStep wait for runtime readiness.
      api.agents.start(name).catch(() => {});
      onUpdate(name, selectedPreset);
      onNext();
    } catch (e: any) {
      if ((e.message || '').toLowerCase().includes('already exists')) {
        setPhase('starting');
        // Repeat setup should be safe: reuse the existing agent and let ChatStep
        // wait for runtime readiness instead of forcing a new name.
        api.agents.start(name).catch(() => {});
        onUpdate(name, selectedPreset);
        onNext();
      } else if (e.message?.includes('Docker') || e.message?.includes('docker')) {
        setError('Docker is required to run agents. Please start Docker and try again.');
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
    <div className="space-y-8">
      <div className="text-center space-y-2">
        <h2 className="text-2xl font-semibold text-foreground">Your First Agent</h2>
        <p className="text-muted-foreground text-sm">
          Give your agent a name. If it already exists, setup will reuse it and continue.
        </p>
      </div>

      <div className="space-y-5 max-w-sm mx-auto">
        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground">Agent name</label>
          <div className="flex gap-2">
            <Input
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
        </div>

        <div
          className={`flex items-start gap-3 px-3 py-3 rounded border cursor-pointer transition-colors ${
            expert ? 'border-foreground/30 bg-secondary/50' : 'border-border hover:border-border/80'
          }`}
          onClick={() => { setExpert(!expert); onPlatformExpertToggle(!expert); }}
        >
          <div className={`mt-0.5 w-4 h-4 rounded border flex items-center justify-center flex-shrink-0 ${
            expert ? 'bg-foreground border-foreground' : 'border-muted-foreground/50'
          }`}>
            {expert && <span className="text-background text-xs font-bold">✓</span>}
          </div>
          <div>
            <div className="text-sm font-medium text-foreground">Platform Expert</div>
            <div className="text-xs text-muted-foreground">
              Your agent will know how Agency works and can help you learn the platform. Recommended for first-time setup.
            </div>
          </div>
        </div>

        {!expert && presets.length > 0 && (
          <div className="space-y-1.5">
            <label className="text-xs text-muted-foreground">Preset</label>
            <select
              value={preset}
              onChange={(e) => setPreset(e.target.value)}
              className="w-full h-9 rounded border border-border bg-card px-3 text-sm text-foreground"
            >
              {presets.map((p) => (
                <option key={p.name} value={p.name}>{p.name}{p.description ? ` — ${p.description}` : ''}</option>
              ))}
            </select>
          </div>
        )}

        {error && (
          <p className="text-xs text-red-400 bg-red-950/30 border border-red-900/50 rounded px-3 py-2">{error}</p>
        )}
      </div>

      <div className="flex items-center justify-between pt-4">
        <button onClick={onBack} className="text-sm text-muted-foreground hover:text-foreground transition-colors">Back</button>
        <div className="flex items-center gap-3">
          <button
            onClick={() => { onUpdate(name, preset); onNext(); }}
            className="text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            Skip
          </button>
          <Button onClick={handleCreate} disabled={!isValid || creating}>
            {creating ? <><Loader2 className="w-3 h-3 mr-1.5 animate-spin" /> {phase === 'starting' ? 'Starting agent...' : 'Creating agent...'}</> : 'Create or Start'}
          </Button>
        </div>
      </div>
    </div>
  );
}
