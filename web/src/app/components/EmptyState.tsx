import { type ReactNode } from 'react';

interface EmptyStateProps {
  icon?: ReactNode;
  title: string;
  description?: string;
  action?: ReactNode;
}

function DefaultMark() {
  return (
    <svg width="32" height="32" viewBox="0 0 52 52" className="opacity-25">
      <rect x="0" y="0" width="22" height="22" rx="3" className="fill-foreground" />
      <rect x="26" y="0" width="22" height="22" rx="3" className="fill-foreground" />
      <rect x="0" y="26" width="22" height="22" rx="3" className="fill-foreground" />
      <rect x="26" y="26" width="22" height="22" rx="3" className="fill-foreground" />
    </svg>
  );
}

export function EmptyState({ icon, title, description, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center py-12 text-center">
      <div className="text-muted-foreground/70 mb-3">
        {icon || <DefaultMark />}
      </div>
      <h3 className="text-xs text-muted-foreground mb-1" style={{ fontFamily: 'var(--font-mono)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>{title}</h3>
      {description && (
        <p className="text-xs text-muted-foreground/70 max-w-sm">{description}</p>
      )}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}
