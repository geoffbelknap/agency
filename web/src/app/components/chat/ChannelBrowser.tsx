import { useEffect, useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '../ui/dialog';
import { Input } from '../ui/input';
import { Button } from '../ui/button';
import { ScrollArea } from '../ui/scroll-area';
import { Badge } from '../ui/badge';
import { Skeleton } from '../ui/skeleton';
import { Search, Users } from 'lucide-react';
import { api } from '../../lib/api';

interface ChannelBrowserProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onJoinChannel: (channelName: string) => void;
}

interface BrowseChannel {
  name: string;
  topic: string;
  memberCount: number;
}

export function ChannelBrowser({ open, onOpenChange, onJoinChannel }: ChannelBrowserProps) {
  const [channels, setChannels] = useState<BrowseChannel[]>([]);
  const [loading, setLoading] = useState(false);
  const [filter, setFilter] = useState('');

  useEffect(() => {
    if (!open) return;
    setFilter('');
    setLoading(true);
    api.channels
      .list()
      .then((data: any[]) => {
        const mapped: BrowseChannel[] = data.map((c) => ({
          name: c.name,
          topic: c.topic || '',
          memberCount: Array.isArray(c.members) ? c.members.length : 0,
        }));
        setChannels(mapped);
      })
      .catch(() => setChannels([]))
      .finally(() => setLoading(false));
  }, [open]);

  const filtered = channels.filter((ch) =>
    ch.name.toLowerCase().includes(filter.toLowerCase()) ||
    ch.topic.toLowerCase().includes(filter.toLowerCase()),
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl bg-card border-border">
        <DialogHeader>
          <DialogTitle className="text-foreground">Browse Channels</DialogTitle>
          <DialogDescription className="text-muted-foreground">
            Discover and open channels available in the platform.
          </DialogDescription>
        </DialogHeader>

        <div className="relative mt-1">
          <Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search channels..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="pl-9 bg-secondary border-border text-foreground placeholder:text-muted-foreground"
            autoFocus
          />
        </div>

        <ScrollArea className="h-[400px] pr-2">
          {loading ? (
            <div className="flex flex-col gap-2 py-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <div
                  key={i}
                  data-testid="channel-skeleton"
                  className="flex items-center gap-3 rounded-md border border-border bg-secondary/40 p-3"
                >
                  <div className="flex-1 space-y-2">
                    <Skeleton className="h-4 w-32 bg-border" />
                    <Skeleton className="h-3 w-56 bg-border" />
                  </div>
                  <Skeleton className="h-8 w-16 bg-border" />
                </div>
              ))}
            </div>
          ) : filtered.length === 0 ? (
            <div className="flex items-center justify-center h-32 text-muted-foreground text-sm">
              No channels found
            </div>
          ) : (
            <div className="flex flex-col gap-1 py-1">
              {filtered.map((ch) => (
                <div
                  key={ch.name}
                  className="flex items-center gap-3 rounded-md border border-transparent hover:border-border hover:bg-secondary/40 p-3 transition-colors"
                >
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-medium text-foreground truncate">{ch.name}</span>
                    </div>
                    {ch.topic && (
                      <p className="text-sm text-muted-foreground truncate mt-0.5">{ch.topic}</p>
                    )}
                    <div className="flex items-center gap-1 mt-1">
                      <Users className="h-3 w-3 text-muted-foreground" />
                      <Badge
                        variant="secondary"
                        className="text-xs text-muted-foreground bg-secondary border-0 px-1.5 py-0"
                      >
                        {ch.memberCount === 1 ? '1 member' : `${ch.memberCount} members`}
                      </Badge>
                    </div>
                  </div>
                  <Button
                    size="sm"
                    variant="secondary"
                    aria-label={`Open ${ch.name}`}
                    onClick={() => onJoinChannel(ch.name)}
                    className="shrink-0 bg-secondary hover:bg-border text-foreground border-border"
                  >
                    Open
                  </Button>
                </div>
              ))}
            </div>
          )}
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}
