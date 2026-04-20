import { Check, RefreshCw, ShieldCheck } from 'lucide-react';
import { Agent } from '../../types';

type PolicyStep = {
  level?: string;
  file?: string;
  status?: string;
  detail?: string;
};

type PolicyTabProps = {
  agents: Agent[];
  policyAgent: string;
  onPolicyAgentChange: (agent: string) => void;
  policyData: any;
  policyLoading: boolean;
  policyError: string | null;
  onValidate: () => void;
  validating: boolean;
};

type Tone = 'teal' | 'amber' | 'red' | 'neutral';

function Badge({ children, tone = 'neutral' }: { children: React.ReactNode; tone?: Tone }) {
  const tones = {
    teal: { bg: 'var(--teal-tint)', color: 'var(--teal-dark)', border: 'var(--teal-border)' },
    amber: { bg: 'var(--amber-tint)', color: 'var(--amber-foreground)', border: 'var(--amber)' },
    red: { bg: 'var(--red-tint)', color: 'var(--red)', border: 'var(--red)' },
    neutral: { bg: 'var(--warm-3)', color: 'var(--ink-mid)', border: 'var(--ink-hairline-strong)' },
  }[tone];
  return (
    <span className="mono" style={{ display: 'inline-flex', alignItems: 'center', padding: '2px 8px', fontSize: 10, letterSpacing: '0.08em', textTransform: 'uppercase', background: tones.bg, color: tones.color, border: `0.5px solid ${tones.border}`, borderRadius: 4, whiteSpace: 'nowrap' }}>
      {children}
    </span>
  );
}

function ActionButton({ children, icon, disabled = false, onClick }: { children: React.ReactNode; icon?: React.ReactNode; disabled?: boolean; onClick?: () => void }) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '5px 10px', border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 999, background: 'var(--warm)', color: 'var(--ink)', fontSize: 12, fontFamily: 'var(--sans)', cursor: disabled ? 'default' : 'pointer', opacity: disabled ? 0.55 : 1, whiteSpace: 'nowrap' }}
    >
      {icon}
      {children}
    </button>
  );
}

function MetaStat({ label, value, tone }: { label: string; value: string | number; tone?: string }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      <span className="eyebrow" style={{ fontSize: 9 }}>{label}</span>
      <span className="mono" style={{ fontSize: 14, color: tone || 'var(--ink)' }}>{value}</span>
    </div>
  );
}

function statusTone(status?: string): Tone {
  if (status === 'ok' || status === 'active') return 'teal';
  if (status === 'violation' || status === 'invalid' || status === 'expired') return 'red';
  if (status === 'missing') return 'amber';
  return 'neutral';
}

function formatPolicyValue(value: unknown) {
  if (typeof value === 'boolean') return value ? 'required' : 'disabled';
  if (value == null) return 'not set';
  if (Array.isArray(value)) return value.join(', ');
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

function prettyPolicyLabel(label: string) {
  return label.replace(/_/g, ' ');
}

function KeyValueSection({ title, description, data, empty }: { title: string; description: string; data: Record<string, unknown>; empty: string }) {
  const entries = Object.entries(data || {});
  return (
    <section style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', overflow: 'hidden' }}>
      <div style={{ padding: '14px 16px', borderBottom: '0.5px solid var(--ink-hairline)', background: 'var(--warm-3)' }}>
        <div className="eyebrow" style={{ fontSize: 9, marginBottom: 6 }}>{title}</div>
        <div style={{ color: 'var(--ink-mid)', fontSize: 12, lineHeight: 1.45 }}>{description}</div>
      </div>
      {entries.length === 0 ? (
        <div style={{ padding: 16, color: 'var(--ink-mid)', fontSize: 12 }}>{empty}</div>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(210px, 1fr))', gap: 0 }}>
          {entries.map(([key, value]) => (
            <div key={key} style={{ padding: '14px 16px', borderTop: '0.5px solid var(--ink-hairline)' }}>
              <div className="mono" style={{ color: 'var(--teal-dark)', fontSize: 11, letterSpacing: '0.08em', textTransform: 'uppercase' }}>{prettyPolicyLabel(key)}</div>
              <div style={{ color: 'var(--ink)', fontSize: 15, marginTop: 8 }}>{formatPolicyValue(value)}</div>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function RuleList({ rules }: { rules: any[] }) {
  return (
    <section style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', overflow: 'hidden' }}>
      <div style={{ padding: '14px 16px', borderBottom: '0.5px solid var(--ink-hairline)', background: 'var(--warm-3)' }}>
        <div className="eyebrow" style={{ fontSize: 9, marginBottom: 6 }}>Decision rules</div>
        <div style={{ color: 'var(--ink-mid)', fontSize: 12, lineHeight: 1.45 }}>Rules evaluated by the gateway before a capability or tool call is allowed.</div>
      </div>
      {rules.length === 0 ? (
        <div style={{ padding: 16, color: 'var(--ink-mid)', fontSize: 12 }}>No custom rules were returned. Gateway defaults still apply.</div>
      ) : (
        rules.map((rule, index) => {
          const appliesTo = Array.isArray(rule?.applies_to) ? rule.applies_to : [];
          const summary = typeof rule?.rule === 'string' ? rule.rule : 'policy rule';
          return (
            <div key={`${summary}-${index}`} style={{ display: 'grid', gridTemplateColumns: '34px minmax(0, 1fr)', gap: 12, padding: '14px 16px', borderTop: index === 0 ? 0 : '0.5px solid var(--ink-hairline)' }}>
              <div className="mono" style={{ width: 26, height: 26, borderRadius: 999, border: '0.5px solid var(--ink-hairline-strong)', display: 'grid', placeItems: 'center', color: 'var(--ink-mid)', fontSize: 11 }}>{index + 1}</div>
              <div style={{ minWidth: 0 }}>
                <div style={{ color: 'var(--ink)', fontSize: 14 }}>{summary}</div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 5, marginTop: 8 }}>
                  {appliesTo.length > 0 ? appliesTo.map((capability: string) => (
                    <span key={capability} className="mono" style={{ padding: '2px 6px', border: '0.5px solid var(--ink-hairline)', borderRadius: 4, color: 'var(--ink-mid)', background: 'var(--warm)', fontSize: 10 }}>
                      {capability}
                    </span>
                  )) : <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)' }}>all matching inputs</span>}
                </div>
              </div>
            </div>
          );
        })
      )}
    </section>
  );
}

function ExceptionList({ exceptions }: { exceptions: any[] }) {
  return (
    <section style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', overflow: 'hidden' }}>
      <div style={{ padding: '14px 16px', borderBottom: '0.5px solid var(--ink-hairline)', background: 'var(--warm-3)' }}>
        <div className="eyebrow" style={{ fontSize: 9, marginBottom: 6 }}>Exceptions</div>
        <div style={{ color: 'var(--ink-mid)', fontSize: 12, lineHeight: 1.45 }}>Validated policy exceptions. Exceptions require explicit grants and remain visible here.</div>
      </div>
      {exceptions.length === 0 ? (
        <div style={{ padding: 16, color: 'var(--ink-mid)', fontSize: 12 }}>No active exceptions.</div>
      ) : (
        exceptions.map((exception, index) => (
          <div key={`${exception?.exception_id || 'exception'}-${index}`} style={{ display: 'grid', gridTemplateColumns: 'minmax(130px, 0.8fr) 90px minmax(160px, 1fr)', gap: 14, alignItems: 'center', padding: '12px 16px', borderTop: index === 0 ? 0 : '0.5px solid var(--ink-hairline)' }}>
            <span className="mono" style={{ color: 'var(--ink)', fontSize: 12 }}>{exception?.exception_id || `exception-${index + 1}`}</span>
            <Badge tone={statusTone(exception?.status)}>{exception?.status || 'unknown'}</Badge>
            <span style={{ color: 'var(--ink-mid)', fontSize: 12 }}>{exception?.detail || exception?.parameter || 'no detail'}</span>
          </div>
        ))
      )}
    </section>
  );
}

function ResolutionChain({ chain }: { chain: PolicyStep[] }) {
  if (!chain.length) {
    return (
      <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm-2)', padding: 18, color: 'var(--ink-mid)', fontSize: 12 }}>
        No policy chain was returned for this agent.
      </div>
    );
  }

  return (
    <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 10, background: 'var(--warm-2)', overflow: 'hidden' }}>
      <div style={{ display: 'grid', gridTemplateColumns: '110px 90px minmax(180px, 1fr) minmax(220px, 1.3fr)', gap: 14, alignItems: 'center', padding: '10px 16px', borderBottom: '0.5px solid var(--ink-hairline)', background: 'var(--warm-3)' }}>
        {['Level', 'Status', 'File', 'Detail'].map((label) => <span key={label} className="eyebrow" style={{ fontSize: 9 }}>{label}</span>)}
      </div>
      {chain.map((step, index) => (
        <div key={`${step.level || 'step'}-${index}`} style={{ display: 'grid', gridTemplateColumns: '110px 90px minmax(180px, 1fr) minmax(220px, 1.3fr)', gap: 14, alignItems: 'center', padding: '12px 16px', borderTop: index === 0 ? 0 : '0.5px solid var(--ink-hairline)' }}>
          <span className="mono" style={{ fontSize: 12, color: 'var(--ink)' }}>{step.level || 'policy'}</span>
          <Badge tone={statusTone(step.status)}>{step.status || 'unknown'}</Badge>
          <span className="mono" style={{ fontSize: 11, color: step.file ? 'var(--ink-mid)' : 'var(--ink-faint)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{step.file || 'not assigned'}</span>
          <span style={{ fontSize: 12, color: 'var(--ink-mid)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{step.detail || 'inherit defaults'}</span>
        </div>
      ))}
    </div>
  );
}

export function PolicyTab({
  agents,
  policyAgent,
  onPolicyAgentChange,
  policyData,
  policyLoading,
  policyError,
  onValidate,
  validating,
}: PolicyTabProps) {
  const chain = Array.isArray(policyData?.chain) ? policyData.chain as PolicyStep[] : [];
  const violations = Array.isArray(policyData?.violations) ? policyData.violations as string[] : [];
  const rules = Array.isArray(policyData?.rules) ? policyData.rules : [];
  const parameters = policyData?.parameters && typeof policyData.parameters === 'object' ? Object.keys(policyData.parameters).length : 0;
  const hardFloors = policyData?.hard_floors && typeof policyData.hard_floors === 'object' ? Object.keys(policyData.hard_floors).length : 0;
  const parameterData = policyData?.parameters && typeof policyData.parameters === 'object' ? policyData.parameters as Record<string, unknown> : {};
  const hardFloorData = policyData?.hard_floors && typeof policyData.hard_floors === 'object' ? policyData.hard_floors as Record<string, unknown> : {};
  const exceptions = Array.isArray(policyData?.exceptions) ? policyData.exceptions : [];
  const valid = policyData ? policyData.valid !== false : null;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
        <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
          <MetaStat label="Status" value={valid == null ? 'unloaded' : valid ? 'valid' : 'invalid'} tone={valid === false ? 'var(--red)' : 'var(--teal-dark)'} />
          <MetaStat label="Rules" value={rules.length} />
          <MetaStat label="Parameters" value={parameters} />
          <MetaStat label="Hard floors" value={hardFloors} />
          <MetaStat label="Violations" value={violations.length} tone={violations.length ? 'var(--red)' : undefined} />
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
          <select
            id="policy-agent"
            name="policy-agent"
            value={policyAgent}
            onChange={(event) => onPolicyAgentChange(event.target.value)}
            style={{ height: 32, minWidth: 160, border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 999, background: 'var(--warm)', color: 'var(--ink)', padding: '0 12px', fontFamily: 'var(--sans)', fontSize: 12 }}
          >
            <option value="">Select agent...</option>
            {agents.map((agent) => <option key={agent.id} value={agent.name}>{agent.name}</option>)}
          </select>
          <ActionButton icon={validating ? <RefreshCw size={13} /> : <Check size={13} />} disabled={!policyAgent || validating} onClick={onValidate}>
            {validating ? 'Validating...' : 'Validate policy'}
          </ActionButton>
        </div>
      </div>

      {policyError && (
        <div style={{ border: '0.5px solid var(--red)', borderRadius: 10, background: 'var(--red-tint)', color: 'var(--red)', padding: '10px 12px', fontSize: 12 }}>
          {policyError}
        </div>
      )}

      {policyLoading ? (
        <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', padding: 28, textAlign: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>Loading policy...</div>
      ) : !policyAgent ? (
        <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', padding: 28, textAlign: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>Select an agent to view policy.</div>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) 260px', gap: 14, alignItems: 'start' }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 20, minWidth: 0 }}>
            <KeyValueSection
              title="Guardrail parameters"
              description="Computed values inherited by the selected agent after platform, org, team, and agent policy are merged."
              data={parameterData}
              empty="No parameters were returned for this agent."
            />
            <KeyValueSection
              title="Hard floors"
              description="Immutable safety floors that policy may not loosen. These remain enforced outside the agent boundary."
              data={hardFloorData}
              empty="No hard floors were returned for this agent."
            />
            <RuleList rules={rules} />
            <ExceptionList exceptions={exceptions} />
            <div>
              <div className="eyebrow" style={{ marginBottom: 10 }}>Resolution chain</div>
              <ResolutionChain chain={chain} />
            </div>
          </div>
          <aside style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', padding: 16, display: 'flex', flexDirection: 'column', gap: 14 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <ShieldCheck size={16} style={{ color: 'var(--teal-dark)' }} />
              <span className="display" style={{ fontSize: 18, color: 'var(--ink)' }}>Boundary</span>
            </div>
            <p style={{ margin: 0, color: 'var(--ink-mid)', fontSize: 12, lineHeight: 1.5 }}>
              Policy is computed outside the agent boundary. This page can validate the server result, but it does not let agents rewrite their own constraints.
            </p>
            <div style={{ borderTop: '0.5px solid var(--ink-hairline)', paddingTop: 12, display: 'flex', flexDirection: 'column', gap: 10 }}>
              <div>
                <div className="eyebrow" style={{ fontSize: 9, marginBottom: 5 }}>Agent</div>
                <div className="mono" style={{ color: 'var(--ink)', fontSize: 13 }}>{policyData?.agent || policyAgent}</div>
              </div>
              <div>
                <div className="eyebrow" style={{ fontSize: 9, marginBottom: 5 }}>Validation</div>
                <Badge tone={valid === false ? 'red' : 'teal'}>{valid === false ? 'invalid' : 'valid'}</Badge>
              </div>
              {violations.length > 0 && (
                <div>
                  <div className="eyebrow" style={{ fontSize: 9, marginBottom: 6 }}>Violations</div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    {violations.slice(0, 5).map((violation, index) => (
                      <div key={`${violation}-${index}`} style={{ color: 'var(--red)', fontSize: 12, lineHeight: 1.4 }}>{violation}</div>
                    ))}
                    {violations.length > 5 && <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)' }}>+{violations.length - 5} more</span>}
                  </div>
                </div>
              )}
            </div>
          </aside>
        </div>
      )}
    </div>
  );
}
