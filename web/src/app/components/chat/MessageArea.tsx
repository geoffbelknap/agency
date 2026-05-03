import type { ReactNode } from 'react';
import { AtSign, Bot, Hash, Menu, MoreHorizontal } from 'lucide-react';
import { MessageList } from './MessageList';
import { ComposeBar } from './ComposeBar';
import { TypingIndicator } from './TypingIndicator';
import type { Channel, Message } from '../../types';

interface MessageAreaProps {
  channel: Channel;
  messages: Message[];
  loading: boolean;
  onSend: (content: string, flags?: { decision?: boolean; blocker?: boolean; question?: boolean }) => void;
  typingAgents?: string[];
  processingAgents?: string[];
  agentStatuses?: Record<string, string>;
  agentActivity?: Record<string, string>;
  onReply?: (message: Message) => void;
  onEdit?: (message: Message, newContent: string) => void;
  onDelete?: (message: Message) => void;
  onReact?: (message: Message, emoji: string) => void;
  onUnreact?: (message: Message, emoji: string) => void;
  scrollToMessageId?: string;
  headerActions?: ReactNode;
  onOpenSidebar?: () => void;
  hasMore?: boolean;
  onLoadMore?: () => void;
  loadingMore?: boolean;
  onAgentClick?: (agentName: string) => void;
}

function isDm(channel: Channel): boolean {
  const type = (channel as Channel & { type?: string }).type;
  return type === 'dm' || channel.name.startsWith('dm-');
}

function displayName(channel: Channel): string {
  return isDm(channel) && channel.name.startsWith('dm-') ? channel.name.slice(3) : channel.name;
}

function subtitle(channel: Channel): string {
  if (channel.topic) return channel.topic;
  if (channel.members.length > 0) {
    return `${channel.members.length} member${channel.members.length === 1 ? '' : 's'}`;
  }
  return isDm(channel) ? 'direct message' : 'workspace channel';
}

export function MessageArea({ channel, messages, loading, onSend, typingAgents, processingAgents, agentStatuses, agentActivity, onReply, onEdit, onDelete, onReact, onUnreact, scrollToMessageId, headerActions, onOpenSidebar, hasMore, onLoadMore, loadingMore, onAgentClick }: MessageAreaProps) {
  const Icon = isDm(channel) ? AtSign : Hash;
  const channelStatus = isDm(channel) ? agentStatuses?.[displayName(channel)] : undefined;

  return (
    <main className="flex min-h-0 min-w-0 flex-1 flex-col" style={{ background: 'var(--warm)' }}>
      <header
        className="flex shrink-0 items-center justify-between gap-5"
        style={{ padding: '16px 28px', borderBottom: '0.5px solid var(--ink-hairline)', background: 'var(--warm)' }}
      >
        <div className="flex min-w-0 items-center gap-3">
          {onOpenSidebar && (
            <button
              type="button"
              onClick={onOpenSidebar}
              aria-label="Open sidebar"
              className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full lg:hidden"
              style={{ border: '0.5px solid var(--ink-hairline)', background: 'var(--warm-2)', color: 'var(--ink-muted)' }}
            >
              <Menu size={18} strokeWidth={1.8} />
            </button>
          )}
          <div
            className="relative flex h-9 w-9 shrink-0 items-center justify-center"
            style={{ borderRadius: 8, background: 'var(--warm-3)', color: 'var(--ink-mid)' }}
          >
            {isDm(channel) ? <Bot size={16} strokeWidth={1.7} /> : <Icon size={16} strokeWidth={1.7} />}
            {isDm(channel) && (
              <span
                aria-label={channelStatus === 'running' ? 'Running' : undefined}
                className="absolute rounded-full"
                style={{ right: -1, bottom: -1, width: 10, height: 10, background: channelStatus === 'running' ? 'var(--teal)' : 'var(--ink-faint)', border: '2px solid var(--warm)' }}
              />
            )}
          </div>
          <div className="min-w-0">
            <h2 className="mono truncate" style={{ fontSize: 14, color: 'var(--ink)' }}>
              {displayName(channel)}
            </h2>
            <p className="mt-1 truncate" style={{ fontSize: 11, color: 'var(--ink-mid)' }}>
              {subtitle(channel)}
              {channelStatus ? ` · ${channelStatus}` : ''}
            </p>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {headerActions}
          <button
            type="button"
            aria-label="More channel actions"
            className="flex h-8 w-8 items-center justify-center rounded-md"
            style={{ border: 0, background: 'transparent', color: 'var(--ink-mid)' }}
          >
            <MoreHorizontal size={16} strokeWidth={1.8} />
          </button>
        </div>
      </header>

      <MessageList
        messages={messages}
        loading={loading}
        agentStatuses={agentStatuses}
        onReply={onReply}
        onEdit={onEdit}
        onDelete={onDelete}
        onReact={onReact}
        onUnreact={onUnreact}
        scrollToMessageId={scrollToMessageId}
        hasMore={hasMore}
        onLoadMore={onLoadMore}
        loadingMore={loadingMore}
        processingAgents={processingAgents}
        agentActivity={agentActivity}
        onAgentClick={onAgentClick}
      />

      <TypingIndicator
        agents={[...new Set([...(typingAgents || []), ...(processingAgents || [])])]}
        activity={agentActivity}
      />

      <ComposeBar onSend={onSend} channelName={displayName(channel)} />
    </main>
  );
}
