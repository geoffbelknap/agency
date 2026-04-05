import { KIND_COLORS } from './constants';

export function KindBadge({ kind }: { kind: string }) {
  const hex = KIND_COLORS[kind] || KIND_COLORS.unknown;
  return (
    <span
      className="text-[10px] px-1.5 py-0.5 rounded font-medium"
      style={{ backgroundColor: hex + '1a', color: hex }}
    >
      {kind}
    </span>
  );
}

export function SourceBadge({ source }: { source: string }) {
  const colors =
    source === 'agent'
      ? 'bg-green-500/10 text-green-600 dark:text-green-400'
      : source === 'platform'
        ? 'bg-cyan-500/10 text-cyan-600 dark:text-cyan-400'
        : 'bg-secondary text-muted-foreground';
  return <span className={`text-[10px] px-1.5 py-0.5 rounded ${colors}`}>{source}</span>;
}
