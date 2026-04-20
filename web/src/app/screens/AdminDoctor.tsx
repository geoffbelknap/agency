import { useState, useCallback } from 'react';
import { Link } from 'react-router';
import { AlertTriangle, CheckCircle, RefreshCw, ShieldAlert, XCircle } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '../lib/api';
import { DoctorCheck } from '../types';

interface AgentGroup {
  name: string;
  checks: DoctorCheck[];
  passed: number;
  warnings: number;
  failures: number;
  total: number;
  allPass: boolean;
}

type Tone = 'teal' | 'amber' | 'red' | 'neutral';

type CachedDoctorResult = {
  checks: DoctorCheck[];
  lastRun: string;
};

const DOCTOR_CACHE_KEY = 'agency.admin.doctor.lastResult';

function normalizeCheckName(name: string) {
  return String(name || '').replace(/^(docker|podman|containerd)_/, '');
}

function normalizeDoctorChecks(checks: any[] | undefined): DoctorCheck[] {
  return (checks || []).map((c: any) => ({
    id: c.id || [c.name, c.agent || c.agentName || '', c.scope || '', c.backend || ''].join(':'),
    agentName: c.agentName || c.agent,
    name: normalizeCheckName(c.name),
    scope: c.scope,
    backend: c.backend,
    status: c.status,
    message: c.message || c.detail || '',
    fix: c.fix || '',
  })).filter((check) => check.name && check.status);
}

function readCachedDoctorResult(): CachedDoctorResult {
  try {
    const raw = window.localStorage.getItem(DOCTOR_CACHE_KEY);
    if (!raw) return { checks: [], lastRun: '' };
    const parsed = JSON.parse(raw) as Partial<CachedDoctorResult>;
    return {
      checks: normalizeDoctorChecks(parsed.checks),
      lastRun: typeof parsed.lastRun === 'string' ? parsed.lastRun : '',
    };
  } catch {
    return { checks: [], lastRun: '' };
  }
}

function writeCachedDoctorResult(result: CachedDoctorResult) {
  try {
    window.localStorage.setItem(DOCTOR_CACHE_KEY, JSON.stringify(result));
  } catch {
    // Cache is opportunistic; failing storage must not block diagnosis.
  }
}

function formatLastRun(value: string) {
  if (!value) return 'never';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

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

function checkTone(status: DoctorCheck['status']): Tone {
  if (status === 'pass') return 'teal';
  if (status === 'warn') return 'amber';
  if (status === 'fail') return 'red';
  return 'neutral';
}

function CheckIcon({ status }: { status: DoctorCheck['status'] }) {
  if (status === 'pass') return <CheckCircle size={15} style={{ color: 'var(--teal-dark)' }} />;
  if (status === 'warn') return <AlertTriangle size={15} style={{ color: 'var(--amber)' }} />;
  return <XCircle size={15} style={{ color: 'var(--red)' }} />;
}

function RecoveryLink({ to, children }: { to: string; children: React.ReactNode }) {
  return (
    <Link to={to} style={{ display: 'inline-flex', alignItems: 'center', padding: '4px 9px', border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 999, color: 'var(--ink)', background: 'var(--warm)', fontSize: 12, textDecoration: 'none' }}>
      {children}
    </Link>
  );
}

function hasImagePruneRemediation(check: DoctorCheck) {
  return check.status !== 'pass' && check.name.endsWith('_dangling_images');
}

export function AdminDoctor() {
  const cached = readCachedDoctorResult();
  const [checks, setChecks] = useState<DoctorCheck[]>(cached.checks);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lastRun, setLastRun] = useState<string>(cached.lastRun);
  const [expandedAgent, setExpandedAgent] = useState<string | null>(null);
  const [remediatingCheck, setRemediatingCheck] = useState<string | null>(null);

  const runDoctor = useCallback(async () => {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 35000);
    try {
      setLoading(true);
      setError(null);
      const data = await api.admin.doctor({ signal: controller.signal });
      const mapped = normalizeDoctorChecks(data.checks);
      const completedAt = new Date().toISOString();
      setChecks(mapped);
      setLastRun(completedAt);
      writeCachedDoctorResult({ checks: mapped, lastRun: completedAt });
    } catch (e: any) {
      setError(e.name === 'AbortError' ? 'Doctor check timed out after 35 seconds' : e.message || 'Doctor check failed');
    } finally {
      window.clearTimeout(timeout);
      setLoading(false);
    }
  }, []);

  const groups: AgentGroup[] = Object.entries(
    checks.reduce((acc, check) => {
      const key = check.agentName || '(platform)';
      if (!acc[key]) acc[key] = [];
      acc[key].push(check);
      return acc;
    }, {} as Record<string, DoctorCheck[]>),
  ).map(([name, agentChecks]) => {
    const passed = agentChecks.filter((check) => check.status === 'pass').length;
    const warnings = agentChecks.filter((check) => check.status === 'warn').length;
    const failures = agentChecks.filter((check) => check.status === 'fail').length;
    return {
      name,
      checks: agentChecks,
      passed,
      warnings,
      failures,
      total: agentChecks.length,
      allPass: passed === agentChecks.length,
    };
  });

  const totalChecks = checks.length;
  const passingChecks = checks.filter((check) => check.status === 'pass').length;
  const warningChecks = checks.filter((check) => check.status === 'warn').length;
  const failedChecks = checks.filter((check) => check.status === 'fail').length;
  const issueCount = groups.filter((group) => !group.allPass).length;
  const platformIssues = groups.find((group) => group.name === '(platform)' && !group.allPass) ?? null;
  const firstAgentIssue = groups.find((group) => group.name !== '(platform)' && !group.allPass) ?? null;

  const summaryText = totalChecks === 0
    ? lastRun ? 'No checks returned' : 'No doctor run yet'
    : issueCount === 0
      ? `${totalChecks} checks passing`
      : `${issueCount} ${issueCount === 1 ? 'surface needs' : 'surfaces need'} attention`;

  const handleCardClick = (agentName: string) => {
    setExpandedAgent(expandedAgent === agentName ? null : agentName);
  };

  const pruneImages = async (checkId: string) => {
    try {
      setRemediatingCheck(checkId);
      const result = await api.admin.pruneImages();
      toast.success(`Pruned ${result.pruned} image${result.pruned === 1 ? '' : 's'}${result.skipped ? `, skipped ${result.skipped}` : ''}`);
      await runDoctor();
    } catch (e: any) {
      toast.error(e.message || 'Failed to prune images');
    } finally {
      setRemediatingCheck(null);
    }
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
        <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
          <MetaStat label="Status" value={loading ? 'running' : error ? 'error' : totalChecks === 0 ? 'idle' : issueCount === 0 ? 'clear' : 'attention'} tone={error ? 'var(--red)' : issueCount > 0 ? 'var(--amber)' : totalChecks > 0 ? 'var(--teal-dark)' : undefined} />
          <MetaStat label="Checks" value={totalChecks} />
          <MetaStat label="Passing" value={passingChecks} tone="var(--teal-dark)" />
          <MetaStat label="Warnings" value={warningChecks} tone={warningChecks ? 'var(--amber)' : undefined} />
          <MetaStat label="Failures" value={failedChecks} tone={failedChecks ? 'var(--red)' : undefined} />
          <MetaStat label="Last run" value={formatLastRun(lastRun)} />
        </div>
        <ActionButton icon={<RefreshCw size={13} />} disabled={loading} onClick={runDoctor}>
          {loading ? 'Running...' : 'Run Doctor'}
        </ActionButton>
      </div>

      {error && (
        <div style={{ border: '0.5px solid var(--red)', borderRadius: 10, background: 'var(--red-tint)', color: 'var(--red)', padding: '10px 12px', fontSize: 12 }}>
          {error}
        </div>
      )}

      {!loading && !error && issueCount > 0 && (
        <section style={{ border: '0.5px solid var(--amber)', borderRadius: 12, background: 'var(--amber-tint)', padding: 16, display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 14, flexWrap: 'wrap' }}>
          <div style={{ display: 'flex', gap: 10, alignItems: 'flex-start' }}>
            <ShieldAlert size={18} style={{ color: 'var(--amber-foreground)', marginTop: 1 }} />
            <div>
              <div style={{ color: 'var(--ink)', fontSize: 15 }}>{issueCount === 1 ? '1 issue needs attention' : `${issueCount} issues need attention`}</div>
              <div style={{ color: 'var(--ink-mid)', fontSize: 12, marginTop: 4 }}>Start with platform checks when shared services fail. Agent-scoped issues should stay scoped to the affected agent.</div>
            </div>
          </div>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            {platformIssues && <RecoveryLink to="/admin/infrastructure">Open Infrastructure</RecoveryLink>}
            {firstAgentIssue && <RecoveryLink to={`/agents/${encodeURIComponent(firstAgentIssue.name)}`}>Open Agent: {firstAgentIssue.name}</RecoveryLink>}
            <ActionButton onClick={runDoctor} disabled={loading}>Re-run checks</ActionButton>
          </div>
        </section>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) 260px', gap: 14, alignItems: 'start' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12, minWidth: 0 }}>
          {loading ? (
            <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', padding: 28, textAlign: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>Running doctor checks...</div>
          ) : groups.length === 0 ? (
            <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', padding: 28, textAlign: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>
              {lastRun ? 'No checks returned by the last doctor run.' : 'No cached doctor results. Run Doctor to check this environment.'}
            </div>
          ) : (
            groups.map((group) => (
              <section key={group.name} style={{ border: `0.5px solid ${group.failures ? 'var(--red)' : group.warnings ? 'var(--amber)' : 'var(--ink-hairline)'}`, borderRadius: 12, background: 'var(--warm-2)', overflow: 'hidden' }}>
                <button type="button" onClick={() => handleCardClick(group.name)} style={{ width: '100%', display: 'grid', gridTemplateColumns: 'minmax(160px, 1fr) 90px 90px 90px 96px', gap: 12, alignItems: 'center', textAlign: 'left', padding: '14px 16px', border: 0, background: expandedAgent === group.name ? 'var(--warm-3)' : 'transparent', cursor: 'pointer', color: 'var(--ink)' }}>
                  <div style={{ minWidth: 0 }}>
                    <div className="mono" style={{ color: 'var(--ink)', fontSize: 13, overflow: 'hidden', textOverflow: 'ellipsis' }}>{group.name}</div>
                    <div style={{ color: 'var(--ink-mid)', fontSize: 12, marginTop: 4 }}>{group.allPass ? 'All checks passing' : `${group.total - group.passed} follow-up ${group.total - group.passed === 1 ? 'check' : 'checks'}`}</div>
                  </div>
                  <MetaStat label="Pass" value={group.passed} tone="var(--teal-dark)" />
                  <MetaStat label="Warn" value={group.warnings} tone={group.warnings ? 'var(--amber)' : undefined} />
                  <MetaStat label="Fail" value={group.failures} tone={group.failures ? 'var(--red)' : undefined} />
                  <Badge tone={group.allPass ? 'teal' : group.failures ? 'red' : 'amber'}>{group.passed}/{group.total}</Badge>
                </button>
                {expandedAgent === group.name && (
                  <div style={{ borderTop: '0.5px solid var(--ink-hairline)' }}>
                    {group.checks.map((check, index) => (
                      <div key={check.id} style={{ display: 'grid', gridTemplateColumns: '24px 150px 78px minmax(200px, 1fr)', gap: 12, alignItems: 'start', padding: '12px 16px', borderTop: index === 0 ? 0 : '0.5px solid var(--ink-hairline)' }}>
                        <CheckIcon status={check.status} />
                        <span className="mono" style={{ color: 'var(--ink)', fontSize: 12 }}>{check.name}</span>
                        <Badge tone={checkTone(check.status)}>{check.status}</Badge>
                        <div style={{ minWidth: 0 }}>
                          {check.message && <div style={{ color: 'var(--ink-mid)', fontSize: 12, lineHeight: 1.4 }}>{check.message}</div>}
                          {check.fix && check.status !== 'pass' && <div style={{ color: 'var(--ink)', fontSize: 12, marginTop: 5 }}>Fix: <span style={{ color: 'var(--ink-mid)' }}>{check.fix}</span></div>}
                          {hasImagePruneRemediation(check) && (
                            <div style={{ marginTop: 8 }}>
                              <ActionButton onClick={() => pruneImages(check.id)} disabled={remediatingCheck === check.id || loading}>
                                {remediatingCheck === check.id ? 'Pruning...' : 'Prune images'}
                              </ActionButton>
                            </div>
                          )}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </section>
            ))
          )}
        </div>

        <aside style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', padding: 16, display: 'flex', flexDirection: 'column', gap: 14 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <ShieldAlert size={16} style={{ color: issueCount > 0 ? 'var(--amber)' : 'var(--teal-dark)' }} />
            <span className="display" style={{ fontSize: 18, color: 'var(--ink)' }}>Triage</span>
          </div>
          <p style={{ margin: 0, color: 'var(--ink-mid)', fontSize: 12, lineHeight: 1.5 }}>{summaryText}</p>
          <div style={{ borderTop: '0.5px solid var(--ink-hairline)', paddingTop: 12, display: 'flex', flexDirection: 'column', gap: 10 }}>
            <div>
              <div className="eyebrow" style={{ fontSize: 9, marginBottom: 5 }}>Scope</div>
              <div style={{ color: 'var(--ink-mid)', fontSize: 12, lineHeight: 1.45 }}>Doctor reports platform and agent scopes separately so remediation does not cross trust boundaries.</div>
            </div>
            <div>
              <div className="eyebrow" style={{ fontSize: 9, marginBottom: 5 }}>Next step</div>
              <div style={{ color: 'var(--ink)', fontSize: 13 }}>{issueCount > 0 ? 'Open the first follow-up scope.' : totalChecks > 0 ? 'No action required.' : 'Run Doctor when ready.'}</div>
            </div>
          </div>
        </aside>
      </div>
    </div>
  );
}
