import { useState } from 'react';
import { AlertTriangle } from 'lucide-react';
import { Button } from '../../components/ui/button';
import { ConfirmDialog } from '../../components/ConfirmDialog';

interface DangerZoneTabProps {
  onDestroy: () => Promise<void>;
  destroying: boolean;
}

export function DangerZoneTab({ onDestroy, destroying }: DangerZoneTabProps) {
  const [showDestroyConfirm, setShowDestroyConfirm] = useState(false);

  const handleConfirm = async () => {
    await onDestroy();
    setShowDestroyConfirm(false);
  };

  return (
    <>
      <div className="bg-red-50 dark:bg-red-950/20 border-2 border-red-200 dark:border-red-900 rounded p-6">
        <h3 className="text-lg font-semibold text-red-400 mb-2 flex items-center gap-2">
          <AlertTriangle className="w-5 h-5" />
          Danger Zone
        </h3>
        <p className="text-sm text-muted-foreground mb-4">
          Destructive actions that cannot be undone. Proceed with extreme caution.
        </p>
        <div className="space-y-4">
          <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3 bg-card border border-red-200 dark:border-red-900 rounded p-4">
            <div>
              <div className="text-sm font-medium text-foreground">Destroy All</div>
              <div className="text-xs text-muted-foreground mt-0.5">
                Destroys all agents and infrastructure
              </div>
            </div>
            <Button
              variant="destructive"
              size="sm"
              onClick={() => setShowDestroyConfirm(true)}
              disabled={destroying}
            >
              {destroying ? 'Destroying...' : 'Destroy All'}
            </Button>
          </div>
        </div>
      </div>

      <ConfirmDialog
        open={showDestroyConfirm}
        onOpenChange={setShowDestroyConfirm}
        title="Destroy All"
        description="This will destroy all agents and infrastructure. This action cannot be undone."
        confirmLabel="Destroy Everything"
        variant="destructive"
        onConfirm={handleConfirm}
      />
    </>
  );
}
