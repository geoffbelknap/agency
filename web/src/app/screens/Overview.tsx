import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router';
import {
  ArrowRight,
  Bot,
  Brain,
  CheckCircle2,
  KeyRound,
  MessageSquare,
  Play,
  RotateCw,
  ShieldCheck,
  Square,
} from 'lucide-react';
import { toast } from 'sonner';
import { StatusIndicator } from '../components/StatusIndicator';
import { Badge } from '../components/ui/badge';
import { Button } from '../components/ui/button';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '../components/ui/card';
import { Progress } from '../components/ui/progress';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table';
import { api } from '../lib/api';
import { contractModules, contractSurfaces, surfaceIsVisible, type ContractSurface } from '../lib/contract-surface';
import { experimentalSurfacesEnabled } from '../lib/features';
import { formatTime } from '../lib/time';
import { socket } from '../lib/ws';
import { Agent, AuditEvent, InfrastructureService, Provider } from '../types';

type InfraAction = 'start' | 'stop' | 'restart';

function isRunningState(state: InfrastructureService['state']) {
  return state === 'running' || state === 'restarting';
}

function isStoppedState(state: InfrastructureService['state']) {
  return state === 'stopped' || state === 'missing' || state === 'exited' || state === 'dead';
}

function formatStateLabel(service: InfrastructureService, action: InfraAction | null) {
  if (action === 'start' && !isRunningState(service.state)) return 'starting';
  if (action === 'stop' && isRunningState(service.state)) return 'stopping';
  if (action === 'restart' && isRunningState(service.state)) return 'restarting';

  switch (service.state) {
    case 'missing':
      return 'not running';
    case 'created':
      return 'starting';
    case 'exited':
    case 'dead':
      return 'stopped';
    default:
      return service.state.replace(/_/g, ' ');
  }
}

function visualStatus(service: InfrastructureService, action: InfraAction | null) {
  if (action === 'start' && !isRunningState(service.state)) return 'starting';
  if (action === 'stop' && isRunningState(service.state)) return 'stopping';
  if (action === 'restart' && isRunningState(service.state)) return 'restarting';
  if (service.health === 'healthy') return 'healthy';
  if (service.health === 'unhealthy') return 'unhealthy';
  if (isStoppedState(service.state)) return 'idle';
  if (service.state === 'created') return 'starting';
  return 'idle';
}

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function surfaceBadgeVariant(surface: ContractSurface) {
  if (surface.tier === 'core') return 'secondary' as const;
  if (surface.tier === 'experimental') return 'outline' as const;
  return 'destructive' as const;
}

const readinessIcons = {
  runtime: CheckCircle2,
  fleet: Bot,
  comms: MessageSquare,
  context: Brain,
  control: ShieldCheck,
  credentials: KeyRound,
};

export function Overview() {
  const navigate = useNavigate();
  const [agents, setAgents] = useState<Agent[]>([]);
  const [infrastructure, setInfrastructure] = useState<InfrastructureService[]>([]);
  const [auditEvents, setAuditEvents] = useState<AuditEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [infraAction, setInfraAction] = useState<InfraAction | null>(null);
  const [infraBuildId, setInfraBuildId] = useState('');
  const [providers, setProviders] = useState<Provider[]>([]);
  const [routingConfigured, setRoutingConfigured] = useState<boolean | null>(null);
  const [capabilityCount, setCapabilityCount] = useState<number | null>(null);

  const loadInfrastructure = useCallback(async () => {
    try {
      const infraData = await api.infra.status();
      const mapped = (infraData.components ?? []).map((s: any) => ({
        id: s.name,
        name: s.name,
        state: s.state || s.status || 'stopped',
        health: s.health === 'healthy' || s.health === 'unhealthy' ? s.health : 'idle',
        containerId: s.container_id || '',
        uptime: s.uptime || '',
      }));
      setInfrastructure(mapped);
      setInfraBuildId(infraData.build_id || '');
      return mapped;
    } catch (err) {
      console.error('Infra load error:', err);
      return [];
    }
  }, []);

  const load = useCallback(async () => {
    const agentPromise = api.agents.list();
    const infraPromise = loadInfrastructure();
    const providerPromise = api.providers.list().catch(() => [] as Provider[]);
    const routingPromise = api.routing.config().catch(() => ({ configured: false }));
    const capabilitiesPromise = api.capabilities.list().catch(() => []);

    agentPromise.then((agentData) => {
      const safeAgentData = agentData ?? [];
      const mapped: Agent[] = safeAgentData.map((a: any) => ({
        id: a.name,
        name: a.name,
        status: a.status,
        mode: a.mode || 'assisted',
        type: a.type || '',
        preset: a.preset || '',
        team: a.team || '',
        enforcerState: a.enforcer || '',
        mission: a.mission,
      }));
      setAgents(mapped);
      setLoading(false);

      Promise.allSettled(
        safeAgentData.map((agent: any) =>
          api.agents.logs(agent.name).then((logs) => (logs ?? []).map((e: any, i: number) => ({
            id: `${agent.name}-${e.timestamp || ''}-${i}`,
            timestamp: e.timestamp || e.ts || '',
            type: e.event || e.type || '',
            message: e.detail || e.event || e.type || '',
            agent: agent.name,
          }))),
        ),
      ).then((logResults) => {
        const allEvents: AuditEvent[] = logResults
          .flatMap((r) => (r.status === 'fulfilled' ? r.value : []))
          .sort((a, b) => b.timestamp.localeCompare(a.timestamp))
          .slice(0, 20);
        setAuditEvents(allEvents);
      });
    }).catch((err) => {
      console.error('Agent load error:', err);
      setLoading(false);
    });

    infraPromise.catch(() => {});
    providerPromise.then((providerData) => setProviders(providerData ?? []));
    routingPromise.then((config) => setRoutingConfigured(Boolean(config.configured)));
    capabilitiesPromise.then((capabilities) => setCapabilityCount(capabilities.length));
  }, [loadInfrastructure]);

  useEffect(() => {
    load();
    const unsubAgent = socket.on('agent_status', load);
    const unsubInfra = socket.on('infra_status', load);
    const unsubDeploy = socket.on('deploy_progress', load);
    const unsubPhase = socket.on('phase', load);
    return () => {
      unsubAgent();
      unsubInfra();
      unsubDeploy();
      unsubPhase();
    };
  }, [load]);

  const waitForInfraState = useCallback(async (target: InfraAction) => {
    for (let attempt = 0; attempt < 12; attempt += 1) {
      const next = await loadInfrastructure();
      if (target === 'stop') {
        if (next.every((service) => isStoppedState(service.state))) return true;
      } else if (next.length > 0 && next.every((service) => service.state === 'running')) {
        return true;
      }
      await sleep(1000);
    }
    return false;
  }, [loadInfrastructure]);

  const hasRunningServices = infrastructure.some((service) => isRunningState(service.state));
  const primaryAction: InfraAction = hasRunningServices ? 'restart' : 'start';
  const hasAgents = agents.length > 0;
  const readyProviders = providers.filter((provider) => provider.credential_configured);
  const runningServices = infrastructure.filter((service) => isRunningState(service.state)).length;
  const healthyServices = infrastructure.filter((service) => service.health === 'healthy').length;
  const healthyServicePercent = infrastructure.length > 0 ? Math.round((healthyServices / infrastructure.length) * 100) : 0;
  const providerCoveragePercent = providers.length > 0 ? Math.round((readyProviders.length / providers.length) * 100) : 0;
  const visibleSurfaces = useMemo(() => contractSurfaces.filter(surfaceIsVisible), []);
  const gatedSurfaces = useMemo(() => contractSurfaces.filter((surface) => !surfaceIsVisible(surface)), []);

  const readiness = [
    {
      id: 'runtime',
      label: 'Runtime',
      value: infrastructure.length > 0 ? `${runningServices}/${infrastructure.length}` : '0',
      detail: `${healthyServicePercent}% healthy mediation services`,
      progress: healthyServicePercent,
      state: hasRunningServices ? 'ready' : 'review',
    },
    {
      id: 'fleet',
      label: 'Fleet',
      value: String(agents.length),
      detail: hasAgents ? 'Agents available for operator work' : 'No agents created yet',
      progress: hasAgents ? 100 : 0,
      state: hasAgents ? 'ready' : 'review',
    },
    {
      id: 'credentials',
      label: 'Providers',
      value: `${readyProviders.length}/${providers.length || 0}`,
      detail: `${providerCoveragePercent}% credential coverage`,
      progress: providerCoveragePercent,
      state: readyProviders.length > 0 ? 'ready' : 'review',
    },
    {
      id: 'control',
      label: 'Controls',
      value: capabilityCount === null ? '...' : String(capabilityCount),
      detail: routingConfigured ? 'Routing and capability contracts visible' : 'Routing configuration needs review',
      progress: routingConfigured ? 100 : 35,
      state: routingConfigured ? 'ready' : 'review',
    },
  ];

  const handleInfraAction = async (action: InfraAction) => {
    setInfraAction(action);
    try {
      if (action === 'start') {
        await api.infra.up();
      } else if (action === 'restart') {
        await api.infra.reload();
      } else {
        await api.infra.down();
      }

      const settled = await waitForInfraState(action);
      if (settled) {
        toast.success(`Infrastructure ${action === 'restart' ? 'running' : action === 'start' ? 'started' : 'stopped'}`);
      } else {
        toast.success(`Infrastructure ${action} initiated`);
      }
    } catch (err: any) {
      console.error(`infra ${action} error:`, err);
      toast.error(err?.message || `Failed to ${action} infrastructure`);
    } finally {
      setInfraAction(null);
    }
  };

  return (
    <div className="min-h-full bg-[linear-gradient(180deg,hsl(var(--background)),hsl(var(--muted)/0.28))]">
      <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-4 md:px-6">
        <div className="min-w-0">
          <div className="text-sm font-medium text-foreground">Contract-first control plane</div>
          <div className="mt-1 text-xs text-muted-foreground">
            OpenAPI surfaces plus feature-registry gates, with ASK control boundaries visible.
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleInfraAction(primaryAction)}
            disabled={infraAction !== null}
          >
            {hasRunningServices ? <RotateCw data-icon="inline-start" /> : <Play data-icon="inline-start" />}
            {infraAction === 'start' ? 'Starting...' : infraAction === 'restart' ? 'Restarting...' : hasRunningServices ? 'Restart Infra' : 'Start Infra'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleInfraAction('stop')}
            disabled={infraAction !== null || !hasRunningServices}
          >
            <Square data-icon="inline-start" />
            {infraAction === 'stop' ? 'Stopping...' : 'Stop Infra'}
          </Button>
        </div>
      </div>

      <div className="space-y-4 p-4 md:p-6">
        <div className="grid gap-4 xl:grid-cols-4">
          {readiness.map((item) => {
            const Icon = readinessIcons[item.id as keyof typeof readinessIcons] ?? CheckCircle2;
            return (
              <Card key={item.id} className="gap-4 overflow-hidden py-5">
                <CardHeader className="px-5">
                  <CardAction>
                    <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-primary/10 text-primary">
                      <Icon className="h-4 w-4" />
                    </div>
                  </CardAction>
                  <CardDescription>{item.label}</CardDescription>
                  <CardTitle className="text-2xl">{item.value}</CardTitle>
                </CardHeader>
                <CardContent className="px-5">
                  <div className="mb-2 flex items-center justify-between gap-3 text-xs text-muted-foreground">
                    <span>{item.detail}</span>
                    <Badge variant={item.state === 'ready' ? 'secondary' : 'outline'} className="rounded-full">
                      {item.state}
                    </Badge>
                  </div>
                  <Progress value={item.progress} />
                </CardContent>
              </Card>
            );
          })}
        </div>

        <div className="grid gap-4 xl:grid-cols-[minmax(0,1.55fr)_minmax(360px,0.95fr)]">
          <Card className="overflow-hidden">
            <CardHeader className="border-b border-border bg-muted/20">
              <CardAction>
                <Badge variant="outline" className="rounded-full">
                  {visibleSurfaces.length} visible
                </Badge>
              </CardAction>
              <CardTitle>OpenAPI Surface Map</CardTitle>
              <CardDescription>
                Core routes are visible by default. Experimental and internal features stay explicit so trust and capability boundaries remain recoverable.
              </CardDescription>
            </CardHeader>
            <CardContent className="p-0">
              <div className="divide-y divide-border">
                {contractModules.map((module) => {
                  const moduleSurfaces = module.surfaces;
                  return (
                    <div key={module.id} className="grid gap-0 md:grid-cols-[220px_minmax(0,1fr)]">
                      <div className="border-b border-border bg-muted/20 p-4 md:border-b-0 md:border-r">
                        <div className="text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">
                          {module.eyebrow}
                        </div>
                        <div className="mt-1 text-lg font-medium">{module.label}</div>
                        <p className="mt-2 text-xs leading-5 text-muted-foreground">{module.summary}</p>
                      </div>
                      <div className="grid gap-3 p-4 lg:grid-cols-2">
                        {moduleSurfaces.map((surface) => {
                          const visible = surfaceIsVisible(surface);
                          return (
                            <Link
                              key={surface.id}
                              to={visible ? surface.route : '/overview'}
                              className="rounded-2xl border border-border bg-background p-4 transition-colors hover:bg-muted/35"
                            >
                              <div className="flex items-start justify-between gap-3">
                                <div>
                                  <div className="font-medium">{surface.label}</div>
                                  <div className="mt-1 font-mono text-[11px] text-muted-foreground">{surface.tag}</div>
                                </div>
                                <Badge variant={visible ? surfaceBadgeVariant(surface) : 'outline'} className="rounded-full">
                                  {visible ? surface.tier : 'gated'}
                                </Badge>
                              </div>
                              <p className="mt-3 line-clamp-2 text-sm leading-6 text-muted-foreground">
                                {surface.summary}
                              </p>
                              <div className="mt-3 flex items-center gap-2 text-xs text-muted-foreground">
                                <span>{surface.endpoints.length} contract endpoints</span>
                                {visible ? <ArrowRight className="h-3.5 w-3.5" /> : null}
                              </div>
                            </Link>
                          );
                        })}
                      </div>
                    </div>
                  );
                })}
              </div>
            </CardContent>
          </Card>

          <div className="space-y-4">
            <Card className="overflow-hidden">
              <CardHeader>
                <CardTitle>Suggested next steps</CardTitle>
                <CardDescription>
                  {!hasRunningServices
                    ? 'Start infrastructure first so the gateway, comms, and mediation plane are available.'
                    : !hasAgents
                      ? 'Infrastructure is up. Create your first agent, then open its DM through the backend contract.'
                      : 'Your platform is running. Open a DM, inspect recent activity, or review system state based on the next operator task.'}
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-wrap gap-2">
                {!hasRunningServices ? (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => handleInfraAction('start')}
                    disabled={infraAction !== null}
                  >
                    <Play data-icon="inline-start" />
                    {infraAction === 'start' ? 'Starting infra...' : 'Start infrastructure'}
                  </Button>
                ) : !hasAgents ? (
                  <>
                    <Button asChild variant="outline" size="sm">
                      <Link to="/agents">Create first agent</Link>
                    </Button>
                    <Button asChild variant="outline" size="sm">
                      <Link to="/setup">Review providers</Link>
                    </Button>
                  </>
                ) : (
                  <>
                    <Button asChild variant="outline" size="sm">
                      <Link to="/channels">Open channels</Link>
                    </Button>
                    <Button asChild variant="outline" size="sm">
                      <Link to="/knowledge">Open knowledge</Link>
                    </Button>
                  </>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardAction>
                  <Badge variant="secondary" className="rounded-full">
                    First run
                  </Badge>
                </CardAction>
                <CardTitle>First Agent Path</CardTitle>
                <CardDescription>
                  Give operators a predictable path: create an agent, open its DM, and verify a simple task or status request end to end.
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-wrap gap-2">
                <Button asChild size="sm">
                  <Link to="/agents">Open agent fleet</Link>
                </Button>
                <Button asChild variant="outline" size="sm">
                  <Link to="/channels">Open channels</Link>
                </Button>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardAction>
                  <Badge variant={routingConfigured ? 'secondary' : 'outline'} className="rounded-full">
                    {routingConfigured ? 'Routing ready' : 'Needs review'}
                  </Badge>
                </CardAction>
                <CardTitle>Provider Coverage</CardTitle>
                <CardDescription>
                  {readyProviders.length > 0
                    ? `${readyProviders.length} provider${readyProviders.length === 1 ? '' : 's'} configured for agent tasks and fallback routing.`
                    : 'No configured providers detected in the web UI yet.'}
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-wrap gap-2">
                {readyProviders.length > 0 ? (
                  readyProviders.map((provider) => (
                    <Badge key={provider.name} variant="outline">
                      {provider.display_name || provider.name}
                    </Badge>
                  ))
                ) : (
                  <Badge variant="outline">No providers configured</Badge>
                )}
                <Button asChild variant="outline" size="sm">
                  <Link to="/setup">Open provider setup</Link>
                </Button>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardAction>
                  <Badge variant={experimentalSurfacesEnabled ? 'secondary' : 'outline'} className="rounded-full">
                    {experimentalSurfacesEnabled ? 'Visible' : 'Gated'}
                  </Badge>
                </CardAction>
                <CardTitle>Feature Registry</CardTitle>
                <CardDescription>
                  Experimental surfaces are routed only when enabled. Gated surfaces stay visible here as product inventory, not active affordances.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                {gatedSurfaces.length === 0 ? (
                  <div className="rounded-2xl border border-border bg-muted/25 p-4 text-sm text-muted-foreground">
                    All registered UI surfaces are currently visible.
                  </div>
                ) : (
                  gatedSurfaces.slice(0, 6).map((surface) => (
                    <div key={surface.id} className="flex items-start justify-between gap-3 rounded-2xl border border-border bg-muted/20 p-3">
                      <div>
                        <div className="text-sm font-medium">{surface.label}</div>
                        <div className="mt-1 text-xs text-muted-foreground">{surface.summary}</div>
                      </div>
                      <Badge variant="outline" className="rounded-full">
                        {surface.tier}
                      </Badge>
                    </div>
                  ))
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>ASK Control Boundary</CardTitle>
                <CardDescription>
                  The UI should expose controls; enforcement remains in the gateway, policy, egress, and enforcer planes.
                </CardDescription>
              </CardHeader>
              <CardContent className="grid gap-3 text-sm">
                {[
                  'Mediation: agent DMs use POST /agents/{name}/dm instead of reconstructing channel state.',
                  'Least privilege: capability grants remain explicit under Admin.',
                  'Auditability: agent logs and admin summaries stay surfaced as first-class contracts.',
                  'Isolation: egress and enforcer status are treated as control-plane state, not UI-only hints.',
                ].map((item) => (
                  <div key={item} className="flex gap-3 rounded-2xl border border-border bg-background p-3 text-muted-foreground">
                    <ShieldCheck className="mt-0.5 h-4 w-4 flex-shrink-0 text-primary" />
                    <span>{item}</span>
                  </div>
                ))}
              </CardContent>
            </Card>
          </div>
        </div>

        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_420px]">
          <Card>
            <CardHeader>
              <CardAction>
                {infraBuildId ? <Badge variant="outline">Build: {infraBuildId}</Badge> : null}
              </CardAction>
              <CardTitle>Infrastructure Services</CardTitle>
              <CardDescription>
                Current mediation plane status across gateway, comms, and supporting services.
              </CardDescription>
            </CardHeader>
            <CardContent>
              {infrastructure.length === 0 ? (
                <div className="rounded-2xl border border-dashed border-border bg-muted/20 p-8 text-center text-sm text-muted-foreground">
                  No infrastructure services detected
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Service</TableHead>
                      <TableHead>State</TableHead>
                      <TableHead>Health</TableHead>
                      <TableHead>Uptime</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {infrastructure.map((service) => (
                      <TableRow key={service.id}>
                        <TableCell>
                          <div className="flex items-center gap-2">
                            <StatusIndicator status={visualStatus(service, infraAction)} size="sm" />
                            <code>{service.name}</code>
                          </div>
                        </TableCell>
                        <TableCell>
                          <Badge variant="outline" className="capitalize">
                            {formatStateLabel(service, infraAction)}
                          </Badge>
                        </TableCell>
                        <TableCell className="capitalize">{service.health}</TableCell>
                        <TableCell>{service.uptime || '-'}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Recent Activity</CardTitle>
              <CardDescription>Latest agent-side logs surfaced through gateway audit contracts.</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="max-h-[480px] space-y-2 overflow-auto pr-1">
                {auditEvents.length === 0 ? (
                  <div className="rounded-2xl border border-dashed border-border bg-muted/20 p-4 text-center text-sm text-muted-foreground">
                    No recent activity
                  </div>
                ) : auditEvents.map((event) => (
                  <div key={event.id} className="rounded-2xl border border-border bg-background p-3">
                    <div className="mb-1 flex items-center justify-between gap-3">
                      <Badge variant="outline">{event.type}</Badge>
                      <span className="text-xs text-muted-foreground">{formatTime(event.timestamp)}</span>
                    </div>
                    <div className="text-sm text-foreground">{event.message}</div>
                    {event.agent ? (
                      <div className="mt-2 text-xs text-muted-foreground">Agent: {event.agent}</div>
                    ) : null}
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </div>

        <Card>
          <CardHeader>
            <CardTitle>Agents</CardTitle>
            <CardDescription>
              Fleet status, mode, and enforcement state for currently discovered agents.
            </CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="py-8 text-center text-sm text-muted-foreground">Loading...</div>
            ) : agents.length === 0 ? (
              <div className="rounded-2xl border border-dashed border-border bg-muted/20 p-8 text-center text-sm text-muted-foreground">
                No agents running. Create your first agent from the fleet view to start the operator flow.
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Agent</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Mode</TableHead>
                    <TableHead>Preset</TableHead>
                    <TableHead>Enforcer</TableHead>
                    <TableHead>Team</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {agents.map((agent) => (
                    <TableRow
                      key={agent.id}
                      className="cursor-pointer"
                      onClick={() => navigate(`/agents/${agent.name}`)}
                    >
                      <TableCell className="font-mono">{agent.name}</TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <StatusIndicator status={agent.status} size="sm" />
                          <span className="capitalize">{agent.status}</span>
                        </div>
                      </TableCell>
                      <TableCell className="capitalize">{agent.mode}</TableCell>
                      <TableCell>{agent.preset || agent.type || 'agent'}</TableCell>
                      <TableCell>{agent.enforcerState || 'unknown'}</TableCell>
                      <TableCell>{agent.team || '-'}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
