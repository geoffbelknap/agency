import { useState, useEffect, useRef, useCallback } from 'react';
import { Loader2, Send } from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { api } from '../../lib/api';
import { Button } from '../../components/ui/button';
import { ALLOWED_ELEMENTS, markdownComponents } from '../../components/chat/StructuredOutput';

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
  onFinish: () => void;
  onBack: () => void;
}

export function ChatStep({ agentName, operatorName, onFinish, onBack }: ChatStepProps) {
  const [channelName, setChannelName] = useState<string | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState('');
  const [loading, setLoading] = useState(true);
  const [sending, setSending] = useState(false);
  const [agentTyping, setAgentTyping] = useState(false);
  const [agentReady, setAgentReady] = useState(false);
  const [error, setError] = useState('');
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const userScrolledUpRef = useRef(false);
  const lastAgentMsgCountRef = useRef(0);
  const sentInitialPromptRef = useRef(false);

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

  // Poll agent status until running
  useEffect(() => {
    if (!agentName) return;
    let cancelled = false;
    const poll = async () => {
      for (let i = 0; i < 40; i++) { // up to ~60s
        if (cancelled) return;
        try {
          const agent = await api.agents.show(agentName);
          if (agent.status === 'running') {
            setAgentReady(true);
            return;
          }
        } catch { /* agent may not exist yet */ }
        await new Promise(r => setTimeout(r, 1500));
      }
      // Timed out — proceed anyway, agent may still come up
      setAgentReady(true);
    };
    poll();
    return () => { cancelled = true; };
  }, [agentName]);

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
          await api.channels.create(dmName);
          setChannelName(dmName);
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

    const greeting = `Hey ${agentName}, I just set up Agency. What can you help me with?`;

    api.channels.send(channelName, greeting).catch(() => {});
    setAgentTyping(true);
  }, [channelName, agentReady, agentName]);

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
          timestamp: m.timestamp || '',
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
        timestamp: new Date().toISOString(),
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
        <Button onClick={onFinish}>Finish Setup</Button>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="text-center space-y-2">
        <h2 className="text-2xl font-semibold text-foreground">Talk to {agentName}</h2>
        <p className="text-muted-foreground text-sm">
          {operatorName ? `${operatorName}, your` : 'Your'} agent is ready to chat.
        </p>
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
              return (
                <div key={msg.id} className="flex gap-3 py-1.5">
                  <div className={`w-8 h-8 rounded flex items-center justify-center flex-shrink-0 ${
                    isMe ? 'bg-border' : 'bg-primary'
                  }`}>
                    <span className="text-xs font-semibold text-white uppercase">
                      {displayAuthor.charAt(0)}
                    </span>
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-0.5">
                      <code className="text-sm font-medium text-foreground">{displayAuthor}</code>
                      {!isMe && (
                        <span className="text-xs bg-accent text-accent-foreground px-1.5 py-0.5 rounded">AGENT</span>
                      )}
                    </div>
                    {isMe ? (
                      <div className="text-sm text-foreground/80 whitespace-pre-wrap">{msg.content}</div>
                    ) : (
                      <div className="text-sm text-foreground/80 prose prose-gray dark:prose-invert prose-sm max-w-none prose-p:my-1">
                        <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents} allowedElements={ALLOWED_ELEMENTS} unwrapDisallowed>
                          {msg.content}
                        </ReactMarkdown>
                      </div>
                    )}
                  </div>
                </div>
              );
            })}

            {/* Agent starting indicator */}
            {!agentReady && (
              <div className="flex gap-3 py-1.5">
                <div className="w-8 h-8 rounded flex items-center justify-center flex-shrink-0 bg-primary">
                  <span className="text-xs font-semibold text-white uppercase">{agentName.charAt(0)}</span>
                </div>
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Loader2 className="w-3 h-3 animate-spin" />
                  Starting {agentName}...
                </div>
              </div>
            )}

            {/* Typing indicator */}
            {agentReady && agentTyping && (
              <div className="flex gap-3 py-1.5">
                <div className="w-8 h-8 rounded flex items-center justify-center flex-shrink-0 bg-primary">
                  <span className="text-xs font-semibold text-white uppercase">{agentName.charAt(0)}</span>
                </div>
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
        <Button onClick={onFinish}>Finish Setup</Button>
      </div>
    </div>
  );
}
