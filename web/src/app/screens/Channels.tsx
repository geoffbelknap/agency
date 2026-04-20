// src/app/screens/Channels.tsx
import { useState, useEffect, useCallback, useMemo } from 'react';
import { useParams } from 'react-router';
import { useIsMobile } from '../components/ui/use-mobile';
import { ChannelSidebar } from '../components/chat/ChannelSidebar';
import { ChannelBrowser } from '../components/chat/ChannelBrowser';
import { CreateChannelDialog } from '../components/chat/CreateChannelDialog';
import { MessageArea } from '../components/chat/MessageArea';
import { ThreadPanel } from '../components/chat/ThreadPanel';
import { SearchPanel } from '../components/chat/SearchPanel';
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts';
import { useChannelSocket, SYSTEM_SENDERS } from '../hooks/useChannelSocket';
import { useChannelMessages, INITIAL_MESSAGE_PAGE_SIZE } from '../hooks/useChannelMessages';
import { HelpDialog } from './channels/HelpDialog';
import { AgentDetailSheet } from './channels/AgentDetailSheet';
import { api, type RawChannel, type RawAgent, type RawBudgetResponse } from '../lib/api';
import { fetchOperatorDisplayName, getOperatorDisplayName } from '../lib/operator';
import { formatMessageTime } from '../lib/time';
import type { Channel, Message } from '../types';

type ChannelFilter = 'all' | 'dms' | 'channels';
const LAST_CHANNEL_KEY = 'agency.channels.lastSelected';

function isDmChannel(channel: Channel): boolean {
  return channel.type === 'dm' || channel.name.startsWith('dm-');
}

function isInfrastructureChannel(channel: Channel): boolean {
  return channel.name.startsWith('_');
}

function filterChannels(channels: Channel[], filter: ChannelFilter, showInfrastructure: boolean): Channel[] {
  return channels
    .filter((channel) => showInfrastructure || !isInfrastructureChannel(channel))
    .filter((channel) => {
      if (filter === 'dms') return isDmChannel(channel);
      if (filter === 'channels') return !isDmChannel(channel);
      return true;
    });
}

function readLastChannelName(): string | null {
  if (typeof window === 'undefined') return null;
  try {
    return window.localStorage.getItem(LAST_CHANNEL_KEY);
  } catch {
    return null;
  }
}

function writeLastChannelName(name: string) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(LAST_CHANNEL_KEY, name);
  } catch {
    // Channel recency is optional.
  }
}

export function Channels() {
  const { name: urlChannelName } = useParams<{ name: string }>();
  const isMobile = useIsMobile();
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);
  const [channels, setChannels] = useState<Channel[]>([]);
  const [selectedChannel, setSelectedChannel] = useState<Channel | null>(null);
  const [threadParent, setThreadParent] = useState<Message | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [scrollTarget, setScrollTarget] = useState<string | undefined>(undefined);
  const [helpOpen, setHelpOpen] = useState(false);
  const [createChannelOpen, setCreateChannelOpen] = useState(false);
  const [browserOpen, setBrowserOpen] = useState(false);
  const [showInactive, setShowInactive] = useState(false);
  const [channelFilter, setChannelFilter] = useState<ChannelFilter>('all');
  const [showInfrastructureChannels, setShowInfrastructureChannels] = useState(false);

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
  }, [showInactive]);

  const loadChannels = useCallback(async () => {
    try {
      const data = await api.channels.list({ includeArchived: showInactive, includeUnavailable: showInactive });
      const mapped: Channel[] = data.map((c: RawChannel) => ({
        id: c.name,
        name: c.name,
        topic: c.topic || '',
        type: c.type,
        state: c.state,
        availability: c.availability,
        unreadCount: c.unread || 0,
        mentionCount: c.mentions || 0,
        lastActivity: '',
        members: (c.members || []).filter((m: string) => m !== '_operator'),
      })).filter((channel) => showInactive || channel.state !== 'archived');
      setChannels(mapped);
      return mapped;
    } catch (err) {
      console.error('loadChannels error:', err);
      return [];
    }
  }, [showInactive]);

  useEffect(() => {
    let active = true;
    setLoading(true);
    fetchOperatorDisplayName();
    loadChannels().then((mapped) => {
      if (!active) return;
      if (mapped.length > 0) {
        if (urlChannelName && urlChannelName.startsWith('_')) setShowInfrastructureChannels(true);
        const defaultPool = mapped.filter((channel) => urlChannelName?.startsWith('_') || !isInfrastructureChannel(channel));
        const fallbackPool = defaultPool.length > 0 ? defaultPool : mapped;
        const lastChannelName = readLastChannelName();
        const defaultChannel =
          (lastChannelName ? fallbackPool.find((c) => c.name === lastChannelName) : null) ||
          fallbackPool.find((c) => c.name === 'general') ||
          fallbackPool.find((c) => !isDmChannel(c)) ||
          fallbackPool.find(isDmChannel) ||
          fallbackPool[0];
        const target = urlChannelName
          ? mapped.find((c) => c.name === urlChannelName) || defaultChannel
          : defaultChannel;
        setSelectedChannel(target);
        writeLastChannelName(target.name);
        loadMessages(target.name, INITIAL_MESSAGE_PAGE_SIZE).finally(() => {
          if (active) setLoading(false);
        });
      } else {
        setLoading(false);
      }
    });
    return () => { active = false; };
  }, [urlChannelName, showInactive]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleAppendMessage = useCallback((msg: Message) => {
    appendMessage(msg);
  }, [appendMessage]);

  const handleUnreadIncrement = useCallback((channelName: string) => {
    setChannels((prev) =>
      prev.map((ch) =>
        ch.name === channelName
          ? { ...ch, unreadCount: ch.unreadCount + 1 }
          : ch,
      ),
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
    writeLastChannelName(channel.name);
    resetForChannel();
    setProcessingAgents([]);
    setThreadParent(null);
    setLoading(true);
    loadMessages(channel.name, INITIAL_MESSAGE_PAGE_SIZE).finally(() => setLoading(false));
    setChannels((prev) =>
      prev.map((ch) =>
        ch.id === channel.id ? { ...ch, unreadCount: 0, mentionCount: 0 } : ch,
      ),
    );
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
    { key: 'k', ctrl: true, handler: () => setSearchOpen((prev) => !prev) },
    {
      key: 'Escape',
      handler: () => {
        setSearchOpen(false);
        setThreadParent(null);
      },
      ignoreWhenEditing: false,
    },
    { key: 'ArrowUp', alt: true, handler: () => navigateChannel(-1) },
    { key: 'ArrowDown', alt: true, handler: () => navigateChannel(1) },
    { key: '?', handler: () => setHelpOpen(true) },
  ]);

  const threadReplies = threadParent
    ? messages.filter((m) => m.parentId === threadParent.id)
    : [];

  const visibleChannels = useMemo(() => filterChannels(channels, channelFilter, showInfrastructureChannels), [channels, channelFilter, showInfrastructureChannels]);

  const hiddenInfrastructureCount = channels.filter(isInfrastructureChannel).length;
  const countableChannels = channels.filter((channel) => showInfrastructureChannels || !isInfrastructureChannel(channel));
  const dmCount = countableChannels.filter(isDmChannel).length;
  const sharedCount = countableChannels.length - dmCount;
  const unreadTotal = countableChannels.reduce((total, channel) => total + (channel.unreadCount || 0), 0);

  const handleFilterChange = (next: ChannelFilter) => {
    setChannelFilter(next);
    const nextVisible = filterChannels(channels, next, showInfrastructureChannels);
    if (selectedChannel && nextVisible.some((channel) => channel.id === selectedChannel.id)) return;
    if (nextVisible[0]) handleChannelSelect(nextVisible[0]);
  };

  const handleInfrastructureToggle = () => {
    const nextShow = !showInfrastructureChannels;
    setShowInfrastructureChannels(nextShow);
    const nextVisible = filterChannels(channels, channelFilter, nextShow);
    if (selectedChannel && nextVisible.some((channel) => channel.id === selectedChannel.id)) return;
    if (nextVisible[0]) handleChannelSelect(nextVisible[0]);
  };

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
      const sent = await api.channels.send(selectedChannel.name, content, threadParent.id);
      const mapped = mapRawMessages([sent], selectedChannel.name)[0];
      if (mapped) {
        setMessages((prev) => [
          ...prev.filter((m) => m.id !== optimisticReply.id && m.id !== mapped.id),
          mapped,
        ]);
      }
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
      const sent = await api.channels.send(selectedChannel.name, content, undefined, flags);
      const mapped = mapRawMessages([sent], selectedChannel.name)[0];
      if (mapped) {
        setMessages((prev) => [
          ...prev.filter((m) => m.id !== optimisticMsg.id && m.id !== mapped.id),
          mapped,
        ]);
      }
    } catch (err) {
      console.error('handleSend error:', err);
      setMessages((prev) => prev.filter((m) => m.id !== optimisticMsg.id));
      if (agentMembers.length > 0) {
        setProcessingAgents((prev) => prev.filter((a) => !agentMembers.includes(a)));
      }
    }
  };

  return (
    <>
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0, height: '100%', background: 'var(--warm)' }}>
        <div style={{ minHeight: 58, padding: '8px 16px', borderBottom: '0.5px solid var(--ink-hairline)', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 14, flexWrap: 'wrap', background: 'var(--warm)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap', minWidth: 0 }}>
            <div className="eyebrow" style={{ fontSize: 9 }}>Channels</div>
            <div style={{ display: 'inline-flex', padding: 2, gap: 2, background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 999 }}>
              {[
                ['all', 'All'],
                ['dms', 'DMs'],
                ['channels', 'Channels'],
              ].map(([id, label]) => (
                <button
                  key={id}
                  type="button"
                  onClick={() => handleFilterChange(id as ChannelFilter)}
                  style={{ padding: '5px 13px', border: 0, borderRadius: 999, background: channelFilter === id ? 'var(--ink)' : 'transparent', color: channelFilter === id ? 'var(--warm)' : 'var(--ink-mid)', fontFamily: 'var(--sans)', fontSize: 12, cursor: 'pointer' }}
                >
                  {label}
                </button>
              ))}
            </div>
            <div className="mono" style={{ fontSize: 10, color: 'var(--ink-faint)', whiteSpace: 'nowrap' }}>
              {countableChannels.length.toLocaleString()} conversations · {dmCount.toLocaleString()} DMs · {sharedCount.toLocaleString()} channels{unreadTotal > 0 ? ` · ${unreadTotal.toLocaleString()} unread` : ''}{!showInfrastructureChannels && hiddenInfrastructureCount > 0 ? ` · ${hiddenInfrastructureCount.toLocaleString()} infra hidden` : ''}
            </div>
          </div>
          {hiddenInfrastructureCount > 0 && (
            <button
              type="button"
              onClick={handleInfrastructureToggle}
              style={{ padding: '5px 11px', border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 999, background: showInfrastructureChannels ? 'var(--ink)' : 'var(--warm)', color: showInfrastructureChannels ? 'var(--warm)' : 'var(--ink-mid)', fontFamily: 'var(--sans)', fontSize: 12, cursor: 'pointer', whiteSpace: 'nowrap' }}
            >
              {showInfrastructureChannels ? 'Hide infra' : 'Show infra'}
            </button>
          )}
        </div>

        <div style={{ flex: 1, display: 'flex', minHeight: 0, background: 'var(--warm)' }}>
            <ChannelSidebar
              channels={visibleChannels}
              selectedChannel={selectedChannel}
              onSelect={(ch) => { handleChannelSelect(ch); setMobileSidebarOpen(false); }}
              onBrowseChannels={() => setBrowserOpen(true)}
              onCreateChannel={() => setCreateChannelOpen(true)}
              showInactive={showInactive}
              onToggleInactive={() => setShowInactive((prev) => !prev)}
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
              <div className="flex flex-1 items-center justify-center p-6">
                {loading ? (
                  <div style={{ fontSize: 13, color: 'var(--ink-faint)' }}>Loading...</div>
                ) : (
                  <div style={{ width: '100%', maxWidth: 420, border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', padding: '28px 24px', textAlign: 'center' }}>
                    <h3 style={{ margin: 0, fontFamily: 'var(--font-display)', fontSize: 24, fontWeight: 400, color: 'var(--ink)' }}>No channels yet</h3>
                    <p style={{ margin: '10px 0 0', fontSize: 13, color: 'var(--ink-mid)' }}>
                      Start with a direct message to an agent or create a shared channel for operator coordination.
                    </p>
                  </div>
                )}
              </div>
            )}
      </div>
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
