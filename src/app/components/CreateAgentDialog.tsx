import { useEffect, useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from './ui/dialog';
import { Input } from './ui/input';
import { Label } from './ui/label';
import { Button } from './ui/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from './ui/select';
import { Loader2 } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../lib/api';

interface Preset {
  name: string;
  description: string;
  type: string;
}

interface CreateAgentDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: () => void;
}

const NAME_PATTERN = /^[a-z0-9][a-z0-9-]*[a-z0-9]$/;
const RESERVED_NAMES = new Set(['infra-egress', 'agency', 'enforcer', 'gateway', 'workspace']);

function validateName(name: string): string | null {
  if (name.length < 2) return 'Name must be at least 2 characters';
  if (!NAME_PATTERN.test(name)) return 'Name must be lowercase alphanumeric with hyphens only';
  if (RESERVED_NAMES.has(name)) return `Name "${name}" is reserved`;
  return null;
}

export function CreateAgentDialog({ open, onOpenChange, onCreated }: CreateAgentDialogProps) {
  const [name, setName] = useState('');
  const [preset, setPreset] = useState('generalist');
  const [mode, setMode] = useState('assisted');
  const [nameError, setNameError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [autoStart, setAutoStart] = useState(true);

  const [presets, setPresets] = useState<Preset[]>([]);
  const [presetsLoading, setPresetsLoading] = useState(false);
  const [presetsFailed, setPresetsFailed] = useState(false);

  useEffect(() => {
    if (!open) return;
    setName('');
    setPreset('generalist');
    setMode('assisted');
    setNameError(null);
    setPresetsLoading(true);
    setPresetsFailed(false);

    api.presets.list()
      .then((data) => {
        setPresets(data);
        setPresetsLoading(false);
      })
      .catch(() => {
        setPresetsFailed(true);
        setPresetsLoading(false);
      });
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
      await api.agents.create(name, preset, mode);
      if (autoStart) {
        try { await api.agents.start(name); } catch { /* best effort */ }
      }
      toast.success(`Agent "${name}" ${autoStart ? 'created and started' : 'created'}`);
      onCreated();
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to create agent');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="bg-card border-border">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle className="text-foreground">Create Agent</DialogTitle>
            <DialogDescription className="text-muted-foreground">
              Configure and launch a new agent.
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-4 py-4">
            <div className="grid gap-2">
              <Label htmlFor="agent-name" className="text-foreground/80">Name</Label>
              <Input
                id="agent-name"
                value={name}
                onChange={(e) => {
                  // Auto-correct: lowercase, replace invalid chars with hyphens, collapse runs
                  const corrected = e.target.value
                    .toLowerCase()
                    .replace(/[^a-z0-9-]/g, '-')
                    .replace(/-{2,}/g, '-')
                    .replace(/^-/, '');
                  setName(corrected);
                  setNameError(null);
                }}
                placeholder="my-agent"
                disabled={submitting}
                className="bg-secondary border-border text-foreground"
              />
              {nameError && (
                <p className="text-sm text-red-400">{nameError}</p>
              )}
            </div>

            <div className="grid gap-2">
              <Label htmlFor="agent-preset" className="text-foreground/80">Preset</Label>
              {presetsFailed ? (
                <Input
                  id="agent-preset"
                  value={preset}
                  onChange={(e) => setPreset(e.target.value)}
                  placeholder="generalist"
                  disabled={submitting}
                  className="bg-secondary border-border text-foreground"
                />
              ) : (
                <Select value={preset} onValueChange={setPreset} disabled={submitting || presetsLoading}>
                  <SelectTrigger id="agent-preset" className="bg-secondary border-border text-foreground">
                    {presetsLoading ? (
                      <span className="flex items-center gap-2 text-muted-foreground">
                        <Loader2 className="w-3.5 h-3.5 animate-spin" /> Loading...
                      </span>
                    ) : (
                      <SelectValue />
                    )}
                  </SelectTrigger>
                  <SelectContent className="bg-secondary border-border">
                    {presets.map((p) => (
                      <SelectItem key={p.name} value={p.name} className="text-foreground">
                        {p.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </div>

            <div className="grid gap-2">
              <Label htmlFor="agent-mode" className="text-foreground/80">Mode</Label>
              <Select value={mode} onValueChange={setMode} disabled={submitting}>
                <SelectTrigger id="agent-mode" className="bg-secondary border-border text-foreground">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent className="bg-secondary border-border">
                  <SelectItem value="assisted" className="text-foreground">assisted</SelectItem>
                  <SelectItem value="autonomous" className="text-foreground">autonomous</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="flex items-center gap-2 pb-2">
            <input
              type="checkbox"
              id="auto-start"
              checked={autoStart}
              onChange={(e) => setAutoStart(e.target.checked)}
              disabled={submitting}
              className="rounded border-border bg-secondary"
            />
            <Label htmlFor="auto-start" className="text-muted-foreground text-sm cursor-pointer">
              Start agent immediately
            </Label>
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
