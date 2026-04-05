// src/app/screens/channels/HelpDialog.tsx
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '../../components/ui/dialog';

interface HelpDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function HelpDialog({ open, onOpenChange }: HelpDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Keyboard Shortcuts</DialogTitle>
          <DialogDescription>
            Available keyboard shortcuts for navigating channels and panels.
          </DialogDescription>
        </DialogHeader>
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border">
              <th className="py-2 text-left font-medium text-muted-foreground">Shortcut</th>
              <th className="py-2 text-left font-medium text-muted-foreground">Action</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            <tr>
              <td className="py-2 pr-4">
                <kbd className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">Ctrl+K</kbd>
                <span className="mx-1 text-muted-foreground">/</span>
                <kbd className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">⌘K</kbd>
              </td>
              <td className="py-2 text-foreground">Toggle search panel</td>
            </tr>
            <tr>
              <td className="py-2 pr-4">
                <kbd className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">Escape</kbd>
              </td>
              <td className="py-2 text-foreground">Close search / thread panel</td>
            </tr>
            <tr>
              <td className="py-2 pr-4">
                <kbd className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">Alt+↑</kbd>
              </td>
              <td className="py-2 text-foreground">Select previous channel</td>
            </tr>
            <tr>
              <td className="py-2 pr-4">
                <kbd className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">Alt+↓</kbd>
              </td>
              <td className="py-2 text-foreground">Select next channel</td>
            </tr>
            <tr>
              <td className="py-2 pr-4">
                <kbd className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">?</kbd>
              </td>
              <td className="py-2 text-foreground">Show this help dialog</td>
            </tr>
          </tbody>
        </table>
      </DialogContent>
    </Dialog>
  );
}
