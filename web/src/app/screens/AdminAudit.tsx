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
import { RefreshCw, Search } from 'lucide-react';

interface AuditEntry {
  timestamp: string;
  actor: string;
  action: string;
  target: string;
  verdict: 'ok' | 'deny' | 'review' | 'halt' | 'warn';
  hash: string;
  raw: RawAuditEntry & Record<string, any>;
}

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
  const date = d.toLocaleDateString(undefined, { year: 'numeric', month: '2-digit', day: '2-digit' }).replaceAll('/', '-');
  const time = d.toLocaleTimeString(undefined, { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
  return `${date} ${time}`;
}

function eventName(raw: RawAuditEntry & Record<string, any>): string {
  return String(raw.event || raw.type || raw.action || 'event').trim();
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
  return normalized.replaceAll('_', '.');
}

function targetFor(raw: RawAuditEntry & Record<string, any>): string {
  const evt = eventName(raw);
  if (raw.target) return String(raw.target);
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
      setEntries(list.map((entry: any) => toAuditEntry(entry)).sort((a, b) => String(b.timestamp).localeCompare(String(a.timestamp))));
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
  }, [selectedAgent, query, verdictFilter, actionFilter, timeFilter, pageSize]);

  const actionOptions = useMemo(() => {
    return Array.from(new Set(entries.map((entry) => entry.action).filter(Boolean))).sort();
  }, [entries]);

  const filtered = entries.filter((entry) => {
    if (!query.trim()) return true;
    const needle = query.trim().toLowerCase();
    return [entry.timestamp, entry.actor, entry.action, entry.target, entry.verdict, entry.hash].some((value) => value.toLowerCase().includes(needle));
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
        <div style={{ display: 'grid', gridTemplateColumns: '160px 160px 150px minmax(180px, 1fr) 80px 80px', gap: 12, padding: '12px 16px', borderBottom: '0.5px solid var(--ink-hairline)' }}>
          {['Timestamp', 'Actor', 'Action', 'Target', 'Verdict', 'Hash'].map((col) => (
            <div key={col} className="mono" style={{ color: 'var(--teal-dark)', fontSize: 10, letterSpacing: '0.16em', textTransform: 'uppercase' }}>{col}</div>
          ))}
        </div>
        {loading ? (
          <div style={{ padding: 28, textAlign: 'center', color: 'var(--ink-mid)' }}>Loading audit log...</div>
        ) : filtered.length === 0 ? (
          <div style={{ padding: 28, textAlign: 'center', color: 'var(--ink-mid)' }}>No audit entries found</div>
        ) : visibleEntries.map((entry, index) => (
          <div key={`${entry.timestamp}:${entry.hash}:${index}`} style={{ display: 'grid', gridTemplateColumns: '160px 160px 150px minmax(180px, 1fr) 80px 80px', gap: 12, padding: '15px 16px', borderBottom: '0.5px solid var(--ink-hairline)', alignItems: 'center' }}>
            <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)', whiteSpace: 'nowrap' }}>{formatTimestamp(entry.timestamp)}</span>
            <span className="mono" style={{ fontSize: 12, color: 'var(--ink-mid)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{entry.actor}</span>
            <span className="mono" style={{ fontSize: 12, color: 'var(--ink)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{entry.action}</span>
            <span style={{ fontSize: 12, color: 'var(--ink-mid)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{entry.target}</span>
            <span><span style={badgeStyle(entry.verdict)}>{entry.verdict}</span></span>
            <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)' }}>{entry.hash}</span>
          </div>
        ))}
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
