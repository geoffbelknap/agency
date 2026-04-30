import { useState, useEffect } from 'react';
import { Shield } from 'lucide-react';
import { type RawCapability, type RawPolicyValidation, type RawAuditEntry } from '../../lib/api';
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
  const grantedCapabilities = agent.grantedCapabilities || [];
  const activeCapabilities = capabilities.filter((cap) => {
    const platformActive = cap.state === 'enabled' || cap.state === 'available' || cap.state === 'restricted';
    const scopedAll = platformActive && (cap.scoped_agents?.length === 0 || !cap.scoped_agents);
    const scopedToThis = platformActive && cap.scoped_agents?.includes(agent.name);
    return grantedCapabilities.includes(cap.name) || scopedAll || scopedToThis;
  });

  useEffect(() => {
    if (agentConfig?.identity) {
      setIdentityDraft(agentConfig.identity);
    }
  }, [agentConfig]);

  return (
    <div className="space-y-4 p-4">
      {/* Identity editor */}
      {agentConfig && (
        <div className="rounded-xl border border-border bg-secondary/30 p-4">
          <div className="flex items-center justify-between mb-2">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">Identity</div>
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
          <div className="mb-2 text-xs text-muted-foreground">
            Operator-facing instructions for this agent.
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

      <div className="grid gap-3 md:grid-cols-2">
        <div className="rounded-xl border border-border bg-secondary/30 p-4">
          <div className="mb-2 text-xs uppercase tracking-wide text-muted-foreground">Runtime</div>
          <div className="space-y-1 text-sm text-foreground">
            <div>Status: <span className="text-muted-foreground">{agent.status}</span></div>
            <div>Mode: <span className="text-muted-foreground">{agent.mode}</span></div>
            <div>Type: <span className="text-muted-foreground">{agent.type || 'agent'}</span></div>
            <div>Role: <span className="text-muted-foreground">{agent.role || 'assistant'}</span></div>
            {agent.enforcerState && <div>Enforcer: <span className="text-muted-foreground">{agent.enforcerState}</span></div>}
          </div>
        </div>
        <div className="rounded-xl border border-border bg-secondary/30 p-4">
          <div className="mb-2 text-xs uppercase tracking-wide text-muted-foreground">Configuration</div>
          <div className="space-y-1 text-sm text-foreground">
            <div>Preset: <span className="text-muted-foreground">{agent.preset || 'default'}</span></div>
            {agent.model && <div>Model: <span className="text-muted-foreground">{agent.model}</span></div>}
            <div>Capabilities: <span className="text-muted-foreground">{activeCapabilities.length}</span></div>
            {policy?.valid != null && (
              <div>
                Policy:{' '}
                <span className={policy.valid ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}>
                  {policy.valid ? 'valid' : 'needs review'}
                </span>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Unified capabilities list */}
      <div>
        <div className="text-xs uppercase tracking-wide text-muted-foreground mb-2">Capabilities</div>
        {capabilities.length === 0 ? (
          <div className="text-xs text-muted-foreground/70">No capabilities available.</div>
        ) : (
          <div className="space-y-1.5">
            {capabilities.map((c: any) => {
              const granted = grantedCapabilities.includes(c.name);
              const platformActive = c.state === 'enabled' || c.state === 'available' || c.state === 'restricted';
              const scopedAll = platformActive && (c.scoped_agents?.length === 0 || !c.scoped_agents);
              const scopedToThis = platformActive && c.scoped_agents?.includes(agent.name);
              const effectiveAccess = granted || scopedAll || scopedToThis;

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
                      {effectiveAccess && !granted && (
                        <div className="text-[10px] text-green-600">Enabled platform-wide</div>
                      )}
                    </div>
                  </div>
                  {(c.state !== 'disabled' || granted) && (
                    <button
                      onClick={() => granted ? handleRevoke(agent.name, c.name) : platformActive ? handleGrant(agent.name, c.name) : undefined}
                      disabled={capLoading === c.name || (!granted && !platformActive)}
                      className={`text-[10px] px-2.5 py-1 rounded cursor-pointer transition-colors font-medium disabled:opacity-50 disabled:cursor-not-allowed flex-shrink-0 ${actionStyle}`}>
                      {capLoading === c.name ? '...' : effectiveAccess && !granted ? 'active' : granted ? 'revoke' : 'grant'}
                    </button>
                  )}
                </div>
              );
            })}
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
            {!policy.violations?.length && (
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
      <div role="tablist" className="flex flex-wrap gap-2 px-3 py-2 border-b border-border">
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
