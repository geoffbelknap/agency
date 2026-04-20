import { useRef, useEffect, useLayoutEffect, useCallback, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { AgencyMessage } from './AgencyMessage';
import { ScrollArea } from '../ui/scroll-area';
import { Skeleton } from '../ui/skeleton';
import type { Message } from '../../types';

interface MessageListProps {
  messages: Message[];
  loading: boolean;
  agentStatuses?: Record<string, string>;
  processingAgents?: string[];
  agentActivity?: Record<string, string>;
  onReply?: (message: Message) => void;
  onEdit?: (message: Message, newContent: string) => void;
  onDelete?: (message: Message) => void;
  onReact?: (message: Message, emoji: string) => void;
  onUnreact?: (message: Message, emoji: string) => void;
  scrollToMessageId?: string;
  hasMore?: boolean;
  onLoadMore?: () => void;
  loadingMore?: boolean;
  onAgentClick?: (agentName: string) => void;
}

function MessageSkeletons() {
  return (
    <div className="space-y-5 px-7 py-6">
      {[1, 2, 3, 4].map((i) => (
        <div key={i} className="flex gap-4">
          <Skeleton className="h-9 w-9 shrink-0 rounded-xl" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-3 w-28" />
            <Skeleton className="h-3 w-full" />
            <Skeleton className="h-3 w-2/3" />
          </div>
        </div>
      ))}
    </div>
  );
}

export function MessageList({ messages, loading, agentStatuses, processingAgents, agentActivity, onReply, onEdit, onDelete, onReact, onUnreact, scrollToMessageId, hasMore, onLoadMore, loadingMore, onAgentClick }: MessageListProps) {
  const scrollAreaRef = useRef<HTMLDivElement>(null);
  const prevLastIdRef = useRef<string | undefined>(undefined);
  const firstRenderRef = useRef(true);
  const isAtBottomRef = useRef(true);
  const preservePositionOnPrependRef = useRef(false);
  const prevScrollHeightRef = useRef(0);
  const lastScrollTopRef = useRef(0);
  const loadMoreTriggeredAtTopRef = useRef(false);
  const [showLoadingEarlier, setShowLoadingEarlier] = useState(false);

  const getViewport = useCallback((): HTMLDivElement | null => {
    if (!scrollAreaRef.current) return null;
    return scrollAreaRef.current.querySelector('[data-slot="scroll-area-viewport"]');
  }, []);

  const requestLoadMore = useCallback((preserveScrollPosition: boolean, visibleIndicator: boolean) => {
    if (!onLoadMore || loading || loadingMore || !hasMore) return;
    const viewport = getViewport();
    if (preserveScrollPosition && viewport) {
      preservePositionOnPrependRef.current = true;
      prevScrollHeightRef.current = viewport.scrollHeight;
    }
    setShowLoadingEarlier(visibleIndicator);
    onLoadMore();
  }, [onLoadMore, loading, loadingMore, hasMore, getViewport]);

  useEffect(() => {
    const viewport = getViewport();
    if (!viewport) return;

    const updateScrollState = () => {
      const currentScrollTop = viewport.scrollTop;
      const distanceToBottom = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight;
      isAtBottomRef.current = distanceToBottom < 24;

      const isNearTop = currentScrollTop <= 120;

      if (!isNearTop) {
        loadMoreTriggeredAtTopRef.current = false;
      }

      lastScrollTopRef.current = currentScrollTop;
    };

    const handleScroll = () => updateScrollState();

    const handleWheel = (event: WheelEvent) => {
      if (firstRenderRef.current) return;
      const isScrollingUp = event.deltaY < 0;
      const isScrollingDown = event.deltaY > 0;
      const isNearTop = viewport.scrollTop <= 120;
      const isAtBottom = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight < 4;

      if ((isNearTop && isScrollingUp) || (isAtBottom && isScrollingDown)) {
        event.preventDefault();
        event.stopPropagation();
      }

      if (isNearTop && isScrollingUp && !loadMoreTriggeredAtTopRef.current) {
        loadMoreTriggeredAtTopRef.current = true;
        requestLoadMore(true, true);
      }
    };

    updateScrollState();
    viewport.addEventListener('scroll', handleScroll);
    viewport.addEventListener('wheel', handleWheel, { passive: false });
    return () => {
      viewport.removeEventListener('scroll', handleScroll);
      viewport.removeEventListener('wheel', handleWheel);
    };
  }, [getViewport, requestLoadMore]);

  useEffect(() => {
    if (!loadingMore) {
      setShowLoadingEarlier(false);
    }
  }, [loadingMore]);

  useLayoutEffect(() => {
    const viewport = getViewport();
    if (!viewport) return;

    if (preservePositionOnPrependRef.current) {
      const delta = viewport.scrollHeight - prevScrollHeightRef.current;
      viewport.scrollTop += Math.max(0, delta);
      lastScrollTopRef.current = viewport.scrollTop;
      preservePositionOnPrependRef.current = false;
    }

    const lastId = messages.length > 0 ? messages[messages.length - 1].id : undefined;

    if (firstRenderRef.current) {
      viewport.scrollTop = viewport.scrollHeight;
      lastScrollTopRef.current = viewport.scrollTop;
      firstRenderRef.current = false;
      prevLastIdRef.current = lastId;
      return;
    }

    if (!loading && lastId && lastId !== prevLastIdRef.current && isAtBottomRef.current) {
      viewport.scrollTop = viewport.scrollHeight;
      lastScrollTopRef.current = viewport.scrollTop;
    }

    prevLastIdRef.current = lastId;
  }, [messages, loading, getViewport]);

  useEffect(() => {
    if (loading || loadingMore || !hasMore) return;
    const viewport = getViewport();
    if (!viewport) return;
    if (viewport.scrollHeight <= viewport.clientHeight + 8) {
      requestLoadMore(false, false);
    }
  }, [messages, loading, loadingMore, hasMore, requestLoadMore, getViewport]);

  useEffect(() => {
    if (scrollToMessageId) {
      document.getElementById(scrollToMessageId)?.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }
  }, [scrollToMessageId]);

  useEffect(() => {
    if ((processingAgents ?? []).length > 0 && isAtBottomRef.current) {
      const viewport = getViewport();
      if (viewport) {
        viewport.scrollTop = viewport.scrollHeight;
      }
    }
  }, [processingAgents, getViewport]);

  return (
    <ScrollArea ref={scrollAreaRef} className="scrollbar-none min-h-0 flex-1 overscroll-contain" style={{ background: 'var(--warm)' }}>
      <div style={{ padding: '24px 28px 28px' }}>
        {loadingMore && !loading && showLoadingEarlier && (
          <div className="mb-4 flex justify-center text-xs" style={{ color: 'var(--ink-muted)' }}>
            <Loader2 className="mr-1 h-3 w-3 animate-spin" />
            Loading earlier messages...
          </div>
        )}
        {loading ? (
          <MessageSkeletons />
        ) : messages.length === 0 ? (
          <div className="flex min-h-40 items-center justify-center text-center" style={{ padding: 24 }}>
            <div>
              <div style={{ fontSize: 15, color: 'var(--ink)' }}>No messages yet</div>
              <div className="mt-1 text-xs" style={{ color: 'var(--ink-muted)' }}>Start with a direct instruction, question, or decision.</div>
            </div>
          </div>
        ) : (
          <div className="space-y-[14px]">
            {messages.map((msg) => (
              <div key={msg.id} id={msg.id}>
                <AgencyMessage
                  message={msg}
                  agentStatus={agentStatuses?.[msg.author]}
                  onReply={onReply}
                  onEdit={onEdit}
                  onDelete={onDelete}
                  onReact={onReact}
                  onUnreact={onUnreact}
                  onAgentClick={onAgentClick}
                />
              </div>
            ))}
          </div>
        )}
      </div>
    </ScrollArea>
  );
}
