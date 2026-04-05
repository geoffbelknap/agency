// src/app/hooks/useChannelMessages.ts
import { useState, useCallback } from 'react';
import { toast } from 'sonner';
import { api, type RawMessage } from '../lib/api';
import { formatMessageTime } from '../lib/time';
import { getOperatorDisplayName } from '../lib/operator';
import { SYSTEM_SENDERS } from './useChannelSocket';
import type { Message } from '../types';

export const INITIAL_MESSAGE_PAGE_SIZE = 20;
export const MESSAGE_PAGE_SIZE = 50;

export interface UseChannelMessagesResult {
  messages: Message[];
  loading: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  messageLimit: number;
  setMessages: React.Dispatch<React.SetStateAction<Message[]>>;
  setLoading: React.Dispatch<React.SetStateAction<boolean>>;
  mapRawMessages: (data: RawMessage[], channelName: string) => Message[];
  loadMessages: (channelName: string, limit?: number) => Promise<void>;
  loadMoreMessages: (channelName: string) => Promise<void>;
  handleEdit: (channelName: string, message: Message, newContent: string) => Promise<void>;
  handleDelete: (channelName: string, message: Message) => Promise<void>;
  handleReact: (channelName: string, message: Message, emoji: string) => Promise<void>;
  handleUnreact: (channelName: string, message: Message, emoji: string) => Promise<void>;
  appendMessage: (msg: Message) => void;
  resetForChannel: () => void;
}

export function useChannelMessages(): UseChannelMessagesResult {
  const [messages, setMessages] = useState<Message[]>([]);
  const [loading, setLoading] = useState(true);
  const [hasMore, setHasMore] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [messageLimit, setMessageLimit] = useState(INITIAL_MESSAGE_PAGE_SIZE);

  const mapRawMessages = useCallback((data: RawMessage[], channelName: string): Message[] =>
    data.map((m) => {
      const isSystem = SYSTEM_SENDERS.has(m.author);
      return {
        id: m.id || m.timestamp || crypto.randomUUID(),
        channelId: channelName,
        author: m.author,
        displayAuthor: isSystem ? 'Agency Platform' : m.author === '_operator' ? getOperatorDisplayName() : m.author,
        isAgent: !isSystem && m.author !== 'operator' && m.author !== '_operator',
        isSystem,
        timestamp: formatMessageTime(m.timestamp),
        rawTimestamp: m.timestamp,
        content: m.content,
        flag: m.flags?.decision
          ? 'DECISION' as const
          : m.flags?.blocker
          ? 'BLOCKER' as const
          : m.flags?.question
          ? 'QUESTION' as const
          : null,
        parentId: m.reply_to || undefined,
        metadata: { ...m.metadata, reactions: m.reactions } as Record<string, any>,
      };
    }), []);

  const loadMessages = useCallback(async (channelName: string, limit?: number) => {
    const effectiveLimit = limit ?? messageLimit;
    try {
      const data = (await api.channels.read(channelName, effectiveLimit)) ?? [];
      setMessages(mapRawMessages(data, channelName));
      setHasMore(data.length >= effectiveLimit);
    } catch (err) {
      console.error('loadMessages error:', err);
    }
  }, [mapRawMessages, messageLimit]);

  // NOTE: The gateway API currently lacks offset/cursor support, so pagination
  // works by increasing the limit and re-fetching all messages. This re-transfers
  // previously loaded messages each time. Migrate to cursor-based pagination
  // (e.g. `?before=<timestamp>`) when the gateway adds support.
  const loadMoreMessages = useCallback(async (channelName: string) => {
    if (loadingMore || !hasMore) return;
    setLoadingMore(true);
    const nextLimit = messageLimit + MESSAGE_PAGE_SIZE;
    try {
      const data = (await api.channels.read(channelName, nextLimit)) ?? [];
      const mapped = mapRawMessages(data, channelName);
      setMessages(mapped);
      setMessageLimit(nextLimit);
      setHasMore(data.length >= nextLimit);
    } catch (err) {
      console.error('loadMoreMessages error:', err);
    } finally {
      setLoadingMore(false);
    }
  }, [loadingMore, hasMore, messageLimit, mapRawMessages]);

  const handleEdit = useCallback(async (channelName: string, message: Message, newContent: string) => {
    try {
      await api.channels.edit(channelName, message.id, newContent);
      await loadMessages(channelName);
    } catch (err: any) {
      console.error('handleEdit error:', err);
      toast.error(err?.message || 'Failed to edit message');
    }
  }, [loadMessages]);

  const handleDelete = useCallback(async (channelName: string, message: Message) => {
    try {
      await api.channels.delete(channelName, message.id);
      await loadMessages(channelName);
    } catch (err: any) {
      console.error('handleDelete error:', err);
      toast.error(err?.message || 'Failed to delete message');
    }
  }, [loadMessages]);

  const handleReact = useCallback(async (channelName: string, message: Message, emoji: string) => {
    try {
      await api.channels.react(channelName, message.id, emoji);
      await loadMessages(channelName);
    } catch (err: any) {
      console.error('handleReact error:', err);
      toast.error(err?.message || 'Failed to add reaction');
    }
  }, [loadMessages]);

  const handleUnreact = useCallback(async (channelName: string, message: Message, emoji: string) => {
    try {
      await api.channels.unreact(channelName, message.id, emoji);
      await loadMessages(channelName);
    } catch (err: any) {
      console.error('handleUnreact error:', err);
      toast.error(err?.message || 'Failed to remove reaction');
    }
  }, [loadMessages]);

  const appendMessage = useCallback((msg: Message) => {
    setMessages((prev) => {
      // Deduplicate by id in case a background sync also fetched it
      if (prev.some((m) => m.id === msg.id)) return prev;
      return [...prev, msg];
    });
  }, []);

  const resetForChannel = useCallback(() => {
    setMessages([]);
    setMessageLimit(INITIAL_MESSAGE_PAGE_SIZE);
  }, []);

  return {
    messages,
    loading,
    hasMore,
    loadingMore,
    messageLimit,
    setMessages,
    setLoading,
    mapRawMessages,
    loadMessages,
    loadMoreMessages,
    handleEdit,
    handleDelete,
    handleReact,
    handleUnreact,
    appendMessage,
    resetForChannel,
  };
}
