# Web UI God Page Decomposition

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Break apart the 5 largest screens in agency-web (Channels, KnowledgeExplorer, AgentDetail, Admin, Hub) into focused, maintainable modules without changing any user-facing behavior.

**Architecture:** Pure refactor — extract hooks, sub-components, and lazy imports from monolithic screen files. Each task produces a working build with passing tests. No new features, no API changes, no routing changes.

**Tech Stack:** React 18, Vite, Tailwind CSS v4, Vitest + RTL + MSW, react-router v7

---

## File Map

### Task 1: Channels.tsx → useChannelSocket hook
- Create: `src/app/hooks/useChannelSocket.ts`
- Modify: `src/app/screens/Channels.tsx`
- Test: `src/test/useChannelSocket.test.ts`

### Task 2: Channels.tsx → useChannelMessages hook
- Create: `src/app/hooks/useChannelMessages.ts`
- Modify: `src/app/screens/Channels.tsx`
- Test: `src/test/useChannelMessages.test.ts`

### Task 3: Channels.tsx → extract AgentDetailSheet + HelpDialog
- Create: `src/app/screens/channels/AgentDetailSheet.tsx`
- Create: `src/app/screens/channels/HelpDialog.tsx`
- Modify: `src/app/screens/Channels.tsx`

### Task 4: KnowledgeExplorer.tsx → extract GraphView to own file
- Create: `src/app/screens/knowledge/GraphView.tsx`
- Create: `src/app/screens/knowledge/types.ts`
- Create: `src/app/screens/knowledge/constants.ts`
- Modify: `src/app/screens/KnowledgeExplorer.tsx`

### Task 5: KnowledgeExplorer.tsx → extract NodeBrowser + NodeDetail
- Create: `src/app/screens/knowledge/NodeBrowser.tsx`
- Create: `src/app/screens/knowledge/NodeDetail.tsx`
- Create: `src/app/screens/knowledge/NodeCard.tsx`
- Create: `src/app/screens/knowledge/badges.tsx`
- Modify: `src/app/screens/KnowledgeExplorer.tsx`

### Task 6: AgentDetail.tsx → extract tab content to sub-components
- Create: `src/app/screens/agents/AgentOverviewTab.tsx`
- Create: `src/app/screens/agents/AgentActivityTab.tsx`
- Create: `src/app/screens/agents/AgentOperationsTab.tsx`
- Create: `src/app/screens/agents/AgentSystemTab.tsx`
- Modify: `src/app/screens/agents/AgentDetail.tsx`

### Task 7: Admin.tsx → extract inline tabs + lazy load all tabs
- Create: `src/app/screens/admin/TrustTab.tsx`
- Create: `src/app/screens/admin/PolicyTab.tsx`
- Create: `src/app/screens/admin/DangerZoneTab.tsx`
- Modify: `src/app/screens/Admin.tsx`

### Task 8: Hub.tsx → extract ComponentInfoDialog + DeploySection
- Create: `src/app/screens/hub/ComponentInfoDialog.tsx`
- Create: `src/app/screens/hub/DeploySection.tsx`
- Modify: `src/app/screens/Hub.tsx`

---

## Tasks

### Task 1: Extract useChannelSocket from Channels.tsx

Channels.tsx has 10 separate `useEffect` blocks subscribing to WebSocket events (`message`, `agent_signal_error`, `agent_signal_processing`, `agent_signal_activity`, `agent_signal_task_complete`, `agent_signal_task_accepted`, `agent_signal_escalation`, `agent_signal_self_halt`, `agent_signal_finding`, `agent_status`). Extract all of them into a single hook.

**Files:**
- Create: `src/app/hooks/useChannelSocket.ts`
- Modify: `src/app/screens/Channels.tsx`
- Test: `src/test/useChannelSocket.test.ts`

- [ ] **Step 1: Write the hook test**

```ts
// src/test/useChannelSocket.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';

// Mock the ws module before importing the hook
const mockOn = vi.fn(() => vi.fn()); // returns unsubscribe
const mockOnConnectionChange = vi.fn(() => vi.fn());
vi.mock('../app/lib/ws', () => ({
  socket: {
    on: mockOn,
    onConnectionChange: mockOnConnectionChange,
    connected: false,
    gaveUp: false,
  },
}));

// Import after mock
import { useChannelSocket } from '../app/hooks/useChannelSocket';

describe('useChannelSocket', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('subscribes to connection changes', () => {
    renderHook(() => useChannelSocket('general'));
    expect(mockOnConnectionChange).toHaveBeenCalled();
  });

  it('subscribes to all agent signal events', () => {
    renderHook(() => useChannelSocket('general'));
    const events = mockOn.mock.calls.map((c) => c[0]);
    expect(events).toContain('message');
    expect(events).toContain('agent_signal_error');
    expect(events).toContain('agent_signal_processing');
    expect(events).toContain('agent_signal_activity');
    expect(events).toContain('agent_signal_task_complete');
    expect(events).toContain('agent_signal_task_accepted');
    expect(events).toContain('agent_signal_escalation');
    expect(events).toContain('agent_signal_self_halt');
    expect(events).toContain('agent_signal_finding');
    expect(events).toContain('agent_status');
  });

  it('cleans up subscriptions on unmount', () => {
    const unsub = vi.fn();
    mockOn.mockReturnValue(unsub);
    mockOnConnectionChange.mockReturnValue(unsub);
    const { unmount } = renderHook(() => useChannelSocket('general'));
    unmount();
    // Each subscription should be cleaned up
    expect(unsub).toHaveBeenCalled();
  });

  it('returns typing/processing/activity state', () => {
    const { result } = renderHook(() => useChannelSocket('general'));
    expect(result.current.typingAgents).toEqual([]);
    expect(result.current.processingAgents).toEqual([]);
    expect(result.current.agentActivity).toEqual({});
    expect(result.current.wsConnected).toBe(false);
  });
});
```

- [ ] **Step 2: Run the test — expect FAIL**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vitest run src/test/useChannelSocket.test.ts`
Expected: FAIL — module not found

- [ ] **Step 3: Create useChannelSocket hook**

Extract the 10 socket `useEffect` blocks from `Channels.tsx` (lines 57-420) into a new hook. The hook receives `selectedChannelName` and `setMessages`/`mapRawMessages` callbacks, and returns `{ typingAgents, processingAgents, agentActivity, wsConnected }`.

```ts
// src/app/hooks/useChannelSocket.ts
import { useState, useEffect } from 'react';
import { socket } from '../lib/ws';
import { formatMessageTime } from '../lib/time';
import type { Message } from '../types';

interface UseChannelSocketOptions {
  selectedChannelName: string | null;
  onAppendMessage: (msg: Message) => void;
  onIncomingMessage: (channelName: string, raw: any) => void;
  onUnreadIncrement: (channelName: string) => void;
}

export function useChannelSocket({
  selectedChannelName,
  onAppendMessage,
  onIncomingMessage,
  onUnreadIncrement,
}: UseChannelSocketOptions) {
  const [typingAgents, setTypingAgents] = useState<string[]>([]);
  const [processingAgents, setProcessingAgents] = useState<string[]>([]);
  const [agentActivity, setAgentActivity] = useState<Record<string, string>>({});
  const [wsConnected, setWsConnected] = useState(socket.connected);

  // Connection state
  useEffect(() => {
    const unsub = socket.onConnectionChange(setWsConnected);
    return () => { unsub(); };
  }, []);

  // Clear indicators on channel switch
  useEffect(() => {
    setTypingAgents([]);
    setProcessingAgents([]);
    setAgentActivity({});
  }, [selectedChannelName]);

  const SYSTEM_SENDERS = new Set(['_platform', '_system']);

  // Real-time messages
  useEffect(() => {
    const unsub = socket.on('message', (event: any) => {
      const msgChannel: string | undefined = event.message?.channel;
      if (msgChannel === selectedChannelName) {
        const author = event.message?.author;
        if (author && !SYSTEM_SENDERS.has(author)) {
          setTypingAgents((prev) => prev.filter((a) => a !== author));
          setProcessingAgents((prev) => prev.filter((a) => a !== author));
          setAgentActivity((prev) => { const next = { ...prev }; delete next[author]; return next; });
        }
        if (event.message) {
          onIncomingMessage(msgChannel!, event.message);
        }
      } else if (msgChannel) {
        onUnreadIncrement(msgChannel);
      }
    });
    return () => { unsub(); };
  }, [selectedChannelName, onIncomingMessage, onUnreadIncrement]);

  // Agent error signals
  useEffect(() => {
    const unsub = socket.on('agent_signal_error', (event: any) => {
      const agent: string = event.agent;
      const data = event.data || {};
      if (!agent || !selectedChannelName) return;
      setTypingAgents((prev) => prev.filter((a) => a !== agent));
      setProcessingAgents((prev) => prev.filter((a) => a !== agent));
      onAppendMessage({
        id: `error-${Date.now()}-${agent}`,
        channelId: selectedChannelName,
        author: agent,
        displayAuthor: agent,
        isAgent: false,
        isSystem: true,
        isError: true,
        timestamp: formatMessageTime(event.timestamp || new Date().toISOString()),
        rawTimestamp: event.timestamp || new Date().toISOString(),
        content: `${agent} couldn't respond: ${data.message || data.category || 'unknown error'}`,
        flag: null,
      });
    });
    return () => { unsub(); };
  }, [selectedChannelName, onAppendMessage]);

  // Agent processing signals
  useEffect(() => {
    const timeouts = new Map<string, ReturnType<typeof setTimeout>>();
    const unsub = socket.on('agent_signal_processing', (event: any) => {
      const agent: string = event.agent;
      const channel: string = event.data?.channel;
      if (!agent || channel !== selectedChannelName) return;
      setProcessingAgents((prev) => (prev.includes(agent) ? prev : [...prev, agent]));
      if (timeouts.has(agent)) clearTimeout(timeouts.get(agent)!);
      timeouts.set(agent, setTimeout(() => {
        setProcessingAgents((prev) => prev.filter((a) => a !== agent));
        timeouts.delete(agent);
      }, 60_000));
    });
    return () => { unsub(); timeouts.forEach((t) => clearTimeout(t)); };
  }, [selectedChannelName]);

  // Agent activity signals
  useEffect(() => {
    const unsub = socket.on('agent_signal_activity', (event: any) => {
      const agent: string = event.agent;
      const activity: string = event.data?.activity;
      if (!agent || !activity) return;
      setAgentActivity((prev) => ({ ...prev, [agent]: activity }));
      setTypingAgents((prev) => (prev.includes(agent) ? prev : [...prev, agent]));
    });
    return () => { unsub(); };
  }, []);

  // Agent task_complete signals
  useEffect(() => {
    const unsub = socket.on('agent_signal_task_complete', (event: any) => {
      const agent: string = event.agent;
      if (!agent) return;
      setTypingAgents((prev) => prev.filter((a) => a !== agent));
      setProcessingAgents((prev) => prev.filter((a) => a !== agent));
      setAgentActivity((prev) => { const next = { ...prev }; delete next[agent]; return next; });
    });
    return () => { unsub(); };
  }, []);

  // Agent task_accepted signals
  useEffect(() => {
    const unsub = socket.on('agent_signal_task_accepted', (event: any) => {
      const agent: string = event.agent;
      if (!agent) return;
      setProcessingAgents((prev) => (prev.includes(agent) ? prev : [...prev, agent]));
    });
    return () => { unsub(); };
  }, []);

  // Agent escalation signals
  useEffect(() => {
    const unsub = socket.on('agent_signal_escalation', (event: any) => {
      const agent: string = event.agent;
      const data = event.data || {};
      if (!agent || !selectedChannelName) return;
      onAppendMessage({
        id: `escalation-${Date.now()}-${agent}`,
        channelId: selectedChannelName,
        author: agent,
        displayAuthor: agent,
        isAgent: false,
        isSystem: true,
        isError: true,
        timestamp: formatMessageTime(event.timestamp || new Date().toISOString()),
        rawTimestamp: event.timestamp || new Date().toISOString(),
        content: `⚠ Security escalation from ${agent}: ${data.message || data.reason || 'constraint violation detected'}`,
        flag: null,
      });
    });
    return () => { unsub(); };
  }, [selectedChannelName, onAppendMessage]);

  // Agent self_halt signals
  useEffect(() => {
    const unsub = socket.on('agent_signal_self_halt', (event: any) => {
      const agent: string = event.agent;
      const data = event.data || {};
      if (!agent || !selectedChannelName) return;
      setTypingAgents((prev) => prev.filter((a) => a !== agent));
      setProcessingAgents((prev) => prev.filter((a) => a !== agent));
      setAgentActivity((prev) => { const next = { ...prev }; delete next[agent]; return next; });
      onAppendMessage({
        id: `halt-${Date.now()}-${agent}`,
        channelId: selectedChannelName,
        author: agent,
        displayAuthor: agent,
        isAgent: false,
        isSystem: true,
        isError: true,
        timestamp: formatMessageTime(event.timestamp || new Date().toISOString()),
        rawTimestamp: event.timestamp || new Date().toISOString(),
        content: `${agent} has self-halted: ${data.reason || 'no reason given'}`,
        flag: null,
      });
    });
    return () => { unsub(); };
  }, [selectedChannelName, onAppendMessage]);

  // Agent finding signals
  useEffect(() => {
    const unsub = socket.on('agent_signal_finding', (event: any) => {
      const agent: string = event.agent;
      const data = event.data || {};
      if (!agent || !selectedChannelName) return;
      onAppendMessage({
        id: `finding-${Date.now()}-${agent}`,
        channelId: selectedChannelName,
        author: agent,
        displayAuthor: agent,
        isAgent: false,
        isSystem: true,
        timestamp: formatMessageTime(event.timestamp || new Date().toISOString()),
        rawTimestamp: event.timestamp || new Date().toISOString(),
        content: data.summary || data.message || 'New finding reported',
        flag: null,
        metadata: data,
      });
    });
    return () => { unsub(); };
  }, [selectedChannelName, onAppendMessage]);

  // Agent status (typing) signals
  useEffect(() => {
    const timeouts = new Map<string, ReturnType<typeof setTimeout>>();
    const unsub = socket.on('agent_status', (event: any) => {
      const agent: string = event.agent;
      const status: string = event.status;
      if (!agent) return;
      if (status === 'running') {
        setTypingAgents((prev) => (prev.includes(agent) ? prev : [...prev, agent]));
        if (timeouts.has(agent)) clearTimeout(timeouts.get(agent)!);
        timeouts.set(agent, setTimeout(() => {
          setTypingAgents((prev) => prev.filter((a) => a !== agent));
          timeouts.delete(agent);
        }, 30_000));
      } else {
        setTypingAgents((prev) => prev.filter((a) => a !== agent));
        if (timeouts.has(agent)) { clearTimeout(timeouts.get(agent)!); timeouts.delete(agent); }
      }
    });
    return () => { unsub(); timeouts.forEach((t) => clearTimeout(t)); };
  }, []);

  return {
    typingAgents,
    processingAgents,
    agentActivity,
    wsConnected,
    setProcessingAgents,
  };
}
```

- [ ] **Step 4: Run the test — expect PASS**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vitest run src/test/useChannelSocket.test.ts`
Expected: PASS

- [ ] **Step 5: Update Channels.tsx to use the hook**

Remove the 10 socket `useEffect` blocks and the related state declarations from `Channels.tsx`. Replace with:

```tsx
// In Channels(), replace lines 36-38 and 57-420 with:
const handleAppendMessage = useCallback((msg: Message) => {
  setMessages((prev) => [...prev, msg]);
}, []);

const handleIncomingMessage = useCallback((channelName: string, raw: RawMessage) => {
  const mapped = mapRawMessages([raw], channelName);
  setMessages((prev) => {
    const newMsg = mapped[0];
    if (newMsg && !prev.some((m) => m.id === newMsg.id)) {
      return [...prev, newMsg];
    }
    return prev;
  });
}, [mapRawMessages]);

const handleUnreadIncrement = useCallback((channelName: string) => {
  setChannels((prev) =>
    prev.map((ch) => ch.name === channelName ? { ...ch, unreadCount: ch.unreadCount + 1 } : ch)
  );
}, []);

const {
  typingAgents,
  processingAgents,
  agentActivity,
  wsConnected,
  setProcessingAgents,
} = useChannelSocket({
  selectedChannelName: selectedChannel?.name ?? null,
  onAppendMessage: handleAppendMessage,
  onIncomingMessage: handleIncomingMessage,
  onUnreadIncrement: handleUnreadIncrement,
});
```

Add import at top: `import { useChannelSocket } from '../hooks/useChannelSocket';`

Remove these `useState` declarations that are now inside the hook:
- `typingAgents`, `processingAgents`, `agentActivity`, `wsConnected`

- [ ] **Step 6: Run full test suite — expect PASS**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vitest run`
Expected: All tests pass, including `Channels.test.tsx`

- [ ] **Step 7: Verify build**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vite build`
Expected: Build succeeds

- [ ] **Step 8: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/hooks/useChannelSocket.ts src/test/useChannelSocket.test.ts src/app/screens/Channels.tsx
git commit -m "refactor: extract useChannelSocket hook from Channels screen

Moves all 10 WebSocket subscription effects into a dedicated hook.
Channels.tsx drops from 910 to ~560 lines."
```

---

### Task 2: Extract useChannelMessages from Channels.tsx

The message loading, pagination, and mapping logic is the other major block of complexity. Extract `loadMessages`, `loadMoreMessages`, `mapRawMessages`, `handleSend`, `handleEdit`, `handleDelete`, `handleReact`, `handleUnreact`, `handleThreadSend` into a hook.

**Files:**
- Create: `src/app/hooks/useChannelMessages.ts`
- Modify: `src/app/screens/Channels.tsx`
- Test: `src/test/useChannelMessages.test.ts`

- [ ] **Step 1: Write the hook test**

```ts
// src/test/useChannelMessages.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';

vi.mock('../app/lib/api', () => ({
  api: {
    channels: {
      read: vi.fn().mockResolvedValue([]),
      send: vi.fn().mockResolvedValue({ ok: true }),
      edit: vi.fn().mockResolvedValue({ ok: true }),
      delete: vi.fn().mockResolvedValue({ ok: true }),
      react: vi.fn().mockResolvedValue({ ok: true }),
      unreact: vi.fn().mockResolvedValue({ ok: true }),
    },
  },
}));

import { useChannelMessages } from '../app/hooks/useChannelMessages';
import { api } from '../app/lib/api';

describe('useChannelMessages', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('returns empty messages initially', () => {
    const { result } = renderHook(() => useChannelMessages(null));
    expect(result.current.messages).toEqual([]);
    expect(result.current.loading).toBe(false);
  });

  it('loads messages when channel name is set', async () => {
    (api.channels.read as any).mockResolvedValue([
      { id: 'm1', author: 'alice', content: 'Hello', timestamp: '2026-03-16T10:00:00Z' },
    ]);
    const { result } = renderHook(() => useChannelMessages('general'));
    await waitFor(() => {
      expect(result.current.messages.length).toBe(1);
      expect(result.current.messages[0].content).toBe('Hello');
    });
  });

  it('maps system senders correctly', async () => {
    (api.channels.read as any).mockResolvedValue([
      { id: 'm1', author: '_platform', content: 'System msg', timestamp: '2026-03-16T10:00:00Z' },
    ]);
    const { result } = renderHook(() => useChannelMessages('general'));
    await waitFor(() => {
      expect(result.current.messages[0].isSystem).toBe(true);
      expect(result.current.messages[0].displayAuthor).toBe('Agency Platform');
    });
  });
});
```

- [ ] **Step 2: Run the test — expect FAIL**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vitest run src/test/useChannelMessages.test.ts`
Expected: FAIL — module not found

- [ ] **Step 3: Create useChannelMessages hook**

Extract `mapRawMessages`, `loadMessages`, `loadMoreMessages`, `handleSend`, `handleEdit`, `handleDelete`, `handleReact`, `handleUnreact`, `handleThreadSend` from `Channels.tsx` into the hook. The hook manages `messages`, `loading`, `hasMore`, `loadingMore`, `messageLimit` state.

```ts
// src/app/hooks/useChannelMessages.ts
import { useState, useCallback } from 'react';
import { api, type RawMessage } from '../lib/api';
import { formatMessageTime } from '../lib/time';
import type { Message } from '../types';

const INITIAL_MESSAGE_PAGE_SIZE = 20;
const MESSAGE_PAGE_SIZE = 50;
const SYSTEM_SENDERS = new Set(['_platform', '_system']);

function mapRawMessages(data: RawMessage[], channelName: string): Message[] {
  return data.map((m) => {
    const isSystem = SYSTEM_SENDERS.has(m.author);
    return {
      id: m.id || m.timestamp || crypto.randomUUID(),
      channelId: channelName,
      author: m.author,
      displayAuthor: isSystem ? 'Agency Platform' : m.author === '_operator' ? 'operator' : m.author,
      isAgent: !isSystem && m.author !== 'operator' && m.author !== '_operator',
      isSystem,
      timestamp: formatMessageTime(m.timestamp),
      rawTimestamp: m.timestamp,
      content: m.content,
      flag: m.flags?.decision ? 'DECISION' as const
        : m.flags?.blocker ? 'BLOCKER' as const
        : m.flags?.question ? 'QUESTION' as const
        : null,
      parentId: m.reply_to || undefined,
      metadata: { ...m.metadata, reactions: m.reactions } as Record<string, any>,
    };
  });
}

export { mapRawMessages };

export function useChannelMessages(channelName: string | null) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [loading, setLoading] = useState(false);
  const [hasMore, setHasMore] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [messageLimit, setMessageLimit] = useState(INITIAL_MESSAGE_PAGE_SIZE);

  const loadMessages = useCallback(async (name: string, limit = INITIAL_MESSAGE_PAGE_SIZE) => {
    try {
      const data = (await api.channels.read(name, limit)) ?? [];
      setMessages(mapRawMessages(data, name));
      setHasMore(data.length >= limit);
    } catch (err) {
      console.error('loadMessages error:', err);
    }
  }, []);

  const loadInitial = useCallback(async (name: string) => {
    setLoading(true);
    setMessageLimit(INITIAL_MESSAGE_PAGE_SIZE);
    try {
      await loadMessages(name, INITIAL_MESSAGE_PAGE_SIZE);
    } finally {
      setLoading(false);
    }
  }, [loadMessages]);

  const loadMore = useCallback(async () => {
    if (!channelName || loadingMore || !hasMore) return;
    setLoadingMore(true);
    const nextLimit = messageLimit + MESSAGE_PAGE_SIZE;
    try {
      const data = (await api.channels.read(channelName, nextLimit)) ?? [];
      setMessages(mapRawMessages(data, channelName));
      setMessageLimit(nextLimit);
      setHasMore(data.length >= nextLimit);
    } catch (err) {
      console.error('loadMore error:', err);
    } finally {
      setLoadingMore(false);
    }
  }, [channelName, loadingMore, hasMore, messageLimit]);

  const appendMessage = useCallback((msg: Message) => {
    setMessages((prev) => [...prev, msg]);
  }, []);

  const appendDeduped = useCallback((msg: Message) => {
    setMessages((prev) => {
      if (prev.some((m) => m.id === msg.id)) return prev;
      return [...prev, msg];
    });
  }, []);

  const removeMessage = useCallback((id: string) => {
    setMessages((prev) => prev.filter((m) => m.id !== id));
  }, []);

  const handleSend = useCallback(async (
    content: string,
    flags?: { decision?: boolean; blocker?: boolean; question?: boolean },
    members?: string[],
  ) => {
    if (!channelName) return;
    const optimisticMsg: Message = {
      id: `optimistic-${Date.now()}`,
      channelId: channelName,
      author: 'operator',
      displayAuthor: 'operator',
      isAgent: false,
      isSystem: false,
      timestamp: formatMessageTime(new Date().toISOString()),
      rawTimestamp: new Date().toISOString(),
      content,
      flag: flags?.decision ? 'DECISION' : flags?.blocker ? 'BLOCKER' : flags?.question ? 'QUESTION' : null,
    };
    setMessages((prev) => [...prev, optimisticMsg]);
    try {
      await api.channels.send(channelName, content, undefined, flags);
      loadMessages(channelName, messageLimit);
    } catch (err) {
      console.error('handleSend error:', err);
      setMessages((prev) => prev.filter((m) => m.id !== optimisticMsg.id));
    }
    return optimisticMsg;
  }, [channelName, loadMessages, messageLimit]);

  const handleEdit = useCallback(async (message: Message, newContent: string) => {
    if (!channelName) return;
    try {
      await api.channels.edit(channelName, message.id, newContent);
      await loadMessages(channelName, messageLimit);
    } catch (err) {
      console.error('handleEdit error:', err);
    }
  }, [channelName, loadMessages, messageLimit]);

  const handleDelete = useCallback(async (message: Message) => {
    if (!channelName) return;
    try {
      await api.channels.delete(channelName, message.id);
      await loadMessages(channelName, messageLimit);
    } catch (err) {
      console.error('handleDelete error:', err);
    }
  }, [channelName, loadMessages, messageLimit]);

  const handleReact = useCallback(async (message: Message, emoji: string) => {
    if (!channelName) return;
    try {
      await api.channels.react(channelName, message.id, emoji);
      await loadMessages(channelName, messageLimit);
    } catch (err) {
      console.error('handleReact error:', err);
    }
  }, [channelName, loadMessages, messageLimit]);

  const handleUnreact = useCallback(async (message: Message, emoji: string) => {
    if (!channelName) return;
    try {
      await api.channels.unreact(channelName, message.id, emoji);
      await loadMessages(channelName, messageLimit);
    } catch (err) {
      console.error('handleUnreact error:', err);
    }
  }, [channelName, loadMessages, messageLimit]);

  const handleThreadSend = useCallback(async (content: string, parentId: string) => {
    if (!channelName) return;
    const optimisticReply: Message = {
      id: `optimistic-${Date.now()}`,
      channelId: channelName,
      author: 'operator',
      displayAuthor: 'operator',
      isAgent: false,
      isSystem: false,
      timestamp: formatMessageTime(new Date().toISOString()),
      rawTimestamp: new Date().toISOString(),
      content,
      flag: null,
      parentId,
    };
    setMessages((prev) => [...prev, optimisticReply]);
    try {
      await api.channels.send(channelName, content, parentId);
      loadMessages(channelName, messageLimit);
    } catch (err) {
      console.error('handleThreadSend error:', err);
      setMessages((prev) => prev.filter((m) => m.id !== optimisticReply.id));
    }
    return optimisticReply;
  }, [channelName, loadMessages, messageLimit]);

  return {
    messages,
    loading,
    hasMore,
    loadingMore,
    setMessages,
    loadInitial,
    loadMore,
    loadMessages,
    appendMessage,
    appendDeduped,
    removeMessage,
    handleSend,
    handleEdit,
    handleDelete,
    handleReact,
    handleUnreact,
    handleThreadSend,
    mapRawMessages,
  };
}
```

- [ ] **Step 4: Run the test — expect PASS**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vitest run src/test/useChannelMessages.test.ts`
Expected: PASS

- [ ] **Step 5: Update Channels.tsx to use the hook**

Replace all message state, `mapRawMessages`, `loadMessages`, `loadMoreMessages`, `handleSend`, `handleEdit`, `handleDelete`, `handleReact`, `handleUnreact`, `handleThreadSend`, and optimistic message logic with the hook.

In the `useChannelSocket` callbacks, use `appendDeduped` for incoming messages and `appendMessage` for system signal messages.

Update `handleChannelSelect` to call `msgHook.loadInitial(channel.name)` instead of inline `loadMessages`.

Update `handleThreadSend` in the JSX to call `msgHook.handleThreadSend(content, threadParent.id)`.

- [ ] **Step 6: Run full test suite**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vitest run`
Expected: PASS

- [ ] **Step 7: Verify build**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vite build`
Expected: Build succeeds

- [ ] **Step 8: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/hooks/useChannelMessages.ts src/test/useChannelMessages.test.ts src/app/screens/Channels.tsx
git commit -m "refactor: extract useChannelMessages hook from Channels screen

Moves message loading, pagination, mapping, and CRUD operations into
a dedicated hook. Channels.tsx is now primarily UI composition."
```

---

### Task 3: Extract AgentDetailSheet and HelpDialog from Channels.tsx

The agent detail slide-over (lines 769-907) and help dialog (lines 716-767) are self-contained UI blocks that add ~190 lines to Channels.tsx.

**Files:**
- Create: `src/app/screens/channels/AgentDetailSheet.tsx`
- Create: `src/app/screens/channels/HelpDialog.tsx`
- Modify: `src/app/screens/Channels.tsx`

- [ ] **Step 1: Create the channels subdirectory**

Run: `mkdir -p /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web/src/app/screens/channels`

- [ ] **Step 2: Create AgentDetailSheet**

Extract lines 769-907 from Channels.tsx. The component receives the agent data, budget, connection state, and callbacks.

```tsx
// src/app/screens/channels/AgentDetailSheet.tsx
import { Button } from '../../components/ui/button';
import { Sheet, SheetContent } from '../../components/ui/sheet';
import { StatusIndicator } from '../../components/StatusIndicator';
import { X } from 'lucide-react';
import { formatDateTimeShort } from '../../lib/time';
import type { AgentStatus } from '../../types';
import type { RawAgent, RawBudgetResponse } from '../../lib/api';

interface AgentDetailSheetProps {
  agentName: string | null;
  agent: RawAgent | null;
  budget: RawBudgetResponse | null;
  onClose: () => void;
  onMessageAgent: (dmChannelName: string) => void;
}

export function AgentDetailSheet({ agentName, agent, budget, onClose, onMessageAgent }: AgentDetailSheetProps) {
  // Paste the Sheet JSX from Channels.tsx lines 770-907
  // Replace inline handlers with props:
  //   - setAgentDetailName(null) → onClose()
  //   - handleChannelSelect(dmChannel) → onMessageAgent('dm-' + agent.name)
  // The rest of the JSX is identical.
  // ... (full JSX copied from original — see Channels.tsx lines 770-907)
}
```

The implementor should copy the full JSX block from Channels.tsx lines 770-907 into this component, replacing:
- `agentDetailName` open check → `agentName` prop
- `setAgentDetailName(null)` → `onClose()`
- The "Message" button handler → `onMessageAgent('dm-' + agentDetail.name); onClose()`

- [ ] **Step 3: Create HelpDialog**

Extract lines 716-767 from Channels.tsx.

```tsx
// src/app/screens/channels/HelpDialog.tsx
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '../../components/ui/dialog';

interface HelpDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function HelpDialog({ open, onOpenChange }: HelpDialogProps) {
  // Paste the Dialog JSX from Channels.tsx lines 716-767
  // ... (full JSX copied from original)
}
```

- [ ] **Step 4: Update Channels.tsx**

Remove the Sheet and Help Dialog JSX blocks. Import and use the new components:

```tsx
import { AgentDetailSheet } from './channels/AgentDetailSheet';
import { HelpDialog } from './channels/HelpDialog';
```

Replace the Sheet block with:
```tsx
<AgentDetailSheet
  agentName={agentDetailName}
  agent={agentDetail}
  budget={agentBudget}
  onClose={() => setAgentDetailName(null)}
  onMessageAgent={(dmName) => {
    const dmChannel = channels.find((c) => c.name === dmName);
    if (dmChannel) handleChannelSelect(dmChannel);
  }}
/>
```

Replace the Help Dialog with:
```tsx
<HelpDialog open={helpOpen} onOpenChange={setHelpOpen} />
```

- [ ] **Step 5: Run full test suite**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vitest run`
Expected: PASS

- [ ] **Step 6: Verify build**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vite build`
Expected: Build succeeds

- [ ] **Step 7: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/screens/channels/ src/app/screens/Channels.tsx
git commit -m "refactor: extract AgentDetailSheet and HelpDialog from Channels

Channels.tsx is now ~350 lines — down from 910."
```

---

### Task 4: Extract GraphView from KnowledgeExplorer.tsx

The GraphView component (lines 617-977) is the largest single block — 360 lines including Cytoscape setup, layout modes, element building, and toolbar. It has zero coupling to the rest of KnowledgeExplorer beyond receiving `nodes`, `realEdges`, `selectedNode`, and `onSelectNode`.

**Files:**
- Create: `src/app/screens/knowledge/GraphView.tsx`
- Create: `src/app/screens/knowledge/types.ts`
- Create: `src/app/screens/knowledge/constants.ts`
- Modify: `src/app/screens/KnowledgeExplorer.tsx`

- [ ] **Step 1: Create the knowledge subdirectory and shared files**

Run: `mkdir -p /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web/src/app/screens/knowledge`

Create `types.ts`:
```ts
// src/app/screens/knowledge/types.ts
export interface KnowledgeNode {
  type?: string;
  label: string;
  kind: string;
  summary?: string;
  properties?: Record<string, unknown>;
  source_type?: string;
  contributed_by?: string;
  created_at?: string;
  updated_at?: string;
  [key: string]: unknown;
}

export type ViewMode = 'browser' | 'graph' | 'search';
```

Create `constants.ts`:
```ts
// src/app/screens/knowledge/constants.ts
export const MAX_GRAPH_NODES = 500;

export const KIND_COLORS: Record<string, string> = {
  finding: '#6366f1',
  fact: '#a855f7',
  agent: '#10b981',
  channel: '#06b6d4',
  project: '#f59e0b',
  task: '#ec4899',
  observation: '#14b8a6',
  decision: '#f97316',
  issue: '#ef4444',
  rule: '#eab308',
  unknown: '#6b7280',
};
```

- [ ] **Step 2: Create GraphView.tsx**

Move lines 617-977 from KnowledgeExplorer.tsx into this file. Update imports to use the shared types and constants.

```tsx
// src/app/screens/knowledge/GraphView.tsx
import { useState, useEffect, useRef, useCallback } from 'react';
import cytoscape from 'cytoscape';
import { KIND_COLORS, MAX_GRAPH_NODES } from './constants';
import type { KnowledgeNode } from './types';

type LayoutMode = 'radial' | 'force' | 'timeline' | 'grid';
// ... paste LAYOUT_LABELS, CY_STYLE, getCyLayout, and GraphView function
// from KnowledgeExplorer.tsx lines 619-977
// Update KIND_COLORS references to use imported constant
export { GraphView };
```

- [ ] **Step 3: Update KnowledgeExplorer.tsx**

Replace the local `KnowledgeNode` interface and `ViewMode` type with imports from `./knowledge/types`. Replace `KIND_COLORS` and `MAX_GRAPH_NODES` with imports from `./knowledge/constants`. Remove the entire `GraphView` function and `getCyLayout`, `CY_STYLE`, `LAYOUT_LABELS` constants. Import `GraphView` from `./knowledge/GraphView`.

```tsx
import type { KnowledgeNode, ViewMode } from './knowledge/types';
import { KIND_COLORS } from './knowledge/constants';
import { GraphView } from './knowledge/GraphView';
```

- [ ] **Step 4: Run full test suite**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vitest run`
Expected: PASS

- [ ] **Step 5: Verify build**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vite build`
Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/screens/knowledge/ src/app/screens/KnowledgeExplorer.tsx
git commit -m "refactor: extract GraphView and shared types from KnowledgeExplorer

GraphView (360 lines) now lives in knowledge/GraphView.tsx.
Shared types and color constants extracted to knowledge/types.ts and constants.ts."
```

---

### Task 5: Extract NodeBrowser, NodeDetail, and badges from KnowledgeExplorer.tsx

After Task 4, the remaining internal components are NodeBrowser (lines 254-341), NodeGroup (344-381), NodeCard (383-418), NodeDetail (422-614), KindBadge (981-990), SourceBadge (993-1001).

**Files:**
- Create: `src/app/screens/knowledge/NodeBrowser.tsx`
- Create: `src/app/screens/knowledge/NodeDetail.tsx`
- Create: `src/app/screens/knowledge/badges.tsx`
- Modify: `src/app/screens/KnowledgeExplorer.tsx`

- [ ] **Step 1: Create badges.tsx**

```tsx
// src/app/screens/knowledge/badges.tsx
import { KIND_COLORS } from './constants';

export function KindBadge({ kind }: { kind: string }) {
  const hex = KIND_COLORS[kind] || KIND_COLORS.unknown;
  return (
    <span
      className="text-[10px] px-1.5 py-0.5 rounded font-medium"
      style={{ backgroundColor: hex + '1a', color: hex }}
    >
      {kind}
    </span>
  );
}

export function SourceBadge({ source }: { source: string }) {
  const colors =
    source === 'agent'
      ? 'bg-green-500/10 text-green-600 dark:text-green-400'
      : source === 'platform'
        ? 'bg-cyan-500/10 text-cyan-600 dark:text-cyan-400'
        : 'bg-secondary text-muted-foreground';
  return <span className={`text-[10px] px-1.5 py-0.5 rounded ${colors}`}>{source}</span>;
}
```

- [ ] **Step 2: Create NodeBrowser.tsx**

Move `NodeBrowser`, `NodeGroup`, and `NodeCard` into this file. They import from `./types`, `./constants`, `./badges`.

```tsx
// src/app/screens/knowledge/NodeBrowser.tsx
import { useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../components/ui/select';
import { Input } from '../../components/ui/input';
import { formatDateTimeShort } from '../../lib/time';
import { KindBadge, SourceBadge } from './badges';
import { KIND_COLORS } from './constants';
import type { KnowledgeNode } from './types';

// ... paste NodeBrowser, NodeGroup, NodeCard from KnowledgeExplorer.tsx
// Update imports to use shared types/constants
export { NodeBrowser };
```

- [ ] **Step 3: Create NodeDetail.tsx**

Move the `NodeDetail` component. It imports from `./types`, `./constants`, `./badges`, and uses `api.knowledge.neighbors`/`query`.

```tsx
// src/app/screens/knowledge/NodeDetail.tsx
import { useState, useEffect } from 'react';
import { Search, X } from 'lucide-react';
import { Button } from '../../components/ui/button';
import { api } from '../../lib/api';
import { formatDateTimeShort } from '../../lib/time';
import { KindBadge, SourceBadge } from './badges';
import { KIND_COLORS } from './constants';
import type { KnowledgeNode } from './types';

// ... paste NodeDetail from KnowledgeExplorer.tsx
export { NodeDetail };
```

- [ ] **Step 4: Update KnowledgeExplorer.tsx**

Remove all internal component definitions. Import from the new files:

```tsx
import { NodeBrowser } from './knowledge/NodeBrowser';
import { NodeDetail } from './knowledge/NodeDetail';
import { KindBadge } from './knowledge/badges';
```

KnowledgeExplorer.tsx should now be ~100 lines: imports, data loading, view switching, and the three-panel layout.

- [ ] **Step 5: Run full test suite**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vitest run`
Expected: PASS

- [ ] **Step 6: Verify build**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vite build`
Expected: Build succeeds

- [ ] **Step 7: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/screens/knowledge/ src/app/screens/KnowledgeExplorer.tsx
git commit -m "refactor: extract NodeBrowser, NodeDetail, badges from KnowledgeExplorer

KnowledgeExplorer.tsx is now ~100 lines — down from 1,001.
All sub-components live in knowledge/ subdirectory."
```

---

### Task 6: Extract tab content from AgentDetail.tsx

AgentDetail defines `renderOverviewContent`, `renderActivityFeedContent`, `renderChannelsContent`, `renderKnowledgeContent`, `renderMeeseeksContent`, `renderConfigContent` as inline functions. Convert each to a proper component in the `agents/` subdirectory.

**Files:**
- Create: `src/app/screens/agents/AgentOverviewTab.tsx`
- Create: `src/app/screens/agents/AgentActivityTab.tsx`
- Create: `src/app/screens/agents/AgentOperationsTab.tsx`
- Create: `src/app/screens/agents/AgentSystemTab.tsx`
- Modify: `src/app/screens/agents/AgentDetail.tsx`

- [ ] **Step 1: Create AgentOverviewTab.tsx**

Extract `renderOverviewContent()` (lines 112-221). The component receives `agent`, `budget` as props.

```tsx
// src/app/screens/agents/AgentOverviewTab.tsx
import { StatusIndicator } from '../../components/StatusIndicator';
import { formatDateTimeShort } from '../../lib/time';
import type { Agent } from '../../types';
import type { RawBudgetResponse } from '../../lib/api';

interface AgentOverviewTabProps {
  agent: Agent;
  budget: RawBudgetResponse | null;
}

export function AgentOverviewTab({ agent, budget }: AgentOverviewTabProps) {
  // Paste the JSX from renderOverviewContent, replacing bare references
  // with the props.
}
```

- [ ] **Step 2: Create AgentActivityTab.tsx**

Extract `renderActivityFeedContent()` (lines 223-243) plus the logs table that follows it. Receives `agent`, `dmText`, `setDmText`, `handleSendDM`, `logs`, `refreshingLogs`, `refreshLogs`, `expandedLog`, `setExpandedLog`.

```tsx
// src/app/screens/agents/AgentActivityTab.tsx
import { useState } from 'react';
import { Send, RefreshCw, ChevronDown, ChevronRight } from 'lucide-react';
import { Link } from 'react-router';
import { Button } from '../../components/ui/button';
import { AgentActivityGroup } from './AgentActivityGroup';
import { formatDateTimeShort } from '../../lib/time';
import type { Agent } from '../../types';

interface AgentActivityTabProps {
  agent: Agent;
  dmText: string;
  onDmTextChange: (text: string) => void;
  onSendDM: (agentName: string, text: string) => Promise<boolean>;
  logs: any[];
  refreshingLogs: boolean;
  onRefreshLogs: () => void;
}

export function AgentActivityTab({ agent, dmText, onDmTextChange, onSendDM, logs, refreshingLogs, onRefreshLogs }: AgentActivityTabProps) {
  const [expandedLog, setExpandedLog] = useState<number | null>(null);
  // Paste JSX from renderActivityFeedContent + logs rendering
}
```

- [ ] **Step 3: Create AgentOperationsTab.tsx**

Extract `renderChannelsContent()`, `renderKnowledgeContent()`, `renderMeeseeksContent()`. Receives the sub-tab state and relevant data.

```tsx
// src/app/screens/agents/AgentOperationsTab.tsx
// Combines channels, knowledge, meeseeks sub-tabs
// Paste JSX from the three render functions
```

- [ ] **Step 4: Create AgentSystemTab.tsx**

Extract `renderConfigContent()` and the logs sub-tab. Receives identity editing state and config data.

```tsx
// src/app/screens/agents/AgentSystemTab.tsx
// Combines config editor + system logs sub-tabs
// Paste JSX from renderConfigContent + capabilities management
```

- [ ] **Step 5: Update AgentDetail.tsx**

Remove all `renderXxxContent` functions. Import the new tab components. AgentDetail becomes a tab router that passes data down:

```tsx
{primaryTab === 'overview' && <AgentOverviewTab agent={agent} budget={budget} />}
{primaryTab === 'activity' && <AgentActivityTab agent={agent} ... />}
{primaryTab === 'operations' && <AgentOperationsTab agent={agent} opsSubTab={opsSubTab} ... />}
{primaryTab === 'system' && <AgentSystemTab agent={agent} sysSubTab={sysSubTab} ... />}
```

- [ ] **Step 6: Run full test suite**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vitest run`
Expected: PASS

- [ ] **Step 7: Verify build**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vite build`
Expected: Build succeeds

- [ ] **Step 8: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/screens/agents/
git commit -m "refactor: extract AgentDetail tab content into sub-components

AgentDetail.tsx is now a tab router (~200 lines) instead of a god file (770 lines).
Tab content lives in AgentOverviewTab, AgentActivityTab, AgentOperationsTab, AgentSystemTab."
```

---

### Task 7: Extract inline tabs from Admin.tsx + lazy load

Admin.tsx has Trust, Policy, and Danger Zone tabs rendered inline (200 lines). It also eagerly imports 13 tab components. Fix both.

**Files:**
- Create: `src/app/screens/admin/TrustTab.tsx`
- Create: `src/app/screens/admin/PolicyTab.tsx`
- Create: `src/app/screens/admin/DangerZoneTab.tsx`
- Modify: `src/app/screens/Admin.tsx`

- [ ] **Step 1: Create admin subdirectory**

Run: `mkdir -p /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web/src/app/screens/admin`

- [ ] **Step 2: Create TrustTab.tsx**

Extract lines 245-343 from Admin.tsx. The component receives `agents`, `agentsLoading`, `onTrust` as props. Move `TrustMeter` and `TRUST_DESCRIPTIONS` into this file.

```tsx
// src/app/screens/admin/TrustTab.tsx
import { Circle } from 'lucide-react';
import { Button } from '../../components/ui/button';
import type { Agent } from '../../types';

const TRUST_DESCRIPTIONS: Record<number, { label: string; description: string }> = {
  1: { label: 'Minimal', description: 'Read-only access, no external actions' },
  2: { label: 'Restricted', description: 'Limited tool use, supervised execution' },
  3: { label: 'Standard', description: 'Normal agent operations within policy' },
  4: { label: 'Elevated', description: 'Extended capabilities, reduced restrictions' },
  5: { label: 'Autonomous', description: 'Full autonomous operation including destructive actions' },
};

function TrustMeter({ level }: { level: number }) {
  return (
    <div className="flex items-center gap-1">
      {[1, 2, 3, 4, 5].map((i) => (
        <Circle key={i} className={`w-2 h-2 ${i <= level ? 'fill-slate-400 text-slate-400' : 'text-muted-foreground/70'}`} />
      ))}
    </div>
  );
}

interface TrustTabProps {
  agents: Agent[];
  agentsLoading: boolean;
  trustError: string | null;
  onTrust: (agentName: string, action: 'elevate' | 'demote') => void;
}

export function TrustTab({ agents, agentsLoading, trustError, onTrust }: TrustTabProps) {
  // Paste lines 246-343 from Admin.tsx
}
```

- [ ] **Step 3: Create PolicyTab.tsx**

Extract lines 352-391. Receives `agents`, `policyAgent`, `onPolicyAgentChange`, `policyData`, `policyLoading`, `policyError`, `onValidate`, `validating`.

```tsx
// src/app/screens/admin/PolicyTab.tsx
import { Button } from '../../components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../components/ui/select';
import { JsonView } from '../../components/JsonView';
import type { Agent } from '../../types';

interface PolicyTabProps {
  agents: Agent[];
  policyAgent: string;
  onPolicyAgentChange: (agent: string) => void;
  policyData: any;
  policyLoading: boolean;
  policyError: string | null;
  onValidate: () => void;
  validating: boolean;
}

export function PolicyTab(props: PolicyTabProps) {
  // Paste lines 353-391 from Admin.tsx
}
```

- [ ] **Step 4: Create DangerZoneTab.tsx**

Extract lines 393-421. Receives `onDestroy`, `destroying`.

```tsx
// src/app/screens/admin/DangerZoneTab.tsx
import { AlertTriangle } from 'lucide-react';
import { Button } from '../../components/ui/button';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { useState } from 'react';

interface DangerZoneTabProps {
  onDestroy: () => Promise<void>;
  destroying: boolean;
}

export function DangerZoneTab({ onDestroy, destroying }: DangerZoneTabProps) {
  const [showConfirm, setShowConfirm] = useState(false);
  // Paste lines 393-433 from Admin.tsx
  // Move ConfirmDialog into this component
}
```

- [ ] **Step 5: Update Admin.tsx — use new components and add lazy loading**

Replace static imports with `React.lazy()` for all delegated tabs:

```tsx
import { lazy, Suspense } from 'react';
import { TrustTab } from './admin/TrustTab';
import { PolicyTab } from './admin/PolicyTab';
import { DangerZoneTab } from './admin/DangerZoneTab';

const Infrastructure = lazy(() => import('./Infrastructure').then(m => ({ default: m.Infrastructure })));
const Hub = lazy(() => import('./Hub').then(m => ({ default: m.Hub })));
const Intake = lazy(() => import('./Intake').then(m => ({ default: m.Intake })));
const Knowledge = lazy(() => import('./Knowledge').then(m => ({ default: m.Knowledge })));
const Capabilities = lazy(() => import('./Capabilities').then(m => ({ default: m.Capabilities })));
const Usage = lazy(() => import('./Usage').then(m => ({ default: m.Usage })));
const Presets = lazy(() => import('./Presets').then(m => ({ default: m.Presets })));
const Events = lazy(() => import('./Events').then(m => ({ default: m.Events })));
const Webhooks = lazy(() => import('./Webhooks').then(m => ({ default: m.Webhooks })));
const Notifications = lazy(() => import('./Notifications').then(m => ({ default: m.Notifications })));
const AdminAudit = lazy(() => import('./AdminAudit').then(m => ({ default: m.AdminAudit })));
const AdminDoctor = lazy(() => import('./AdminDoctor').then(m => ({ default: m.AdminDoctor })));
const AdminEgress = lazy(() => import('./AdminEgress').then(m => ({ default: m.AdminEgress })));
```

Wrap each `TabsContent` child in `<Suspense fallback={<div className="text-sm text-muted-foreground text-center py-8">Loading...</div>}>`.

Remove inline Trust/Policy/Danger Zone JSX. Replace with:
```tsx
<TabsContent value="trust">
  <TrustTab agents={agents} agentsLoading={agentsLoading} trustError={trustError} onTrust={handleTrust} />
</TabsContent>
<TabsContent value="policy">
  <PolicyTab agents={agents} policyAgent={policyAgent} onPolicyAgentChange={handlePolicyAgentChange}
    policyData={policyData} policyLoading={policyLoading} policyError={policyError}
    onValidate={handleValidatePolicy} validating={validating} />
</TabsContent>
<TabsContent value="danger">
  <DangerZoneTab onDestroy={handleDestroyAll} destroying={destroying} />
</TabsContent>
```

Remove `TrustMeter`, `TRUST_DESCRIPTIONS`, `showDestroyConfirm`, and `ConfirmDialog` from Admin.tsx.

- [ ] **Step 6: Run full test suite**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vitest run`
Expected: PASS (Admin.test.tsx tests tab switching with MSW, lazy loading is transparent)

Note: If Suspense causes test issues, add `await waitFor(...)` around tab content assertions.

- [ ] **Step 7: Verify build**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vite build`
Expected: Build succeeds

- [ ] **Step 8: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/screens/admin/ src/app/screens/Admin.tsx
git commit -m "refactor: extract inline admin tabs + lazy-load all tab components

Admin.tsx is now ~150 lines — down from 436.
Trust, Policy, DangerZone are proper components.
All 13 delegated tab screens are lazy-loaded."
```

---

### Task 8: Extract ComponentInfoDialog and DeploySection from Hub.tsx

Hub.tsx has a component info modal and deploy section that can be extracted.

**Files:**
- Create: `src/app/screens/hub/ComponentInfoDialog.tsx`
- Create: `src/app/screens/hub/DeploySection.tsx`
- Modify: `src/app/screens/Hub.tsx`

- [ ] **Step 1: Create hub subdirectory**

Run: `mkdir -p /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web/src/app/screens/hub`

- [ ] **Step 2: Create ComponentInfoDialog.tsx**

Extract the info modal (the section that shows detailed component info when `infoTarget` is set). It receives `component`, `data`, `loading`, `onClose`.

- [ ] **Step 3: Create DeploySection.tsx**

Extract the deploy/teardown section (pack deployment, upgrade banner, update sources). It receives the deploy state and handlers.

- [ ] **Step 4: Update Hub.tsx**

Import and use the new components. Hub.tsx should drop to ~350 lines.

- [ ] **Step 5: Run full test suite**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vitest run`
Expected: PASS

- [ ] **Step 6: Verify build**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npx vite build`
Expected: Build succeeds

- [ ] **Step 7: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web
git add src/app/screens/hub/ src/app/screens/Hub.tsx
git commit -m "refactor: extract ComponentInfoDialog and DeploySection from Hub

Hub.tsx drops from 584 to ~350 lines."
```

---

## Final Verification

After all 8 tasks are complete:

- [ ] Run `npx vitest run` — all tests pass
- [ ] Run `npx vite build` — clean build
- [ ] Run `npx agency-web dev` — smoke test the app manually
- [ ] Verify file sizes with `find src -name "*.tsx" -o -name "*.ts" | xargs wc -l | sort -rn | head 20` — no file over 400 lines

### Expected line count changes:

| File | Before | After |
|------|--------|-------|
| Channels.tsx | 910 | ~350 |
| KnowledgeExplorer.tsx | 1,001 | ~100 |
| AgentDetail.tsx | 770 | ~200 |
| Admin.tsx | 436 | ~150 |
| Hub.tsx | 584 | ~350 |

New files created: ~15 focused modules averaging 100-200 lines each.
