import { useState, useMemo, type ReactNode } from 'react';
import { Link } from 'react-router';
import { FileText, Send, RefreshCw, ChevronDown, ChevronRight } from 'lucide-react';
import { type RawAgentResult, type RawAuditEntry } from '../../lib/api';
import { pactSummary } from '../../components/PactStatusBadge';
import { ResultReportDialog, useResultReport } from './ResultReportDialog';

interface Props {
  agentName: string;
  logs: RawAuditEntry[];
  refreshingLogs: boolean;
  refreshLogs: (name: string) => Promise<void>;
  results: RawAgentResult[];
  handleSendDM: (agentName: string, dmText: string) => Promise<boolean>;
}

function Card({ children }: { children: ReactNode }) {
  return (
    <div style={{ background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 10, padding: 20 }}>
      {children}
    </div>
  );
}

function SmallButton({ children, onClick, disabled = false, primary = false, ariaLabel }: { children: ReactNode; onClick?: () => void; disabled?: boolean; primary?: boolean; ariaLabel?: string }) {
  return (
    <button
      type="button"
      aria-label={ariaLabel}
      disabled={disabled}
      onClick={onClick}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        border: primary ? '0.5px solid var(--ink)' : '0.5px solid var(--ink-hairline-strong)',
        background: primary ? 'var(--ink)' : 'var(--warm)',
        color: primary ? 'var(--warm)' : 'var(--ink)',
        fontFamily: 'var(--font-sans)',
        fontSize: 12,
        padding: '5px 10px',
        borderRadius: 999,
        cursor: disabled ? 'default' : 'pointer',
        opacity: disabled ? 0.5 : 1,
      }}
    >
      {children}
    </button>
  );
}

function DmSection({ agentName, handleSendDM }: { agentName: string; handleSendDM: (name: string, text: string) => Promise<boolean> }) {
  const [dmText, setDmText] = useState('');

  return (
    <Card>
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12 }}>
        <div className="eyebrow">Send task via DM</div>
        <Link to={`/channels/dm-${agentName}`} className="font-mono" style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--teal-dark)', textDecoration: 'none' }}>
          open conversation
        </Link>
      </div>
      <textarea
        value={dmText}
        onChange={(e) => setDmText(e.target.value)}
        placeholder="Describe the task..."
        style={{
          width: '100%',
          minHeight: 112,
          resize: 'vertical',
          border: '0.5px solid var(--ink-hairline)',
          borderRadius: 8,
          background: 'var(--warm)',
          color: 'var(--ink)',
          outline: 0,
          padding: 12,
          fontFamily: 'var(--font-sans)',
          fontSize: 13,
          lineHeight: 1.5,
        }}
      />
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 12 }}>
        <SmallButton primary disabled={!dmText.trim()} onClick={async () => { const ok = await handleSendDM(agentName, dmText); if (ok) setDmText(''); }}>
          <Send size={13} />
          Send to DM
        </SmallButton>
        <span style={{ fontSize: 12, color: 'var(--ink-faint)' }}>Routes through the agent DM channel.</span>
      </div>
    </Card>
  );
}

type AuditFilter = 'all' | 'lifecycle' | 'mediation' | 'security' | 'llm' | 'errors';
type AuditTone = 'neutral' | 'success' | 'warn' | 'danger';
type AuditRaw = RawAuditEntry & Record<string, unknown>;

const AUDIT_FILTERS: Array<{ id: AuditFilter; label: string }> = [
  { id: 'all', label: 'All' },
  { id: 'lifecycle', label: 'Lifecycle' },
  { id: 'mediation', label: 'Mediation' },
  { id: 'security', label: 'Security' },
  { id: 'llm', label: 'LLM' },
  { id: 'errors', label: 'Errors' },
];

const CORE_FIELDS = ['timestamp', 'ts', 'event', 'type', 'agent', 'agent_name', 'source', 'lifecycle_id', 'event_id', 'request_id', 'task_id'];
const ACTION_FIELDS = ['method', 'path', 'url', 'host', 'domain', 'capability', 'provider_tool_capability', 'provider_tool_capabilities', 'tool', 'name', 'phase', 'phase_name', 'scan_type', 'scan_surface', 'scan_action', 'scan_mode'];
const RESULT_FIELDS = ['status', 'error', 'duration_ms', 'elapsed_ms', 'phase_elapsed_ms', 'input_tokens', 'output_tokens', 'cost', 'model', 'provider_model', 'finding_count', 'findings'];
const PROVENANCE_FIELDS = ['initiator', 'delivered_by', 'mode', 'provider_tool_type', 'provider_tool_types', 'provider_source_count', 'provider_citation_count', 'provider_search_query_count', 'provider_source_urls'];
const PACT_FIELDS = ['kind', 'verdict', 'required_evidence', 'answer_requirements', 'missing_evidence', 'observed', 'source_urls', 'tools'];
const PAYLOAD_FIELDS = ['task_content', 'content', 'detail', 'reason', 'data', 'args', 'content_sha256', 'content_bytes', 'content_count'];
const KNOWN_AUDIT_FIELDS = new Set([...CORE_FIELDS, ...ACTION_FIELDS, ...RESULT_FIELDS, ...PROVENANCE_FIELDS, ...PACT_FIELDS, ...PAYLOAD_FIELDS]);

function eventName(e: AuditRaw): string {
  return text(e.event) || text(e.type) || 'event';
}

function eventTime(e: AuditRaw): string {
  const value = text(e.timestamp) || text(e.ts);
  return value.includes('T') ? value.slice(11, 19) : value;
}

function text(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return '';
}

function numberValue(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : undefined;
  }
  return undefined;
}

function arrayCount(value: unknown): number {
  if (Array.isArray(value)) return value.length;
  if (typeof value === 'string' && value.trim()) return value.split(',').filter((item) => item.trim()).length;
  return 0;
}

function valueSummary(value: unknown): string {
  if (value === undefined || value === null || value === '') return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function truncate(value: string, max = 120): string {
  return value.length > max ? `${value.slice(0, max - 1)}...` : value;
}

function formatMs(value: unknown): string {
  const ms = numberValue(value);
  if (ms === undefined) return '';
  return ms < 1000 ? `${Math.round(ms)}ms` : `${(ms / 1000).toFixed(1)}s`;
}

function classifyAuditEntry(e: AuditRaw): Exclude<AuditFilter, 'all'> {
  const name = eventName(e);
  const status = numberValue(e.status);
  if (e.error || status && status >= 400 || /error|failed|denied|violation/i.test(name)) return 'errors';
  if (name === 'agent_signal_pact_verdict') return 'lifecycle';
  if (/SECURITY_SCAN|XPIA|MCP_TOOL_MUTATION/i.test(name) || e.scan_type || e.findings || e.finding_count !== undefined) return 'security';
  if (/^LLM_|infra_llm/i.test(name)) return 'llm';
  if (/MEDIATION|HTTP_PROXY|PROXY|capability|credential/i.test(name) || e.method || e.url || e.path || e.host || e.domain) return 'mediation';
  return 'lifecycle';
}

function toneForEntry(e: AuditRaw): AuditTone {
  const status = numberValue(e.status);
  if (e.error || status && status >= 400 || /error|failed|denied/i.test(eventName(e))) return 'danger';
  if (eventName(e) === 'agent_signal_pact_verdict') {
    if (text(e.verdict) === 'completed') return 'success';
    if (text(e.verdict) === 'blocked') return 'danger';
    if (text(e.verdict) === 'needs_action') return 'warn';
  }
  if (/FLAGGED|MUTATION|XPIA/i.test(eventName(e))) return 'warn';
  if (/SECURITY_SCAN_PASSED/i.test(eventName(e))) return 'success';
  if (/SECURITY_SCAN_NOT_APPLICABLE|SECURITY_SCAN_SKIPPED/i.test(eventName(e))) return 'neutral';
  if (/halt|pause|revoke/i.test(eventName(e))) return 'warn';
  if (/started|complete|valid|granted|delivered/i.test(eventName(e))) return 'success';
  return 'neutral';
}

function chip(label: string, value: unknown): string {
  const rendered = valueSummary(value);
  return rendered ? `${label}: ${rendered}` : '';
}

function summarizeAuditEntry(e: AuditRaw) {
  const name = eventName(e);
  const duration = formatMs(e.duration_ms);
  const elapsed = formatMs(e.elapsed_ms);
  const phaseElapsed = formatMs(e.phase_elapsed_ms);
  const status = text(e.status);
  const model = text(e.model) || text(e.provider_model);
  const inputTokens = numberValue(e.input_tokens);
  const outputTokens = numberValue(e.output_tokens);

  if (/SECURITY_SCAN|XPIA|MCP_TOOL_MUTATION/i.test(name) || e.scan_type || e.findings || e.finding_count !== undefined) {
    const scanType = text(e.scan_type) || (name.includes('XPIA') ? 'xpia' : 'security');
    const surface = text(e.scan_surface);
    const action = text(e.scan_action);
    const findingCount = numberValue(e.finding_count);
    const findingText = valueSummary(e.findings) || text(e.error);
    const contentCount = numberValue(e.content_count);
    const contentBytes = numberValue(e.content_bytes);
    const title = name === 'SECURITY_SCAN_PASSED'
      ? 'Security scan passed'
      : name === 'SECURITY_SCAN_SKIPPED'
        ? 'Security scan skipped'
        : name === 'SECURITY_SCAN_NOT_APPLICABLE'
          ? 'Security scan not applicable'
          : 'Security scan flagged';
    return {
      title,
      summary: [
        scanType,
        surface,
        action,
        findingCount !== undefined ? `${findingCount} findings` : '',
        contentCount !== undefined ? `${contentCount} items` : '',
        contentBytes !== undefined ? `${contentBytes} bytes` : '',
        findingText ? truncate(findingText, 90) : '',
      ].filter(Boolean).join(' · '),
      chips: [
        chip('scan', scanType),
        chip('surface', surface),
        chip('action', action),
        chip('findings', findingCount),
        chip('hash', e.content_sha256),
      ].filter(Boolean),
      tone: toneForEntry(e),
    };
  }

  if (/^LLM_DIRECT/.test(name) || /^infra_llm/.test(name)) {
    return {
      title: name.includes('ERROR') ? 'LLM request failed' : 'LLM request',
      summary: [model, duration, inputTokens !== undefined || outputTokens !== undefined ? `${inputTokens ?? 0} in / ${outputTokens ?? 0} out` : '', status ? `status ${status}` : '', text(e.source)].filter(Boolean).join(' · '),
      chips: [chip('model', model), chip('duration', duration), chip('tokens', inputTokens !== undefined || outputTokens !== undefined ? `${inputTokens ?? 0}/${outputTokens ?? 0}` : ''), chip('status', status)].filter(Boolean),
      tone: toneForEntry(e),
    };
  }

  if (name === 'agent_signal_pact_verdict') {
    const verdict = text(e.verdict) || 'unknown';
    const kind = text(e.kind) || 'contract';
    const sources = arrayCount(e.source_urls);
    const missing = arrayCount(e.missing_evidence);
    const tools = arrayCount(e.tools);
    return {
      title: `PACT ${verdict}`,
      summary: [
        kind,
        pactSummary({ kind, verdict, source_urls: Array.isArray(e.source_urls) ? e.source_urls : [], missing_evidence: Array.isArray(e.missing_evidence) ? e.missing_evidence : [] }),
        tools > 0 ? `${tools} tools` : '',
        text(e.task_id) ? `task ${text(e.task_id)}` : '',
      ].filter(Boolean).join(' · '),
      chips: [
        chip('contract', kind),
        chip('task', e.task_id),
        chip('sources', sources || ''),
        chip('missing', missing || ''),
        chip('tools', tools || ''),
      ].filter(Boolean),
      tone: toneForEntry(e),
    };
  }

  if (/MEDIATION|HTTP_PROXY|PROXY/.test(name) || e.method || e.path || e.url || e.host || e.domain) {
    const target = text(e.path) || text(e.url) || text(e.host) || text(e.domain);
    return {
      title: name === 'HTTP_PROXY' ? 'Mediated HTTP' : 'Mediation event',
      summary: [text(e.method), target, text(e.source), status ? `status ${status}` : '', duration].filter(Boolean).join(' · '),
      chips: [chip('method', e.method), chip('target', target), chip('status', status), chip('source', e.source)].filter(Boolean),
      tone: toneForEntry(e),
    };
  }

  if (name === 'start_phase') {
    return {
      title: 'Startup phase',
      summary: [`phase ${text(e.phase)}`, text(e.phase_name), phaseElapsed ? `phase ${phaseElapsed}` : '', elapsed ? `gateway t+${elapsed}` : ''].filter(Boolean).join(' · '),
      chips: [chip('phase', e.phase), chip('elapsed', elapsed), chip('step', phaseElapsed)].filter(Boolean),
      tone: 'neutral' as AuditTone,
    };
  }

  if (name === 'agent_started' || name === 'agent_restarted') {
    return {
      title: name === 'agent_restarted' ? 'Agent restarted' : 'Agent started',
      summary: [elapsed ? `gateway t+${elapsed}` : '', text(e.instance_id)].filter(Boolean).join(' · '),
      chips: [chip('elapsed', elapsed), chip('instance', e.instance_id)].filter(Boolean),
      tone: 'success' as AuditTone,
    };
  }

  if (name === 'task_delivered' || e.task_content || e.content) {
    const content = text(e.task_content) || text(e.content);
    return {
      title: name === 'task_delivered' ? 'Task delivered' : name,
      summary: truncate(content || text(e.detail) || text(e.reason)),
      chips: [chip('task', e.task_id), chip('by', e.delivered_by), chip('source', e.source)].filter(Boolean),
      tone: toneForEntry(e),
    };
  }

  const fallback = text(e.error) || text(e.reason) || text(e.capability) || text(e.phase_name) || text(e.detail) || text(e.source);
  return {
    title: name.replace(/_/g, ' '),
    summary: truncate(fallback),
    chips: [chip('source', e.source), chip('agent', e.agent || e.agent_name), chip('status', status)].filter(Boolean),
    tone: toneForEntry(e),
  };
}

function groupedFields(e: AuditRaw) {
  const section = (title: string, fields: string[]) => ({
    title,
    rows: fields
      .map((field) => [field, valueSummary(e[field])] as const)
      .filter(([, value]) => value !== ''),
  });
  const remaining = Object.keys(e)
    .filter((key) => !KNOWN_AUDIT_FIELDS.has(key) && valueSummary(e[key]) !== '')
    .sort()
    .map((key) => [key, valueSummary(e[key])] as const);
  return [
    section('Actor and identity', CORE_FIELDS),
    section('Action', ACTION_FIELDS),
    section('Result', RESULT_FIELDS),
    section('Mediation and provenance', PROVENANCE_FIELDS),
    section('PACT', PACT_FIELDS),
    section('Payload', PAYLOAD_FIELDS),
    { title: 'Additional fields', rows: remaining },
  ].filter((group) => group.rows.length > 0);
}

function AuditChip({ children, tone = 'neutral' }: { children: ReactNode; tone?: AuditTone }) {
  const colors = {
    neutral: { bg: 'var(--warm-3)', color: 'var(--ink-mid)' },
    success: { bg: 'var(--teal-tint)', color: 'var(--teal-dark)' },
    warn: { bg: 'var(--amber-tint)', color: '#8B5A00' },
    danger: { bg: 'var(--red-tint)', color: 'var(--red)' },
  }[tone];
  return <span className="font-mono" style={{ display: 'inline-flex', alignItems: 'center', padding: '2px 6px', borderRadius: 4, fontSize: 10, background: colors.bg, color: colors.color, whiteSpace: 'nowrap' }}>{children}</span>;
}

function AuditFieldGroups({ entry }: { entry: AuditRaw }) {
  return (
    <div style={{ display: 'grid', gap: 12 }}>
      {groupedFields(entry).map((group) => (
        <div key={group.title}>
          <div className="eyebrow" style={{ fontSize: 8, marginBottom: 6 }}>{group.title}</div>
          <div style={{ display: 'grid', gridTemplateColumns: 'minmax(110px, 170px) minmax(0, 1fr)', border: '0.5px solid var(--ink-hairline)', borderRadius: 8, overflow: 'hidden', background: 'var(--warm)' }}>
            {group.rows.map(([key, value], index) => (
              <div key={key} style={{ display: 'contents' }}>
                <div className="font-mono" style={{ padding: '7px 9px', fontSize: 10, color: 'var(--ink-faint)', borderTop: index === 0 ? 0 : '0.5px solid var(--ink-hairline)', background: 'var(--warm-2)' }}>{key}</div>
                <div className="font-mono" style={{ padding: '7px 9px', fontSize: 11, color: 'var(--ink-mid)', borderTop: index === 0 ? 0 : '0.5px solid var(--ink-hairline)', overflowWrap: 'anywhere', whiteSpace: value.length > 180 ? 'pre-wrap' : 'normal' }}>{value}</div>
              </div>
            ))}
          </div>
        </div>
      ))}
      <details>
        <summary className="font-mono" style={{ cursor: 'pointer', fontSize: 10, color: 'var(--teal-dark)' }}>Raw JSON</summary>
        <pre style={{ margin: '8px 0 0', maxHeight: 260, overflow: 'auto', background: 'var(--warm)', border: '0.5px solid var(--ink-hairline)', borderRadius: 8, padding: 10, color: 'var(--ink-mid)', fontSize: 11, lineHeight: 1.5 }}>{JSON.stringify(entry, null, 2)}</pre>
      </details>
    </div>
  );
}

function LogsSection({ agentName, logs, refreshingLogs, refreshLogs, results = [] }: {
  agentName: string;
  logs: RawAuditEntry[];
  refreshingLogs: boolean;
  refreshLogs: (name: string) => Promise<void>;
  results?: RawAgentResult[];
}) {
  const [expandedLog, setExpandedLog] = useState<number | null>(null);
  const [filter, setFilter] = useState<AuditFilter>('all');
  const report = useResultReport(agentName);
  const resultTaskIDs = useMemo(() => new Set(results.map((result) => result.task_id)), [results]);
  const reversedLogs = useMemo(() => logs.slice().reverse() as AuditRaw[], [logs]);
  const visibleLogs = useMemo(() => reversedLogs.filter((entry) => {
    if (filter === 'all') return true;
    if (filter === 'errors') return classifyAuditEntry(entry) === 'errors';
    return classifyAuditEntry(entry) === filter;
  }), [filter, reversedLogs]);

  return (
    <Card>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12, flexWrap: 'wrap' }}>
        <div className="eyebrow">Audit log</div>
        <span className="font-mono" style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--ink-faint)' }}>{visibleLogs.length} / {logs.length} events</span>
        <SmallButton ariaLabel={refreshingLogs ? 'Refreshing logs' : 'Refresh logs'} disabled={refreshingLogs} onClick={() => void refreshLogs(agentName)}>
          <RefreshCw size={13} className={refreshingLogs ? 'animate-spin' : ''} />
          Refresh
        </SmallButton>
      </div>
      <div role="tablist" aria-label="Audit log filters" style={{ display: 'flex', gap: 6, marginBottom: 12, flexWrap: 'wrap' }}>
        {AUDIT_FILTERS.map((item) => (
          <button
            key={item.id}
            type="button"
            role="tab"
            aria-selected={filter === item.id}
            onClick={() => { setFilter(item.id); setExpandedLog(null); }}
            style={{ border: '0.5px solid var(--ink-hairline-strong)', borderRadius: 999, background: filter === item.id ? 'var(--ink)' : 'var(--warm)', color: filter === item.id ? 'var(--warm)' : 'var(--ink-mid)', fontFamily: 'var(--font-sans)', fontSize: 11, padding: '4px 9px', cursor: 'pointer' }}
          >
            {item.label}
          </button>
        ))}
      </div>
      <div style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 8, overflow: 'hidden', background: 'var(--warm)' }}>
        {logs.length === 0 ? (
          <div style={{ padding: 16, fontSize: 13, color: 'var(--ink-faint)' }}>No audit logs yet.</div>
        ) : visibleLogs.length === 0 ? (
          <div style={{ padding: 16, fontSize: 13, color: 'var(--ink-faint)' }}>No matching audit events.</div>
        ) : (
          visibleLogs.map((e, i) => {
            const isExpanded = expandedLog === i;
            const summary = summarizeAuditEntry(e);
            const taskID = text(e.task_id);
            const hasResult = taskID && resultTaskIDs.has(taskID);
            return (
              <div key={i} style={{ borderTop: i === 0 ? 0 : '0.5px solid var(--ink-hairline)' }}>
                <button
                  type="button"
                  onClick={() => setExpandedLog(isExpanded ? null : i)}
                  style={{
                    display: 'grid',
                    gridTemplateColumns: '18px 76px minmax(135px, 180px) minmax(0, 1fr)',
                    gap: 12,
                    alignItems: 'start',
                    width: '100%',
                    border: 0,
                    background: 'transparent',
                    padding: '12px',
                    textAlign: 'left',
                    cursor: 'pointer',
                    fontFamily: 'var(--font-mono)',
                    fontSize: 11,
                    color: 'var(--ink-mid)',
                  }}
                >
                  {isExpanded ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                  <span style={{ color: 'var(--ink-faint)' }}>{eventTime(e)}</span>
                  <span style={{ color: summary.tone === 'danger' ? 'var(--red)' : 'var(--ink)' }}>{summary.title}</span>
                  <span style={{ minWidth: 0 }}>
                    <span style={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{summary.summary || eventName(e)}</span>
                    {summary.chips.length > 0 && (
                      <span style={{ display: 'flex', gap: 4, flexWrap: 'wrap', marginTop: 6 }}>
                        {summary.chips.slice(0, 5).map((item) => <AuditChip key={item} tone={summary.tone}>{item}</AuditChip>)}
                      </span>
                    )}
                  </span>
                </button>
                {hasResult && (
                  <div style={{ padding: '0 12px 10px 118px' }}>
                    <button
                      type="button"
                      onClick={() => void report.openReport(taskID)}
                      className="font-mono"
                      style={{ display: 'inline-flex', alignItems: 'center', gap: 4, padding: '3px 8px', borderRadius: 4, border: '0.5px solid var(--ink-hairline)', background: 'var(--warm-3)', color: 'var(--teal-dark)', fontSize: 10, cursor: 'pointer' }}
                    >
                      <FileText size={10} />
                      View result
                    </button>
                  </div>
                )}
                {isExpanded && (
                  <div style={{ padding: '0 12px 14px 118px', fontSize: 12, color: e.error ? 'var(--red)' : 'var(--ink-mid)', lineHeight: 1.5 }}>
                    <AuditFieldGroups entry={e} />
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>
      <ResultReportDialog
        openTask={report.openTask}
        reportContent={report.reportContent}
        reportLoading={report.reportLoading}
        onClose={report.closeReport}
      />
    </Card>
  );
}

export function AgentActivityTab({ agentName, logs, refreshingLogs, refreshLogs, results, handleSendDM }: Props) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <DmSection agentName={agentName} handleSendDM={handleSendDM} />
      <LogsSection agentName={agentName} logs={logs} refreshingLogs={refreshingLogs} refreshLogs={refreshLogs} results={results} />
    </div>
  );
}

export { LogsSection };
