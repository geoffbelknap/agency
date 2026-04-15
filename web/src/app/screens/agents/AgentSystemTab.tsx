import { useState, useEffect } from 'react';
import { Shield } from 'lucide-react';
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
  subTab: SystemSubTab;
  onSubTabChange: (tab: SystemSubTab) => void;
}

function ConfigContent({ agent, agentConfig, capabilities, policy, capLoading, handleGrant, handleRevoke, handleSaveConfig }: {
  agent: Agent;
  agentConfig: Record<string, any> | null;
  capabilities: RawCapability[];
  policy: RawPolicyValidation | null;
  capLoading: string | null;
  handleGrant: (agentName: string, capability: string) => Promise<void>;
  handleRevoke: (agentName: string, capability: string) => Promise<void>;
  handleSaveConfig: (agentName: string, identity: string) => Promise<Record<string, any> | null>;
}) {
  const [editingIdentity, setEditingIdentity] = useState(false);
  const [identityDraft, setIdentityDraft] = useState('');
  const [savingConfig, setSavingConfig] = useState(false);
  const [providerToolCatalog, setProviderToolCatalog] = useState<Record<string, RawProviderToolCapability>>({});
  const [providerToolCatalogError, setProviderToolCatalogError] = useState('');

  useEffect(() => {
    if (agentConfig?.identity) {
      setIdentityDraft(agentConfig.identity);
    }
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
    <div className="space-y-4 p-4">
      {/* Identity editor */}
      {agentConfig && (
        <div>
          <div className="flex items-center justify-between mb-2">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">Identity & Personality</div>
            {!editingIdentity ? (
              <button
                onClick={() => { setIdentityDraft(agentConfig.identity || ''); setEditingIdentity(true); }}
                className="text-[10px] px-2 py-0.5 rounded bg-secondary text-muted-foreground hover:text-foreground transition-colors"
              >
                Edit
              </button>
            ) : (
              <div className="flex gap-1">
                <button
                  onClick={async () => {
                    setSavingConfig(true);
                    try {
                      const updated = await handleSaveConfig(agent.name, identityDraft);
                      if (updated) {
                        setEditingIdentity(false);
                      }
                    } finally {
                      setSavingConfig(false);
                    }
                  }}
                  disabled={savingConfig}
                  className="text-[10px] px-2 py-0.5 rounded bg-primary text-primary-foreground hover:opacity-90 transition-colors disabled:opacity-50"
                >
                  {savingConfig ? 'Saving...' : 'Save'}
                </button>
                <button
                  onClick={() => setEditingIdentity(false)}
                  className="text-[10px] px-2 py-0.5 rounded bg-secondary text-muted-foreground hover:text-foreground transition-colors"
                >
                  Cancel
                </button>
              </div>
            )}
          </div>
          {editingIdentity ? (
            <textarea
              value={identityDraft}
              onChange={(e) => setIdentityDraft(e.target.value)}
              className="w-full h-48 bg-background border border-border rounded p-3 text-xs font-mono text-foreground resize-y"
              placeholder="Agent identity markdown..."
            />
          ) : (
            <div className="bg-secondary rounded p-3 text-xs text-foreground/80 font-mono whitespace-pre-wrap max-h-32 overflow-y-auto">
              {agentConfig.identity || 'No identity configured'}
            </div>
          )}
        </div>
      )}

      {/* Constraints summary */}
      {agentConfig?.constraints && (
        <div>
          <div className="text-xs uppercase tracking-wide text-muted-foreground mb-2">Constraints</div>
          <div className="space-y-2">
            {agentConfig.constraints.hard_limits?.length > 0 && (
              <div>
                <div className="text-[10px] text-muted-foreground mb-1">Hard Limits</div>
                <div className="space-y-1">
                  {agentConfig.constraints.hard_limits.map((h: any, i: number) => (
                    <div key={i} className="text-xs text-amber-700 dark:text-amber-400/80 bg-amber-50 dark:bg-amber-950/20 border border-amber-200 dark:border-amber-900/30 rounded px-2.5 py-1.5">
                      {h.rule}
                      {h.reason && <span className="text-muted-foreground ml-1">— {h.reason}</span>}
                    </div>
                  ))}
                </div>
              </div>
            )}
            {agentConfig.constraints.escalation && (
              <div>
                <div className="text-[10px] text-muted-foreground mb-1">Escalation</div>
                <div className="space-y-1">
                  {(agentConfig.constraints.escalation.always_escalate || []).map((e: string, i: number) => (
                    <div key={i} className="text-xs text-red-700 dark:text-red-400/80 bg-red-50 dark:bg-red-950/20 border border-red-200 dark:border-red-900/30 rounded px-2.5 py-1.5">
                      Always escalate: {e}
                    </div>
                  ))}
                  {(agentConfig.constraints.escalation.flag_before_proceeding || []).map((e: string, i: number) => (
                    <div key={i} className="text-xs text-blue-700 dark:text-blue-400/80 bg-blue-50 dark:bg-blue-950/20 border border-blue-200 dark:border-blue-900/30 rounded px-2.5 py-1.5">
                      Flag: {e}
                    </div>
                  ))}
                </div>
              </div>
            )}
            {agentConfig.constraints.autonomy && (
              <div className="flex gap-3 text-xs text-muted-foreground">
                <span>Mode: <span className="text-foreground/80">{agentConfig.constraints.autonomy.default_mode}</span></span>
                <span>Max duration: <span className="text-foreground/80">{agentConfig.constraints.autonomy.autonomous_max_duration}</span></span>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Unified capabilities list */}
      <div>
        <div className="text-xs uppercase tracking-wide text-muted-foreground mb-2">Capabilities</div>
        {visibleCapabilities.length === 0 ? (
          <div className="text-xs text-muted-foreground/70">No capabilities available.</div>
        ) : (
          <div className="space-y-1.5">
            {visibleCapabilities.map((c: any) => {
              const agentGrants = agent.grantedCapabilities || [];
              const granted = agentGrants.includes(c.name);
              const providerTool = c.kind === 'provider-tool';
              const providerToolMeta = providerToolCatalog[c.name];
              const platformActive = !providerTool && (c.state === 'enabled' || c.state === 'available' || c.state === 'restricted');
              const scopedAll = platformActive && (c.scoped_agents?.length === 0 || !c.scoped_agents);
              const scopedToThis = platformActive && c.scoped_agents?.includes(agent.name);
              const effectiveAccess = granted || scopedAll || scopedToThis;
              const providerStatuses = providerToolMeta?.providers
                ? Object.entries(providerToolMeta.providers)
                    .filter(([, provider]) => provider.status && provider.status !== 'no_equivalent')
                    .map(([provider, meta]) => `${provider}: ${meta.status}`)
                : [];

              let actionStyle = 'bg-border text-muted-foreground hover:bg-blue-50 dark:hover:bg-blue-950 hover:text-blue-700 dark:hover:text-blue-400';

              if (effectiveAccess && granted) {
                actionStyle = 'bg-blue-50 dark:bg-blue-900/50 text-blue-700 dark:text-blue-400 hover:bg-red-50 dark:hover:bg-red-950 hover:text-red-700 dark:hover:text-red-400';
              } else if (effectiveAccess && !granted) {
                actionStyle = 'bg-green-50 dark:bg-green-900/50 text-green-700 dark:text-green-400';
              }

              const bgClass = effectiveAccess
                ? granted ? 'bg-blue-50 dark:bg-blue-950/40 border border-blue-200 dark:border-blue-900/50' : 'bg-green-50 dark:bg-green-950/20 border border-green-200 dark:border-green-900/30'
                : platformActive ? 'bg-secondary border border-border' : 'bg-secondary/50 border border-transparent opacity-60';

              return (
                <div key={c.name} className={`flex items-start justify-between gap-3 rounded px-3 py-2 transition-colors ${bgClass}`}>
                  <div className="flex items-start gap-2 min-w-0">
                    <Shield className={`w-3.5 h-3.5 flex-shrink-0 mt-0.5 ${effectiveAccess ? granted ? 'text-blue-400' : 'text-green-400' : 'text-muted-foreground/70'}`} />
                    <div className="min-w-0">
                      <span className={`text-xs ${effectiveAccess ? granted ? 'text-blue-300' : 'text-green-300' : 'text-foreground/80'}`}>{c.name}</span>
                      {c.description && <div className="text-[10px] text-muted-foreground line-clamp-1">{c.description}</div>}
                      {providerToolMeta && (
                        <div className="mt-1 flex flex-wrap gap-1">
                          <span className="rounded border border-border bg-background/50 px-1.5 py-0.5 text-[10px] text-muted-foreground">risk: {providerToolMeta.risk}</span>
                          <span className="rounded border border-border bg-background/50 px-1.5 py-0.5 text-[10px] text-muted-foreground">{providerToolMeta.execution.replace(/_/g, ' ')}</span>
                          {providerToolMeta.default_grant && (
                            <span className="rounded border border-green-200 dark:border-green-900/40 bg-green-50 dark:bg-green-950/20 px-1.5 py-0.5 text-[10px] text-green-700 dark:text-green-400">default</span>
                          )}
                          {providerStatuses.slice(0, 3).map((status) => (
                            <span key={status} className="rounded border border-border bg-background/50 px-1.5 py-0.5 text-[10px] text-muted-foreground">{status}</span>
                          ))}
                        </div>
                      )}
                      {effectiveAccess && !granted && (
                        <div className="text-[10px] text-green-600">Enabled platform-wide</div>
                      )}
                    </div>
                  </div>
                  {(providerTool || c.state !== 'disabled' || granted) && (
                    <button
                      onClick={() => granted ? handleRevoke(agent.name, c.name) : (platformActive || providerTool) ? handleGrant(agent.name, c.name) : undefined}
                      disabled={capLoading === c.name || (!granted && !platformActive && !providerTool)}
                      className={`text-[10px] px-2.5 py-1 rounded cursor-pointer transition-colors font-medium disabled:opacity-50 disabled:cursor-not-allowed flex-shrink-0 ${actionStyle}`}>
                      {capLoading === c.name ? '...' : effectiveAccess && !granted ? 'active' : granted ? 'revoke' : 'grant'}
                    </button>
                  )}
                </div>
              );
            })}
            {providerToolCatalogError && (
              <div className="text-[10px] text-amber-700 dark:text-amber-400/80">
                Provider tool catalog unavailable: {providerToolCatalogError}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Policy summary */}
      {policy && (
        <div>
          <div className="text-xs uppercase tracking-wide text-muted-foreground mb-2">Policy</div>
          <div className="bg-secondary rounded p-3 space-y-2">
            {policy.valid != null && (
              <span className={`text-xs ${policy.valid ? 'text-green-400' : 'text-red-400'}`}>
                {policy.valid ? 'Valid' : 'Invalid'}
              </span>
            )}
            {policy.violations && policy.violations.length > 0 && (
              <div className="space-y-1">
                {policy.violations.map((v: string, i: number) => (
                  <div key={i} className="text-xs text-red-400">{v}</div>
                ))}
              </div>
            )}
            {policy.effective && (
              <pre className="text-[10px] text-muted-foreground overflow-x-auto">{JSON.stringify(policy.effective, null, 2)}</pre>
            )}
            {!policy.effective && !policy.violations && (
              <div className="text-xs text-muted-foreground">Default policy applied.</div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

export function AgentSystemTab({
  agent,
  agentConfig,
  capabilities,
  policy,
  capLoading,
  logs,
  refreshingLogs,
  refreshLogs,
  handleGrant,
  handleRevoke,
  handleSaveConfig,
  subTab,
  onSubTabChange,
}: Props) {
  return (
    <div className="flex flex-col h-full">
      <div role="tablist" className="flex gap-2 px-2 py-1 border-b border-border">
        {SYSTEM_TABS.map((t) => (
          <button key={t.id} role="tab" aria-selected={subTab === t.id} aria-controls={`sys-panel-${t.id}`} onClick={() => onSubTabChange(t.id)}
            className={`text-xs px-2 py-1 rounded transition-colors ${
              subTab === t.id ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:text-foreground'
            }`}>
            {t.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`sys-panel-${subTab}`} className="flex-1 overflow-auto">
        {subTab === 'config' && (
          <ConfigContent
            agent={agent}
            agentConfig={agentConfig}
            capabilities={capabilities}
            policy={policy}
            capLoading={capLoading}
            handleGrant={handleGrant}
            handleRevoke={handleRevoke}
            handleSaveConfig={handleSaveConfig}
          />
        )}
        {subTab === 'logs' && (
          <LogsSection
            agentName={agent.name}
            logs={logs}
            refreshingLogs={refreshingLogs}
            refreshLogs={refreshLogs}
          />
        )}
      </div>
    </div>
  );
}
