import { useState, useEffect, useCallback, useMemo, useDeferredValue } from 'react';
import { useParams, useNavigate } from 'react-router';
import { Plus, RefreshCw, Search } from 'lucide-react';
import { toast } from 'sonner';
import { Agent } from '../types';
import { Button } from '../components/ui/button';
import { Badge } from '../components/ui/badge';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '../components/ui/card';
import { Input } from '../components/ui/input';
import { Tabs, TabsList, TabsTrigger } from '../components/ui/tabs';
import { api, type RawAgent, type RawCapability } from '../lib/api';
import { socket } from '../lib/ws';
import { useIsMobile } from '../components/ui/use-mobile';
import { CreateAgentDialog } from '../components/CreateAgentDialog';
import { AgentList } from './agents/AgentList';
import { AgentDetail } from './agents/AgentDetail';

function mapAgent(a: RawAgent): Agent {
  return {
    id: a.name,
    name: a.name,
    status: (a.status || 'stopped') as Agent['status'],
    mode: (a.mode || 'assisted') as Agent['mode'],
    type: a.type || 'agent',
    preset: a.preset || '',
    team: a.team || '',
    enforcerState: a.enforcer || '',
    model: a.model,
    role: a.role,
    uptime: a.uptime,
    lastActive: a.last_active,
    trustLevel: a.trust_level,
    restrictions: a.restrictions,
    grantedCapabilities: a.granted_capabilities,
    currentTask: a.current_task,
    mission: a.mission,
    missionStatus: a.mission_status,
    buildId: a.build_id,
  };
}

type FleetView = 'all' | 'running' | 'attention';

export function Agents() {
  const { name: urlAgentName } = useParams<{ name: string }>();
  const navigate = useNavigate();
  const isMobile = useIsMobile();
  const [agents, setAgents] = useState<Agent[]>([]);
  const [capabilities, setCapabilities] = useState<RawCapability[]>([]);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [refreshingAgents, setRefreshingAgents] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [infraBuildId, setInfraBuildId] = useState('');
  const [budgets, setBudgets] = useState<Record<string, { daily_used: number; daily_limit: number }>>({});
  const [fleetView, setFleetView] = useState<FleetView>('all');
  const [query, setQuery] = useState('');
  const deferredQuery = useDeferredValue(query);

  const selectedAgent = urlAgentName ? agents.find((a) => a.name === urlAgentName) ?? null : null;

  const load = useCallback(async () => {
    setRefreshingAgents(true);
    try {
      const [data, caps] = await Promise.all([
        api.agents.list(),
        api.capabilities.list().catch(() => [] as RawCapability[]),
      ]);
      const mapped = (data ?? []).map(mapAgent);
      setAgents(mapped);
      setCapabilities(caps);
      Promise.allSettled(mapped.map((a) =>
        api.agents.budget(a.name).then((b) => ({ name: a.name, daily_used: b.daily_used, daily_limit: b.daily_limit })),
      )).then((results) => {
        const bmap: Record<string, { daily_used: number; daily_limit: number }> = {};
        for (const r of results) {
          if (r.status === 'fulfilled' && r.value.daily_limit > 0) {
            bmap[r.value.name] = { daily_used: r.value.daily_used, daily_limit: r.value.daily_limit };
          }
        }
        setBudgets(bmap);
      });
    } catch (e) {
      console.error(e);
    } finally {
      setRefreshingAgents(false);
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    api.infra.status().then((data) => setInfraBuildId(data.build_id || '')).catch(() => {});
    const unsub = socket.on('agent_status', () => load());
    return () => { unsub(); };
  }, [load]);

  const handleAction = async (name: string, action: string) => {
    setActionLoading(`${name}-${action}`);
    try {
      if (action === 'start') { await api.agents.start(name); toast.success(`Agent "${name}" started`); }
      else if (action === 'pause') { await api.agents.halt(name, 'supervised'); toast.success(`Agent "${name}" paused`); }
      else if (action === 'resume') { await api.agents.resume(name); toast.success(`Agent "${name}" resumed`); }
      else if (action === 'restart') { await api.agents.restart(name); toast.success(`Agent "${name}" restarted`); }
      await load();
    } catch (e: any) {
      toast.error(e.message || 'Action failed');
    } finally {
      setActionLoading(null);
    }
  };

  const handleDelete = async (name: string) => {
    setActionLoading(`${name}-delete`);
    try {
      await api.agents.delete(name);
      toast.success(`Agent "${name}" deleted`);
      if (urlAgentName === name) {
        navigate('/agents', { replace: true });
      }
      await load();
    } catch (e: any) {
      toast.error(e.message || 'Delete failed');
      throw e;
    } finally {
      setActionLoading(null);
    }
  };

  const handleSelect = (name: string) => {
    navigate(`/agents/${name}`, { replace: true });
  };

  const handleCloseDetail = () => {
    navigate('/agents', { replace: true });
  };

  const runningAgents = agents.filter((agent) => agent.status === 'running').length;
  const attentionAgents = agents.filter((agent) => ['halted', 'paused', 'stopped'].includes(agent.status)).length;

  const filteredAgents = useMemo(() => {
    const normalizedQuery = deferredQuery.trim().toLowerCase();

    return agents.filter((agent) => {
      if (fleetView === 'running' && agent.status !== 'running') return false;
      if (fleetView === 'attention' && !['halted', 'paused', 'stopped'].includes(agent.status)) return false;
      if (!normalizedQuery) return true;

      return [
        agent.name,
        agent.team,
        agent.preset,
        agent.role,
        agent.mission,
        agent.mode,
      ]
        .filter(Boolean)
        .some((value) => value!.toLowerCase().includes(normalizedQuery));
    });
  }, [agents, deferredQuery, fleetView]);

  const listRows = useMemo(
    () => filteredAgents.map((a) => ({
      name: a.name,
      status: a.status,
      team: a.team,
      mission: a.mission,
      lastActive: a.lastActive || '',
      budget: budgets[a.name],
    })),
    [filteredAgents, budgets],
  );

  return (
    <div className="flex h-full flex-col">
      <div className="shrink-0 flex items-center justify-between gap-3 px-4 py-3 md:px-6 md:py-4">
        <div className="text-sm text-muted-foreground">{agents.length} total agents</div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" className="h-8 px-3" onClick={() => setCreateOpen(true)}>
            <Plus data-icon="inline-start" />
            Create
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="h-8 w-8 p-0"
            onClick={load}
            disabled={refreshingAgents}
            aria-label={refreshingAgents ? 'Refreshing agents' : 'Refresh agents'}
          >
            <RefreshCw className={refreshingAgents ? 'animate-spin' : ''} aria-hidden="true" />
          </Button>
        </div>
      </div>

      <div className="flex-1 min-h-0 px-4 pb-4 md:px-6 md:pb-6">
        <div className="flex h-full min-h-0 flex-col gap-4 lg:grid lg:grid-cols-[360px_minmax(0,1fr)]">
          <Card className={`${selectedAgent ? 'hidden lg:flex' : 'flex'} min-h-0`}>
            <CardHeader className="border-b border-border/70">
              <CardAction className="flex items-center gap-2">
                <Badge variant="outline">{runningAgents} running</Badge>
                {infraBuildId ? <Badge variant="secondary">Build {infraBuildId}</Badge> : null}
              </CardAction>
              <CardTitle>Fleet</CardTitle>
              <CardDescription>
                Search, segment, and inspect the current agent fleet without leaving the shell.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex min-h-0 flex-1 flex-col gap-4">
              <Tabs value={fleetView} onValueChange={(value) => setFleetView(value as FleetView)}>
                <TabsList className="w-full justify-start">
                  <TabsTrigger value="all">All {agents.length}</TabsTrigger>
                  <TabsTrigger value="running">Running {runningAgents}</TabsTrigger>
                  <TabsTrigger value="attention">Attention {attentionAgents}</TabsTrigger>
                </TabsList>
              </Tabs>

              <div className="relative">
                <Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
                <Input
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder="Search fleet"
                  className="pl-9"
                  aria-label="Search fleet"
                />
              </div>

              <div className="min-h-0 flex-1 overflow-auto">
                {loading ? (
                  <div className="py-4 text-sm text-muted-foreground">Loading agents…</div>
                ) : filteredAgents.length === 0 ? (
                  <div className="rounded-lg border border-dashed border-border/80 bg-background/60 p-4 text-sm text-muted-foreground">
                    {agents.length === 0 ? 'No agents. Create one to get started.' : 'No agents match the current fleet filter.'}
                  </div>
                ) : (
                  <AgentList
                    agents={listRows}
                    selectedAgent={selectedAgent?.name ?? null}
                    onSelect={handleSelect}
                  />
                )}
              </div>
            </CardContent>
          </Card>

          <Card className={`${!selectedAgent && isMobile ? 'hidden' : 'flex'} min-h-0`}>
            {selectedAgent ? (
              <AgentDetail
                agent={selectedAgent}
                infraBuildId={infraBuildId}
                capabilities={capabilities}
                onClose={handleCloseDetail}
                onAction={handleAction}
                onDelete={handleDelete}
                actionLoading={actionLoading}
                onRefreshAgents={load}
              />
            ) : (
              <div className="flex h-full min-h-[320px] items-center justify-center px-6 text-center">
                <div className="space-y-2">
                  <div className="text-sm font-medium text-foreground">No agent selected</div>
                  <div className="text-sm text-muted-foreground">
                    Choose an agent from the fleet panel to inspect activity, controls, and system state.
                  </div>
                </div>
              </div>
            )}
          </Card>
        </div>
      </div>

      <CreateAgentDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={({ name, started, dmChannel }) => {
          void load();
          if (dmChannel) {
            navigate(`/channels/${encodeURIComponent(dmChannel)}`);
            return;
          }
          if (started) {
            navigate(`/agents/${encodeURIComponent(name)}`);
          }
        }}
      />
    </div>
  );
}
