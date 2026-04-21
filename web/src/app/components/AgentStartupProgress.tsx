import type { ReactNode } from 'react';
import { Loader2, RefreshCw } from 'lucide-react';
import { Button } from './ui/button';
import type { RawAgentStartProgress } from '../lib/api';

export type StartupLine = {
  id: string;
  label: string;
  detail?: string;
  state: 'active' | 'done' | 'error';
};

export type StartupDiagnostic = {
  label: string;
  value: string;
  state?: 'active' | 'done' | 'error' | 'muted';
};

export function formatStartupElapsed(ms?: number) {
  if (ms === undefined || !Number.isFinite(ms)) return 'pending';
  if (ms < 1000) return `${Math.max(0, Math.round(ms))}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export function startupLabel(event: RawAgentStartProgress) {
  const raw = event.description || event.name || event.type;
  return raw.replace(/_/g, ' ');
}

export function applyStartupProgress(current: StartupLine[], event: RawAgentStartProgress, fallbackElapsedMs?: number): StartupLine[] {
  const elapsedMs = event.elapsed_ms ?? fallbackElapsedMs;
  const elapsed = elapsedMs === undefined ? undefined : `gateway t+${formatStartupElapsed(elapsedMs)}`;
  const phaseElapsed = event.phase_elapsed_ms === undefined ? undefined : `phase ${formatStartupElapsed(event.phase_elapsed_ms)}`;
  const detail = [phaseElapsed, elapsed].filter(Boolean).join(' · ');
  if (event.type === 'complete') {
    return [
      ...current.map((line) => line.state === 'active' ? { ...line, state: 'done' as const } : line),
      { id: 'startup-complete', label: 'Runtime startup complete', detail: [event.model, elapsed].filter(Boolean).join(' · '), state: 'done' },
    ];
  }
  if (event.type === 'error') {
    return [
      ...current.map((line) => line.state === 'active' ? { ...line, state: 'error' as const } : line),
      { id: `error-${Date.now()}`, label: event.error || 'Startup failed', detail: elapsed, state: 'error' },
    ];
  }

  const phase = event.phase ?? Date.now();
  return [
    ...current.map((line) => line.state === 'active' ? { ...line, state: 'done' as const } : line),
    {
      id: `phase-${phase}-${event.name || event.description || event.type}`,
      label: startupLabel(event),
      detail,
      state: 'active',
    },
  ];
}

interface AgentStartupProgressProps {
  agentName: string;
  lines: StartupLine[];
  error?: string;
  complete?: boolean;
  title?: string;
  description?: ReactNode;
  diagnostics?: StartupDiagnostic[];
  onRetry?: () => void;
  onCancel?: () => void;
  retryLabel?: string;
}

export function AgentStartupProgress({
  agentName,
  lines,
  error = '',
  complete = false,
  title = 'Starting agent',
  description,
  diagnostics = [],
  onRetry,
  onCancel,
  retryLabel = 'Try again',
}: AgentStartupProgressProps) {
  return (
    <div style={{ display: 'grid', gap: 18 }}>
      <div style={{ display: 'grid', gap: 8 }}>
        <div className="eyebrow">{complete ? 'Startup complete' : title}</div>
        <p style={{ margin: 0, color: 'var(--ink-mid)', fontSize: 13, lineHeight: 1.55 }}>
          {description ?? (
            <>Agency is starting <span className="mono" style={{ color: 'var(--ink)' }}>@{agentName}</span>. Startup progress below is streamed from the gateway.</>
          )}
        </p>
        {diagnostics.length > 0 && (
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(120px, 1fr))', gap: 8 }}>
            {diagnostics.map((item) => (
              <div key={item.label} style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 8, padding: '8px 10px', background: 'var(--warm-2)' }}>
                <div className="eyebrow" style={{ fontSize: 8 }}>{item.label}</div>
                <div className="mono" style={{
                  marginTop: 4,
                  fontSize: 12,
                  color: item.state === 'error' ? 'var(--red)' : item.state === 'active' ? 'var(--ink)' : item.state === 'muted' ? 'var(--ink-faint)' : 'var(--teal)',
                  overflowWrap: 'anywhere',
                }}>
                  {item.value}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, overflow: 'hidden', background: 'var(--warm-2)' }}>
        <div style={{ display: 'grid', gridTemplateColumns: '88px minmax(0, 1fr)', minHeight: 174 }}>
          <div style={{ display: 'grid', placeItems: 'center', borderRight: '0.5px solid var(--ink-hairline)', background: 'var(--warm)' }}>
            <div style={{ position: 'relative', width: 54, height: 54, display: 'grid', placeItems: 'center' }}>
              <span style={{ position: 'absolute', inset: 0, border: '0.5px solid var(--teal-border)', borderRadius: '50%', opacity: 0.45 }} />
              <span style={{ position: 'absolute', inset: 8, border: '0.5px solid var(--ink-hairline-strong)', borderRadius: '50%' }} />
              <Loader2 className={complete || error ? '' : 'animate-spin'} style={{ width: 22, height: 22, color: error ? 'var(--red)' : complete ? 'var(--teal)' : 'var(--ink)' }} />
            </div>
          </div>
          <div style={{ padding: 18, display: 'grid', gap: 12, alignContent: 'start' }}>
            <div className="eyebrow" style={{ fontSize: 9 }}>Startup stream</div>
            <div style={{ display: 'grid', gap: 10, maxHeight: 210, overflowY: 'auto', paddingRight: 6 }}>
              {lines.slice(-8).map((line) => (
                <div key={line.id} style={{ display: 'grid', gridTemplateColumns: '18px minmax(0, 1fr)', gap: 10, alignItems: 'start' }}>
                  <span style={{ marginTop: 5, width: 8, height: 8, borderRadius: 999, background: line.state === 'error' ? 'var(--red)' : line.state === 'done' ? 'var(--teal)' : 'var(--ink)', opacity: line.state === 'done' ? 0.7 : 1 }} />
                  <span className="mono" style={{ fontSize: 12, color: line.state === 'error' ? 'var(--red)' : line.state === 'active' ? 'var(--ink)' : 'var(--ink-mid)', lineHeight: 1.4, overflowWrap: 'anywhere' }}>
                    {line.label}
                    {line.detail && <span style={{ color: 'var(--ink-faint)' }}> · {line.detail}</span>}
                  </span>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>

      {error && <p style={{ margin: 0, color: 'var(--red)', fontSize: 13, lineHeight: 1.5, overflowWrap: 'anywhere' }}>{error}</p>}

      {(onCancel || (error && onRetry)) && (
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
          {onCancel ? (
            <button type="button" onClick={onCancel} style={{ border: 0, background: 'transparent', color: 'var(--ink-mid)', fontSize: 13, cursor: 'pointer' }}>
              Close
            </button>
          ) : <span />}
          {error && onRetry && (
            <Button variant="outline" onClick={onRetry}>
              <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
              {retryLabel}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
