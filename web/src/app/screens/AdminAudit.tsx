import { useState, useEffect, useCallback } from 'react';
import { api } from '../lib/api';
import { Button } from '../components/ui/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '../components/ui/select';
import { JsonView } from '../components/JsonView';
import { ExportButton } from '../components/ExportButton';
import { RefreshCw } from 'lucide-react';
import { toast } from 'sonner';

interface AuditEntry {
  timestamp: string;
  event: string;
  detail: string;
  raw: Record<string, any>;
}

function auditSummary(entry: AuditEntry): string {
  const r = entry.raw;
  const evt = entry.event || r.type || '';
  if (evt === 'LLM_DIRECT_STREAM' || evt === 'LLM_DIRECT') {
    const parts = [
      r.model,
      r.duration_ms ? `${(r.duration_ms / 1000).toFixed(1)}s` : null,
      r.input_tokens != null ? `in:${r.input_tokens}` : null,
      r.output_tokens != null ? `out:${r.output_tokens}` : null,
      r.status && r.status !== 200 ? `status:${r.status}` : null,
    ].filter(Boolean);
    return parts.join(' · ');
  }
  if (evt === 'HTTP_PROXY' || evt === 'HTTP_PROXY_ERROR') {
    const parts = [
      r.method,
      r.host || r.url,
      r.status ? `${r.status}` : null,
      r.duration_ms ? `${r.duration_ms}ms` : null,
    ].filter(Boolean);
    return parts.join(' · ');
  }
  if (evt === 'LLM_DIRECT_ERROR') {
    return r.error || r.detail || '';
  }
  if (evt === 'DOMAIN_BLOCKED' || evt === 'SERVICE_DENIED') {
    return [r.host || r.domain, r.reason].filter(Boolean).join(' — ');
  }
  if (evt.startsWith('agent_signal_')) {
    const sig = evt.replace('agent_signal_', '');
    const parts = [sig, r.data?.channel, r.data?.task_id].filter(Boolean);
    return parts.join(' · ');
  }
  if (evt === 'start_phase') {
    return r.phase_name ? `phase ${r.phase}: ${r.phase_name}` : '';
  }
  return entry.detail || r.detail || r.reason || '';
}

function entryColorClass(event: string): string {
  if (event === 'LLM_DIRECT' || event === 'LLM_DIRECT_STREAM') return 'text-cyan-400';
  if (event === 'TOOL_CALL') return 'text-emerald-400';
  if (event.includes('MEDIATION')) return 'text-amber-400';
  if (event.includes('ERROR')) return 'text-red-400';
  return 'text-foreground/80';
}

function formatTime(ts: string): string {
  if (!ts) return '';
  try {
    const d = new Date(ts);
    return d.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
  } catch {
    return ts;
  }
}

function entryDetail(entry: AuditEntry): { primary: string; secondary: string } {
  const r = entry.raw;
  const evt = entry.event || r.type || '';

  if (evt === 'LLM_DIRECT' || evt === 'LLM_DIRECT_STREAM') {
    const primary = [
      r.model,
      r.input_tokens != null ? `in:${Number(r.input_tokens).toLocaleString()}` : null,
      r.output_tokens != null ? `out:${Number(r.output_tokens).toLocaleString()}` : null,
    ].filter(Boolean).join('  ');
    const cost = r.cost != null ? `$${Number(r.cost).toFixed(4)}` : '';
    const dur = r.duration_ms ? `${(r.duration_ms / 1000).toFixed(1)}s` : '';
    const secondary = [dur, cost].filter(Boolean).join('  ');
    return { primary, secondary };
  }

  if (evt === 'TOOL_CALL') {
    const toolName = r.tool || r.name || '';
    const args = r.args
      ? Object.entries(r.args as Record<string, unknown>)
          .slice(0, 2)
          .map(([k, v]) => `${k}=${JSON.stringify(v)}`)
          .join(' ')
      : '';
    return { primary: toolName, secondary: args };
  }

  if (evt.includes('HTTP_PROXY') || evt.includes('MEDIATION')) {
    const method = r.method || '';
    const endpoint = r.path || r.url || r.host || '';
    const status = r.status ? String(r.status) : '';
    return { primary: [method, endpoint].filter(Boolean).join(' '), secondary: status };
  }

  const summary = auditSummary(entry);
  return { primary: summary, secondary: '' };
}

export function AdminAudit() {
  const [agents, setAgents] = useState<Array<{ name: string }>>([]);
  const [selectedAgent, setSelectedAgent] = useState<string>('_all');
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [typeFilter, setTypeFilter] = useState<string>('__all__');

  // Audit summarize
  const [summarizing, setSummarizing] = useState(false);
  const [summaryData, setSummaryData] = useState<any>(null);

  const loadAgents = useCallback(async () => {
    try {
      const raw = await api.agents.list();
      const mapped = (raw ?? []).filter((a: any) => a.name).map((a: any) => ({ name: a.name }));
      setAgents(mapped);
    } catch {
      setAgents([]);
    }
  }, []);

  const loadEntries = useCallback(async (agent: string) => {
    try {
      setLoading(true);
      setError(null);
      const raw = await api.admin.audit(agent);
      const list = Array.isArray(raw) ? raw : (raw as any)?.entries ?? [];
      const mapped: AuditEntry[] = list.map((e: any) => ({
        timestamp: e.timestamp || e.ts || '',
        event: e.event || e.type || '',
        detail: e.detail || '',
        raw: e,
      }));
      setEntries(mapped);
    } catch (e: any) {
      setError(e.message || 'Failed to load audit log');
      setEntries([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadAgents();
  }, [loadAgents]);

  useEffect(() => {
    loadEntries(selectedAgent);
  }, [selectedAgent, loadEntries]);

  const handleAuditSummarize = async () => {
    try {
      setSummarizing(true);
      const data = await api.admin.auditSummarize();
      setSummaryData(data);
      toast.success(`Audit summarized: ${data.count ?? 0} metrics`);
    } catch (e: any) {
      toast.error(e.message || 'Audit summarization failed');
    } finally {
      setSummarizing(false);
    }
  };

  const uniqueTypes = [...new Set(entries.map((e) => e.event).filter(Boolean))].sort();

  const filtered = entries.filter((entry) => {
    if (typeFilter !== '__all__' && entry.event !== typeFilter) return false;
    return true;
  });

  return (
    <div className="space-y-4">
      {/* Filter bar */}
      <div className="flex flex-wrap items-center gap-3">
        {/* Agent selector */}
        <Select value={selectedAgent} onValueChange={setSelectedAgent}>
          <SelectTrigger className="w-full sm:w-48 bg-card border-border">
            <SelectValue placeholder="All agents" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="_all">All agents</SelectItem>
            {agents.map((agent) => (
              <SelectItem key={agent.name} value={agent.name}>
                {agent.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        {/* Type selector */}
        <Select value={typeFilter} onValueChange={setTypeFilter}>
          <SelectTrigger className="w-full sm:w-48 bg-card border-border">
            <SelectValue placeholder="All types" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">All types</SelectItem>
            {uniqueTypes.map((t) => (
              <SelectItem key={t} value={t}>
                {t}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        {/* Entry count */}
        <span className="text-xs text-muted-foreground ml-auto">
          {filtered.length} {filtered.length === 1 ? 'entry' : 'entries'}
        </span>

        <Button
          variant="outline"
          size="sm"
          onClick={handleAuditSummarize}
          disabled={summarizing}
        >
          {summarizing ? 'Summarizing...' : 'Summarize'}
        </Button>

        <ExportButton data={filtered} filename={`audit-${selectedAgent}`} format="csv" />

        <Button
          variant="outline"
          size="sm"
          onClick={() => loadEntries(selectedAgent)}
          disabled={loading}
        >
          <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
        </Button>
      </div>

      {error && (
        <div className="text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
          {error}
        </div>
      )}

      {summaryData && (
        <div className="bg-card border border-border rounded p-4">
          <div className="flex items-center justify-between mb-2">
            <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
              Audit Summary ({summaryData.count ?? 0} metrics)
            </h4>
            <Button variant="ghost" size="sm" className="h-6 text-[10px]" onClick={() => setSummaryData(null)}>
              Dismiss
            </Button>
          </div>
          <JsonView data={summaryData.metrics || summaryData} />
        </div>
      )}

      {/* Log viewer */}
      <div className="bg-card border border-border rounded">
        {loading ? (
          <div className="text-muted-foreground text-center py-8 text-sm">Loading audit log...</div>
        ) : filtered.length === 0 ? (
          <div className="text-muted-foreground text-center py-8 text-sm">No audit entries found</div>
        ) : (
          <div className="max-h-[600px] overflow-y-auto">
            {filtered.map((entry, i) => {
              const colorClass = entryColorClass(entry.event);
              const { primary, secondary } = entryDetail(entry);
              const agentName = entry.raw.agent || entry.raw.agent_name || '';
              return (
                <div
                  key={i}
                  className="flex items-baseline gap-3 px-3 py-1 font-mono text-xs hover:bg-secondary/30 transition-colors border-b border-border/40 last:border-0"
                >
                  {/* Time */}
                  <span className="shrink-0 text-muted-foreground/70 w-20">
                    {formatTime(entry.timestamp)}
                  </span>
                  {/* Agent */}
                  {agentName && (
                    <span className="shrink-0 text-muted-foreground w-24 truncate">
                      {agentName}
                    </span>
                  )}
                  {/* Type */}
                  <span className={`shrink-0 w-44 truncate font-semibold ${colorClass}`}>
                    {entry.event}
                  </span>
                  {/* Detail */}
                  <span className="flex-1 truncate text-foreground/80">
                    {primary}
                    {secondary && (
                      <span className="text-muted-foreground ml-3">{secondary}</span>
                    )}
                  </span>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
