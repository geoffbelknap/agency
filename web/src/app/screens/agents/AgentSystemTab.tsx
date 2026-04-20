import { useState, useEffect, type ReactNode } from 'react';
import { Shield, Trash2 } from 'lucide-react';
import { api, type RawCapability, type RawPolicyValidation, type RawAuditEntry, type RawProviderToolCapability } from '../../lib/api';
import { Agent } from '../../types';
import { LogsSection } from './AgentActivityTab';

type SystemSubTab = 'config' | 'logs';

const SYSTEM_TABS: { id: SystemSubTab; label: string }[] = [
  { id: 'config', label: 'Config' },
  { id: 'logs', label: 'Logs' },
];

interface Props {
  agent: Agent;
  agentConfig: Record<string, any> | null;
  capabilities: RawCapability[];
  policy: RawPolicyValidation | null;
  capLoading: string | null;
  logs: RawAuditEntry[];
  refreshingLogs: boolean;
  refreshLogs: (name: string) => Promise<void>;
  handleGrant: (agentName: string, capability: string) => Promise<void>;
  handleRevoke: (agentName: string, capability: string) => Promise<void>;
  handleSaveConfig: (agentName: string, identity: string) => Promise<Record<string, any> | null>;
  onRequestDelete: (agentName: string) => void;
  subTab: SystemSubTab;
  onSubTabChange: (tab: SystemSubTab) => void;
}

const cardStyle = { background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 10, padding: 20 } as const;
const innerStyle = { background: 'var(--warm)', border: '0.5px solid var(--ink-hairline)', borderRadius: 8 } as const;

function SmallButton({ children, icon, onClick, disabled = false, primary = false, danger = false }: { children: ReactNode; icon?: ReactNode; onClick?: () => void; disabled?: boolean; primary?: boolean; danger?: boolean }) {
  return (
    <button type="button" disabled={disabled} onClick={onClick} style={{ display: 'inline-flex', alignItems: 'center', gap: 6, border: primary ? '0.5px solid var(--ink)' : '0.5px solid var(--ink-hairline-strong)', background: primary ? 'var(--ink)' : 'var(--warm)', color: danger ? 'var(--red)' : primary ? 'var(--warm)' : 'var(--ink)', fontFamily: 'var(--font-sans)', fontSize: 12, padding: '5px 10px', borderRadius: 999, cursor: disabled ? 'default' : 'pointer', opacity: disabled ? 0.5 : 1 }}>
      {icon}
      {children}
    </button>
  );
}

function Badge({ children, tone = 'neutral' }: { children: ReactNode; tone?: 'neutral' | 'active' | 'warn' | 'danger' }) {
  const colors = {
    neutral: { bg: 'var(--warm-3)', color: 'var(--ink-mid)' },
    active: { bg: 'var(--teal-tint)', color: 'var(--teal-dark)' },
    warn: { bg: 'var(--amber-tint)', color: '#8B5A00' },
    danger: { bg: 'var(--red-tint)', color: 'var(--red)' },
  }[tone];
  return <span className="font-mono" style={{ display: 'inline-flex', alignItems: 'center', padding: '2px 7px', borderRadius: 4, fontSize: 10, background: colors.bg, color: colors.color }}>{children}</span>;
}

function PanelHeader({ title, meta, action }: { title: string; meta?: string; action?: ReactNode }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12 }}>
      <div className="eyebrow">{title}</div>
      {meta && <span className="font-mono" style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--ink-faint)' }}>{meta}</span>}
      {action}
    </div>
  );
}

function ConfigContent({ agent, agentConfig, capabilities, policy, capLoading, handleGrant, handleRevoke, handleSaveConfig, onRequestDelete }: {
  agent: Agent;
  agentConfig: Record<string, any> | null;
  capabilities: RawCapability[];
  policy: RawPolicyValidation | null;
  capLoading: string | null;
  handleGrant: (agentName: string, capability: string) => Promise<void>;
  handleRevoke: (agentName: string, capability: string) => Promise<void>;
  handleSaveConfig: (agentName: string, identity: string) => Promise<Record<string, any> | null>;
  onRequestDelete: (agentName: string) => void;
}) {
  const [editingIdentity, setEditingIdentity] = useState(false);
  const [identityDraft, setIdentityDraft] = useState('');
  const [savingConfig, setSavingConfig] = useState(false);
  const [providerToolCatalog, setProviderToolCatalog] = useState<Record<string, RawProviderToolCapability>>({});
  const [providerToolCatalogError, setProviderToolCatalogError] = useState('');

  useEffect(() => {
    if (agentConfig?.identity) setIdentityDraft(agentConfig.identity);
  }, [agentConfig]);

  useEffect(() => {
    let cancelled = false;
    api.providers.tools()
      .then((inventory) => {
        if (cancelled) return;
        setProviderToolCatalog(inventory.capabilities || {});
        setProviderToolCatalogError('');
      })
      .catch((err) => {
        if (cancelled) return;
        setProviderToolCatalog({});
        setProviderToolCatalogError(err instanceof Error ? err.message : 'Provider tool catalog unavailable.');
      });
    return () => { cancelled = true; };
  }, []);

  const providerToolCapabilities: RawCapability[] = Object.entries(providerToolCatalog).map(([name, tool]) => ({
    name,
    kind: 'provider-tool',
    state: 'available',
    description: tool.description || tool.title,
  }));

  const visibleCapabilities = [
    ...capabilities,
    ...providerToolCapabilities.filter((providerCap) => !capabilities.some((cap) => cap.name === providerCap.name)),
  ];

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      {agentConfig && (
        <div style={cardStyle}>
          <PanelHeader
            title="Identity & personality"
            action={!editingIdentity ? (
              <SmallButton onClick={() => { setIdentityDraft(agentConfig.identity || ''); setEditingIdentity(true); }}>Edit</SmallButton>
            ) : (
              <>
                <SmallButton primary disabled={savingConfig} onClick={async () => { setSavingConfig(true); try { const updated = await handleSaveConfig(agent.name, identityDraft); if (updated) setEditingIdentity(false); } finally { setSavingConfig(false); } }}>{savingConfig ? 'Saving...' : 'Save'}</SmallButton>
                <SmallButton onClick={() => setEditingIdentity(false)}>Cancel</SmallButton>
              </>
            )}
          />
          {editingIdentity ? (
            <textarea value={identityDraft} onChange={(e) => setIdentityDraft(e.target.value)} placeholder="Agent identity markdown..." style={{ width: '100%', minHeight: 190, resize: 'vertical', border: '0.5px solid var(--ink-hairline)', borderRadius: 8, background: 'var(--warm)', color: 'var(--ink)', outline: 0, padding: 12, fontFamily: 'var(--font-mono)', fontSize: 12, lineHeight: 1.6 }} />
          ) : (
            <div className="scrollbar-none" style={{ ...innerStyle, maxHeight: 160, overflowY: 'auto', padding: 12, whiteSpace: 'pre-wrap', color: 'var(--ink-mid)', fontFamily: 'var(--font-mono)', fontSize: 12, lineHeight: 1.6 }}>
              {agentConfig.identity || 'No identity configured'}
            </div>
          )}
        </div>
      )}

      {agentConfig?.constraints && (
        <div style={cardStyle}>
          <PanelHeader title="Constraints" />
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            {agentConfig.constraints.hard_limits?.map((h: any, index: number) => (
              <div key={`hard-${index}`} style={{ ...innerStyle, padding: 12, color: 'var(--ink)', fontSize: 12 }}>
                <Badge tone="warn">hard limit</Badge>
                <span style={{ marginLeft: 8 }}>{h.rule}</span>
                {h.reason && <span style={{ color: 'var(--ink-mid)', marginLeft: 6 }}>- {h.reason}</span>}
              </div>
            ))}
            {(agentConfig.constraints.escalation?.always_escalate || []).map((item: string, index: number) => (
              <div key={`always-${index}`} style={{ ...innerStyle, padding: 12, color: 'var(--ink)', fontSize: 12 }}><Badge tone="danger">escalate</Badge><span style={{ marginLeft: 8 }}>{item}</span></div>
            ))}
            {(agentConfig.constraints.escalation?.flag_before_proceeding || []).map((item: string, index: number) => (
              <div key={`flag-${index}`} style={{ ...innerStyle, padding: 12, color: 'var(--ink)', fontSize: 12 }}><Badge>flag</Badge><span style={{ marginLeft: 8 }}>{item}</span></div>
            ))}
            {agentConfig.constraints.autonomy && (
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                <Badge>default: {agentConfig.constraints.autonomy.default_mode}</Badge>
                <Badge>max: {agentConfig.constraints.autonomy.autonomous_max_duration}</Badge>
              </div>
            )}
          </div>
        </div>
      )}

      <div style={cardStyle}>
        <PanelHeader title="Capabilities" meta={`${visibleCapabilities.length} available`} />
        <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 8, overflow: 'hidden', background: 'var(--warm)' }}>
          {visibleCapabilities.length === 0 ? (
            <div style={{ padding: 16, fontSize: 13, color: 'var(--ink-faint)' }}>No capabilities available.</div>
          ) : visibleCapabilities.map((cap: any, index: number) => {
            const agentGrants = agent.grantedCapabilities || [];
            const granted = agentGrants.includes(cap.name);
            const providerTool = cap.kind === 'provider-tool';
            const providerToolMeta = providerToolCatalog[cap.name];
            const platformActive = !providerTool && (cap.state === 'enabled' || cap.state === 'available' || cap.state === 'restricted');
            const scopedAll = platformActive && (cap.scoped_agents?.length === 0 || !cap.scoped_agents);
            const scopedToThis = platformActive && cap.scoped_agents?.includes(agent.name);
            const effectiveAccess = granted || scopedAll || scopedToThis;
            return (
              <div key={cap.name} style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) auto', gap: 12, padding: 14, borderTop: index === 0 ? 0 : '0.5px solid var(--ink-hairline)', alignItems: 'start' }}>
                <div style={{ minWidth: 0 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <Shield size={14} style={{ color: effectiveAccess ? 'var(--teal-dark)' : 'var(--ink-faint)' }} />
                    <span className="font-mono" style={{ fontSize: 12, color: 'var(--ink)' }}>{cap.name}</span>
                    {effectiveAccess && <Badge tone="active">active</Badge>}
                    {providerTool && <Badge>provider tool</Badge>}
                  </div>
                  {cap.description && <div style={{ marginTop: 4, fontSize: 12, color: 'var(--ink-mid)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{cap.description}</div>}
                  {providerToolMeta && <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4, marginTop: 6 }}><Badge>risk: {providerToolMeta.risk}</Badge><Badge>{providerToolMeta.execution.replace(/_/g, ' ')}</Badge>{providerToolMeta.default_grant && <Badge tone="active">default</Badge>}</div>}
                </div>
                {(providerTool || cap.state !== 'disabled' || granted) && (
                  <SmallButton disabled={capLoading === cap.name || (!granted && !platformActive && !providerTool)} danger={granted} onClick={() => void (granted ? handleRevoke(agent.name, cap.name) : handleGrant(agent.name, cap.name))}>
                    {capLoading === cap.name ? '...' : granted ? 'revoke' : effectiveAccess ? 'active' : 'grant'}
                  </SmallButton>
                )}
              </div>
            );
          })}
        </div>
        {providerToolCatalogError && <div style={{ marginTop: 10, fontSize: 12, color: '#8B5A00' }}>Provider tool catalog unavailable: {providerToolCatalogError}</div>}
      </div>

      {policy && (
        <div style={cardStyle}>
          <PanelHeader title="Policy" />
          <div style={{ ...innerStyle, padding: 12 }}>
            {policy.valid != null && <Badge tone={policy.valid ? 'active' : 'danger'}>{policy.valid ? 'valid' : 'invalid'}</Badge>}
            {policy.violations?.length > 0 && <div style={{ marginTop: 10, display: 'flex', flexDirection: 'column', gap: 6 }}>{policy.violations.map((violation: string, index: number) => <div key={index} style={{ fontSize: 12, color: 'var(--red)' }}>{violation}</div>)}</div>}
            {policy.effective && <pre style={{ marginTop: 10, background: 'transparent', border: 0, color: 'var(--ink-mid)', fontSize: 11, overflowX: 'auto' }}>{JSON.stringify(policy.effective, null, 2)}</pre>}
            {!policy.effective && !policy.violations && <div style={{ marginTop: 10, fontSize: 12, color: 'var(--ink-mid)' }}>Default policy applied.</div>}
          </div>
        </div>
      )}

      <div style={{ ...cardStyle, borderColor: 'var(--red)' }}>
        <PanelHeader
          title="Danger zone"
          action={
            <SmallButton danger icon={<Trash2 size={13} aria-hidden="true" />} onClick={() => onRequestDelete(agent.name)}>
              Delete agent
            </SmallButton>
          }
        />
        <div style={{ ...innerStyle, padding: 12, color: 'var(--ink-mid)', fontSize: 12, lineHeight: 1.5 }}>
          Deletes the agent runtime, workspace state, local agent files, and comms membership. Audit logs are archived and preserved.
        </div>
      </div>
    </div>
  );
}

export function AgentSystemTab({ agent, agentConfig, capabilities, policy, capLoading, logs, refreshingLogs, refreshLogs, handleGrant, handleRevoke, handleSaveConfig, onRequestDelete, subTab, onSubTabChange }: Props) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div role="tablist" style={{ display: 'flex', gap: 18, borderBottom: '0.5px solid var(--ink-hairline)' }}>
        {SYSTEM_TABS.map((tab) => (
          <button key={tab.id} type="button" role="tab" aria-selected={subTab === tab.id} aria-controls={`sys-panel-${tab.id}`} onClick={() => onSubTabChange(tab.id)} style={{ background: 'transparent', border: 0, borderBottom: subTab === tab.id ? '1.5px solid var(--teal)' : '1.5px solid transparent', color: subTab === tab.id ? 'var(--ink)' : 'var(--ink-mid)', cursor: 'pointer', fontFamily: 'var(--font-sans)', fontSize: 12, marginBottom: -0.5, padding: '8px 0' }}>
            {tab.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`sys-panel-${subTab}`}>
        {subTab === 'config' && <ConfigContent agent={agent} agentConfig={agentConfig} capabilities={capabilities} policy={policy} capLoading={capLoading} handleGrant={handleGrant} handleRevoke={handleRevoke} handleSaveConfig={handleSaveConfig} onRequestDelete={onRequestDelete} />}
        {subTab === 'logs' && <LogsSection agentName={agent.name} logs={logs} refreshingLogs={refreshingLogs} refreshLogs={refreshLogs} />}
      </div>
    </div>
  );
}
