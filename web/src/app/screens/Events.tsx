import { Fragment, useState, useEffect, useCallback, useMemo } from 'react';
import { Link } from 'react-router';
import { api, RawEvent } from '../lib/api';
import { Button } from '../components/ui/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '../components/ui/select';
import { AlertTriangle, ChevronDown, ChevronRight, RefreshCw } from 'lucide-react';
import { JsonView } from '../components/JsonView';

const SOURCE_TYPES = ['connector', 'channel', 'schedule', 'webhook', 'platform'];

function severityDotClass(eventType: string): string {
  const t = eventType.toLowerCase();
  if (t.includes('error')) return 'bg-red-500';
  if (t.includes('warning')) return 'bg-amber-500';
  return 'bg-emerald-500';
}

function isAttentionEvent(eventType: string): boolean {
  const t = eventType.toLowerCase();
  return t.includes('error') || t.includes('warning');
}

function eventActionFor(event: RawEvent): { label: string; href: string } | null {
  if (event.source_type === 'connector') return { label: 'Open Intake', href: '/intake' };
  if (event.source_type === 'channel' && event.source_name) return { label: 'Open Channel', href: `/channels/${event.source_name}` };
  if (event.source_type === 'webhook') return { label: 'Open Webhooks', href: '/admin/webhooks' };
  if (event.source_type === 'platform') return { label: 'Open Infrastructure', href: '/admin/infrastructure' };
  return null;
}

function eventRecoveryHint(event: RawEvent): string {
  if (event.source_type === 'connector') {
    return 'Connector-sourced failures usually mean intake setup, credentials, or route handling need review first.';
  }
  if (event.source_type === 'webhook') {
    return 'Webhook failures usually mean delivery configuration or upstream sender verification needs review first.';
  }
  if (event.source_type === 'channel') {
    return 'Channel-sourced failures usually mean the conversation target or downstream handling path needs review first.';
  }
  if (event.source_type === 'platform') {
    return 'Platform failures usually mean infrastructure health or gateway state needs review first.';
  }
  return 'Review the event payload, then open Doctor if the right next surface is still unclear.';
}

function formatRelative(ts: string | undefined | null): string {
  if (!ts) return '';
  const d = new Date(ts);
  if (isNaN(d.getTime())) return ts;
  const diffMs = Date.now() - d.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  if (diffSec < 60) return `${diffSec}s ago`;
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h ago`;
  const diffDay = Math.floor(diffHr / 24);
  return `${diffDay}d ago`;
}

function briefDetail(data?: Record<string, unknown>): string {
  if (!data) return '';
  // Try common informative fields first
  for (const key of ['message', 'summary', 'reason', 'status', 'description']) {
    const val = data[key];
    if (typeof val === 'string' && val.trim()) {
      return val.length > 80 ? val.slice(0, 80) + '…' : val;
    }
  }
  return '';
}

export function Events() {
  const [events, setEvents] = useState<RawEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [sourceTypeFilter, setSourceTypeFilter] = useState<string>('__all__');
  const [eventTypeFilter, setEventTypeFilter] = useState('');
  const [agentFilter, setAgentFilter] = useState('all');

  const [expandedEvent, setExpandedEvent] = useState<string | null>(null);

  const [subscriptions, setSubscriptions] = useState<Record<string, unknown>[]>([]);
  const [subsLoading, setSubsLoading] = useState(false);
  const [subsOpen, setSubsOpen] = useState(false);

  const loadEvents = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const opts: { limit?: number; source_type?: string; event_type?: string } = { limit: 200 };
      if (sourceTypeFilter !== '__all__') opts.source_type = sourceTypeFilter;
      if (eventTypeFilter.trim()) opts.event_type = eventTypeFilter.trim();
      const data = await api.events.list(opts);
      setEvents(Array.isArray(data) ? data : []);
    } catch (e: any) {
      const msg = e.message || '';
      if (msg.includes('404') || msg.includes('503')) {
        setError('Event bus is not available on this gateway build.');
      } else {
        setError(msg || 'Failed to load events');
      }
      setEvents([]);
    } finally {
      setLoading(false);
    }
  }, [sourceTypeFilter, eventTypeFilter]);

  const loadSubscriptions = useCallback(async () => {
    try {
      setSubsLoading(true);
      const data = await api.events.subscriptions();
      setSubscriptions(Array.isArray(data) ? data : []);
    } catch {
      setSubscriptions([]);
    } finally {
      setSubsLoading(false);
    }
  }, []);

  useEffect(() => {
    loadEvents();
  }, [loadEvents]);

  useEffect(() => {
    if (subsOpen) {
      loadSubscriptions();
    }
  }, [subsOpen, loadSubscriptions]);

  const toggleEvent = (id: string) => {
    setExpandedEvent((prev) => (prev === id ? null : id));
  };

  const uniqueAgents = [
    ...new Set(
      events
        .map((e) => e.source_name || e.source_type)
        .filter(Boolean)
    ),
  ];

  const filteredEvents = agentFilter === 'all'
    ? events
    : events.filter((e) => (e.source_name || e.source_type) === agentFilter);
  const attentionEvents = useMemo(
    () => filteredEvents.filter((event) => isAttentionEvent(event.event_type)),
    [filteredEvents],
  );
  const warningCount = attentionEvents.filter((event) => event.event_type.toLowerCase().includes('warning')).length;
  const errorCount = attentionEvents.filter((event) => event.event_type.toLowerCase().includes('error')).length;
  const primaryAttentionAction = attentionEvents.map(eventActionFor).find((action): action is { label: string; href: string } => Boolean(action));

  return (
    <div className="space-y-6">
      {/* Header row with filters */}
      <div className="flex flex-wrap items-center gap-2">
        <Select value={sourceTypeFilter} onValueChange={setSourceTypeFilter}>
          <SelectTrigger className="w-[140px] h-8 text-xs">
            <SelectValue placeholder="All sources" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">All sources</SelectItem>
            {SOURCE_TYPES.map((st) => (
              <SelectItem key={st} value={st}>{st}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={agentFilter} onValueChange={setAgentFilter}>
          <SelectTrigger className="w-[160px] h-8 text-xs">
            <SelectValue placeholder="All agents" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All agents</SelectItem>
            {uniqueAgents.map((a) => (
              <SelectItem key={a} value={a}>{a}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        <input
          type="text"
          value={eventTypeFilter}
          onChange={(e) => setEventTypeFilter(e.target.value)}
          placeholder="Filter by event type..."
          className="flex-1 min-w-0 sm:min-w-[180px] bg-card border border-border text-foreground rounded px-3 py-1 text-xs placeholder:text-muted-foreground/70 h-8"
        />

        <Button
          variant="outline"
          size="sm"
          onClick={loadEvents}
          disabled={loading}
          className="h-8"
        >
          <RefreshCw className={`w-3.5 h-3.5 mr-1.5 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>

      {error && (
        <div className="text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
          {error}
        </div>
      )}

      {!loading && !error && attentionEvents.length > 0 && (
        <div className="rounded-lg border border-amber-900/50 bg-amber-950/20 p-4">
          <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
            <div className="space-y-1">
              <div className="flex items-center gap-2 text-sm font-medium text-amber-300">
                <AlertTriangle className="h-4 w-4" />
                {attentionEvents.length} recent event{attentionEvents.length !== 1 ? 's' : ''} need attention
              </div>
              <p className="text-xs text-muted-foreground">
                {errorCount > 0 && `${errorCount} error${errorCount !== 1 ? 's' : ''}`}
                {errorCount > 0 && warningCount > 0 && ' · '}
                {warningCount > 0 && `${warningCount} warning${warningCount !== 1 ? 's' : ''}`}
                {' · '}
                Review the recent feed and jump straight to the most relevant recovery surface.
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              {primaryAttentionAction && (
                <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                  <Link to={primaryAttentionAction.href}>{primaryAttentionAction.label}</Link>
                </Button>
              )}
              <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                <Link to="/admin/doctor">Open Doctor</Link>
              </Button>
            </div>
          </div>
        </div>
      )}

      {/* Event feed */}
      <div className="bg-card border border-border rounded overflow-hidden">
        {loading ? (
          <div className="text-muted-foreground text-center py-8 text-sm">Loading events...</div>
        ) : filteredEvents.length === 0 ? (
          <div className="text-muted-foreground text-center py-8 text-sm">No events recorded</div>
        ) : (
          <div className="max-h-[540px] overflow-y-auto divide-y divide-border">
            {filteredEvents.map((event) => {
              const detail = briefDetail(event.data);
              const isExpanded = expandedEvent === event.id;
              return (
                <Fragment key={event.id}>
                  <div
                    className="flex items-center gap-2.5 px-3 py-2 cursor-pointer hover:bg-secondary/30 transition-colors"
                    onClick={() => toggleEvent(event.id)}
                  >
                    {/* Severity dot */}
                    <span
                      className={`flex-none w-1.5 h-1.5 rounded-full ${severityDotClass(event.event_type)}`}
                    />

                    {/* Source name */}
                    <span
                      className="flex-none font-medium text-foreground text-xs truncate"
                      style={{ maxWidth: 140 }}
                      title={event.source_name || event.source_type}
                    >
                      {event.source_name || event.source_type}
                    </span>

                    {/* Event type */}
                    <span className="flex-none text-xs text-muted-foreground">
                      {event.event_type}
                    </span>

                    {/* Brief detail */}
                    {detail && (
                      <span className="flex-1 min-w-0 text-xs text-muted-foreground/70 truncate">
                        {detail}
                      </span>
                    )}
                    {!detail && <span className="flex-1" />}

                    {/* Relative timestamp */}
                    <span className="flex-none text-xs text-muted-foreground/60 whitespace-nowrap">
                      {formatRelative(event.timestamp)}
                    </span>
                  </div>

                  {/* Expanded detail */}
                  {isExpanded && (
                    <div className="bg-secondary/20 px-4 py-3 space-y-3">
                      <div className="rounded border border-amber-900/40 bg-amber-950/20 px-3 py-2 text-xs">
                        <div className="font-medium text-amber-300">Likely next step</div>
                        <div className="mt-1 text-muted-foreground">{eventRecoveryHint(event)}</div>
                        <div className="mt-2 flex flex-wrap gap-2">
                          {eventActionFor(event) && (
                            <Button asChild variant="outline" size="sm" className="h-7 text-xs">
                              <Link to={eventActionFor(event)!.href}>{eventActionFor(event)!.label}</Link>
                            </Button>
                          )}
                          <Button asChild variant="outline" size="sm" className="h-7 text-xs">
                            <Link to="/admin/doctor">Open Doctor</Link>
                          </Button>
                        </div>
                      </div>
                      {event.data && Object.keys(event.data).length > 0 && (
                        <div>
                          <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-1.5">Data</div>
                          <pre className="text-xs text-foreground/80 bg-background/60 border border-border rounded p-3 overflow-x-auto whitespace-pre-wrap break-words">
                            {JSON.stringify(event.data, null, 2)}
                          </pre>
                        </div>
                      )}
                      {event.metadata && Object.keys(event.metadata).length > 0 && (
                        <div>
                          <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-1.5">Metadata</div>
                          <pre className="text-xs text-foreground/80 bg-background/60 border border-border rounded p-3 overflow-x-auto whitespace-pre-wrap break-words">
                            {JSON.stringify(event.metadata, null, 2)}
                          </pre>
                        </div>
                      )}
                      {(!event.data || Object.keys(event.data).length === 0) &&
                       (!event.metadata || Object.keys(event.metadata).length === 0) && (
                        <div className="text-xs text-muted-foreground">No additional data</div>
                      )}
                    </div>
                  )}
                </Fragment>
              );
            })}
          </div>
        )}
      </div>

      {/* Subscriptions section */}
      <div className="bg-card border border-border rounded">
        <button
          className="w-full flex items-center gap-2 p-4 text-left hover:bg-secondary/50 transition-colors"
          onClick={() => setSubsOpen((prev) => !prev)}
        >
          {subsOpen ? (
            <ChevronDown className="w-4 h-4 text-muted-foreground" />
          ) : (
            <ChevronRight className="w-4 h-4 text-muted-foreground" />
          )}
          <span className="text-sm font-medium text-foreground">Active Subscriptions</span>
        </button>
        {subsOpen && (
          <div className="border-t border-border p-4">
            {subsLoading ? (
              <div className="text-sm text-muted-foreground text-center py-4">Loading subscriptions...</div>
            ) : subscriptions.length === 0 ? (
              <div className="text-sm text-muted-foreground text-center py-4">No active subscriptions</div>
            ) : (
              <JsonView data={subscriptions} defaultExpanded />
            )}
          </div>
        )}
      </div>
    </div>
  );
}
