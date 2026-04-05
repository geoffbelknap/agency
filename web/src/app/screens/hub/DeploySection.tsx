import { Component } from '../../types';
import { Button } from '../../components/ui/button';
import { ConfirmDialog } from '../../components/ConfirmDialog';

interface DeploySectionProps {
  installedComponents: Component[];
  selectedPackName: string;
  onSelectPack: (name: string) => void;
  deploying: boolean;
  deployResult: string | null;
  onDeploy: () => void;
  teardownTarget: string | null;
  onTeardownRequest: (packName: string) => void;
  onTeardownConfirm: () => void;
  onTeardownCancel: () => void;
}

export function DeploySection({
  installedComponents,
  selectedPackName,
  onSelectPack,
  deploying,
  deployResult,
  onDeploy,
  teardownTarget,
  onTeardownRequest,
  onTeardownConfirm,
  onTeardownCancel,
}: DeploySectionProps) {
  const installedPacks = installedComponents.filter((c) => c.kind === 'pack');

  return (
    <div className="space-y-6">
      {/* Pack Selector */}
      <div className="bg-card border border-border rounded p-4 md:p-6">
        <h3 className="text-sm font-semibold text-foreground/80 mb-4">Deploy Pack</h3>
        <div className="flex gap-2">
          <select
            className="flex-1 bg-background border border-border text-foreground/80 rounded px-3 py-2 text-sm"
            value={selectedPackName}
            onChange={(e) => onSelectPack(e.target.value)}
          >
            <option value="">Select installed pack...</option>
            {installedPacks.map((pack) => (
              <option key={pack.id} value={pack.name}>
                {pack.name}
              </option>
            ))}
          </select>
          <Button size="sm" onClick={onDeploy} disabled={deploying || !selectedPackName}>
            {deploying ? 'Deploying...' : 'Deploy'}
          </Button>
        </div>
        {deployResult && (
          <pre className="mt-4 font-mono text-xs text-muted-foreground bg-background rounded p-3 overflow-x-auto">
            {deployResult}
          </pre>
        )}
      </div>

      {/* Installed Packs for Teardown */}
      <div>
        <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wide mb-3">
          Installed Packs
        </h3>
        <div className="bg-card border border-border rounded overflow-x-auto">
          <table className="w-full text-sm min-w-[480px]">
            <thead>
              <tr className="border-b border-border text-xs text-muted-foreground uppercase tracking-wide">
                <th className="text-left p-3 md:p-4 font-medium">Pack Name</th>
                <th className="text-left p-3 md:p-4 font-medium">Source</th>
                <th className="text-left p-3 md:p-4 font-medium">Installed At</th>
                <th className="text-left p-3 md:p-4 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {installedPacks.length === 0 ? (
                <tr>
                  <td colSpan={4} className="p-8 text-center text-muted-foreground text-sm">
                    No packs installed
                  </td>
                </tr>
              ) : (
                installedPacks.map((pack) => (
                  <tr
                    key={pack.id}
                    className="border-b border-border hover:bg-secondary/50 transition-colors"
                  >
                    <td className="p-4">
                      <code className="text-foreground">{pack.name}</code>
                    </td>
                    <td className="p-4">
                      <span className="text-muted-foreground text-xs">{pack.source}</span>
                    </td>
                    <td className="p-4">
                      <span className="text-muted-foreground text-xs">
                        {(pack as any).installedAt || '—'}
                      </span>
                    </td>
                    <td className="p-4">
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-7 text-xs"
                        onClick={() => onTeardownRequest(pack.name)}
                      >
                        Teardown
                      </Button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      <ConfirmDialog
        open={!!teardownTarget}
        onOpenChange={(open) => { if (!open) onTeardownCancel(); }}
        title="Teardown Pack"
        description={`Are you sure you want to tear down "${teardownTarget}"?`}
        confirmLabel="Teardown"
        variant="destructive"
        onConfirm={onTeardownConfirm}
      />
    </div>
  );
}
