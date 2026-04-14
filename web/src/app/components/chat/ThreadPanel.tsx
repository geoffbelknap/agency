import { useRef, useEffect } from 'react';
import { X } from 'lucide-react';
import { AgencyMessage } from './AgencyMessage';
import { ComposeBar } from './ComposeBar';
import { ScrollArea } from '../ui/scroll-area';
import { Button } from '../ui/button';
import { Sheet, SheetContent } from '../ui/sheet';
import { useIsMobile } from '../ui/use-mobile';
import type { Message } from '../../types';

interface ThreadPanelProps {
  parentMessage: Message;
  replies: Message[];
  onClose: () => void;
  onSend: (content: string) => void;
  agentStatuses?: Record<string, string>;
}

function ThreadPanelContent({
  parentMessage,
  replies,
  onClose,
  onSend,
  agentStatuses,
}: ThreadPanelProps) {
  const repliesEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    repliesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [replies]);

  return (
    <>
      <div className="flex items-start justify-between border-b border-border px-4 py-4">
        <div>
          <div className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">Thread</div>
          <h3 className="mt-1 text-base font-semibold text-foreground">Replies and follow-up</h3>
        </div>
        <Button
          variant="ghost"
          size="icon"
          onClick={onClose}
          aria-label="Close thread"
          className="h-9 w-9 text-muted-foreground hover:text-foreground"
        >
          <X className="w-5 h-5" />
        </Button>
      </div>

      <div className="border-b border-border bg-card/40 px-4 py-4">
        <div className="mb-2 text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">Parent message</div>
        <AgencyMessage
          message={parentMessage}
          agentStatus={agentStatuses?.[parentMessage.author]}
          showReplyButton={false}
        />
      </div>

      <ScrollArea className="flex-1">
        <div className="px-4 py-4">
          <div className="mb-3 text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
            Replies
          </div>
          {replies.length === 0 ? (
            <div className="flex h-24 items-center justify-center rounded-2xl border border-dashed border-border bg-card px-4 text-center">
              <div>
                <div className="text-sm font-medium text-foreground">No replies yet</div>
                <div className="mt-1 text-xs text-muted-foreground">Use the thread to keep side conversations out of the main channel timeline.</div>
              </div>
            </div>
          ) : (
            <div className="space-y-1">
              {replies.map((msg) => (
                <AgencyMessage
                  key={msg.id}
                  message={msg}
                  agentStatus={agentStatuses?.[msg.author]}
                  showReplyButton={false}
                />
              ))}
            </div>
          )}
          <div ref={repliesEndRef} />
        </div>
      </ScrollArea>

      <ComposeBar onSend={(content) => onSend(content)} channelName={`thread-${parentMessage.id}`} placeholder="Reply in thread" />
    </>
  );
}

export function ThreadPanel({ parentMessage, replies, onClose, onSend, agentStatuses }: ThreadPanelProps) {
  const isMobile = useIsMobile();

  if (isMobile) {
    return (
      <Sheet open onOpenChange={(open) => { if (!open) onClose(); }}>
        <SheetContent
          side="right"
          hideClose
          className="p-0 w-full sm:max-w-full bg-background border-border flex flex-col"
        >
          <ThreadPanelContent
            parentMessage={parentMessage}
            replies={replies}
            onClose={onClose}
            onSend={onSend}
            agentStatuses={agentStatuses}
          />
        </SheetContent>
      </Sheet>
    );
  }

  return (
    <div className="w-96 border-l border-border bg-background flex flex-col h-full">
      <ThreadPanelContent
        parentMessage={parentMessage}
        replies={replies}
        onClose={onClose}
        onSend={onSend}
        agentStatuses={agentStatuses}
      />
    </div>
  );
}
