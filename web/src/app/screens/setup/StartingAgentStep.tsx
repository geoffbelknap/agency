import { useEffect, useState } from 'react';
import { api, type RawAgentStartProgress } from '../../lib/api';
import { AgentStartupProgress, applyStartupProgress, type StartupLine } from '../../components/AgentStartupProgress';

interface StartingAgentStepProps {
  agentName: string;
  onReady: () => void;
  onBack: () => void;
  handoffDelayMs?: number;
}

function wait(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
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
      setLines((current) => applyStartupProgress(current, event));
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
      <AgentStartupProgress
        agentName={agentName}
        lines={lines}
        error={error}
        complete={mode === 'connecting'}
        onRetry={error ? retry : undefined}
        title={mode === 'connecting' ? 'Opening chat' : 'Starting agent'}
        description={mode === 'connecting'
          ? <>Agent <span className="mono" style={{ color: 'var(--ink)' }}>@{agentName}</span> is running. Agency is connecting you to the direct message chat.</>
          : undefined}
      />

      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <button type="button" onClick={onBack} style={{ border: 0, background: 'transparent', color: 'var(--ink-mid)', fontSize: 13, cursor: 'pointer' }}>Back</button>
      </div>
    </div>
  );
}
