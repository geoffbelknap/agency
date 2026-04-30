import { useState, useEffect, useCallback } from 'react';
import { Link } from 'react-router';
import { toast } from 'sonner';
import { api } from '../lib/api';
import { Capability, Agent } from '../types';
import { Button } from '../components/ui/button';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '../components/ui/card';
import { AlertTriangle, Plus, Shield } from 'lucide-react';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { Input } from '../components/ui/input';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table';

function stateColor(state: string) {
  if (state === 'enabled') return 'bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-400';
  if (state === 'available') return 'bg-blue-50 dark:bg-blue-950 text-blue-700 dark:text-blue-400';
  if (state === 'restricted') return 'bg-amber-50 dark:bg-amber-950 text-amber-700 dark:text-amber-400';
  return 'bg-secondary text-muted-foreground';
}

function isActive(state: string) {
  return state === 'enabled' || state === 'available' || state === 'restricted';
}

export function Capabilities() {
  const [capabilities, setCapabilities] = useState<Capability[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showAddForm, setShowAddForm] = useState(false);
  const [newCapName, setNewCapName] = useState('');
  const [newCapKind, setNewCapKind] = useState('service');
  const [adding, setAdding] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Capability | null>(null);
  const [filterText, setFilterText] = useState('');
  const [filterKind, setFilterKind] = useState<string>('all');

  // Enable dialog state
  const [enableTarget, setEnableTarget] = useState<Capability | null>(null);
  const [enableKey, setEnableKey] = useState('');
  const [enableAgents, setEnableAgents] = useState<string[]>([]);
  const [enabling, setEnabling] = useState(false);

  const filteredCapabilities = capabilities.filter((cap) => {
    if (filterKind !== 'all' && cap.kind !== filterKind) return false;
    if (filterText) {
      return cap.name.toLowerCase().includes(filterText.toLowerCase());
    }
    return true;
  });

  const loadCapabilities = useCallback(async (showLoading = true) => {
    try {
      if (showLoading) setLoading(true);
      setError(null);
      const raw = await api.capabilities.list();
      const mapped: Capability[] = (raw ?? []).map((c: any) => ({
        id: c.name,
        name: c.name,
        kind: c.kind || 'service',
        state: c.state || 'disabled',
        scopedAgents: c.agents || [],
        description: c.description || '',
      }));
      setCapabilities(mapped);
    } catch (e: any) {
      setError(e.message || 'Failed to load capabilities');
    } finally {
      setLoading(false);
    }
  }, []);

  const loadAgents = useCallback(async () => {
    try {
      const raw = await api.agents.list();
      setAgents((raw ?? []).map((a: any) => ({
        id: a.name, name: a.name, status: a.status || 'stopped',
        mode: a.mode || '', type: a.type || '', preset: a.preset || '',
        team: a.team || '', enforcerState: a.enforcer || '',
      })));
    } catch { /* ignore */ }
  }, []);

  const handleEnable = async () => {
    if (!enableTarget) return;
    try {
      setEnabling(true);
      setError(null);
      await api.capabilities.enable(
        enableTarget.name,
        enableKey || undefined,
        enableAgents.length > 0 ? enableAgents : undefined,
      );
      // Brief delay to let gateway persist state change before re-fetching
      await new Promise((r) => setTimeout(r, 500));
      await loadCapabilities(false);
      toast.success(`Capability "${enableTarget.name}" enabled`);
      setEnableTarget(null);
      setEnableKey('');
      setEnableAgents([]);
    } catch (e: any) {
      setError(e.message || 'Failed to enable capability');
    } finally {
      setEnabling(false);
    }
  };

  const handleDisable = async (cap: Capability) => {
    try {
      setError(null);
      await api.capabilities.disable(cap.name);
      await new Promise((r) => setTimeout(r, 500));
      await loadCapabilities(false);
      toast.success(`Capability "${cap.name}" disabled`);
    } catch (e: any) {
      setError(e.message || 'Failed to disable capability');
    }
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    const name = deleteTarget.name;
    try {
      setError(null);
      await api.capabilities.delete(name);
      setCapabilities((prev) => prev.filter((cap) => cap.name !== name));
      await new Promise((r) => setTimeout(r, 500));
      await loadCapabilities(false);
      setDeleteTarget(null);
      toast.success(`Capability "${name}" deleted`);
    } catch (e: any) {
      setError(e.message || 'Failed to delete capability');
      setDeleteTarget(null);
    }
  };

  const handleAdd = async () => {
    if (!newCapName.trim()) return;
    try {
      setAdding(true);
      setError(null);
      await api.capabilities.add(newCapName.trim(), newCapKind);
      const addedName = newCapName.trim();
      setNewCapName('');
      setNewCapKind('service');
      setShowAddForm(false);
      await loadCapabilities(false);
      toast.success(`Capability "${addedName}" added`);
    } catch (e: any) {
      setError(e.message || 'Failed to add capability');
    } finally {
      setAdding(false);
    }
  };

  const openEnableDialog = (cap: Capability) => {
    setEnableTarget(cap);
    setEnableKey('');
    setEnableAgents(cap.scopedAgents || []);
    loadAgents();
  };

  const toggleAgent = (name: string) => {
    setEnableAgents((prev) =>
      prev.includes(name) ? prev.filter((a) => a !== name) : [...prev, name]
    );
  };

  useEffect(() => {
    loadCapabilities();
  }, [loadCapabilities]);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">Platform capability registry</p>
        <Button size="sm" onClick={() => setShowAddForm((v) => !v)}>
          <Plus data-icon="inline-start" />
          Add Capability
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Use capabilities to control what agents can touch</CardTitle>
          <CardDescription>
              Enable a capability only when an agent or mission needs it. Leave access platform-wide only for shared operator tools; otherwise scope it to the smallest useful set of agents.
          </CardDescription>
          <CardAction className="flex gap-2">
            <Button asChild variant="outline" size="sm">
              <Link to="/admin/presets">Open Presets</Link>
            </Button>
            <Button asChild variant="outline" size="sm">
              <Link to="/admin/policy">Review Policy</Link>
            </Button>
          </CardAction>
        </CardHeader>
      </Card>

      <div className="flex flex-wrap gap-2">
        <Input
          type="text"
          value={filterText}
          onChange={(e) => setFilterText(e.target.value)}
          placeholder="Filter by name..."
          className="w-full sm:w-64"
        />
        <select
          value={filterKind}
          onChange={(e) => setFilterKind(e.target.value)}
          className="bg-card border border-border text-foreground/80 rounded px-3 py-1.5 text-sm"
        >
          <option value="all">All kinds</option>
          <option value="service">service</option>
          <option value="tool">tool</option>
          <option value="integration">integration</option>
        </select>
      </div>

      {showAddForm && (
        <Card>
          <CardHeader>
            <CardTitle>Add capability</CardTitle>
            <CardDescription>Register a new service, tool, or integration before exposing it to agents.</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-wrap items-center gap-2">
          <Input
            type="text"
            value={newCapName}
            onChange={(e) => setNewCapName(e.target.value)}
            placeholder="Capability name..."
            className="flex-1"
            onKeyDown={(e) => e.key === 'Enter' && handleAdd()}
          />
          <select
            value={newCapKind}
            onChange={(e) => setNewCapKind(e.target.value)}
            className="bg-background border border-border text-foreground/80 rounded px-3 py-1.5 text-sm"
          >
            <option value="service">service</option>
            <option value="tool">tool</option>
            <option value="integration">integration</option>
          </select>
          <Button size="sm" onClick={handleAdd} disabled={adding}>
            {adding ? 'Adding...' : 'Add'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              setShowAddForm(false);
              setNewCapName('');
            }}
          >
            Cancel
          </Button>
          </CardContent>
        </Card>
      )}

      {error && (
        <div className="text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
          {error}
        </div>
      )}

      {loading ? (
        <div className="text-sm text-muted-foreground text-center py-12">Loading capabilities...</div>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle>Capability directory</CardTitle>
            <CardDescription>Review state, scope access, and enable or disable capabilities safely.</CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Kind</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead>Scoped Agents</TableHead>
                  <TableHead>Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredCapabilities.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5} className="p-8 text-center text-muted-foreground text-sm">
                      {capabilities.length === 0 ? (
                        <div className="flex flex-col items-center gap-2">
                          <div className="flex items-center gap-2 text-sm font-medium text-amber-300">
                            <AlertTriangle className="h-4 w-4" />
                            No capabilities found
                          </div>
                          <p className="text-xs text-muted-foreground">
                            Add a capability only when you need to expose a new tool, service, or integration to agents.
                          </p>
                        </div>
                      ) : 'No capabilities match your filter'}
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredCapabilities.map((capability) => (
                    <TableRow
                      key={capability.id}
                    >
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Shield className="w-4 h-4 text-muted-foreground/70" />
                          <code className="text-foreground">{capability.name}</code>
                        </div>
                      </TableCell>
                      <TableCell>
                        <span className="text-xs bg-secondary text-muted-foreground px-2 py-0.5 rounded capitalize">
                          {capability.kind}
                        </span>
                      </TableCell>
                      <TableCell>
                        <span className={`text-xs px-2 py-0.5 rounded ${stateColor(capability.state)}`}>
                          {capability.state}
                        </span>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap gap-1">
                          {capability.scopedAgents.length === 0 ? (
                            <span className="text-xs text-muted-foreground/70">Platform-wide</span>
                          ) : (
                            capability.scopedAgents.map((agent) => (
                              <span
                                key={agent}
                                className="text-xs bg-secondary text-muted-foreground px-2 py-0.5 rounded"
                              >
                                {agent}
                              </span>
                            ))
                          )}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex gap-2">
                          {isActive(capability.state) && (
                            <Button
                              variant="outline"
                              size="sm"
                              className="h-7 text-xs"
                              onClick={() => openEnableDialog(capability)}
                            >
                              Configure
                            </Button>
                          )}
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-7 text-xs"
                            onClick={() =>
                              isActive(capability.state)
                                ? handleDisable(capability)
                                : openEnableDialog(capability)
                            }
                          >
                            {isActive(capability.state) ? 'Disable' : 'Enable'}
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-7 text-xs"
                            onClick={() => setDeleteTarget(capability)}
                          >
                            Delete
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      {/* Enable / Configure Dialog */}
      {enableTarget && (() => {
        const isConfiguring = isActive(enableTarget.state);
        return (
          <div className="fixed inset-0 z-50 flex items-center justify-center">
            <div className="fixed inset-0 bg-black/60" onClick={() => setEnableTarget(null)} />
            <div className="relative bg-card border border-border rounded-lg p-4 md:p-6 w-full max-w-md mx-4 space-y-4 shadow-xl">
              <h3 className="text-lg font-semibold text-foreground">
                {isConfiguring ? 'Configure' : 'Enable'} {enableTarget.name}
              </h3>
              {enableTarget.description && (
                <p className="text-sm text-muted-foreground">{enableTarget.description}</p>
              )}
              <div className="space-y-2">
                <label className="text-xs text-muted-foreground uppercase tracking-wide">
                  API Key / Credential {isConfiguring ? '(leave blank to keep current)' : '(optional)'}
                </label>
                <Input
                  type="password"
                  value={enableKey}
                  onChange={(e) => setEnableKey(e.target.value)}
                  placeholder={isConfiguring ? 'Enter new key to replace...' : 'Enter API key if required...'}
                />
              </div>
              <div className="space-y-2">
                <label className="text-xs text-muted-foreground uppercase tracking-wide">Agent access</label>
                <div className="flex flex-wrap gap-2">
                  {agents.map((agent) => (
                    <button
                      key={agent.name}
                      onClick={() => toggleAgent(agent.name)}
                      className={`text-xs px-2.5 py-1 rounded border transition-colors ${
                        enableAgents.includes(agent.name)
                          ? 'bg-primary border-primary/80 text-white'
                          : 'bg-secondary border-border text-muted-foreground hover:border-border'
                      }`}
                    >
                      {agent.name}
                    </button>
                  ))}
                  {agents.length === 0 && (
                    <span className="text-xs text-muted-foreground">No agents found</span>
                  )}
                </div>
                {enableAgents.length === 0 && agents.length > 0 && (
                  <p className="text-xs text-muted-foreground">No agents selected — all agents have access</p>
                )}
              </div>
              <div className="flex justify-end gap-2 pt-2">
                <Button variant="outline" size="sm" onClick={() => setEnableTarget(null)}>
                  Cancel
                </Button>
                <Button size="sm" onClick={handleEnable} disabled={enabling}>
                  {enabling ? 'Saving...' : isConfiguring ? 'Save' : 'Enable'}
                </Button>
              </div>
            </div>
          </div>
        );
      })()}

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => { if (!open) setDeleteTarget(null); }}
        title="Delete Capability"
        description={`Are you sure you want to delete "${deleteTarget?.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={confirmDelete}
      />
    </div>
  );
}
