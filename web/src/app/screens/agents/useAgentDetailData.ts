import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router';
import { toast } from 'sonner';
import { api, type RawAuditEntry, type RawChannel, type RawCapability, type RawPolicyValidation, type RawMeeseeks, type RawBudgetResponse } from '../../lib/api';

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

  // Loading states
  capLoading: string | null;
  refreshingLogs: boolean;

  // Actions
  refreshLogs: (name: string) => Promise<void>;
  handleSendDM: (agentName: string, dmText: string) => Promise<boolean>;
  handleGrant: (agentName: string, capability: string) => Promise<void>;
  handleRevoke: (agentName: string, capability: string) => Promise<void>;
  handleSaveConfig: (agentName: string, identity: string) => Promise<Record<string, any> | null>;
  handleKillMeeseeks: (agentName: string, meeseeksId: string) => Promise<void>;
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

  // Loading states
  const [capLoading, setCapLoading] = useState<string | null>(null);
  const [refreshingLogs, setRefreshingLogs] = useState(false);

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

  // Fetch logs on mount / agent change
  useEffect(() => {
    refreshLogs(agentName);
  }, [agentName, refreshLogs]);

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

  const handleSendDM = useCallback(async (name: string, dmText: string): Promise<boolean> => {
    if (!dmText.trim()) return false;
    try {
      await api.channels.send('dm-' + name, dmText.trim());
      toast.success('Message sent to DM');
      navigate(`/channels/dm-${name}`);
      return true;
    } catch (e: any) {
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

  const handleKillMeeseeks = useCallback(async (name: string, meeseeksId: string) => {
    try {
      await api.meeseeks.kill(meeseeksId);
      toast.success(`Killed ${meeseeksId}`);
      api.meeseeks.list(name).then(d => setMeeseeksList(d ?? [])).catch(() => {});
    } catch (e: any) {
      toast.error(e.message || 'Kill failed');
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
    capLoading,
    refreshingLogs,
    refreshLogs,
    handleSendDM,
    handleGrant,
    handleRevoke,
    handleSaveConfig,
    handleKillMeeseeks,
  };
}
