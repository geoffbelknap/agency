import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';

// Mock the socket module before importing the hook
vi.mock('../app/lib/ws', () => ({
  socket: {
    on: vi.fn(() => vi.fn()), // returns unsubscribe fn
    onConnectionChange: vi.fn(() => vi.fn()),
    connect: vi.fn(),
    disconnect: vi.fn(),
    connected: false,
    gaveUp: false,
  },
}));

vi.mock('../app/lib/time', () => ({
  formatMessageTime: (ts: string) => ts,
}));

import { useChannelSocket } from '../app/hooks/useChannelSocket';
import { socket } from '../app/lib/ws';
import type { Message } from '../app/types';
import type { RawMessage } from '../app/lib/api';

const mockMapRawMessages = (data: RawMessage[], channelName: string): Message[] =>
  data.map((m) => ({
    id: m.id || m.timestamp || 'test-id',
    channelId: channelName,
    author: m.author,
    displayAuthor: m.author,
    isAgent: false,
    isSystem: false,
    timestamp: m.timestamp || '',
    rawTimestamp: m.timestamp || '',
    content: m.content,
    flag: null,
  }));

function makeOptions(overrides: Partial<Parameters<typeof useChannelSocket>[0]> = {}) {
  return {
    selectedChannelName: 'general',
    onAppendMessage: vi.fn(),
    onUnreadIncrement: vi.fn(),
    ...overrides,
  };
}

describe('useChannelSocket', () => {
  let socketOnMock: ReturnType<typeof vi.fn>;
  let socketOnConnectionChangeMock: ReturnType<typeof vi.fn>;
  // Map of event type -> registered handler
  const handlers: Record<string, (event: any) => void> = {};
  let connectionChangeHandler: ((connected: boolean) => void) | null = null;

  beforeEach(() => {
    vi.clearAllMocks();

    // Set up socket.on to capture handlers by event type
    socketOnMock = vi.mocked(socket.on);
    socketOnMock.mockImplementation((type: string, handler: (event: any) => void) => {
      handlers[type] = handler;
      return () => { delete handlers[type]; };
    });

    socketOnConnectionChangeMock = vi.mocked(socket.onConnectionChange);
    socketOnConnectionChangeMock.mockImplementation((handler: (connected: boolean) => void) => {
      connectionChangeHandler = handler;
      return () => { connectionChangeHandler = null; };
    });
  });

  afterEach(() => {
    // Clean up captured handlers
    for (const key in handlers) {
      delete handlers[key];
    }
    connectionChangeHandler = null;
  });

  it('returns initial state with socket.connected value', () => {
    vi.mocked(socket).connected = false;
    const { result } = renderHook(() =>
      useChannelSocket(makeOptions(), mockMapRawMessages)
    );

    expect(result.current.typingAgents).toEqual([]);
    expect(result.current.processingAgents).toEqual([]);
    expect(result.current.agentActivity).toEqual({});
    expect(result.current.wsConnected).toBe(false);
  });

  it('updates wsConnected when connection changes', () => {
    const { result } = renderHook(() =>
      useChannelSocket(makeOptions(), mockMapRawMessages)
    );

    act(() => {
      connectionChangeHandler?.(true);
    });

    expect(result.current.wsConnected).toBe(true);

    act(() => {
      connectionChangeHandler?.(false);
    });

    expect(result.current.wsConnected).toBe(false);
  });

  it('subscribes to onConnectionChange on mount and unsubscribes on unmount', () => {
    const { unmount } = renderHook(() =>
      useChannelSocket(makeOptions(), mockMapRawMessages)
    );

    expect(socketOnConnectionChangeMock).toHaveBeenCalledTimes(1);

    unmount();

    // After unmount, connectionChangeHandler should be unregistered
    expect(connectionChangeHandler).toBeNull();
  });

  it('calls onAppendMessage when a message arrives on the selected channel', () => {
    const onAppendMessage = vi.fn();
    renderHook(() =>
      useChannelSocket(makeOptions({ onAppendMessage }), mockMapRawMessages)
    );

    act(() => {
      handlers['message']?.({
        type: 'message',
        message: {
          id: 'msg-1',
          channel: 'general',
          author: 'alice',
          content: 'hello',
          timestamp: '2024-01-01T00:00:00Z',
        },
      });
    });

    expect(onAppendMessage).toHaveBeenCalledTimes(1);
    expect(onAppendMessage.mock.calls[0][0]).toMatchObject({
      id: 'msg-1',
      channelId: 'general',
      author: 'alice',
      content: 'hello',
    });
  });

  it('calls onUnreadIncrement for messages on other channels', () => {
    const onUnreadIncrement = vi.fn();
    renderHook(() =>
      useChannelSocket(makeOptions({ onUnreadIncrement }), mockMapRawMessages)
    );

    act(() => {
      handlers['message']?.({
        type: 'message',
        message: {
          id: 'msg-2',
          channel: 'other-channel',
          author: 'bob',
          content: 'hey',
          timestamp: '2024-01-01T00:00:00Z',
        },
      });
    });

    expect(onUnreadIncrement).toHaveBeenCalledWith('other-channel');
  });

  it('clears typing/processing indicators when agent sends a message', () => {
    const { result } = renderHook(() =>
      useChannelSocket(makeOptions(), mockMapRawMessages)
    );

    // First, put the agent in typing and processing state
    act(() => {
      handlers['agent_status']?.({ agent: 'alice', status: 'running' });
      handlers['agent_signal_task_accepted']?.({ agent: 'alice' });
    });

    expect(result.current.typingAgents).toContain('alice');
    expect(result.current.processingAgents).toContain('alice');

    // Message arrives from alice
    act(() => {
      handlers['message']?.({
        type: 'message',
        message: {
          id: 'msg-3',
          channel: 'general',
          author: 'alice',
          content: 'done',
          timestamp: '2024-01-01T00:00:00Z',
        },
      });
    });

    expect(result.current.typingAgents).not.toContain('alice');
    expect(result.current.processingAgents).not.toContain('alice');
  });

  it('notifies callers when an agent task completes', () => {
    const onTaskComplete = vi.fn();
    renderHook(() =>
      useChannelSocket(makeOptions({ onTaskComplete }), mockMapRawMessages)
    );

    act(() => {
      handlers['agent_signal_task_complete']?.({ agent: 'alice' });
    });

    expect(onTaskComplete).toHaveBeenCalledWith('alice');
  });

  it('adds agent to processingAgents on agent_signal_processing for selected channel', () => {
    const { result } = renderHook(() =>
      useChannelSocket(makeOptions({ selectedChannelName: 'general' }), mockMapRawMessages)
    );

    act(() => {
      handlers['agent_signal_processing']?.({
        agent: 'alice',
        data: { channel: 'general' },
      });
    });

    expect(result.current.processingAgents).toContain('alice');
  });

  it('ignores agent_signal_processing for a different channel', () => {
    const { result } = renderHook(() =>
      useChannelSocket(makeOptions({ selectedChannelName: 'general' }), mockMapRawMessages)
    );

    act(() => {
      handlers['agent_signal_processing']?.({
        agent: 'alice',
        data: { channel: 'other' },
      });
    });

    expect(result.current.processingAgents).not.toContain('alice');
  });

  it('sets agent activity and typing on agent_signal_activity', () => {
    const { result } = renderHook(() =>
      useChannelSocket(makeOptions(), mockMapRawMessages)
    );

    act(() => {
      handlers['agent_signal_activity']?.({
        agent: 'alice',
        data: { activity: 'searching the web' },
      });
    });

    expect(result.current.agentActivity).toMatchObject({ alice: 'searching the web' });
    expect(result.current.typingAgents).toContain('alice');
  });

  it('clears agent state on agent_signal_task_complete', () => {
    const { result } = renderHook(() =>
      useChannelSocket(makeOptions(), mockMapRawMessages)
    );

    act(() => {
      handlers['agent_signal_activity']?.({ agent: 'alice', data: { activity: 'working' } });
      handlers['agent_signal_task_accepted']?.({ agent: 'alice' });
    });

    act(() => {
      handlers['agent_signal_task_complete']?.({ agent: 'alice' });
    });

    expect(result.current.typingAgents).not.toContain('alice');
    expect(result.current.processingAgents).not.toContain('alice');
    expect(result.current.agentActivity).not.toHaveProperty('alice');
  });

  it('adds agent to processingAgents on agent_signal_task_accepted', () => {
    const { result } = renderHook(() =>
      useChannelSocket(makeOptions(), mockMapRawMessages)
    );

    act(() => {
      handlers['agent_signal_task_accepted']?.({ agent: 'alice' });
    });

    expect(result.current.processingAgents).toContain('alice');
  });

  it('appends error message on agent_signal_error', () => {
    const onAppendMessage = vi.fn();
    renderHook(() =>
      useChannelSocket(makeOptions({ onAppendMessage }), mockMapRawMessages)
    );

    act(() => {
      handlers['agent_signal_error']?.({
        agent: 'alice',
        data: { message: 'timeout' },
        timestamp: '2024-01-01T00:00:00Z',
      });
    });

    expect(onAppendMessage).toHaveBeenCalledTimes(1);
    const msg: Message = onAppendMessage.mock.calls[0][0];
    expect(msg.isError).toBe(true);
    expect(msg.content).toContain("alice couldn't respond");
    expect(msg.content).toContain('timeout');
  });

  it('appends escalation message on agent_signal_escalation', () => {
    const onAppendMessage = vi.fn();
    renderHook(() =>
      useChannelSocket(makeOptions({ onAppendMessage }), mockMapRawMessages)
    );

    act(() => {
      handlers['agent_signal_escalation']?.({
        agent: 'alice',
        data: { reason: 'XPIA detected' },
        timestamp: '2024-01-01T00:00:00Z',
      });
    });

    expect(onAppendMessage).toHaveBeenCalledTimes(1);
    const msg: Message = onAppendMessage.mock.calls[0][0];
    expect(msg.isError).toBe(true);
    expect(msg.content).toContain('Security escalation from alice');
    expect(msg.content).toContain('XPIA detected');
  });

  it('appends halt message and clears agent state on agent_signal_self_halt', () => {
    const onAppendMessage = vi.fn();
    const { result } = renderHook(() =>
      useChannelSocket(makeOptions({ onAppendMessage }), mockMapRawMessages)
    );

    act(() => {
      handlers['agent_signal_task_accepted']?.({ agent: 'alice' });
    });

    act(() => {
      handlers['agent_signal_self_halt']?.({
        agent: 'alice',
        data: { reason: 'budget exceeded' },
        timestamp: '2024-01-01T00:00:00Z',
      });
    });

    expect(onAppendMessage).toHaveBeenCalledTimes(1);
    const msg: Message = onAppendMessage.mock.calls[0][0];
    expect(msg.isError).toBe(true);
    expect(msg.content).toContain('self-halted');
    expect(msg.content).toContain('budget exceeded');
    expect(result.current.processingAgents).not.toContain('alice');
  });

  it('appends finding message on agent_signal_finding', () => {
    const onAppendMessage = vi.fn();
    renderHook(() =>
      useChannelSocket(makeOptions({ onAppendMessage }), mockMapRawMessages)
    );

    act(() => {
      handlers['agent_signal_finding']?.({
        agent: 'alice',
        data: { summary: 'Found suspicious IP' },
        timestamp: '2024-01-01T00:00:00Z',
      });
    });

    expect(onAppendMessage).toHaveBeenCalledTimes(1);
    const msg: Message = onAppendMessage.mock.calls[0][0];
    expect(msg.content).toBe('Found suspicious IP');
    expect(msg.isSystem).toBe(true);
  });

  it('adds agent to typingAgents on agent_status running', () => {
    const { result } = renderHook(() =>
      useChannelSocket(makeOptions(), mockMapRawMessages)
    );

    act(() => {
      handlers['agent_status']?.({ agent: 'alice', status: 'running' });
    });

    expect(result.current.typingAgents).toContain('alice');
  });

  it('removes agent from typingAgents on agent_status idle', () => {
    const { result } = renderHook(() =>
      useChannelSocket(makeOptions(), mockMapRawMessages)
    );

    act(() => {
      handlers['agent_status']?.({ agent: 'alice', status: 'running' });
    });
    act(() => {
      handlers['agent_status']?.({ agent: 'alice', status: 'idle' });
    });

    expect(result.current.typingAgents).not.toContain('alice');
  });

  it('does not call onAppendMessage for signal events when selectedChannelName is undefined', () => {
    const onAppendMessage = vi.fn();
    renderHook(() =>
      useChannelSocket(
        makeOptions({ selectedChannelName: undefined, onAppendMessage }),
        mockMapRawMessages
      )
    );

    act(() => {
      handlers['agent_signal_error']?.({ agent: 'alice', data: {} });
      handlers['agent_signal_escalation']?.({ agent: 'alice', data: {} });
      handlers['agent_signal_self_halt']?.({ agent: 'alice', data: {} });
      handlers['agent_signal_finding']?.({ agent: 'alice', data: { summary: 'x' } });
    });

    expect(onAppendMessage).not.toHaveBeenCalled();
  });

  it('exposes setProcessingAgents for external use', () => {
    const { result } = renderHook(() =>
      useChannelSocket(makeOptions(), mockMapRawMessages)
    );

    act(() => {
      result.current.setProcessingAgents(['agent-a', 'agent-b']);
    });

    expect(result.current.processingAgents).toEqual(['agent-a', 'agent-b']);
  });
});
