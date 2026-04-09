import { useState, useEffect, useCallback } from 'react';
import { toast } from 'sonner';
import { api } from '../lib/api';
import { Capability, Agent } from '../types';
import { Button } from '../components/ui/button';
import { Plus, Shield } from 'lucide-react';
import { ConfirmDialog } from '../components/ConfirmDialog';

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
          <Plus className="w-4 h-4 mr-1" />
          Add Capability
        </Button>
      </div>

      <div className="flex flex-wrap gap-2">
        <input
          type="text"
          value={filterText}
          onChange={(e) => setFilterText(e.target.value)}
          placeholder="Filter by name..."
          className="bg-card border border-border text-foreground rounded px-3 py-1.5 text-sm placeholder:text-muted-foreground/70 w-full sm:w-64"
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
        <div className="flex flex-wrap gap-2 items-center bg-card border border-border rounded p-3">
          <input
            type="text"
            value={newCapName}
            onChange={(e) => setNewCapName(e.target.value)}
            placeholder="Capability name..."
            className="flex-1 bg-background border border-border text-foreground rounded px-3 py-1.5 text-sm placeholder:text-muted-foreground/70"
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
        </div>
      )}

      {error && (
        <div className="text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
          {error}
        </div>
      )}

      {loading ? (
        <div className="text-sm text-muted-foreground text-center py-12">Loading capabilities...</div>
      ) : (
        <div className="bg-card border border-border rounded overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-sm min-w-[640px]">
              <thead>
                <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
                  <th className="text-left p-3 md:p-4 font-medium">Name</th>
                  <th className="text-left p-3 md:p-4 font-medium">Kind</th>
                  <th className="text-left p-3 md:p-4 font-medium">State</th>
                  <th className="text-left p-3 md:p-4 font-medium">Scoped Agents</th>
                  <th className="text-left p-3 md:p-4 font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {filteredCapabilities.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="p-8 text-center text-muted-foreground text-sm">
                      {capabilities.length === 0 ? 'No capabilities found' : 'No capabilities match your filter'}
                    </td>
                  </tr>
                ) : (
                  filteredCapabilities.map((capability) => (
                    <tr
                      key={capability.id}
                      className="border-b border-border hover:bg-secondary/50 transition-colors"
                    >
                      <td className="p-4">
                        <div className="flex items-center gap-2">
                          <Shield className="w-4 h-4 text-muted-foreground/70" />
                          <code className="text-foreground">{capability.name}</code>
                        </div>
                      </td>
                      <td className="p-4">
                        <span className="text-xs bg-secondary text-muted-foreground px-2 py-0.5 rounded capitalize">
                          {capability.kind}
                        </span>
                      </td>
                      <td className="p-4">
                        <span className={`text-xs px-2 py-0.5 rounded ${stateColor(capability.state)}`}>
                          {capability.state}
                        </span>
                      </td>
                      <td className="p-4">
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
                      </td>
                      <td className="p-4">
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
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>
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
                <input
                  type="password"
                  value={enableKey}
                  onChange={(e) => setEnableKey(e.target.value)}
                  placeholder={isConfiguring ? 'Enter new key to replace...' : 'Enter API key if required...'}
                  className="w-full bg-background border border-border text-foreground rounded px-3 py-2 text-sm placeholder:text-muted-foreground/70"
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
