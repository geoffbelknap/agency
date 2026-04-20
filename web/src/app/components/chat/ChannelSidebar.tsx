import { AtSign, Hash } from 'lucide-react';
import { Sheet, SheetContent, SheetDescription, SheetTitle } from '../ui/sheet';
import { useIsMobile } from '../ui/use-mobile';
import type { Channel } from '../../types';
import { cn } from '../ui/utils';

interface ChannelSidebarProps {
  channels: Channel[];
  selectedChannel: Channel | null;
  onSelect: (channel: Channel) => void;
  onCreateChannel?: () => void;
  onBrowseChannels?: () => void;
  showInactive?: boolean;
  onToggleInactive?: () => void;
  mobileOpen?: boolean;
  onMobileClose?: () => void;
}

function isDm(channel: Channel): boolean {
  const type = (channel as Channel & { type?: string }).type;
  return type === 'dm' || channel.name.startsWith('dm-');
}

function displayName(channel: Channel): string {
  return isDm(channel) && channel.name.startsWith('dm-') ? channel.name.slice(3) : channel.name;
}

interface SidebarContentProps {
  channels: Channel[];
  selectedChannel: Channel | null;
  onSelect: (channel: Channel) => void;
}

function SidebarContent({ channels, selectedChannel, onSelect }: SidebarContentProps) {
  return (
    <aside
      className="flex h-full min-h-0 w-[280px] shrink-0 flex-col overflow-hidden"
      style={{
        borderRight: '0.5px solid var(--ink-hairline)',
        background: 'var(--warm-2)',
      }}
    >
      <div className="scrollbar-none min-h-0 flex-1 overflow-y-auto" style={{ padding: 8 }}>
        {channels.map((channel) => {
          const active = channel.id === selectedChannel?.id;
          const dm = isDm(channel);
          const Icon = dm ? AtSign : Hash;

          return (
            <button
              key={channel.id}
              type="button"
              onClick={() => onSelect(channel)}
              className={cn('group flex w-full items-center gap-3 text-left transition-colors', active && 'is-active')}
              style={{
                minHeight: 42,
                padding: '10px 12px',
                marginBottom: 2,
                border: 0,
                borderRadius: 8,
                background: active ? 'var(--warm)' : 'transparent',
                boxShadow: active ? '0 0 0 0.5px var(--ink-hairline-strong)' : 'none',
              }}
            >
              <span
                className="relative flex h-5 w-5 shrink-0 items-center justify-center"
              >
                <Icon size={13} strokeWidth={1.7} style={{ color: 'var(--ink-mid)' }} />
              </span>
              <span className="min-w-0 flex-1">
                <span className="mono block truncate" style={{ fontSize: 12, color: 'var(--ink)' }}>
                  {displayName(channel)}
                </span>
              </span>
              {channel.unreadCount > 0 && (
                <span
                  className="mono flex h-6 min-w-6 items-center justify-center rounded-full px-2"
                  style={{ background: 'var(--teal)', color: 'white', fontSize: 9, height: 18, minWidth: 18 }}
                >
                  {channel.unreadCount}
                </span>
              )}
            </button>
          );
        })}
      </div>
    </aside>
  );
}

export function ChannelSidebar({
  channels,
  selectedChannel,
  onSelect,
  mobileOpen,
  onMobileClose,
}: ChannelSidebarProps) {
  const isMobile = useIsMobile();

  if (isMobile) {
    return (
      <Sheet open={mobileOpen} onOpenChange={(open) => { if (!open) onMobileClose?.(); }}>
        <SheetContent side="left" className="w-[280px] border-r-0 p-0">
          <SheetTitle className="sr-only">Conversations</SheetTitle>
          <SheetDescription className="sr-only">
            Select a direct message or shared channel.
          </SheetDescription>
          <SidebarContent
            channels={channels}
            selectedChannel={selectedChannel}
            onSelect={(channel) => { onSelect(channel); onMobileClose?.(); }}
          />
        </SheetContent>
      </Sheet>
    );
  }

  return (
    <div className="hidden h-full min-h-0 lg:block">
      <SidebarContent
        channels={channels}
        selectedChannel={selectedChannel}
        onSelect={onSelect}
      />
    </div>
  );
}
