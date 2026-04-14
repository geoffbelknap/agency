import { useState, useEffect } from 'react';
import { Play, Pause, RefreshCw, X, Loader2 } from 'lucide-react';
import { Button } from '../../components/ui/button';
import { Agent } from '../../types';
import { type RawCapability } from '../../lib/api';
import { useAgentDetailData } from './useAgentDetailData';
import { AgentOverviewTab } from './AgentOverviewTab';
import { AgentActivityTab } from './AgentActivityTab';
import { AgentOperationsTab } from './AgentOperationsTab';
import { AgentSystemTab } from './AgentSystemTab';
import { formatDateTimeShort } from '../../lib/time';
import { adminFeatureFlags, featureEnabled } from '../../lib/features';

type PrimaryTab = 'overview' | 'activity' | 'operations' | 'system';
type OperationsSubTab = 'channels' | 'knowledge' | 'meeseeks' | 'economics';
type SystemSubTab = 'config' | 'logs';

interface Props {
  agent: Agent;
  infraBuildId: string;
  capabilities: RawCapability[];
  onClose: () => void;
  onAction: (name: string, action: string) => Promise<void>;
  actionLoading: string | null;
  onRefreshAgents: () => void;
}

export function AgentDetail({ agent, infraBuildId, capabilities: initialCapabilities, onClose, onAction, actionLoading, onRefreshAgents }: Props) {
  const [primaryTab, setPrimaryTab] = useState<PrimaryTab>('overview');
  const [opsSubTab, setOpsSubTab] = useState<OperationsSubTab>('channels');
  const [sysSubTab, setSysSubTab] = useState<SystemSubTab>('config');

  // Reset tab when agent changes
  useEffect(() => {
    setPrimaryTab('overview');
    setOpsSubTab('channels');
    setSysSubTab('config');
  }, [agent.name]);

  // Compute effective data tab for lazy loading in the hook
  const effectiveDataTab = primaryTab === 'operations' ? opsSubTab
    : primaryTab === 'system' ? sysSubTab
    : primaryTab;

  const data = useAgentDetailData(agent.name, initialCapabilities, effectiveDataTab, onRefreshAgents);
  const showTeams = featureEnabled('teams');
  const showMissions = featureEnabled('missions');
  const showTrust = adminFeatureFlags.trust;

  return (
    <div className="flex flex-col h-full bg-card">
      {/* Header */}
      <div className="shrink-0 border-b border-border p-4">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0 space-y-3">
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <code className="truncate text-lg text-foreground">{agent.name}</code>
                <span className="rounded-full border border-border bg-muted px-2 py-0.5 text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                  {agent.status}
                </span>
              {agent.buildId && infraBuildId && agent.buildId !== infraBuildId && (
                <span className="text-[10px] bg-amber-50 dark:bg-amber-950 text-amber-700 dark:text-amber-400 px-1.5 py-0.5 rounded">
                  stale
                </span>
              )}
              </div>
              <div className="text-xs text-muted-foreground">
                {[agent.type, agent.role, showTeams && agent.team && `team: ${agent.team}`].filter(Boolean).join(' · ')}
              </div>
              {showMissions && agent.mission && (
                <div className="rounded-xl border border-border bg-muted/40 px-3 py-2">
                  <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">Mission</div>
                  <div className="mt-1 text-sm text-foreground">{agent.mission}</div>
                </div>
              )}
            </div>
            <div className="flex flex-wrap gap-2 text-[11px] text-muted-foreground">
              {agent.mode && (
                <span className="rounded-full bg-secondary px-2.5 py-1">
                  Mode: <span className="text-foreground">{agent.mode}</span>
                </span>
              )}
              {showTrust && agent.trustLevel != null && agent.trustLevel > 0 && (
                <span className="rounded-full bg-secondary px-2.5 py-1">
                  Trust: <span className="text-foreground">{agent.trustLevel}/5</span>
                </span>
              )}
              {agent.lastActive && (
                <span className="rounded-full bg-secondary px-2.5 py-1">
                  Last active: <span className="text-foreground">{formatDateTimeShort(agent.lastActive)}</span>
                </span>
              )}
            </div>
          </div>
          <Button size="sm" variant="ghost" onClick={onClose} className="h-8 w-8 p-0" aria-label="Close">
            <X className="w-4 h-4" aria-hidden="true" />
          </Button>
        </div>
        <div className="mt-3 flex flex-wrap gap-2 items-center">
          {agent.status !== 'running' && agent.status !== 'halted' && agent.status !== 'unhealthy' && (
            <Button size="sm" variant="outline" className="h-7 text-xs"
              disabled={!!actionLoading}
              onClick={() => onAction(agent.name, 'start')}>
              {actionLoading === `${agent.name}-start`
                ? <Loader2 className="w-3 h-3 mr-1 animate-spin" aria-hidden="true" />
                : <Play className="w-3 h-3 mr-1" />}
              {actionLoading === `${agent.name}-start` ? 'Starting...' : 'Start'}
            </Button>
          )}
          {agent.status === 'halted' && (
            <Button size="sm" variant="outline" className="h-7 text-xs"
              disabled={!!actionLoading}
              onClick={() => onAction(agent.name, 'resume')}>
              {actionLoading === `${agent.name}-resume`
                ? <Loader2 className="w-3 h-3 mr-1 animate-spin" />
                : <Play className="w-3 h-3 mr-1" />}
              {actionLoading === `${agent.name}-resume` ? 'Resuming...' : 'Resume'}
            </Button>
          )}
          {(agent.status === 'running' || agent.status === 'unhealthy') && (
            <>
              <Button size="sm" variant="outline" className="h-7 text-xs"
                disabled={!!actionLoading}
                onClick={() => onAction(agent.name, 'restart')}>
                {actionLoading === `${agent.name}-restart`
                  ? <Loader2 className="w-3 h-3 mr-1 animate-spin" aria-hidden="true" />
                  : <RefreshCw className="w-3 h-3 mr-1" />}
                {actionLoading === `${agent.name}-restart` ? 'Restarting…' : 'Restart'}
              </Button>
              <Button size="sm" variant="outline" className="h-7 text-xs"
                disabled={!!actionLoading}
                onClick={() => onAction(agent.name, 'pause')}>
                {actionLoading === `${agent.name}-pause`
                  ? <Loader2 className="w-3 h-3 mr-1 animate-spin" />
                  : <Pause className="w-3 h-3 mr-1" />}
                {actionLoading === `${agent.name}-pause` ? 'Pausing…' : 'Pause'}
              </Button>
            </>
          )}
        </div>
      </div>

      {/* Primary tabs */}
      <div role="tablist" className="shrink-0 flex border-b border-border px-2 overflow-x-auto">
        {([
          { id: 'overview' as PrimaryTab, label: 'Overview' },
          { id: 'activity' as PrimaryTab, label: 'Activity' },
          { id: 'operations' as PrimaryTab, label: 'Operations' },
          { id: 'system' as PrimaryTab, label: 'System' },
        ]).map((tab) => (
          <button key={tab.id} role="tab" aria-selected={primaryTab === tab.id} aria-controls={`panel-${tab.id}`} onClick={() => setPrimaryTab(tab.id)}
            className={`py-2 px-3 text-xs font-medium border-b-2 transition-colors whitespace-nowrap ${primaryTab === tab.id ? 'border-primary text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground/80'}`}>
            {tab.label}
          </button>
        ))}
      </div>

      {/* Tab content */}
      <div role="tabpanel" id={`panel-${primaryTab}`} className="flex-1 overflow-auto">
        {primaryTab === 'overview' && (
          <AgentOverviewTab agent={agent} budget={data.budget} />
        )}
        {primaryTab === 'activity' && (
          <AgentActivityTab
            agentName={agent.name}
            logs={data.logs}
            refreshingLogs={data.refreshingLogs}
            refreshLogs={data.refreshLogs}
            handleSendDM={data.handleSendDM}
          />
        )}
        {primaryTab === 'operations' && (
          <AgentOperationsTab
            agentName={agent.name}
            channels={data.channels}
            knowledge={data.knowledge}
            meeseeksList={data.meeseeksList}
            economics={data.economics}
            refreshMeeseeks={data.refreshMeeseeks}
            handleKillMeeseeks={data.handleKillMeeseeks}
            handleKillAllMeeseeks={data.handleKillAllMeeseeks}
            handleClearCache={data.handleClearCache}
            subTab={opsSubTab}
            onSubTabChange={setOpsSubTab}
          />
        )}
        {primaryTab === 'system' && (
          <AgentSystemTab
            agent={agent}
            agentConfig={data.agentConfig}
            capabilities={data.capabilities}
            policy={data.policy}
            capLoading={data.capLoading}
            logs={data.logs}
            refreshingLogs={data.refreshingLogs}
            refreshLogs={data.refreshLogs}
            handleGrant={data.handleGrant}
            handleRevoke={data.handleRevoke}
            handleSaveConfig={data.handleSaveConfig}
            subTab={sysSubTab}
            onSubTabChange={setSysSubTab}
          />
        )}
      </div>
    </div>
  );
}
