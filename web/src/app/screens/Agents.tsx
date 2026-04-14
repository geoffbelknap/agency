import { useState, useEffect, useCallback, useMemo } from 'react';
import { useParams, useNavigate } from 'react-router';
import { RefreshCw, Plus } from 'lucide-react';
import { toast } from 'sonner';
import { Agent } from '../types';
import { Button } from '../components/ui/button';
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
      // Load budgets for the table (non-blocking)
      Promise.allSettled(mapped.map((a) =>
        api.agents.budget(a.name).then((b) => ({ name: a.name, daily_used: b.daily_used, daily_limit: b.daily_limit }))
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
    api.infra.status().then(data => setInfraBuildId(data.build_id || '')).catch(() => {});
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

  const handleSelect = (name: string) => {
    navigate(`/agents/${name}`, { replace: true });
  };

  const handleCloseDetail = () => {
    navigate('/agents', { replace: true });
  };

  // Build AgentList rows
  const listRows = useMemo(
    () => agents.map((a) => ({
      name: a.name,
      status: a.status,
      team: a.team,
      mission: a.mission,
      lastActive: a.lastActive || '',
      budget: budgets[a.name],
    })),
    [agents, budgets],
  );

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="shrink-0 p-4 md:px-8 md:pt-6 md:pb-4 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-xl md:text-2xl text-foreground">Agents</h1>
          <p className="text-sm text-muted-foreground mt-1">{agents.length} total agents</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" className="h-8 px-3" onClick={() => setCreateOpen(true)}>
            <Plus className="w-3.5 h-3.5 mr-1" aria-hidden="true" />
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
            <RefreshCw className={`w-3.5 h-3.5 ${refreshingAgents ? 'animate-spin' : ''}`} aria-hidden="true" />
          </Button>
        </div>
      </div>

      <div className="flex-1 min-h-0 px-4 pb-4 md:px-8 md:pb-8">
        <div className="flex h-full min-h-0 flex-col gap-4 lg:grid lg:grid-cols-[360px_minmax(0,1fr)] lg:gap-6">
          <div className={`${selectedAgent ? 'hidden lg:flex' : 'flex'} min-h-0 flex-col rounded-2xl border border-border bg-card`}>
            <div className="border-b border-border px-4 py-3">
              <h2 className="text-sm font-medium text-foreground">Fleet</h2>
              <p className="mt-1 text-xs text-muted-foreground">
                Review status, budget pressure, and recent activity at a glance.
              </p>
            </div>
            <div className="min-h-0 flex-1 overflow-auto px-4 py-3">
              {loading ? (
                <div className="text-sm text-muted-foreground py-4">Loading agents…</div>
              ) : agents.length === 0 ? (
                <div className="text-sm text-muted-foreground py-4">No agents. Create one to get started.</div>
              ) : (
                <AgentList
                  agents={listRows}
                  selectedAgent={selectedAgent?.name ?? null}
                  onSelect={handleSelect}
                />
              )}
            </div>
          </div>

          <div className={`${!selectedAgent && isMobile ? 'hidden' : 'block'} min-h-0 overflow-auto rounded-2xl border border-border bg-card`}>
            {selectedAgent ? (
              <AgentDetail
                agent={selectedAgent}
                infraBuildId={infraBuildId}
                capabilities={capabilities}
                onClose={handleCloseDetail}
                onAction={handleAction}
                actionLoading={actionLoading}
                onRefreshAgents={load}
              />
            ) : (
              <div className="flex h-full min-h-[280px] items-center justify-center px-6 text-center">
                <div className="space-y-2">
                  <div className="text-sm font-medium text-foreground">No agent selected</div>
                  <div className="text-sm text-muted-foreground">
                    Choose an agent from the fleet panel to inspect activity, controls, and system state.
                  </div>
                </div>
              </div>
            )}
          </div>
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
