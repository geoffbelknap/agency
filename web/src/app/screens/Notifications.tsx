import { useState, useEffect, useCallback } from 'react';
import { Link } from 'react-router';
import { toast } from 'sonner';
import { api, RawNotification } from '../lib/api';
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
      <Card>
        <CardHeader>
          <CardTitle>Notification routing</CardTitle>
          <CardDescription>
            Operator notification destinations for ntfy topics and outbound alert delivery.
          </CardDescription>
          <CardDescription>
              Use notifications for operator alerts. Use webhooks when another system needs a signed inbound endpoint instead.
          </CardDescription>
          <CardAction className="flex gap-2">
            <Button asChild variant="outline" size="sm">
              <Link to="/admin/webhooks">Open Webhooks</Link>
            </Button>
            <Button asChild variant="outline" size="sm">
              <Link to="/admin/events">Review Events</Link>
            </Button>
          </CardAction>
        </CardHeader>
      </Card>

      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium text-foreground">Destinations</h3>
          <p className="text-xs text-muted-foreground">Add, test, or remove outbound alert targets.</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={load} disabled={loading}>
            <RefreshCw className={loading ? 'animate-spin' : ''} />
            Refresh
          </Button>
          <Button variant="outline" size="sm" onClick={() => setShowAdd(true)} disabled={showAdd} aria-label="Add destination">
            <Plus data-icon="inline-start" />
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
        <Card>
          <CardHeader>
            <CardTitle>New destination</CardTitle>
            <CardDescription>Type is inferred from the URL and alert events default to the operator-safe set.</CardDescription>
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
              value={newUrl}
              onChange={(e) => setNewUrl(e.target.value)}
              placeholder="url (e.g. https://ntfy.sh/my-topic)"
              className="flex-[2]"
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
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Destinations</CardTitle>
          <CardDescription>Add, test, or remove outbound alert targets.</CardDescription>
        </CardHeader>
        <CardContent>
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
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>URL</TableHead>
                <TableHead>Events</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
                {destinations.map((d) => (
                  <TableRow key={d.name}>
                    <TableCell>
                      <div className="flex items-center gap-1.5">
                        <Bell className="w-3 h-3 text-muted-foreground" />
                        <code className="text-foreground">{d.name}</code>
                      </div>
                    </TableCell>
                    <TableCell>
                      <span className="rounded-full bg-secondary px-2 py-0.5 text-[11px] text-muted-foreground">{d.type}</span>
                    </TableCell>
                    <TableCell className="max-w-[200px] truncate font-mono text-xs text-muted-foreground">{d.url}</TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {d.events?.map((evt) => (
                          <span key={evt} className="rounded-full bg-secondary px-1.5 py-0.5 text-[10px] text-muted-foreground">{evt}</span>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
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
