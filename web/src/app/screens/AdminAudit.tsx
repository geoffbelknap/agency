import { useState, useEffect, useCallback, useMemo } from 'react';
import { api, type RawAuditEntry } from '../lib/api';
import { Button } from '../components/ui/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '../components/ui/select';
import { ExportButton } from '../components/ExportButton';
import { ChevronDown, ChevronRight, RefreshCw, Search } from 'lucide-react';

interface AuditEntry {
  timestamp: string;
  actor: string;
  action: string;
  target: string;
  verdict: 'ok' | 'deny' | 'review' | 'halt' | 'warn';
  hash: string;
  raw: RawAuditEntry & Record<string, any>;
}

const CORE_FIELDS = ['timestamp', 'ts', 'event', 'type', 'agent', 'agent_name', 'actor', 'source', 'event_id', 'request_id', 'task_id'];
const ACTION_FIELDS = ['action', 'method', 'path', 'url', 'host', 'domain', 'capability', 'provider_tool_capability', 'provider_tool_capabilities', 'tool', 'name'];
const SECURITY_FIELDS = ['scan_type', 'scan_surface', 'scan_action', 'scan_mode', 'finding_count', 'findings', 'content_sha256', 'content_bytes', 'content_count', 'security_boundary'];
const RESULT_FIELDS = ['status', 'error', 'reason', 'detail', 'duration_ms', 'elapsed_ms', 'phase_elapsed_ms', 'input_tokens', 'output_tokens', 'cost', 'model', 'provider_model'];
const PROVENANCE_FIELDS = ['initiator', 'delivered_by', 'mode', 'provider_tool_type', 'provider_tool_types', 'provider_source_count', 'provider_citation_count', 'provider_search_query_count', 'provider_source_urls'];
const PAYLOAD_FIELDS = ['task_content', 'content', 'data', 'args'];
const KNOWN_AUDIT_FIELDS = new Set([...CORE_FIELDS, ...ACTION_FIELDS, ...SECURITY_FIELDS, ...RESULT_FIELDS, ...PROVENANCE_FIELDS, ...PAYLOAD_FIELDS, 'target', 'hash', 'sig', 'phase', 'phase_name', 'preset']);

function stableHash(value: string): string {
  let hash = 2166136261;
  for (let i = 0; i < value.length; i += 1) {
    hash ^= value.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0).toString(16).padStart(8, '0').slice(0, 6);
}

function formatTimestamp(ts: string): string {
  if (!ts) return '';
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return ts;
  const date = d.toLocaleDateString(undefined, { year: 'numeric', month: '2-digit', day: '2-digit' }).replace(/\//g, '-');
  const time = d.toLocaleTimeString(undefined, { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
  return `${date} ${time}`;
}

function eventName(raw: RawAuditEntry & Record<string, any>): string {
  return String(raw.event || raw.type || raw.action || 'event').trim();
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

function isSecurityEntry(raw: RawAuditEntry & Record<string, any>): boolean {
  const evt = eventName(raw);
  return /SECURITY_SCAN|XPIA|MCP_TOOL_MUTATION/i.test(evt) || raw.scan_type != null || raw.findings != null || raw.finding_count != null;
}

function securitySummary(raw: RawAuditEntry & Record<string, any>): string {
  const evt = eventName(raw);
  const scanType = text(raw.scan_type) || (evt.toUpperCase().includes('XPIA') ? 'xpia' : 'security');
  const surface = text(raw.scan_surface);
  const action = text(raw.scan_action);
  const findingCount = numberValue(raw.finding_count);
  const contentCount = numberValue(raw.content_count);
  const contentBytes = numberValue(raw.content_bytes);
  const boundary = text(raw.security_boundary);
  const findingText = valueSummary(raw.findings) || text(raw.error);
  return [
    scanType,
    surface,
    action,
    findingCount !== undefined ? `${findingCount} findings` : '',
    contentCount !== undefined ? `${contentCount} items` : '',
    contentBytes !== undefined ? `${contentBytes} bytes` : '',
    boundary,
    findingText ? truncate(findingText, 90) : '',
  ].filter(Boolean).join(' · ');
}

function actorFor(raw: RawAuditEntry & Record<string, any>): string {
  if (raw.actor) return String(raw.actor);
  if (raw.initiator) return String(raw.initiator);
  if (raw.agent && raw.agent !== '_system' && raw.agent !== 'system') return String(raw.agent);
  if (raw.agent_name) return String(raw.agent_name);
  if (raw.source && raw.source !== 'enforcer') return String(raw.source);
  return eventName(raw).startsWith('agent_') ? 'system' : 'admin@agency.local';
}

function dottedAction(raw: RawAuditEntry & Record<string, any>): string {
  const evt = eventName(raw);
  const normalized = evt.toLowerCase().replace(/^llm_direct_stream$/, 'llm.stream').replace(/^llm_direct$/, 'llm.call');
  return normalized.replace(/_/g, '.');
}

function targetFor(raw: RawAuditEntry & Record<string, any>): string {
  const evt = eventName(raw);
  if (raw.target) return String(raw.target);
  if (isSecurityEntry(raw)) return securitySummary(raw) || 'security scan';
  if (raw.domain) return String(raw.domain);
  if (raw.host) return String(raw.host);
  if (raw.capability) return String(raw.capability);
  if (raw.model) {
    const parts = [raw.model];
    if (raw.input_tokens != null) parts.push(`in ${Number(raw.input_tokens).toLocaleString()}`);
    if (raw.output_tokens != null) parts.push(`out ${Number(raw.output_tokens).toLocaleString()}`);
    return parts.join(' · ');
  }
  if (evt === 'start_phase') return raw.phase_name ? `phase ${raw.phase}: ${raw.phase_name}` : `phase ${raw.phase ?? ''}`;
  if (raw.preset) return String(raw.preset);
  if (raw.name) return String(raw.name);
  if (raw.error) return String(raw.error);
  if (raw.detail) return String(raw.detail);
  if (raw.reason) return String(raw.reason);
  return raw.agent || raw.agent_name || 'platform';
}

function verdictFor(raw: RawAuditEntry & Record<string, any>): AuditEntry['verdict'] {
  const evt = eventName(raw).toLowerCase();
  const status = Number(raw.status || 0);
  if (evt.includes('deny') || evt.includes('blocked') || evt.includes('unsupported')) return 'deny';
  if (evt.includes('halt') || evt.includes('budget.exceeded') || evt.includes('budget_exceeded')) return 'halt';
  if (evt.includes('security_scan_flagged') || evt.includes('xpia') || evt.includes('mcp_tool_mutation')) return 'warn';
  if (evt.includes('security_scan_skipped') || evt.includes('security_scan_not_applicable')) return 'review';
  if (evt.includes('request') || evt.includes('review')) return 'review';
  if (evt.includes('warn') || evt.includes('error') || status >= 400) return 'warn';
  return 'ok';
}

function toAuditEntry(raw: RawAuditEntry & Record<string, any>): AuditEntry {
  const canonical = JSON.stringify(raw);
  return {
    timestamp: raw.timestamp || raw.ts || '',
    actor: actorFor(raw),
    action: dottedAction(raw),
    target: targetFor(raw),
    verdict: verdictFor(raw),
    hash: String(raw.hash || raw.sig || stableHash(canonical)).slice(0, 6),
    raw,
  };
}

function badgeStyle(verdict: AuditEntry['verdict']) {
  const tones: Record<AuditEntry['verdict'], { color: string; bg: string; border: string }> = {
    ok: { color: 'var(--teal-dark)', bg: 'var(--teal-tint)', border: 'rgba(0, 164, 126, 0.22)' },
    deny: { color: 'var(--red)', bg: 'var(--red-tint)', border: 'rgba(205, 66, 66, 0.22)' },
    review: { color: 'var(--amber)', bg: 'rgba(230, 163, 0, 0.10)', border: 'rgba(230, 163, 0, 0.25)' },
    halt: { color: 'var(--red)', bg: 'var(--red-tint)', border: 'rgba(205, 66, 66, 0.22)' },
    warn: { color: 'var(--amber)', bg: 'rgba(230, 163, 0, 0.10)', border: 'rgba(230, 163, 0, 0.25)' },
  };
  const tone = tones[verdict];
  return {
    display: 'inline-flex',
    alignItems: 'center',
    border: `0.5px solid ${tone.border}`,
    borderRadius: 5,
    padding: '3px 8px',
    background: tone.bg,
    color: tone.color,
    fontFamily: 'var(--mono)',
    fontSize: 10,
    letterSpacing: '0.12em',
    textTransform: 'uppercase' as const,
  };
}

function MetaStat({ label, value, tone }: { label: string; value: string | number; tone?: string }) {
  return (
    <div style={{ minWidth: 92 }}>
      <div className="eyebrow" style={{ fontSize: 9 }}>{label}</div>
      <div className="mono" style={{ marginTop: 5, fontSize: 15, color: tone || 'var(--ink)' }}>{value}</div>
    </div>
  );
}

function groupedFields(raw: RawAuditEntry & Record<string, any>) {
  const section = (title: string, fields: string[]) => ({
    title,
    rows: fields
      .map((field) => [field, valueSummary(raw[field])] as const)
      .filter(([, value]) => value !== ''),
  });
  const remaining = Object.keys(raw)
    .filter((key) => !KNOWN_AUDIT_FIELDS.has(key) && valueSummary(raw[key]) !== '')
    .sort()
    .map((key) => [key, valueSummary(raw[key])] as const);
  return [
    section('Actor and identity', CORE_FIELDS),
    section('Action', ACTION_FIELDS),
    section('Security scan', SECURITY_FIELDS),
    section('Result', RESULT_FIELDS),
    section('Mediation and provenance', PROVENANCE_FIELDS),
    section('Payload', PAYLOAD_FIELDS),
    { title: 'Additional fields', rows: remaining },
  ].filter((group) => group.rows.length > 0);
}

function AuditFieldGroups({ entry }: { entry: AuditEntry }) {
  return (
    <div style={{ display: 'grid', gap: 12 }}>
      {groupedFields(entry.raw).map((group) => (
        <div key={group.title}>
          <div className="eyebrow" style={{ fontSize: 8, marginBottom: 6 }}>{group.title}</div>
          <div style={{ display: 'grid', gridTemplateColumns: 'minmax(112px, 180px) minmax(0, 1fr)', border: '0.5px solid var(--ink-hairline)', borderRadius: 8, overflow: 'hidden', background: 'var(--warm)' }}>
            {group.rows.map(([key, value], index) => (
              <div key={key} style={{ display: 'contents' }}>
                <div className="mono" style={{ padding: '7px 9px', fontSize: 10, color: 'var(--ink-faint)', borderTop: index === 0 ? 0 : '0.5px solid var(--ink-hairline)', background: 'var(--warm-2)' }}>{key}</div>
                <div className="mono" style={{ padding: '7px 9px', fontSize: 11, color: 'var(--ink-mid)', borderTop: index === 0 ? 0 : '0.5px solid var(--ink-hairline)', overflowWrap: 'anywhere', whiteSpace: value.length > 180 ? 'pre-wrap' : 'normal' }}>{value}</div>
              </div>
            ))}
          </div>
        </div>
      ))}
      <details>
        <summary className="mono" style={{ cursor: 'pointer', fontSize: 10, color: 'var(--teal-dark)' }}>Raw JSON</summary>
        <pre style={{ margin: '8px 0 0', maxHeight: 260, overflow: 'auto', background: 'var(--warm)', border: '0.5px solid var(--ink-hairline)', borderRadius: 8, padding: 10, color: 'var(--ink-mid)', fontSize: 11, lineHeight: 1.5 }}>{JSON.stringify(entry.raw, null, 2)}</pre>
      </details>
    </div>
  );
}

const PAGE_SIZE_OPTIONS = [25, 50, 100];
const TIME_FILTERS = [
  { value: 'all', label: 'All time' },
  { value: '1h', label: 'Last hour' },
  { value: '24h', label: 'Last 24h' },
  { value: '7d', label: 'Last 7d' },
];

function isWithinTimeFilter(timestamp: string, filter: string): boolean {
  if (filter === 'all') return true;
  const time = new Date(timestamp).getTime();
  if (Number.isNaN(time)) return true;
  const now = Date.now();
  const windows: Record<string, number> = {
    '1h': 60 * 60 * 1000,
    '24h': 24 * 60 * 60 * 1000,
    '7d': 7 * 24 * 60 * 60 * 1000,
  };
  return now - time <= (windows[filter] ?? Number.POSITIVE_INFINITY);
}

function sinceForTimeFilter(filter: string): string | undefined {
  const windows: Record<string, number> = {
    '1h': 60 * 60 * 1000,
    '24h': 24 * 60 * 60 * 1000,
    '7d': 7 * 24 * 60 * 60 * 1000,
  };
  const windowMs = windows[filter];
  if (!windowMs) return undefined;
  return new Date(Date.now() - windowMs).toISOString();
}

export function AdminAudit() {
  const [agents, setAgents] = useState<Array<{ name: string }>>([]);
  const [selectedAgent, setSelectedAgent] = useState<string>('_all');
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [verdictFilter, setVerdictFilter] = useState<string>('all');
  const [actionFilter, setActionFilter] = useState<string>('all');
  const [timeFilter, setTimeFilter] = useState<string>('all');
  const [pageSize, setPageSize] = useState<number>(25);
  const [page, setPage] = useState<number>(1);
  const [expandedEntry, setExpandedEntry] = useState<string | null>(null);

  const loadAgents = useCallback(async () => {
    try {
      const raw = await api.agents.list();
      setAgents((raw ?? []).filter((a: any) => a.name).map((a: any) => ({ name: a.name })));
    } catch {
      setAgents([]);
    }
  }, []);

  const loadEntries = useCallback(async (agent: string, timeRange: string) => {
    setLoading(true);
    setError(null);
    try {
      const raw = await api.admin.audit(agent, { since: sinceForTimeFilter(timeRange) });
      const list = Array.isArray(raw) ? raw : (raw as any)?.entries ?? [];
      setEntries(list.map((entry: any) => toAuditEntry(entry)).sort((a: AuditEntry, b: AuditEntry) => String(b.timestamp).localeCompare(String(a.timestamp))));
    } catch (err: any) {
      setError(err.message || 'Failed to load audit log');
      setEntries([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadAgents();
  }, [loadAgents]);

  useEffect(() => {
    loadEntries(selectedAgent, timeFilter);
  }, [selectedAgent, timeFilter, loadEntries]);

  useEffect(() => {
    setPage(1);
    setExpandedEntry(null);
  }, [selectedAgent, query, verdictFilter, actionFilter, timeFilter, pageSize]);

  const actionOptions = useMemo(() => {
    return Array.from(new Set(entries.map((entry) => entry.action).filter(Boolean))).sort();
  }, [entries]);

  const filtered = entries.filter((entry) => {
    if (!query.trim()) return true;
    const needle = query.trim().toLowerCase();
    return [entry.timestamp, entry.actor, entry.action, entry.target, entry.verdict, entry.hash, JSON.stringify(entry.raw)].some((value) => value.toLowerCase().includes(needle));
  }).filter((entry) => {
    if (verdictFilter !== 'all' && entry.verdict !== verdictFilter) return false;
    if (actionFilter !== 'all' && entry.action !== actionFilter) return false;
    return isWithinTimeFilter(entry.timestamp, timeFilter);
  });
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
  const safePage = Math.min(page, totalPages);
  const pageStart = filtered.length === 0 ? 0 : (safePage - 1) * pageSize + 1;
  const pageEnd = Math.min(safePage * pageSize, filtered.length);
  const visibleEntries = filtered.slice((safePage - 1) * pageSize, safePage * pageSize);
  const lastHash = entries[0]?.hash || '—';
  const chainVerified = entries.length > 0 ? 'verified' : 'waiting';
  const securityEvents = entries.filter((entry) => isSecurityEntry(entry.raw)).length;
  const exportRows = filtered.map(({ timestamp, actor, action, target, verdict, hash }) => ({ timestamp, actor, action, target, verdict, hash }));

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <section style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 18, flexWrap: 'wrap' }}>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
          <Button variant="outline" size="sm" onClick={() => setSearchOpen((open) => !open)}>
            <Search className="w-3.5 h-3.5 mr-1" />
            Search
          </Button>
          <ExportButton data={exportRows} filename={`audit-${selectedAgent}`} format="csv" label="Export (signed)" />
          <Button variant="outline" size="sm" onClick={() => loadEntries(selectedAgent, timeFilter)} disabled={loading} aria-label="Refresh audit">
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
          </Button>
        </div>
      </section>

      <section style={{ display: 'flex', gap: 24, flexWrap: 'wrap', alignItems: 'flex-end' }}>
        <MetaStat label="Entries" value={entries.length.toLocaleString()} />
        <MetaStat label="Retention" value="7 years" />
        <MetaStat label="Last hash" value={lastHash} tone="var(--teal-dark)" />
        <MetaStat label="Chain" value={chainVerified} tone={entries.length > 0 ? 'var(--teal-dark)' : 'var(--ink-faint)'} />
        <MetaStat label="Security" value={securityEvents.toLocaleString()} tone={securityEvents > 0 ? 'var(--amber)' : 'var(--ink-faint)'} />
        <div style={{ marginLeft: 'auto', minWidth: 190 }}>
          <Select value={selectedAgent} onValueChange={setSelectedAgent}>
            <SelectTrigger className="w-full bg-card border-border">
              <SelectValue placeholder="All agents" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="_all">All agents</SelectItem>
              {agents.map((agent) => <SelectItem key={agent.name} value={agent.name}>{agent.name}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
      </section>

      {searchOpen && (
        <input
          id="audit-search"
          name="audit-search"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Search actor, action, target, verdict, hash..."
          style={{ width: '100%', border: '0.5px solid var(--ink-hairline)', borderRadius: 999, padding: '10px 14px', background: 'var(--warm-2)', color: 'var(--ink)', fontFamily: 'var(--mono)', fontSize: 12 }}
        />
      )}

      <section style={{ display: 'grid', gridTemplateColumns: 'repeat(4, minmax(150px, 1fr))', gap: 10 }}>
        <div>
          <div className="eyebrow" style={{ fontSize: 9, marginBottom: 6 }}>Time</div>
          <Select value={timeFilter} onValueChange={setTimeFilter}>
            <SelectTrigger className="w-full bg-card border-border">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {TIME_FILTERS.map((filter) => <SelectItem key={filter.value} value={filter.value}>{filter.label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div>
          <div className="eyebrow" style={{ fontSize: 9, marginBottom: 6 }}>Verdict</div>
          <Select value={verdictFilter} onValueChange={setVerdictFilter}>
            <SelectTrigger className="w-full bg-card border-border">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All verdicts</SelectItem>
              {(['ok', 'warn', 'review', 'deny', 'halt'] as const).map((verdict) => <SelectItem key={verdict} value={verdict}>{verdict}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div>
          <div className="eyebrow" style={{ fontSize: 9, marginBottom: 6 }}>Action</div>
          <Select value={actionFilter} onValueChange={setActionFilter}>
            <SelectTrigger className="w-full bg-card border-border">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All actions</SelectItem>
              {actionOptions.map((action) => <SelectItem key={action} value={action}>{action}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div>
          <div className="eyebrow" style={{ fontSize: 9, marginBottom: 6 }}>Page size</div>
          <Select value={String(pageSize)} onValueChange={(value) => setPageSize(Number(value))}>
            <SelectTrigger className="w-full bg-card border-border">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {PAGE_SIZE_OPTIONS.map((size) => <SelectItem key={size} value={String(size)}>{size} rows</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
      </section>

      {error && <div style={{ border: '0.5px solid var(--red)', borderRadius: 10, background: 'var(--red-tint)', color: 'var(--red)', padding: '10px 12px', fontSize: 13 }}>{error}</div>}

      <section style={{ border: '0.5px solid var(--ink-hairline)', borderRadius: 12, background: 'var(--warm-2)', overflow: 'hidden' }}>
        <div style={{ display: 'grid', gridTemplateColumns: '28px 160px 160px 150px minmax(180px, 1fr) 80px 80px', gap: 12, padding: '12px 16px', borderBottom: '0.5px solid var(--ink-hairline)' }}>
          {['', 'Timestamp', 'Actor', 'Action', 'Target', 'Verdict', 'Hash'].map((col) => (
            <div key={col} className="mono" style={{ color: 'var(--teal-dark)', fontSize: 10, letterSpacing: '0.16em', textTransform: 'uppercase' }}>{col}</div>
          ))}
        </div>
        {loading ? (
          <div style={{ padding: 28, textAlign: 'center', color: 'var(--ink-mid)' }}>Loading audit log...</div>
        ) : filtered.length === 0 ? (
          <div style={{ padding: 28, textAlign: 'center', color: 'var(--ink-mid)' }}>No audit entries found</div>
        ) : visibleEntries.map((entry, index) => {
          const rowId = `${entry.timestamp}:${entry.hash}:${index}`;
          const expanded = expandedEntry === rowId;
          return (
            <div key={rowId} style={{ borderBottom: '0.5px solid var(--ink-hairline)' }}>
              <div style={{ display: 'grid', gridTemplateColumns: '28px 160px 160px 150px minmax(180px, 1fr) 80px 80px', gap: 12, padding: '15px 16px', alignItems: 'center' }}>
                <button
                  type="button"
                  aria-label={expanded ? 'Collapse audit entry' : 'Expand audit entry'}
                  onClick={() => setExpandedEntry(expanded ? null : rowId)}
                  style={{ width: 24, height: 24, border: '0.5px solid var(--ink-hairline)', borderRadius: 6, background: 'var(--warm)', color: 'var(--ink-mid)', display: 'inline-flex', alignItems: 'center', justifyContent: 'center', cursor: 'pointer' }}
                >
                  {expanded ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                </button>
                <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)', whiteSpace: 'nowrap' }}>{formatTimestamp(entry.timestamp)}</span>
                <span className="mono" style={{ fontSize: 12, color: 'var(--ink-mid)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{entry.actor}</span>
                <span className="mono" style={{ fontSize: 12, color: 'var(--ink)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{entry.action}</span>
                <span style={{ fontSize: 12, color: 'var(--ink-mid)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{entry.target}</span>
                <span><span style={badgeStyle(entry.verdict)}>{entry.verdict}</span></span>
                <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)' }}>{entry.hash}</span>
              </div>
              {expanded && (
                <div style={{ padding: '0 16px 16px 56px' }}>
                  <AuditFieldGroups entry={entry} />
                </div>
              )}
            </div>
          );
        })}
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, padding: '12px 16px', borderTop: '0.5px solid var(--ink-hairline)', background: 'var(--warm-1)', flexWrap: 'wrap' }}>
          <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)' }}>
            Showing {pageStart.toLocaleString()}-{pageEnd.toLocaleString()} of {filtered.length.toLocaleString()} entries
          </span>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Button variant="outline" size="sm" onClick={() => setPage((current) => Math.max(1, current - 1))} disabled={loading || safePage <= 1}>
              Previous
            </Button>
            <span className="mono" style={{ fontSize: 11, color: 'var(--ink-mid)', minWidth: 74, textAlign: 'center' }}>
              {safePage} / {totalPages}
            </span>
            <Button variant="outline" size="sm" onClick={() => setPage((current) => Math.min(totalPages, current + 1))} disabled={loading || safePage >= totalPages}>
              Next
            </Button>
          </div>
        </div>
      </section>
    </div>
  );
}
