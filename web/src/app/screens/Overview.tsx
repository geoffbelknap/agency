import { useEffect, useState, useCallback } from 'react';
import { Link, useNavigate } from 'react-router';
import { StatusIndicator } from '../components/StatusIndicator';
import { Agent, InfrastructureService, AuditEvent, Provider } from '../types';
import { Button } from '../components/ui/button';
import { Play, RotateCw, Square } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../lib/api';
import { socket } from '../lib/ws';
import { formatTime } from '../lib/time';

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
    // Fire all requests concurrently — render each section as it arrives
    const agentPromise = api.agents.list();
    const infraPromise = loadInfrastructure();
    const providerPromise = api.providers.list().catch(() => [] as Provider[]);
    const routingPromise = api.routing.config().catch(() => ({ configured: false }));

    // Agents come back fast (~35ms) — render immediately
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

      // Fetch audit events in parallel, non-blocking
      Promise.allSettled(
        safeAgentData.map((agent: any) =>
          api.agents.logs(agent.name).then((logs) => (logs ?? []).map((e: any, i: number) => ({
            id: `${agent.name}-${e.timestamp || ''}-${i}`,
            timestamp: e.timestamp || e.ts || '',
            type: e.event || e.type || '',
            message: e.detail || e.event || e.type || '',
            agent: agent.name,
          })))
        )
      ).then((logResults) => {
        const allEvents: AuditEvent[] = logResults
          .flatMap((r) => r.status === 'fulfilled' ? r.value : [])
          .sort((a, b) => b.timestamp.localeCompare(a.timestamp))
          .slice(0, 20);
        setAuditEvents(allEvents);
      });
    }).catch((err) => {
      console.error('Agent load error:', err);
      setLoading(false);
    });

    // Infra comes back slower — renders when ready
    infraPromise.catch(() => {});
    providerPromise.then((providerData) => setProviders(providerData ?? []));
    routingPromise.then((config) => setRoutingConfigured(Boolean(config.configured)));
  }, [loadInfrastructure]);

  useEffect(() => {
    load();
    const unsubAgent = socket.on('agent_status', load);
    const unsubInfra = socket.on('infra_status', load);
    const unsubDeploy = socket.on('deploy_progress', load);
    const unsubPhase = socket.on('phase', load);
    return () => { unsubAgent(); unsubInfra(); unsubDeploy(); unsubPhase(); };
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
  const readyProviderNames = readyProviders.map((provider) => provider.display_name || provider.name);

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
    <div className="h-full flex flex-col">
      {/* Header */}
      <div className="border-b border-border px-4 md:px-8 py-4 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl text-foreground">Overview</h1>
          <p className="text-sm text-muted-foreground mt-0.5">Platform health at a glance</p>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleInfraAction(primaryAction)}
            disabled={infraAction !== null}
          >
            {hasRunningServices ? <RotateCw className="w-3 h-3 mr-1" /> : <Play className="w-3 h-3 mr-1" />}
            {infraAction === 'start' ? 'Starting...' : infraAction === 'restart' ? 'Restarting...' : hasRunningServices ? 'Restart Infra' : 'Start Infra'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleInfraAction('stop')}
            disabled={infraAction !== null || !hasRunningServices}
          >
            <Square className="w-3 h-3 mr-1" />
            {infraAction === 'stop' ? 'Stopping...' : 'Stop Infra'}
          </Button>
        </div>
      </div>

      {/* Infrastructure Status Strip */}
      <div className="border-b border-border px-4 md:px-8 py-3 bg-background relative">
        {infraBuildId && (
          <div className="text-[10px] text-muted-foreground font-mono mb-2">
            Build: {infraBuildId}
          </div>
        )}
        <div className="flex gap-2 md:gap-3 justify-between overflow-x-auto scrollbar-none pb-1">
          {infrastructure.length === 0 ? (
            <div className="flex-1 min-w-[120px] text-center py-2">
              <span className="text-xs text-muted-foreground">No infrastructure services detected</span>
            </div>
          ) : infrastructure.map((service) => (
            <div key={service.id} className="flex-1 min-w-[120px] bg-card border border-border rounded px-3 py-2.5">
              <div className="flex items-center gap-2 mb-1">
                <StatusIndicator status={visualStatus(service, infraAction)} size="sm" />
                <code className="text-xs text-foreground/80">{service.name}</code>
              </div>
              <div className="text-[10px] text-muted-foreground capitalize">{formatStateLabel(service, infraAction)}</div>
            </div>
          ))}
        </div>
      </div>

      {/* Main Content */}
      <div className="flex-1 p-4 md:p-8 grid grid-cols-1 lg:grid-cols-3 gap-6 overflow-auto">
        {/* Agent Summary */}
        <div className="lg:col-span-2">
          <div className="mb-4 rounded-lg border border-border bg-card p-4">
            <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
              <div className="space-y-1">
                <div className="text-sm font-medium text-foreground">Suggested next steps</div>
                <p className="text-xs text-muted-foreground">
                  {!hasRunningServices
                    ? 'Start infrastructure first so the web UI, comms, and gateway services are available.'
                    : !hasAgents
                      ? 'Infrastructure is up. Create a research agent, then open its DM to verify the full research loop.'
                      : 'Your platform is running. Open a DM, inspect recent activity, or review graph context depending on the next operator task.'}
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                {!hasRunningServices ? (
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-8 text-xs"
                    onClick={() => handleInfraAction('start')}
                    disabled={infraAction !== null}
                  >
                    <Play className="w-3 h-3 mr-1" />
                    {infraAction === 'start' ? 'Starting infra...' : 'Start infrastructure'}
                  </Button>
                ) : !hasAgents ? (
                  <>
                    <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                      <Link to="/agents">Create research agent</Link>
                    </Button>
                    <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                      <Link to="/setup">Review providers</Link>
                    </Button>
                  </>
                ) : (
                  <>
                    <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                      <Link to="/channels">Open channels</Link>
                    </Button>
                    <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                      <Link to="/knowledge">Open knowledge</Link>
                    </Button>
                  </>
                )}
              </div>
            </div>
          </div>

          <div className="mb-4 grid grid-cols-1 gap-3 xl:grid-cols-2">
            <div className="rounded-lg border border-border bg-card p-4">
              <div className="mb-2 text-sm font-medium text-foreground">Researcher Path</div>
              <p className="text-xs leading-relaxed text-muted-foreground">
                Give testers a predictable path: create a <span className="text-foreground">researcher</span> agent,
                open its DM, and ask for a topic summary or source-backed brief.
              </p>
              <div className="mt-3 flex flex-wrap gap-2">
                <Button asChild size="sm" className="h-8 text-xs">
                  <Link to="/agents">Open agent fleet</Link>
                </Button>
                <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                  <Link to="/channels">Open channels</Link>
                </Button>
              </div>
            </div>

            <div className="rounded-lg border border-border bg-card p-4">
              <div className="mb-2 flex items-center justify-between gap-3">
                <div className="text-sm font-medium text-foreground">Provider Coverage</div>
                <div className="text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
                  {routingConfigured ? 'Routing ready' : 'Needs review'}
                </div>
              </div>
              <p className="text-xs leading-relaxed text-muted-foreground">
                {readyProviders.length > 0
                  ? `${readyProviders.length} provider${readyProviders.length === 1 ? '' : 's'} configured for agent research and fallback routing.`
                  : 'No configured providers detected in the web UI yet.'}
              </p>
              <div className="mt-3 flex flex-wrap gap-2">
                {readyProviderNames.length > 0 ? (
                  readyProviderNames.map((providerName) => (
                    <span
                      key={providerName}
                      className="rounded-full border border-border bg-secondary px-2.5 py-1 text-[11px] text-foreground/80"
                    >
                      {providerName}
                    </span>
                  ))
                ) : (
                  <span className="rounded-full border border-border bg-secondary px-2.5 py-1 text-[11px] text-muted-foreground">
                    No providers configured
                  </span>
                )}
              </div>
              <div className="mt-3 flex flex-wrap gap-2">
                <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                  <Link to="/setup">Open provider setup</Link>
                </Button>
                <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                  <Link to="/admin/usage">Review usage</Link>
                </Button>
              </div>
            </div>
          </div>

          <h2 className="text-xs font-bold text-foreground/80 uppercase tracking-widest mb-3">
            Agents ({agents.length})
          </h2>
          {loading ? (
            <div className="text-sm text-muted-foreground text-center py-8">Loading...</div>
          ) : agents.length === 0 ? (
            <div className="bg-card border border-border rounded p-8 text-center text-sm text-muted-foreground">
              No agents running. Create a research agent from the fleet view to start the tester flow.
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              {agents.map((agent) => (
                <div
                  key={agent.id}
                  className="bg-card border border-border rounded p-4 hover:border-border transition-colors cursor-pointer group"
                  onClick={() => navigate(`/agents/${agent.name}`)}
                >
                  <div className="flex items-start justify-between mb-3">
                    <div className="flex items-center gap-2.5">
                      <div className="w-8 h-8 rounded bg-primary flex items-center justify-center text-xs font-semibold text-white uppercase">
                        {agent.name.charAt(0)}
                      </div>
                      <div>
                        <code className="text-sm text-foreground group-hover:text-white transition-colors">{agent.name}</code>
                        <div className="text-[10px] text-muted-foreground">{agent.preset || agent.type} · {agent.role || 'agent'}</div>
                      </div>
                    </div>
                    <div className="flex items-center gap-1.5">
                      <StatusIndicator status={agent.status} size="sm" />
                      <span className="text-xs text-muted-foreground capitalize">{agent.status}</span>
                    </div>
                  </div>
                  <div className="flex items-center gap-4 text-[10px] text-muted-foreground">
                    <span>Mode: <span className="text-foreground/80 capitalize">{agent.mode}</span></span>
                    <span>Enforcer: <span className={
                      agent.enforcerState === 'running' ? 'text-green-400' :
                      agent.enforcerState === 'halted' ? 'text-red-400' :
                      'text-foreground/80'
                    }>{agent.enforcerState}</span></span>
                    {agent.team && <span>Team: <span className="text-foreground/80">{agent.team}</span></span>}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Recent Activity Feed */}
        <div>
          <h2 className="text-xs font-bold text-foreground/80 uppercase tracking-widest mb-3">
            Recent Activity
          </h2>
          <div className="bg-card border border-border rounded overflow-hidden">
            <div className="divide-y divide-border max-h-[600px] overflow-y-auto">
              {auditEvents.length === 0 ? (
                <div className="p-4 text-center text-xs text-muted-foreground">No recent activity</div>
              ) : auditEvents.map((event) => (
                <div key={event.id} className="p-3 hover:bg-secondary/50 transition-colors">
                  <div className="flex items-start gap-3">
                    <div className="text-[10px] text-muted-foreground w-14 flex-shrink-0 pt-0.5">
                      {formatTime(event.timestamp)}
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="text-[10px] text-muted-foreground uppercase tracking-wider mb-1">
                        {event.type}
                      </div>
                      <div className="text-xs text-foreground/80 leading-relaxed">{event.message}</div>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
