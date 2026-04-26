import { useState, useEffect } from 'react';
import { socket } from '../lib/ws';
import { formatMessageTime } from '../lib/time';
import type { Message, Channel } from '../types';
import type { RawMessage } from '../lib/api';

export const SYSTEM_SENDERS = new Set(['_platform', '_system']);

export interface UseChannelSocketOptions {
  selectedChannelName: string | undefined;
  onAppendMessage: (msg: Message) => void;
  onUnreadIncrement: (channelName: string) => void;
  onTaskComplete?: (agent: string) => void;
}

export interface UseChannelSocketResult {
  typingAgents: string[];
  processingAgents: string[];
  agentActivity: Record<string, string>;
  wsConnected: boolean;
  setProcessingAgents: React.Dispatch<React.SetStateAction<string[]>>;
}

export function useChannelSocket(
  options: UseChannelSocketOptions,
  mapRawMessages: (data: RawMessage[], channelName: string) => Message[],
): UseChannelSocketResult {
  const { selectedChannelName, onAppendMessage, onUnreadIncrement, onTaskComplete } = options;

  const [typingAgents, setTypingAgents] = useState<string[]>([]);
  const [processingAgents, setProcessingAgents] = useState<string[]>([]);
  const [agentActivity, setAgentActivity] = useState<Record<string, string>>({});
  const [wsConnected, setWsConnected] = useState(socket.connected);

  // Reset indicators when channel changes
  useEffect(() => {
    setTypingAgents([]);
    setProcessingAgents([]);
    setAgentActivity({});
  }, [selectedChannelName]);

  // Track WebSocket connection state
  useEffect(() => {
    const unsub = socket.onConnectionChange(setWsConnected);
    return () => { unsub(); };
  }, []);

  // WebSocket real-time updates — append messages directly for instant display
  useEffect(() => {
    const unsub = socket.on('message', (event: any) => {
      const msgChannel: string | undefined = event.message?.channel;
      if (msgChannel === selectedChannelName) {
        // Clear typing/processing indicators for the agent that just sent a message
        // (but not for system messages — the agent is still working)
        const author = event.message?.author;
        if (author && !SYSTEM_SENDERS.has(author)) {
          setTypingAgents((prev) => prev.filter((a) => a !== author));
          setProcessingAgents((prev) => prev.filter((a) => a !== author));
          setAgentActivity((prev) => { const next = { ...prev }; delete next[author]; return next; });
        }
        // Append the message directly from the WebSocket event
        const raw = event.message as RawMessage;
        if (raw) {
          const mapped = mapRawMessages([raw], msgChannel!);
          const newMsg = mapped[0];
          if (newMsg) {
            onAppendMessage(newMsg);
          }
        }
      } else if (msgChannel) {
        // Increment unread count for the channel the message arrived on
        onUnreadIncrement(msgChannel);
      }
    });
    return () => { unsub(); };
  }, [selectedChannelName, mapRawMessages, onAppendMessage, onUnreadIncrement]);

  // Agent error signals — inject inline error messages
  useEffect(() => {
    const unsub = socket.on('agent_signal_error', (event: any) => {
      const agent: string = event.agent;
      const data = event.data || {};
      if (!agent || !selectedChannelName) return;

      // Clear typing/processing indicators for the errored agent
      setTypingAgents((prev) => prev.filter((a) => a !== agent));
      setProcessingAgents((prev) => prev.filter((a) => a !== agent));

      const errorMsg: Message = {
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
      };

      onAppendMessage(errorMsg);
    });
    return () => { unsub(); };
  }, [selectedChannelName, onAppendMessage]);

  // Agent processing signals — show acknowledgment when agent picks up a message
  useEffect(() => {
    const timeouts = new Map<string, ReturnType<typeof setTimeout>>();

    const unsub = socket.on('agent_signal_processing', (event: any) => {
      const agent: string = event.agent;
      const channel: string = event.data?.channel;
      if (!agent || channel !== selectedChannelName) return;

      setProcessingAgents((prev) => (prev.includes(agent) ? prev : [...prev, agent]));

      // 60s safety timeout
      if (timeouts.has(agent)) clearTimeout(timeouts.get(agent)!);
      timeouts.set(
        agent,
        setTimeout(() => {
          setProcessingAgents((prev) => prev.filter((a) => a !== agent));
          timeouts.delete(agent);
        }, 60_000),
      );
    });

    return () => {
      unsub();
      timeouts.forEach((t) => clearTimeout(t));
    };
  }, [selectedChannelName]);

  // Agent activity signals — show what the agent is doing (e.g. "searching the web")
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

  // Agent task_complete signals — clear typing/processing indicators
  useEffect(() => {
    const unsub = socket.on('agent_signal_task_complete', (event: any) => {
      const agent: string = event.agent;
      if (!agent) return;
      setTypingAgents((prev) => prev.filter((a) => a !== agent));
      setProcessingAgents((prev) => prev.filter((a) => a !== agent));
      setAgentActivity((prev) => { const next = { ...prev }; delete next[agent]; return next; });
      onTaskComplete?.(agent);
    });
    return () => { unsub(); };
  }, [onTaskComplete]);

  // Agent task_accepted signals — agent acknowledged the task
  useEffect(() => {
    const unsub = socket.on('agent_signal_task_accepted', (event: any) => {
      const agent: string = event.agent;
      if (!agent) return;
      setProcessingAgents((prev) => (prev.includes(agent) ? prev : [...prev, agent]));
    });
    return () => { unsub(); };
  }, []);

  // Agent escalation signals — XPIA detection or constraint violations
  useEffect(() => {
    const unsub = socket.on('agent_signal_escalation', (event: any) => {
      const agent: string = event.agent;
      const data = event.data || {};
      if (!agent || !selectedChannelName) return;

      const warnMsg: Message = {
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
      };
      onAppendMessage(warnMsg);
    });
    return () => { unsub(); };
  }, [selectedChannelName, onAppendMessage]);

  // Agent self_halt signals — agent halted itself
  useEffect(() => {
    const unsub = socket.on('agent_signal_self_halt', (event: any) => {
      const agent: string = event.agent;
      const data = event.data || {};
      if (!agent || !selectedChannelName) return;

      setTypingAgents((prev) => prev.filter((a) => a !== agent));
      setProcessingAgents((prev) => prev.filter((a) => a !== agent));
      setAgentActivity((prev) => { const next = { ...prev }; delete next[agent]; return next; });

      const haltMsg: Message = {
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
      };
      onAppendMessage(haltMsg);
    });
    return () => { unsub(); };
  }, [selectedChannelName, onAppendMessage]);

  // Agent finding signals — noteworthy discovery
  useEffect(() => {
    const unsub = socket.on('agent_signal_finding', (event: any) => {
      const agent: string = event.agent;
      const data = event.data || {};
      if (!agent || !selectedChannelName) return;

      const findingMsg: Message = {
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
      };
      onAppendMessage(findingMsg);
    });
    return () => { unsub(); };
  }, [selectedChannelName, onAppendMessage]);

  // Track agent typing/thinking state
  useEffect(() => {
    const timeouts = new Map<string, ReturnType<typeof setTimeout>>();

    const unsub = socket.on('agent_status', (event: any) => {
      const agent: string = event.agent;
      const status: string = event.status;

      if (!agent) return;

      if (status === 'running') {
        setTypingAgents((prev) => (prev.includes(agent) ? prev : [...prev, agent]));

        // Auto-clear after 30 seconds as a safety valve
        if (timeouts.has(agent)) clearTimeout(timeouts.get(agent)!);
        timeouts.set(
          agent,
          setTimeout(() => {
            setTypingAgents((prev) => prev.filter((a) => a !== agent));
            timeouts.delete(agent);
          }, 30_000),
        );
      } else {
        // idle or halted — remove from typing set immediately
        setTypingAgents((prev) => prev.filter((a) => a !== agent));
        if (timeouts.has(agent)) {
          clearTimeout(timeouts.get(agent)!);
          timeouts.delete(agent);
        }
      }
    });

    return () => {
      unsub();
      timeouts.forEach((t) => clearTimeout(t));
    };
  }, []);

  return { typingAgents, processingAgents, agentActivity, wsConnected, setProcessingAgents };
}
