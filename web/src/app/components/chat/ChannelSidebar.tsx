import { useState } from 'react';
import { Input } from '../ui/input';
import { ScrollArea } from '../ui/scroll-area';
import { ChannelItem } from './ChannelItem';
import { ChevronDown, ChevronRight, Hash, Plus, Search } from 'lucide-react';
import { Sheet, SheetContent } from '../ui/sheet';
import { useIsMobile } from '../ui/use-mobile';
import type { Channel } from '../../types';
import { cn } from '../ui/utils';
import { AgentStatusDot } from './AgentStatusDot';

type DmStatus = 'running' | 'idle' | 'halted' | 'unknown';

interface ChannelSidebarProps {
  channels: Channel[];
  selectedChannel: Channel | null;
  onSelect: (channel: Channel) => void;
  dmStatuses?: Record<string, DmStatus>;
  onCreateChannel?: () => void;
  onBrowseChannels?: () => void;
  mobileOpen?: boolean;
  onMobileClose?: () => void;
}

interface GroupedChannels {
  channels: Channel[];
  dms: Channel[];
  internal: Channel[];
}

function groupChannels(channels: Channel[]): GroupedChannels {
  const result: GroupedChannels = { channels: [], dms: [], internal: [] };
  for (const ch of channels) {
    const type = (ch as Channel & { type?: string }).type;
    if (type === 'dm' || ch.name.startsWith('dm-')) {
      result.dms.push(ch);
    } else if (ch.name.startsWith('_')) {
      result.internal.push(ch);
    } else {
      result.channels.push(ch);
    }
  }
  return result;
}

function dmDisplayName(name: string): string {
  return name.startsWith('dm-') ? name.slice(3) : name;
}

interface SidebarSectionProps {
  title: string;
  defaultOpen?: boolean;
  titleClassName?: string;
  children: React.ReactNode;
}

function SidebarSection({ title, defaultOpen = true, titleClassName, children }: SidebarSectionProps) {
  const [open, setOpen] = useState(defaultOpen);

  return (
    <div className="mb-1">
      <button
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex w-full items-center gap-1 px-1 py-1 group"
      >
        {open ? (
          <ChevronDown className="h-3 w-3 text-muted-foreground/60 shrink-0" />
        ) : (
          <ChevronRight className="h-3 w-3 text-muted-foreground/60 shrink-0" />
        )}
        <span
          className={cn(
            'text-[10px] uppercase tracking-widest font-medium',
            titleClassName ?? 'text-muted-foreground/60',
          )}
        >
          {title}
        </span>
      </button>
      {open && <div>{children}</div>}
    </div>
  );
}

function SidebarContent({
  channels,
  selectedChannel,
  onSelect,
  dmStatuses,
  onCreateChannel,
}: Omit<ChannelSidebarProps, 'mobileOpen' | 'onMobileClose' | 'onBrowseChannels'> & { onBrowseChannels?: () => void }) {
  const [filter, setFilter] = useState('');

  const filtered = channels.filter((ch) =>
    ch.name.toLowerCase().includes(filter.toLowerCase()),
  );

  const grouped = groupChannels(filtered);

  return (
    <div className="flex h-full w-72 flex-col bg-sidebar">
      <div className="p-2">
        <div className="relative flex-1">
          <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search channels..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="pl-8 bg-card border-border text-sm"
          />
        </div>
      </div>
      <ScrollArea className="flex-1 px-2 overscroll-contain">
        {/* Channels section */}
        <SidebarSection title="Channels">
          {grouped.channels.map((ch) => (
            <ChannelItem
              key={ch.id}
              channel={ch}
              active={ch.id === selectedChannel?.id}
              onClick={() => onSelect(ch)}
            />
          ))}
          {onCreateChannel && (
            <button
              onClick={onCreateChannel}
              className="flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-sm text-muted-foreground/60 hover:text-muted-foreground transition-colors"
            >
              <Plus className="h-3.5 w-3.5 shrink-0" />
              <span className="text-xs">Add channel</span>
            </button>
          )}
        </SidebarSection>

        {grouped.dms.length > 0 && (
          <>
            <div className="my-2 border-t border-border/40" />
            <SidebarSection title="Direct Messages">
              {grouped.dms.map((ch) => {
                const isActive = ch.id === selectedChannel?.id;
                const dmName = dmDisplayName(ch.name);
                return (
                  <button
                    key={ch.id}
                    onClick={() => onSelect(ch)}
                    className={cn(
                      'flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm transition-colors',
                      'hover:bg-accent',
                      isActive && 'bg-accent',
                    )}
                  >
                    <AgentStatusDot status={dmStatuses?.[dmName] ?? 'unknown'} className="shrink-0" />
                    <span className={cn('flex-1 truncate font-medium', ch.unreadCount > 0 && 'text-white')}>
                      {dmName}
                    </span>
                    <div className="flex items-center gap-1 shrink-0">
                      {ch.unreadCount > 0 && (
                        <span className="flex h-5 min-w-5 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-semibold text-primary-foreground">
                          {ch.unreadCount}
                        </span>
                      )}
                      <span className="text-[10px] bg-secondary px-1 py-0.5 rounded text-muted-foreground">
                        AGENT
                      </span>
                    </div>
                  </button>
                );
              })}
            </SidebarSection>
          </>
        )}

        {grouped.internal.length > 0 && (
          <>
            <div className="my-2 border-t border-border/40" />
            <SidebarSection title="Internal" defaultOpen={false} titleClassName="text-muted-foreground/50">
              {grouped.internal.map((ch) => (
                <button
                  key={ch.id}
                  onClick={() => onSelect(ch)}
                  className={cn(
                    'flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm transition-colors',
                    'hover:bg-accent',
                    ch.id === selectedChannel?.id && 'bg-accent',
                  )}
                >
                  <Hash className="h-4 w-4 shrink-0 text-muted-foreground/60 mt-0.5" />
                  <span className={cn('truncate font-medium', ch.unreadCount > 0 && 'text-white')}>
                    {ch.name}
                  </span>
                  {ch.unreadCount > 0 && (
                    <span className="ml-auto flex h-5 min-w-5 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-semibold text-primary-foreground shrink-0">
                      {ch.unreadCount}
                    </span>
                  )}
                </button>
              ))}
            </SidebarSection>
          </>
        )}
      </ScrollArea>
      <div className="px-3 py-1.5 text-[10px] text-muted-foreground/50 select-all">
        {__BUILD_ID__}
      </div>
    </div>
  );
}

export function ChannelSidebar({
  channels,
  selectedChannel,
  onSelect,
  dmStatuses,
  onCreateChannel,
  onBrowseChannels,
  mobileOpen,
  onMobileClose,
}: ChannelSidebarProps) {
  const isMobile = useIsMobile();

  if (isMobile) {
    return (
      <Sheet open={mobileOpen} onOpenChange={(open) => { if (!open) onMobileClose?.(); }}>
        <SheetContent side="left" className="p-0 w-72 bg-sidebar border-border">
        <SidebarContent
          channels={channels}
          selectedChannel={selectedChannel}
          onSelect={onSelect}
          dmStatuses={dmStatuses}
          onCreateChannel={onCreateChannel}
          onBrowseChannels={onBrowseChannels}
        />
        </SheetContent>
      </Sheet>
    );
  }

  return (
    <div className="border-r border-border">
      <SidebarContent
        channels={channels}
        selectedChannel={selectedChannel}
        onSelect={onSelect}
        dmStatuses={dmStatuses}
        onCreateChannel={onCreateChannel}
        onBrowseChannels={onBrowseChannels}
      />
    </div>
  );
}
