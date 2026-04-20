import { useEffect, useMemo, useState } from 'react';
import { Check, Package, Plus, RefreshCw, Rocket, Search } from 'lucide-react';
import { toast } from 'sonner';
import {
  api,
  type RawInstalledPackage,
  type RawInstance,
  type RawInstanceApplyResult,
} from '../lib/api';

const INSTANCEABLE_PACKAGE_KINDS = new Set(['connector']);

type ViewMode = 'packages' | 'instances';
type PackageFilter = 'all' | 'installed' | 'updates' | 'available';
type BadgeTone = 'teal' | 'amber' | 'red' | 'neutral';

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

function packageStatus(pkg: RawInstalledPackage) {
  if (!isInstanceable(pkg)) return { label: 'Package only', tone: 'amber' as BadgeTone };
  if (!meetsInstanceAssurance(pkg)) return { label: 'Needs assurance', tone: 'red' as BadgeTone };
  return { label: 'Ready', tone: 'teal' as BadgeTone };
}

function assuranceSummary(pkg: RawInstalledPackage) {
  const statements = pkg.assurance ?? [];
  if (statements.length === 0) return 'No assurance recorded';
  return statements.join(', ');
}

function primaryAskReview(pkg: RawInstalledPackage) {
  return (pkg.assurance_statements ?? []).find((statement) => statement.statement_type === 'ask_reviewed') ?? null;
}

function assuranceIssuer(pkg: RawInstalledPackage) {
  return pkg.assurance_issuer || primaryAskReview(pkg)?.issuer_hub_id || null;
}

function formatTimestamp(value?: string) {
  if (!value) return 'Unknown';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function packageDescription(pkg: RawInstalledPackage) {
  const spec = pkg.spec ?? {};
  const description = spec.description;
  if (typeof description === 'string' && description.trim()) return description;
  if (pkg.path) return pkg.path;
  return 'Installed package metadata from the local gateway registry.';
}

function packageCaps(pkg: RawInstalledPackage) {
  const spec = pkg.spec ?? {};
  const capabilities = spec.capabilities;
  if (Array.isArray(capabilities)) return capabilities.length;
  const grants = spec.grants;
  if (Array.isArray(grants)) return grants.length;
  return pkg.assurance?.length ?? 0;
}

function hasUpdate(_pkg: RawInstalledPackage) {
  return false;
}

function Badge({ children, tone = 'neutral' }: { children: React.ReactNode; tone?: BadgeTone }) {
  const styles = {
    teal: { bg: 'var(--teal-tint)', color: 'var(--teal-dark)', border: 'var(--teal-border)' },
    amber: { bg: 'var(--amber-tint)', color: '#8B5A00', border: 'var(--amber)' },
    red: { bg: 'var(--red-tint)', color: 'var(--red)', border: 'var(--red)' },
    neutral: { bg: 'var(--warm-3)', color: 'var(--ink-mid)', border: 'var(--ink-hairline-strong)' },
  }[tone];
  return (
    <span className="mono" style={{ display: 'inline-flex', alignItems: 'center', gap: 4, padding: '2px 7px', fontSize: 10, letterSpacing: '0.08em', textTransform: 'uppercase', background: styles.bg, color: styles.color, border: `0.5px solid ${styles.border}`, borderRadius: 4, whiteSpace: 'nowrap' }}>
      {children}
    </span>
  );
}

function DesignButton({ children, icon, variant = 'default', disabled = false, onClick }: { children: React.ReactNode; icon?: React.ReactNode; variant?: 'default' | 'primary' | 'ghost'; disabled?: boolean; onClick?: () => void }) {
  const variants = {
    default: { bg: 'var(--warm)', color: 'var(--ink)', border: '0.5px solid var(--ink-hairline-strong)' },
    primary: { bg: 'var(--ink)', color: 'var(--warm)', border: '0.5px solid var(--ink)' },
    ghost: { bg: 'transparent', color: 'var(--ink-mid)', border: '0.5px solid transparent' },
  }[variant];
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', gap: 6, padding: '5px 10px', minHeight: 28, borderRadius: 6, background: variants.bg, color: variants.color, border: variants.border, fontFamily: 'var(--sans)', fontSize: 12, cursor: disabled ? 'not-allowed' : 'pointer', opacity: disabled ? 0.45 : 1, whiteSpace: 'nowrap' }}
    >
      {icon}
      {children}
    </button>
  );
}

function MetaStat({ label, value, tone }: { label: string; value: string | number; tone?: string }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      <span className="eyebrow" style={{ fontSize: 9 }}>{label}</span>
      <span className="mono" style={{ fontSize: 14, color: tone || 'var(--ink)' }}>{value}</span>
    </div>
  );
}

function TableHeader({ cols, widths }: { cols: string[]; widths: string }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: widths, gap: 16, padding: '10px 18px', background: 'var(--warm-2)' }}>
      {cols.map((col) => <div key={col} className="eyebrow" style={{ fontSize: 9 }}>{col}</div>)}
    </div>
  );
}

function TableRow({ children, widths, accent, selected, onClick }: { children: React.ReactNode[]; widths: string; accent?: string; selected?: boolean; onClick?: () => void }) {
  return (
    <div
      onClick={onClick}
      style={{ display: 'grid', gridTemplateColumns: widths, gap: 16, padding: '12px 18px', alignItems: 'center', borderTop: '0.5px solid var(--ink-hairline)', borderLeft: `2px solid ${accent || (selected ? 'var(--teal)' : 'transparent')}`, background: selected ? 'var(--warm)' : 'transparent', cursor: onClick ? 'pointer' : 'default' }}
    >
      {children.map((child, index) => <div key={index} style={{ minWidth: 0 }}>{child}</div>)}
    </div>
  );
}

export function Hub() {
  const [packages, setPackages] = useState<RawInstalledPackage[]>([]);
  const [instances, setInstances] = useState<RawInstance[]>([]);
  const [selectedInstance, setSelectedInstance] = useState<RawInstance | null>(null);
  const [view, setView] = useState<ViewMode>('packages');
  const [filter, setFilter] = useState<PackageFilter>('all');
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
      const body: Record<string, unknown> = { kind: pkg.kind, name: pkg.name };
      const instanceName = draftNames[key]?.trim();
      if (instanceName) body.instance_name = instanceName;
      const created = await api.instances.createFromPackage(body);
      toast.success(`Created instance ${created.name}`);
      setSelectedInstance(created);
      setView('instances');
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

  const counts = useMemo(() => {
    const instanceable = packages.filter(isInstanceable).length;
    const ready = packages.filter((pkg) => isInstanceable(pkg) && meetsInstanceAssurance(pkg)).length;
    const claimed = instances.filter((item) => item.claim).length;
    const authorityNodes = instances.reduce((sum, item) => sum + (item.nodes ?? []).filter((node) => node.kind === 'connector.authority').length, 0);
    return { instanceable, ready, claimed, authorityNodes };
  }, [packages, instances]);

  const shownPackages = packages.filter((pkg) => {
    if (filter === 'installed') return Boolean(pkg.installed || pkg.path);
    if (filter === 'updates') return hasUpdate(pkg);
    if (filter === 'available') return !pkg.installed && !pkg.path;
    return true;
  });

  const filters: Array<[PackageFilter, string, number]> = [
    ['all', 'All', packages.length],
    ['installed', 'Installed', packages.filter((pkg) => pkg.installed || pkg.path).length],
    ['updates', 'Updates', packages.filter(hasUpdate).length],
    ['available', 'Available', packages.filter((pkg) => !pkg.installed && !pkg.path).length],
  ];

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      {error && (
        <div style={{ padding: '10px 12px', border: '0.5px solid var(--red)', background: 'var(--red-tint)', color: 'var(--red)', borderRadius: 8, fontSize: 12 }}>
          {error}
        </div>
      )}

      <div style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
        <div style={{ display: 'flex', gap: 18, flexWrap: 'wrap' }}>
          <MetaStat label="Installed packages" value={packages.length} />
          <MetaStat label="Instanceable" value={counts.instanceable} />
          <MetaStat label="Ready" value={counts.ready} tone="var(--teal-dark)" />
          <MetaStat label="Instances" value={instances.length} />
          <MetaStat label="Authority nodes" value={counts.authorityNodes} />
        </div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <DesignButton icon={<Search size={13} />} disabled>Browse registry</DesignButton>
          <DesignButton variant="primary" icon={<Plus size={13} />} disabled>Install from file</DesignButton>
          <DesignButton icon={<RefreshCw size={13} className={refreshing ? 'animate-spin' : ''} />} onClick={refreshAll} disabled={refreshing}>
            {refreshing ? 'Refreshing...' : 'Refresh'}
          </DesignButton>
        </div>
      </div>

      <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
        <div role="tablist" style={{ display: 'inline-flex', background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 999, padding: 2 }}>
          {(['packages', 'instances'] as ViewMode[]).map((mode) => (
            <button
              key={mode}
              type="button"
              role="tab"
              aria-selected={view === mode}
              onClick={() => setView(mode)}
              style={{ padding: '6px 12px', border: 0, background: view === mode ? 'var(--ink)' : 'transparent', color: view === mode ? 'var(--warm)' : 'var(--ink-mid)', fontFamily: 'var(--sans)', fontSize: 12, borderRadius: 999, cursor: 'pointer', textTransform: 'capitalize' }}
            >
              {mode}
            </button>
          ))}
        </div>
        {view === 'packages' && (
          <div style={{ display: 'inline-flex', background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 999, padding: 2 }}>
            {filters.map(([id, label, count]) => (
              <button
                key={id}
                type="button"
                onClick={() => setFilter(id)}
                style={{ padding: '6px 12px', border: 0, background: filter === id ? 'var(--ink)' : 'transparent', color: filter === id ? 'var(--warm)' : 'var(--ink-mid)', fontFamily: 'var(--sans)', fontSize: 12, borderRadius: 999, cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 6 }}
              >
                {label} <span className="mono" style={{ fontSize: 10, opacity: 0.7 }}>{count}</span>
              </button>
            ))}
          </div>
        )}
        <div style={{ flex: 1 }} />
        <span className="mono" style={{ fontSize: 10, color: 'var(--ink-faint)' }}>
          registry · local gateway · {packages.length + instances.length} records
        </span>
      </div>

      {view === 'packages' ? (
        <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, overflow: 'hidden', background: 'var(--warm)' }}>
          <TableHeader widths="minmax(220px,1.8fr) 90px 92px 72px 110px 160px" cols={['Package', 'Version', 'Kind', 'Caps', 'Assurance', '']} />
          {loadingPackages ? (
            <div style={{ padding: 32, textAlign: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>Loading packages...</div>
          ) : shownPackages.length === 0 ? (
            <div style={{ padding: 32, textAlign: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>No packages match this filter.</div>
          ) : shownPackages.map((pkg) => {
            const key = packageKey(pkg);
            const busy = actionState[`create:${key}`];
            const status = packageStatus(pkg);
            const askReview = primaryAskReview(pkg);
            const issuer = assuranceIssuer(pkg);
            const instanceable = isInstanceable(pkg);
            const assuranceOkay = meetsInstanceAssurance(pkg);
            return (
              <TableRow
                key={key}
                widths="minmax(220px,1.8fr) 90px 92px 72px 110px 160px"
                accent={status.tone === 'red' ? 'var(--red)' : status.tone === 'teal' ? 'var(--teal)' : 'transparent'}
              >
                {[
                  <div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                      <Package size={14} color="var(--ink-mid)" />
                      <span className="mono" style={{ fontSize: 13, color: 'var(--ink)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{pkg.name}</span>
                      <Badge tone={status.tone}>{status.label}</Badge>
                    </div>
                    <div style={{ fontSize: 11, color: 'var(--ink-mid)', marginTop: 2, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{packageDescription(pkg)}</div>
                    {(askReview || issuer) && (
                      <div style={{ fontSize: 11, color: 'var(--ink-faint)', marginTop: 2 }}>
                        {askReview ? `${askReview.result}${askReview.reviewer_type ? ` via ${askReview.reviewer_type}` : ''}` : ''}
                        {issuer ? `${askReview ? ' · ' : ''}${issuer}` : ''}
                      </div>
                    )}
                    {!assuranceOkay && instanceable && (
                      <div style={{ fontSize: 11, color: 'var(--red)', marginTop: 2 }}>This package cannot be instantiated yet because it does not meet the local assurance policy.</div>
                    )}
                  </div>,
                  <span className="mono" style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{pkg.version || 'unversioned'}</span>,
                  <Badge>{pkg.kind}</Badge>,
                  <span className="mono" style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{packageCaps(pkg)}</span>,
                  <span className="mono" style={{ display: 'block', fontSize: 11, color: 'var(--ink-faint)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{assuranceSummary(pkg)}</span>,
                  <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end', alignItems: 'center' }}>
                    <input
                      aria-label={`Instance name for ${pkg.name}`}
                      value={draftNames[key] ?? ''}
                      onChange={(event) => setDraftNames((current) => ({ ...current, [key]: event.target.value }))}
                      placeholder="name"
                      disabled={!instanceable || !assuranceOkay || !!busy}
                      style={{ width: 74, minHeight: 28, padding: '4px 7px', borderRadius: 6, border: '0.5px solid var(--ink-hairline-strong)', background: 'var(--warm-2)', color: 'var(--ink)', fontFamily: 'var(--mono)', fontSize: 11, opacity: (!instanceable || !assuranceOkay) ? 0.45 : 1 }}
                    />
                    <DesignButton variant={instanceable && assuranceOkay ? 'primary' : 'default'} icon={<Rocket size={13} />} disabled={!instanceable || !assuranceOkay || !!busy} onClick={() => handleCreateInstance(pkg)}>
                      {busy || 'Create instance'}
                    </DesignButton>
                  </div>,
                ]}
              </TableRow>
            );
          })}
        </div>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1.3fr) minmax(280px,0.8fr)', gap: 14 }}>
          <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, overflow: 'hidden', background: 'var(--warm)' }}>
            <TableHeader widths="minmax(180px,1.4fr) minmax(160px,1fr) 70px 70px 140px 160px" cols={['Instance', 'Source', 'Nodes', 'Grants', 'Updated', '']} />
            {loadingInstances ? (
              <div style={{ padding: 32, textAlign: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>Loading instances...</div>
            ) : instances.length === 0 ? (
              <div style={{ padding: 32, textAlign: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>No instances exist yet. Create one from the Packages view first.</div>
            ) : instances.map((instance) => {
              const validateBusy = actionState[`validate:${instance.id}`];
              const applyBusy = actionState[`apply:${instance.id}`];
              const applyResult = lastApply[instance.id];
              return (
                <TableRow
                  key={instance.id}
                  widths="minmax(180px,1.4fr) minmax(160px,1fr) 70px 70px 140px 160px"
                  selected={selectedInstance?.id === instance.id}
                  onClick={() => handleSelectInstance(instance.id)}
                >
                  {[
                    <div>
                      <div className="mono" style={{ fontSize: 13, color: 'var(--ink)' }}>{instance.name}</div>
                      <div style={{ fontSize: 11, color: 'var(--ink-mid)' }}>{instance.claim ? `claimed by ${instance.claim.owner}` : 'unclaimed'}</div>
                      {applyResult && <div style={{ display: 'flex', alignItems: 'center', gap: 4, color: 'var(--teal-dark)', fontSize: 11, marginTop: 2 }}><Check size={12} /> Last apply reconciled {applyResult.nodes?.length ?? 0} runtime node(s).</div>}
                    </div>,
                    <span className="mono" style={{ fontSize: 11, color: 'var(--ink-mid)' }}>{sourceLabel(instance)}</span>,
                    <span className="mono" style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{(instance.nodes ?? []).length}</span>,
                    <span className="mono" style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{(instance.grants ?? []).length}</span>,
                    <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)' }}>{formatTimestamp(instance.updated_at)}</span>,
                    <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 6 }}>
                      <DesignButton onClick={() => handleValidate(instance.id)} disabled={!!validateBusy}>{validateBusy || 'Validate'}</DesignButton>
                      <DesignButton variant="primary" onClick={() => handleApply(instance.id)} disabled={!!applyBusy}>{applyBusy || 'Apply'}</DesignButton>
                    </div>,
                  ]}
                </TableRow>
              );
            })}
          </div>

          <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm)', padding: 16, minHeight: 280 }}>
            <div className="eyebrow" style={{ marginBottom: 10 }}>Instance detail</div>
            {!selectedInstance ? (
              <div style={{ color: 'var(--ink-mid)', fontSize: 13 }}>Select an instance to inspect nodes and grants.</div>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
                <div>
                  <div className="mono" style={{ fontSize: 15, color: 'var(--ink)' }}>{selectedInstance.name}</div>
                  <div className="mono" style={{ fontSize: 10, color: 'var(--ink-faint)', marginTop: 2 }}>{selectedInstance.id}</div>
                  <div style={{ color: 'var(--ink-mid)', fontSize: 12, marginTop: 4 }}>{sourceLabel(selectedInstance)}</div>
                </div>
                <div>
                  <div className="eyebrow" style={{ fontSize: 9, marginBottom: 6 }}>Nodes</div>
                  {(selectedInstance.nodes ?? []).length === 0 ? (
                    <div style={{ color: 'var(--ink-mid)', fontSize: 12 }}>No nodes recorded.</div>
                  ) : (selectedInstance.nodes ?? []).map((node) => (
                    <div key={node.id} style={{ borderTop: '0.5px solid var(--ink-hairline)', padding: '8px 0' }}>
                      <div className="mono" style={{ fontSize: 12, color: 'var(--ink)' }}>{node.id}</div>
                      <div style={{ fontSize: 11, color: 'var(--ink-mid)' }}>{node.kind}</div>
                    </div>
                  ))}
                </div>
                <div>
                  <div className="eyebrow" style={{ fontSize: 9, marginBottom: 6 }}>Grants</div>
                  {(selectedInstance.grants ?? []).length === 0 ? (
                    <div style={{ color: 'var(--ink-mid)', fontSize: 12 }}>No grants configured.</div>
                  ) : (selectedInstance.grants ?? []).map((grant, index) => (
                    <div key={`${grant.principal}:${grant.action}:${index}`} style={{ borderTop: '0.5px solid var(--ink-hairline)', padding: '8px 0' }}>
                      <div className="mono" style={{ fontSize: 12, color: 'var(--ink)' }}>{grant.action}</div>
                      <div style={{ fontSize: 11, color: 'var(--ink-mid)' }}>{grant.principal}</div>
                      <div style={{ fontSize: 11, color: 'var(--ink-faint)' }}>{grant.resource || 'instance-scoped'}</div>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
