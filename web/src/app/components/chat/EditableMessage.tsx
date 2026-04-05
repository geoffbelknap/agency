import { useState } from 'react';
import { Check, X } from 'lucide-react';
import { Textarea } from '../ui/textarea';
import { Button } from '../ui/button';
import type { Message } from '../../types';

interface EditableMessageProps {
  message: Message;
  onSave: (newContent: string) => void;
  onCancel: () => void;
}

export function EditableMessage({ message, onSave, onCancel }: EditableMessageProps) {
  const [content, setContent] = useState(message.content);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      onCancel();
    } else if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      onSave(content);
    }
  };

  return (
    <div className="space-y-2">
      <Textarea
        value={content}
        onChange={(e) => setContent(e.target.value)}
        onKeyDown={handleKeyDown}
        className="min-h-[60px] text-sm bg-card border-border"
        autoFocus
      />
      <div className="flex items-center gap-2">
        <Button size="sm" onClick={() => onSave(content)} disabled={!content.trim()}>
          <Check className="w-3.5 h-3.5" />
          Save
        </Button>
        <Button size="sm" variant="ghost" onClick={onCancel}>
          <X className="w-3.5 h-3.5" />
          Cancel
        </Button>
        <span className="text-xs text-muted-foreground">Esc to cancel, Ctrl+Enter to save</span>
      </div>
    </div>
  );
}
