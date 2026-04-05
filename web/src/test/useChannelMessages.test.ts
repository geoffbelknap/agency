import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';

// Mock the api module
vi.mock('../app/lib/api', () => ({
  api: {
    channels: {
      read: vi.fn(),
      edit: vi.fn(),
      delete: vi.fn(),
      react: vi.fn(),
      unreact: vi.fn(),
    },
  },
}));

vi.mock('../app/lib/time', () => ({
  formatMessageTime: (ts: string) => ts,
}));

// useChannelMessages re-exports SYSTEM_SENDERS from useChannelSocket
vi.mock('../app/hooks/useChannelSocket', () => ({
  SYSTEM_SENDERS: new Set(['_platform', '_system']),
}));

import { useChannelMessages, INITIAL_MESSAGE_PAGE_SIZE, MESSAGE_PAGE_SIZE } from '../app/hooks/useChannelMessages';
import { api } from '../app/lib/api';
import type { Message } from '../app/types';
import type { RawMessage } from '../app/lib/api';

const mockReadFn = vi.mocked(api.channels.read);
const mockEditFn = vi.mocked(api.channels.edit);
const mockDeleteFn = vi.mocked(api.channels.delete);
const mockReactFn = vi.mocked(api.channels.react);
const mockUnreactFn = vi.mocked(api.channels.unreact);

function makeRawMessage(overrides: Partial<RawMessage> = {}): RawMessage {
  return {
    id: 'msg-1',
    timestamp: '2024-01-01T00:00:00Z',
    author: 'alice',
    content: 'hello',
    ...overrides,
  };
}

describe('useChannelMessages', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockReadFn.mockResolvedValue([]);
    mockEditFn.mockResolvedValue({ ok: true });
    mockDeleteFn.mockResolvedValue({ ok: true });
    mockReactFn.mockResolvedValue({ ok: true });
    mockUnreactFn.mockResolvedValue({ ok: true });
  });

  it('initializes with empty messages and default state', () => {
    const { result } = renderHook(() => useChannelMessages());

    expect(result.current.messages).toEqual([]);
    expect(result.current.loading).toBe(true);
    expect(result.current.hasMore).toBe(false);
    expect(result.current.loadingMore).toBe(false);
    expect(result.current.messageLimit).toBe(INITIAL_MESSAGE_PAGE_SIZE);
  });

  describe('mapRawMessages', () => {
    it('maps a raw message from a regular user', () => {
      const { result } = renderHook(() => useChannelMessages());
      const raw = makeRawMessage({ id: 'msg-1', author: 'alice', content: 'hi' });

      const mapped = result.current.mapRawMessages([raw], 'general');

      expect(mapped).toHaveLength(1);
      expect(mapped[0]).toMatchObject({
        id: 'msg-1',
        channelId: 'general',
        author: 'alice',
        displayAuthor: 'alice',
        isAgent: true,
        isSystem: false,
        content: 'hi',
        flag: null,
      });
    });

    it('maps operator messages correctly', () => {
      const { result } = renderHook(() => useChannelMessages());
      const raw = makeRawMessage({ author: 'operator', content: 'ok' });

      const mapped = result.current.mapRawMessages([raw], 'general');

      expect(mapped[0].isAgent).toBe(false);
      expect(mapped[0].isSystem).toBe(false);
      expect(mapped[0].displayAuthor).toBe('operator');
    });

    it('maps _operator to displayAuthor "operator"', () => {
      const { result } = renderHook(() => useChannelMessages());
      const raw = makeRawMessage({ author: '_operator', content: 'msg' });

      const mapped = result.current.mapRawMessages([raw], 'general');

      expect(mapped[0].displayAuthor).toBe('operator');
      expect(mapped[0].isAgent).toBe(false);
    });

    it('marks system senders as isSystem', () => {
      const { result } = renderHook(() => useChannelMessages());
      const raw = makeRawMessage({ author: '_platform', content: 'sys msg' });

      const mapped = result.current.mapRawMessages([raw], 'general');

      expect(mapped[0].isSystem).toBe(true);
      expect(mapped[0].isAgent).toBe(false);
      expect(mapped[0].displayAuthor).toBe('Agency Platform');
    });

    it('maps DECISION flag', () => {
      const { result } = renderHook(() => useChannelMessages());
      const raw = makeRawMessage({ flags: { decision: true } });

      const mapped = result.current.mapRawMessages([raw], 'general');

      expect(mapped[0].flag).toBe('DECISION');
    });

    it('maps BLOCKER flag', () => {
      const { result } = renderHook(() => useChannelMessages());
      const raw = makeRawMessage({ flags: { blocker: true } });

      const mapped = result.current.mapRawMessages([raw], 'general');

      expect(mapped[0].flag).toBe('BLOCKER');
    });

    it('maps QUESTION flag', () => {
      const { result } = renderHook(() => useChannelMessages());
      const raw = makeRawMessage({ flags: { question: true } });

      const mapped = result.current.mapRawMessages([raw], 'general');

      expect(mapped[0].flag).toBe('QUESTION');
    });

    it('maps reply_to to parentId', () => {
      const { result } = renderHook(() => useChannelMessages());
      const raw = makeRawMessage({ reply_to: 'parent-id' });

      const mapped = result.current.mapRawMessages([raw], 'general');

      expect(mapped[0].parentId).toBe('parent-id');
    });

    it('merges reactions into metadata', () => {
      const { result } = renderHook(() => useChannelMessages());
      const reactions = [{ emoji: '👍', author: 'bob' }];
      const raw = makeRawMessage({ reactions, metadata: { foo: 'bar' } });

      const mapped = result.current.mapRawMessages([raw], 'general');

      expect(mapped[0].metadata).toMatchObject({ foo: 'bar', reactions });
    });
  });

  describe('loadMessages', () => {
    it('fetches messages and sets state', async () => {
      const raw = makeRawMessage({ id: 'msg-1', author: 'alice', content: 'hello' });
      mockReadFn.mockResolvedValue([raw]);

      const { result } = renderHook(() => useChannelMessages());

      await act(async () => {
        await result.current.loadMessages('general');
      });

      expect(mockReadFn).toHaveBeenCalledWith('general', INITIAL_MESSAGE_PAGE_SIZE);
      expect(result.current.messages).toHaveLength(1);
      expect(result.current.messages[0].id).toBe('msg-1');
    });

    it('sets hasMore to true when data length equals limit', async () => {
      const raws = Array.from({ length: INITIAL_MESSAGE_PAGE_SIZE }, (_, i) =>
        makeRawMessage({ id: `msg-${i}` })
      );
      mockReadFn.mockResolvedValue(raws);

      const { result } = renderHook(() => useChannelMessages());

      await act(async () => {
        await result.current.loadMessages('general', INITIAL_MESSAGE_PAGE_SIZE);
      });

      expect(result.current.hasMore).toBe(true);
    });

    it('sets hasMore to false when data length is less than limit', async () => {
      mockReadFn.mockResolvedValue([makeRawMessage()]);

      const { result } = renderHook(() => useChannelMessages());

      await act(async () => {
        await result.current.loadMessages('general', INITIAL_MESSAGE_PAGE_SIZE);
      });

      expect(result.current.hasMore).toBe(false);
    });

    it('uses explicit limit over internal messageLimit', async () => {
      mockReadFn.mockResolvedValue([]);

      const { result } = renderHook(() => useChannelMessages());

      await act(async () => {
        await result.current.loadMessages('general', 10);
      });

      expect(mockReadFn).toHaveBeenCalledWith('general', 10);
    });

    it('handles fetch errors gracefully', async () => {
      mockReadFn.mockRejectedValue(new Error('network error'));

      const { result } = renderHook(() => useChannelMessages());

      await act(async () => {
        await result.current.loadMessages('general');
      });

      // Should not throw, messages stay empty
      expect(result.current.messages).toEqual([]);
    });
  });

  describe('loadMoreMessages', () => {
    it('fetches more messages and increases limit', async () => {
      // First set hasMore = true by loading a full page
      const raws = Array.from({ length: INITIAL_MESSAGE_PAGE_SIZE }, (_, i) =>
        makeRawMessage({ id: `msg-${i}` })
      );
      mockReadFn.mockResolvedValue(raws);

      const { result } = renderHook(() => useChannelMessages());

      await act(async () => {
        await result.current.loadMessages('general', INITIAL_MESSAGE_PAGE_SIZE);
      });

      expect(result.current.hasMore).toBe(true);

      // Now load more
      const morePlusOriginal = Array.from({ length: INITIAL_MESSAGE_PAGE_SIZE + MESSAGE_PAGE_SIZE }, (_, i) =>
        makeRawMessage({ id: `msg-${i}` })
      );
      mockReadFn.mockResolvedValue(morePlusOriginal);

      await act(async () => {
        await result.current.loadMoreMessages('general');
      });

      expect(mockReadFn).toHaveBeenLastCalledWith('general', INITIAL_MESSAGE_PAGE_SIZE + MESSAGE_PAGE_SIZE);
      expect(result.current.messageLimit).toBe(INITIAL_MESSAGE_PAGE_SIZE + MESSAGE_PAGE_SIZE);
      expect(result.current.messages).toHaveLength(INITIAL_MESSAGE_PAGE_SIZE + MESSAGE_PAGE_SIZE);
    });

    it('does nothing when hasMore is false', async () => {
      mockReadFn.mockResolvedValue([makeRawMessage()]);

      const { result } = renderHook(() => useChannelMessages());

      await act(async () => {
        await result.current.loadMessages('general');
      });

      expect(result.current.hasMore).toBe(false);

      mockReadFn.mockClear();

      await act(async () => {
        await result.current.loadMoreMessages('general');
      });

      // Should not call read again
      expect(mockReadFn).not.toHaveBeenCalled();
    });

    it('does nothing when already loadingMore', async () => {
      // Need hasMore=true first
      const raws = Array.from({ length: INITIAL_MESSAGE_PAGE_SIZE }, (_, i) =>
        makeRawMessage({ id: `msg-${i}` })
      );
      mockReadFn.mockResolvedValue(raws);

      const { result } = renderHook(() => useChannelMessages());

      await act(async () => {
        await result.current.loadMessages('general', INITIAL_MESSAGE_PAGE_SIZE);
      });

      // Slow read to test concurrent guard
      let resolveRead!: (v: any) => void;
      const slowPromise = new Promise((r) => { resolveRead = r; });
      mockReadFn.mockReturnValue(slowPromise as any);

      // Start first loadMore (doesn't resolve yet)
      act(() => {
        result.current.loadMoreMessages('general');
      });

      mockReadFn.mockClear();

      // Second call should be a no-op
      await act(async () => {
        await result.current.loadMoreMessages('general');
      });

      expect(mockReadFn).not.toHaveBeenCalled();

      // Resolve the first one
      resolveRead([]);
    });
  });

  describe('handleEdit', () => {
    it('calls api.channels.edit then reloads messages', async () => {
      mockReadFn.mockResolvedValue([]);
      const { result } = renderHook(() => useChannelMessages());

      const msg: Message = {
        id: 'msg-1',
        channelId: 'general',
        author: 'alice',
        displayAuthor: 'alice',
        isAgent: true,
        isSystem: false,
        timestamp: '2024-01-01T00:00:00Z',
        content: 'old',
        flag: null,
      };

      await act(async () => {
        await result.current.handleEdit('general', msg, 'new content');
      });

      expect(mockEditFn).toHaveBeenCalledWith('general', 'msg-1', 'new content');
      expect(mockReadFn).toHaveBeenCalled();
    });
  });

  describe('handleDelete', () => {
    it('calls api.channels.delete then reloads messages', async () => {
      mockReadFn.mockResolvedValue([]);
      const { result } = renderHook(() => useChannelMessages());

      const msg: Message = {
        id: 'msg-1',
        channelId: 'general',
        author: 'alice',
        displayAuthor: 'alice',
        isAgent: true,
        isSystem: false,
        timestamp: '2024-01-01T00:00:00Z',
        content: 'bye',
        flag: null,
      };

      await act(async () => {
        await result.current.handleDelete('general', msg);
      });

      expect(mockDeleteFn).toHaveBeenCalledWith('general', 'msg-1');
      expect(mockReadFn).toHaveBeenCalled();
    });
  });

  describe('handleReact', () => {
    it('calls api.channels.react then reloads messages', async () => {
      mockReadFn.mockResolvedValue([]);
      const { result } = renderHook(() => useChannelMessages());

      const msg: Message = {
        id: 'msg-1',
        channelId: 'general',
        author: 'alice',
        displayAuthor: 'alice',
        isAgent: true,
        isSystem: false,
        timestamp: '2024-01-01T00:00:00Z',
        content: 'hi',
        flag: null,
      };

      await act(async () => {
        await result.current.handleReact('general', msg, '👍');
      });

      expect(mockReactFn).toHaveBeenCalledWith('general', 'msg-1', '👍');
      expect(mockReadFn).toHaveBeenCalled();
    });
  });

  describe('handleUnreact', () => {
    it('calls api.channels.unreact then reloads messages', async () => {
      mockReadFn.mockResolvedValue([]);
      const { result } = renderHook(() => useChannelMessages());

      const msg: Message = {
        id: 'msg-1',
        channelId: 'general',
        author: 'alice',
        displayAuthor: 'alice',
        isAgent: true,
        isSystem: false,
        timestamp: '2024-01-01T00:00:00Z',
        content: 'hi',
        flag: null,
      };

      await act(async () => {
        await result.current.handleUnreact('general', msg, '👍');
      });

      expect(mockUnreactFn).toHaveBeenCalledWith('general', 'msg-1', '👍');
      expect(mockReadFn).toHaveBeenCalled();
    });
  });

  describe('appendMessage', () => {
    it('appends a new message to the list', () => {
      const { result } = renderHook(() => useChannelMessages());

      const msg: Message = {
        id: 'new-msg',
        channelId: 'general',
        author: 'bob',
        displayAuthor: 'bob',
        isAgent: true,
        isSystem: false,
        timestamp: '2024-01-01T00:00:00Z',
        content: 'hi there',
        flag: null,
      };

      act(() => {
        result.current.appendMessage(msg);
      });

      expect(result.current.messages).toHaveLength(1);
      expect(result.current.messages[0].id).toBe('new-msg');
    });

    it('deduplicates messages with the same id', () => {
      const { result } = renderHook(() => useChannelMessages());

      const msg: Message = {
        id: 'dup-msg',
        channelId: 'general',
        author: 'bob',
        displayAuthor: 'bob',
        isAgent: true,
        isSystem: false,
        timestamp: '2024-01-01T00:00:00Z',
        content: 'hi',
        flag: null,
      };

      act(() => {
        result.current.appendMessage(msg);
        result.current.appendMessage(msg);
      });

      expect(result.current.messages).toHaveLength(1);
    });
  });

  describe('resetForChannel', () => {
    it('clears messages and resets messageLimit', async () => {
      mockReadFn.mockResolvedValue([makeRawMessage()]);

      const { result } = renderHook(() => useChannelMessages());

      await act(async () => {
        await result.current.loadMessages('general');
      });

      expect(result.current.messages).toHaveLength(1);

      act(() => {
        result.current.resetForChannel();
      });

      expect(result.current.messages).toEqual([]);
      expect(result.current.messageLimit).toBe(INITIAL_MESSAGE_PAGE_SIZE);
    });
  });
});
