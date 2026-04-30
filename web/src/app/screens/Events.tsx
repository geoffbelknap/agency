import { Fragment, useState, useEffect, useCallback, useMemo } from 'react';
import { Link } from 'react-router';
import { api, RawEvent } from '../lib/api';
import { Button } from '../components/ui/button';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '../components/ui/card';
import { Input } from '../components/ui/input';
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
      <Card>
        <CardHeader>
          <CardTitle>Event filters</CardTitle>
          <CardDescription>Scope the operational feed by source, event type, or runtime owner.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-1 flex-wrap items-center gap-2 lg:justify-end">
            <Select value={sourceTypeFilter} onValueChange={setSourceTypeFilter}>
              <SelectTrigger className="h-8 w-[140px] text-xs">
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
              <SelectTrigger className="h-8 w-[160px] text-xs">
                <SelectValue placeholder="All agents" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All agents</SelectItem>
                {uniqueAgents.map((a) => (
                  <SelectItem key={a} value={a}>{a}</SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Input
              type="text"
              value={eventTypeFilter}
              onChange={(e) => setEventTypeFilter(e.target.value)}
              placeholder="Filter by event type..."
              className="h-8 min-w-0 flex-1 text-xs sm:min-w-[200px]"
            />

            <Button
              variant="outline"
              size="sm"
              onClick={loadEvents}
              disabled={loading}
              className="h-8"
            >
              <RefreshCw className={`mr-1.5 h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
              Refresh
            </Button>
        </CardContent>
      </Card>

      {error && (
        <div className="text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded px-3 py-2">
          {error}
        </div>
      )}

      {!loading && !error && attentionEvents.length > 0 && (
        <Card className="border-amber-900/40 bg-amber-950/20">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm text-amber-300">
                <AlertTriangle className="h-4 w-4" />
                {attentionEvents.length} recent event{attentionEvents.length !== 1 ? 's' : ''} need attention
            </CardTitle>
            <CardDescription>
                {errorCount > 0 && `${errorCount} error${errorCount !== 1 ? 's' : ''}`}
                {errorCount > 0 && warningCount > 0 && ' · '}
                {warningCount > 0 && `${warningCount} warning${warningCount !== 1 ? 's' : ''}`}
                {' · '}
                Review the recent feed and jump straight to the most relevant recovery surface.
            </CardDescription>
            <CardAction className="flex flex-wrap gap-2">
              {primaryAttentionAction && (
                <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                  <Link to={primaryAttentionAction.href}>{primaryAttentionAction.label}</Link>
                </Button>
              )}
              <Button asChild variant="outline" size="sm" className="h-8 text-xs">
                <Link to="/admin/doctor">Open Doctor</Link>
              </Button>
            </CardAction>
          </CardHeader>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Event feed</CardTitle>
          <CardDescription>Recent runtime, connector, channel, and webhook activity with expandable payload detail.</CardDescription>
          <CardAction>
            {!loading && !error && (
              <div className="text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                {filteredEvents.length} item{filteredEvents.length !== 1 ? 's' : ''}
              </div>
            )}
          </CardAction>
        </CardHeader>
        <CardContent className="p-0">
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
                    className="flex cursor-pointer items-center gap-3 px-4 py-3 transition-colors hover:bg-secondary/30"
                    onClick={() => toggleEvent(event.id)}
                  >
                    <span
                      className={`h-1.5 w-1.5 flex-none rounded-full ${severityDotClass(event.event_type)}`}
                    />
                    <div className="min-w-0 flex-none" style={{ maxWidth: 168 }}>
                      <div
                        className="truncate text-xs font-medium text-foreground"
                        title={event.source_name || event.source_type}
                      >
                        {event.source_name || event.source_type}
                      </div>
                      <div className="text-[11px] text-muted-foreground">{event.source_type}</div>
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="text-xs font-medium text-foreground">{event.event_type}</div>
                      {detail ? (
                        <div className="truncate text-xs text-muted-foreground/80">
                          {detail}
                        </div>
                      ) : (
                        <div className="text-xs text-muted-foreground/60">No inline detail</div>
                      )}
                    </div>
                    {detail && (
                      <span className="sr-only">{detail}</span>
                    )}
                    <span className="flex-none text-xs text-muted-foreground/60 whitespace-nowrap">
                      {formatRelative(event.timestamp)}
                    </span>
                  </div>

                  {/* Expanded detail */}
                  {isExpanded && (
                    <div className="space-y-3 bg-secondary/20 px-4 py-3">
                      <div className="rounded-xl border border-amber-900/40 bg-amber-950/20 px-3 py-3 text-xs">
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
                          <div className="mb-1.5 text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">Data</div>
                          <pre className="overflow-x-auto rounded-xl border border-border bg-background/60 p-3 text-xs text-foreground/80 whitespace-pre-wrap break-words">
                            {JSON.stringify(event.data, null, 2)}
                          </pre>
                        </div>
                      )}
                      {event.metadata && Object.keys(event.metadata).length > 0 && (
                        <div>
                          <div className="mb-1.5 text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground">Metadata</div>
                          <pre className="overflow-x-auto rounded-xl border border-border bg-background/60 p-3 text-xs text-foreground/80 whitespace-pre-wrap break-words">
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
        </CardContent>
      </Card>

      <Card>
        <button
          className="flex w-full items-center gap-2 p-4 text-left transition-colors hover:bg-secondary/50"
          onClick={() => setSubsOpen((prev) => !prev)}
        >
          {subsOpen ? (
            <ChevronDown className="w-4 h-4 text-muted-foreground" />
          ) : (
            <ChevronRight className="w-4 h-4 text-muted-foreground" />
          )}
          <div>
            <div className="text-sm font-medium text-foreground">Active subscriptions</div>
            <div className="text-xs text-muted-foreground">Current event bus subscribers and their live scopes.</div>
          </div>
        </button>
        {subsOpen && (
          <CardContent className="border-t border-border p-4">
            {subsLoading ? (
              <div className="text-sm text-muted-foreground text-center py-4">Loading subscriptions...</div>
            ) : subscriptions.length === 0 ? (
              <div className="text-sm text-muted-foreground text-center py-4">No active subscriptions</div>
            ) : (
              <JsonView data={subscriptions} defaultExpanded />
            )}
          </CardContent>
        )}
      </Card>
    </div>
  );
}
