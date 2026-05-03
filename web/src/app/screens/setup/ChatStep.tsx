import { useState, useEffect, useRef, useCallback } from 'react';
import { Loader2, Send } from 'lucide-react';
import { api } from '../../lib/api';
import { socket } from '../../lib/ws';
import { Button } from '../../components/ui/button';
import { AgencyMessage, AgencyMessageAvatar } from '../../components/chat/AgencyMessage';
import { formatMessageTime } from '../../lib/time';
import type { Message } from '../../types';

interface ChatMessage {
  id: string;
  author: string;
  content: string;
  timestamp: string;
  flags: Record<string, boolean>;
}

interface ChatStepProps {
  agentName: string;
  operatorName?: string;
  onFinish: (channelName?: string) => void;
  onBack: () => void;
  initialAgentReady?: boolean;
  agentReadyPolls?: number;
  agentReadyPollDelayMs?: number;
}

const FIRST_TASK_PROMPTS = [
  {
    label: 'Explain Agency',
    prompt: 'Give me a short tour of Agency. What should I use first, and what should I avoid until I understand the system better?',
  },
  {
    label: 'Check My Setup',
    prompt: 'Check whether my local Agency setup looks healthy. Tell me what you can verify from inside Agency and what I should check manually.',
  },
  {
    label: 'Plan A First Task',
    prompt: 'Help me turn a real goal into a safe first Agency task. Ask me for the goal, then suggest the smallest useful next step.',
  },
];

const INITIAL_PROMPT_RETRIES = 5;
const INITIAL_PROMPT_RETRY_DELAY_MS = 1500;
const AGENT_READY_POLLS = 120;
const AGENT_READY_POLL_DELAY_MS = 3000;

function wait(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function indicatorMessage(agentName: string): Message {
  return {
    id: `setup-indicator-${agentName}`,
    channelId: 'setup',
    author: agentName,
    displayAuthor: agentName,
    isAgent: true,
    isSystem: false,
    timestamp: '',
    content: '',
    flag: null,
  };
}

export function ChatStep({
  agentName,
  operatorName,
  onFinish,
  onBack,
  initialAgentReady = false,
  agentReadyPolls = AGENT_READY_POLLS,
  agentReadyPollDelayMs = AGENT_READY_POLL_DELAY_MS,
}: ChatStepProps) {
  const [channelName, setChannelName] = useState<string | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState('');
  const [loading, setLoading] = useState(true);
  const [sending, setSending] = useState(false);
  const [agentTyping, setAgentTyping] = useState(false);
  const [agentReady, setAgentReady] = useState(initialAgentReady);
  const [agentReadyError, setAgentReadyError] = useState('');
  const [error, setError] = useState('');
  const [agentPollAttempt, setAgentPollAttempt] = useState(0);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const userScrolledUpRef = useRef(false);
  const lastAgentMsgCountRef = useRef(0);
  const sentInitialPromptRef = useRef(false);
  const agentReadyRef = useRef(initialAgentReady);

  const markAgentReady = useCallback(() => {
    if (agentReadyRef.current) return;
    agentReadyRef.current = true;
    setAgentReady(true);
    setAgentReadyError('');
  }, []);

  const scrollToBottom = useCallback(() => {
    if (!userScrolledUpRef.current && scrollContainerRef.current) {
      scrollContainerRef.current.scrollTop = scrollContainerRef.current.scrollHeight;
    }
  }, []);

  const handleScroll = useCallback(() => {
    const el = scrollContainerRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    userScrolledUpRef.current = distanceFromBottom > 60;
  }, []);

  const isOperatorMsg = (author: string) =>
    author === 'operator' || author === '_operator' || author === operatorName;

  // Prefer lifecycle events for readiness; keep polling as a fallback for
  // missed events or socket reconnects.
  useEffect(() => {
    if (!agentName) return;
    let cancelled = false;
    agentReadyRef.current = initialAgentReady;
    setAgentReady(initialAgentReady);
    setAgentReadyError('');
    if (initialAgentReady) return;

    const unsub = socket.on('agent_status', (event: any) => {
      if (event?.agent !== agentName) return;
      if (event?.status === 'running') {
        markAgentReady();
      }
    });

    const poll = async () => {
      for (let i = 0; i < agentReadyPolls; i++) {
        if (cancelled) return;
        try {
          const agent = await api.agents.show(agentName);
          if (agent.status === 'running') {
            markAgentReady();
            return;
          }
        } catch { /* agent may not exist yet */ }
        await wait(agentReadyPollDelayMs);
      }
      if (!cancelled) {
        setAgentReadyError(`${agentName} did not report ready. It may still be building, or startup may have failed.`);
      }
    };
    poll();
    return () => {
      cancelled = true;
      unsub();
    };
  }, [agentName, agentPollAttempt, agentReadyPolls, agentReadyPollDelayMs, initialAgentReady, markAgentReady]);

  // Find or create DM channel (can happen in parallel with agent polling)
  useEffect(() => {
    const setup = async () => {
      try {
        const channels = await api.channels.list();
        const dmName = `dm-${agentName}`;
        const existing = (channels || []).find((c: any) => c.name === dmName || c.name === agentName);
        if (existing) {
          setChannelName(existing.name);
        } else {
          const ensured = await api.agents.ensureDM(agentName);
          setChannelName(ensured.channel || dmName);
        }
      } catch (e: any) {
        setError(e.message || 'Could not open chat');
      } finally {
        setLoading(false);
      }
    };
    if (agentName) setup();
  }, [agentName]);

  // Send initial welcome prompt once channel is ready AND agent is running
  useEffect(() => {
    if (!channelName || !agentReady || sentInitialPromptRef.current) return;
    sentInitialPromptRef.current = true;
    let cancelled = false;

    const greeting = `Hey ${agentName}, I just set up Agency. What can you help me with?`;

    const sendInitialPrompt = async () => {
      setAgentTyping(true);
      for (let attempt = 1; attempt <= INITIAL_PROMPT_RETRIES; attempt++) {
        try {
          await api.channels.send(channelName, greeting);
          if (cancelled) return;
          setError('');
          setMessages((prev) => {
            if (prev.some((message) => isOperatorMsg(message.author) && message.content === greeting)) {
              return prev;
            }
            return [...prev, {
              id: `initial-${Date.now()}`,
              author: operatorName || 'operator',
              content: greeting,
              timestamp: formatMessageTime(new Date().toISOString()),
              flags: {},
            }];
          });
          return;
        } catch (e: any) {
          if (attempt === INITIAL_PROMPT_RETRIES) {
            if (!cancelled) {
              setAgentTyping(false);
              setError(e.message || 'Failed to send the initial prompt');
            }
            return;
          }
          await wait(INITIAL_PROMPT_RETRY_DELAY_MS);
        }
      }
    };

    sendInitialPrompt();
    return () => { cancelled = true; };
  }, [channelName, agentReady, agentName, operatorName]);

  // Poll for new messages
  useEffect(() => {
    if (!channelName) return;

    const loadMessages = async () => {
      try {
        const raw = await api.channels.read(channelName, 50);
        const mapped: ChatMessage[] = (raw || []).map((m: any) => ({
          id: m.id || m.timestamp,
          author: m.author || 'unknown',
          content: m.content || '',
          timestamp: formatMessageTime(m.timestamp || ''),
          flags: m.flags || {},
        }));
        const visible = mapped.filter(m => !m.flags.system);

        // Check for new agent messages to clear typing indicator
        const agentMsgCount = visible.filter(m => !isOperatorMsg(m.author)).length;
        if (agentMsgCount > lastAgentMsgCountRef.current) {
          setAgentTyping(false);
          lastAgentMsgCountRef.current = agentMsgCount;
        }

        setMessages(visible);
        // Use requestAnimationFrame so DOM has updated before scrolling
        requestAnimationFrame(scrollToBottom);
      } catch { /* ignore polling errors */ }
    };

    loadMessages();
    pollRef.current = setInterval(loadMessages, 1500);

    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [channelName, scrollToBottom]);

  const handleSend = async () => {
    if (!input.trim() || !channelName || sending) return;
    const content = input.trim();
    setInput('');
    setSending(true);
    setAgentTyping(true);
    try {
      await api.channels.send(channelName, content);
      // Optimistically add the message
      setMessages((prev) => [...prev, {
        id: `pending-${Date.now()}`,
        author: 'operator',
        content,
        timestamp: formatMessageTime(new Date().toISOString()),
        flags: {},
      }]);
      userScrolledUpRef.current = false;
      requestAnimationFrame(scrollToBottom);
    } catch (e: any) {
      setError(e.message || 'Failed to send');
    } finally {
      setSending(false);
    }
  };

  const finishSetup = () => {
    onFinish(channelName ?? undefined);
  };

  if (loading) {
    return (
      <div className="text-center space-y-4">
        <h2 className="text-2xl font-semibold text-foreground">Opening chat...</h2>
        <Loader2 className="w-5 h-5 animate-spin mx-auto text-muted-foreground" />
      </div>
    );
  }

  if (error && !channelName) {
    return (
      <div className="text-center space-y-4">
        <h2 className="text-2xl font-semibold text-foreground">Chat</h2>
        <p className="text-sm text-red-400">{error}</p>
        <Button onClick={finishSetup}>Finish Setup</Button>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="text-center space-y-2">
        <h2 className="text-2xl font-semibold text-foreground">Talk to {agentName}</h2>
        <p className="text-muted-foreground text-sm">
          {agentReady
            ? `${operatorName ? `${operatorName}, your` : 'Your'} agent is ready to chat.`
            : `Waiting for ${agentName} to finish starting.`}
        </p>
      </div>

      <div className="rounded-lg border border-border bg-card/70 p-3 space-y-3">
        <div>
          <div className="text-sm font-medium text-foreground">Try a first task</div>
          <p className="text-xs text-muted-foreground">
            Pick a prompt, edit it if needed, then send it. You can also finish setup and keep this DM open in Channels.
          </p>
        </div>
        <div className="grid gap-2 sm:grid-cols-3">
          {FIRST_TASK_PROMPTS.map((item) => (
            <button
              key={item.label}
              type="button"
              onClick={() => setInput(item.prompt)}
              className="rounded-md border border-border bg-background px-3 py-2 text-left text-xs text-foreground transition-colors hover:bg-accent"
            >
              {item.label}
            </button>
          ))}
        </div>
      </div>

      {/* Chat area */}
      <div className="border border-border rounded-lg bg-card overflow-hidden flex flex-col" style={{ height: '380px' }}>
        <div
          ref={scrollContainerRef}
          onScroll={handleScroll}
          className="flex-1 overflow-y-auto p-4 flex flex-col justify-end min-h-0"
        >
          <div className="space-y-1">
            {messages.map((msg) => {
              const isMe = isOperatorMsg(msg.author);
              const displayAuthor = isMe ? (operatorName || 'operator') : msg.author;
              const message: Message = {
                id: msg.id,
                channelId: channelName || 'setup',
                author: msg.author,
                displayAuthor,
                isAgent: !isMe,
                isSystem: false,
                timestamp: msg.timestamp,
                content: msg.content,
                flag: null,
              };
              return (
                <AgencyMessage key={msg.id} message={message} agentStatus={message.isAgent ? 'running' : undefined} showReplyButton={false} />
              );
            })}

            {/* Agent starting indicator */}
            {!agentReady && !agentReadyError && (
              <div className="flex gap-3 py-1.5">
                <AgencyMessageAvatar message={indicatorMessage(agentName)} />
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Loader2 className="w-3 h-3 animate-spin" />
                  Starting {agentName}...
                </div>
              </div>
            )}

            {/* Startup recovery */}
            {agentReadyError && (
              <div className="rounded-lg border border-amber-900/50 bg-amber-950/30 p-4 text-sm text-amber-100">
                <div className="font-medium">Agent is not ready yet</div>
                <p className="mt-1 text-xs text-amber-100/80">{agentReadyError}</p>
                <div className="mt-3 flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setAgentPollAttempt((attempt) => attempt + 1)}
                  >
                    Check Again
                  </Button>
                  <Button size="sm" variant="ghost" onClick={finishSetup}>
                    Open Channels
                  </Button>
                </div>
              </div>
            )}

            {/* Typing indicator */}
            {agentReady && agentTyping && (
              <div className="flex gap-3 py-1.5">
                <AgencyMessageAvatar message={indicatorMessage(agentName)} agentStatus="running" />
                <div className="flex items-center gap-2">
                  <code className="text-sm font-medium text-foreground">{agentName}</code>
                  <div className="flex items-center gap-0.5">
                    <span className="w-1.5 h-1.5 rounded-full bg-muted-foreground animate-bounce" style={{ animationDelay: '0ms' }} />
                    <span className="w-1.5 h-1.5 rounded-full bg-muted-foreground animate-bounce" style={{ animationDelay: '150ms' }} />
                    <span className="w-1.5 h-1.5 rounded-full bg-muted-foreground animate-bounce" style={{ animationDelay: '300ms' }} />
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>

        {/* Input */}
        <div className="border-t border-border p-3 flex gap-2">
          <input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && !e.shiftKey && handleSend()}
            placeholder={agentReady ? 'What can you help me with?' : `Waiting for ${agentName} to start...`}
            className="flex-1 bg-background border border-border rounded px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/60 focus:outline-none focus:border-foreground/30"
            disabled={sending || !agentReady}
          />
          <Button size="icon" onClick={handleSend} disabled={!input.trim() || sending || !agentReady}>
            {sending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Send className="w-4 h-4" />}
          </Button>
        </div>
      </div>

      {/* Navigation */}
      <div className="flex items-center justify-between pt-2">
        <button onClick={onBack} className="text-sm text-muted-foreground hover:text-foreground transition-colors">
          Back
        </button>
        <Button onClick={finishSetup}>Finish Setup</Button>
      </div>
    </div>
  );
}
