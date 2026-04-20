import { useState, useEffect, type ButtonHTMLAttributes, type CSSProperties, type ReactNode } from 'react';
import { api } from '../lib/api';
import { formatDateTimeShort } from '../lib/time';
import { toast } from 'sonner';
import { adminFeatureFlags } from '../lib/features';

interface KnowledgeStats {
  node_count: number;
  edge_count: number;
}

interface OntologyCandidate {
  id: string;
  value?: string;
  label?: string;
  name?: string;
  count?: number;
  source?: string;
  status?: string;
  candidate_type?: string;
  properties?: {
    value?: string;
    occurrence_count?: number;
    source_count?: number;
    status?: string;
  };
}

interface OntologyDecision {
  id: string;
  nodeId: string;
  value: string;
  action: 'promote' | 'reject' | 'restore' | 'unknown';
  timestamp?: string;
}

interface PendingContribution {
  id: string;
  title: string;
  subject?: string;
  type?: string;
  agent?: string;
  confidence?: number;
  summary?: string;
  proposed?: string;
  reason?: string;
  createdAt?: string;
}

interface MemoryProposal {
  id: string;
  summary: string;
  memoryType?: string;
  confidence?: string;
  agent?: string;
  channel?: string;
  reason?: string;
  createdAt?: string;
}

interface QuarantinedNode {
  id: string;
  label: string;
  agent?: string;
  type?: string;
  reason?: string;
  quarantinedAt?: string;
}

interface TopologyItem {
  id: string;
  label: string;
  detail?: string;
  count?: number;
}

interface GraphTopology {
  tiers: TopologyItem[];
  principals: TopologyItem[];
  communities: TopologyItem[];
  hubs: TopologyItem[];
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

function parseJSONRecord(value: unknown): Record<string, unknown> {
  if (typeof value === 'string') {
    try {
      return asRecord(JSON.parse(value));
    } catch {
      return {};
    }
  }
  return asRecord(value);
}

function firstString(record: Record<string, unknown>, keys: string[]): string | undefined {
  for (const key of keys) {
    const value = record[key];
    if (typeof value === 'string' && value.trim()) return value;
  }
  return undefined;
}

function firstNumber(record: Record<string, unknown>, keys: string[]): number | undefined {
  for (const key of keys) {
    const value = record[key];
    if (typeof value === 'number') return value;
  }
  return undefined;
}

function candidateValue(candidate: OntologyCandidate, fallback = 'candidate') {
  return (
    candidate.value ||
    candidate.properties?.value ||
    candidate.label ||
    candidate.name ||
    fallback
  );
}

function candidateStatus(candidate: OntologyCandidate) {
  return candidate.status || candidate.properties?.status || 'candidate';
}

function parseOntologyDecisions(raw: unknown): OntologyDecision[] {
  const entries = Array.isArray(raw)
    ? raw
    : Array.isArray((raw as { entries?: unknown[] } | null)?.entries)
      ? (raw as { entries: unknown[] }).entries
      : [];

  return entries
    .map((entry, index) => {
      const record = (entry ?? {}) as Record<string, unknown>;
      const detail = typeof record.detail === 'string'
        ? (() => {
            try {
              return JSON.parse(record.detail) as Record<string, unknown>;
            } catch {
              return {};
            }
          })()
        : {};
      const data = {
        ...detail,
        ...((record.data ?? {}) as Record<string, unknown>),
      };
      const actionText = String(record.action ?? record.event ?? record.type ?? '');
      const normalizedAction = actionText.toLowerCase();
      const action: OntologyDecision['action'] =
        normalizedAction.includes('promote') ? 'promote' :
        normalizedAction.includes('reject') ? 'reject' :
        normalizedAction.includes('restore') ? 'restore' :
        'unknown';

      if (action === 'unknown') {
        return null;
      }

      const nodeId = String(
        record.node_id ??
        record.nodeId ??
        data.node_id ??
        data.nodeId ??
        record.id ??
        ''
      );
      const value = String(
        record.value ??
        data.value ??
        record.label ??
        data.label ??
        data.subject ??
        record.subject ??
        nodeId
      );

      if (!nodeId || !value) {
        return null;
      }

      const timestamp = typeof record.timestamp === 'string'
        ? record.timestamp
        : typeof record.ts === 'string'
          ? record.ts
          : undefined;

      return {
        id: String(record.id ?? `${action}-${nodeId}-${timestamp ?? index}`),
        nodeId,
        value,
        action,
        timestamp,
      };
    })
    .filter((entry): entry is OntologyDecision => entry !== null);
}

function parsePendingContributions(raw: unknown): PendingContribution[] {
  const record = asRecord(raw);
  const entries = Array.isArray(raw)
    ? raw
    : Array.isArray(record.pending)
      ? record.pending
      : Array.isArray(record.items)
        ? record.items
        : Array.isArray(record.contributions)
          ? record.contributions
          : [];

  return entries.map((entry, index) => {
    const item = asRecord(entry);
    const data = asRecord(item.data);
    const proposal = asRecord(item.proposal);
    const merged = { ...data, ...proposal, ...item };
    const id = firstString(merged, ['id', 'uuid', 'contribution_id', 'node_id']) ?? `pending-${index}`;
    const title = firstString(merged, ['title', 'name', 'label', 'subject', 'relation', 'kind']) ?? id;
    const proposedValue = firstString(merged, ['proposed', 'value', 'object', 'target', 'content']);

    return {
      id,
      title,
      subject: firstString(merged, ['subject', 'source', 'node']),
      type: firstString(merged, ['type', 'kind', 'relation']),
      agent: firstString(merged, ['agent', 'source_agent', 'created_by', 'author']),
      confidence: firstNumber(merged, ['confidence', 'score']),
      summary: firstString(merged, ['summary', 'description', 'rationale']),
      proposed: proposedValue,
      reason: firstString(merged, ['reason', 'evidence']),
      createdAt: firstString(merged, ['created_at', 'timestamp', 'created']),
    };
  });
}

function parseMemoryProposals(raw: unknown): MemoryProposal[] {
  const record = asRecord(raw);
  const entries = Array.isArray(raw)
    ? raw
    : Array.isArray(record.items)
      ? record.items
      : Array.isArray(record.proposals)
        ? record.proposals
        : [];

  return entries.map((entry, index) => {
    const item = asRecord(entry);
    const props = parseJSONRecord(item.properties);
    const merged = { ...props, ...item };
    const id = firstString(merged, ['id', 'node_id', 'uuid']) ?? `memory-${index}`;
    return {
      id,
      summary: firstString(merged, ['summary', 'label', 'title']) ?? id,
      memoryType: firstString(merged, ['memory_type', 'type', 'kind']),
      confidence: firstString(merged, ['confidence', 'score']),
      agent: firstString(merged, ['agent', 'source_agent', 'author']),
      channel: firstString(merged, ['channel', 'source_channel']),
      reason: firstString(merged, ['decision_reason', 'reason', 'rationale']),
      createdAt: firstString(merged, ['created_at', 'timestamp', 'created']),
    };
  });
}

function parseQuarantinedNodes(raw: unknown): QuarantinedNode[] {
  const record = asRecord(raw);
  const entries = Array.isArray(raw)
    ? raw
    : Array.isArray(record.nodes)
      ? record.nodes
      : Array.isArray(record.items)
        ? record.items
        : Array.isArray(record.quarantined)
          ? record.quarantined
          : [];

  return entries.map((entry, index) => {
    const item = asRecord(entry);
    const data = asRecord(item.data);
    const merged = { ...data, ...item };
    const id = firstString(merged, ['id', 'node_id', 'uuid']) ?? `quarantine-${index}`;
    return {
      id,
      label: firstString(merged, ['label', 'name', 'title', 'subject', 'kind']) ?? id,
      agent: firstString(merged, ['agent', 'source_agent', 'author']),
      type: firstString(merged, ['type', 'kind', 'source_type']),
      reason: firstString(merged, ['reason', 'quarantine_reason', 'summary']),
      quarantinedAt: firstString(merged, ['quarantined_at', 'timestamp', 'created_at']),
    };
  });
}

function unwrapList(raw: unknown, keys: string[]): unknown[] {
  const record = asRecord(raw);
  if (Array.isArray(raw)) return raw;
  for (const key of keys) {
    if (Array.isArray(record[key])) return record[key] as unknown[];
  }
  return [];
}

function parseTopologyItems(raw: unknown, keys: string[], fallbackPrefix: string): TopologyItem[] {
  return unwrapList(raw, keys).map((entry, index) => {
    const item = asRecord(entry);
    const id = firstString(item, ['id', 'uuid', 'name', 'tier', 'label']) ?? `${fallbackPrefix}-${index}`;
    return {
      id,
      label: firstString(item, ['label', 'name', 'tier', 'type', 'uuid']) ?? id,
      detail: firstString(item, ['description', 'summary', 'scope', 'classification', 'type']),
      count: firstNumber(item, ['count', 'size', 'nodes', 'node_count', 'degree']),
    };
  });
}

type ButtonTone = 'default' | 'primary' | 'ghost';
const REVIEW_WIDTHS = 'minmax(180px, 1.4fr) minmax(150px, 1fr) 82px 118px';
const MEMORY_WIDTHS = 'minmax(180px, 1.35fr) minmax(120px, 0.75fr) minmax(150px, 1fr) 118px';
const QUARANTINE_WIDTHS = 'minmax(180px, 1.2fr) minmax(160px, 1fr) 92px';
const ONTOLOGY_WIDTHS = 'minmax(0, 1fr) 92px minmax(0, 1.35fr) 154px';
const TOPOLOGY_WIDTHS = '118px minmax(0, 1.05fr) minmax(0, 1.3fr) 54px';

function actionStyle(tone: ButtonTone = 'default', disabled = false): CSSProperties {
  const variants = {
    default: { bg: 'var(--warm)', color: 'var(--ink)', border: '0.5px solid var(--ink-hairline-strong)' },
    primary: { bg: 'var(--ink)', color: 'var(--warm)', border: '0.5px solid var(--ink)' },
    ghost: { bg: 'transparent', color: 'var(--ink-mid)', border: '0.5px solid transparent' },
  }[tone];
  return { display: 'inline-flex', alignItems: 'center', justifyContent: 'center', gap: 6, padding: '5px 10px', fontSize: 12, fontWeight: 400, fontFamily: 'var(--sans)', cursor: disabled ? 'default' : 'pointer', background: variants.bg, color: variants.color, border: variants.border, borderRadius: 999, opacity: disabled ? 0.5 : 1, whiteSpace: 'nowrap' };
}

function ActionButton({ children, tone = 'default', style, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { tone?: ButtonTone }) {
  return <button type="button" {...props} style={{ ...actionStyle(tone, Boolean(props.disabled)), ...style }}>{children}</button>;
}

function Card({ children, padded = false }: { children: ReactNode; padded?: boolean }) {
  return <div style={{ background: 'var(--warm-2)', border: '0.5px solid var(--ink-hairline)', borderRadius: 10, padding: padded ? 14 : 0, overflow: 'hidden' }}>{children}</div>;
}

function MetaStat({ label, value, tone }: { label: string; value: string | number; tone?: string }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      <span className="eyebrow" style={{ fontSize: 9 }}>{label}</span>
      <span className="mono" style={{ fontSize: 14, color: tone || 'var(--ink)' }}>{value}</span>
    </div>
  );
}

function TableHeader({ cols, widths }: { cols: ReactNode[]; widths: string }) {
  return <div style={{ display: 'grid', gridTemplateColumns: widths, gap: 14, padding: '9px 16px', background: 'var(--warm-2)' }}>{cols.map((col, index) => <div key={index} className="eyebrow" style={{ fontSize: 9 }}>{col}</div>)}</div>;
}

function TableRow({ cols, widths, accent }: { cols: ReactNode[]; widths: string; accent?: string }) {
  return <div style={{ display: 'grid', gridTemplateColumns: widths, gap: 14, padding: '10px 16px', alignItems: 'center', borderTop: '0.5px solid var(--ink-hairline)', borderLeft: accent ? `2px solid ${accent}` : '2px solid transparent' }}>{cols.map((col, index) => <div key={index} style={{ minWidth: 0 }}>{col}</div>)}</div>;
}

function EmptyLine({ children }: { children: ReactNode }) {
  return <div style={{ padding: 22, textAlign: 'center', color: 'var(--ink-mid)', fontSize: 13 }}>{children}</div>;
}

function SectionTitle({ title, action }: { title: string; action?: ReactNode }) {
  return <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, marginBottom: 8 }}><div className="eyebrow" style={{ fontSize: 9 }}>{title}</div>{action}</div>;
}

function InlineGroupLabel({ children }: { children: ReactNode }) {
  return (
    <div
      className="eyebrow"
      style={{
        padding: '10px 16px 8px',
        borderTop: '0.5px solid var(--ink-hairline)',
        background: 'var(--warm)',
        color: 'var(--ink-mid)',
        fontSize: 9,
      }}
    >
      {children}
    </div>
  );
}

function Truncate({ children, className, style }: { children: ReactNode; className?: string; style?: CSSProperties }) {
  return (
    <span
      className={className}
      style={{
        display: 'block',
        minWidth: 0,
        overflow: 'hidden',
        textOverflow: 'ellipsis',
        whiteSpace: 'nowrap',
        ...style,
      }}
    >
      {children}
    </span>
  );
}

function TopologyRows({ label, items }: { label: string; items: TopologyItem[] }) {
  if (items.length === 0) {
    return (
      <TableRow
        widths={TOPOLOGY_WIDTHS}
        cols={[
          <span className="mono" style={{ fontSize: 12, color: 'var(--ink-faint)' }}>{label}</span>,
          <span style={{ fontSize: 12, color: 'var(--ink-mid)' }}>No data</span>,
          <span />,
          <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)', textAlign: 'right', display: 'block' }}>0</span>,
        ]}
      />
    );
  }

  return (
    <>
      {items.slice(0, 5).map((item, index) => (
        <TableRow
          key={`${label}-${item.id}`}
          widths={TOPOLOGY_WIDTHS}
          cols={[
            <span className="mono" style={{ fontSize: 12, color: index === 0 ? 'var(--ink)' : 'var(--ink-faint)' }}>{index === 0 ? label : ''}</span>,
            <Truncate className="mono" style={{ fontSize: 12, color: 'var(--ink)' }}>{item.label}</Truncate>,
            <Truncate style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{item.detail || '...'}</Truncate>,
            <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)', textAlign: 'right', display: 'block' }}>{item.count ?? ''}</span>,
          ]}
        />
      ))}
      {items.length > 5 && (
        <TableRow
          widths={TOPOLOGY_WIDTHS}
          cols={[
            <span />,
            <span className="mono" style={{ fontSize: 11, color: 'var(--ink-faint)' }}>+ {items.length - 5} more</span>,
            <span />,
            <span />,
          ]}
        />
      )}
    </>
  );
}

export function Knowledge({ onSelectResult: _onSelectResult }: { onSelectResult?: (label: string, kind: string) => void }) {
  const graphAdminEnabled = adminFeatureFlags.graphAdmin;
  const [stats, setStats] = useState<KnowledgeStats | null>(null);
  const [statsLoading, setStatsLoading] = useState(true);
  const [ontologyCandidates, setOntologyCandidates] = useState<OntologyCandidate[]>([]);
  const [ontologyLoading, setOntologyLoading] = useState(false);
  const [ontologyDecisions, setOntologyDecisions] = useState<OntologyDecision[]>([]);
  const [ontologyActionLoading, setOntologyActionLoading] = useState<string | null>(null);
  const [pendingContributions, setPendingContributions] = useState<PendingContribution[]>([]);
  const [pendingLoading, setPendingLoading] = useState(false);
  const [reviewActionLoading, setReviewActionLoading] = useState<string | null>(null);
  const [memoryProposals, setMemoryProposals] = useState<MemoryProposal[]>([]);
  const [memoryLoading, setMemoryLoading] = useState(false);
  const [memoryActionLoading, setMemoryActionLoading] = useState<string | null>(null);
  const [quarantinedNodes, setQuarantinedNodes] = useState<QuarantinedNode[]>([]);
  const [quarantineLoading, setQuarantineLoading] = useState(false);
  const [quarantineActionLoading, setQuarantineActionLoading] = useState<string | null>(null);
  const [topology, setTopology] = useState<GraphTopology>({ tiers: [], principals: [], communities: [], hubs: [] });
  const [topologyLoading, setTopologyLoading] = useState(false);

  const loadStats = async () => {
    try {
      setStatsLoading(true);
      const data = await api.knowledge.stats();
      const d = data as any;
      setStats({ node_count: d.nodes ?? d.node_count ?? 0, edge_count: d.edges ?? d.edge_count ?? 0 });
    } catch {
      setStats(null);
    } finally {
      setStatsLoading(false);
    }
  };

  const loadPendingContributions = async () => {
    try {
      setPendingLoading(true);
      const data = await api.knowledge.pending();
      setPendingContributions(parsePendingContributions(data));
    } catch {
      setPendingContributions([]);
    } finally {
      setPendingLoading(false);
    }
  };

  const loadMemoryProposals = async () => {
    try {
      setMemoryLoading(true);
      const data = await api.knowledge.memoryProposals('needs_review');
      setMemoryProposals(parseMemoryProposals(data));
    } catch {
      setMemoryProposals([]);
    } finally {
      setMemoryLoading(false);
    }
  };

  const loadQuarantinedNodes = async () => {
    try {
      setQuarantineLoading(true);
      const data = await api.knowledge.quarantineList();
      setQuarantinedNodes(parseQuarantinedNodes(data));
    } catch {
      setQuarantinedNodes([]);
    } finally {
      setQuarantineLoading(false);
    }
  };

  const loadTopology = async () => {
    try {
      setTopologyLoading(true);
      const [classification, principals, communities, hubs] = await Promise.all([
        api.knowledge.classification().catch(() => null),
        api.knowledge.principals().catch(() => null),
        api.knowledge.communities().catch(() => null),
        api.knowledge.hubs(10).catch(() => null),
      ]);
      setTopology({
        tiers: parseTopologyItems(classification, ['tiers', 'classifications', 'items'], 'tier'),
        principals: parseTopologyItems(principals, ['principals', 'items'], 'principal'),
        communities: parseTopologyItems(communities, ['communities', 'items'], 'community'),
        hubs: parseTopologyItems(hubs, ['hubs', 'items'], 'hub'),
      });
    } finally {
      setTopologyLoading(false);
    }
  };

  const loadOntologyReviewData = async () => {
    try {
      setOntologyLoading(true);
      const candidateData = await api.knowledge.ontologyCandidates().catch(() => null);
      const curationLog = await api.knowledge.curationLog().catch(() => null);
      setOntologyCandidates(candidateData?.candidates || []);
      setOntologyDecisions(parseOntologyDecisions(curationLog));
    } finally {
      setOntologyLoading(false);
    }
  };

  const reloadAll = async () => {
    await Promise.all([loadStats(), loadOntologyReviewData(), loadPendingContributions(), loadMemoryProposals(), loadQuarantinedNodes(), loadTopology()]);
  };

  useEffect(() => {
    if (!graphAdminEnabled) {
      setStatsLoading(false);
      return;
    }
    reloadAll();
  }, [graphAdminEnabled]);

  const handlePromote = async (candidate: OntologyCandidate) => {
    const value = candidateValue(candidate, candidate.id);
    try {
      setOntologyActionLoading(candidate.id);
      await api.knowledge.ontologyPromote(candidate.id, value);
      toast.success(`Accepted "${value}"`);
      await loadOntologyReviewData();
    } catch (e: any) {
      toast.error(e.message || 'Accept failed');
    } finally {
      setOntologyActionLoading(null);
    }
  };

  const handleReject = async (candidate: OntologyCandidate) => {
    const value = candidateValue(candidate, candidate.id);
    try {
      setOntologyActionLoading(candidate.id);
      await api.knowledge.ontologyReject(candidate.id, value);
      toast.success(`Dismissed "${value}"`);
      await loadOntologyReviewData();
    } catch (e: any) {
      toast.error(e.message || 'Dismiss failed');
    } finally {
      setOntologyActionLoading(null);
    }
  };

  const handleRestore = async (nodeId: string, value: string) => {
    try {
      setOntologyActionLoading(nodeId);
      await api.knowledge.ontologyRestore(nodeId, value);
      toast.success(`Restored "${value}" to ontology review`);
      await loadOntologyReviewData();
    } catch (e: any) {
      toast.error(e.message || 'Restore failed');
    } finally {
      setOntologyActionLoading(null);
    }
  };

  const handleReviewContribution = async (contribution: PendingContribution, action: 'approve' | 'reject') => {
    try {
      setReviewActionLoading(`${contribution.id}:${action}`);
      await api.knowledge.review(contribution.id, action);
      toast.success(`${action === 'approve' ? 'Approved' : 'Rejected'} "${contribution.title}"`);
      await loadPendingContributions();
    } catch (e: any) {
      toast.error(e.message || 'Review failed');
    } finally {
      setReviewActionLoading(null);
    }
  };

  const handleReviewMemoryProposal = async (proposal: MemoryProposal, action: 'approve' | 'reject') => {
    try {
      setMemoryActionLoading(`${proposal.id}:${action}`);
      await api.knowledge.reviewMemoryProposal(proposal.id, action);
      toast.success(`${action === 'approve' ? 'Approved' : 'Rejected'} memory proposal`);
      await loadMemoryProposals();
      await loadStats();
    } catch (e: any) {
      toast.error(e.message || 'Memory review failed');
    } finally {
      setMemoryActionLoading(null);
    }
  };

  const handleReleaseQuarantine = async (node: QuarantinedNode) => {
    try {
      setQuarantineActionLoading(node.id);
      await api.knowledge.quarantineRelease({ node_id: node.id });
      toast.success(`Released "${node.label}" from quarantine`);
      await loadQuarantinedNodes();
    } catch (e: any) {
      toast.error(e.message || 'Release failed');
    } finally {
      setQuarantineActionLoading(null);
    }
  };

  if (!graphAdminEnabled) return null;

  const topologyCount = topology.tiers.length + topology.principals.length + topology.communities.length + topology.hubs.length;
  const refreshing = statsLoading || pendingLoading || memoryLoading || quarantineLoading || topologyLoading || ontologyLoading;
  const evidenceText = (candidate: OntologyCandidate, count?: number, source?: string) => [
    count != null ? `${count} occ.` : '',
    source || '',
    candidate.candidate_type || '',
  ].filter(Boolean).join(' · ') || '...';
  const statItems = [
    { label: 'Nodes', value: statsLoading ? '...' : stats ? stats.node_count.toLocaleString() : '0' },
    { label: 'Edges', value: statsLoading ? '...' : stats ? stats.edge_count.toLocaleString() : '0' },
    { label: 'Pending', value: pendingLoading ? '...' : pendingContributions.length.toLocaleString() },
    { label: 'Memory', value: memoryLoading ? '...' : memoryProposals.length.toLocaleString() },
    { label: 'Quarantined', value: quarantineLoading ? '...' : quarantinedNodes.length.toLocaleString() },
    { label: 'Ontology', value: ontologyLoading ? '...' : (ontologyCandidates.length + ontologyDecisions.length).toLocaleString() },
    { label: 'Topology', value: topologyLoading ? '...' : topologyCount.toLocaleString() },
  ];

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
        <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }} aria-label="Knowledge metrics">
          {statItems.map((item) => (
            <MetaStat key={item.label} label={item.label} value={item.value} tone={(item.label === 'Pending' || item.label === 'Memory') && item.value !== '0' ? 'var(--amber)' : item.label === 'Quarantined' && item.value !== '0' ? 'var(--red)' : undefined} />
          ))}
        </div>
        <ActionButton onClick={reloadAll} disabled={refreshing}>{refreshing ? 'Refreshing...' : 'Refresh'}</ActionButton>
      </div>

      {!stats && !statsLoading && (
        <div style={{ padding: '10px 12px', border: '0.5px solid var(--amber)', background: 'var(--amber-tint)', color: '#8B5A00', borderRadius: 8, fontSize: 12 }}>
          Knowledge graph stats are unavailable. Administration controls remain available if the graph APIs respond.
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 520px), 1fr))', gap: 14 }}>
        <div>
          <SectionTitle title="Structural Review" />
          <Card>
            <TableHeader widths={REVIEW_WIDTHS} cols={['Proposal', 'Subject', 'Confidence', '']} />
            {pendingContributions.length === 0 ? <EmptyLine>No pending structural contributions</EmptyLine> : pendingContributions.map((item) => (
              <TableRow key={item.id} widths={REVIEW_WIDTHS} accent="var(--amber)" cols={[
                <div><Truncate className="mono" style={{ fontSize: 13, color: 'var(--ink)' }}>{item.title}</Truncate><Truncate className="mono" style={{ marginTop: 3, fontSize: 11, color: 'var(--ink-faint)' }}>{item.id}</Truncate>{item.summary && <Truncate style={{ marginTop: 4, fontSize: 12, color: 'var(--ink-mid)' }}>{item.summary}</Truncate>}</div>,
                <Truncate style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{[item.subject, item.proposed].filter(Boolean).join(' -> ') || item.type || '...'}</Truncate>,
                <span className="mono" style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{item.confidence != null ? `${Math.round(item.confidence * 100)}%` : '...'}</span>,
                <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 6 }}><ActionButton tone="ghost" onClick={() => handleReviewContribution(item, 'reject')} disabled={reviewActionLoading === `${item.id}:reject`}>Reject</ActionButton><ActionButton tone="primary" onClick={() => handleReviewContribution(item, 'approve')} disabled={reviewActionLoading === `${item.id}:approve`}>Approve</ActionButton></div>,
              ]} />
            ))}
          </Card>
        </div>

        <div>
          <SectionTitle title="Memory Review" />
          <Card>
            <TableHeader widths={MEMORY_WIDTHS} cols={['Memory', 'Type', 'Evidence', '']} />
            {memoryProposals.length === 0 ? <EmptyLine>No memory proposals awaiting review</EmptyLine> : memoryProposals.map((proposal) => (
              <TableRow key={proposal.id} widths={MEMORY_WIDTHS} accent="var(--amber)" cols={[
                <div><Truncate style={{ fontSize: 13, color: 'var(--ink)' }}>{proposal.summary}</Truncate><Truncate className="mono" style={{ marginTop: 3, fontSize: 11, color: 'var(--ink-faint)' }}>{proposal.id}</Truncate></div>,
                <span className="mono" style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{proposal.memoryType || 'memory'}</span>,
                <Truncate style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{[proposal.confidence, proposal.agent, proposal.channel, proposal.reason].filter(Boolean).join(' · ') || '...'}</Truncate>,
                <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 6 }}><ActionButton tone="ghost" onClick={() => handleReviewMemoryProposal(proposal, 'reject')} disabled={memoryActionLoading === `${proposal.id}:reject`}>Reject</ActionButton><ActionButton tone="primary" onClick={() => handleReviewMemoryProposal(proposal, 'approve')} disabled={memoryActionLoading === `${proposal.id}:approve`}>Approve</ActionButton></div>,
              ]} />
            ))}
          </Card>
        </div>

        <div>
          <SectionTitle title="Quarantine" />
          <Card>
            <TableHeader widths={QUARANTINE_WIDTHS} cols={['Node', 'Reason', '']} />
            {quarantinedNodes.length === 0 ? <EmptyLine>No quarantined knowledge</EmptyLine> : quarantinedNodes.map((node) => (
              <TableRow key={node.id} widths={QUARANTINE_WIDTHS} accent="var(--red)" cols={[
                <div><Truncate className="mono" style={{ fontSize: 13, color: 'var(--ink)' }}>{node.label}</Truncate><Truncate className="mono" style={{ marginTop: 3, fontSize: 11, color: 'var(--ink-faint)' }}>{node.id}</Truncate></div>,
                <Truncate style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{node.reason || node.type || node.agent || '...'}</Truncate>,
                <div style={{ display: 'flex', justifyContent: 'flex-end' }}><ActionButton tone="ghost" onClick={() => handleReleaseQuarantine(node)} disabled={quarantineActionLoading === node.id}>Release</ActionButton></div>,
              ]} />
            ))}
          </Card>
        </div>

        <div>
          <SectionTitle title="Graph Topology" />
          <Card>
            <TableHeader widths={TOPOLOGY_WIDTHS} cols={['Type', 'Node', 'Detail', 'Count']} />
            <TopologyRows label="Classification" items={topology.tiers} />
            <TopologyRows label="Principals" items={topology.principals} />
            <TopologyRows label="Communities" items={topology.communities} />
            <TopologyRows label="Hubs" items={topology.hubs} />
          </Card>
        </div>

        <div>
          <SectionTitle title="Ontology Review" />
          <Card>
            <TableHeader widths={ONTOLOGY_WIDTHS} cols={['Concept', 'Status', 'Evidence', '']} />
            {ontologyCandidates.length === 0 && ontologyDecisions.length === 0 ? <EmptyLine>No ontology candidates or recent decisions</EmptyLine> : <>
              {ontologyCandidates.length > 0 && <InlineGroupLabel>Pending Candidates</InlineGroupLabel>}
              {ontologyCandidates.map((candidate, idx) => {
                const val = candidateValue(candidate, `candidate_${idx}`);
                const count = candidate.count ?? candidate.properties?.occurrence_count;
                const source = candidate.source ?? (candidate.properties?.source_count ? 'knowledge' : undefined);
                const status = candidateStatus(candidate);
                return <TableRow key={candidate.id || val} widths={ONTOLOGY_WIDTHS} accent="var(--teal)" cols={[
                  <Truncate className="mono" style={{ fontSize: 13, color: 'var(--ink)' }}>{val}</Truncate>,
                  <span className="mono" style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{status}</span>,
                  <Truncate style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{evidenceText(candidate, count, source)}</Truncate>,
                  <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 6 }}><ActionButton tone="ghost" onClick={() => handleReject(candidate)} disabled={ontologyActionLoading === candidate.id}>Dismiss</ActionButton><ActionButton tone="primary" onClick={() => handlePromote(candidate)} disabled={ontologyActionLoading === candidate.id}>Accept</ActionButton></div>,
                ]} />;
              })}
              {ontologyDecisions.length > 0 && <InlineGroupLabel>Recent Decisions</InlineGroupLabel>}
              {ontologyDecisions.map((decision) => <TableRow key={decision.id} widths={ONTOLOGY_WIDTHS} cols={[
                <Truncate className="mono" style={{ fontSize: 13, color: 'var(--ink)' }}>{decision.value}</Truncate>,
                <span className="mono" style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{decision.action}</span>,
                <Truncate style={{ fontSize: 12, color: 'var(--ink-mid)' }}>{decision.timestamp ? formatDateTimeShort(decision.timestamp) : 'recent decision'}</Truncate>,
                <div style={{ display: 'flex', justifyContent: 'flex-end' }}>{decision.action !== 'restore' && <ActionButton tone="ghost" onClick={() => handleRestore(decision.nodeId, decision.value)} disabled={ontologyActionLoading === decision.nodeId}>Restore</ActionButton>}</div>,
              ]} />)}
            </>}
          </Card>
        </div>
      </div>
    </div>
  );
}
