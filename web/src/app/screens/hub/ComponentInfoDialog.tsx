import type { ReactNode } from 'react';
import { X } from 'lucide-react';
import { Component } from '../../types';
import { Button } from '../../components/ui/button';
import { hubManagementLabel, hubSourceGuidance, hubSourceLabel, isHubManagedKind } from './sourceMeta';

interface ComponentInfoDialogProps {
  component: Component;
  infoData: any;
  infoLoading: boolean;
  onClose: () => void;
}

const value = (infoData: any, ...keys: string[]) => {
  for (const key of keys) {
    const found = infoData?.[key];
    if (found !== undefined && found !== null && found !== '') return String(found);
  }
  return '';
};

const Row = ({ label, children }: { label: string; children?: ReactNode }) => {
  if (!children) return null;
  return (
    <div className="grid grid-cols-[110px_1fr] gap-3 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="text-foreground break-words">{children}</dd>
    </div>
  );
};

export function ComponentInfoDialog({ component, infoData, infoLoading, onClose }: ComponentInfoDialogProps) {
  const name = value(infoData, 'name', 'component') || component.name;
  const kind = value(infoData, '_kind', 'kind') || component.kind;
  const source = value(infoData, '_source', 'source') || component.source;
  const description = value(infoData, 'description');
  const version = value(infoData, 'version');
  const author = value(infoData, 'author');
  const license = value(infoData, 'license');
  const path = value(infoData, '_path');
  const installedAt = value(infoData, '_installed_at', 'installed_at');
  const installedSource = value(infoData, '_installed_source');
  const sourceLabel = hubSourceLabel(source);
  const sourceGuidance = hubSourceGuidance(source);
  const isHubManaged = isHubManagedKind(kind);
  const managementLabel = hubManagementLabel(kind);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="fixed inset-0 bg-black/60" onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={`${component.name} component info`}
        className="relative bg-card border border-border rounded-lg p-6 w-full max-w-lg space-y-4 shadow-xl max-h-[80vh] overflow-y-auto"
      >
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-lg font-semibold text-foreground">{component.name}</h3>
            <span className="text-xs text-muted-foreground capitalize">{component.kind} · {sourceLabel}</span>
          </div>
          <button onClick={onClose} className="text-muted-foreground hover:text-foreground p-1">
            <X className="w-4 h-4" />
          </button>
        </div>
        {infoLoading ? (
          <div className="text-sm text-muted-foreground py-4 text-center">Loading...</div>
        ) : infoData ? (
          <div className="space-y-5">
            <section className="rounded-lg border border-border bg-background/60 p-4">
              <h4 className="text-xs font-medium uppercase tracking-wide text-muted-foreground mb-3">Component</h4>
              <dl className="space-y-2">
                <Row label="Name"><code>{name}</code></Row>
                <Row label="Kind"><span className="capitalize">{kind}</span></Row>
                <Row label="Version">{version || 'Not declared'}</Row>
                <Row label="Description">{description}</Row>
                <Row label="Author">{author}</Row>
                <Row label="License">{license}</Row>
              </dl>
            </section>

            <section className="rounded-lg border border-border bg-background/60 p-4">
              <h4 className="text-xs font-medium uppercase tracking-wide text-muted-foreground mb-3">Trust & Provenance</h4>
              <dl className="space-y-2">
                <Row label="Source">{sourceLabel}</Row>
                <Row label="Trust">{sourceGuidance}</Row>
                <Row label="Raw Source"><code>{source || 'Unknown'}</code></Row>
                <Row label="Installed">{infoData._installed ? 'Yes' : 'No'}</Row>
                <Row label="Installed At">{installedAt}</Row>
                <Row label="Install Source">{installedSource}</Row>
                <Row label="Management">{managementLabel}</Row>
                {isHubManaged && <Row label="Installability">This kind is curated through hub source sync, not direct install.</Row>}
                <Row label="Cache Path"><code className="text-xs">{path}</code></Row>
              </dl>
            </section>
          </div>
        ) : (
          <div className="text-sm text-muted-foreground py-4 text-center">No additional info available</div>
        )}
        <div className="flex justify-end">
          <Button variant="outline" size="sm" onClick={onClose}>Close</Button>
        </div>
      </div>
    </div>
  );
}
