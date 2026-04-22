import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router';
import { toast } from 'sonner';
import { api, type RawAuditEntry, type RawChannel, type RawCapability, type RawPolicyValidation, type RawMeeseeks, type RawBudgetResponse, type RawEconomicsResponse, type RawAgentResult } from '../../lib/api';

export interface AgentDetailData {
  // Data
  logs: RawAuditEntry[];
  channels: RawChannel[];
  knowledge: Record<string, any>[];
  capabilities: RawCapability[];
  policy: RawPolicyValidation | null;
  meeseeksList: RawMeeseeks[];
  agentConfig: Record<string, any> | null;
  budget: RawBudgetResponse | null;
  economics: RawEconomicsResponse | null;
  results: RawAgentResult[];

  // Loading states
  capLoading: string | null;
  refreshingLogs: boolean;
  refreshingResults: boolean;

  // Actions
  refreshLogs: (name: string) => Promise<void>;
  refreshResults: (name: string) => Promise<void>;
  handleOpenDM: (agentName: string) => Promise<void>;
  handleSendDM: (agentName: string, dmText: string) => Promise<boolean>;
  handleGrant: (agentName: string, capability: string) => Promise<void>;
  handleRevoke: (agentName: string, capability: string) => Promise<void>;
  handleSaveConfig: (agentName: string, identity: string) => Promise<Record<string, any> | null>;
  refreshMeeseeks: (agentName: string) => Promise<void>;
  handleKillMeeseeks: (agentName: string, meeseeksId: string) => Promise<void>;
  handleKillAllMeeseeks: (agentName: string) => Promise<void>;
  handleClearCache: (agentName: string) => Promise<void>;
}

export function useAgentDetailData(
  agentName: string,
  initialCapabilities: RawCapability[],
  effectiveDataTab: string,
  onRefreshAgents: () => void,
): AgentDetailData {
  const navigate = useNavigate();

  // Data state
  const [logs, setLogs] = useState<RawAuditEntry[]>([]);
  const [channels, setChannels] = useState<RawChannel[]>([]);
  const [knowledge, setKnowledge] = useState<Record<string, any>[]>([]);
  const [capabilities, setCapabilities] = useState<RawCapability[]>(initialCapabilities);
  const [policy, setPolicy] = useState<RawPolicyValidation | null>(null);
  const [meeseeksList, setMeeseeksList] = useState<RawMeeseeks[]>([]);
  const [agentConfig, setAgentConfig] = useState<Record<string, any> | null>(null);
  const [budget, setBudget] = useState<RawBudgetResponse | null>(null);
  const [economics, setEconomics] = useState<RawEconomicsResponse | null>(null);
  const [results, setResults] = useState<RawAgentResult[]>([]);

  // Loading states
  const [capLoading, setCapLoading] = useState<string | null>(null);
  const [refreshingLogs, setRefreshingLogs] = useState(false);
  const [refreshingResults, setRefreshingResults] = useState(false);

  // Sync capabilities from parent
  useEffect(() => { setCapabilities(initialCapabilities); }, [initialCapabilities]);

  const refreshLogs = useCallback(async (name: string) => {
    setRefreshingLogs(true);
    try {
      const data = await api.agents.logs(name);
      setLogs(data ?? []);
    } catch {
      setLogs([]);
    } finally {
      setRefreshingLogs(false);
    }
  }, []);

  const refreshResults = useCallback(async (name: string) => {
    setRefreshingResults(true);
    try {
      const data = await api.agents.results(name);
      setResults(data ?? []);
    } catch {
      setResults([]);
    } finally {
      setRefreshingResults(false);
    }
  }, []);

  // Fetch logs only when a visible tab needs them. This runs after first paint,
  // so the overview can use recent audit events without blocking the shell.
  useEffect(() => {
    if (effectiveDataTab === 'overview' || effectiveDataTab === 'activity' || effectiveDataTab === 'logs') {
      refreshLogs(agentName);
    }
  }, [agentName, effectiveDataTab, refreshLogs]);

  // Fetch budget
  useEffect(() => {
    api.agents.budget(agentName).then(setBudget).catch(() => setBudget(null));
  }, [agentName]);

  // Lazy-load tab data
  useEffect(() => {
    const name = agentName;
    if (effectiveDataTab === 'channels') {
      api.agents.channels(name).then(setChannels).catch(() => setChannels([]));
    } else if (effectiveDataTab === 'knowledge') {
      api.agents.knowledge(name).then((data: any) => {
        setKnowledge(Array.isArray(data) ? data : data?.nodes ?? []);
      }).catch(() => setKnowledge([]));
    } else if (effectiveDataTab === 'meeseeks') {
      api.meeseeks.list(name).then(data => setMeeseeksList(data ?? [])).catch(() => setMeeseeksList([]));
    } else if (effectiveDataTab === 'economics') {
      api.agents.economics(name).then(setEconomics).catch(() => setEconomics(null));
    } else if (effectiveDataTab === 'results') {
      refreshResults(name);
    } else if (effectiveDataTab === 'config') {
      Promise.all([
        api.capabilities.list().catch(() => []),
        api.policy.show(name).catch(() => null),
        api.agentConfig.get(name).catch(() => null),
      ]).then(([caps, pol, cfg]) => {
        setCapabilities(caps);
        setPolicy(pol);
        setAgentConfig(cfg);
      });
    }
  }, [agentName, effectiveDataTab]);

  const handleOpenDM = useCallback(async (name: string) => {
    try {
      const result = await api.agents.ensureDM(name);
      navigate(`/channels/${encodeURIComponent(result.channel || `dm-${name}`)}`);
    } catch (e: any) {
      toast.error(e.message || 'Failed to open DM');
    }
  }, [navigate]);

  const handleSendDM = useCallback(async (name: string, dmText: string): Promise<boolean> => {
    if (!dmText.trim()) return false;
    const channelName = `dm-${name}`;
    try {
      await api.agents.ensureDM(name);
      await api.channels.send(channelName, dmText.trim());
      toast.success('Message sent to DM');
      navigate(`/channels/${channelName}`);
      return true;
    } catch (e: any) {
      try {
        const channels = await api.agents.channels(name);
        const hasDM = channels.some((channel) => channel.name === channelName);
        if (!hasDM) {
          toast.error(`DM for "${name}" is not ready yet. Start the agent and wait for its conversation to appear, then try again.`);
          return false;
        }
      } catch {
        // Fall through to the original API error when channel discovery also fails.
      }
      toast.error(e.message || 'Failed to send message');
      return false;
    }
  }, [navigate]);

  const handleGrant = useCallback(async (name: string, capability: string) => {
    setCapLoading(capability);
    try {
      await api.agents.grant(name, capability);
      toast.success(`Granted ${capability}`);
      await new Promise((r) => setTimeout(r, 500));
      onRefreshAgents();
      if (effectiveDataTab === 'config') {
        api.capabilities.list().then(setCapabilities).catch(() => {});
      }
    } catch (e: any) {
      toast.error(e.message || 'Grant failed');
    } finally {
      setCapLoading(null);
    }
  }, [effectiveDataTab, onRefreshAgents]);

  const handleRevoke = useCallback(async (name: string, capability: string) => {
    setCapLoading(capability);
    try {
      await api.agents.revoke(name, capability);
      toast.success(`Revoked ${capability}`);
      await new Promise((r) => setTimeout(r, 500));
      onRefreshAgents();
      if (effectiveDataTab === 'config') {
        api.capabilities.list().then(setCapabilities).catch(() => {});
      }
    } catch (e: any) {
      toast.error(e.message || 'Revoke failed');
    } finally {
      setCapLoading(null);
    }
  }, [effectiveDataTab, onRefreshAgents]);

  const handleSaveConfig = useCallback(async (name: string, identity: string): Promise<Record<string, any> | null> => {
    try {
      const updated = await api.agentConfig.update(name, { identity });
      setAgentConfig(updated);
      toast.success('Identity updated');
      return updated;
    } catch (e: any) {
      toast.error(e.message || 'Failed to save');
      return null;
    }
  }, []);

  const refreshMeeseeks = useCallback(async (name: string) => {
    try {
      const data = await api.meeseeks.list(name);
      setMeeseeksList(data ?? []);
    } catch {
      setMeeseeksList([]);
    }
  }, []);

  const handleKillMeeseeks = useCallback(async (name: string, meeseeksId: string) => {
    try {
      await api.meeseeks.kill(meeseeksId);
      toast.success(`Killed ${meeseeksId}`);
      refreshMeeseeks(name);
    } catch (e: any) {
      toast.error(e.message || 'Kill failed');
    }
  }, [refreshMeeseeks]);

  const handleKillAllMeeseeks = useCallback(async (name: string) => {
    try {
      const result = await api.meeseeks.killByParent(name);
      const count = result.killed?.length ?? 0;
      toast.success(`Killed ${count} meeseeks for ${name}`);
      await refreshMeeseeks(name);
    } catch (e: any) {
      toast.error(e.message || 'Kill all failed');
    }
  }, [refreshMeeseeks]);

  const handleClearCache = useCallback(async (name: string) => {
    try {
      const result = await api.agents.clearCache(name);
      const deleted = typeof result.deleted === 'number' ? ` (${result.deleted} deleted)` : '';
      toast.success(`Cache cleared for ${name}${deleted}`);
      api.agents.economics(name).then(setEconomics).catch(() => {});
    } catch (e: any) {
      toast.error(e.message || 'Cache clear failed');
    }
  }, []);

  return {
    logs,
    channels,
    knowledge,
    capabilities,
    policy,
    meeseeksList,
    agentConfig,
    budget,
    economics,
    results,
    capLoading,
    refreshingLogs,
    refreshingResults,
    refreshLogs,
    refreshResults,
    handleOpenDM,
    handleSendDM,
    handleGrant,
    handleRevoke,
    handleSaveConfig,
    refreshMeeseeks,
    handleKillMeeseeks,
    handleKillAllMeeseeks,
    handleClearCache,
  };
}
