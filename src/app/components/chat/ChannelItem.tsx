import { Hash } from 'lucide-react';
import { Badge } from '../ui/badge';
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
        'flex w-full items-start gap-2 rounded-md px-2.5 py-2 text-left text-sm transition-colors',
        'hover:bg-accent',
        active && 'bg-accent',
      )}
    >
      <Hash className="h-4 w-4 shrink-0 text-muted-foreground mt-0.5" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <span className={cn('truncate font-medium', channel.unreadCount > 0 && 'text-white')}>
            {channel.name}
          </span>
          <div className="flex gap-1 shrink-0">
            {channel.mentionCount > 0 && (
              <Badge variant="destructive" className="h-5 px-1 text-xs">
                @{channel.mentionCount}
              </Badge>
            )}
            {channel.unreadCount > 0 && (
              <Badge className="h-5 px-1 text-xs bg-primary">
                {channel.unreadCount}
              </Badge>
            )}
          </div>
        </div>
        {channel.topic && (
          <span className="block truncate text-[11px] text-muted-foreground mt-0.5">{channel.topic}</span>
        )}
      </div>
    </button>
  );
}
