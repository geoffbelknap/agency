import { Reply, Smile, Pencil, Trash2 } from 'lucide-react';
import { Button } from '../ui/button';
import type { Message } from '../../types';

interface MessageActionsProps {
  message: Message;
  onReply: () => void;
  onReact: () => void;
  onEdit: () => void;
  onDelete: () => void;
}

export function MessageActions({ message, onReply, onReact, onEdit, onDelete }: MessageActionsProps) {
  return (
    <div className="flex items-center gap-0.5 bg-secondary border border-border rounded shadow px-0.5 py-0.5">
      <Button variant="ghost" size="icon" className="h-9 w-9 md:h-7 md:w-7" onClick={onReply} aria-label="Reply">
        <Reply className="w-3.5 h-3.5" />
      </Button>
      <Button variant="ghost" size="icon" className="h-9 w-9 md:h-7 md:w-7" onClick={onReact} aria-label="React">
        <Smile className="w-3.5 h-3.5" />
      </Button>
      {!message.isAgent && !message.isSystem && (
        <>
          <Button variant="ghost" size="icon" className="h-9 w-9 md:h-7 md:w-7" onClick={onEdit} aria-label="Edit">
            <Pencil className="w-3.5 h-3.5" />
          </Button>
          <Button variant="ghost" size="icon" className="h-9 w-9 md:h-7 md:w-7" onClick={onDelete} aria-label="Delete">
            <Trash2 className="w-3.5 h-3.5" />
          </Button>
        </>
      )}
    </div>
  );
}
