# Agency Web UI Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** 7/8 complete. Section 8 (Egress domain merge) requires backend API changes.

**Goal:** Redesign 8 agency-web UI components to improve usability as the platform scales.

**Architecture:** Each task modifies agency-web React components. Admin.tsx (939 lines) will be split — Doctor, Audit, and Egress become separate sub-screen components following the existing delegation pattern. All styling uses the existing Mission Control theme (DM Sans, JetBrains Mono, deep slate-blue, cyan accents). No new dependencies.

**Tech Stack:** React 18, Tailwind CSS v4, shadcn/ui (Radix), Recharts, Vitest + RTL + MSW

**Spec:** `agency/docs/specs/platform/2026-03-29-agency-web-redesign.md`

**Working directory:** `/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web`

---

### Task 1: Channel Sidebar — Slack-style Groups

Rewrite the channel sidebar to group channels into Channels, Direct Messages, and Internal sections.

**Files:**
- Modify: `src/app/components/chat/ChannelSidebar.tsx`

- [ ] **Step 1: Read the current ChannelSidebar.tsx**

Read `src/app/components/chat/ChannelSidebar.tsx` (127 lines). Understand the `SidebarContent` inner component, `ChannelSidebarProps` interface, and the mobile/desktop split pattern.

- [ ] **Step 2: Rewrite SidebarContent with grouped sections**

Replace the body of `SidebarContent` (the content inside `ScrollArea`). Keep the same props interface. Replace the flat filtered list with three collapsible sections.

The grouping logic (add above `SidebarContent`):

```tsx
function groupChannels(channels: Channel[]) {
  const groups = { channels: [] as Channel[], dms: [] as Channel[], internal: [] as Channel[] };
  for (const ch of channels) {
    if (ch.type === 'dm' || ch.name.startsWith('dm-')) {
      groups.dms.push(ch);
    } else if (ch.name.startsWith('_')) {
      groups.internal.push(ch);
    } else {
      groups.channels.push(ch);
    }
  }
  return groups;
}
```

The section component (add above `SidebarContent`):

```tsx
function SidebarSection({
  label, channels, selectedChannel, onSelect, defaultOpen = true,
  renderItem,
}: {
  label: string;
  channels: Channel[];
  selectedChannel: string | null;
  onSelect: (name: string) => void;
  defaultOpen?: boolean;
  renderItem?: (ch: Channel, isSelected: boolean) => React.ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  if (channels.length === 0) return null;
  return (
    <div>
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center justify-between px-3 py-1.5 group"
      >
        <span className="text-[10px] uppercase tracking-widest text-muted-foreground/60 font-medium">
          {label}
        </span>
        <ChevronDown className={`w-3 h-3 text-muted-foreground/40 transition-transform ${open ? '' : '-rotate-90'}`} />
      </button>
      {open && (
        <div className="px-1.5 space-y-0.5">
          {channels.map((ch) => {
            const isSelected = ch.name === selectedChannel;
            return renderItem ? renderItem(ch, isSelected) : (
              <button
                key={ch.name}
                onClick={() => onSelect(ch.name)}
                className={`w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md text-[13px] transition-colors ${
                  isSelected ? 'bg-sidebar-accent text-sidebar-accent-foreground' : 'text-sidebar-foreground hover:bg-sidebar-accent/50'
                }`}
              >
                <span className="text-muted-foreground/60">#</span>
                <span className={isSelected ? 'font-medium' : ''}>{ch.name}</span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
```

Replace the current `SidebarContent` body with:

```tsx
function SidebarContent({ channels, selectedChannel, onSelect, onCreateChannel }: /* existing props minus mobile */) {
  const [filter, setFilter] = useState('');
  const filtered = filter ? channels.filter((c) => c.name.toLowerCase().includes(filter.toLowerCase())) : channels;
  const groups = groupChannels(filtered);

  return (
    <div className="flex flex-col h-full">
      <div className="p-2">
        <Input
          placeholder="Filter channels..."
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="h-7 text-xs"
        />
      </div>
      <ScrollArea className="flex-1">
        {/* Channels */}
        <SidebarSection
          label="Channels"
          channels={groups.channels}
          selectedChannel={selectedChannel}
          onSelect={onSelect}
        />
        {onCreateChannel && !filter && (
          <button
            onClick={onCreateChannel}
            className="flex items-center gap-2 px-4 py-1 text-[12px] text-muted-foreground/50 hover:text-muted-foreground transition-colors"
          >
            <Plus className="w-3 h-3" /> Add channel
          </button>
        )}

        <div className="mx-3 my-2 border-t border-sidebar-border" />

        {/* Direct Messages */}
        <SidebarSection
          label="Direct Messages"
          channels={groups.dms}
          selectedChannel={selectedChannel}
          onSelect={onSelect}
          renderItem={(ch, isSelected) => {
            const displayName = ch.name.replace(/^dm-/, '');
            return (
              <button
                key={ch.name}
                onClick={() => onSelect(ch.name)}
                className={`w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md text-[13px] transition-colors ${
                  isSelected ? 'bg-sidebar-accent text-sidebar-accent-foreground' : 'text-sidebar-foreground hover:bg-sidebar-accent/50'
                }`}
              >
                <span className="w-2 h-2 rounded-full bg-emerald-500 flex-shrink-0" />
                <span className={isSelected ? 'font-medium' : ''}>{displayName}</span>
                <span className="ml-auto text-[10px] text-muted-foreground/50 bg-secondary px-1 py-0.5 rounded">AGENT</span>
              </button>
            );
          }}
        />

        {/* Internal */}
        {groups.internal.length > 0 && (
          <>
            <div className="mx-3 my-2 border-t border-sidebar-border" />
            <SidebarSection
              label="Internal"
              channels={groups.internal}
              selectedChannel={selectedChannel}
              onSelect={onSelect}
              defaultOpen={false}
            />
          </>
        )}
      </ScrollArea>

      {/* Build ID footer */}
      <div className="px-3 py-2 border-t border-sidebar-border">
        <span className="text-[10px] font-mono text-muted-foreground/30">
          {typeof __BUILD_ID__ !== 'undefined' ? __BUILD_ID__ : ''}
        </span>
      </div>
    </div>
  );
}
```

Add `ChevronDown` to the lucide imports at the top of the file.

- [ ] **Step 3: Verify it builds**

Run: `cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-web && npm run build 2>&1 | tail -5`
Expected: clean build

- [ ] **Step 4: Run tests**

Run: `npm test -- --run 2>&1 | tail -10`
Expected: all existing tests pass

- [ ] **Step 5: Commit**

```bash
git add src/app/components/chat/ChannelSidebar.tsx
git commit -m "feat: Slack-style channel sidebar with grouped sections"
```

---

### Task 2: Audit Log Viewer

Extract the Audit section from Admin.tsx into a dedicated sub-screen with a monospace log viewer.

**Files:**
- Create: `src/app/screens/AdminAudit.tsx`
- Modify: `src/app/screens/Admin.tsx` (remove inline audit, import new component)
- Modify: `src/app/lib/api.ts` (add audit log endpoint if needed)

- [ ] **Step 1: Read the current Audit section in Admin.tsx**

Read `src/app/screens/Admin.tsx` lines 582–685 to understand the current audit rendering (agent selector, event-type filter, table). Also read the `AuditEntry` interface and `handleAudit` function at lines 30–72 and 179–210.

- [ ] **Step 2: Create AdminAudit.tsx**

Create `src/app/screens/AdminAudit.tsx` as a standalone component:

```tsx
import { useState, useEffect, useCallback } from 'react';
import { api } from '../lib/api';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../components/ui/select';
import { Button } from '../components/ui/button';
import { RefreshCw } from 'lucide-react';

interface AuditEntry {
  ts: string;
  type: string;
  agent?: string;
  model?: string;
  input_tokens?: number;
  output_tokens?: number;
  tool?: string;
  args?: string;
  endpoint?: string;
  method?: string;
  status?: number;
  error?: string;
  cost?: number;
}

const TYPE_COLORS: Record<string, string> = {
  LLM_DIRECT: 'text-cyan-400',
  LLM_DIRECT_STREAM: 'text-cyan-400',
  LLM_DIRECT_ERROR: 'text-red-400',
  TOOL_CALL: 'text-emerald-400',
  TOOL_ERROR: 'text-red-400',
  MEDIATION: 'text-amber-400',
  MEDIATION_ERROR: 'text-red-400',
};

function formatTime(ts: string): string {
  try {
    const d = new Date(ts);
    return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
  } catch {
    return ts;
  }
}

function formatTokens(n?: number): string {
  if (!n) return '';
  if (n >= 1000) return `${(n / 1000).toFixed(1)}K`;
  return String(n);
}

function entryDetail(e: AuditEntry): string {
  if (e.type.startsWith('LLM')) {
    const parts = [e.model || ''];
    if (e.input_tokens) parts.push(`input=${formatTokens(e.input_tokens)}`);
    if (e.output_tokens) parts.push(`output=${formatTokens(e.output_tokens)}`);
    if (e.cost) parts.push(`cost=$${e.cost.toFixed(3)}`);
    if (e.error) parts.push(e.error);
    return parts.filter(Boolean).join(' ');
  }
  if (e.type.startsWith('TOOL')) {
    return [e.tool, e.args].filter(Boolean).join(' ');
  }
  if (e.type.startsWith('MEDIATION')) {
    return [e.method, e.endpoint, e.status ? String(e.status) : ''].filter(Boolean).join(' ');
  }
  return e.error || '';
}

export function AdminAudit() {
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [agents, setAgents] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedAgent, setSelectedAgent] = useState('all');
  const [selectedType, setSelectedType] = useState('all');

  const load = useCallback(async () => {
    setLoading(true);
    try {
      // Load agent list for filter
      const agentList = await api.agents.list();
      setAgents((agentList as any[]).map((a: any) => a.name));

      // Load audit entries for selected agent (or all)
      const agent = selectedAgent === 'all' ? undefined : selectedAgent;
      const data = await api.admin.audit(agent || '_all');
      const parsed = Array.isArray(data) ? data : (data as any).entries || [];
      setEntries(parsed);
    } catch {
      setEntries([]);
    } finally {
      setLoading(false);
    }
  }, [selectedAgent]);

  useEffect(() => { load(); }, [load]);

  const filtered = selectedType === 'all'
    ? entries
    : entries.filter((e) => e.type === selectedType);

  const types = [...new Set(entries.map((e) => e.type))].sort();

  return (
    <div className="space-y-3">
      {/* Filter bar */}
      <div className="flex items-center gap-3">
        <Select value={selectedAgent} onValueChange={(v) => setSelectedAgent(v)}>
          <SelectTrigger className="w-[180px] h-8 text-xs">
            <SelectValue placeholder="All agents" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All agents</SelectItem>
            {agents.map((a) => <SelectItem key={a} value={a}>{a}</SelectItem>)}
          </SelectContent>
        </Select>

        <Select value={selectedType} onValueChange={(v) => setSelectedType(v)}>
          <SelectTrigger className="w-[180px] h-8 text-xs">
            <SelectValue placeholder="All types" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All types</SelectItem>
            {types.map((t) => <SelectItem key={t} value={t}>{t}</SelectItem>)}
          </SelectContent>
        </Select>

        <span className="ml-auto text-xs text-muted-foreground">
          {filtered.length} entries
        </span>
        <Button variant="outline" size="sm" className="h-8" onClick={load} disabled={loading}>
          <RefreshCw className={`w-3 h-3 ${loading ? 'animate-spin' : ''}`} />
        </Button>
      </div>

      {/* Log viewer */}
      <div className="bg-card border border-border rounded overflow-hidden">
        <div className="max-h-[600px] overflow-y-auto p-4 font-mono text-xs leading-relaxed">
          {loading ? (
            <div className="text-muted-foreground text-center py-8">Loading audit log...</div>
          ) : filtered.length === 0 ? (
            <div className="text-muted-foreground text-center py-8">No audit entries</div>
          ) : (
            filtered.map((e, i) => (
              <div key={i} className="flex gap-3 hover:bg-secondary/30 px-1 -mx-1 rounded">
                <span className="text-muted-foreground/60 flex-shrink-0">{formatTime(e.ts)}</span>
                <span className="text-cyan-300 flex-shrink-0 w-[120px] truncate">{e.agent || '_unknown'}</span>
                <span className={`flex-shrink-0 w-[120px] ${TYPE_COLORS[e.type] || 'text-muted-foreground'}`}>{e.type}</span>
                <span className="text-muted-foreground truncate">{entryDetail(e)}</span>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Replace inline Audit in Admin.tsx**

In Admin.tsx, add `import { AdminAudit } from './AdminAudit';` at the top.

Find the `<TabsContent value="audit">` section (around lines 582–685) and replace its contents with:

```tsx
<TabsContent value="audit" className="space-y-4 mt-0">
  <AdminAudit />
</TabsContent>
```

Remove the `AuditEntry` interface, `auditSummary()` helper, audit state variables, and `handleAudit` callback that are now unused.

- [ ] **Step 4: Verify it builds**

Run: `npm run build 2>&1 | tail -5`
Expected: clean build

- [ ] **Step 5: Run tests**

Run: `npm test -- --run 2>&1 | tail -10`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add src/app/screens/AdminAudit.tsx src/app/screens/Admin.tsx
git commit -m "feat: extract Audit to monospace log viewer with agent/type filters"
```

---

### Task 3: Intake — Expandable Connectors + Clickable Work Items

Redesign the Intake tab with expandable connector rows and clickable work item detail.

**Files:**
- Modify: `src/app/screens/Intake.tsx`

- [ ] **Step 1: Read current Intake.tsx**

Read `src/app/screens/Intake.tsx` (415 lines). Understand the Connectors table (lines 150–325) and Work Items table (lines 327–410).

- [ ] **Step 2: Rewrite the Connectors tab**

Replace the Connectors tab content (inside `<TabsContent value="connectors">`) with expandable rows. Add state for expanded connector:

```tsx
const [expandedConnector, setExpandedConnector] = useState<string | null>(null);
```

Replace the connectors table with:

```tsx
<div className="bg-card border border-border rounded overflow-hidden divide-y divide-border">
  {connectors.map((conn) => {
    const isExpanded = expandedConnector === conn.name;
    return (
      <div key={conn.name}>
        <button
          onClick={() => setExpandedConnector(isExpanded ? null : conn.name)}
          className="w-full flex items-center gap-3 px-4 py-3 hover:bg-secondary/30 transition-colors text-left"
        >
          <span className={`w-2 h-2 rounded-full flex-shrink-0 ${conn.state === 'active' ? 'bg-emerald-500' : 'bg-muted-foreground/30'}`} />
          <div className="flex-1 min-w-0">
            <div className="font-medium text-foreground text-sm">{conn.name}</div>
            <div className="text-xs text-muted-foreground">
              {conn.kind}
              {conn.source && <> &middot; {conn.source}</>}
            </div>
          </div>
          {conn.version && (
            <span className="text-[10px] font-mono text-muted-foreground bg-secondary px-1.5 py-0.5 rounded">{conn.version}</span>
          )}
          <ChevronDown className={`w-4 h-4 text-muted-foreground transition-transform ${isExpanded ? 'rotate-180' : ''}`} />
        </button>
        {isExpanded && (
          <div className="bg-background px-4 py-3 border-t border-border">
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4 text-sm mb-3">
              <div>
                <div className="text-[10px] uppercase tracking-wide text-muted-foreground/60 mb-1">Source</div>
                <div className="text-foreground">{conn.source || '—'}</div>
              </div>
              <div>
                <div className="text-[10px] uppercase tracking-wide text-muted-foreground/60 mb-1">State</div>
                <div className="text-foreground">{conn.state}</div>
              </div>
              <div>
                <div className="text-[10px] uppercase tracking-wide text-muted-foreground/60 mb-1">Version</div>
                <div className="text-foreground font-mono">{conn.version || '—'}</div>
              </div>
            </div>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => handleSetup(conn.name)}>Setup</Button>
              <Button variant="outline" size="sm" className="h-7 text-xs text-destructive" onClick={() => handleToggle(conn)}>
                {conn.state === 'active' ? 'Deactivate' : 'Activate'}
              </Button>
            </div>
          </div>
        )}
      </div>
    );
  })}
</div>
```

Add `ChevronDown` to lucide imports.

- [ ] **Step 3: Rewrite the Work Items tab**

Replace the Work Items tab content. Add expanded work item state:

```tsx
const [expandedItem, setExpandedItem] = useState<string | null>(null);
```

Replace the work items section with:

```tsx
{/* Stats bar */}
<div className="grid grid-cols-2 md:grid-cols-4 gap-3 mb-4">
  {[
    { label: 'Total', value: workItems.length, color: 'text-foreground' },
    { label: 'Routed', value: workItems.filter((w) => w.status === 'routed' || w.status === 'assigned').length, color: 'text-emerald-400' },
    { label: 'Relayed', value: workItems.filter((w) => w.status === 'relayed').length, color: 'text-cyan-400' },
    { label: 'Unrouted', value: workItems.filter((w) => w.status === 'unrouted').length, color: 'text-muted-foreground' },
  ].map(({ label, value, color }) => (
    <div key={label} className="bg-card border border-border rounded p-3">
      <div className="text-[10px] uppercase tracking-wide text-muted-foreground/60">{label}</div>
      <div className={`text-xl font-semibold ${color}`}>{value}</div>
    </div>
  ))}
</div>

{/* Work item rows */}
<div className="bg-card border border-border rounded overflow-hidden divide-y divide-border">
  {workItems.length === 0 ? (
    <div className="text-sm text-muted-foreground text-center py-8">No work items</div>
  ) : (
    workItems.map((item) => {
      const isExpanded = expandedItem === item.id;
      return (
        <div key={item.id}>
          <button
            onClick={() => setExpandedItem(isExpanded ? null : item.id)}
            className="w-full flex items-center gap-3 px-4 py-2.5 hover:bg-secondary/30 transition-colors text-left text-sm"
          >
            <span className="font-medium text-foreground">{item.connector}</span>
            <span className={`text-[10px] px-1.5 py-0.5 rounded ${
              item.status === 'routed' || item.status === 'assigned' ? 'bg-emerald-950 text-emerald-400' :
              item.status === 'relayed' ? 'bg-cyan-950 text-cyan-400' :
              'bg-secondary text-muted-foreground'
            }`}>{item.status}</span>
            {item.target_name && <span className="text-xs text-muted-foreground">→ {item.target_name}</span>}
            <span className="ml-auto text-xs text-muted-foreground/60">{item.created_at ? new Date(item.created_at).toLocaleTimeString() : ''}</span>
          </button>
          {isExpanded && (
            <div className="bg-background px-4 py-3 border-t border-border">
              <pre className="text-xs font-mono text-muted-foreground bg-background border border-border rounded p-3 overflow-x-auto max-h-[300px] overflow-y-auto">
                {JSON.stringify(item.payload ? JSON.parse(item.payload) : item, null, 2)}
              </pre>
            </div>
          )}
        </div>
      );
    })
  )}
</div>
```

- [ ] **Step 4: Verify build and tests**

Run: `npm run build 2>&1 | tail -5 && npm test -- --run 2>&1 | tail -10`
Expected: clean build, all tests pass

- [ ] **Step 5: Commit**

```bash
git add src/app/screens/Intake.tsx
git commit -m "feat: expandable connector rows and clickable work item detail"
```

---

### Task 4: Hub — Update/Upgrade UX

Add Homebrew-style upgrade banner and "Upgrade All" button to the Hub tab.

**Files:**
- Modify: `src/app/screens/Hub.tsx`
- Modify: `src/app/lib/api.ts`

- [ ] **Step 1: Add API types and methods**

In `src/app/lib/api.ts`, add after the existing `hub.update()`:

```typescript
    upgrade: (components?: string[]) => {
      const body = components ? { components } : {};
      return req<{ files?: any[]; components?: any[]; warnings?: string[] }>('/hub/upgrade', {
        method: 'POST', body: JSON.stringify(body),
      });
    },
    outdated: () => req<any[]>('/hub/outdated'),
```

- [ ] **Step 2: Add upgrade state and banner to Hub.tsx**

In Hub.tsx, add state:

```tsx
const [updateReport, setUpdateReport] = useState<any>(null);
const [upgrading, setUpgrading] = useState(false);
```

Update `handleUpdateSources` to store the report:

```tsx
const handleUpdateSources = async () => {
  try {
    setUpdatingSources(true);
    const report = await api.hub.update();
    setUpdateReport(report);
    if (report?.available?.length > 0) {
      toast.success(`${report.available.length} upgrade(s) available`);
    } else {
      toast.success('Hub sources up to date');
    }
    await handleSearch();
  } catch (e: any) {
    toast.error(e.message || 'Update failed');
  } finally {
    setUpdatingSources(false);
  }
};
```

Add `handleUpgrade`:

```tsx
const handleUpgrade = async (components?: string[]) => {
  try {
    setUpgrading(true);
    const report = await api.hub.upgrade(components);
    const upgraded = (report.components || []).filter((c: any) => c.status === 'upgraded');
    if (upgraded.length > 0) {
      toast.success(`Upgraded ${upgraded.length} component(s)`);
    } else {
      toast.success('Everything up to date');
    }
    setUpdateReport(null);
    await loadInstalled();
    await handleSearch();
  } catch (e: any) {
    toast.error(e.message || 'Upgrade failed');
  } finally {
    setUpgrading(false);
  }
};
```

- [ ] **Step 3: Add upgrade banner and button to the UI**

In the Browse tab, after the search/filter bar and before the results grid, add:

```tsx
{/* Upgrade banner */}
{updateReport?.available?.length > 0 && (
  <div className="bg-green-950/30 border border-green-900/50 rounded-lg px-4 py-3 flex items-center gap-3">
    <div className="flex-1">
      <span className="text-emerald-400 font-medium text-sm">
        {updateReport.available.length} upgrade{updateReport.available.length > 1 ? 's' : ''} available
      </span>
      <span className="text-muted-foreground text-xs ml-2">
        {updateReport.available.map((u: any) =>
          u.kind === 'managed' ? `${u.name} ${u.summary}` : `${u.name} ${u.installed_version} → ${u.available_version}`
        ).join(', ')}
      </span>
    </div>
    <Button size="sm" className="h-7 text-xs bg-emerald-600 hover:bg-emerald-500" onClick={() => handleUpgrade()} disabled={upgrading}>
      {upgrading ? 'Upgrading...' : 'Upgrade'}
    </Button>
  </div>
)}
```

Add "Upgrade All" button next to "Update Sources":

```tsx
<Button variant="outline" size="sm" onClick={() => handleUpgrade()} disabled={upgrading} className="h-8 text-xs">
  {upgrading ? 'Upgrading...' : 'Upgrade All'}
</Button>
```

- [ ] **Step 4: Verify build and tests**

Run: `npm run build 2>&1 | tail -5 && npm test -- --run 2>&1 | tail -10`
Expected: clean build, all tests pass

- [ ] **Step 5: Commit**

```bash
git add src/app/screens/Hub.tsx src/app/lib/api.ts
git commit -m "feat: Hub update/upgrade UX with banner and Upgrade All button"
```

---

### Task 5: Events — Simplified Feed

Extract the Events section and simplify the display.

**Files:**
- Modify: `src/app/screens/Events.tsx` (if it exists as a separate component already)

- [ ] **Step 1: Read the current Events component**

Check if Events is already a separate component (Admin.tsx delegates to `<Events />`). Read it to understand the current structure.

- [ ] **Step 2: Simplify the event rows**

Replace the table with a feed. Each event row should be a single line:

```tsx
<div className="flex items-center gap-2 px-4 py-2 hover:bg-secondary/30 transition-colors text-sm">
  <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${
    event.event_type?.includes('error') ? 'bg-red-500' :
    event.event_type?.includes('warning') ? 'bg-amber-500' :
    'bg-emerald-500'
  }`} />
  <span className="font-medium text-foreground truncate w-[140px]">{event.source_name || event.source_type}</span>
  <span className="text-muted-foreground truncate">{event.event_type}</span>
  <span className="ml-auto text-xs text-muted-foreground/60 flex-shrink-0 whitespace-nowrap">
    {new Date(event.timestamp).toLocaleTimeString()}
  </span>
</div>
```

Remove the raw JSON "DATA" column. Add click-to-expand for event detail.

- [ ] **Step 3: Add agent filter dropdown**

Add a second filter dropdown alongside the existing event type filter:

```tsx
<Select value={agentFilter} onValueChange={setAgentFilter}>
  <SelectTrigger className="w-[160px] h-8 text-xs">
    <SelectValue placeholder="All agents" />
  </SelectTrigger>
  <SelectContent>
    <SelectItem value="all">All agents</SelectItem>
    {uniqueAgents.map((a) => <SelectItem key={a} value={a}>{a}</SelectItem>)}
  </SelectContent>
</Select>
```

- [ ] **Step 4: Verify build and tests**

Run: `npm run build 2>&1 | tail -5 && npm test -- --run 2>&1 | tail -10`
Expected: clean build, all tests pass

- [ ] **Step 5: Commit**

```bash
git add src/app/screens/Events.tsx
git commit -m "feat: simplified Events feed with severity dots and agent filter"
```

---

### Task 6: Knowledge Graph — Kind Filters and Controls

Add kind filter toggles, max nodes cap, fit button, and force-directed default to the graph view.

**Files:**
- Modify: `src/app/screens/KnowledgeExplorer.tsx`

- [ ] **Step 1: Read the graph view section**

Read `src/app/screens/KnowledgeExplorer.tsx` lines 868–1144 (GraphView function) and lines 616–867 (layout engine). Understand `SimNode`, `applyLayout`, `fitViewport`, and the existing `MAX_GRAPH_NODES` constant.

- [ ] **Step 2: Add kind filter state and controls**

In the `GraphView` function, add state for kind filtering:

```tsx
const [hiddenKinds, setHiddenKinds] = useState<Set<string>>(new Set());
```

Add a filter bar below the layout mode buttons:

```tsx
<div className="flex flex-wrap items-center gap-1.5 px-4 py-2 border-b border-border">
  {Object.entries(kindCounts).map(([kind, count]) => {
    const hidden = hiddenKinds.has(kind);
    const color = KIND_COLORS[kind] || KIND_COLORS.unknown;
    return (
      <button
        key={kind}
        onClick={() => {
          const next = new Set(hiddenKinds);
          hidden ? next.delete(kind) : next.add(kind);
          setHiddenKinds(next);
        }}
        className={`flex items-center gap-1.5 px-2 py-1 rounded text-[11px] transition-colors ${
          hidden ? 'opacity-40 bg-transparent border border-border' : 'bg-secondary'
        }`}
      >
        <span className="w-2 h-2 rounded-full" style={{ backgroundColor: color }} />
        {kind} ({count})
      </button>
    );
  })}
</div>
```

`kindCounts` should be computed from the unfiltered nodes passed to `GraphView`. Filter the nodes before passing them to the layout engine:

```tsx
const visibleNodes = nodes.filter((n) => !hiddenKinds.has(n.kind));
```

Use `visibleNodes` instead of `nodes` when building `simNodes`.

- [ ] **Step 3: Change default layout to force-directed**

Find where `layout` state is initialized (it defaults to `'radial'`). Change to:

```tsx
const [layout, setLayout] = useState<LayoutMode>('force');
```

Or whatever the force-directed mode identifier is in the existing code.

- [ ] **Step 4: Add Fit button**

Add a "Fit" button next to the layout toggles that calls the existing `fitViewport` function:

```tsx
<Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => { /* call fitViewport with current simNodes */ }}>
  Fit
</Button>
```

- [ ] **Step 5: Verify build and tests**

Run: `npm run build 2>&1 | tail -5 && npm test -- --run 2>&1 | tail -10`
Expected: clean build, all tests pass

- [ ] **Step 6: Commit**

```bash
git add src/app/screens/KnowledgeExplorer.tsx
git commit -m "feat: knowledge graph kind filters, force-directed default, fit button"
```

---

### Task 7: Doctor — Summary Cards

Extract Doctor from Admin.tsx into a summary card view.

**Files:**
- Create: `src/app/screens/AdminDoctor.tsx`
- Modify: `src/app/screens/Admin.tsx`

- [ ] **Step 1: Read current Doctor section**

Read Admin.tsx lines 422–581 (Doctor TabsContent) and the `loadDoctor` callback.

- [ ] **Step 2: Create AdminDoctor.tsx**

Create `src/app/screens/AdminDoctor.tsx` with summary cards that expand to show check details:

```tsx
import { useState, useCallback, useEffect } from 'react';
import { api } from '../lib/api';
import { Button } from '../components/ui/button';
import { RefreshCw } from 'lucide-react';

interface Check {
  name: string;
  status: string;
  detail?: string;
}

interface AgentChecks {
  agent: string;
  checks: Check[];
}

export function AdminDoctor() {
  const [results, setResults] = useState<AgentChecks[]>([]);
  const [loading, setLoading] = useState(false);
  const [lastRun, setLastRun] = useState<string | null>(null);
  const [expandedAgent, setExpandedAgent] = useState<string | null>(null);

  const run = useCallback(async () => {
    setLoading(true);
    try {
      const data = await api.admin.doctor();
      const checks = (data as any).checks || data;
      // Group by agent
      const grouped: Record<string, Check[]> = {};
      for (const c of (Array.isArray(checks) ? checks : [])) {
        const agent = c.agent || '_platform';
        if (!grouped[agent]) grouped[agent] = [];
        grouped[agent].push(c);
      }
      setResults(Object.entries(grouped).map(([agent, checks]) => ({ agent, checks })));
      setLastRun(new Date().toLocaleTimeString());
    } catch {
      setResults([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { run(); }, [run]);

  const totalChecks = results.reduce((sum, r) => sum + r.checks.length, 0);
  const passingChecks = results.reduce((sum, r) => sum + r.checks.filter((c) => c.status === 'pass').length, 0);
  const allPassing = totalChecks > 0 && passingChecks === totalChecks;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          {lastRun && <span className="text-xs text-muted-foreground">Last run: {lastRun}</span>}
          {totalChecks > 0 && (
            <span className={`text-sm font-medium ${allPassing ? 'text-emerald-400' : 'text-amber-400'}`}>
              {results.length} agent{results.length !== 1 ? 's' : ''} &middot; {allPassing ? 'all checks passing' : `${totalChecks - passingChecks} issue(s)`}
            </span>
          )}
        </div>
        <Button onClick={run} disabled={loading} className="bg-primary">
          <RefreshCw className={`w-3.5 h-3.5 mr-1.5 ${loading ? 'animate-spin' : ''}`} />
          Run Doctor
        </Button>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-3">
        {results.map(({ agent, checks }) => {
          const passed = checks.filter((c) => c.status === 'pass').length;
          const total = checks.length;
          const ok = passed === total;
          const isExpanded = expandedAgent === agent;

          return (
            <div key={agent}>
              <button
                onClick={() => setExpandedAgent(isExpanded ? null : agent)}
                className={`w-full bg-card border rounded-lg p-4 text-center transition-colors hover:bg-secondary/30 ${
                  ok ? 'border-emerald-900/50 border-l-[3px] border-l-emerald-500' : 'border-amber-900/50 border-l-[3px] border-l-amber-500'
                }`}
              >
                <div className={`text-2xl font-bold ${ok ? 'text-emerald-400' : 'text-amber-400'}`}>{passed}/{total}</div>
                <div className="text-xs text-muted-foreground mt-1 font-mono">{agent}</div>
              </button>
              {isExpanded && (
                <div className="mt-2 bg-card border border-border rounded-lg p-3 space-y-1.5">
                  {checks.map((c) => (
                    <div key={c.name} className="flex items-center gap-2 text-xs">
                      <span className={c.status === 'pass' ? 'text-emerald-500' : 'text-amber-500'}>
                        {c.status === 'pass' ? '✓' : '✗'}
                      </span>
                      <span className="text-foreground font-medium">{c.name}</span>
                      {c.detail && <span className="text-muted-foreground truncate">{c.detail}</span>}
                    </div>
                  ))}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Replace inline Doctor in Admin.tsx**

Import and use the new component:

```tsx
import { AdminDoctor } from './AdminDoctor';
```

Replace the Doctor `TabsContent` with:

```tsx
<TabsContent value="doctor" className="space-y-4 mt-0">
  <AdminDoctor />
</TabsContent>
```

Remove the doctor state and `loadDoctor` callback from Admin.tsx.

- [ ] **Step 4: Verify build and tests**

Run: `npm run build 2>&1 | tail -5 && npm test -- --run 2>&1 | tail -10`
Expected: clean build, all tests pass

- [ ] **Step 5: Commit**

```bash
git add src/app/screens/AdminDoctor.tsx src/app/screens/Admin.tsx
git commit -m "feat: Doctor summary cards with expandable per-agent checks"
```

---

### Task 8: Egress — Merge Policy Domains

Fix the Egress tab to show both auto-managed and policy-configured domains.

**Files:**
- Create: `src/app/screens/AdminEgress.tsx`
- Modify: `src/app/screens/Admin.tsx`

- [ ] **Step 1: Read current Egress section**

Read Admin.tsx lines 687–853 (Egress TabsContent). Understand the domain provenance table and per-agent egress display.

- [ ] **Step 2: Create AdminEgress.tsx**

Extract the Egress section into its own component. The key change: if the domain provenance list is empty, show a message explaining that domains are auto-provisioned when connectors are activated.

Keep the existing table structure (it's well-designed) but ensure it loads data correctly via the already-fixed `api.admin.egressDomains()` which unwraps the `{domains: [...]}` response.

- [ ] **Step 3: Replace inline Egress in Admin.tsx**

Import and delegate:

```tsx
import { AdminEgress } from './AdminEgress';
```

```tsx
<TabsContent value="egress" className="space-y-4 mt-0">
  <AdminEgress />
</TabsContent>
```

Remove egress state and callbacks from Admin.tsx.

- [ ] **Step 4: Verify build and tests**

Run: `npm run build 2>&1 | tail -5 && npm test -- --run 2>&1 | tail -10`
Expected: clean build, all tests pass

- [ ] **Step 5: Commit**

```bash
git add src/app/screens/AdminEgress.tsx src/app/screens/Admin.tsx
git commit -m "feat: extract Egress to sub-screen with domain provenance display"
```
