import { api, type RawAgentRuntimeStatus } from './api';
import type { StartupDiagnostic, StartupLine } from '../components/AgentStartupProgress';
import { formatStartupElapsed } from '../components/AgentStartupProgress';

export type StartupInstrumentation = {
  startedAtMs: number;
  eventCount: number;
  streamState: 'opening' | 'streaming' | 'complete' | 'error';
  streamElapsedMs?: number;
  lastEvent?: string;
  agentStatus?: string;
  runtimePhase?: string;
  runtimeHealthy?: boolean;
  enforcerConnected?: boolean;
  verificationState: 'pending' | 'checking' | 'confirmed' | 'failed';
  verificationElapsedMs?: number;
};

export function initialStartupInstrumentation(startedAtMs = Date.now()): StartupInstrumentation {
  return {
    startedAtMs,
    eventCount: 0,
    streamState: 'opening',
    verificationState: 'pending',
  };
}

export function diagnosticsForStartup(instrumentation: StartupInstrumentation): StartupDiagnostic[] {
  return [
    {
      label: 'Stream',
      value: instrumentation.streamState === 'complete'
        ? `complete in ${formatStartupElapsed(instrumentation.streamElapsedMs)}`
        : instrumentation.streamState,
      state: instrumentation.streamState === 'error' ? 'error' : instrumentation.streamState === 'complete' ? 'done' : 'active',
    },
    {
      label: 'Events',
      value: `${instrumentation.eventCount}${instrumentation.lastEvent ? ` · ${instrumentation.lastEvent}` : ''}`,
      state: instrumentation.eventCount > 0 ? 'done' : 'muted',
    },
    {
      label: 'Agent status',
      value: instrumentation.agentStatus || 'pending',
      state: instrumentation.agentStatus === 'running' ? 'done' : instrumentation.verificationState === 'failed' ? 'error' : 'active',
    },
    {
      label: 'Runtime',
      value: runtimeSummary(instrumentation),
      state: instrumentation.runtimePhase === 'running' && instrumentation.runtimeHealthy !== false ? 'done' : instrumentation.verificationState === 'failed' ? 'error' : 'muted',
    },
    {
      label: 'Verification',
      value: instrumentation.verificationState === 'confirmed'
        ? `confirmed in ${formatStartupElapsed(instrumentation.verificationElapsedMs)}`
        : instrumentation.verificationState,
      state: instrumentation.verificationState === 'failed' ? 'error' : instrumentation.verificationState === 'confirmed' ? 'done' : 'active',
    },
  ];
}

export function markStartupLine(lines: StartupLine[], next: StartupLine): StartupLine[] {
  const existing = lines.find((line) => line.id === next.id);
  if (existing) {
    return lines.map((line) => line.id === next.id ? { ...line, ...next } : line);
  }
  return [
    ...lines.map((line) => line.state === 'active' ? { ...line, state: 'done' as const } : line),
    next,
  ];
}

export function markActiveStartupLines(lines: StartupLine[], state: 'done' | 'error'): StartupLine[] {
  return lines.map((line) => line.state === 'active' ? { ...line, state } : line);
}

export async function verifyStartupReportedRunning(
  agentName: string,
  startedAtMs: number,
  onInstrumentation: (update: (current: StartupInstrumentation) => StartupInstrumentation) => void,
  onLines: (update: (current: StartupLine[]) => StartupLine[]) => void,
  timeoutMs = 8000,
) {
  const deadline = Date.now() + timeoutMs;
  let lastAgentStatus = 'unknown';
  let lastRuntime: RawAgentRuntimeStatus | undefined;

  onInstrumentation((current) => ({ ...current, verificationState: 'checking' }));
  onLines((current) => markStartupLine(current, {
    id: 'verify-running',
    label: 'Verifying gateway reported running status',
    detail: `t+${formatStartupElapsed(Date.now() - startedAtMs)}`,
    state: 'active',
  }));

  while (Date.now() <= deadline) {
    const agent = await api.agents.show(agentName);
    lastAgentStatus = agent.status || 'unknown';
    try {
      lastRuntime = await api.agents.runtimeStatus(agentName);
    } catch {
      lastRuntime = undefined;
    }

    const elapsedMs = Date.now() - startedAtMs;
    onInstrumentation((current) => ({
      ...current,
      agentStatus: lastAgentStatus,
      runtimePhase: lastRuntime?.phase,
      runtimeHealthy: lastRuntime?.healthy,
      enforcerConnected: lastRuntime?.transport?.enforcerConnected,
      verificationElapsedMs: elapsedMs,
    }));
    onLines((current) => markStartupLine(current, {
      id: 'verify-running',
      label: `Gateway status: ${lastAgentStatus}`,
      detail: runtimeLineDetail(lastRuntime, elapsedMs),
      state: lastAgentStatus === 'running' ? 'done' : 'active',
    }));

    if (lastAgentStatus === 'running') {
      onInstrumentation((current) => ({ ...current, verificationState: 'confirmed', verificationElapsedMs: elapsedMs }));
      return;
    }

    await wait(500);
  }

  onInstrumentation((current) => ({ ...current, verificationState: 'failed' }));
  onLines((current) => markActiveStartupLines(current, 'error'));
  throw new Error(`Startup stream completed, but gateway status is "${lastAgentStatus}"${lastRuntime?.phase ? ` and runtime phase is "${lastRuntime.phase}"` : ''}.`);
}

function runtimeSummary(instrumentation: StartupInstrumentation) {
  if (!instrumentation.runtimePhase) return 'pending';
  const health = instrumentation.runtimeHealthy === undefined ? '' : instrumentation.runtimeHealthy ? ' healthy' : ' unhealthy';
  const transport = instrumentation.enforcerConnected === undefined ? '' : instrumentation.enforcerConnected ? ' · enforcer connected' : ' · enforcer disconnected';
  return `${instrumentation.runtimePhase}${health}${transport}`;
}

function runtimeLineDetail(runtime: RawAgentRuntimeStatus | undefined, elapsedMs: number) {
  const elapsed = `t+${formatStartupElapsed(elapsedMs)}`;
  if (!runtime?.phase) return elapsed;
  const health = runtime.healthy === undefined ? '' : runtime.healthy ? 'healthy' : 'unhealthy';
  const enforcer = runtime.transport?.enforcerConnected === undefined ? '' : runtime.transport.enforcerConnected ? 'enforcer connected' : 'enforcer disconnected';
  return [runtime.phase, health, enforcer, elapsed].filter(Boolean).join(' · ');
}

function wait(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
