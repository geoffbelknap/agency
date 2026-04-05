import { useState, useEffect, useCallback } from 'react';
import { api } from '../lib/api';
import { Agent } from '../types';
import { Button } from '../components/ui/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '../components/ui/select';
import { formatDateTime } from '../lib/time';
import { JsonView } from '../components/JsonView';
import { ExportButton } from '../components/ExportButton';

export function AdminEgress() {
  const [agents, setAgents] = useState<Agent[]>([]);

  // Domain provenance
  const [egressDomains, setEgressDomains] = useState<any[]>([]);
  const [egressDomainsLoading, setEgressDomainsLoading] = useState(false);
  const [egressDomainsError, setEgressDomainsError] = useState<string | null>(null);
  const [provenanceDomain, setProvenanceDomain] = useState<string | null>(null);
  const [provenanceData, setProvenanceData] = useState<any>(null);
  const [provenanceLoading, setProvenanceLoading] = useState(false);

  // Per-agent egress
  const [egressAgent, setEgressAgent] = useState<string>('');
  const [egressData, setEgressData] = useState<any>(null);
  const [egressLoading, setEgressLoading] = useState(false);
  const [egressError, setEgressError] = useState<string | null>(null);

  const loadAgents = useCallback(async () => {
    try {
      const raw = await api.agents.list();
      const mapped: Agent[] = (raw ?? []).filter((a: any) => a.name).map((a: any) => ({
        id: a.name,
        name: a.name,
        status: a.status || 'stopped',
        preset: a.preset || '',
        mode: a.mode || 'assisted',
        type: a.type || '',
        team: a.team || '',
        enforcerState: a.enforcer || '',
        trustLevel: a.trust_level ?? 3,
        restrictions: a.restrictions || [],
        mission: a.mission,
        missionStatus: a.mission_status,
      }));
      setAgents(mapped);
    } catch {
      setAgents([]);
    }
  }, []);

  const loadEgressDomains = useCallback(async () => {
    try {
      setEgressDomainsLoading(true);
      setEgressDomainsError(null);
      const data = await api.admin.egressDomains();
      setEgressDomains(data ?? []);
    } catch (e: any) {
      setEgressDomainsError(e.message || 'Failed to load egress domains');
    } finally {
      setEgressDomainsLoading(false);
    }
  }, []);

  const loadProvenance = async (domain: string) => {
    try {
      setProvenanceDomain(domain);
      setProvenanceLoading(true);
      const data = await api.admin.egressDomainProvenance(domain);
      setProvenanceData(data);
    } catch {
      setProvenanceData(null);
    } finally {
      setProvenanceLoading(false);
    }
  };

  const loadEgress = async (agent?: string) => {
    const target = agent || egressAgent;
    if (!target) return;
    try {
      setEgressLoading(true);
      setEgressError(null);
      const data = await api.admin.egress(target);
      setEgressData(data);
    } catch (e: any) {
      setEgressError(e.message || 'Failed to load egress status');
    } finally {
      setEgressLoading(false);
    }
  };

  useEffect(() => {
    loadAgents();
    loadEgressDomains();
  }, [loadAgents, loadEgressDomains]);

  // Auto-select when there's only one agent
  useEffect(() => {
    if (agents.length === 1 && !egressAgent) {
      setEgressAgent(agents[0].name);
    }
  }, [agents, egressAgent]);

  useEffect(() => {
    if (egressAgent && !egressData && !egressLoading) {
      loadEgress(egressAgent);
    }
  }, [egressAgent]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="space-y-4">
      {/* Platform-wide domain provenance */}
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-foreground/80">Domain Provenance</h3>
        <Button variant="outline" size="sm" onClick={loadEgressDomains} disabled={egressDomainsLoading}>
          {egressDomainsLoading ? 'Loading...' : 'Refresh'}
        </Button>
      </div>

      {egressDomainsError && (
        <div className="text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
          {egressDomainsError}
        </div>
      )}

      {!egressDomainsLoading && egressDomains.length === 0 && !egressDomainsError ? (
        <div className="text-sm text-muted-foreground text-center py-6 bg-card border border-border rounded">
          Domains are auto-provisioned when connectors are activated. Use{' '}
          <code className="text-foreground/80">agency hub activate</code> to set up connectors.
        </div>
      ) : egressDomains.length > 0 && (
        <div className="bg-card border border-border rounded overflow-hidden">
          <div className="overflow-x-auto max-h-[400px] overflow-y-auto">
            <table className="w-full text-sm min-w-[500px]">
              <thead className="sticky top-0 bg-card">
                <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
                  <th className="text-left p-3 font-medium">Domain</th>
                  <th className="text-left p-3 font-medium">Sources</th>
                  <th className="text-left p-3 font-medium">Managed</th>
                  <th className="text-left p-3 font-medium w-20"></th>
                </tr>
              </thead>
              <tbody>
                {egressDomains.map((entry: any) => (
                  <tr key={entry.domain} className="border-b border-border hover:bg-secondary/50 transition-colors">
                    <td className="p-3 font-mono text-xs text-foreground">{entry.domain}</td>
                    <td className="p-3">
                      <div className="flex flex-wrap gap-1">
                        {(entry.sources || []).map((s: any, i: number) => (
                          <span key={i} className="text-[10px] bg-secondary text-muted-foreground px-1.5 py-0.5 rounded">
                            {s.type}:{s.name}
                          </span>
                        ))}
                      </div>
                    </td>
                    <td className="p-3 text-xs text-muted-foreground">
                      {entry.auto_managed ? 'auto' : 'manual'}
                    </td>
                    <td className="p-3">
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-6 text-[10px]"
                        onClick={() => loadProvenance(entry.domain)}
                      >
                        Details
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Provenance detail */}
      {provenanceDomain && provenanceData && (
        <div className="bg-card border border-border rounded p-4 space-y-3">
          <div className="flex items-center justify-between">
            <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
              Provenance: <code className="text-foreground normal-case">{provenanceDomain}</code>
            </h4>
            <Button variant="ghost" size="sm" className="h-6 text-[10px]" onClick={() => { setProvenanceDomain(null); setProvenanceData(null); }}>
              Close
            </Button>
          </div>
          {provenanceLoading ? (
            <div className="text-sm text-muted-foreground text-center py-4">Loading...</div>
          ) : (
            <>
              <div className="text-xs text-muted-foreground">
                {provenanceData.auto_managed ? 'Auto-managed' : 'Manually added'}
              </div>
              {provenanceData.sources?.length > 0 && (
                <div className="space-y-1">
                  {provenanceData.sources.map((s: any, i: number) => (
                    <div key={i} className="text-xs text-muted-foreground flex gap-2">
                      <span className="text-foreground/80">{s.type}</span>
                      <code>{s.name}</code>
                      {s.added_at && <span className="text-muted-foreground/70">{formatDateTime(s.added_at)}</span>}
                    </div>
                  ))}
                </div>
              )}
              {provenanceData.active_dependents?.length > 0 && (
                <div>
                  <div className="text-xs text-muted-foreground mb-1">Active dependents</div>
                  <div className="flex flex-wrap gap-1">
                    {provenanceData.active_dependents.map((d: string) => (
                      <span key={d} className="text-xs bg-secondary text-muted-foreground px-1.5 py-0.5 rounded font-mono">{d}</span>
                    ))}
                  </div>
                </div>
              )}
            </>
          )}
        </div>
      )}

      {/* Per-agent egress */}
      <div className="border-t border-border pt-4 mt-4">
        <h3 className="text-sm font-semibold text-foreground/80 mb-3">Per-Agent Egress</h3>
        <div className="flex flex-wrap items-center gap-3 md:gap-4">
          <Select
            value={egressAgent}
            onValueChange={(v) => { setEgressAgent(v); loadEgress(v); }}
          >
            <SelectTrigger className="w-full sm:w-64 bg-card border-border">
              <SelectValue placeholder="Select agent..." />
            </SelectTrigger>
            <SelectContent>
              {agents.map((agent) => (
                <SelectItem key={agent.id} value={agent.name}>
                  {agent.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button variant="outline" size="sm" onClick={() => loadEgress()} disabled={egressLoading || !egressAgent}>
            {egressLoading ? 'Loading...' : 'Refresh'}
          </Button>
        </div>

        {egressError && (
          <div className="mt-3 text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
            {egressError}
          </div>
        )}

        {!egressData ? (
          <div className="text-sm text-muted-foreground text-center py-6">
            Select an agent to view per-agent egress
          </div>
        ) : (
          <div className="mt-3 space-y-4">
            <div className="flex justify-end">
              <ExportButton data={egressData} filename={`egress-${egressAgent}`} />
            </div>
            {(egressData.allowed_domains || egressData.domains) && (
              <div className="bg-card border border-border rounded p-4">
                <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-3">Allowed Domains</h4>
                <div className="flex flex-wrap gap-2">
                  {(egressData.allowed_domains || egressData.domains || []).map((domain: string) => (
                    <span key={domain} className="text-xs bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-400 px-2 py-1 rounded font-mono">
                      {domain}
                    </span>
                  ))}
                  {(egressData.allowed_domains || egressData.domains || []).length === 0 && (
                    <span className="text-xs text-muted-foreground/70">No domains configured</span>
                  )}
                </div>
              </div>
            )}
            {(() => {
              const { allowed_domains, domains, ...rest } = egressData;
              return Object.keys(rest).length > 0 ? <JsonView data={rest} /> : null;
            })()}
          </div>
        )}
      </div>
    </div>
  );
}
