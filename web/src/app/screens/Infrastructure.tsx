import { useState, useEffect, useCallback, type ReactNode } from 'react';
import { RefreshCw, Activity, Play, Square, RotateCw, Terminal } from 'lucide-react';
import { toast } from 'sonner';
import { InfrastructureService } from '../types';
import { api, type RawInfraCapacity } from '../lib/api';
import { featureEnabled } from '../lib/features';
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
  const used = Math.max(0, capacity.max_agents - capacity.available_slots);
  return Math.min(100, Math.round((used / capacity.max_agents) * 100));
}

type StatusKey = 'healthy' | 'running' | 'idle' | 'starting' | 'warning' | 'unhealthy' | 'stopped';
type BadgeTone = 'teal' | 'amber' | 'red' | 'neutral';

const STATUS_STYLE: Record<StatusKey, { label: string; dot: string; tone: BadgeTone }> = {
  healthy: { label: 'Healthy', dot: 'var(--teal)', tone: 'teal' },
  running: { label: 'Running', dot: 'var(--teal)', tone: 'teal' },
  idle: { label: 'Idle', dot: 'var(--ink-faint)', tone: 'neutral' },
  starting: { label: 'Starting', dot: 'var(--amber)', tone: 'amber' },
  warning: { label: 'Warning', dot: 'var(--amber)', tone: 'amber' },
  unhealthy: { label: 'Unhealthy', dot: 'var(--red)', tone: 'red' },
  stopped: { label: 'Stopped', dot: 'var(--red)', tone: 'red' },
};

function serviceStatusKey(service: InfrastructureService, action: InfraAction | null): StatusKey {
  const status = visualStatus(service, action);
  if (status === 'healthy') return 'healthy';
  if (status === 'unhealthy') return 'unhealthy';
  if (status === 'starting' || status === 'stopping' || status === 'restarting') return 'starting';
  if (isStoppedState(service.state)) return 'stopped';
  if (isRunningState(service.state)) return 'running';
  return 'idle';
}

function StatusDot({ status, pulse = false }: { status: StatusKey; pulse?: boolean }) {
  const style = STATUS_STYLE[status];
  return (
    <span style={{ position: 'relative', width: 8, height: 8, borderRadius: '50%', background: style.dot, flexShrink: 0 }}>
      {pulse && <span style={{ position: 'absolute', inset: 0, borderRadius: '50%', background: style.dot, animation: 'agencyPulse 1.8s ease-out infinite' }} />}
    </span>
  );
}

function Badge({ children, tone = 'neutral' }: { children: ReactNode; tone?: BadgeTone }) {
  const tones = {
    teal: { bg: 'var(--teal-tint)', color: 'var(--teal-dark)', border: 'var(--teal-border)' },
    amber: { bg: 'var(--amber-tint)', color: '#8B5A00', border: 'var(--amber)' },
    red: { bg: 'var(--red-tint)', color: 'var(--red)', border: 'var(--red)' },
    neutral: { bg: 'var(--warm-3)', color: 'var(--ink-mid)', border: 'var(--ink-hairline-strong)' },
  }[tone];

  return (
    <span className="mono" style={{ display: 'inline-flex', alignItems: 'center', gap: 4, padding: '2px 8px', fontSize: 10, letterSpacing: '0.08em', textTransform: 'uppercase', background: tones.bg, color: tones.color, border: `0.5px solid ${tones.border}`, borderRadius: 4 }}>
      {children}
    </span>
  );
}

function MetaStat({ label, value, tone }: { label: string; value: string; tone?: string }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      <span className="eyebrow" style={{ fontSize: 9 }}>{label}</span>
      <span className="mono" style={{ fontSize: 14, color: tone || 'var(--ink)' }}>{value}</span>
    </div>
  );
}

function DesignButton({ children, icon, variant = 'default', disabled = false, onClick }: { children: ReactNode; icon?: ReactNode; variant?: 'default' | 'primary' | 'ghost'; disabled?: boolean; onClick?: () => void }) {
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
      style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '5px 10px', fontSize: 12, fontWeight: 400, fontFamily: 'var(--sans)', cursor: disabled ? 'default' : 'pointer', background: variants.bg, color: variants.color, border: variants.border, borderRadius: 999, opacity: disabled ? 0.5 : 1 }}
    >
      {icon}
      {children}
    </button>
  );
}

export function Infrastructure() {
  const showMeeseeks = featureEnabled('meeseeks');
  const [services, setServices] = useState<InfrastructureService[]>([]);
  const [capacity, setCapacity] = useState<RawInfraCapacity | null>(null);
  const [capacityError, setCapacityError] = useState<string | null>(null);
  const [capacityLoading, setCapacityLoading] = useState(true);
  const [restarting, setRestarting] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [globalAction, setGlobalAction] = useState<InfraAction | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [infraBuildId, setInfraBuildId] = useState('');
  const [logComponent, setLogComponent] = useState<string | null>(null);
  const [logText, setLogText] = useState('');
  const [logLoading, setLogLoading] = useState(false);
  const [logError, setLogError] = useState<string | null>(null);

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

  const loadLogs = async (component: string) => {
    setLogComponent(component);
    setLogLoading(true);
    setLogError(null);
    setLogText('');
    try {
      const result = await api.infra.logs(component, 200);
      setLogText(result.logs || '');
    } catch (e: any) {
      setLogError(e.message || 'Failed to load logs');
    } finally {
      setLogLoading(false);
    }
  };

  const healthyCount = services.filter((s) => s.health === 'healthy').length;
  const usedSlots = capacity ? Math.max(0, capacity.max_agents - capacity.available_slots) : 0;
  const hasRunningServices = services.some((service) => isRunningState(service.state));
  const unhealthyServices = services.filter((service) => service.health === 'unhealthy');
  const stoppedServices = services.filter((service) => isStoppedState(service.state));
  const primaryAction: InfraAction = hasRunningServices ? 'restart' : 'start';
  const primaryActionLabel =
    globalAction === 'start' ? 'Starting...' :
    globalAction === 'restart' ? 'Restarting...' :
    hasRunningServices ? 'Restart All' : 'Start All';

  return (
    <div>
      <div style={{ marginBottom: 20 }}>
        <div style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
          <div style={{ minWidth: 0, flex: 1 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, color: unhealthyServices.length > 0 || stoppedServices.length > 0 ? 'var(--amber)' : 'var(--teal-dark)' }}>
              <Activity size={14} />
              <span className="mono" style={{ fontSize: 12 }}>
                {globalAction === 'start' && 'starting infrastructure'}
                {globalAction === 'restart' && 'restarting infrastructure'}
                {globalAction === 'stop' && 'stopping infrastructure'}
                {globalAction === null && `${healthyCount} / ${services.length || 0} services healthy`}
              </span>
            </div>
            {(unhealthyServices.length > 0 || (!hasRunningServices && services.length > 0)) && (
              <p style={{ margin: '6px 0 0', color: 'var(--ink-mid)', fontSize: 12, maxWidth: 680 }}>
                {unhealthyServices.length > 0
                  ? `${unhealthyServices.length} ${unhealthyServices.length === 1 ? 'service is' : 'services are'} unhealthy. Restart the affected service first, then run Doctor if it persists.`
                  : `${stoppedServices.length} ${stoppedServices.length === 1 ? 'service is' : 'services are'} not running.`}
              </p>
            )}
          </div>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
            <DesignButton
              icon={<RefreshCw size={13} className={refreshing || capacityLoading ? 'animate-spin' : ''} />}
              disabled={globalAction !== null || refreshing || capacityLoading}
              onClick={() => { load(); loadCapacity(); }}
            >
              Reload config
            </DesignButton>
            <DesignButton
              variant="primary"
              icon={hasRunningServices ? <RotateCw size={13} /> : <Play size={13} />}
              disabled={globalAction !== null}
              onClick={() => handleGlobalAction(primaryAction)}
            >
              {primaryActionLabel}
            </DesignButton>
            <DesignButton
              icon={<Square size={13} />}
              disabled={globalAction !== null || !hasRunningServices}
              onClick={() => handleGlobalAction('stop')}
            >
              {globalAction === 'stop' ? 'Stopping...' : 'Stop All'}
            </DesignButton>
          </div>
        </div>
      </div>

      <div style={{ display: 'flex', gap: 20, alignItems: 'center', flexWrap: 'wrap', marginBottom: 20 }}>
        <MetaStat label="Build" value={infraBuildId || 'local'} />
        <MetaStat label="Services" value={loading ? '...' : `${healthyCount} / ${services.length}`} tone={healthyCount === services.length && services.length > 0 ? 'var(--teal-dark)' : 'var(--amber)'} />
        <MetaStat label="Slots" value={capacityLoading || !capacity ? '...' : `${usedSlots} / ${capacity.max_agents}`} />
        <MetaStat label="CPU" value={capacityLoading || !capacity ? '...' : `${capacity.host_cpu_cores} cores`} />
        <MetaStat label="Memory" value={capacityLoading || !capacity ? '...' : formatGB(capacity.host_memory_mb)} />
        {showMeeseeks && <MetaStat label="Meeseeks" value={capacityLoading || !capacity ? '...' : `${capacity.running_meeseeks} / ${capacity.max_concurrent_meesks}`} />}
      </div>

      {capacityError && (
        <div style={{ marginBottom: 14, padding: '10px 12px', border: '0.5px solid var(--amber)', background: 'var(--amber-tint)', color: '#8B5A00', borderRadius: 8, fontSize: 12 }}>
          {capacityError}
        </div>
      )}

      {capacity && (
        <div style={{ marginBottom: 18, padding: 14, background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 10 }}>
          <div style={{ display: 'flex', alignItems: 'baseline', gap: 12, marginBottom: 8, flexWrap: 'wrap' }}>
            <div className="eyebrow">Host capacity</div>
            <span className="mono" style={{ color: 'var(--ink-mid)', fontSize: 11 }}>
              {usedSlots} slots used / {capacity.available_slots} available / {formatGB(capacity.agent_slot_mb)} per agent
            </span>
          </div>
          <div style={{ height: 8, borderRadius: 2, background: 'var(--warm-3)', overflow: 'hidden' }} aria-label={`Capacity ${capacityPercent(capacity)}% used`}>
            <div style={{ width: `${capacityPercent(capacity)}%`, height: '100%', background: 'var(--teal)' }} />
          </div>
        </div>
      )}

      {loading ? (
        <div style={{ padding: 28, textAlign: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>Loading infrastructure...</div>
      ) : services.length === 0 ? (
        <div style={{ padding: 28, textAlign: 'center', color: 'var(--ink-mid)', fontSize: 13, background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 10 }}>
          No infrastructure services running. Start all services to launch the local platform.
        </div>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))', gap: 12 }}>
          {services.map((service) => {
            const status = serviceStatusKey(service, restarting === service.id ? 'restart' : globalAction);
            const statusStyle = STATUS_STYLE[status];
            return (
              <div key={service.id} style={{ background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 10, padding: 14 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
                  <StatusDot status={status} pulse={status === 'starting'} />
                  <span className="mono" style={{ fontSize: 13, color: 'var(--ink)', minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{service.name}</span>
                  <span style={{ marginLeft: 'auto' }}>
                    <Badge tone={statusStyle.tone}>{formatStateLabel(service, restarting === service.id ? 'restart' : globalAction)}</Badge>
                  </span>
                </div>
                <div style={{ fontSize: 11, color: 'var(--ink-mid)', display: 'flex', justifyContent: 'space-between', gap: 10 }}>
                  <span>uptime</span>
                  <span className="mono" style={{ color: 'var(--ink)' }}>{service.uptime || '...'}</span>
                </div>
                <div style={{ fontSize: 11, color: 'var(--ink-mid)', display: 'flex', justifyContent: 'space-between', gap: 10, marginTop: 2 }}>
                  <span>container</span>
                  <span className="mono" style={{ color: 'var(--ink)', maxWidth: 130, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{service.containerId || 'not assigned'}</span>
                </div>
                <div style={{ marginTop: 12, display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                  <DesignButton
                    variant="ghost"
                    icon={<RotateCw size={13} className={restarting === service.id ? 'animate-spin' : ''} />}
                    onClick={() => handleRestart(service.id)}
                    disabled={restarting === service.id || globalAction !== null}
                  >
                    {restarting === service.id ? 'Restarting...' : 'Restart'}
                  </DesignButton>
                  <DesignButton variant="ghost" icon={<Terminal size={13} />} onClick={() => loadLogs(service.name)}>
                    Logs
                  </DesignButton>
                </div>
              </div>
            );
          })}
        </div>
      )}
      {logComponent && (
        <div style={{ position: 'fixed', inset: 0, zIndex: 80, display: 'flex', alignItems: 'flex-end', justifyContent: 'center', background: 'rgba(26, 23, 20, 0.28)', padding: 24 }} onClick={() => setLogComponent(null)}>
          <div style={{ width: 'min(960px, 100%)', maxHeight: '78vh', display: 'flex', flexDirection: 'column', background: 'var(--warm)', border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 14, overflow: 'hidden' }} onClick={(event) => event.stopPropagation()}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '14px 16px', borderBottom: '0.5px solid var(--ink-hairline)', background: 'var(--warm-2)' }}>
              <Terminal size={15} color="var(--ink-mid)" />
              <div style={{ minWidth: 0, flex: 1 }}>
                <div className="eyebrow" style={{ fontSize: 9 }}>Container logs</div>
                <div className="mono" style={{ fontSize: 13, color: 'var(--ink)' }}>{logComponent}</div>
              </div>
              <DesignButton variant="ghost" icon={<RefreshCw size={13} className={logLoading ? 'animate-spin' : ''} />} disabled={logLoading} onClick={() => loadLogs(logComponent)}>
                Refresh
              </DesignButton>
              <DesignButton variant="default" onClick={() => setLogComponent(null)}>
                Close
              </DesignButton>
            </div>
            <div style={{ overflow: 'auto', padding: 16, background: 'var(--ink)', color: 'var(--warm)', minHeight: 260 }}>
              {logLoading ? (
                <div className="mono" style={{ color: 'rgba(253,250,245,0.65)', fontSize: 12 }}>Loading logs...</div>
              ) : logError ? (
                <div className="mono" style={{ color: '#FCA5A5', fontSize: 12 }}>{logError}</div>
              ) : (
                <pre className="mono" style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-word', fontSize: 11, lineHeight: 1.55 }}>
                  {logText || 'No log output returned.'}
                </pre>
              )}
            </div>
          </div>
        </div>
      )}
      </div>
  );
}
