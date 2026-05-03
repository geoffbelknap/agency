import { useEffect, useMemo, useState, useCallback, type ReactNode } from 'react';
import { Link, useNavigate } from 'react-router';
import { AlertTriangle, ArrowRight, Play, RotateCw, Send, Sparkles, Square } from 'lucide-react';
import { StatusIndicator } from '../components/StatusIndicator';
import { Agent, InfrastructureService, AuditEvent, Provider } from '../types';
import { Button } from '../components/ui/button';
import { toast } from 'sonner';
import { api } from '../lib/api';
import { socket } from '../lib/ws';
import { formatTime } from '../lib/time';
import { featureEnabled } from '../lib/features';

type InfraAction = 'start' | 'stop' | 'restart';

type DecisionItem = {
  id: string;
  tone: 'amber' | 'red' | 'teal';
  title: string;
  meta: string;
  context: string;
  primaryLabel: string;
  primaryHref?: string;
  secondaryLabel?: string;
  secondaryHref?: string;
};

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

function Panel({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <section className={`rounded-2xl border border-border bg-card ${className}`}>{children}</section>;
}

function SectionHeading({ eyebrow, title, meta }: { eyebrow: string; title: string; meta?: string }) {
  return (
    <div className="mb-4 flex items-center justify-between gap-3">
      <div>
        <div className="text-[10px] uppercase tracking-[0.18em] text-muted-foreground">{eyebrow}</div>
        <div className="mt-1 text-sm text-foreground">{title}</div>
      </div>
      {meta && <div className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">{meta}</div>}
    </div>
  );
}

function DecisionTone({ tone }: { tone: DecisionItem['tone'] }) {
  const styles = {
    amber: 'bg-amber-500/20 text-amber-700 dark:text-amber-400',
    red: 'bg-red-500/20 text-red-700 dark:text-red-400',
    teal: 'bg-primary/15 text-primary',
  } as const;

  return (
    <span className={`inline-flex h-7 w-7 items-center justify-center rounded-lg ${styles[tone]}`}>
      <AlertTriangle className="h-4 w-4" />
    </span>
  );
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
  const [dispatchText, setDispatchText] = useState('');
  const [dispatchAgent, setDispatchAgent] = useState('');
  const showTeams = featureEnabled('teams');

  const loadInfrastructure = useCallback(async () => {
    try {
      const infraData = await api.infra.status();
      const mapped = (infraData.components ?? []).map((s: any) => ({
        id: s.name,
        name: s.name,
        state: s.state || s.status || 'stopped',
        health: s.health === 'healthy' || s.health === 'unhealthy' ? s.health : 'idle',
        componentId: s.component_id || s.container_id || '',
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
          .flatMap((r) => r.status === 'fulfilled' ? r.value : [])
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
  const activeAgents = agents.filter((agent) => agent.status === 'running').length;
  const unhealthyAgents = agents.filter((agent) => agent.status === 'unhealthy').length;
  const pausedAgents = agents.filter((agent) => agent.status === 'paused' || agent.status === 'halted').length;
  const healthyServices = infrastructure.filter((service) => service.health === 'healthy').length;
  const runningAgents = agents.filter((agent) => agent.status === 'running');

  useEffect(() => {
    if (!dispatchAgent && runningAgents.length > 0) {
      setDispatchAgent(runningAgents[0].name);
    }
  }, [runningAgents, dispatchAgent]);

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

  const handleDispatch = () => {
    if (!dispatchAgent || !dispatchText.trim()) return;
    navigate(`/agents/${dispatchAgent}`);
  };

  const heroSummary = !hasRunningServices
    ? 'Infrastructure is offline. Bring up the gateway and supporting services before validating the operator flow.'
    : !hasAgents
      ? 'Platform services are available, but the fleet is empty. Seed an agent to test direct-message and mission flows.'
      : `Your agents wrapped up ${auditEvents.length} recent events. ${unhealthyAgents > 0 ? `${unhealthyAgents} agent${unhealthyAgents === 1 ? ' is' : 's are'} unhealthy.` : 'Everything else is humming.'}`;

  const decisions = useMemo<DecisionItem[]>(() => {
    const items: DecisionItem[] = [];

    const overBudgetAgent = runningAgents[0];
    if (overBudgetAgent) {
      items.push({
        id: `agent-${overBudgetAgent.name}`,
        tone: 'amber',
        title: `${overBudgetAgent.name} needs direction`,
        meta: `${overBudgetAgent.mode} · ${overBudgetAgent.enforcerState || 'enforcer status unknown'}`,
        context: overBudgetAgent.mission || 'No mission assigned yet.',
        primaryLabel: 'Open agent',
        primaryHref: `/agents/${overBudgetAgent.name}`,
        secondaryLabel: 'Open channels',
        secondaryHref: '/channels',
      });
    }

    if (unhealthyAgents > 0) {
      items.push({
        id: 'unhealthy',
        tone: 'red',
        title: `${unhealthyAgents} unhealthy agent${unhealthyAgents === 1 ? '' : 's'} need attention`,
        meta: 'health regression detected',
        context: 'Review the affected agent detail and recent event feed before restarting the runtime.',
        primaryLabel: 'Review fleet',
        primaryHref: '/agents',
        secondaryLabel: 'Open admin',
        secondaryHref: '/admin/doctor',
      });
    }

    if (!routingConfigured || readyProviders.length === 0) {
      items.push({
        id: 'providers',
        tone: 'amber',
        title: 'Provider setup is incomplete',
        meta: routingConfigured ? 'routing ready, providers missing' : 'routing needs review',
        context: 'Add at least one validated provider so the first agent can complete useful work.',
        primaryLabel: 'Open provider setup',
        primaryHref: '/setup',
        secondaryLabel: 'Review usage',
        secondaryHref: '/admin/usage',
      });
    }

    if (items.length === 0) {
      items.push({
        id: 'healthy',
        tone: 'teal',
        title: 'No immediate interventions required',
        meta: `${activeAgents} running agents · ${healthyServices} healthy services`,
        context: 'Open channels, dispatch a new task, or inspect knowledge context for the next operator decision.',
        primaryLabel: 'Open channels',
        primaryHref: '/channels',
        secondaryLabel: 'Open knowledge',
        secondaryHref: '/knowledge',
      });
    }

    return items.slice(0, 3);
  }, [activeAgents, healthyServices, readyProviders.length, routingConfigured, runningAgents, unhealthyAgents]);

  const recentTimeline = auditEvents.slice(0, 8);

  return (
    <div className="min-h-full bg-background">
      <div className="border-b border-border bg-background px-4 py-8 md:px-8 md:py-10">
        <div className="mx-auto max-w-7xl">
          <div className="flex flex-col gap-6 lg:flex-row lg:items-end lg:justify-between">
            <div className="max-w-4xl">
              <div className="mb-3 text-[10px] uppercase tracking-[0.22em] text-muted-foreground">Today</div>
              <h1 className="text-4xl leading-none md:text-6xl">
                Good evening, <span className="text-primary">Operator.</span>
              </h1>
              <p className="mt-4 max-w-3xl text-base leading-8 text-muted-foreground md:text-lg">
                {heroSummary}
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleInfraAction(primaryAction)}
                disabled={infraAction !== null}
              >
                {hasRunningServices ? <RotateCw className="mr-1 h-3 w-3" /> : <Play className="mr-1 h-3 w-3" />}
                {infraAction === 'start' ? 'Starting...' : infraAction === 'restart' ? 'Restarting...' : hasRunningServices ? 'Restart Infra' : 'Start Infra'}
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleInfraAction('stop')}
                disabled={infraAction !== null || !hasRunningServices}
              >
                <Square className="mr-1 h-3 w-3" />
                {infraAction === 'stop' ? 'Stopping...' : 'Stop Infra'}
              </Button>
            </div>
          </div>

          <div className="mt-8 rounded-2xl border border-border bg-card p-4 md:p-5">
            <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
              <div className="flex flex-wrap items-center gap-2">
                <div className="inline-flex items-center gap-2 rounded-full border border-border bg-background px-3 py-2 text-xs text-foreground">
                  <Sparkles className="h-3.5 w-3.5 text-primary" />
                  Dispatch
                </div>
                <select
                  value={dispatchAgent}
                  onChange={(e) => setDispatchAgent(e.target.value)}
                  aria-label="Dispatch target"
                  className="rounded-full border border-border bg-background px-3 py-2 text-sm text-foreground outline-none"
                >
                  {runningAgents.length > 0 ? runningAgents.map((agent) => (
                    <option key={agent.name} value={agent.name}>{`Agent: ${agent.name}`}</option>
                  )) : <option value="">No running agents</option>}
                </select>
                <input
                  value={dispatchText}
                  onChange={(e) => setDispatchText(e.target.value)}
                  placeholder={dispatchAgent ? `Dispatch ${dispatchAgent} to...` : 'Start infrastructure to dispatch work'}
                  className="min-w-0 flex-1 rounded-full border border-border bg-background px-4 py-2.5 text-sm text-foreground outline-none placeholder:text-muted-foreground"
                />
              </div>
              <Button size="sm" onClick={handleDispatch} disabled={!dispatchAgent || !dispatchText.trim()}>
                <Send className="mr-1 h-3.5 w-3.5" />
                Dispatch
              </Button>
            </div>
          </div>

          <div className="mt-8 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <Panel className="p-4">
              <div className="text-[10px] uppercase tracking-[0.18em] text-muted-foreground">Running agents</div>
              <div className="mt-3 flex items-end gap-3">
                <div className="text-3xl text-foreground">{activeAgents}</div>
                <div className="pb-1 text-xs text-muted-foreground">of {agents.length || 0} total</div>
              </div>
            </Panel>
            <Panel className="p-4">
              <div className="text-[10px] uppercase tracking-[0.18em] text-muted-foreground">Infrastructure</div>
              <div className="mt-3 flex items-end gap-3">
                <div className="text-3xl text-foreground">{healthyServices}</div>
                <div className="pb-1 text-xs text-muted-foreground">healthy services</div>
              </div>
            </Panel>
            <Panel className="p-4">
              <div className="text-[10px] uppercase tracking-[0.18em] text-muted-foreground">Provider coverage</div>
              <div className="mt-3 flex items-end gap-3">
                <div className="text-3xl text-foreground">{readyProviders.length}</div>
                <div className="pb-1 text-xs text-muted-foreground">configured providers</div>
              </div>
            </Panel>
            <Panel className="p-4">
              <div className="text-[10px] uppercase tracking-[0.18em] text-muted-foreground">Build</div>
              <div className="mt-3 text-lg text-foreground">{infraBuildId || 'unknown'}</div>
              <div className="mt-1 text-xs text-muted-foreground">{routingConfigured ? 'Router configured' : 'Needs review'}</div>
            </Panel>
          </div>
        </div>
      </div>

      <div className="mx-auto grid max-w-7xl gap-6 px-4 py-6 md:px-8 lg:grid-cols-[minmax(0,1.5fr)_minmax(320px,0.9fr)] lg:py-8">
        <div className="space-y-6">
          <Panel className="p-4 md:p-5">
            <SectionHeading eyebrow="Needs your decision" title="Decision inbox" meta={`${decisions.length} open`} />
            <div className="space-y-3">
              {decisions.map((decision) => (
                <div key={decision.id} className="rounded-xl border border-border bg-background p-4">
                  <div className="flex items-start gap-3">
                    <DecisionTone tone={decision.tone} />
                    <div className="min-w-0 flex-1">
                      <div className="text-sm text-foreground">{decision.title}</div>
                      <div className="mt-1 text-[11px] uppercase tracking-[0.14em] text-muted-foreground">{decision.meta}</div>
                      <div className="mt-2 text-sm leading-6 text-muted-foreground">{decision.context}</div>
                      <div className="mt-3 flex flex-wrap gap-2">
                        {decision.primaryHref ? (
                          <Button asChild size="sm"><Link to={decision.primaryHref}>{decision.primaryLabel}</Link></Button>
                        ) : (
                          <Button size="sm">{decision.primaryLabel}</Button>
                        )}
                        {decision.secondaryLabel && decision.secondaryHref && (
                          <Button asChild variant="outline" size="sm"><Link to={decision.secondaryHref}>{decision.secondaryLabel}</Link></Button>
                        )}
                      </div>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </Panel>

          <Panel className="p-4 md:p-5">
            <SectionHeading eyebrow={`Agents (${agents.length})`} title="Agents in flight" meta={pausedAgents > 0 ? `${pausedAgents} paused or halted` : undefined} />
            {loading ? (
              <div className="py-8 text-sm text-muted-foreground">Loading...</div>
            ) : agents.length === 0 ? (
              <div className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-sm text-muted-foreground">
                No agents running. Create your first agent from the fleet view to start the tester flow.
              </div>
            ) : (
              <div className="grid gap-3 xl:grid-cols-2">
                {agents.map((agent) => (
                  <button
                    key={agent.id}
                    type="button"
                    onClick={() => navigate(`/agents/${agent.name}`)}
                    className="rounded-xl border border-border bg-background p-4 text-left transition-colors hover:border-primary/40 hover:bg-accent/20"
                  >
                    <div className="flex items-start gap-3">
                      <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-secondary text-xs font-semibold uppercase text-foreground">
                        {agent.name.charAt(0)}
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <code className="text-sm text-foreground">{agent.name}</code>
                          <StatusIndicator status={agent.status} size="sm" />
                          <span className="ml-auto text-[11px] uppercase tracking-[0.14em] text-muted-foreground">{agent.mode}</span>
                        </div>
                        <div className="mt-1 text-xs text-muted-foreground">{agent.preset || agent.type} · {agent.role || 'agent'}</div>
                        <div className="mt-3 text-sm leading-6 text-foreground/85">
                          {agent.mission || 'Idle — no mission assigned yet.'}
                        </div>
                        <div className="mt-3 flex items-center justify-between gap-3 text-[11px] text-muted-foreground">
                          <span>{agent.enforcerState || 'unknown enforcer'}</span>
                          {showTeams && agent.team ? <span>{agent.team}</span> : <span>{agent.status}</span>}
                        </div>
                      </div>
                    </div>
                  </button>
                ))}
              </div>
            )}
          </Panel>

          <Panel className="p-4 md:p-5">
            <SectionHeading eyebrow="Since last visit" title="Recent Activity" meta={`${auditEvents.length} items`} />
            <div className="space-y-2">
              {recentTimeline.length === 0 ? (
                <div className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-sm text-muted-foreground">No recent activity</div>
              ) : recentTimeline.map((event) => (
                <div key={event.id} className="rounded-xl border border-border bg-background px-4 py-3">
                  <div className="flex items-start gap-3">
                    <div className="w-14 flex-shrink-0 pt-0.5 text-[10px] text-muted-foreground">{formatTime(event.timestamp)}</div>
                    <div className="min-w-0 flex-1">
                      <div className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">{event.type}</div>
                      <div className="mt-1 text-sm leading-6 text-foreground/85">{event.message}</div>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </Panel>
        </div>

        <div className="space-y-6">
          <Panel className="p-4 md:p-5">
            <SectionHeading eyebrow="Suggested next steps" title="Operator guidance" />
            <p className="text-sm leading-6 text-muted-foreground">
              {!hasRunningServices
                ? 'Start infrastructure first so the web UI, comms, and gateway services are available.'
                : !hasAgents
                  ? 'Create your first agent, then open its DM to verify the core operator flow.'
                  : 'Open a DM, inspect recent activity, or review graph context depending on the next operator task.'}
            </p>
            <div className="mt-4 flex flex-wrap gap-2">
              {!hasRunningServices ? (
                <Button variant="outline" size="sm" className="h-8 text-xs" onClick={() => handleInfraAction('start')} disabled={infraAction !== null}>
                  <Play className="mr-1 h-3 w-3" />
                  {infraAction === 'start' ? 'Starting infra...' : 'Start infrastructure'}
                </Button>
              ) : !hasAgents ? (
                <>
                  <Button asChild variant="outline" size="sm" className="h-8 text-xs"><Link to="/agents">Create first agent</Link></Button>
                  <Button asChild variant="outline" size="sm" className="h-8 text-xs"><Link to="/setup">Review providers</Link></Button>
                </>
              ) : (
                <>
                  <Button asChild variant="outline" size="sm" className="h-8 text-xs"><Link to="/channels">Open channels</Link></Button>
                  <Button asChild variant="outline" size="sm" className="h-8 text-xs"><Link to="/knowledge">Open knowledge</Link></Button>
                </>
              )}
            </div>
          </Panel>

          <Panel className="p-4 md:p-5">
            <SectionHeading eyebrow="Provider Coverage" title="Setup posture" meta={routingConfigured ? 'Routing ready' : 'Needs review'} />
            <p className="text-sm leading-6 text-muted-foreground">
              {readyProviders.length > 0
                ? `${readyProviders.length} provider${readyProviders.length === 1 ? '' : 's'} configured for agent research and fallback routing.`
                : 'No configured providers detected in the web UI yet.'}
            </p>
            <div className="mt-4 flex flex-wrap gap-2">
              {readyProviderNames.length > 0 ? readyProviderNames.map((providerName) => (
                <span key={providerName} className="rounded-full border border-border bg-background px-2.5 py-1 text-[11px] text-foreground/80">
                  {providerName}
                </span>
              )) : (
                <span className="rounded-full border border-border bg-background px-2.5 py-1 text-[11px] text-muted-foreground">No providers configured</span>
              )}
            </div>
            <div className="mt-4 flex flex-wrap gap-2">
              <Button asChild variant="outline" size="sm" className="h-8 text-xs"><Link to="/setup">Open provider setup</Link></Button>
              <Button asChild variant="outline" size="sm" className="h-8 text-xs"><Link to="/admin/usage">Review usage</Link></Button>
            </div>
          </Panel>

          <Panel className="p-4 md:p-5">
            <SectionHeading eyebrow="Infrastructure" title="Live runtime surface" meta={infraBuildId ? `Build: ${infraBuildId}` : undefined} />
            <div className="space-y-2">
              {infrastructure.length === 0 ? (
                <div className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-sm text-muted-foreground">No infrastructure services detected</div>
              ) : infrastructure.map((service) => (
                <div key={service.id} className="rounded-xl border border-border bg-background px-4 py-3">
                  <div className="flex items-center gap-2">
                    <StatusIndicator status={visualStatus(service, infraAction)} size="sm" />
                    <code className="text-xs text-foreground">{service.name}</code>
                    <span className="ml-auto text-[10px] uppercase tracking-[0.14em] text-muted-foreground">{formatStateLabel(service, infraAction)}</span>
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">{service.uptime || 'No uptime reported'}</div>
                </div>
              ))}
            </div>
          </Panel>

          <Panel className="p-4 md:p-5">
            <SectionHeading eyebrow="Ambient strip" title="Pulse" />
            <div className="grid gap-3 sm:grid-cols-3">
              <div className="rounded-xl border border-border bg-background px-4 py-3">
                <div className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Events</div>
                <div className="mt-2 text-lg text-foreground">{auditEvents.length}</div>
              </div>
              <div className="rounded-xl border border-border bg-background px-4 py-3">
                <div className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Providers</div>
                <div className="mt-2 text-lg text-foreground">{readyProviders.length}</div>
              </div>
              <div className="rounded-xl border border-border bg-background px-4 py-3">
                <div className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Healthy services</div>
                <div className="mt-2 text-lg text-foreground">{healthyServices}</div>
              </div>
            </div>
          </Panel>
        </div>
      </div>
    </div>
  );
}
