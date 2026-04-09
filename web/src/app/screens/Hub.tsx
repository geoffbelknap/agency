import { useState, useEffect, useCallback, useRef } from 'react';
import { Search, Download, CheckCircle, RefreshCw, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../lib/api';
import { Component, ComponentKind } from '../types';
import { Input } from '../components/ui/input';
import { Button } from '../components/ui/button';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs';
import { ComponentInfoDialog } from './hub/ComponentInfoDialog';
import { DeploySection } from './hub/DeploySection';

const HUB_KIND_FILTERS: Array<ComponentKind | 'all'> = [
  'all',
  'pack',
  'preset',
  'connector',
  'service',
  'mission',
  'skill',
  'workspace',
  'policy',
  'ontology',
  'provider',
  'setup',
];

const INSTALLABLE_HUB_KINDS = new Set<ComponentKind>([
  'pack',
  'preset',
  'connector',
  'service',
  'mission',
  'skill',
  'workspace',
  'policy',
  'provider',
]);

const isInstallableHubKind = (kind: ComponentKind) => INSTALLABLE_HUB_KINDS.has(kind);

const kindBadgeClass = (kind: ComponentKind) => {
  switch (kind) {
    case 'pack':
      return 'bg-violet-50 dark:bg-purple-950 text-violet-700 dark:text-purple-400';
    case 'preset':
      return 'bg-blue-50 dark:bg-blue-950 text-blue-700 dark:text-blue-400';
    case 'connector':
      return 'bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-400';
    case 'service':
      return 'bg-cyan-50 dark:bg-cyan-950 text-cyan-700 dark:text-cyan-400';
    case 'mission':
      return 'bg-rose-50 dark:bg-rose-950 text-rose-700 dark:text-rose-400';
    case 'provider':
      return 'bg-indigo-50 dark:bg-indigo-950 text-indigo-700 dark:text-indigo-400';
    case 'setup':
      return 'bg-slate-100 dark:bg-slate-900 text-slate-700 dark:text-slate-300';
    default:
      return 'bg-amber-50 dark:bg-amber-950 text-amber-700 dark:text-amber-400';
  }
};

export function Hub() {
  const [allResults, setAllResults] = useState<Component[]>([]);
  const [installedComponents, setInstalledComponents] = useState<Component[]>([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [filterKind, setFilterKind] = useState<ComponentKind | 'all'>('all');
  const [searchLoading, setSearchLoading] = useState(false);
  const [installedLoading, setInstalledLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedPackName, setSelectedPackName] = useState('');
  const [deploying, setDeploying] = useState(false);
  const [deployResult, setDeployResult] = useState<string | null>(null);
  const [updatingSources, setUpdatingSources] = useState(false);
  const [updateReport, setUpdateReport] = useState<any>(null);
  const [upgrading, setUpgrading] = useState(false);
  const [teardownTarget, setTeardownTarget] = useState<string | null>(null);
  const [infoTarget, setInfoTarget] = useState<Component | null>(null);
  const [infoData, setInfoData] = useState<any>(null);
  const [infoLoading, setInfoLoading] = useState(false);

  const loadInstalled = useCallback(async () => {
    try {
      setInstalledLoading(true);
      const raw = await api.hub.list();
      const mapped: Component[] = (raw ?? []).map((i: any) => ({
        id: i.id || `${i.name}-${i.kind}`,
        name: i.name || i.component,
        kind: i.kind,
        description: '',
        source: i.source || '',
        installed: true,
        installedAt: i.created || i.installed_at,
        version: i.version,
        state: i.state,
      }));
      setInstalledComponents(mapped);
    } catch (e: any) {
      setError(e.message || 'Failed to load installed components');
    } finally {
      setInstalledLoading(false);
    }
  }, []);

  const handleSearch = useCallback(async () => {
    try {
      setSearchLoading(true);
      setError(null);
      const raw = await api.hub.search(searchQuery);
      const installedNames = new Set(installedComponents.map((c) => c.name));
      const mapped: Component[] = (raw ?? []).map((r: any) => ({
        id: `${r.name}-${r.kind}-${r.source || ''}`,
        name: r.name,
        kind: r.kind,
        description: r.description || '',
        source: r.source || '',
        installed: installedNames.has(r.name),
      }));
      setAllResults(mapped);
    } catch (e: any) {
      setError(e.message || 'Failed to search hub');
    } finally {
      setSearchLoading(false);
    }
  }, [searchQuery, installedComponents]);

  const searchResults = filterKind === 'all' ? allResults : allResults.filter((c) => c.kind === filterKind);

  const handleInstall = async (component: Component) => {
    try {
      setError(null);
      await api.hub.install(component.name, component.kind, component.source || undefined);
      await loadInstalled();
      // Re-search to refresh installed status across all results
      if (allResults.length > 0) {
        handleSearch();
      }
      toast.success('Installed ' + component.name);
    } catch (e: any) {
      setError(e.message || 'Failed to install component');
    }
  };

  const handleRemove = async (component: Component) => {
    try {
      setError(null);
      await api.hub.remove(component.name, component.kind);
      await loadInstalled();
      if (allResults.length > 0) {
        handleSearch();
      }
      toast.success('Removed ' + component.name);
    } catch (e: any) {
      setError(e.message || 'Failed to remove component');
    }
  };

  const handleUpdateSources = async () => {
    try {
      setUpdatingSources(true);
      const report = await api.hub.update();
      setUpdateReport(report);
      if ((report as any)?.available?.length > 0) {
        toast.success(`${(report as any).available.length} upgrade(s) available`);
      } else {
        toast.success('Hub sources up to date');
      }
      await handleSearch();
    } catch (e: any) {
      toast.error(e.message || 'Update failed');
    } finally {
      setUpdatingSources(false);
    }
  };

  const handleUpgrade = async (components?: string[]) => {
    try {
      setUpgrading(true);
      const report = await api.hub.upgrade(components);
      const upgraded = (report.components || []).filter((c: any) => c.status === 'upgraded');
      if (upgraded.length > 0) {
        toast.success(`Upgraded ${upgraded.length} component(s)`);
      } else {
        toast.success('Everything up to date');
      }
      setUpdateReport(null);
      await loadInstalled();
      await handleSearch();
    } catch (e: any) {
      toast.error(e.message || 'Upgrade failed');
    } finally {
      setUpgrading(false);
    }
  };

  const handleDeploy = async () => {
    if (!selectedPackName) return;
    try {
      setDeploying(true);
      setDeployResult(null);
      setError(null);
      const result = await api.deploy.deploy(selectedPackName);
      setDeployResult(JSON.stringify(result, null, 2));
    } catch (e: any) {
      setError(e.message || 'Failed to deploy pack');
    } finally {
      setDeploying(false);
    }
  };

  const confirmTeardown = async () => {
    if (!teardownTarget) return;
    try {
      setError(null);
      await api.deploy.teardown(teardownTarget, false);
      toast.success(`Teardown complete: ${teardownTarget}`);
      setDeployResult(`Teardown complete: ${teardownTarget}`);
      setTeardownTarget(null);
      await loadInstalled();
    } catch (e: any) {
      setError(e.message || 'Failed to teardown pack');
      setTeardownTarget(null);
    }
  };

  const openInfo = async (component: Component) => {
    setInfoTarget(component);
    setInfoData(null);
    setInfoLoading(true);
    try {
      const data = await api.hub.info(component.name, component.kind);
      setInfoData(data);
    } catch {
      setInfoData(null);
    } finally {
      setInfoLoading(false);
    }
  };

  useEffect(() => {
    loadInstalled();
  }, [loadInstalled]);

  // Fetch browse results after installed list is loaded (so we can mark installed items)
  const initialSearchDone = useRef(false);
  useEffect(() => {
    if (!installedLoading && !initialSearchDone.current) {
      initialSearchDone.current = true;
      handleSearch();
    }
  }, [installedLoading, handleSearch]);

  const kindCounts = Object.fromEntries(
    HUB_KIND_FILTERS.map((kind) => [
      kind,
      kind === 'all' ? allResults.length : allResults.filter((c) => c.kind === kind).length,
    ]),
  ) as Record<ComponentKind | 'all', number>;

  return (
    <div className="space-y-4">
      {error && (
        <div className="text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
          {error}
        </div>
      )}

      <Tabs defaultValue="browse" className="space-y-6">
        <TabsList>
          <TabsTrigger value="browse">Browse</TabsTrigger>
          <TabsTrigger value="installed">Installed</TabsTrigger>
          <TabsTrigger value="deploy">Deploy</TabsTrigger>
        </TabsList>

        <TabsContent value="browse" className="space-y-4">
          {/* Search and Filter */}
          <div className="flex flex-col sm:flex-row gap-4">
            <div className="flex-1 relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
              <Input
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
                placeholder="Search components..."
                className="pl-10 bg-card border-border text-foreground placeholder:text-muted-foreground/70"
              />
            </div>
            <Button variant="outline" size="sm" onClick={handleSearch} disabled={searchLoading}>
              <Search className="w-3 h-3 mr-1" />
              {searchLoading ? 'Searching...' : 'Search'}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={handleUpdateSources}
              disabled={updatingSources}
            >
              <RefreshCw className={`w-3 h-3 mr-1 ${updatingSources ? 'animate-spin' : ''}`} />
              {updatingSources ? 'Updating...' : 'Update Sources'}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleUpgrade()}
              disabled={upgrading}
            >
              {upgrading ? 'Upgrading...' : 'Upgrade All'}
            </Button>
          </div>

          {/* Upgrade Banner */}
          {updateReport?.available?.length > 0 && (
            <div className="bg-green-950/30 border border-green-900/50 rounded-lg px-4 py-3 flex items-center gap-3">
              <div className="flex-1">
                <span className="text-emerald-400 font-medium text-sm">
                  {updateReport.available.length} upgrade{updateReport.available.length > 1 ? 's' : ''} available
                </span>
                <span className="text-muted-foreground text-xs ml-2">
                  {updateReport.available.map((u: any) =>
                    u.kind === 'managed' ? `${u.name} ${u.summary}` : `${u.name} ${u.installed_version} → ${u.available_version}`
                  ).join(', ')}
                </span>
              </div>
              <Button size="sm" className="h-7 text-xs bg-emerald-600 hover:bg-emerald-500" onClick={() => handleUpgrade()} disabled={upgrading}>
                {upgrading ? 'Upgrading...' : 'Upgrade'}
              </Button>
            </div>
          )}

          {/* Kind Filters */}
          <div className="flex flex-wrap gap-2">
            {HUB_KIND_FILTERS.map((kind) => (
              <Button
                key={kind}
                variant={filterKind === kind ? 'default' : 'outline'}
                size="sm"
                onClick={() => setFilterKind(kind)}
                className="capitalize text-xs"
              >
                {kind}
                <span className="ml-1.5 opacity-70">({kindCounts[kind]})</span>
              </Button>
            ))}
          </div>

          {/* Results Grid */}
          {searchLoading ? (
            <div className="text-sm text-muted-foreground text-center py-12">Searching...</div>
          ) : searchResults.length === 0 ? (
            <div className="text-sm text-muted-foreground/70 text-center py-12">
              Search the hub to find components
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {searchResults.map((component) => (
                <div
                  key={component.id}
                  className="bg-card border border-border rounded p-4 hover:border-border transition-colors"
                >
                  <div className="flex items-start justify-between mb-2">
                    <div className="flex-1">
                      <div className="flex items-center gap-2 mb-1">
                        <h3 className="font-semibold text-foreground text-sm">{component.name}</h3>
                        {component.installed && (
                          <CheckCircle className="w-3 h-3 text-green-500" />
                        )}
                      </div>
                      <div className="flex items-center gap-2 text-xs">
                        <span
                          className={`px-2 py-0.5 rounded ${kindBadgeClass(component.kind)}`}
                        >
                          {component.kind}
                        </span>
                        <span className="text-muted-foreground/70">·</span>
                        <span className="text-muted-foreground">{component.source}</span>
                      </div>
                    </div>
                  </div>

                  <p className="text-xs text-muted-foreground mb-3 line-clamp-2">{component.description}</p>

                  <div>
                    {component.installed ? (
                      <Button
                        variant="outline"
                        size="sm"
                        className="w-full h-7 text-xs"
                        onClick={() => handleRemove(component)}
                      >
                        <Trash2 className="w-3 h-3 mr-1" />
                        Remove
                      </Button>
                    ) : isInstallableHubKind(component.kind) ? (
                      <Button
                        variant="default"
                        size="sm"
                        onClick={() => handleInstall(component)}
                        className="w-full h-7 text-xs"
                      >
                        <Download className="w-3 h-3 mr-1" />
                        Install
                      </Button>
                    ) : (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => openInfo(component)}
                        className="w-full h-7 text-xs"
                      >
                        View Hub-Managed Info
                      </Button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </TabsContent>

        <TabsContent value="installed">
          {installedLoading ? (
            <div className="text-sm text-muted-foreground text-center py-12">Loading installed components...</div>
          ) : (
            <div className="bg-card border border-border rounded overflow-x-auto">
              <table className="w-full text-sm min-w-[560px]">
                <thead>
                  <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
                    <th className="text-left p-3 md:p-4 font-medium">Name</th>
                    <th className="text-left p-3 md:p-4 font-medium">Kind</th>
                    <th className="text-left p-3 md:p-4 font-medium">Source</th>
                    <th className="text-left p-3 md:p-4 font-medium">Installed At</th>
                    <th className="text-left p-3 md:p-4 font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {installedComponents.length === 0 ? (
                    <tr>
                      <td colSpan={5} className="p-8 text-center text-muted-foreground text-sm">
                        No components installed
                      </td>
                    </tr>
                  ) : (
                    installedComponents.map((component) => (
                      <tr
                        key={component.id}
                        className="border-b border-border hover:bg-secondary/50 transition-colors"
                      >
                        <td className="p-4">
                          <code className="text-foreground">{component.name}</code>
                        </td>
                        <td className="p-4">
                          <span className="text-xs bg-secondary text-muted-foreground px-2 py-0.5 rounded capitalize">
                            {component.kind}
                          </span>
                        </td>
                        <td className="p-4">
                          <span className="text-muted-foreground text-xs">{component.source}</span>
                        </td>
                        <td className="p-4">
                          <span className="text-muted-foreground text-xs">
                            {(component as any).installedAt || '—'}
                          </span>
                        </td>
                        <td className="p-4">
                          <div className="flex gap-2">
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => openInfo(component)}
                              className="h-7 text-xs"
                            >
                              Info
                            </Button>
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => handleRemove(component)}
                              className="h-7 text-xs"
                            >
                              Remove
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          )}
        </TabsContent>

        <TabsContent value="deploy">
          <DeploySection
            installedComponents={installedComponents}
            selectedPackName={selectedPackName}
            onSelectPack={setSelectedPackName}
            deploying={deploying}
            deployResult={deployResult}
            onDeploy={handleDeploy}
            teardownTarget={teardownTarget}
            onTeardownRequest={setTeardownTarget}
            onTeardownConfirm={confirmTeardown}
            onTeardownCancel={() => setTeardownTarget(null)}
          />
        </TabsContent>
      </Tabs>

      {/* Component Info Dialog */}
      {infoTarget && (
        <ComponentInfoDialog
          component={infoTarget}
          infoData={infoData}
          infoLoading={infoLoading}
          onClose={() => setInfoTarget(null)}
        />
      )}
    </div>
  );
}
