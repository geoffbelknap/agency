import { useState, useEffect } from 'react';
import { Link } from 'react-router';
import { api } from '../lib/api';
import { Connector, WorkItem } from '../types';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs';
import { EmptyState } from '../components/EmptyState';
import { Cable, ClipboardList, Settings, CheckCircle, XCircle, ChevronDown, ChevronRight, RefreshCw } from 'lucide-react';
import { toast } from 'sonner';

type ConnectorCredentialRequirement = {
  name?: string;
  key?: string;
  description?: string;
  required?: boolean;
  configured?: boolean;
  setup_url?: string;
  placeholder?: string;
};

type ConnectorEgressDomainRequirement = string | { domain?: string; allowed?: boolean };

type ConnectorSetupData = {
  connector: string;
  version?: string;
  ready: boolean;
  credentials?: ConnectorCredentialRequirement[];
  auth?: { type?: string; configured?: boolean };
  egress_domains?: ConnectorEgressDomainRequirement[];
};

function mapConnector(raw: any): Connector {
  return {
    id: raw.id ?? raw.name,
    name: raw.name,
    kind: raw.kind,
    source: raw.source ?? '',
    state: raw.state,
    version: raw.version,
  };
}

function mapWorkItem(raw: any): WorkItem {
  return {
    id: raw.id,
    connector: raw.connector ?? raw.source ?? '',
    status: raw.status ?? raw.state ?? 'unrouted',
    target_type: raw.target_type,
    target_name: raw.target_name,
    payload: typeof raw.payload === 'string' ? raw.payload : raw.payload != null ? JSON.stringify(raw.payload, null, 2) : undefined,
    created_at: raw.created_at ?? raw.created ?? '',
    route_index: raw.route_index,
    priority: raw.priority,
    // legacy
    state: raw.state,
    source: raw.source,
    summary: raw.summary,
    created: raw.created,
  };
}

function workItemStatusBadge(status: string) {
  if (status === 'routed' || status === 'assigned') {
    return 'bg-emerald-950 text-emerald-400';
  }
  if (status === 'relayed') {
    return 'bg-cyan-950 text-cyan-400';
  }
  return 'bg-secondary text-muted-foreground';
}

function connectorPollStatus(health: Record<string, unknown> | null, connectorName: string): Record<string, unknown> | null {
  const connectors = health?.connectors;
  if (connectors && typeof connectors === 'object' && !Array.isArray(connectors)) {
    const value = (connectors as Record<string, unknown>)[connectorName];
    return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null;
  }
  const direct = health?.[connectorName];
  return direct && typeof direct === 'object' && !Array.isArray(direct) ? direct as Record<string, unknown> : null;
}

function connectorSourceLabel(source?: string) {
  const normalized = (source || '').trim().toLowerCase();
  if (!normalized) return 'Unknown source';
  if (normalized === 'local') return 'Local operator content';
  if (normalized.startsWith('hub:') || normalized === 'official' || normalized === 'default') return 'Hub catalog';
  return `Custom source: ${source}`;
}

function connectorReadiness(connector: Connector, pollStatus: Record<string, unknown> | null) {
  const pollState = String(pollStatus?.status ?? pollStatus?.state ?? '').toLowerCase();
  if (connector.state !== 'active') {
    return {
      tone: 'amber' as const,
      title: 'Inactive connector',
      detail: 'This connector is installed but not currently ingesting work. Activate it after verifying setup and egress requirements.',
    };
  }
  if (!pollStatus) {
    return {
      tone: 'muted' as const,
      title: 'No poll health yet',
      detail: 'This connector is active, but intake has not reported polling health yet. Refresh health or trigger a manual poll if this is a poll-based connector.',
    };
  }
  if (pollState === 'healthy' || pollState === 'ok' || pollState === 'ready') {
    return {
      tone: 'emerald' as const,
      title: 'Ready to ingest',
      detail: 'Connector state and intake health both look good. Use Poll now or inspect work items if you need to confirm delivery.',
    };
  }
  return {
    tone: 'amber' as const,
    title: 'Needs connector review',
    detail: 'Connector is active but intake health is degraded or unknown. Inspect setup, credentials, egress, and recent work-item behavior.',
  };
}

function targetHref(targetType?: string, targetName?: string): string | null {
  if (!targetType || !targetName) return null;
  if (targetType === 'agent') return `/agents/${encodeURIComponent(targetName)}`;
  if (targetType === 'mission') return `/missions/${encodeURIComponent(targetName)}`;
  if (targetType === 'channel') return `/channels/${encodeURIComponent(targetName)}`;
  return null;
}

function workItemGuidance(item: WorkItem) {
  if (item.status === 'unrouted') {
    return {
      tone: 'amber' as const,
      title: 'Needs routing',
      detail: 'No route target has been assigned yet. Inspect the payload and connector config, then check whether a mission or routing rule should claim this item.',
    };
  }
  if (item.status === 'relayed') {
    return {
      tone: 'cyan' as const,
      title: 'Relayed downstream',
      detail: item.target_name
        ? 'This item was relayed to another target. Open the route target to continue the workflow there.'
        : 'This item was relayed downstream. Inspect the payload and event history if you need to confirm where it went.',
    };
  }
  if (item.status === 'routed' || item.status === 'assigned') {
    return {
      tone: 'emerald' as const,
      title: 'Route target assigned',
      detail: item.target_name
        ? 'Continue in the assigned route target to see handling and follow-up work.'
        : 'This item has already been routed and is waiting on the target workflow.',
    };
  }
  return {
    tone: 'muted' as const,
    title: 'Pending review',
    detail: 'This item is still in intake. Refresh after polling or inspect the payload for routing context.',
  };
}

function workItemAttribution(item: WorkItem, connectors: Connector[]) {
  const connector = connectors.find((candidate) => candidate.name === item.connector);
  if (!connector) {
    return {
      tone: 'amber' as const,
      title: 'Connector definition missing',
      detail: 'This work item references a connector that is not currently present in Intake. Refresh connectors or inspect hub state before debugging routing.',
    };
  }
  if (connector.state !== 'active') {
    return {
      tone: 'amber' as const,
      title: 'Connector is inactive',
      detail: 'The connector that produced this item is not active now. Re-check setup, activation, and poll health before assuming the route logic is broken.',
    };
  }
  if (!item.target_name && (item.status === 'unrouted' || item.status === 'pending')) {
    return {
      tone: 'muted' as const,
      title: 'Route rule likely missing',
      detail: 'The connector delivered the item, but no mission, agent, or channel claimed it. Inspect routing rules and mission triggers next.',
    };
  }
  if (item.target_name && !targetHref(item.target_type, item.target_name)) {
    return {
      tone: 'amber' as const,
      title: 'Target type needs review',
      detail: 'This item has a route target, but the UI does not know how to open it directly. Check the recorded target type and the downstream workflow.',
    };
  }
  return {
    tone: 'emerald' as const,
    title: 'Connector-to-route chain looks intact',
    detail: 'The connector and route target metadata line up. Continue in the target workflow or inspect payload details if handling still looks wrong.',
  };
}

function intakeNextStepSummary(connectors: Connector[], workItems: WorkItem[], activeConnectors: number, unroutedItems: number) {
  if (connectors.length === 0) {
    return {
      tone: 'muted' as const,
      title: 'Start by adding a connector',
      detail: 'Hub is the right next stop when Intake is empty. Install a connector, complete setup, then return here to verify work delivery.',
      links: [
        { label: 'Open Hub', href: '/admin/hub' },
        { label: 'Open Missions', href: '/missions' },
      ],
    };
  }
  if (unroutedItems > 0) {
    return {
      tone: 'amber' as const,
      title: 'Work is arriving but needs routing',
      detail: 'Connectors are delivering items, but at least one item is not being claimed. Review mission triggers and recent routing events next.',
      links: [
        { label: 'Open Missions', href: '/missions' },
        { label: 'Open Events', href: '/admin/events' },
      ],
    };
  }
  if (activeConnectors > 0 && workItems.length === 0) {
    return {
      tone: 'cyan' as const,
      title: 'Connectors are ready, now verify delivery',
      detail: 'At least one connector is active, but no work items are visible yet. Refresh health, poll where appropriate, or inspect recent events for delivery clues.',
      links: [
        { label: 'Open Events', href: '/admin/events' },
        { label: 'Open Hub', href: '/admin/hub' },
      ],
    };
  }
  if (workItems.length > 0) {
    return {
      tone: 'emerald' as const,
      title: 'Intake is delivering work',
      detail: 'Follow routed items into their assigned mission, channel, or agent workflow, and use Events when handling looks wrong.',
      links: [
        { label: 'Open Missions', href: '/missions' },
        { label: 'Open Channels', href: '/channels' },
      ],
    };
  }
  return null;
}

export function Intake() {
  const [activeTab, setActiveTab] = useState<'connectors' | 'work-items'>('connectors');
  const [connectors, setConnectors] = useState<Connector[]>([]);
  const [workItems, setWorkItems] = useState<WorkItem[]>([]);
  const [pollHealth, setPollHealth] = useState<Record<string, unknown> | null>(null);
  const [pollHealthLoading, setPollHealthLoading] = useState(false);
  const [pollingConnector, setPollingConnector] = useState<string | null>(null);

  const [expandedConnector, setExpandedConnector] = useState<string | null>(null);
  const [expandedItem, setExpandedItem] = useState<string | null>(null);

  const loadWorkItems = async () => {
    const data = await api.intake.items();
    setWorkItems((data ?? []).map(mapWorkItem));
  };

  const loadPollHealth = async () => {
    setPollHealthLoading(true);
    try {
      const data = await api.intake.pollHealth();
      setPollHealth(data ?? {});
    } catch (err: any) {
      setPollHealth(null);
      toast.error(`Failed to load polling health: ${err.message}`);
    } finally {
      setPollHealthLoading(false);
    }
  };

  useEffect(() => {
    api.connectors
      .list()
      .then((data) => setConnectors((data ?? []).map(mapConnector)))
      .catch((err) => toast.error(`Failed to load connectors: ${err.message}`));

    loadWorkItems().catch((err) => toast.error(`Failed to load work items: ${err.message}`));
    loadPollHealth();
  }, []);

  const handleToggle = async (connector: Connector) => {
    try {
      if (connector.state === 'active') {
        await api.connectors.deactivate(connector.name);
        setConnectors((prev) =>
          prev.map((c) => (c.name === connector.name ? { ...c, state: 'inactive' } : c)),
        );
        toast.success(`Connector "${connector.name}" deactivated`);
      } else {
        await api.connectors.activate(connector.name);
        setConnectors((prev) =>
          prev.map((c) => (c.name === connector.name ? { ...c, state: 'active' } : c)),
        );
        toast.success(`Connector "${connector.name}" activated`);
      }
    } catch (err: any) {
      toast.error(`Failed to toggle connector: ${err.message}`);
    }
  };

  const handleStatus = async (connector: Connector) => {
    try {
      const result = await api.connectors.status(connector.name);
      toast.info(`"${connector.name}" status: ${result.state ?? JSON.stringify(result)}`);
    } catch (err: any) {
      toast.error(`Failed to get status: ${err.message}`);
    }
  };

  const handlePollNow = async (connector: Connector) => {
    try {
      setPollingConnector(connector.name);
      await api.intake.triggerPoll(connector.name);
      toast.success(`Poll triggered for "${connector.name}"`);
      await Promise.all([
        loadWorkItems().catch((err) => toast.error(`Failed to refresh work items: ${err.message}`)),
        loadPollHealth(),
      ]);
    } catch (err: any) {
      toast.error(`Failed to trigger poll: ${err.message}`);
    } finally {
      setPollingConnector(null);
    }
  };

  // Connector setup
  const [setupConnector, setSetupConnector] = useState<string | null>(null);
  const [setupData, setSetupData] = useState<ConnectorSetupData | null>(null);
  const [setupLoading, setSetupLoading] = useState(false);
  const [credValues, setCredValues] = useState<Record<string, string>>({});
  const [configuring, setConfiguring] = useState(false);

  const handleSetup = async (name: string) => {
    try {
      setSetupConnector(name);
      setSetupLoading(true);
      setCredValues({});
      const data = await api.connectors.requirements(name);
      setSetupData(data);
    } catch (err: any) {
      toast.error(`Failed to load requirements: ${err.message}`);
      setSetupConnector(null);
    } finally {
      setSetupLoading(false);
    }
  };

  const handleConfigure = async () => {
    if (!setupConnector) return;
    try {
      setConfiguring(true);
      const result = await api.connectors.configure(setupConnector, credValues);
      if (result.ready) {
        await api.connectors.activate(setupConnector);
        toast.success(`Connector "${setupConnector}" configured and activated`);
      } else {
        toast.success(`Configured: ${result.configured.join(', ')}`);
      }
      setSetupConnector(null);
      setSetupData(null);
      const data = await api.connectors.list();
      setConnectors((data ?? []).map(mapConnector));
    } catch (err: any) {
      toast.error(`Configuration failed: ${err.message}`);
    } finally {
      setConfiguring(false);
    }
  };

  const handleActivateFromSetup = async () => {
    if (!setupConnector) return;
    try {
      setConfiguring(true);
      await api.connectors.activate(setupConnector);
      toast.success(`Connector "${setupConnector}" activated`);
      setSetupConnector(null);
      setSetupData(null);
      const data = await api.connectors.list();
      setConnectors((data ?? []).map(mapConnector));
    } catch (err: any) {
      toast.error(`Activation failed: ${err.message}`);
    } finally {
      setConfiguring(false);
    }
  };

  const missingCredentialNames = (setupData?.credentials ?? [])
    .filter((cred) => {
      const name = cred.name || cred.key;
      if (!name || cred.configured) return false;
      return cred.required !== false && !credValues[name]?.trim();
    })
    .map((cred) => cred.name || cred.key)
    .filter(Boolean) as string[];

  // Work item stats derived from items
  const totalItems = workItems.length;
  const routedItems = workItems.filter((i) => i.status === 'routed' || i.status === 'assigned').length;
  const relayedItems = workItems.filter((i) => i.status === 'relayed').length;
  const unroutedItems = workItems.filter((i) => i.status !== 'routed' && i.status !== 'assigned' && i.status !== 'relayed').length;
  const activeConnectors = connectors.filter((connector) => connector.state === 'active').length;
  const inactiveConnectors = connectors.filter((connector) => connector.state !== 'active').length;
  const healthyConnectors = connectors.filter((connector) => {
    const pollState = String(connectorPollStatus(pollHealth, connector.name)?.status ?? connectorPollStatus(pollHealth, connector.name)?.state ?? '').toLowerCase();
    return pollState === 'healthy' || pollState === 'ok' || pollState === 'ready';
  }).length;
  const connectorsNeedingAttention = connectors.filter((connector) => {
    const readiness = connectorReadiness(connector, connectorPollStatus(pollHealth, connector.name));
    return readiness.tone !== 'emerald';
  }).length;
  const nextStepSummary = intakeNextStepSummary(connectors, workItems, activeConnectors, unroutedItems);

  return (
    <div className="space-y-4">
      {nextStepSummary && (
        <div
          className={`rounded border px-4 py-3 ${
            nextStepSummary.tone === 'amber' ? 'border-amber-900/50 bg-amber-950/20 text-amber-300' :
            nextStepSummary.tone === 'cyan' ? 'border-cyan-900/50 bg-cyan-950/20 text-cyan-300' :
            nextStepSummary.tone === 'emerald' ? 'border-emerald-900/50 bg-emerald-950/20 text-emerald-300' :
            'border-border bg-card text-foreground'
          }`}
        >
          <div className="text-sm font-medium">{nextStepSummary.title}</div>
          <div className="mt-1 text-xs opacity-90">{nextStepSummary.detail}</div>
          <div className="mt-3 flex flex-wrap gap-2">
            {nextStepSummary.links.map((link) => (
              <Button key={link.href} asChild variant="outline" size="sm" className="h-7 text-xs">
                <Link to={link.href}>{link.label}</Link>
              </Button>
            ))}
          </div>
        </div>
      )}
      <Tabs value={activeTab} onValueChange={(value) => setActiveTab(value as 'connectors' | 'work-items')} className="space-y-6">
        <TabsList>
          <TabsTrigger value="connectors">Connectors</TabsTrigger>
          <TabsTrigger value="work-items">Work Items</TabsTrigger>
        </TabsList>

        {/* ── Connectors Tab ── */}
        <TabsContent value="connectors">
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <div className="bg-card border border-border rounded p-3">
              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">Active</div>
              <div className="text-2xl font-semibold text-emerald-400">{activeConnectors}</div>
              <div className="text-xs text-muted-foreground">Currently able to ingest work.</div>
            </div>
            <div className="bg-card border border-border rounded p-3">
              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">Inactive</div>
              <div className="text-2xl font-semibold text-muted-foreground">{inactiveConnectors}</div>
              <div className="text-xs text-muted-foreground">Installed but not ingesting yet.</div>
            </div>
            <div className="bg-card border border-border rounded p-3">
              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">Healthy Polling</div>
              <div className="text-2xl font-semibold text-cyan-400">{healthyConnectors}</div>
              <div className="text-xs text-muted-foreground">Connectors with healthy intake status.</div>
            </div>
            <div className="bg-card border border-border rounded p-3">
              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">Needs Review</div>
              <div className="text-2xl font-semibold text-amber-400">{connectorsNeedingAttention}</div>
              <div className="text-xs text-muted-foreground">Inactive or degraded connectors worth checking.</div>
            </div>
          </div>

          <div className="bg-card border border-border rounded p-3 mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <div className="text-sm font-medium text-foreground">Polling health</div>
              <div className="text-xs text-muted-foreground">
                {pollHealth
                  ? 'Intake polling status is available for configured connectors.'
                  : 'Polling status is unavailable until the intake service responds.'}
              </div>
            </div>
            <Button
              variant="outline"
              size="sm"
              className="h-8 text-xs self-start sm:self-auto"
              onClick={loadPollHealth}
              disabled={pollHealthLoading}
            >
              <RefreshCw className={`w-3 h-3 mr-1 ${pollHealthLoading ? 'animate-spin' : ''}`} />
              Refresh health
            </Button>
          </div>

          <div className="bg-card border border-border rounded overflow-hidden">
            {connectors.length === 0 ? (
              <EmptyState
                icon={<Cable className="w-8 h-8" />}
                title="No connectors configured"
                description="Connectors bring external work into the platform. Configure webhook, poll, schedule, or channel-watch connectors to get started."
              />
            ) : (
              <div className="divide-y divide-border">
                {connectors.map((connector) => {
                  const isExpanded = expandedConnector === connector.id;
                  const pollStatus = connectorPollStatus(pollHealth, connector.name);
                  const pollStatusText = String(pollStatus?.status ?? pollStatus?.state ?? 'unknown');
                  const readiness = connectorReadiness(connector, pollStatus);
                  return (
                    <div key={connector.id}>
                      {/* Collapsed row — clickable button */}
                      <button
                        className="w-full text-left px-4 py-3 flex items-center gap-3 hover:bg-secondary/40 transition-colors"
                        onClick={() => setExpandedConnector(isExpanded ? null : connector.id)}
                      >
                        {/* Status dot */}
                        <span
                          className={`w-2 h-2 rounded-full flex-shrink-0 ${
                            connector.state === 'active' ? 'bg-emerald-500' : 'bg-muted-foreground/30'
                          }`}
                        />

                        {/* Name */}
                        <span className="text-sm text-foreground font-medium flex-1 min-w-0 truncate">
                          {connector.name}
                        </span>

                        {/* Kind badge */}
                        <span className="text-xs bg-secondary text-muted-foreground px-2 py-0.5 rounded flex-shrink-0">
                          {connector.kind}
                        </span>

                        {/* Source */}
                        <span className="text-xs text-muted-foreground font-mono truncate max-w-[180px] hidden sm:block">
                          {connector.source}
                        </span>

                        {/* Version badge */}
                        {connector.version && (
                          <span className="font-mono text-[10px] bg-secondary text-muted-foreground px-1.5 py-0.5 rounded flex-shrink-0">
                            v{connector.version}
                          </span>
                        )}

                        {/* Expand chevron */}
                        {isExpanded ? (
                          <ChevronDown className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                        ) : (
                          <ChevronRight className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                        )}
                      </button>

                      {/* Expanded content */}
                      {isExpanded && (
                        <div className="bg-background border-t border-border px-4 py-4 space-y-4">
                          <div className="grid grid-cols-3 gap-4">
                            <div>
                              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">Source</div>
                              <div className="text-xs text-foreground/80">{connectorSourceLabel(connector.source)}</div>
                              <code className="text-[11px] text-muted-foreground break-all">{connector.source || '—'}</code>
                            </div>
                            <div>
                              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">State</div>
                              <div className="flex items-center gap-1.5">
                                <span
                                  className={`w-1.5 h-1.5 rounded-full ${
                                    connector.state === 'active' ? 'bg-emerald-500' : 'bg-muted-foreground/30'
                                  }`}
                                />
                                <span className="text-xs text-foreground/80 capitalize">{connector.state}</span>
                              </div>
                            </div>
                            <div>
                              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">Poll Health</div>
                              <span className="text-xs text-foreground/80 capitalize">{pollStatusText}</span>
                            </div>
                          </div>
                          <div
                            className={`rounded border px-3 py-2 text-xs ${
                              readiness.tone === 'amber' ? 'border-amber-900/50 bg-amber-950/20 text-amber-300' :
                              readiness.tone === 'emerald' ? 'border-emerald-900/50 bg-emerald-950/20 text-emerald-300' :
                              'border-border bg-secondary/40 text-muted-foreground'
                            }`}
                          >
                            <div className="font-medium">{readiness.title}</div>
                            <div className="mt-1">{readiness.detail}</div>
                          </div>
                          {pollStatus && (
                            <pre className="font-mono text-[11px] bg-secondary/40 border border-border rounded p-2 overflow-x-auto whitespace-pre-wrap break-all">
                              {JSON.stringify(pollStatus, null, 2)}
                            </pre>
                          )}
                          <div className="flex gap-2">
                            <Button
                              variant="outline"
                              size="sm"
                              className="h-7 text-xs"
                              onClick={() => handleSetup(connector.name)}
                            >
                              <Settings className="w-3 h-3 mr-1" />
                              Setup
                            </Button>
                            <Button
                              variant="outline"
                              size="sm"
                              className="h-7 text-xs"
                              onClick={() => handleStatus(connector)}
                            >
                              Status
                            </Button>
                            <Button
                              variant="outline"
                              size="sm"
                              className="h-7 text-xs"
                              onClick={() => handleToggle(connector)}
                            >
                              {connector.state === 'active' ? 'Deactivate' : 'Activate'}
                            </Button>
                            <Button
                              variant="outline"
                              size="sm"
                              className="h-7 text-xs"
                              onClick={() => handlePollNow(connector)}
                              disabled={pollingConnector === connector.name}
                            >
                              <RefreshCw className={`w-3 h-3 mr-1 ${pollingConnector === connector.name ? 'animate-spin' : ''}`} />
                              {pollingConnector === connector.name ? 'Polling...' : 'Poll now'}
                            </Button>
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          {/* Connector Setup Panel */}
          {setupConnector && (
            <div className="mt-4 bg-card border border-border rounded p-4 space-y-4">
              <div className="flex items-center justify-between">
                <h3 className="text-sm font-semibold text-foreground/80">
                  Setup: <code>{setupConnector}</code>
                </h3>
                <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={() => { setSetupConnector(null); setSetupData(null); }}>
                  Close
                </Button>
              </div>

              {setupLoading ? (
                <div className="text-sm text-muted-foreground text-center py-4">Loading requirements...</div>
              ) : !setupData ? (
                <div className="text-sm text-muted-foreground text-center py-4">No requirements data</div>
              ) : (
                <>
                  <div className="flex items-center gap-2 text-sm">
                    {setupData.ready ? (
                      <CheckCircle className="w-4 h-4 text-green-500" />
                    ) : (
                      <XCircle className="w-4 h-4 text-amber-500" />
                    )}
                    <span className={setupData.ready ? 'text-green-400' : 'text-amber-400'}>
                      {setupData.ready ? 'Ready' : 'Not configured'}
                    </span>
                    {setupData.version && (
                      <span className="text-xs text-muted-foreground ml-2">v{setupData.version}</span>
                    )}
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Setup saves required credentials, applies connector egress rules, and activates the connector when it is ready.
                  </p>

                  {setupData.egress_domains && setupData.egress_domains.length > 0 && (
                    <div>
                      <div className="text-xs text-muted-foreground mb-1">Required egress domains</div>
                      <div className="flex flex-wrap gap-1">
                        {setupData.egress_domains.map((d: any, i: number) => {
                          const domain = typeof d === 'string' ? d : d.domain;
                          const allowed = typeof d === 'object' ? d.allowed : true;
                          return (
                            <span key={domain || i} className={`text-xs px-2 py-0.5 rounded font-mono ${allowed ? 'bg-green-950 text-green-400' : 'bg-secondary text-muted-foreground'}`}>
                              {domain}{allowed ? ' \u2713' : ''}
                            </span>
                          );
                        })}
                      </div>
                    </div>
                  )}

                  {setupData.credentials && setupData.credentials.length > 0 && (
                    <div className="space-y-3">
                      <div className="text-xs text-muted-foreground uppercase tracking-wide">Credentials</div>
                      {setupData.credentials.map((cred: any, i: number) => {
                        const name = cred.name || cred.key || `credential_${i}`;
                        return (
                          <div key={name} className="space-y-1">
                            <label className="text-xs text-foreground/80 flex flex-wrap items-center gap-1.5">
                              <code>{name}</code>
                              {cred.required && <span className="text-red-400">*</span>}
                              {cred.configured && <span className="text-emerald-400">configured</span>}
                              {cred.description && (
                                <span className="text-muted-foreground/70 font-normal">— {cred.description}</span>
                              )}
                              {cred.setup_url && (
                                <a
                                  className="text-cyan-400 hover:underline"
                                  href={cred.setup_url}
                                  target="_blank"
                                  rel="noreferrer"
                                >
                                  get key
                                </a>
                              )}
                            </label>
                            <Input
                              type="password"
                              value={credValues[name] || ''}
                              onChange={(e) => setCredValues((prev) => ({ ...prev, [name]: e.target.value }))}
                              placeholder={cred.configured ? 'Already configured' : cred.placeholder || name}
                              className="bg-background border-border text-foreground h-8 text-sm font-mono"
                              disabled={cred.configured}
                            />
                          </div>
                        );
                      })}
                      {missingCredentialNames.length > 0 && (
                        <div className="text-xs text-amber-400">
                          Missing: {missingCredentialNames.join(', ')}
                        </div>
                      )}
                      <Button size="sm" onClick={handleConfigure} disabled={configuring || missingCredentialNames.length > 0}>
                        {configuring ? 'Configuring...' : 'Configure and activate'}
                      </Button>
                    </div>
                  )}

                  {(!setupData.credentials || setupData.credentials.length === 0) && setupData.ready && (
                    <div className="space-y-3">
                      <div className="text-sm text-muted-foreground">No credentials required — connector is ready.</div>
                      <Button size="sm" onClick={handleActivateFromSetup} disabled={configuring}>
                        {configuring ? 'Activating...' : 'Activate connector'}
                      </Button>
                    </div>
                  )}
                </>
              )}
            </div>
          )}
        </TabsContent>

        {/* ── Work Items Tab ── */}
        <TabsContent value="work-items" className="space-y-4">
          <div className="bg-card border border-border rounded p-3 flex items-center justify-between gap-3">
            <div>
              <div className="text-sm font-medium text-foreground">Inbound work queue</div>
              <div className="text-xs text-muted-foreground">
                Review connector-delivered work, inspect payloads, and jump to the current route target when one exists.
              </div>
            </div>
            <Button
              variant="outline"
              size="sm"
              className="h-8 text-xs"
              onClick={() => loadWorkItems().catch((err) => toast.error(`Failed to refresh work items: ${err.message}`))}
            >
              <RefreshCw className="w-3 h-3 mr-1" />
              Refresh work items
            </Button>
          </div>

          {unroutedItems > 0 && (
            <div className="bg-card border border-border rounded p-4 space-y-2">
              <div className="text-sm font-medium text-amber-400">
                {unroutedItems === 1 ? '1 item needs routing' : `${unroutedItems} items need routing`}
              </div>
              <div className="text-xs text-muted-foreground">
                Unrouted intake usually means the connector delivered work successfully, but no mission or route target matched it yet.
              </div>
            </div>
          )}

          {/* Stats bar */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <div className="bg-card border border-border rounded p-3">
              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">Total</div>
              <div className="text-2xl font-semibold text-foreground">{totalItems}</div>
            </div>
            <div className="bg-card border border-border rounded p-3">
              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">Routed</div>
              <div className="text-2xl font-semibold text-emerald-400">{routedItems}</div>
            </div>
            <div className="bg-card border border-border rounded p-3">
              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">Relayed</div>
              <div className="text-2xl font-semibold text-cyan-400">{relayedItems}</div>
            </div>
            <div className="bg-card border border-border rounded p-3">
              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">Unrouted</div>
              <div className="text-2xl font-semibold text-muted-foreground">{unroutedItems}</div>
            </div>
          </div>

          {/* Work items list */}
          <div className="bg-card border border-border rounded overflow-hidden">
            {workItems.length === 0 ? (
              <EmptyState
                icon={<ClipboardList className="w-8 h-8" />}
                title="No work items yet"
                description="Work items will appear here when connectors deliver tasks. Configure and activate connectors to start receiving work."
              />
            ) : (
              <div className="divide-y divide-border">
                {workItems.map((item) => {
                  const isExpanded = expandedItem === item.id;
                  const routeTarget = item.target_name
                    ? item.target_type
                      ? `${item.target_type}: ${item.target_name}`
                      : item.target_name
                    : '—';
                  const routeHref = targetHref(item.target_type, item.target_name);
                  const guidance = workItemGuidance(item);
                  const attribution = workItemAttribution(item, connectors);

                  // payload preview: first 80 chars of JSON
                  const payloadPreview = item.payload
                    ? item.payload.length > 80
                      ? item.payload.slice(0, 80) + '…'
                      : item.payload
                    : null;
                  const summaryText = item.summary || 'No summary';

                  return (
                    <div key={item.id}>
                      <button
                        className="w-full text-left px-4 py-3 flex items-center gap-3 hover:bg-secondary/40 transition-colors"
                        onClick={() => setExpandedItem(isExpanded ? null : item.id)}
                      >
                        {/* Connector name */}
                        <span className="text-sm text-foreground font-medium flex-shrink-0 w-28 truncate">
                          {item.connector || '—'}
                        </span>

                        {/* Status badge */}
                        <span className={`text-xs px-2 py-0.5 rounded flex-shrink-0 ${workItemStatusBadge(item.status)}`}>
                          {item.status}
                        </span>

                        <span className="text-xs text-foreground/80 flex-1 min-w-0 truncate">
                          {summaryText}
                        </span>

                        {/* Route target */}
                        <span className="text-xs text-muted-foreground flex-shrink-0 truncate max-w-[220px] hidden lg:block">
                          {routeTarget}
                        </span>

                        {/* Timestamp */}
                        <span className="text-xs text-muted-foreground flex-shrink-0 hidden md:block">
                          {item.created_at}
                        </span>

                        {/* Payload preview */}
                        {payloadPreview && (
                          <span className="text-[10px] font-mono text-muted-foreground/60 truncate max-w-[160px] hidden lg:block">
                            {payloadPreview}
                          </span>
                        )}

                        {/* Chevron */}
                        {isExpanded ? (
                          <ChevronDown className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                        ) : (
                          <ChevronRight className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                        )}
                      </button>

                      {/* Expanded payload */}
                      {isExpanded && (
                        <div className="bg-background border-t border-border px-4 py-3 space-y-3">
                          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-xs">
                            <div>
                              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-0.5">Summary</div>
                              <span className="text-foreground/80">{summaryText}</span>
                            </div>
                            <div>
                              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-0.5">Connector</div>
                              <span className="text-foreground/80">{item.connector || '—'}</span>
                            </div>
                            <div>
                              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-0.5">Status</div>
                              <span className={`px-2 py-0.5 rounded ${workItemStatusBadge(item.status)}`}>{item.status}</span>
                            </div>
                            <div>
                              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-0.5">Route Target</div>
                              {routeHref ? (
                                <Link className="text-cyan-400 hover:underline" to={routeHref}>
                                  {routeTarget}
                                </Link>
                              ) : (
                                <span className="text-foreground/80">{routeTarget}</span>
                              )}
                            </div>
                            <div>
                              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-0.5">Created</div>
                              <span className="text-foreground/80">{item.created_at || '—'}</span>
                            </div>
                          </div>
                          <div
                            className={`rounded border px-3 py-2 text-xs ${
                              guidance.tone === 'amber' ? 'border-amber-900/50 bg-amber-950/20 text-amber-300' :
                              guidance.tone === 'cyan' ? 'border-cyan-900/50 bg-cyan-950/20 text-cyan-300' :
                              guidance.tone === 'emerald' ? 'border-emerald-900/50 bg-emerald-950/20 text-emerald-300' :
                              'border-border bg-secondary/40 text-muted-foreground'
                            }`}
                          >
                            <div className="font-medium">{guidance.title}</div>
                            <div className="mt-1">{guidance.detail}</div>
                            {item.route_index != null && (
                              <div className="mt-1 text-[11px] opacity-80">Matched route index: {item.route_index}</div>
                            )}
                          </div>
                          <div
                            className={`rounded border px-3 py-2 text-xs ${
                              attribution.tone === 'amber' ? 'border-amber-900/50 bg-amber-950/20 text-amber-300' :
                              attribution.tone === 'emerald' ? 'border-emerald-900/50 bg-emerald-950/20 text-emerald-300' :
                              'border-border bg-secondary/40 text-muted-foreground'
                            }`}
                          >
                            <div className="font-medium">{attribution.title}</div>
                            <div className="mt-1">{attribution.detail}</div>
                          </div>
                          {(item.route_index != null || item.target_name) && (
                            <div className="flex flex-wrap gap-2">
                              {routeHref && (
                                <Button asChild variant="outline" size="sm" className="h-7 text-xs">
                                  <Link to={routeHref}>Open route target</Link>
                                </Button>
                              )}
                              {item.connector && (
                                <Button
                                  variant="outline"
                                  size="sm"
                                  className="h-7 text-xs"
                                  onClick={() => {
                                    const connector = connectors.find((candidate) => candidate.name === item.connector);
                                    setActiveTab('connectors');
                                    setExpandedConnector(connector?.id ?? item.connector);
                                    setExpandedItem(item.id);
                                  }}
                                >
                                  Open connector
                                </Button>
                              )}
                            </div>
                          )}
                          {item.payload ? (
                            <div>
                              <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">Payload</div>
                              <pre className="font-mono text-xs bg-background border border-border rounded p-3 overflow-x-auto whitespace-pre-wrap break-all">
                                {item.payload}
                              </pre>
                            </div>
                          ) : (
                            <div className="text-xs text-muted-foreground">No payload</div>
                          )}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}
