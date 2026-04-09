// src/app/screens/Channels.tsx
import { useState, useEffect, useCallback } from 'react';
import { useParams } from 'react-router';
import { Search } from 'lucide-react';
import { useIsMobile } from '../components/ui/use-mobile';
import { ChannelSidebar } from '../components/chat/ChannelSidebar';
import { ChannelBrowser } from '../components/chat/ChannelBrowser';
import { CreateChannelDialog } from '../components/chat/CreateChannelDialog';
import { MessageArea } from '../components/chat/MessageArea';
import { ThreadPanel } from '../components/chat/ThreadPanel';
import { SearchPanel } from '../components/chat/SearchPanel';
import { Button } from '../components/ui/button';
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts';
import { useChannelSocket, SYSTEM_SENDERS } from '../hooks/useChannelSocket';
import { useChannelMessages, INITIAL_MESSAGE_PAGE_SIZE } from '../hooks/useChannelMessages';
import { HelpDialog } from './channels/HelpDialog';
import { AgentDetailSheet } from './channels/AgentDetailSheet';
import { api, type RawChannel, type RawAgent, type RawBudgetResponse } from '../lib/api';
import { fetchOperatorDisplayName, getOperatorDisplayName } from '../lib/operator';
import { formatMessageTime } from '../lib/time';
import type { Channel, Message } from '../types';

export function Channels() {
  const { name: urlChannelName } = useParams<{ name: string }>();
  const isMobile = useIsMobile();
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);
  const [channels, setChannels] = useState<Channel[]>([]);
  const [agentStatuses, setAgentStatuses] = useState<Record<string, 'running' | 'idle' | 'halted' | 'unknown'>>({});
  const [selectedChannel, setSelectedChannel] = useState<Channel | null>(null);
  const [threadParent, setThreadParent] = useState<Message | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [scrollTarget, setScrollTarget] = useState<string | undefined>(undefined);
  const [helpOpen, setHelpOpen] = useState(false);
  const [createChannelOpen, setCreateChannelOpen] = useState(false);
  const [browserOpen, setBrowserOpen] = useState(false);

  // Agent detail overlay
  const [agentDetailName, setAgentDetailName] = useState<string | null>(null);
  const [agentDetail, setAgentDetail] = useState<RawAgent | null>(null);
  const [agentBudget, setAgentBudget] = useState<RawBudgetResponse | null>(null);

  const {
    messages,
    loading,
    hasMore,
    loadingMore,
    setMessages,
    setLoading,
    mapRawMessages,
    loadMessages,
    loadMoreMessages,
    handleEdit: hookHandleEdit,
    handleDelete: hookHandleDelete,
    handleReact: hookHandleReact,
    handleUnreact: hookHandleUnreact,
    appendMessage,
    resetForChannel,
  } = useChannelMessages();

  const handleAgentClick = useCallback((agentName: string) => {
    setAgentDetailName(agentName);
    setAgentDetail(null);
    setAgentBudget(null);
    api.agents.show(agentName).then(setAgentDetail).catch(() => setAgentDetail(null));
    api.agents.budget(agentName).then(setAgentBudget).catch(() => setAgentBudget(null));
  }, []);

  const loadChannels = useCallback(async () => {
    try {
      const [data, agents] = await Promise.all([
        api.channels.list(),
        api.agents.list().catch(() => [] as RawAgent[]),
      ]);
      const mapped: Channel[] = data.map((c: RawChannel) => ({
        id: c.name,
        name: c.name,
        topic: c.topic || '',
        type: c.type,
        state: c.state,
        unreadCount: c.unread || 0,
        mentionCount: c.mentions || 0,
        lastActivity: '',
        members: (c.members || []).filter((m: string) => m !== '_operator'),
      })).filter((channel) => channel.state !== 'archived');
      const nextStatuses: Record<string, 'running' | 'idle' | 'halted' | 'unknown'> = {};
      for (const agent of agents ?? []) {
        if (agent.status === 'running') {
          nextStatuses[agent.name] = 'running';
        } else if (agent.status === 'halted' || agent.status === 'stopped' || agent.status === 'paused' || agent.status === 'unhealthy') {
          nextStatuses[agent.name] = 'halted';
        } else {
          nextStatuses[agent.name] = 'unknown';
        }
      }
      setAgentStatuses(nextStatuses);
      setChannels(mapped);
      return mapped;
    } catch (err) {
      console.error('loadChannels error:', err);
      return [];
    }
  }, []);

  // Initial load — runs once on mount (or when urlChannelName changes)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => {
    let active = true;
    setLoading(true);
    // Fetch operator display name for message rendering (fire-and-forget, cached after first call)
    fetchOperatorDisplayName();
    loadChannels().then((mapped) => {
      if (!active) return;
      if (mapped.length > 0) {
        const defaultChannel = mapped.find((c) => !c.name.startsWith('_')) || mapped[0];
        const target = urlChannelName
          ? mapped.find((c) => c.name === urlChannelName) || defaultChannel
          : defaultChannel;
        setSelectedChannel(target);
        loadMessages(target.name, INITIAL_MESSAGE_PAGE_SIZE).finally(() => {
          if (active) setLoading(false);
        });
      } else {
        setLoading(false);
      }
    });
    return () => { active = false; };
  }, [urlChannelName]);

  // Stable callbacks for useChannelSocket
  const handleAppendMessage = useCallback((msg: Message) => {
    appendMessage(msg);
  }, [appendMessage]);

  const handleUnreadIncrement = useCallback((channelName: string) => {
    setChannels((prev) =>
      prev.map((ch) =>
        ch.name === channelName
          ? { ...ch, unreadCount: ch.unreadCount + 1 }
          : ch
      )
    );
  }, []);

  const { typingAgents, processingAgents, agentActivity, setProcessingAgents } =
    useChannelSocket(
      {
        selectedChannelName: selectedChannel?.name,
        onAppendMessage: handleAppendMessage,
        onUnreadIncrement: handleUnreadIncrement,
      },
      mapRawMessages,
    );

  const handleChannelSelect = useCallback((channel: Channel) => {
    setSelectedChannel(channel);
    resetForChannel();
    setProcessingAgents([]);
    setThreadParent(null);
    setLoading(true);
    loadMessages(channel.name, INITIAL_MESSAGE_PAGE_SIZE).finally(() => setLoading(false));
    // Zero out unread/mention counts locally
    setChannels((prev) =>
      prev.map((ch) =>
        ch.id === channel.id ? { ...ch, unreadCount: 0, mentionCount: 0 } : ch
      )
    );
    // Mark read on server — fire and forget
    api.channels.markRead(channel.name).catch(() => {});
  }, [loadMessages, resetForChannel, setLoading, setProcessingAgents]);

  const handleBrowseJoin = (channelName: string) => {
    setBrowserOpen(false);
    const channel = channels.find((c) => c.name === channelName);
    if (channel) handleChannelSelect(channel);
  };

  const navigateChannel = useCallback(
    (direction: 1 | -1) => {
      const idx = channels.findIndex((c) => c.name === selectedChannel?.name);
      const next = channels[idx + direction];
      if (next) handleChannelSelect(next);
    },
    [channels, selectedChannel, handleChannelSelect],
  );

  useKeyboardShortcuts([
    {
      key: 'k',
      ctrl: true,
      handler: () => setSearchOpen((prev) => !prev),
    },
    {
      key: 'Escape',
      handler: () => {
        setSearchOpen(false);
        setThreadParent(null);
      },
      ignoreWhenEditing: false,
    },
    {
      key: 'ArrowUp',
      alt: true,
      handler: () => navigateChannel(-1),
    },
    {
      key: 'ArrowDown',
      alt: true,
      handler: () => navigateChannel(1),
    },
    {
      key: '?',
      handler: () => setHelpOpen(true),
    },
  ]);

  const threadReplies = threadParent
    ? messages.filter((m) => m.parentId === threadParent.id)
    : [];

  const handleJumpToMessage = (channelName: string, messageId: string) => {
    const channel = channels.find((c) => c.name === channelName);
    if (channel && channel.name !== selectedChannel?.name) {
      handleChannelSelect(channel);
    }
    setScrollTarget(messageId);
    setSearchOpen(false);
  };

  const handleReply = (message: Message) => setThreadParent(message);
  const handleCloseThread = () => setThreadParent(null);
  const handleThreadSend = async (content: string) => {
    if (!selectedChannel || !threadParent) return;
    const optimisticReply: Message = {
      id: `optimistic-${crypto.randomUUID()}`,
      channelId: selectedChannel.name,
      author: 'operator',
      displayAuthor: getOperatorDisplayName(),
      isAgent: false,
      isSystem: false,
      timestamp: formatMessageTime(new Date().toISOString()),
      rawTimestamp: new Date().toISOString(),
      content,
      flag: null,
      parentId: threadParent.id,
    };
    setMessages((prev) => [...prev, optimisticReply]);

    const agentMembers = selectedChannel.members.filter((m) => !SYSTEM_SENDERS.has(m) && m !== 'operator' && m !== '_operator');
    if (agentMembers.length > 0) {
      setProcessingAgents((prev) => {
        const next = new Set(prev);
        agentMembers.forEach((a) => next.add(a));
        return [...next];
      });
    }

    try {
      await api.channels.send(selectedChannel.name, content, threadParent.id);
      await loadMessages(selectedChannel.name);
    } catch (err) {
      console.error('handleThreadSend error:', err);
      setMessages((prev) => prev.filter((m) => m.id !== optimisticReply.id));
      if (agentMembers.length > 0) {
        setProcessingAgents((prev) => prev.filter((a) => !agentMembers.includes(a)));
      }
    }
  };

  const handleEdit = async (message: Message, newContent: string) => {
    if (!selectedChannel) return;
    hookHandleEdit(selectedChannel.name, message, newContent);
  };

  const handleDelete = async (message: Message) => {
    if (!selectedChannel) return;
    hookHandleDelete(selectedChannel.name, message);
  };

  const handleReact = async (message: Message, emoji: string) => {
    if (!selectedChannel) return;
    hookHandleReact(selectedChannel.name, message, emoji);
  };

  const handleUnreact = async (message: Message, emoji: string) => {
    if (!selectedChannel) return;
    hookHandleUnreact(selectedChannel.name, message, emoji);
  };

  const handleSend = async (
    content: string,
    flags?: { decision?: boolean; blocker?: boolean; question?: boolean },
  ) => {
    if (!selectedChannel) return;
    // Optimistic append
    const optimisticMsg: Message = {
      id: `optimistic-${crypto.randomUUID()}`,
      channelId: selectedChannel.name,
      author: 'operator',
      displayAuthor: getOperatorDisplayName(),
      isAgent: false,
      isSystem: false,
      timestamp: formatMessageTime(new Date().toISOString()),
      rawTimestamp: new Date().toISOString(),
      content,
      flag: flags?.decision ? 'DECISION' : flags?.blocker ? 'BLOCKER' : flags?.question ? 'QUESTION' : null,
    };
    setMessages((prev) => [...prev, optimisticMsg]);

    // Optimistically show processing indicator for agents likely to respond.
    // DM channels: all agent members. Other channels: only @mentioned agents.
    const isDM = selectedChannel.name.startsWith('dm-');
    const agentMembers = selectedChannel.members.filter((m) => !SYSTEM_SENDERS.has(m) && m !== 'operator' && m !== '_operator');
    const respondingAgents = isDM
      ? agentMembers
      : agentMembers.filter((a) => content.includes(`@${a}`));
    if (respondingAgents.length > 0) {
      setProcessingAgents((prev) => {
        const next = new Set(prev);
        respondingAgents.forEach((a) => next.add(a));
        return [...next];
      });
    }

    try {
      await api.channels.send(selectedChannel.name, content, undefined, flags);
      // Sync to get server-assigned ID and any concurrent messages
      await loadMessages(selectedChannel.name);
    } catch (err) {
      console.error('handleSend error:', err);
      // Roll back optimistic message on failure
      setMessages((prev) => prev.filter((m) => m.id !== optimisticMsg.id));
      // Roll back optimistic processing indicators
      if (agentMembers.length > 0) {
        setProcessingAgents((prev) => prev.filter((a) => !agentMembers.includes(a)));
      }
    }
  };

  return (
    <>
      <div className="flex h-full min-h-0">
        <ChannelSidebar
          channels={channels}
          selectedChannel={selectedChannel}
          onSelect={(ch) => { handleChannelSelect(ch); setMobileSidebarOpen(false); }}
          dmStatuses={agentStatuses}
          onBrowseChannels={() => setBrowserOpen(true)}
          onCreateChannel={() => setCreateChannelOpen(true)}
          mobileOpen={mobileSidebarOpen}
          onMobileClose={() => setMobileSidebarOpen(false)}
        />
        {selectedChannel ? (
          <>
            <MessageArea
              key={selectedChannel.id}
              channel={selectedChannel}
              messages={messages}
              loading={loading}
              onSend={handleSend}
              typingAgents={typingAgents}
              processingAgents={processingAgents}
              agentActivity={agentActivity}
              onReply={handleReply}
              onEdit={handleEdit}
              onDelete={handleDelete}
              onReact={handleReact}
              onUnreact={handleUnreact}
              scrollToMessageId={scrollTarget}
              hasMore={hasMore}
              onLoadMore={() => selectedChannel && loadMoreMessages(selectedChannel.name)}
              loadingMore={loadingMore}
              onOpenSidebar={isMobile ? () => setMobileSidebarOpen(true) : undefined}
              onAgentClick={handleAgentClick}
              headerActions={
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setSearchOpen((prev) => !prev)}
                  aria-label="Toggle search"
                  className="text-muted-foreground hover:text-accent-foreground"
                >
                  <Search className="w-4 h-4" />
                </Button>
              }
            />
            {threadParent && (
              <ThreadPanel
                parentMessage={threadParent}
                replies={threadReplies}
                onClose={handleCloseThread}
                onSend={handleThreadSend}
              />
            )}
            {searchOpen && (
              <SearchPanel
                onClose={() => setSearchOpen(false)}
                onJumpToMessage={handleJumpToMessage}
              />
            )}
          </>
        ) : (
          <div className="flex-1 flex items-center justify-center text-muted-foreground text-sm">
            {loading ? 'Loading...' : 'No channels available'}
          </div>
        )}
      </div>

      <ChannelBrowser
        open={browserOpen}
        onOpenChange={setBrowserOpen}
        onJoinChannel={handleBrowseJoin}
      />

      <CreateChannelDialog
        open={createChannelOpen}
        onOpenChange={setCreateChannelOpen}
        onCreated={() => { setCreateChannelOpen(false); loadChannels(); }}
      />

      <HelpDialog open={helpOpen} onOpenChange={setHelpOpen} />

      <AgentDetailSheet
        agentName={agentDetailName}
        agent={agentDetail}
        budget={agentBudget}
        onClose={() => setAgentDetailName(null)}
        onMessageAgent={(dmChannelName) => {
          const dmChannel = channels.find((c) => c.name === dmChannelName);
          if (dmChannel) handleChannelSelect(dmChannel);
        }}
      />
    </>
  );
}
