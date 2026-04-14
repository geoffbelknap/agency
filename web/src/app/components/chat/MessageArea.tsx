import { useState, type ReactNode } from 'react';
import { Hash, Menu, Users } from 'lucide-react';
import { Button } from '../ui/button';
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

export function MessageArea({ channel, messages, loading, onSend, typingAgents, processingAgents, agentStatuses, agentActivity, onReply, onEdit, onDelete, onReact, onUnreact, scrollToMessageId, headerActions, onOpenSidebar, hasMore, onLoadMore, loadingMore, onAgentClick }: MessageAreaProps) {
  const [membersOpen, setMembersOpen] = useState(false);
  const memberCount = channel.members.length;

  return (
    <div className="flex-1 flex min-h-0 min-w-0 flex-col">
      <div className="border-b border-border px-4 py-4 md:px-5">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-center gap-2">
              {onOpenSidebar && (
                <Button
                  variant="ghost"
                size="icon"
                onClick={onOpenSidebar}
                aria-label="Open sidebar"
                className="h-8 w-8 text-muted-foreground hover:text-accent-foreground hover:bg-accent shrink-0"
                >
                  <Menu className="w-5 h-5" />
                </Button>
              )}
              <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-2xl bg-secondary text-muted-foreground">
                <Hash className="h-4 w-4" />
              </div>
              <div className="min-w-0">
                <div className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                  Conversation
                </div>
                <h2 className="truncate text-lg font-semibold text-foreground">{channel.name}</h2>
              </div>
            </div>
            {channel.topic && (
              <p className="mt-2 max-w-2xl text-sm text-muted-foreground">{channel.topic}</p>
            )}
          </div>
          <div className="flex shrink-0 items-start gap-2">
            {memberCount > 0 && (
              <div className="relative">
                <button
                  className="flex items-center gap-2 rounded-2xl border border-border bg-card px-3 py-2 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                  onClick={() => setMembersOpen((v) => !v)}
                >
                  <Users className="h-3.5 w-3.5" />
                  <span>{memberCount} member{memberCount === 1 ? '' : 's'}</span>
                </button>
                {membersOpen && (
                  <>
                    <div className="fixed inset-0 z-40" onClick={() => setMembersOpen(false)} />
                    <div className="absolute right-0 top-full z-50 mt-2 min-w-[220px] max-w-[280px] rounded-2xl border border-border bg-card p-3 shadow-xl">
                      <div className="mb-2 text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                        Members ({memberCount})
                      </div>
                      <div className="max-h-[240px] space-y-1.5 overflow-auto">
                        {channel.members.map((member) => (
                          <div key={member} className="flex items-center gap-2">
                            <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-xl bg-primary/12 text-[10px] font-semibold text-primary">
                              {member.charAt(0).toUpperCase()}
                            </div>
                            <span className="truncate text-xs text-foreground">{member}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  </>
                )}
              </div>
            )}
            {headerActions && (
              <div className="rounded-2xl border border-border bg-card p-1">
                {headerActions}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Messages */}
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

      <ComposeBar onSend={onSend} channelName={channel.name} />
    </div>
  );
}
