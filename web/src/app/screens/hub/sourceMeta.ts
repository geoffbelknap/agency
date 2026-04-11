import type { ComponentKind } from '../../types';

export type HubSourceCategory = 'official' | 'local' | 'custom' | 'unknown';

export function classifyHubSource(source?: string): HubSourceCategory {
  const normalized = (source || '').trim().toLowerCase();
  if (!normalized) return 'unknown';
  if (normalized === 'official' || normalized === 'default') return 'official';
  if (normalized === 'local') return 'local';
  return 'custom';
}

export function hubSourceLabel(source?: string): string {
  switch (classifyHubSource(source)) {
    case 'official':
      return 'Official Hub';
    case 'local':
      return 'Local Operator Content';
    case 'custom':
      return `Custom Source${source ? `: ${source}` : ''}`;
    default:
      return 'Unknown Source';
  }
}

export function hubSourceGuidance(source?: string): string {
  switch (classifyHubSource(source)) {
    case 'official':
      return 'OCI content from the official Agency Hub source.';
    case 'local':
      return 'Local content under direct operator control on this machine.';
    case 'custom':
      return 'Review source ownership and trust before installing or upgrading.';
    default:
      return 'Verify the source before relying on this component.';
  }
}

export function isHubManagedKind(kind: ComponentKind | string): boolean {
  return kind === 'setup' || kind === 'ontology';
}

export function hubManagementLabel(kind: ComponentKind | string): string {
  return isHubManagedKind(kind)
    ? 'Hub-managed content updated through source refresh and upgrade.'
    : 'Operator-installable content that can be added or removed directly.';
}
