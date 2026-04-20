import { useEffect, useState } from 'react';
import { Loader2, RefreshCw } from 'lucide-react';
import { api, type RawAgentStartProgress } from '../../lib/api';
import { Button } from '../../components/ui/button';

interface StartingAgentStepProps {
  agentName: string;
  onReady: () => void;
  onBack: () => void;
  handoffDelayMs?: number;
}

type StartupLine = {
  id: string;
  label: string;
  detail?: string;
  state: 'active' | 'done' | 'error';
};

function wait(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function startupLabel(event: RawAgentStartProgress) {
  const raw = event.description || event.name || event.type;
  return raw.replace(/_/g, ' ');
}

export function StartingAgentStep({
  agentName,
  onReady,
  onBack,
  handoffDelayMs = 500,
}: StartingAgentStepProps) {
  const [lines, setLines] = useState<StartupLine[]>([]);
  const [mode, setMode] = useState<'starting' | 'connecting'>('starting');
  const [error, setError] = useState('');
  const [retryKey, setRetryKey] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setError('');
    setMode('starting');
    setLines([{ id: 'request-start', label: 'Requesting agent startup', state: 'active' }]);

    const appendProgress = (event: RawAgentStartProgress) => {
      if (cancelled) return;
      if (event.type === 'complete') {
        setLines((current) => [
          ...current.map((line) => line.state === 'active' ? { ...line, state: 'done' as const } : line),
          { id: 'startup-complete', label: 'Runtime startup complete', detail: event.model, state: 'done' },
        ]);
        return;
      }
      if (event.type === 'error') {
        setLines((current) => [
          ...current.map((line) => line.state === 'active' ? { ...line, state: 'error' as const } : line),
          { id: `error-${Date.now()}`, label: event.error || 'Startup failed', state: 'error' },
        ]);
        return;
      }

      const phase = event.phase ?? Date.now();
      setLines((current) => [
        ...current.map((line) => line.state === 'active' ? { ...line, state: 'done' as const } : line),
        {
          id: `phase-${phase}-${event.name || event.description || event.type}`,
          label: startupLabel(event),
          state: 'active',
        },
      ]);
    };

    const isAgentRunning = async () => {
      const agent = await api.agents.show(agentName).catch(() => null);
      return agent?.status === 'running';
    };

    const run = async () => {
      try {
        const alreadyRunning = await isAgentRunning();
        if (cancelled) return;
        if (alreadyRunning) {
          setLines([{ id: 'already-running', label: 'Agent is already running', state: 'done' }]);
        } else {
          await api.agents.startStream(agentName, appendProgress);
          if (cancelled) return;
        }
        setLines((current) => current.map((line) => line.state === 'active' ? { ...line, state: 'done' as const } : line));
        setMode('connecting');
        setLines((current) => [
          ...current,
          { id: 'connect-chat', label: 'Connecting you to chat', state: 'active' },
        ]);
        await wait(handoffDelayMs);
        if (!cancelled) onReady();
      } catch (e: any) {
        if (!cancelled) setError(e.message || 'Agent startup failed');
      }
    };

    run();
    return () => {
      cancelled = true;
    };
  }, [agentName, handoffDelayMs, onReady, retryKey]);

  const retry = () => {
    setError('');
    setRetryKey((key) => key + 1);
  };

  return (
    <div style={{ display: 'grid', gap: 24 }}>
      <div style={{ display: 'grid', gap: 10 }}>
        <div className="eyebrow">{mode === 'connecting' ? 'Opening chat' : 'Starting agent'}</div>
        <p style={{ margin: 0, color: 'var(--ink-mid)', fontSize: 13, lineHeight: 1.55, maxWidth: 650 }}>
          {mode === 'connecting' ? (
            <>Agent <span className="mono" style={{ color: 'var(--ink)' }}>@{agentName}</span> is running. Agency is connecting you to the direct message chat.</>
          ) : (
            <>Agency is starting <span className="mono" style={{ color: 'var(--ink)' }}>@{agentName}</span>. Startup progress below is streamed from the gateway.</>
          )}
        </p>
      </div>

      <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, overflow: 'hidden', background: 'var(--warm-2)' }}>
        <div style={{ display: 'grid', gridTemplateColumns: '110px minmax(0, 1fr)', minHeight: 190 }}>
          <div style={{ display: 'grid', placeItems: 'center', borderRight: '0.5px solid var(--ink-hairline)', background: 'var(--warm)' }}>
            <div style={{ position: 'relative', width: 58, height: 58, display: 'grid', placeItems: 'center' }}>
              <span style={{ position: 'absolute', inset: 0, border: '0.5px solid var(--teal-border)', borderRadius: '50%', opacity: 0.45 }} />
              <span style={{ position: 'absolute', inset: 8, border: '0.5px solid var(--ink-hairline-strong)', borderRadius: '50%' }} />
              <Loader2 className="h-6 w-6 animate-spin" style={{ color: error ? 'var(--red)' : 'var(--teal)' }} />
            </div>
          </div>
          <div style={{ padding: 22, display: 'grid', gap: 12, alignContent: 'start' }}>
            <div className="eyebrow" style={{ fontSize: 9 }}>Startup stream</div>
            <div style={{ display: 'grid', gap: 10, maxHeight: 210, overflowY: 'auto', paddingRight: 6 }}>
              {lines.slice(-8).map((line) => (
                <div key={line.id} style={{ display: 'grid', gridTemplateColumns: '18px minmax(0, 1fr)', gap: 10, alignItems: 'start' }}>
                  <span style={{ marginTop: 5, width: 8, height: 8, borderRadius: 999, background: line.state === 'error' ? 'var(--red)' : line.state === 'done' ? 'var(--teal)' : 'var(--ink)', opacity: line.state === 'done' ? 0.7 : 1 }} />
                  <span className="mono" style={{ fontSize: 12, color: line.state === 'error' ? 'var(--red)' : line.state === 'active' ? 'var(--ink)' : 'var(--ink-mid)', lineHeight: 1.4 }}>
                    {line.label}
                    {line.detail && <span style={{ color: 'var(--ink-faint)' }}> · {line.detail}</span>}
                  </span>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>

      {error && <p style={{ margin: 0, color: 'var(--red)', fontSize: 13 }}>{error}</p>}

      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <button type="button" onClick={onBack} style={{ border: 0, background: 'transparent', color: 'var(--ink-mid)', fontSize: 13, cursor: 'pointer' }}>Back</button>
        {error && (
          <Button variant="outline" onClick={retry}>
            <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
            Try again
          </Button>
        )}
      </div>
    </div>
  );
}
