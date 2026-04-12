import { useEffect, useState } from 'react';
import { Boxes, CheckCircle2, Package, RefreshCw, Rocket } from 'lucide-react';
import { toast } from 'sonner';
import {
  api,
  type RawInstalledPackage,
  type RawInstance,
  type RawInstanceApplyResult,
} from '../lib/api';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs';

const INSTANCEABLE_PACKAGE_KINDS = new Set(['connector']);

function formatTimestamp(value?: string) {
  if (!value) return 'Unknown';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function packageKey(pkg: RawInstalledPackage) {
  return `${pkg.kind}:${pkg.name}`;
}

function sourceLabel(instance: RawInstance) {
  const source = instance.source?.package || instance.source?.template;
  if (!source) return 'Manual';
  return `${source.kind}/${source.name}${source.version ? `@${source.version}` : ''}`;
}

function isInstanceable(pkg: RawInstalledPackage) {
  return INSTANCEABLE_PACKAGE_KINDS.has(pkg.kind);
}

function assuranceSet(pkg: RawInstalledPackage) {
  return new Set(pkg.assurance ?? []);
}

function meetsInstanceAssurance(pkg: RawInstalledPackage) {
  const statements = assuranceSet(pkg);
  if (statements.has('official_source')) return true;
  if (pkg.kind === 'connector') return statements.has('ask_partial');
  return statements.has('publisher_verified');
}

function assuranceSummary(pkg: RawInstalledPackage) {
  const statements = pkg.assurance ?? [];
  if (statements.length === 0) return 'No assurance recorded';
  return statements.join(', ');
}

export function Hub() {
  const [packages, setPackages] = useState<RawInstalledPackage[]>([]);
  const [instances, setInstances] = useState<RawInstance[]>([]);
  const [selectedInstance, setSelectedInstance] = useState<RawInstance | null>(null);
  const [draftNames, setDraftNames] = useState<Record<string, string>>({});
  const [actionState, setActionState] = useState<Record<string, string>>({});
  const [lastApply, setLastApply] = useState<Record<string, RawInstanceApplyResult>>({});
  const [loadingPackages, setLoadingPackages] = useState(true);
  const [loadingInstances, setLoadingInstances] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadPackages = async () => {
    setLoadingPackages(true);
    try {
      const response = await api.packages.list();
      setPackages(response.packages ?? []);
    } catch (e: any) {
      setError(e.message || 'Failed to load packages');
    } finally {
      setLoadingPackages(false);
    }
  };

  const loadInstances = async () => {
    setLoadingInstances(true);
    try {
      const response = await api.instances.list();
      const items = response.instances ?? [];
      setInstances(items);
      setSelectedInstance((current) => {
        if (!current) return items[0] ?? null;
        return items.find((item) => item.id === current.id) ?? items[0] ?? null;
      });
    } catch (e: any) {
      setError(e.message || 'Failed to load instances');
    } finally {
      setLoadingInstances(false);
    }
  };

  const refreshAll = async () => {
    try {
      setRefreshing(true);
      setError(null);
      await Promise.all([loadPackages(), loadInstances()]);
    } finally {
      setRefreshing(false);
    }
  };

  useEffect(() => {
    void refreshAll();
  }, []);

  const setBusy = (key: string, message: string | null) => {
    setActionState((current) => {
      const next = { ...current };
      if (message) next[key] = message;
      else delete next[key];
      return next;
    });
  };

  const handleCreateInstance = async (pkg: RawInstalledPackage) => {
    const key = packageKey(pkg);
    try {
      setBusy(`create:${key}`, 'Creating...');
      setError(null);
      const body: Record<string, unknown> = {
        kind: pkg.kind,
        name: pkg.name,
      };
      const instanceName = draftNames[key]?.trim();
      if (instanceName) body.instance_name = instanceName;
      const created = await api.instances.createFromPackage(body);
      toast.success(`Created instance ${created.name}`);
      setSelectedInstance(created);
      await loadInstances();
    } catch (e: any) {
      setError(e.message || 'Failed to create instance');
      toast.error(e.message || 'Failed to create instance');
    } finally {
      setBusy(`create:${key}`, null);
    }
  };

  const handleSelectInstance = async (id: string) => {
    try {
      setBusy(`show:${id}`, 'Loading...');
      setError(null);
      const detail = await api.instances.show(id);
      setSelectedInstance(detail);
    } catch (e: any) {
      setError(e.message || 'Failed to load instance');
    } finally {
      setBusy(`show:${id}`, null);
    }
  };

  const handleValidate = async (id: string) => {
    try {
      setBusy(`validate:${id}`, 'Validating...');
      setError(null);
      await api.instances.validate(id);
      toast.success('Instance is valid');
    } catch (e: any) {
      setError(e.message || 'Validation failed');
      toast.error(e.message || 'Validation failed');
    } finally {
      setBusy(`validate:${id}`, null);
    }
  };

  const handleApply = async (id: string) => {
    try {
      setBusy(`apply:${id}`, 'Applying...');
      setError(null);
      const result = await api.instances.apply(id);
      setLastApply((current) => ({ ...current, [id]: result }));
      setSelectedInstance(result.instance);
      toast.success(`Applied ${result.instance.name}`);
      await loadInstances();
    } catch (e: any) {
      setError(e.message || 'Apply failed');
      toast.error(e.message || 'Apply failed');
    } finally {
      setBusy(`apply:${id}`, null);
    }
  };

  const instanceCount = instances.length;
  const claimedCount = instances.filter((item) => item.claim).length;
  const authorityNodeCount = instances.reduce(
    (sum, item) => sum + (item.nodes ?? []).filter((node) => node.kind === 'connector.authority').length,
    0,
  );

  return (
    <div className="space-y-6">
      {error && (
        <div className="rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">
          {error}
        </div>
      )}

      <div className="flex flex-col gap-3 rounded-xl border border-border bg-card/70 p-4 md:flex-row md:items-start md:justify-between">
        <div className="space-y-1">
          <div className="flex items-center gap-2 text-sm font-medium text-foreground">
            <Boxes className="h-4 w-4" />
            Packages and instances
          </div>
          <p className="max-w-3xl text-sm text-muted-foreground">
            Installed packages are reusable local building blocks. Instances are the governed local realizations
            created from those packages with bindings, grants, and runtime state.
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={refreshAll} disabled={refreshing}>
          <RefreshCw className={`mr-2 h-4 w-4 ${refreshing ? 'animate-spin' : ''}`} />
          {refreshing ? 'Refreshing...' : 'Refresh'}
        </Button>
      </div>

      <Tabs defaultValue="packages" className="space-y-6">
        <TabsList>
          <TabsTrigger value="packages">Packages</TabsTrigger>
          <TabsTrigger value="instances">Instances</TabsTrigger>
        </TabsList>

        <TabsContent value="packages" className="space-y-4">
          <div className="grid gap-3 md:grid-cols-3">
            <div className="rounded-lg border border-border bg-card/70 px-4 py-3">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Installed packages</div>
              <div className="mt-1 text-2xl font-semibold text-foreground">{packages.length}</div>
              <div className="text-xs text-muted-foreground">Local packages available to scaffold new instances.</div>
            </div>
            <div className="rounded-lg border border-border bg-card/70 px-4 py-3">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Instanceable now</div>
              <div className="mt-1 text-2xl font-semibold text-foreground">
                {packages.filter((pkg) => isInstanceable(pkg)).length}
              </div>
              <div className="text-xs text-muted-foreground">Currently limited to connector packages on the V2 path.</div>
            </div>
            <div className="rounded-lg border border-border bg-card/70 px-4 py-3">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Trust modes</div>
              <div className="mt-1 text-2xl font-semibold text-foreground">
                {new Set(packages.map((pkg) => pkg.trust || 'unspecified')).size}
              </div>
              <div className="text-xs text-muted-foreground">Review trust and version before instantiating authority packages.</div>
            </div>
          </div>

          {loadingPackages ? (
            <div className="py-12 text-center text-sm text-muted-foreground">Loading packages...</div>
          ) : packages.length === 0 ? (
            <div className="rounded-lg border border-dashed border-border px-4 py-10 text-center text-sm text-muted-foreground">
              No local packages are installed yet.
            </div>
          ) : (
            <div className="grid gap-4 lg:grid-cols-2">
              {packages.map((pkg) => {
                const key = packageKey(pkg);
                const busy = actionState[`create:${key}`];
                const instanceable = isInstanceable(pkg);
                const assuranceOkay = meetsInstanceAssurance(pkg);
                return (
                  <div key={key} className="rounded-xl border border-border bg-card/70 p-4">
                    <div className="flex items-start justify-between gap-3">
                      <div className="space-y-1">
                        <div className="flex items-center gap-2">
                          <Package className="h-4 w-4 text-muted-foreground" />
                          <h3 className="text-sm font-semibold text-foreground">{pkg.name}</h3>
                        </div>
                        <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
                          <span className="rounded bg-slate-100 px-2 py-0.5 dark:bg-slate-900">{pkg.kind}</span>
                          <span className="rounded bg-slate-100 px-2 py-0.5 dark:bg-slate-900">
                            {pkg.version || 'unversioned'}
                          </span>
                          <span className="rounded bg-slate-100 px-2 py-0.5 dark:bg-slate-900">
                            trust: {pkg.trust || 'unspecified'}
                          </span>
                        </div>
                      </div>
                      {instanceable && assuranceOkay ? (
                        <span className="rounded-full bg-emerald-50 px-2 py-0.5 text-[11px] text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300">
                          Ready to instantiate
                        </span>
                      ) : instanceable ? (
                        <span className="rounded-full bg-red-50 px-2 py-0.5 text-[11px] text-red-700 dark:bg-red-950 dark:text-red-300">
                          More assurance required
                        </span>
                      ) : (
                        <span className="rounded-full bg-amber-50 px-2 py-0.5 text-[11px] text-amber-700 dark:bg-amber-950 dark:text-amber-300">
                          Package only
                        </span>
                      )}
                    </div>

                    <div className="mt-3 space-y-2 text-xs text-muted-foreground">
                      <div>
                        <span className="font-medium text-foreground">Local path:</span> {pkg.path || 'Unknown'}
                      </div>
                      <div>
                        <span className="font-medium text-foreground">Assurance:</span> {assuranceSummary(pkg)}
                      </div>
                      {pkg.publisher && (
                        <div>
                          <span className="font-medium text-foreground">Publisher:</span> {pkg.publisher}
                        </div>
                      )}
                      <div>
                        {instanceable
                          ? assuranceOkay
                            ? 'Create a local instance from this package, then validate and apply it from the Instances tab.'
                            : 'This package cannot be instantiated yet because it does not meet the local assurance policy.'
                          : 'This package kind is not scaffoldable into a V2 instance yet.'}
                      </div>
                    </div>

                    <div className="mt-4 flex flex-col gap-2 sm:flex-row">
                      <Input
                        aria-label={`Instance name for ${pkg.name}`}
                        value={draftNames[key] ?? ''}
                        onChange={(event) =>
                          setDraftNames((current) => ({ ...current, [key]: event.target.value }))
                        }
                        placeholder={`Optional instance name for ${pkg.name}`}
                        disabled={!instanceable || !assuranceOkay || !!busy}
                      />
                      <Button
                        onClick={() => handleCreateInstance(pkg)}
                        disabled={!instanceable || !assuranceOkay || !!busy}
                        className="sm:w-auto"
                      >
                        <Rocket className="mr-2 h-4 w-4" />
                        {busy || 'Create instance'}
                      </Button>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </TabsContent>

        <TabsContent value="instances" className="space-y-4">
          <div className="grid gap-3 md:grid-cols-3">
            <div className="rounded-lg border border-border bg-card/70 px-4 py-3">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Local instances</div>
              <div className="mt-1 text-2xl font-semibold text-foreground">{instanceCount}</div>
              <div className="text-xs text-muted-foreground">Governed realizations ready for validate and apply.</div>
            </div>
            <div className="rounded-lg border border-border bg-card/70 px-4 py-3">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Claimed instances</div>
              <div className="mt-1 text-2xl font-semibold text-foreground">{claimedCount}</div>
              <div className="text-xs text-muted-foreground">Instances currently owned by a named operator or process.</div>
            </div>
            <div className="rounded-lg border border-border bg-card/70 px-4 py-3">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Authority nodes</div>
              <div className="mt-1 text-2xl font-semibold text-foreground">{authorityNodeCount}</div>
              <div className="text-xs text-muted-foreground">Authority runtime nodes available for mediated actions.</div>
            </div>
          </div>

          {loadingInstances ? (
            <div className="py-12 text-center text-sm text-muted-foreground">Loading instances...</div>
          ) : instances.length === 0 ? (
            <div className="rounded-lg border border-dashed border-border px-4 py-10 text-center text-sm text-muted-foreground">
              No instances exist yet. Create one from the Packages tab first.
            </div>
          ) : (
            <div className="grid gap-4 xl:grid-cols-[1.3fr_0.9fr]">
              <div className="space-y-4">
                {instances.map((instance) => {
                  const validateBusy = actionState[`validate:${instance.id}`];
                  const applyBusy = actionState[`apply:${instance.id}`];
                  const showBusy = actionState[`show:${instance.id}`];
                  const applyResult = lastApply[instance.id];
                  return (
                    <div key={instance.id} className="rounded-xl border border-border bg-card/70 p-4">
                      <div className="flex items-start justify-between gap-3">
                        <div className="space-y-1">
                          <h3 className="text-sm font-semibold text-foreground">{instance.name}</h3>
                          <div className="text-xs text-muted-foreground">Source: {sourceLabel(instance)}</div>
                        </div>
                        {instance.claim ? (
                          <span className="rounded-full bg-sky-50 px-2 py-0.5 text-[11px] text-sky-700 dark:bg-sky-950 dark:text-sky-300">
                            Claimed by {instance.claim.owner}
                          </span>
                        ) : (
                          <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] text-slate-700 dark:bg-slate-900 dark:text-slate-300">
                            Unclaimed
                          </span>
                        )}
                      </div>

                      <div className="mt-3 grid gap-2 text-xs text-muted-foreground md:grid-cols-2">
                        <div>
                          <span className="font-medium text-foreground">Nodes:</span> {(instance.nodes ?? []).length}
                        </div>
                        <div>
                          <span className="font-medium text-foreground">Grants:</span> {(instance.grants ?? []).length}
                        </div>
                        <div>
                          <span className="font-medium text-foreground">Created:</span> {formatTimestamp(instance.created_at)}
                        </div>
                        <div>
                          <span className="font-medium text-foreground">Updated:</span> {formatTimestamp(instance.updated_at)}
                        </div>
                      </div>

                      {applyResult && (
                        <div className="mt-3 flex items-center gap-2 rounded border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-300">
                          <CheckCircle2 className="h-4 w-4" />
                          Last apply reconciled {applyResult.nodes?.length ?? 0} runtime node(s).
                        </div>
                      )}

                      <div className="mt-4 flex flex-wrap gap-2">
                        <Button variant="outline" size="sm" onClick={() => handleSelectInstance(instance.id)} disabled={!!showBusy}>
                          {showBusy || 'Details'}
                        </Button>
                        <Button variant="outline" size="sm" onClick={() => handleValidate(instance.id)} disabled={!!validateBusy}>
                          {validateBusy || 'Validate'}
                        </Button>
                        <Button size="sm" onClick={() => handleApply(instance.id)} disabled={!!applyBusy}>
                          {applyBusy || 'Apply'}
                        </Button>
                      </div>
                    </div>
                  );
                })}
              </div>

              <div className="rounded-xl border border-border bg-card/70 p-4">
                <div className="space-y-1">
                  <h3 className="text-sm font-semibold text-foreground">Instance detail</h3>
                  <p className="text-xs text-muted-foreground">
                    Inspect the selected instance graph before testing runtime behavior.
                  </p>
                </div>

                {!selectedInstance ? (
                  <div className="py-10 text-sm text-muted-foreground">Select an instance to inspect its nodes and grants.</div>
                ) : (
                  <div className="mt-4 space-y-4">
                    <div>
                      <div className="text-sm font-medium text-foreground">{selectedInstance.name}</div>
                      <div className="text-xs text-muted-foreground">ID: {selectedInstance.id}</div>
                      <div className="text-xs text-muted-foreground">Source: {sourceLabel(selectedInstance)}</div>
                    </div>

                    <div className="space-y-2">
                      <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Nodes</div>
                      {(selectedInstance.nodes ?? []).length === 0 ? (
                        <div className="text-sm text-muted-foreground">No nodes recorded.</div>
                      ) : (
                        <div className="space-y-2">
                          {(selectedInstance.nodes ?? []).map((node) => (
                            <div key={node.id} className="rounded border border-border px-3 py-2">
                              <div className="text-sm font-medium text-foreground">{node.id}</div>
                              <div className="text-xs text-muted-foreground">{node.kind}</div>
                              {node.package && (
                                <div className="text-xs text-muted-foreground">
                                  package: {node.package.kind}/{node.package.name}
                                </div>
                              )}
                            </div>
                          ))}
                        </div>
                      )}
                    </div>

                    <div className="space-y-2">
                      <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Grants</div>
                      {(selectedInstance.grants ?? []).length === 0 ? (
                        <div className="text-sm text-muted-foreground">No grants configured.</div>
                      ) : (
                        <div className="space-y-2">
                          {(selectedInstance.grants ?? []).map((grant, index) => (
                            <div key={`${grant.principal}:${grant.action}:${index}`} className="rounded border border-border px-3 py-2">
                              <div className="text-sm font-medium text-foreground">{grant.action}</div>
                              <div className="text-xs text-muted-foreground">principal: {grant.principal}</div>
                              <div className="text-xs text-muted-foreground">resource: {grant.resource || 'instance-scoped'}</div>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                )}
              </div>
            </div>
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}
