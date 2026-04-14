import { Hash } from 'lucide-react';
import type { Channel } from '../../types';
import { cn } from '../ui/utils';

interface ChannelItemProps {
  channel: Channel;
  active: boolean;
  onClick: () => void;
}

export function ChannelItem({ channel, active, onClick }: ChannelItemProps) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'flex w-full items-start gap-3 rounded-2xl px-3 py-2.5 text-left text-sm transition-colors',
        active ? 'bg-accent/80 text-foreground ring-1 ring-primary/10' : 'hover:bg-accent/45',
      )}
    >
      <div className={cn(
        'mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-xl',
        active ? 'bg-primary/12 text-primary' : 'text-muted-foreground',
      )}>
        <Hash className="h-4 w-4" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <span className={cn('truncate font-medium', channel.unreadCount > 0 && !active && 'text-foreground')}>
            {channel.name}
          </span>
          <div className="flex gap-1 shrink-0">
            {channel.mentionCount > 0 && (
              <span className="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-destructive px-1.5 text-[11px] font-semibold text-destructive-foreground">
                @{channel.mentionCount}
              </span>
            )}
            {channel.unreadCount > 0 && (
              <span className="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-primary/90 px-1.5 text-[11px] font-semibold text-primary-foreground">
                {channel.unreadCount}
              </span>
            )}
          </div>
        </div>
        {channel.topic && (
          <span className="mt-0.5 block truncate text-xs text-muted-foreground">{channel.topic}</span>
        )}
      </div>
    </button>
  );
}
