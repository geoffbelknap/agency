import { X } from 'lucide-react';
import { Component } from '../../types';
import { Button } from '../../components/ui/button';

interface ComponentInfoDialogProps {
  component: Component;
  infoData: any;
  infoLoading: boolean;
  onClose: () => void;
}

export function ComponentInfoDialog({ component, infoData, infoLoading, onClose }: ComponentInfoDialogProps) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="fixed inset-0 bg-black/60" onClick={onClose} />
      <div className="relative bg-card border border-border rounded-lg p-6 w-full max-w-lg space-y-4 shadow-xl max-h-[80vh] overflow-y-auto">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-lg font-semibold text-foreground">{component.name}</h3>
            <span className="text-xs text-muted-foreground capitalize">{component.kind} · {component.source}</span>
          </div>
          <button onClick={onClose} className="text-muted-foreground hover:text-foreground p-1">
            <X className="w-4 h-4" />
          </button>
        </div>
        {infoLoading ? (
          <div className="text-sm text-muted-foreground py-4 text-center">Loading...</div>
        ) : infoData ? (
          <pre className="font-mono text-xs text-muted-foreground bg-background rounded p-4 overflow-x-auto whitespace-pre-wrap">
            {JSON.stringify(infoData, null, 2)}
          </pre>
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
