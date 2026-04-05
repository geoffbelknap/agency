import { useEffect, useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../ui/dialog';
import { Input } from '../ui/input';
import { Label } from '../ui/label';
import { Button } from '../ui/button';
import { Loader2 } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../../lib/api';

interface CreateChannelDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: () => void;
}

const NAME_PATTERN = /^[a-z0-9][a-z0-9-]*[a-z0-9]$/;

function validateName(name: string): string | null {
  if (name.length < 2) return 'Name must be at least 2 characters';
  if (!NAME_PATTERN.test(name)) return 'Name must be lowercase alphanumeric with hyphens only';
  return null;
}

export function CreateChannelDialog({ open, onOpenChange, onCreated }: CreateChannelDialogProps) {
  const [name, setName] = useState('');
  const [topic, setTopic] = useState('');
  const [nameError, setNameError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setName('');
    setTopic('');
    setNameError(null);
  }, [open]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();

    const error = validateName(name);
    if (error) {
      setNameError(error);
      return;
    }
    setNameError(null);
    setSubmitting(true);

    try {
      await api.channels.create(name, topic || undefined);
      toast.success('Channel created');
      onCreated();
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to create channel');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="bg-card border-border">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle className="text-foreground">Create Channel</DialogTitle>
            <DialogDescription className="text-muted-foreground">
              Create a new channel for conversations.
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-4 py-4">
            <div className="grid gap-2">
              <Label htmlFor="channel-name" className="text-foreground/80">Name</Label>
              <Input
                id="channel-name"
                value={name}
                onChange={(e) => { setName(e.target.value); setNameError(null); }}
                placeholder="my-channel"
                disabled={submitting}
                className="bg-secondary border-border text-foreground"
              />
              {nameError && (
                <p className="text-sm text-red-400">{nameError}</p>
              )}
            </div>

            <div className="grid gap-2">
              <Label htmlFor="channel-topic" className="text-foreground/80">Topic</Label>
              <Input
                id="channel-topic"
                value={topic}
                onChange={(e) => setTopic(e.target.value)}
                placeholder="Optional topic"
                disabled={submitting}
                className="bg-secondary border-border text-foreground"
              />
            </div>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
              disabled={submitting}
              className="text-foreground/80 hover:bg-secondary"
            >
              Cancel
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting && <Loader2 className="w-3.5 h-3.5 mr-1 animate-spin" />}
              Create
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
