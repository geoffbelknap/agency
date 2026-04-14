import { useState, useEffect, useCallback, lazy, Suspense } from 'react';
import { useParams, useNavigate } from 'react-router';
import { toast } from 'sonner';
import { api } from '../lib/api';
import { Agent } from '../types';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs';
import { useIsMobile } from '../components/ui/use-mobile';
import { TrustTab } from './admin/TrustTab';
import { PolicyTab } from './admin/PolicyTab';
import { DangerZoneTab } from './admin/DangerZoneTab';

const Infrastructure = lazy(() => import('./Infrastructure').then(m => ({ default: m.Infrastructure })));
const Hub = lazy(() => import('./Hub').then(m => ({ default: m.Hub })));
const Intake = lazy(() => import('./Intake').then(m => ({ default: m.Intake })));
const Knowledge = lazy(() => import('./Knowledge').then(m => ({ default: m.Knowledge })));
const Capabilities = lazy(() => import('./Capabilities').then(m => ({ default: m.Capabilities })));
const Usage = lazy(() => import('./Usage').then(m => ({ default: m.Usage })));
const Presets = lazy(() => import('./Presets').then(m => ({ default: m.Presets })));
const Events = lazy(() => import('./Events').then(m => ({ default: m.Events })));
const Webhooks = lazy(() => import('./Webhooks').then(m => ({ default: m.Webhooks })));
const Notifications = lazy(() => import('./Notifications').then(m => ({ default: m.Notifications })));
const AdminAudit = lazy(() => import('./AdminAudit').then(m => ({ default: m.AdminAudit })));
const AdminDoctor = lazy(() => import('./AdminDoctor').then(m => ({ default: m.AdminDoctor })));
const AdminEgress = lazy(() => import('./AdminEgress').then(m => ({ default: m.AdminEgress })));

const LAZY_FALLBACK = <div className="text-sm text-muted-foreground text-center py-8">Loading...</div>;

const VALID_TABS = new Set([
  'infrastructure', 'hub', 'intake', 'knowledge', 'capabilities', 'presets',
  'trust', 'egress', 'policy', 'doctor', 'usage', 'events', 'webhooks',
  'notifications', 'audit', 'setup', 'danger',
]);

const TAB_GROUPS = [
  {
    label: 'Operations',
    description: 'Infrastructure, intake, and shared platform services.',
    tabs: [
      { value: 'infrastructure', label: 'Infrastructure', description: 'Provision and rebuild the local platform.' },
      { value: 'hub', label: 'Packages', description: 'Install local packages and manage governed instances.' },
      { value: 'intake', label: 'Intake', description: 'Manage inbound channels and collection paths.' },
      { value: 'knowledge', label: 'Knowledge', description: 'Inspect stored knowledge bases and retrieval inputs.' },
    ],
  },
  {
    label: 'Governance',
    description: 'Policies, trust, and agent operating boundaries.',
    tabs: [
      { value: 'capabilities', label: 'Capabilities', description: 'Review scoped capabilities and assignment rules.' },
      { value: 'presets', label: 'Presets', description: 'Manage reusable agent configuration presets.' },
      { value: 'trust', label: 'Trust', description: 'Adjust agent trust tiers and restrictions.' },
      { value: 'egress', label: 'Egress', description: 'Define allowed outbound network destinations.' },
      { value: 'policy', label: 'Policy', description: 'Inspect and validate per-agent policy state.' },
      { value: 'doctor', label: 'Doctor', description: 'Run platform health checks and spot drift.' },
    ],
  },
  {
    label: 'Observability',
    description: 'Usage, events, notifications, and operational history.',
    tabs: [
      { value: 'usage', label: 'Usage', description: 'Track spend, token flow, and runtime volume.' },
      { value: 'events', label: 'Events', description: 'Inspect recent system and agent event streams.' },
      { value: 'webhooks', label: 'Webhooks', description: 'Manage outbound event delivery endpoints.' },
      { value: 'notifications', label: 'Notifications', description: 'Configure alerting and delivery preferences.' },
      { value: 'audit', label: 'Audit', description: 'Review recorded agent actions and policy decisions.' },
      { value: 'setup', label: 'Setup Wizard', description: 'Re-run guided environment setup.' },
    ],
  },
  {
    label: 'Critical',
    description: 'High-impact controls that affect the entire environment.',
    tabs: [
      { value: 'danger', label: 'Danger Zone', description: 'Destroy infrastructure and reset the environment.' },
    ],
  },
];

const TAB_INDEX = new Map(
  TAB_GROUPS.flatMap((group) =>
    group.tabs.map((tab) => [tab.value, { ...tab, group: group.label, groupDescription: group.description }] as const),
  ),
);

export function Admin() {
  const { tab: urlTab } = useParams<{ tab: string }>();
  const navigate = useNavigate();
  const isMobile = useIsMobile();
  const activeTab = urlTab && VALID_TABS.has(urlTab) ? urlTab : 'infrastructure';
  const activeSection = TAB_INDEX.get(activeTab) ?? TAB_INDEX.get('infrastructure')!;
  const activeGroup = TAB_GROUPS.find((group) => group.label === activeSection.group) ?? TAB_GROUPS[0];

  const handleTabChange = useCallback((value: string) => {
    navigate(`/admin/${value}`, { replace: true });
  }, [navigate]);

  const handleGroupChange = useCallback((groupLabel: string) => {
    const targetGroup = TAB_GROUPS.find((group) => group.label === groupLabel);
    if (!targetGroup) return;
    const fallbackTab = targetGroup.tabs[0]?.value;
    if (fallbackTab && fallbackTab !== activeTab) {
      handleTabChange(fallbackTab);
    }
  }, [activeTab, handleTabChange]);

  // Agents (trust + policy selector)
  const [agents, setAgents] = useState<Agent[]>([]);
  const [agentsLoading, setAgentsLoading] = useState(true);

  // Trust
  const [trustError, setTrustError] = useState<string | null>(null);

  // Policy
  const [policyAgent, setPolicyAgent] = useState<string>('');
  const [policyData, setPolicyData] = useState<any>(null);
  const [policyLoading, setPolicyLoading] = useState(false);
  const [policyError, setPolicyError] = useState<string | null>(null);
  const [validating, setValidating] = useState(false);

  // Danger Zone
  const [destroying, setDestroying] = useState(false);

  const loadAgents = useCallback(async () => {
    try {
      setAgentsLoading(true);
      const raw = await api.agents.list();
      const mapped: Agent[] = (raw ?? []).filter((a: any) => a.name).map((a: any) => ({
        id: a.name,
        name: a.name,
        status: a.status || 'stopped',
        preset: a.preset || '',
        mode: a.mode || 'assisted',
        type: a.type || '',
        team: a.team || '',
        enforcerState: a.enforcer || '',
        trustLevel: a.trust_level ?? 3,
        restrictions: a.restrictions || [],
        mission: a.mission,
        missionStatus: a.mission_status,
      }));
      setAgents(mapped);
    } catch {
      setAgents([]);
    } finally {
      setAgentsLoading(false);
    }
  }, []);

  const handleTrust = async (agentName: string, action: 'elevate' | 'demote') => {
    try {
      setTrustError(null);
      await api.admin.trust(action, agentName);
      await loadAgents();
    } catch (e: any) {
      setTrustError(e.message || `Failed to ${action} agent`);
    }
  };

  const loadPolicy = async (agentName: string) => {
    if (!agentName) return;
    try {
      setPolicyLoading(true);
      setPolicyError(null);
      const data = await api.policy.show(agentName);
      setPolicyData(data);
    } catch (e: any) {
      setPolicyError(e.message || 'Failed to load policy');
      setPolicyData(null);
    } finally {
      setPolicyLoading(false);
    }
  };

  const handlePolicyAgentChange = (agentName: string) => {
    setPolicyAgent(agentName);
    loadPolicy(agentName);
  };

  const handleValidatePolicy = async () => {
    if (!policyAgent) return;
    try {
      setValidating(true);
      const result = await api.policy.validate(policyAgent);
      const valid = result?.valid !== false;
      if (valid) {
        toast.success('Policy validation passed');
      } else {
        toast.error('Policy validation failed');
      }
    } catch (e: any) {
      toast.error(e.message || 'Policy validation failed');
    } finally {
      setValidating(false);
    }
  };

  const handleDestroyAll = async () => {
    try {
      setDestroying(true);
      await api.admin.destroy();
      toast.success('All agents and infrastructure destroyed');
      await loadAgents();
    } catch (e: any) {
      toast.error(e.message || 'Destroy failed');
    } finally {
      setDestroying(false);
    }
  };

  useEffect(() => {
    loadAgents();
  }, [loadAgents]);

  // Auto-select when there's only one agent
  useEffect(() => {
    if (agents.length === 1) {
      const name = agents[0].name;
      if (!policyAgent) setPolicyAgent(name);
    }
  }, [agents, policyAgent]);

  useEffect(() => {
    if (policyAgent && !policyData && !policyLoading) {
      loadPolicy(policyAgent);
    }
  }, [policyAgent]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="flex h-full flex-col bg-background">
      <div className="border-b border-border bg-surface-alt/55 px-4 py-4 md:px-8">
        <div className="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
          <div className="space-y-1.5">
            <p className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
              Administrative surfaces
            </p>
            <div className="flex flex-col gap-2 xl:flex-row xl:items-end xl:gap-4">
              <h1 className="text-2xl text-foreground">Admin</h1>
              <p className="max-w-3xl text-sm text-muted-foreground">
                Security, governance, runtime controls, and operational evidence for the local Agency environment.
              </p>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <span className="text-muted-foreground">Current section</span>
            <span className="rounded-full border border-border bg-card px-3 py-1.5 font-medium text-foreground">
              {activeSection.label}
            </span>
          </div>
        </div>
      </div>

      <div className="flex-1 overflow-auto px-4 py-4 md:px-8 md:py-5">
        <Tabs value={activeTab} onValueChange={handleTabChange} className="space-y-4">
          <section className="rounded-2xl border border-border bg-card px-4 py-4 md:px-5">
            <div className="space-y-4">
              <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
                <div className="max-w-2xl">
                  <div className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                    {activeSection.group}
                  </div>
                  <div className="mt-1 flex flex-col gap-1 xl:flex-row xl:items-baseline xl:gap-3">
                    <h2 className="text-xl text-foreground">{activeSection.label}</h2>
                    <p className="text-sm text-muted-foreground">
                      {activeSection.description}
                    </p>
                  </div>
                </div>
                <div className="text-sm text-muted-foreground xl:max-w-sm xl:text-right">
                  {activeSection.groupDescription}
                </div>
              </div>

              {!isMobile && (
                <div className="space-y-3 border-t border-border/80 pt-3">
                  <div className="flex flex-wrap gap-2">
                    {TAB_GROUPS.map((group) => {
                      const isCurrentGroup = group.label === activeGroup.label;
                      return (
                        <button
                          key={group.label}
                          type="button"
                          onClick={() => handleGroupChange(group.label)}
                          className={`rounded-full border px-3 py-1.5 text-sm transition-colors ${
                            isCurrentGroup
                              ? 'border-primary/30 bg-primary/10 text-primary'
                              : 'border-border bg-background text-muted-foreground hover:border-border-mid hover:text-foreground'
                          }`}
                        >
                          {group.label}
                        </button>
                      );
                    })}
                  </div>

                  <TabsList className="h-auto w-full flex-wrap justify-start gap-2 bg-transparent p-0">
                    {activeGroup.tabs.map((tab) => (
                      <TabsTrigger
                        key={tab.value}
                        value={tab.value}
                        className={tab.value === 'danger' ? 'text-red-500 data-[state=active]:text-red-600 dark:text-red-300 dark:data-[state=active]:text-red-200' : ''}
                      >
                        {tab.label}
                      </TabsTrigger>
                    ))}
                  </TabsList>
                </div>
              )}
            </div>
          </section>

          {isMobile && (
            <div className="space-y-2">
              <label htmlFor="admin-section" className="text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">
                Section
              </label>
              <select
                id="admin-section"
                value={activeTab}
                onChange={(e) => handleTabChange(e.target.value)}
                className="w-full rounded-xl border border-border bg-card px-3 py-2.5 text-sm text-foreground"
              >
                {TAB_GROUPS.map((group) => (
                  <optgroup key={group.label} label={group.label}>
                    {group.tabs.map((tab) => (
                      <option key={tab.value} value={tab.value}>{tab.label}</option>
                    ))}
                  </optgroup>
                ))}
              </select>
              <p className="text-sm text-muted-foreground">
                {activeSection.group}: {activeSection.description}
              </p>
            </div>
          )}

          <TabsContent value="infrastructure"><Suspense fallback={LAZY_FALLBACK}><Infrastructure /></Suspense></TabsContent>
          <TabsContent value="hub"><Suspense fallback={LAZY_FALLBACK}><Hub /></Suspense></TabsContent>
          <TabsContent value="intake"><Suspense fallback={LAZY_FALLBACK}><Intake /></Suspense></TabsContent>
          <TabsContent value="knowledge"><Suspense fallback={LAZY_FALLBACK}><Knowledge /></Suspense></TabsContent>
          <TabsContent value="capabilities"><Suspense fallback={LAZY_FALLBACK}><Capabilities /></Suspense></TabsContent>
          <TabsContent value="presets"><Suspense fallback={LAZY_FALLBACK}><Presets /></Suspense></TabsContent>
          <TabsContent value="usage"><Suspense fallback={LAZY_FALLBACK}><Usage /></Suspense></TabsContent>
          <TabsContent value="events"><Suspense fallback={LAZY_FALLBACK}><Events /></Suspense></TabsContent>
          <TabsContent value="webhooks"><Suspense fallback={LAZY_FALLBACK}><Webhooks /></Suspense></TabsContent>
          <TabsContent value="notifications"><Suspense fallback={LAZY_FALLBACK}><Notifications /></Suspense></TabsContent>
          <TabsContent value="doctor"><Suspense fallback={LAZY_FALLBACK}><AdminDoctor /></Suspense></TabsContent>
          <TabsContent value="audit" className="space-y-4 mt-0"><Suspense fallback={LAZY_FALLBACK}><AdminAudit /></Suspense></TabsContent>
          <TabsContent value="egress"><Suspense fallback={LAZY_FALLBACK}><AdminEgress /></Suspense></TabsContent>

          <TabsContent value="setup">
            <div className="space-y-4 rounded-3xl border border-border bg-card px-6 py-10 text-center">
              <h3 className="text-lg font-medium text-foreground">Re-run Setup Wizard</h3>
              <p className="text-sm text-muted-foreground max-w-md mx-auto">
                Walk through platform configuration again — update providers, capabilities, and agent settings.
              </p>
              <button
                onClick={() => navigate('/setup')}
                className="rounded-xl bg-foreground px-4 py-2 text-sm font-medium text-background transition-opacity hover:opacity-90"
              >
                Open Setup Wizard
              </button>
            </div>
          </TabsContent>

          <TabsContent value="trust">
            <TrustTab
              agents={agents}
              agentsLoading={agentsLoading}
              trustError={trustError}
              onTrust={handleTrust}
            />
          </TabsContent>

          <TabsContent value="policy" className="space-y-4">
            <PolicyTab
              agents={agents}
              policyAgent={policyAgent}
              onPolicyAgentChange={handlePolicyAgentChange}
              policyData={policyData}
              policyLoading={policyLoading}
              policyError={policyError}
              onValidate={handleValidatePolicy}
              validating={validating}
            />
          </TabsContent>

          <TabsContent value="danger" className="space-y-4">
            <DangerZoneTab onDestroy={handleDestroyAll} destroying={destroying} />
          </TabsContent>
        </Tabs>
      </div>
    </div>
  );
}
