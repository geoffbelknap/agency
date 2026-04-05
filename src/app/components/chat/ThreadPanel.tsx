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
      {/* Header */}
      <div className="p-4 border-b border-border flex items-center justify-between">
        <h3 className="font-semibold text-foreground">Thread</h3>
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

      {/* Parent message */}
      <div className="p-4 bg-card/50 border-b border-border">
        <AgencyMessage
          message={parentMessage}
          agentStatus={agentStatuses?.[parentMessage.author]}
          showReplyButton={false}
        />
      </div>

      {/* Replies */}
      <ScrollArea className="flex-1">
        <div className="p-4 space-y-1">
          {replies.length === 0 ? (
            <div className="flex items-center justify-center h-16 text-sm text-muted-foreground">
              No replies yet
            </div>
          ) : (
            replies.map((msg) => (
              <AgencyMessage
                key={msg.id}
                message={msg}
                agentStatus={agentStatuses?.[msg.author]}
                showReplyButton={false}
              />
            ))
          )}
          <div ref={repliesEndRef} />
        </div>
      </ScrollArea>

      {/* Compose */}
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
