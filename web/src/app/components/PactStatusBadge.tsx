import { ShieldCheck } from 'lucide-react';

export interface PactMetadata {
  kind?: string;
  verdict?: string;
  source_urls?: unknown[];
  missing_evidence?: unknown[];
}

export function extractPactMetadata(payload: Record<string, unknown> | null | undefined): PactMetadata | null {
  const direct = payload?.pact;
  const nested = (payload?.metadata as Record<string, unknown> | undefined)?.pact;
  const pact = (direct || nested) as PactMetadata | undefined;
  if (!pact || typeof pact !== 'object') return null;
  if (!pact.verdict && !pact.kind) return null;
  return pact;
}

export function pactVerdictColor(verdict?: string): string {
  switch (verdict) {
    case 'completed':
      return 'var(--teal)';
    case 'blocked':
      return '#d64b4b';
    case 'needs_action':
      return '#b7791f';
    default:
      return 'var(--ink-muted)';
  }
}

export function pactSummary(pact: PactMetadata): string {
  const sources = Array.isArray(pact.source_urls) ? pact.source_urls.length : 0;
  const missing = Array.isArray(pact.missing_evidence) ? pact.missing_evidence.length : 0;
  if (sources > 0) return `${sources} source${sources === 1 ? '' : 's'}`;
  if (missing > 0) return `${missing} gap${missing === 1 ? '' : 's'}`;
  return pact.kind || 'PACT';
}

export function PactStatusBadge({ pact, metadataError }: { pact?: PactMetadata | null; metadataError?: string }) {
  if (metadataError) {
    return (
      <span
        className="mono inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[11px]"
        style={{ border: '0.5px solid var(--ink-hairline)', background: 'var(--warm-2)', color: '#b7791f' }}
        title={metadataError}
      >
        <ShieldCheck className="h-3 w-3" />
        metadata issue
      </span>
    );
  }
  if (!pact) return null;
  return (
    <span
      className="mono inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[11px]"
      style={{
        border: '0.5px solid var(--ink-hairline)',
        background: 'var(--warm-2)',
        color: pactVerdictColor(pact.verdict),
      }}
      title={pact.kind ? `PACT ${pact.kind}` : 'PACT metadata'}
    >
      <ShieldCheck className="h-3 w-3" />
      <span>{pact.verdict || 'PACT'}</span>
      <span style={{ color: 'var(--ink-faint)' }}>{pactSummary(pact)}</span>
    </span>
  );
}
