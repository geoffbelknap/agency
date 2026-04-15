import { useState, useEffect, useCallback } from 'react';
import { Link } from 'react-router';
import { toast } from 'sonner';
import { api, RawWebhook } from '../lib/api';
import { Button } from '../components/ui/button';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '../components/ui/card';
import { Input } from '../components/ui/input';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table';
import { AlertTriangle, Copy, Plus, RefreshCw, RotateCw, Trash2 } from 'lucide-react';
import { formatDateTime } from '../lib/time';

export function Webhooks() {
  const [webhooks, setWebhooks] = useState<RawWebhook[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [newName, setNewName] = useState('');
  const [newEventType, setNewEventType] = useState('');
  const [creating, setCreating] = useState(false);
  const [createdSecret, setCreatedSecret] = useState<{ name: string; secret: string } | null>(null);

  const load = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await api.webhooks.list();
      setWebhooks(Array.isArray(data) ? data : []);
    } catch (e: any) {
      setError(e.message || 'Failed to load webhooks');
      setWebhooks([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const handleCreate = async () => {
    if (!newName.trim() || !newEventType.trim()) return;
    setCreating(true);
    try {
      const result = await api.webhooks.create(newName.trim(), newEventType.trim());
      if (result.secret) {
        setCreatedSecret({ name: result.name, secret: result.secret });
      }
      setNewName('');
      setNewEventType('');
      setShowCreate(false);
      toast.success(`Webhook "${result.name}" created`);
      load();
    } catch (e: any) {
      toast.error(e.message || 'Failed to create webhook');
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (name: string) => {
    try {
      await api.webhooks.delete(name);
      toast.success(`Webhook "${name}" deleted`);
      load();
    } catch (e: any) {
      toast.error(e.message || 'Failed to delete webhook');
    }
  };

  const handleRotateSecret = async (name: string) => {
    try {
      const result = await api.webhooks.rotateSecret(name);
      if (result.secret) {
        setCreatedSecret({ name: result.name, secret: result.secret });
      }
      toast.success(`Secret rotated for "${name}"`);
    } catch (e: any) {
      toast.error(e.message || 'Failed to rotate secret');
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text).then(() => toast.success('Copied to clipboard'));
  };

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Inbound delivery</CardTitle>
          <CardDescription>Inbound webhooks for external systems that need signed event delivery into Agency.</CardDescription>
          <CardDescription>
              Use webhooks when another system needs a signed inbound endpoint. Use notifications for operator-facing alerts instead.
          </CardDescription>
          <CardAction className="flex gap-2">
            <Button asChild variant="outline" size="sm">
              <Link to="/admin/notifications">Open Notifications</Link>
            </Button>
            <Button asChild variant="outline" size="sm">
              <Link to="/admin/events">Review Events</Link>
            </Button>
          </CardAction>
        </CardHeader>
      </Card>

      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium text-foreground">Webhooks</h3>
          <p className="text-xs text-muted-foreground">Create signed endpoints, rotate secrets, and remove stale receivers.</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={load} disabled={loading}>
            <RefreshCw className={loading ? 'animate-spin' : ''} />
            Refresh
          </Button>
          {!showCreate && (
            <Button variant="outline" size="sm" onClick={() => setShowCreate(true)}>
              <Plus data-icon="inline-start" />
              Create
            </Button>
          )}
        </div>
      </div>

      {error && (
        <div className="text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
          {error}
        </div>
      )}

      {/* Secret reveal banner */}
      {createdSecret && (
        <div className="space-y-1 rounded-2xl border border-amber-200 bg-amber-50 px-3 py-3 dark:border-amber-900 dark:bg-amber-950/30">
          <div className="text-xs font-medium text-amber-700 dark:text-amber-300">
            Secret for "{createdSecret.name}" — copy now, it won't be shown again
          </div>
          <div className="flex items-center gap-2">
            <code className="text-xs bg-card px-2 py-1 rounded border border-border text-foreground flex-1 break-all">
              {createdSecret.secret}
            </code>
            <Button variant="ghost" size="sm" className="h-7" onClick={() => copyToClipboard(createdSecret.secret)}>
              <Copy className="w-3 h-3" />
            </Button>
            <Button variant="ghost" size="sm" className="h-7 text-muted-foreground" onClick={() => setCreatedSecret(null)}>
              Dismiss
            </Button>
          </div>
        </div>
      )}

      {/* Create form */}
      {showCreate && (
        <Card>
          <CardHeader>
            <CardTitle>New webhook</CardTitle>
            <CardDescription>Create a named endpoint for one event stream, then copy the generated secret immediately.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
          <div className="flex flex-col sm:flex-row gap-2">
            <Input
              type="text"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder="name"
              className="flex-1"
            />
            <Input
              type="text"
              value={newEventType}
              onChange={(e) => setNewEventType(e.target.value)}
              placeholder="event type"
              className="flex-1"
            />
          </div>
          <div className="flex gap-2">
            <Button size="sm" onClick={handleCreate} disabled={creating || !newName.trim() || !newEventType.trim()}>
              {creating ? 'Creating...' : 'Create'}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => { setShowCreate(false); setNewName(''); setNewEventType(''); }}>
              Cancel
            </Button>
          </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Webhooks</CardTitle>
          <CardDescription>Create signed endpoints, rotate secrets, and remove stale receivers.</CardDescription>
        </CardHeader>
        <CardContent>
        {loading ? (
          <div className="text-muted-foreground text-center py-8 text-sm">Loading webhooks...</div>
        ) : webhooks.length === 0 ? (
          <div className="py-8 px-4 text-center">
            <div className="flex items-center justify-center gap-2 text-sm font-medium text-amber-300">
              <AlertTriangle className="h-4 w-4" />
              No webhooks registered
            </div>
            <p className="mt-2 text-xs text-muted-foreground">
              Create a webhook when an external system needs to push signed events into Agency or verify delivery end to end.
            </p>
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Event Type</TableHead>
                <TableHead>URL</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
                {webhooks.map((wh) => (
                  <TableRow key={wh.name}>
                    <TableCell><code className="text-foreground">{wh.name}</code></TableCell>
                    <TableCell className="text-xs text-foreground/80">{wh.event_type}</TableCell>
                    <TableCell className="max-w-[200px] truncate font-mono text-xs text-muted-foreground">{wh.url}</TableCell>
                    <TableCell className="text-xs text-muted-foreground whitespace-nowrap">
                      {wh.created_at ? formatDateTime(wh.created_at) : '—'}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 text-xs"
                          onClick={() => handleRotateSecret(wh.name)}
                          title="Rotate secret"
                        >
                          <RotateCw className="w-3 h-3" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 text-xs text-red-400 hover:text-red-300"
                          onClick={() => handleDelete(wh.name)}
                          title="Delete webhook"
                        >
                          <Trash2 className="w-3 h-3" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
            </TableBody>
          </Table>
        )}
        </CardContent>
      </Card>
    </div>
  );
}
