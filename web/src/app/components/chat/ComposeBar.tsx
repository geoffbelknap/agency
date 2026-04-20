import { AtSign, Paperclip, Send, X } from 'lucide-react';
import { MentionInput } from '../MentionInput';
import { SlashCommandMenu } from './SlashCommandMenu';
import type { Message } from '../../types';
import { useDraft } from '../../hooks/useDraft';

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

  const fieldName = `message-${channelName.replace(/[^a-zA-Z0-9_-]/g, '-')}`;
  const showSlashMenu = newMessage.startsWith('/') && !newMessage.includes(' ');
  const slashFilter = showSlashMenu ? newMessage.slice(1) : '';
  const overLimit = newMessage.length > MAX_MESSAGE_LENGTH;

  const handleSend = () => {
    if (!newMessage.trim() || overLimit) return;
    onSend(newMessage, undefined);
    clearDraft();
    setNewMessage('');
  };

  return (
    <div className="shrink-0 safe-bottom" style={{ borderTop: '0.5px solid var(--ink-hairline)', padding: '16px 28px 20px', background: 'var(--warm)' }}>
      {replyTo && (
        <div
          className="mb-3 flex items-center justify-between px-3 py-2 text-xs"
          style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 8, background: 'var(--warm-2)', color: 'var(--ink-muted)' }}
        >
          <span>Replying to <span style={{ color: 'var(--ink)' }}>{replyTo.author}</span></span>
          <button type="button" onClick={onCancelReply} aria-label="Cancel reply" className="ml-2" style={{ color: 'var(--ink-muted)' }}>
            <X className="h-3 w-3" />
          </button>
        </div>
      )}
      <div
        className="relative"
        style={{ border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 12, background: 'var(--warm)', padding: '12px 14px 11px' }}
      >
        {showSlashMenu && (
          <SlashCommandMenu filter={slashFilter} onSelect={(command) => setNewMessage(command + ' ')} onClose={() => setNewMessage('')} />
        )}
        <MentionInput
          name={fieldName}
          value={newMessage}
          onChange={setNewMessage}
          onSubmit={handleSend}
          placeholder={placeholder || `Message ${channelName}...`}
          className="min-h-6 border-0 bg-transparent px-0 py-0 text-sm leading-6 text-foreground shadow-none placeholder:text-muted-foreground focus-visible:ring-0"
        />
        <div className="mt-2.5 flex items-center gap-2">
          <button type="button" aria-label="Attach file" className="flex h-7 w-7 items-center justify-center rounded-md transition-colors hover:bg-black/[0.035]" style={{ color: 'var(--ink-mid)' }}>
            <Paperclip size={14} strokeWidth={1.7} />
          </button>
          <button type="button" aria-label="Mention agent" className="flex h-7 w-7 items-center justify-center rounded-md transition-colors hover:bg-black/[0.035]" style={{ color: 'var(--ink-mid)' }}>
            <AtSign size={14} strokeWidth={1.7} />
          </button>
          <div className="flex-1" />
          <kbd className="mono rounded-md px-1.5 py-0.5" style={{ border: '0.5px solid var(--ink-hairline)', color: 'var(--ink-mid)', fontSize: 11 }}>↵</kbd>
          <span style={{ fontSize: 11, color: 'var(--ink-faint)' }}>to send</span>
          <button
            type="button"
            onClick={handleSend}
            aria-label="Send message"
            disabled={disabled || !newMessage.trim() || overLimit}
            className="inline-flex h-8 items-center gap-2 rounded-md px-3 text-xs transition-opacity disabled:cursor-not-allowed disabled:opacity-35"
            style={{ background: 'var(--ink)', color: 'var(--warm)' }}
          >
            <Send className="h-3.5 w-3.5" />
            <span>Send</span>
          </button>
        </div>
      </div>
      {overLimit && (
        <div className="mt-2 px-1 text-xs text-red-500">
          Message too long ({newMessage.length.toLocaleString()} / {MAX_MESSAGE_LENGTH.toLocaleString()})
        </div>
      )}
    </div>
  );
}
