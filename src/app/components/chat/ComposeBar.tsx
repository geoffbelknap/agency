import { useState } from 'react';
import { Send, Flag, X } from 'lucide-react';
import { MentionInput } from '../MentionInput';
import { SlashCommandMenu } from './SlashCommandMenu';
import { Button } from '../ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
  DropdownMenuLabel,
} from '../ui/dropdown-menu';
import { MessageFlagBadge } from './MessageFlagBadge';
import type { Message } from '../../types';
import { useDraft } from '../../hooks/useDraft';

type FlagType = 'DECISION' | 'BLOCKER' | 'QUESTION';

const MAX_MESSAGE_LENGTH = 32_000;

interface ComposeBarProps {
  onSend: (content: string, flags?: { decision?: boolean; blocker?: boolean; question?: boolean }) => void;
  channelName: string;
  disabled?: boolean;
  replyTo?: Message;
  onCancelReply?: () => void;
  placeholder?: string;
}

export function ComposeBar({ onSend, channelName, disabled, replyTo, onCancelReply, placeholder }: ComposeBarProps) {
  const [newMessage, setNewMessage, clearDraft] = useDraft(channelName);
  const [selectedFlag, setSelectedFlag] = useState<FlagType | null>(null);

  const showSlashMenu =
    newMessage.startsWith('/') && !newMessage.includes(' ');
  const slashFilter = showSlashMenu ? newMessage.slice(1) : '';

  const handleSlashSelect = (command: string) => {
    setNewMessage(command + ' ');
  };

  const overLimit = newMessage.length > MAX_MESSAGE_LENGTH;

  const handleSend = () => {
    if (!newMessage.trim() || overLimit) return;
    const flags = selectedFlag
      ? {
          decision: selectedFlag === 'DECISION',
          blocker: selectedFlag === 'BLOCKER',
          question: selectedFlag === 'QUESTION',
        }
      : undefined;
    onSend(newMessage, flags);
    clearDraft();
    setNewMessage('');
    setSelectedFlag(null);
  };

  return (
    <div className="p-4 border-t border-border safe-bottom">
      {replyTo && (
        <div className="flex items-center justify-between mb-2 px-2 py-1 bg-secondary rounded text-xs text-muted-foreground">
          <span>Replying to <span className="text-foreground">{replyTo.author}</span></span>
          <button
            onClick={onCancelReply}
            aria-label="Cancel reply"
            className="text-muted-foreground hover:text-accent-foreground ml-2"
          >
            <X className="w-3 h-3" />
          </button>
        </div>
      )}
      <div className="flex gap-2 items-center">
        <div className="relative flex-1">
          {showSlashMenu && (
            <SlashCommandMenu
              filter={slashFilter}
              onSelect={handleSlashSelect}
              onClose={() => setNewMessage('')}
            />
          )}
          <MentionInput
            value={newMessage}
            onChange={setNewMessage}
            onSubmit={handleSend}
            placeholder={placeholder || `Message #${channelName}`}
            className="bg-card border-border text-foreground placeholder:text-muted-foreground"
          />
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="outline"
              size="sm"
              aria-label="Set message flag"
              className={`border-border bg-card hover:bg-secondary ${selectedFlag ? 'text-foreground' : 'text-muted-foreground'}`}
            >
              <Flag className="w-4 h-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="bg-card border-border text-foreground">
            <DropdownMenuLabel className="text-muted-foreground text-xs">Flag message as</DropdownMenuLabel>
            <DropdownMenuSeparator className="bg-border" />
            <DropdownMenuItem
              onClick={() => setSelectedFlag(selectedFlag === 'DECISION' ? null : 'DECISION')}
              className="gap-2 focus:bg-secondary cursor-pointer"
            >
              <span className="text-green-400">●</span>
              DECISION
              {selectedFlag === 'DECISION' && <span className="ml-auto text-green-400">✓</span>}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => setSelectedFlag(selectedFlag === 'BLOCKER' ? null : 'BLOCKER')}
              className="gap-2 focus:bg-secondary cursor-pointer"
            >
              <span className="text-red-400">●</span>
              BLOCKER
              {selectedFlag === 'BLOCKER' && <span className="ml-auto text-red-400">✓</span>}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => setSelectedFlag(selectedFlag === 'QUESTION' ? null : 'QUESTION')}
              className="gap-2 focus:bg-secondary cursor-pointer"
            >
              <span className="text-amber-400">●</span>
              QUESTION
              {selectedFlag === 'QUESTION' && <span className="ml-auto text-amber-400">✓</span>}
            </DropdownMenuItem>
            {selectedFlag && (
              <>
                <DropdownMenuSeparator className="bg-border" />
                <DropdownMenuItem
                  onClick={() => setSelectedFlag(null)}
                  className="gap-2 focus:bg-secondary cursor-pointer text-muted-foreground"
                >
                  <X className="w-3 h-3" />
                  Clear flag
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
        {selectedFlag && (
          <MessageFlagBadge flag={selectedFlag} />
        )}
        <Button
          onClick={handleSend}
          size="sm"
          aria-label="Send message"
          disabled={disabled || !newMessage.trim() || overLimit}
        >
          <Send className="w-4 h-4" />
        </Button>
      </div>
      {overLimit && (
        <div className="text-xs text-red-400 px-1 mt-1">
          Message too long ({newMessage.length.toLocaleString()} / {MAX_MESSAGE_LENGTH.toLocaleString()})
        </div>
      )}
    </div>
  );
}
