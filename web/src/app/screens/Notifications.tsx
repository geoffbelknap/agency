import { useState, useEffect, useCallback } from 'react';
import { Link } from 'react-router';
import { toast } from 'sonner';
import { api, RawNotification } from '../lib/api';
import { Button } from '../components/ui/button';
import { AlertTriangle, Bell, Plus, RefreshCw, Send, Trash2 } from 'lucide-react';

export function Notifications() {
  const [destinations, setDestinations] = useState<RawNotification[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showAdd, setShowAdd] = useState(false);
  const [newName, setNewName] = useState('');
  const [newUrl, setNewUrl] = useState('');
  const [adding, setAdding] = useState(false);
  const [testing, setTesting] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await api.notifications.list();
      setDestinations(Array.isArray(data) ? data : []);
    } catch (e: any) {
      setError(e.message || 'Failed to load notifications');
      setDestinations([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const handleAdd = async () => {
    if (!newName.trim() || !newUrl.trim()) return;
    setAdding(true);
    try {
      await api.notifications.add(newName.trim(), newUrl.trim());
      setNewName('');
      setNewUrl('');
      setShowAdd(false);
      toast.success(`Notification destination "${newName.trim()}" added`);
      load();
    } catch (e: any) {
      toast.error(e.message || 'Failed to add destination');
    } finally {
      setAdding(false);
    }
  };

  const handleRemove = async (name: string) => {
    try {
      await api.notifications.remove(name);
      toast.success(`Removed "${name}"`);
      load();
    } catch (e: any) {
      toast.error(e.message || 'Failed to remove destination');
    }
  };

  const handleTest = async (name: string) => {
    setTesting(name);
    try {
      const result = await api.notifications.test(name);
      toast.success(`Test sent to "${name}" (${result.event_id})`);
    } catch (e: any) {
      toast.error(e.message || 'Test failed');
    } finally {
      setTesting(null);
    }
  };

  return (
    <div className="space-y-6">
      <div className="rounded-2xl border border-border bg-card px-4 py-4 md:px-5">
        <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
          <div className="space-y-1">
            <div className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">Notification routing</div>
            <p className="text-sm text-muted-foreground">Operator notification destinations for ntfy topics and outbound alert delivery.</p>
            <p className="text-xs text-muted-foreground">
              Use notifications for operator alerts. Use webhooks when another system needs a signed inbound endpoint instead.
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button asChild variant="outline" size="sm" className="h-8 text-xs">
              <Link to="/admin/webhooks">Open Webhooks</Link>
            </Button>
            <Button asChild variant="outline" size="sm" className="h-8 text-xs">
              <Link to="/admin/events">Review Events</Link>
            </Button>
          </div>
        </div>
      </div>

      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium text-foreground">Destinations</h3>
          <p className="text-xs text-muted-foreground">Add, test, or remove outbound alert targets.</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={load} disabled={loading}>
            <RefreshCw className={`w-3 h-3 mr-1 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </Button>
          <Button variant="outline" size="sm" onClick={() => setShowAdd(true)} disabled={showAdd} aria-label="Add destination">
            <Plus className="w-3 h-3 mr-1" />
            Add
          </Button>
        </div>
      </div>

      {error && (
        <div className="text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
          {error}
        </div>
      )}

      {/* Add form */}
      {showAdd && (
        <div className="space-y-3 rounded-2xl border border-border bg-card p-4">
          <div>
            <div className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">New destination</div>
            <p className="mt-1 text-xs text-muted-foreground">Type is inferred from the URL and alert events default to the operator-safe set.</p>
          </div>
          <div className="flex flex-col sm:flex-row gap-2">
            <input
              type="text"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder="name"
              className="flex-1 rounded-md border border-border bg-background px-3 py-1.5 text-sm text-foreground placeholder:text-muted-foreground/70"
            />
            <input
              type="text"
              value={newUrl}
              onChange={(e) => setNewUrl(e.target.value)}
              placeholder="url (e.g. https://ntfy.sh/my-topic)"
              className="flex-[2] rounded-md border border-border bg-background px-3 py-1.5 text-sm text-foreground placeholder:text-muted-foreground/70"
            />
          </div>
          <div className="flex gap-2">
            <Button size="sm" onClick={handleAdd} disabled={adding || !newName.trim() || !newUrl.trim()}>
              {adding ? 'Adding...' : 'Add'}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => { setShowAdd(false); setNewName(''); setNewUrl(''); }}>
              Cancel
            </Button>
          </div>
        </div>
      )}

      <div className="overflow-hidden rounded-2xl border border-border bg-card">
        {loading ? (
          <div className="text-muted-foreground text-center py-8 text-sm">Loading notification destinations...</div>
        ) : destinations.length === 0 ? (
          <div className="py-8 px-4 text-center">
            <div className="flex items-center justify-center gap-2 text-sm font-medium text-amber-300">
              <AlertTriangle className="h-4 w-4" />
              No notification destinations configured
            </div>
            <p className="mt-2 text-xs text-muted-foreground">
              Add an ntfy topic or webhook destination so operator alerts and enforcer exits do not stay local-only.
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm min-w-[500px]">
              <thead>
                <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-[0.14em]">
                  <th className="text-left p-3 font-medium">Name</th>
                  <th className="text-left p-3 font-medium">Type</th>
                  <th className="text-left p-3 font-medium">URL</th>
                  <th className="text-left p-3 font-medium">Events</th>
                  <th className="text-right p-3 font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {destinations.map((d) => (
                  <tr key={d.name} className="border-b border-border hover:bg-secondary/50 transition-colors">
                    <td className="p-3">
                      <div className="flex items-center gap-1.5">
                        <Bell className="w-3 h-3 text-muted-foreground" />
                        <code className="text-foreground">{d.name}</code>
                      </div>
                    </td>
                    <td className="p-3">
                      <span className="rounded-full bg-secondary px-2 py-0.5 text-[11px] text-muted-foreground">{d.type}</span>
                    </td>
                    <td className="p-3 text-xs text-muted-foreground font-mono truncate max-w-[200px]">{d.url}</td>
                    <td className="p-3">
                      <div className="flex flex-wrap gap-1">
                        {d.events?.map((evt) => (
                          <span key={evt} className="rounded-full bg-secondary px-1.5 py-0.5 text-[10px] text-muted-foreground">{evt}</span>
                        ))}
                      </div>
                    </td>
                    <td className="p-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 text-xs"
                          onClick={() => handleTest(d.name)}
                          disabled={testing === d.name}
                          title="Send test notification"
                        >
                          <Send className={`w-3 h-3 ${testing === d.name ? 'animate-pulse' : ''}`} />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 text-xs text-red-400 hover:text-red-300"
                          onClick={() => handleRemove(d.name)}
                          title="Remove destination"
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
