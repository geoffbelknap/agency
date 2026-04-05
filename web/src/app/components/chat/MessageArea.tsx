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

  return (
    <div className="flex-1 flex min-h-0 min-w-0 flex-col">
      {/* Channel Header */}
      <div className="p-3 md:p-4 border-b border-border">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0">
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
            <Hash className="w-5 h-5 text-muted-foreground shrink-0" />
            <h2 className="font-semibold text-foreground truncate">{channel.name}</h2>
            {channel.topic && (
              <span className="text-xs text-muted-foreground ml-2 truncate hidden sm:inline">{channel.topic}</span>
            )}
          </div>
          <div className="flex items-center gap-2 shrink-0">
            {channel.members.length > 0 && (
              <div className="relative">
                <button
                  className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors px-2 py-1 rounded hover:bg-accent"
                  onClick={() => setMembersOpen((v) => !v)}
                >
                  <Users className="w-3.5 h-3.5" />
                  <span className="hidden sm:inline">{channel.members.length} members</span>
                  <span className="sm:hidden">{channel.members.length}</span>
                </button>
                {membersOpen && (
                  <>
                    <div className="fixed inset-0 z-40" onClick={() => setMembersOpen(false)} />
                    <div className="absolute right-0 top-full mt-1 bg-secondary border border-border rounded-lg p-3 min-w-[200px] max-w-[280px] shadow-xl z-50">
                      <div className="text-xs text-muted-foreground mb-2 font-medium">Members ({channel.members.length})</div>
                      <div className="space-y-1.5 max-h-[240px] overflow-auto">
                        {channel.members.map((member) => (
                          <div key={member} className="flex items-center gap-2">
                            <div className="w-5 h-5 rounded bg-primary flex items-center justify-center text-[9px] font-semibold text-white shrink-0">
                              {member.charAt(0).toUpperCase()}
                            </div>
                            <span className="text-xs text-foreground truncate">{member}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  </>
                )}
              </div>
            )}
            {headerActions}
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

      {/* Typing Indicator — combines typing and processing agents */}
      <TypingIndicator
        agents={[...new Set([...(typingAgents || []), ...(processingAgents || [])])]}
        activity={agentActivity}
      />

      {/* Compose Bar */}
      <ComposeBar onSend={onSend} channelName={channel.name} />
    </div>
  );
}
