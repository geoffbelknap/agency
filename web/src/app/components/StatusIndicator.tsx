import { AgentStatus, HealthStatus } from '../types';

interface StatusIndicatorProps {
  status: AgentStatus | HealthStatus;
  size?: 'sm' | 'md';
  pulse?: boolean;
  label?: string;
}

const statusConfig = {
  running:    { color: 'bg-primary', glow: 'shadow-[0_0_6px_var(--color-primary,#00A882)]/40', pulse: true },
  healthy:    { color: 'bg-primary', glow: 'shadow-[0_0_6px_var(--color-primary,#00A882)]/40', pulse: false },
  paused:     { color: 'bg-[hsl(38,92%,55%)]', glow: 'shadow-[0_0_6px_hsl(38,92%,55%,0.4)]', pulse: true },
  halted:     { color: 'bg-[hsl(38,92%,55%)]', glow: 'shadow-[0_0_6px_hsl(38,92%,55%,0.4)]', pulse: false },
  stopped:    { color: 'bg-[hsl(0,72%,55%)]', glow: 'shadow-[0_0_6px_hsl(0,72%,55%,0.3)]', pulse: false },
  unhealthy:  { color: 'bg-[hsl(0,72%,55%)]', glow: 'shadow-[0_0_6px_hsl(0,72%,55%,0.3)]', pulse: true },
  idle:       { color: 'bg-muted-foreground/40', glow: '', pulse: false },
  starting:   { color: 'bg-primary', glow: 'shadow-[0_0_6px_var(--color-primary,#00A882)]/40', pulse: true },
  restarting: { color: 'bg-primary', glow: 'shadow-[0_0_6px_var(--color-primary,#00A882)]/40', pulse: true },
  stopping:   { color: 'bg-[hsl(38,92%,55%)]', glow: 'shadow-[0_0_6px_hsl(38,92%,55%,0.4)]', pulse: true },
} as const;

export function StatusIndicator({ status, size = 'md', pulse: forcePulse, label }: StatusIndicatorProps) {
  const config = statusConfig[status as keyof typeof statusConfig] ?? {
    color: 'bg-[hsl(215,12%,40%)]',
    glow: '',
    pulse: false,
  };

  const shouldPulse = forcePulse ?? config.pulse;
  const sizeClass = size === 'sm' ? 'w-1.5 h-1.5' : 'w-2.5 h-2.5';
  const pingSizeClass = size === 'sm' ? 'w-1.5 h-1.5' : 'w-2.5 h-2.5';

  return (
    <span className="relative inline-flex" role="img" aria-label={label ?? status}>
      {shouldPulse && (
        <span className={`animate-ping absolute inline-flex ${pingSizeClass} rounded-full ${config.color} opacity-30`} />
      )}
      <span className={`relative inline-flex ${sizeClass} rounded-full ${config.color} ${config.glow}`} />
    </span>
  );
}
