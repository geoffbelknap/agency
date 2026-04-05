import { useState, useEffect, useCallback } from 'react';
import { toast } from 'sonner';
import { api, RawWebhook } from '../lib/api';
import { Button } from '../components/ui/button';
import { RefreshCw, Plus, Trash2, RotateCw, Copy } from 'lucide-react';
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
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">Inbound webhooks for external event delivery</p>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={load} disabled={loading}>
            <RefreshCw className={`w-3 h-3 mr-1 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </Button>
          {!showCreate && (
            <Button variant="outline" size="sm" onClick={() => setShowCreate(true)}>
              <Plus className="w-3 h-3 mr-1" />
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
        <div className="bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-900 rounded px-3 py-2 space-y-1">
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
        <div className="bg-card border border-border rounded p-4 space-y-3">
          <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide">New Webhook</div>
          <div className="flex flex-col sm:flex-row gap-2">
            <input
              type="text"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder="name"
              className="flex-1 bg-secondary border border-border text-foreground rounded px-3 py-1.5 text-sm placeholder:text-muted-foreground/70"
            />
            <input
              type="text"
              value={newEventType}
              onChange={(e) => setNewEventType(e.target.value)}
              placeholder="event type"
              className="flex-1 bg-secondary border border-border text-foreground rounded px-3 py-1.5 text-sm placeholder:text-muted-foreground/70"
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
        </div>
      )}

      {/* Webhook list */}
      <div className="bg-card border border-border rounded overflow-hidden">
        {loading ? (
          <div className="text-muted-foreground text-center py-8 text-sm">Loading webhooks...</div>
        ) : webhooks.length === 0 ? (
          <div className="text-muted-foreground text-center py-8 text-sm">No webhooks registered</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm min-w-[500px]">
              <thead>
                <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
                  <th className="text-left p-3 font-medium">Name</th>
                  <th className="text-left p-3 font-medium">Event Type</th>
                  <th className="text-left p-3 font-medium">URL</th>
                  <th className="text-left p-3 font-medium">Created</th>
                  <th className="text-right p-3 font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {webhooks.map((wh) => (
                  <tr key={wh.name} className="border-b border-border hover:bg-secondary/50 transition-colors">
                    <td className="p-3"><code className="text-foreground">{wh.name}</code></td>
                    <td className="p-3 text-xs text-foreground/80">{wh.event_type}</td>
                    <td className="p-3 text-xs text-muted-foreground font-mono truncate max-w-[200px]">{wh.url}</td>
                    <td className="p-3 text-xs text-muted-foreground whitespace-nowrap">
                      {wh.created_at ? formatDateTime(wh.created_at) : '—'}
                    </td>
                    <td className="p-3 text-right">
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
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
