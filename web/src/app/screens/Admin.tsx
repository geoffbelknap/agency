import { useState, useEffect, useCallback, lazy, Suspense } from 'react';
import { Link, useParams, useNavigate } from 'react-router';
import { toast } from 'sonner';
import { api } from '../lib/api';
import { adminFeatureFlags } from '../lib/features';
import { Agent } from '../types';
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
const AdminProviders = lazy(() => import('./AdminProviders').then(m => ({ default: m.AdminProviders })));
const Events = lazy(() => import('./Events').then(m => ({ default: m.Events })));
const Webhooks = lazy(() => import('./Webhooks').then(m => ({ default: m.Webhooks })));
const Notifications = lazy(() => import('./Notifications').then(m => ({ default: m.Notifications })));
const AdminAudit = lazy(() => import('./AdminAudit').then(m => ({ default: m.AdminAudit })));
const AdminDoctor = lazy(() => import('./AdminDoctor').then(m => ({ default: m.AdminDoctor })));
const AdminEgress = lazy(() => import('./AdminEgress').then(m => ({ default: m.AdminEgress })));

const LAZY_FALLBACK = <div className="text-sm text-muted-foreground text-center py-8">Loading...</div>;

const TAB_GROUPS = [
  {
    label: 'Operations',
    color: 'var(--teal)',
    description: 'Infrastructure and shared platform services.',
    tabs: [
      { value: 'infrastructure', label: 'Infrastructure', description: 'Provision and rebuild the local platform.' },
      { value: 'hub', label: 'Packages', description: 'Install local packages and manage governed instances.', enabled: adminFeatureFlags.hub, experimental: true },
      { value: 'intake', label: 'Intake', description: 'Manage inbound channels and collection paths.', enabled: adminFeatureFlags.intake, experimental: true },
      { value: 'knowledge', label: 'Knowledge', description: 'Review graph curation, quarantine, topology, and ontology changes.', enabled: adminFeatureFlags.graphAdmin },
    ],
  },
  {
    label: 'Governance',
    color: 'var(--amber)',
    description: 'Capabilities, presets, policy, and agent operating boundaries.',
    tabs: [
      { value: 'capabilities', label: 'Capabilities', description: 'Review scoped capabilities and assignment rules.' },
      { value: 'providers', label: 'Providers', description: 'Configure model providers, credentials, and routing visibility.' },
      { value: 'presets', label: 'Presets', description: 'Manage reusable agent role presets for core operator workflows.' },
      { value: 'trust', label: 'Trust', description: 'Adjust agent trust tiers and restrictions.', enabled: adminFeatureFlags.trust, experimental: true },
      { value: 'egress', label: 'Egress', description: 'Define allowed outbound network destinations.' },
      { value: 'policy', label: 'Policy', description: 'Inspect and validate per-agent policy state.' },
      { value: 'doctor', label: 'Doctor', description: 'Run platform health checks and spot drift.' },
    ],
  },
  {
    label: 'Observability',
    color: '#C084FC',
    description: 'Usage, events, notifications, and operational history.',
    tabs: [
      { value: 'usage', label: 'Usage', description: 'Track spend, token flow, and runtime volume.' },
      { value: 'events', label: 'Events', description: 'Inspect recent system and agent event streams.', enabled: adminFeatureFlags.events, experimental: true },
      { value: 'webhooks', label: 'Webhooks', description: 'Manage outbound event delivery endpoints.', enabled: adminFeatureFlags.webhooks, experimental: true },
      { value: 'notifications', label: 'Notifications', description: 'Configure alerting and delivery preferences.', enabled: adminFeatureFlags.notifications, experimental: true },
      { value: 'audit', label: 'Audit', description: 'Review recorded agent actions and policy decisions.' },
      { value: 'setup', label: 'Setup', description: 'Re-run guided environment setup.' },
    ],
  },
  {
    label: 'Critical',
    color: 'var(--red)',
    description: 'High-impact controls that affect the entire environment.',
    tabs: [
      { value: 'danger', label: 'Danger Zone', description: 'Destroy infrastructure and reset the environment.' },
    ],
  },
];

const FILTERED_TAB_GROUPS = TAB_GROUPS
  .map((group) => ({
    ...group,
    tabs: group.tabs.filter((tab) => tab.enabled !== false),
  }))
  .filter((group) => group.tabs.length > 0);

const VALID_TABS = new Set(FILTERED_TAB_GROUPS.flatMap((group) => group.tabs.map((tab) => tab.value)));

const TAB_INDEX = new Map(
  FILTERED_TAB_GROUPS.flatMap((group) =>
    group.tabs.map((tab) => [tab.value, { ...tab, group: group.label, groupDescription: group.description }] as const),
  ),
);

function AdminSetupTab() {
  return (
    <section style={{ display: 'grid', gap: 16, maxWidth: 920 }}>
      <div
        style={{
          border: '0.5px solid var(--ink-hairline)',
          borderRadius: 14,
          background: 'var(--warm-2)',
          overflow: 'hidden',
        }}
      >
        <div style={{ padding: '24px 28px', borderBottom: '0.5px solid var(--ink-hairline)' }}>
          <div className="eyebrow" style={{ marginBottom: 14 }}>Guided Setup</div>
          <h3 className="display" style={{ margin: 0, fontSize: 32, fontWeight: 300, lineHeight: 1.1, letterSpacing: '-0.02em', color: 'var(--ink)' }}>
            Re-run setup wizard
          </h3>
          <p style={{ margin: '10px 0 0', color: 'var(--ink-mid)', fontSize: 14, lineHeight: 1.55, maxWidth: 620 }}>
            Open the full setup flow to re-check platform readiness, verify providers, create or reuse an agent, and confirm the first direct-message chat.
          </p>
        </div>
        <div style={{ padding: '18px 28px', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
          <div className="mono" style={{ color: 'var(--ink-faint)', fontSize: 11 }}>
            Platform {'->'} Providers {'->'} Agent {'->'} Chat
          </div>
          <Link
            to="/setup"
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              minHeight: 38,
              padding: '0 18px',
              borderRadius: 999,
              background: 'var(--ink)',
              color: 'var(--warm)',
              textDecoration: 'none',
              fontSize: 13,
              fontWeight: 500,
            }}
          >
            Re-run setup wizard
          </Link>
        </div>
      </div>
    </section>
  );
}

export function Admin() {
  const { tab: urlTab } = useParams<{ tab: string }>();
  const navigate = useNavigate();
  const activeTab = urlTab && VALID_TABS.has(urlTab) ? urlTab : 'infrastructure';
  const activeSection = TAB_INDEX.get(activeTab) ?? TAB_INDEX.get('infrastructure')!;
  const activeGroup = FILTERED_TAB_GROUPS.find((group) => group.label === activeSection.group) ?? FILTERED_TAB_GROUPS[0];

  const handleTabChange = useCallback((value: string) => {
    navigate(`/admin/${value}`, { replace: true });
  }, [navigate]);

  const handleGroupChange = useCallback((groupLabel: string) => {
    const targetGroup = FILTERED_TAB_GROUPS.find((group) => group.label === groupLabel);
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

  // Auto-select the first agent so policy opens with live data instead of an empty picker.
  useEffect(() => {
    if (agents.length > 0) {
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
    <div className="admin-page" style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0, background: 'var(--warm)' }}>
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
        <nav className="admin-nav" style={{ height: 58, minHeight: 58, display: 'flex', alignItems: 'center', flexWrap: 'nowrap', overflowX: 'auto', overflowY: 'hidden', columnGap: 12, padding: '8px 16px', borderBottom: '0.5px solid var(--ink-hairline)', background: 'var(--warm)' }}>
          <div className="eyebrow" style={{ fontSize: 9 }}>Admin</div>
          <div style={{ display: 'inline-flex', alignItems: 'center', gap: 2, padding: 2, border: '0.5px solid var(--ink-hairline)', borderRadius: 999, background: 'var(--warm-2)', flexShrink: 0 }}>
            {FILTERED_TAB_GROUPS.map((group) => {
              const selected = group.label === activeGroup.label;
              return (
                <button
                  key={group.label}
                  type="button"
                  onClick={() => handleGroupChange(group.label)}
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 6,
                    border: 0,
                    borderRadius: 999,
                    background: selected ? 'var(--ink)' : 'transparent',
                    color: selected ? 'var(--warm)' : 'var(--ink-mid)',
                    padding: '5px 10px',
                    cursor: 'pointer',
                    whiteSpace: 'nowrap',
                  }}
                >
                  <span style={{ width: 5, height: 5, borderRadius: 1, background: selected ? 'var(--warm)' : group.color }} />
                  <span className="mono" style={{ fontSize: 9, letterSpacing: '0.12em', textTransform: 'uppercase', fontWeight: 400 }}>{group.label}</span>
                </button>
              );
            })}
          </div>
          <span style={{ width: 1, height: 20, background: 'var(--ink-hairline)', flexShrink: 0 }} />
          <div style={{ display: 'flex', alignItems: 'center', gap: 2, minWidth: 0 }}>
            {activeGroup.tabs.map((tab) => {
              const selected = tab.value === activeTab;
              return (
                <button
                  key={tab.value}
                  type="button"
                  role="tab"
                  aria-selected={selected}
                  onClick={() => handleTabChange(tab.value)}
                  style={{
                    padding: '5px 11px',
                    border: 0,
                    borderRadius: 999,
                    background: selected ? 'var(--ink)' : 'transparent',
                    color: selected ? 'var(--warm)' : 'var(--ink-mid)',
                    fontFamily: 'var(--sans)',
                    fontSize: 12,
                    lineHeight: '18px',
                    cursor: 'pointer',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {tab.label}
                </button>
              );
            })}
          </div>
        </nav>

        <div className="admin-pane" style={{ flex: 1, overflowY: 'auto', padding: '24px 32px', minWidth: 0, position: 'relative' }}>
          <Suspense fallback={LAZY_FALLBACK}>
            {activeTab === 'infrastructure' && <Infrastructure />}
            {activeTab === 'hub' && <Hub />}
            {activeTab === 'intake' && <Intake />}
            {activeTab === 'knowledge' && <Knowledge />}
            {activeTab === 'capabilities' && <Capabilities />}
            {activeTab === 'providers' && <AdminProviders />}
            {activeTab === 'presets' && <Presets />}
            {activeTab === 'events' && <Events />}
            {activeTab === 'webhooks' && <Webhooks />}
            {activeTab === 'notifications' && <Notifications />}
            {activeTab === 'usage' && <Usage />}
          </Suspense>

          {activeTab === 'setup' && <AdminSetupTab />}

          {activeTab === 'trust' && (
            <TrustTab
              agents={agents}
              agentsLoading={agentsLoading}
              trustError={trustError}
              onTrust={handleTrust}
            />
          )}

          {activeTab === 'policy' && (
            <PolicyTab
              agents={agents}
              agentsLoading={agentsLoading}
              policyAgent={policyAgent}
              policyData={policyData}
              policyLoading={policyLoading}
              policyError={policyError}
              validating={validating}
              onPolicyAgentChange={handlePolicyAgentChange}
              onValidate={handleValidatePolicy}
            />
          )}

          {activeTab === 'doctor' && <AdminDoctor />}
          {activeTab === 'egress' && <AdminEgress />}
          {activeTab === 'audit' && <AdminAudit />}
          {activeTab === 'danger' && (
            <DangerZoneTab destroying={destroying} onDestroy={handleDestroyAll} />
          )}
        </div>
      </div>
    </div>
  );
}
