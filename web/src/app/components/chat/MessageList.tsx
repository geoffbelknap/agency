import { useRef, useEffect, useLayoutEffect, useCallback, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { AgencyMessage } from './AgencyMessage';
import { ScrollArea } from '../ui/scroll-area';
import { Skeleton } from '../ui/skeleton';
import { dateKey, formatDateLabel } from '../../lib/time';
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
    <div className="space-y-4 p-4">
      {[1, 2, 3, 4].map((i) => (
        <div key={i} className="flex gap-3">
          <Skeleton className="w-8 h-8 rounded flex-shrink-0" />
          <div className="flex-1 space-y-1.5">
            <Skeleton className="h-3 w-24" />
            <Skeleton className="h-3 w-full" />
            <Skeleton className="h-3 w-3/4" />
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

      // Prevent scroll from escaping the message list at boundaries
      if ((isNearTop && isScrollingUp) || (isAtBottom && isScrollingDown)) {
        event.preventDefault();
        event.stopPropagation();
      }

      if (isNearTop && isScrollingUp && !loadMoreTriggeredAtTopRef.current) {
        loadMoreTriggeredAtTopRef.current = true;
        requestLoadMore(true, true);
      }
    };

    // Initial measurement should never trigger loading-more side effects.
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

  // useLayoutEffect to scroll before paint — prevents flash of top content
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

  // If the latest chunk does not fill the viewport, pull older messages until it does.
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

  // Scroll to bottom when processing indicator appears
  useEffect(() => {
    if ((processingAgents ?? []).length > 0 && isAtBottomRef.current) {
      const viewport = getViewport();
      if (viewport) {
        viewport.scrollTop = viewport.scrollHeight;
      }
    }
  }, [processingAgents, getViewport]);

  return (
    <ScrollArea ref={scrollAreaRef} className="min-h-0 flex-1 overscroll-contain">
      <div className="p-4 space-y-1">
        {loadingMore && !loading && showLoadingEarlier && (
          <div className="flex justify-center pb-2 text-xs text-muted-foreground">
            <Loader2 className="mr-1 h-3 w-3 animate-spin" />
            Loading earlier messages...
          </div>
        )}
        {loading ? (
          <MessageSkeletons />
        ) : messages.length === 0 ? (
          <div className="flex items-center justify-center h-32 text-sm text-muted-foreground">
            No messages yet
          </div>
        ) : (
          messages.map((msg, idx) => {
            const prevKey = idx > 0 ? dateKey(messages[idx - 1].rawTimestamp) : '';
            const curKey = dateKey(msg.rawTimestamp);
            const showDate = curKey && curKey !== prevKey;
            return (
              <div key={msg.id}>
                {showDate && (
                  <div className="flex items-center gap-3 my-3">
                    <div className="flex-1 h-px bg-border" />
                    <span className="text-xs text-muted-foreground font-medium px-2">
                      {formatDateLabel(msg.rawTimestamp)}
                    </span>
                    <div className="flex-1 h-px bg-border" />
                  </div>
                )}
                <div id={msg.id}>
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
              </div>
            );
          })
        )}
      </div>
    </ScrollArea>
  );
}
