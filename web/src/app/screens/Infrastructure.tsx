import { useState, useEffect, useCallback } from 'react';
import { Link } from 'react-router';
import { RefreshCw, Activity, Play, Square, RotateCw } from 'lucide-react';
import { toast } from 'sonner';
import { StatusIndicator } from '../components/StatusIndicator';
import { InfrastructureService } from '../types';
import { Button } from '../components/ui/button';
import { api, type RawInfraCapacity } from '../lib/api';
import { socket } from '../lib/ws';

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

function formatGB(mb: number) {
  if (!Number.isFinite(mb) || mb <= 0) return '0 GB';
  return `${(mb / 1024).toFixed(1)} GB`;
}

function capacityPercent(capacity: RawInfraCapacity) {
  if (!capacity.max_agents) return 0;
  return Math.min(100, Math.round(((capacity.running_agents + capacity.running_meeseeks) / capacity.max_agents) * 100));
}

export function Infrastructure() {
  const [services, setServices] = useState<InfrastructureService[]>([]);
  const [capacity, setCapacity] = useState<RawInfraCapacity | null>(null);
  const [capacityError, setCapacityError] = useState<string | null>(null);
  const [capacityLoading, setCapacityLoading] = useState(true);
  const [restarting, setRestarting] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [globalAction, setGlobalAction] = useState<InfraAction | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [infraBuildId, setInfraBuildId] = useState('');

  const load = useCallback(async () => {
    setRefreshing(true);
    try {
      const infraData = await api.infra.status();
      const mapped: InfrastructureService[] = (infraData.components ?? []).map((s: any) => ({
        id: s.name,
        name: s.name,
        state: s.state || s.status || 'stopped',
        health: s.health === 'healthy' || s.health === 'unhealthy' ? s.health : 'idle',
        containerId: s.container_id || '',
        uptime: s.uptime || '',
      }));
      setServices(mapped);
      setInfraBuildId(infraData.build_id || '');
      return mapped;
    } catch (err) {
      console.error('Infrastructure load error:', err);
      return [];
    } finally {
      setRefreshing(false);
      setLoading(false);
    }
  }, []);

  const loadCapacity = useCallback(async () => {
    setCapacityLoading(true);
    try {
      setCapacity(await api.infra.capacity());
      setCapacityError(null);
    } catch (err: any) {
      setCapacity(null);
      setCapacityError(err.message || 'Capacity config not available');
    } finally {
      setCapacityLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    loadCapacity();
    const unsub = socket.on('infra_status', load);
    return () => { unsub(); };
  }, [load, loadCapacity]);

  const handleRestart = async (serviceId: string) => {
    setRestarting(serviceId);
    try {
      await api.infra.rebuild(serviceId);
      await load();
    } catch (err) {
      console.error('handleRestart error:', err);
    } finally {
      setRestarting(null);
    }
  };

  const waitForInfraState = useCallback(async (target: InfraAction) => {
    for (let attempt = 0; attempt < 12; attempt += 1) {
      const next = await load();
      if (target === 'stop') {
        if (next.every((service) => isStoppedState(service.state))) return true;
      } else if (next.length > 0 && next.every((service) => service.state === 'running')) {
        return true;
      }
      await sleep(1000);
    }
    return false;
  }, [load]);

  const handleGlobalAction = async (action: InfraAction) => {
    setGlobalAction(action);
    try {
      if (action === 'start') await api.infra.up();
      else if (action === 'stop') await api.infra.down();
      else await api.infra.reload();

      const settled = await waitForInfraState(action);
      if (settled) {
        toast.success(`Infrastructure ${action === 'restart' ? 'running' : action === 'start' ? 'started' : 'stopped'}`);
      } else {
        toast.success(`Infrastructure ${action} initiated`);
      }
    } catch (e: any) {
      toast.error(e.message || `Failed to ${action} infrastructure`);
    } finally {
      setGlobalAction(null);
    }
  };

  const healthyCount = services.filter((s) => s.health === 'healthy').length;
  const usedSlots = capacity ? capacity.running_agents + capacity.running_meeseeks : 0;
  const hasRunningServices = services.some((service) => isRunningState(service.state));
  const unhealthyServices = services.filter((service) => service.health === 'unhealthy');
  const stoppedServices = services.filter((service) => isStoppedState(service.state));
  const primaryAction: InfraAction = hasRunningServices ? 'restart' : 'start';
  const primaryActionLabel =
    globalAction === 'start' ? 'Starting...' :
    globalAction === 'restart' ? 'Restarting...' :
    hasRunningServices ? 'Restart All' : 'Start All';

  return (
    <div className="space-y-4">
      {/* Actions */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-2">
              <Activity className="w-4 h-4 text-muted-foreground" />
              <span className="text-sm text-muted-foreground">
                {globalAction === 'start' && 'Starting infrastructure...'}
                {globalAction === 'restart' && 'Restarting infrastructure...'}
                {globalAction === 'stop' && 'Stopping infrastructure...'}
                {globalAction === null && `${healthyCount} / ${services.length} healthy`}
              </span>
            </div>
            {infraBuildId && (
              <span className="text-[10px] text-muted-foreground font-mono">
                Build: {infraBuildId}
              </span>
            )}
          </div>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => { load(); loadCapacity(); }}
            disabled={globalAction !== null || refreshing || capacityLoading}
            aria-label={refreshing || capacityLoading ? 'Refreshing infrastructure' : 'Refresh infrastructure'}
          >
            <RefreshCw className={`w-3 h-3 mr-1 ${refreshing || capacityLoading ? 'animate-spin' : ''}`} />
            {refreshing || capacityLoading ? 'Refreshing...' : 'Refresh'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleGlobalAction(primaryAction)}
            disabled={globalAction !== null}
          >
            {hasRunningServices ? <RotateCw className="w-3 h-3 mr-1" /> : <Play className="w-3 h-3 mr-1" />}
            {primaryActionLabel}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleGlobalAction('stop')}
            disabled={globalAction !== null || !hasRunningServices}
          >
            <Square className="w-3 h-3 mr-1" />
            {globalAction === 'stop' ? 'Stopping...' : 'Stop All'}
          </Button>
        </div>
      </div>

      {!loading && (unhealthyServices.length > 0 || (!hasRunningServices && services.length > 0)) && (
        <div className="bg-card border border-border rounded p-4 space-y-3">
          <div className="flex items-center gap-2 text-sm text-amber-400">
            <Activity className="w-4 h-4" />
            <span>
              {unhealthyServices.length > 0
                ? `${unhealthyServices.length} ${unhealthyServices.length === 1 ? 'service is' : 'services are'} unhealthy`
                : `${stoppedServices.length} ${stoppedServices.length === 1 ? 'service is' : 'services are'} not running`}
            </span>
          </div>
          <div className="text-xs text-muted-foreground">
            Use infrastructure controls first. If services still do not recover cleanly, run Doctor to identify whether the problem is platform-wide or isolated to one agent.
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              size="sm"
              className="h-8 text-xs"
              onClick={() => handleGlobalAction(primaryAction)}
              disabled={globalAction !== null}
            >
              {hasRunningServices ? 'Restart infrastructure' : 'Start infrastructure'}
            </Button>
            <Button asChild variant="outline" size="sm" className="h-8 text-xs">
              <Link to="/admin/doctor">Open Doctor</Link>
            </Button>
          </div>
        </div>
      )}

      <div className="bg-card border border-border rounded overflow-hidden">
        <div className="px-4 py-3 border-b border-border">
          <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Host Capacity</h3>
          <p className="text-[10px] text-muted-foreground/70 mt-0.5">Agent and meeseeks slot budget enforced by the runtime</p>
        </div>
        {capacityLoading ? (
          <div className="px-4 py-6 text-sm text-muted-foreground text-center">Loading capacity...</div>
        ) : capacityError ? (
          <div className="px-4 py-4 text-sm text-amber-700 dark:text-amber-300 bg-amber-50 dark:bg-amber-950/20">
            {capacityError}
          </div>
        ) : capacity && (
          <div className="p-4 space-y-4">
            <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
              <div>
                <div className="text-[10px] text-muted-foreground uppercase tracking-wide">Slots Used</div>
                <div className="text-lg font-semibold text-foreground">{usedSlots} / {capacity.max_agents}</div>
                <div className="text-[10px] text-muted-foreground">{capacity.available_slots} available</div>
              </div>
              <div>
                <div className="text-[10px] text-muted-foreground uppercase tracking-wide">Agents</div>
                <div className="text-lg font-semibold text-foreground">{capacity.running_agents}</div>
                <div className="text-[10px] text-muted-foreground">{formatGB(capacity.agent_slot_mb)} each</div>
              </div>
              <div>
                <div className="text-[10px] text-muted-foreground uppercase tracking-wide">Meeseeks</div>
                <div className="text-lg font-semibold text-foreground">{capacity.running_meeseeks}</div>
                <div className="text-[10px] text-muted-foreground">{capacity.max_concurrent_meesks} max concurrent</div>
              </div>
              <div>
                <div className="text-[10px] text-muted-foreground uppercase tracking-wide">Memory</div>
                <div className="text-lg font-semibold text-foreground">{formatGB(capacity.host_memory_mb)}</div>
                <div className="text-[10px] text-muted-foreground">{formatGB(capacity.system_reserve_mb + capacity.infra_overhead_mb)} reserved</div>
              </div>
              <div>
                <div className="text-[10px] text-muted-foreground uppercase tracking-wide">Networks</div>
                <div className={capacity.network_pool_configured ? 'text-lg font-semibold text-green-400' : 'text-lg font-semibold text-amber-400'}>
                  {capacity.network_pool_configured ? 'Configured' : 'Default'}
                </div>
                <div className="text-[10px] text-muted-foreground">{capacity.host_cpu_cores} CPU cores</div>
              </div>
            </div>
            <div className="h-2 rounded bg-secondary overflow-hidden" aria-label={`Capacity ${capacityPercent(capacity)}% used`}>
              <div className="h-full bg-green-500" style={{ width: `${capacityPercent(capacity)}%` }} />
            </div>
          </div>
        )}
      </div>

      {/* Services Table */}
      <div className="bg-card border border-border rounded overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm min-w-[480px]">
            <thead>
              <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
                <th className="text-left p-3 md:p-4 font-medium">Service</th>
                <th className="text-left p-3 md:p-4 font-medium">State</th>
                <th className="text-left p-3 md:p-4 font-medium">Health</th>
                <th className="text-left p-3 md:p-4 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <td colSpan={4} className="p-4 text-center text-muted-foreground text-xs">Loading...</td>
                </tr>
              ) : services.length === 0 ? (
                <tr>
                  <td colSpan={4} className="p-8 text-center text-muted-foreground text-sm">
                    No infrastructure services running. Click "Start All" to launch the platform.
                  </td>
                </tr>
              ) : (
                services.map((service) => (
                  <tr
                    key={service.id}
                    className="border-b border-border hover:bg-secondary/50 transition-colors"
                  >
                    <td className="p-4">
                      <code className="text-foreground">{service.name}</code>
                    </td>
                    <td className="p-4">
                      <span className="text-muted-foreground capitalize text-xs">{formatStateLabel(service, globalAction)}</span>
                    </td>
                    <td className="p-4">
                      <div className="flex items-center gap-2">
                        <StatusIndicator status={visualStatus(service, globalAction)} size="sm" />
                        <span className="text-muted-foreground capitalize text-xs">{formatStateLabel(service, globalAction)}</span>
                      </div>
                    </td>
                    <td className="p-4">
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-7 text-xs"
                        onClick={() => handleRestart(service.id)}
                        disabled={restarting === service.id}
                      >
                        {restarting === service.id ? (
                          <>
                            <RotateCw className="w-3 h-3 mr-1 animate-spin" />
                            Restarting...
                          </>
                        ) : (
                          <>
                            <RotateCw className="w-3 h-3 mr-1" />
                            Restart
                          </>
                        )}
                      </Button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
