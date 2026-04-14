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
  showInactive?: boolean;
  onToggleInactive?: () => void;
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

function hasKnownDmStatus(dmStatuses: Record<string, DmStatus> | undefined, dmName: string) {
  return !!dmStatuses && Object.prototype.hasOwnProperty.call(dmStatuses, dmName);
}

function displayBuildId(buildId: string): string | null {
  const normalized = buildId.trim();
  if (!normalized || normalized === 'unknown' || normalized === 'unknown-dirty') return null;
  return normalized;
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
        className="flex w-full items-center gap-1.5 px-2 py-1.5"
      >
        {open ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        <span
          className={cn(
            'text-[11px] font-medium uppercase tracking-[0.14em]',
            titleClassName ?? 'text-muted-foreground',
          )}
        >
          {title}
        </span>
      </button>
      {open && <div className="mt-1 space-y-1">{children}</div>}
    </div>
  );
}

function SidebarContent({
  channels,
  selectedChannel,
  onSelect,
  dmStatuses,
  onCreateChannel,
  showInactive,
  onToggleInactive,
}: Omit<ChannelSidebarProps, 'mobileOpen' | 'onMobileClose' | 'onBrowseChannels'> & { onBrowseChannels?: () => void }) {
  const [filter, setFilter] = useState('');

  const filtered = channels.filter((ch) =>
    ch.name.toLowerCase().includes(filter.toLowerCase()),
  );

  const grouped = groupChannels(filtered);

  return (
    <div className="flex h-full w-[18.5rem] flex-col bg-sidebar">
      <div className="border-b border-border/70 px-3 pb-3 pt-4">
        <div className="mb-3 px-1">
          <div className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
            Messaging
          </div>
          <h2 className="mt-1 text-lg text-sidebar-foreground">Channels</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Direct agent work, DMs, and audit-friendly coordination.
          </p>
        </div>
        <div className="relative flex-1">
          <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search channels..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="rounded-2xl bg-card pl-8 text-sm"
          />
        </div>
        <div className="mt-3 flex items-center gap-2">
          {onCreateChannel && (
            <button
              onClick={onCreateChannel}
              className="inline-flex flex-1 items-center justify-center gap-2 rounded-2xl border border-border bg-card px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-sidebar-accent"
            >
              <Plus className="h-3.5 w-3.5 shrink-0" />
              <span>Add channel</span>
            </button>
          )}
          {onToggleInactive && (
            <button
              onClick={onToggleInactive}
              className={cn(
                'rounded-2xl px-3 py-2 text-sm font-medium transition-colors',
                showInactive
                  ? 'bg-accent text-foreground'
                  : 'text-muted-foreground hover:bg-sidebar-accent hover:text-foreground',
              )}
            >
              {showInactive ? 'Hide inactive' : 'Show inactive'}
            </button>
          )}
        </div>
      </div>
      <ScrollArea className="flex-1 px-3 py-3 overscroll-contain">
        <SidebarSection title="Channels">
          {grouped.channels.map((ch) => (
            <ChannelItem
              key={ch.id}
              channel={ch}
              active={ch.id === selectedChannel?.id}
              onClick={() => onSelect(ch)}
            />
          ))}
        </SidebarSection>

        {grouped.dms.length > 0 && (
          <SidebarSection title="Direct messages" titleClassName="text-muted-foreground">
            {grouped.dms.map((ch) => {
              const isActive = ch.id === selectedChannel?.id;
              const dmName = dmDisplayName(ch.name);
              const knownAgent = hasKnownDmStatus(dmStatuses, dmName);
              return (
                <button
                  key={ch.id}
                  onClick={() => onSelect(ch)}
                  className={cn(
                    'flex w-full items-center gap-3 rounded-2xl px-3 py-2.5 text-left text-sm transition-colors',
                    isActive ? 'bg-accent/80 text-foreground ring-1 ring-primary/10' : 'hover:bg-accent/45',
                  )}
                >
                  {knownAgent ? (
                    <div className={cn('flex h-7 w-7 shrink-0 items-center justify-center rounded-xl', isActive ? 'bg-primary/12' : 'bg-background/70')}>
                      <AgentStatusDot status={dmStatuses?.[dmName] ?? 'unknown'} className="shrink-0" />
                    </div>
                  ) : (
                    <span
                      className="flex h-7 w-7 shrink-0 items-center justify-center rounded-xl bg-background/70"
                      aria-label="Unavailable"
                      title="Agent unavailable"
                    >
                      <span className="h-2.5 w-2.5 rounded-full bg-muted-foreground/45" />
                    </span>
                  )}
                  <span className={cn('flex-1 truncate font-medium', ch.unreadCount > 0 && !isActive && 'text-foreground')}>
                    {dmName}
                  </span>
                  {ch.unreadCount > 0 && (
                    <span className="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-primary/90 px-1.5 text-[11px] font-semibold text-primary-foreground">
                      {ch.unreadCount}
                    </span>
                  )}
                </button>
              );
            })}
          </SidebarSection>
        )}

        {grouped.internal.length > 0 && (
          <SidebarSection title="Internal" defaultOpen={false} titleClassName="text-muted-foreground">
            {grouped.internal.map((ch) => (
              <button
                key={ch.id}
                onClick={() => onSelect(ch)}
                className={cn(
                  'flex w-full items-center gap-3 rounded-2xl px-3 py-2.5 text-left text-sm transition-colors',
                  ch.id === selectedChannel?.id ? 'bg-accent/80 text-foreground ring-1 ring-primary/10' : 'hover:bg-accent/45',
                )}
              >
                <div className={cn(
                  'flex h-7 w-7 shrink-0 items-center justify-center rounded-xl',
                  ch.id === selectedChannel?.id ? 'bg-primary/12 text-primary' : 'text-muted-foreground',
                )}>
                  <Hash className="h-4 w-4" />
                </div>
                <span className={cn('truncate font-medium', ch.unreadCount > 0 && ch.id !== selectedChannel?.id && 'text-foreground')}>
                  {ch.name}
                </span>
                {ch.unreadCount > 0 && (
                  <span className="ml-auto inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-primary/90 px-1.5 text-[11px] font-semibold text-primary-foreground shrink-0">
                    {ch.unreadCount}
                  </span>
                )}
              </button>
            ))}
          </SidebarSection>
        )}
      </ScrollArea>
      {displayBuildId(__BUILD_ID__) && (
        <div className="border-t border-border/70 px-4 py-2 text-[11px] text-muted-foreground select-all">
          {displayBuildId(__BUILD_ID__)}
        </div>
      )}
    </div>
  );
}

export function ChannelSidebar({
  channels,
  selectedChannel,
  onSelect,
  dmStatuses,
  onCreateChannel,
  showInactive,
  onToggleInactive,
  onBrowseChannels,
  mobileOpen,
  onMobileClose,
}: ChannelSidebarProps) {
  const isMobile = useIsMobile();

  if (isMobile) {
    return (
        <Sheet open={mobileOpen} onOpenChange={(open) => { if (!open) onMobileClose?.(); }}>
        <SheetContent side="left" className="w-[18.5rem] border-border bg-sidebar p-0">
          <SidebarContent
            channels={channels}
            selectedChannel={selectedChannel}
            onSelect={onSelect}
            dmStatuses={dmStatuses}
            onCreateChannel={onCreateChannel}
            showInactive={showInactive}
            onToggleInactive={onToggleInactive}
            onBrowseChannels={onBrowseChannels}
          />
        </SheetContent>
      </Sheet>
    );
  }

  return (
    <div className="border-r border-border/70 bg-sidebar">
      <SidebarContent
        channels={channels}
        selectedChannel={selectedChannel}
        onSelect={onSelect}
        dmStatuses={dmStatuses}
        onCreateChannel={onCreateChannel}
        showInactive={showInactive}
        onToggleInactive={onToggleInactive}
        onBrowseChannels={onBrowseChannels}
      />
    </div>
  );
}
